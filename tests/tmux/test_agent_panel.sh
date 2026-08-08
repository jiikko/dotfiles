#!/usr/bin/env bash
# scripts/tmux_agent_panel.sh の unit テスト (PATH stub 方式。実 tmux には触れない)。
# 固定する不変条件:
#   - toggle on: @agent_panel_on=1 を立て、new-pane に -d (フォーカス非奪取) と
#     -X (floating 位置) が渡り、pane id が @agent_panel_pane に記録される
#   - kill/create の直前に @agent_panel_busy へ epoch が書かれる (toast / debounce の
#     ノイズ抑止ガードの供給側。読み手は bin/tmux-toast と
#     scripts/tmux_resurrect_debounced_save.sh)
#   - toggle off: panel pane を kill し @agent_panel_on / @agent_panel_pane を unset
#   - follow: panel off なら tmux を一切呼ばず即 exit 0 (window 切替ごとの hook なので
#     軽量パスが必須) / 同一 window なら no-op / 別 window なら kill + create
#   - tmux 外 ($TMUX 無し) では非 0 で終わる
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_agent_panel.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

[[ -x "$SCRIPT" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"; exit 1; }

CALLS="$TMP_DIR/calls.log"
export CALLS

# stub tmux: 呼び出しを記録し、応答は環境変数で制御する
#   STUB_PANEL_ON=1        → @agent_panel_on が 1
#   STUB_PANEL_PANE=%N     → @agent_panel_pane の値
#   STUB_ALIVE=0           → display-message -t <pane> が空を返す (pane 消滅の再現。
#                            実 tmux 3.7b は消滅 pane でも exit 0 で空出力)
#   STUB_PANE_WINDOW=@N    → panel pane の所属 window
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  show-option)
    case "$*" in
      *@agent_panel_on*)   echo "${STUB_PANEL_ON:-}" ;;
      *@agent_panel_pane*) echo "${STUB_PANEL_PANE:-}" ;;
    esac ;;
  display-message)
    case "$*" in
      *window_width*) echo '200' ;;
      *window_id*)
        # -t %N (pane 指定) は所属 window、無指定はカレント window
        case "$*" in
          *-t\ %*) [ "${STUB_ALIVE:-1}" = 1 ] && echo "${STUB_PANE_WINDOW:-@1}" ;;
          *) echo '@1' ;;
        esac ;;
      *pane_id*)
        [ "${STUB_ALIVE:-1}" = 1 ] && echo "${STUB_PANEL_PANE:-%9}" ;;
    esac ;;
  list-panes) : ;;   # agent 0 件 (高さは下限 3 に clamp される)
  new-pane) echo '%42' ;;
esac
exit 0
EOS
chmod +x "$TMP_DIR/bin/tmux"
export PATH="$TMP_DIR/bin:$PATH"
export TMUX="stub,1,0"

fail=0
ok()   { printf '✓ %s\n' "$1"; }
ng()   { printf '✗ %s\n' "$1"; fail=1; }

# --- tmux 外では非 0 -----------------------------------------------------------
if env -u TMUX "$SCRIPT" toggle >/dev/null 2>&1; then
  ng "tmux 外で成功してしまう"
else
  ok "tmux 外では非 0 で終わる"
fi

# --- toggle on ------------------------------------------------------------------
: > "$CALLS"
STUB_PANEL_ON='' STUB_PANEL_PANE='' "$SCRIPT" toggle '@1' || ng "toggle on が失敗"
grep -q 'set-option -g @agent_panel_on 1' "$CALLS" || ng "toggle on: @agent_panel_on=1 が立たない"
grep -q 'new-pane -d' "$CALLS" || ng "toggle on: new-pane -d (フォーカス非奪取) でない"
grep -q -- '-X 162' "$CALLS" || ng "toggle on: 右上座標 -X (200-38) が渡らない"
grep -q 'set-option -g @agent_panel_pane %42' "$CALLS" || ng "toggle on: pane id が記録されない"
grep -Eq 'set-option -g @agent_panel_busy [0-9]+' "$CALLS" || ng "toggle on: busy epoch が書かれない"
# busy は new-pane より前に書く (hook は new-pane 実行中に発火するため後書きでは間に合わない)
busy_line=$(grep -n '@agent_panel_busy' "$CALLS" | head -1 | cut -d: -f1)
create_line=$(grep -n 'new-pane' "$CALLS" | head -1 | cut -d: -f1)
if [ -n "$busy_line" ] && [ -n "$create_line" ] && [ "$busy_line" -lt "$create_line" ]; then
  ok "toggle on: busy epoch → new-pane の順序 (hook 抑止が間に合う)"
else
  ng "toggle on: busy epoch が new-pane より後"
fi
ok "toggle on: 一式 (上の ✗ が無ければ)"

# --- toggle off -----------------------------------------------------------------
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='%9' "$SCRIPT" toggle '@1' || ng "toggle off が失敗"
grep -q 'kill-pane -t %9' "$CALLS" || ng "toggle off: panel pane を kill しない"
grep -q 'set-option -gu @agent_panel_on' "$CALLS" || ng "toggle off: @agent_panel_on を unset しない"
grep -q 'set-option -gu @agent_panel_pane' "$CALLS" || ng "toggle off: @agent_panel_pane を unset しない"
grep -q 'new-pane' "$CALLS" && ng "toggle off: new-pane を呼んでいる"
ok "toggle off: kill + unset (上の ✗ が無ければ)"

# --- follow (panel off) ---------------------------------------------------------
: > "$CALLS"
STUB_PANEL_ON='' "$SCRIPT" follow '@2' || ng "follow (off) が非 0"
if grep -Eq 'new-pane|kill-pane' "$CALLS"; then
  ng "follow (off): pane 操作をしている (即 exit の軽量パスが壊れた)"
else
  ok "follow (off): 即 exit で pane 操作なし"
fi

# --- follow (同一 window) -------------------------------------------------------
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='%9' STUB_PANE_WINDOW='@2' "$SCRIPT" follow '@2' || ng "follow (同一 window) が非 0"
if grep -Eq 'new-pane|kill-pane' "$CALLS"; then
  ng "follow (同一 window): no-op でない"
else
  ok "follow (同一 window): no-op"
fi

# --- follow (別 window へ移動) ---------------------------------------------------
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='%9' STUB_PANE_WINDOW='@1' "$SCRIPT" follow '@2' || ng "follow (別 window) が失敗"
grep -q 'kill-pane -t %9' "$CALLS" || ng "follow (別 window): 旧 panel を kill しない"
grep -q 'new-pane -d' "$CALLS" || ng "follow (別 window): 新 window に作らない"
grep -q -- '-t @2' "$CALLS" || ng "follow (別 window): 移動先 window を target にしない"
ok "follow (別 window): kill + create (上の ✗ が無ければ)"

# --- follow (panel pane が消滅済み: q 巻き添え等) ---------------------------------
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='%9' STUB_ALIVE=0 "$SCRIPT" follow '@2' || ng "follow (pane 消滅) が失敗"
grep -q 'kill-pane' "$CALLS" && ng "follow (pane 消滅): 死んだ pane を kill しようとする"
grep -q 'new-pane -d' "$CALLS" || ng "follow (pane 消滅): 作り直さない"
ok "follow (pane 消滅): kill せず作り直す (上の ✗ が無ければ)"

if [ "$fail" -eq 0 ]; then
  echo "test_agent_panel: all ok"
else
  echo "test_agent_panel: FAILED"
  exit 1
fi
