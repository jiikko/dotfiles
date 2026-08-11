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
  show-option|show)
    case "$*" in
      *@agent_panel_on*)   echo "${STUB_PANEL_ON:-}" ;;
      *@agent_panel_pane*) echo "${STUB_PANEL_PANE:-}" ;;
      *@agent_panel_saving*) echo "${STUB_SAVING:-}" ;;
      *@agent_panel_saved_window*) echo "${STUB_SAVED_WINDOW:-}" ;;
      *@tt-restore-in-progress*) echo "${STUB_RESTORING:-}" ;;
    esac ;;
  list-sessions) printf '%s\n' "${STUB_SESSIONS:-main}" ;;
  display-message)
    case "$*" in
      *window_width*) echo '200' ;;
      *session_name*) echo "${STUB_SESSION:-main}:" ;;
      *window_id*)
        # -t %N (pane 指定) は所属 window、無指定はカレント window
        case "$*" in
          *-t\ %*) [ "${STUB_ALIVE:-1}" = 1 ] && echo "${STUB_PANE_WINDOW:-@1}" ;;
          *) echo '@1' ;;
        esac ;;
      *pane_active*)
        echo "${STUB_PANEL_ACTIVE:-0}" ;;
      *pane_id*)
        [ "${STUB_ALIVE:-1}" = 1 ] && echo "${STUB_PANEL_PANE:-%9}" ;;
    esac ;;
  list-panes)
    # kill_panel の掃討クエリ (pane_start_command) には「render 実行中の panel pane」を
    # 返す (STUB_RENDER_PANES: 空白区切りの pane id 列。STUB_SELF = 実スクリプトのパス)。
    # list_agents のクエリ (@claude_state) には何も返さない = agent 0 件 (高さ下限 3)
    case "$*" in
      *pane_start_command*)
        for rp in ${STUB_RENDER_PANES:-}; do
          echo "$rp ${STUB_SELF:-/x/tmux_agent_panel.sh} render"
        done ;;
      *-F\ x*)   # unfocus の pane 数カウント (STUB_WIN_PANES 個の x を返す)
        i=0; while [ "$i" -lt "${STUB_WIN_PANES:-2}" ]; do echo x; i=$((i+1)); done ;;
      *pane_id*) # unfocus の弾き先候補列挙
        printf '%s\n' "${STUB_PANEL_PANE:-%9}" '%50' ;;
    esac ;;
  new-pane) echo '%42' ;;
esac
exit 0
EOS
chmod +x "$TMP_DIR/bin/tmux"
export PATH="$TMP_DIR/bin:$PATH"
export TMUX="stub,1,0"
export STUB_SELF="$SCRIPT"                            # kill_panel 掃討の自己一致用
export TT_AGENT_PANEL_LOCK="$TMP_DIR/panel.lock"      # follow の直列化 lock をテスト内に隔離

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
grep -q -- '-X 50' "$CALLS" || ng "toggle on: 右上座標 -X (200-150) が渡らない"
grep -q 'set-option -g @agent_panel_pane %42' "$CALLS" || ng "toggle on: pane id が記録されない"
grep -q 'select-pane -d -t %42' "$CALLS" || ng "toggle on: 入力無効化 (select-pane -d) が無い"
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
STUB_PANEL_ON=1 STUB_PANEL_PANE='%9' STUB_RENDER_PANES='%9' "$SCRIPT" toggle '@1' || ng "toggle off が失敗"
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

# --- follow (復元中 / bootstrap): panel を作らない --------------------------------
# 復元前に panel pane を作ると resurrect の restore_from_scratch (総 pane 数=1 条件) を
# 破ってスクロールバック復元が不発になる。デフォルト表示化で必須になったガード
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='' STUB_RESTORING="$(date +%s)" "$SCRIPT" follow '@2' || ng "follow (復元中) が非 0"
if grep -Eq 'new-pane|kill-pane' "$CALLS"; then
  ng "follow (復元中): pane 操作をしている (restore_from_scratch を壊す)"
else
  ok "follow (復元中): pane 操作なし"
fi
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='' STUB_SESSIONS='__tt_hold_123' "$SCRIPT" follow '@2' || ng "follow (hold のみ) が非 0"
if grep -Eq 'new-pane|kill-pane' "$CALLS"; then
  ng "follow (hold のみ): pane 操作をしている (bootstrap を壊す)"
else
  ok "follow (bootstrap: hold のみ): pane 操作なし"
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
STUB_PANEL_ON=1 STUB_PANEL_PANE='%9' STUB_RENDER_PANES='%9' STUB_PANE_WINDOW='@1' "$SCRIPT" follow '@2' || ng "follow (別 window) が失敗"
grep -q 'kill-pane -t %9' "$CALLS" || ng "follow (別 window): 旧 panel を kill しない"
grep -q 'new-pane -d' "$CALLS" || ng "follow (別 window): 新 window に作らない"
grep -q -- '-t @2' "$CALLS" || ng "follow (別 window): 移動先 window を target にしない"
ok "follow (別 window): kill + create (上の ✗ が無ければ)"

# --- follow (popup 専用セッションへは追従しない) ----------------------------------
# scratch popup の開閉は client-attached / session-changed を発火させるが、そこへ panel を
# 作ると popup を覆う + 閉じた後にセッションへ取り残される (孤児の温床。2026-08-08 実発)
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='' STUB_SESSION='scratch' "$SCRIPT" follow '@9' || ng "follow (scratch) が非 0"
if grep -Eq 'new-pane|kill-pane' "$CALLS"; then
  ng "follow (scratch): popup 専用セッションに panel を作っている"
else
  ok "follow (popup 専用セッション): 追従しない"
fi

# --- kill の掃討: 記録に無い孤児 panel も消す --------------------------------------
# 並走 follow の二重作成で記録から漏れた render pane が残っても、toggle off が全部消す
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='%9' STUB_RENDER_PANES='%9 %77' "$SCRIPT" toggle '@1' || ng "toggle off (孤児あり) が失敗"
grep -q 'kill-pane -t %9'  "$CALLS" || ng "掃討: 記録 pane %9 を kill しない"
grep -q 'kill-pane -t %77' "$CALLS" || ng "掃討: 孤児 %77 を kill しない"
ok "掃討: 記録外の孤児 render pane も kill (上の ✗ が無ければ)"

# --- follow (panel pane が消滅済み: q 巻き添え等) ---------------------------------
: > "$CALLS"
STUB_PANEL_ON=1 STUB_PANEL_PANE='%9' STUB_ALIVE=0 "$SCRIPT" follow '@2' || ng "follow (pane 消滅) が失敗"
grep -q 'kill-pane' "$CALLS" && ng "follow (pane 消滅): 死んだ pane を kill しようとする"
grep -q 'new-pane -d' "$CALLS" || ng "follow (pane 消滅): 作り直さない"
ok "follow (pane 消滅): kill せず作り直す (上の ✗ が無ければ)"

# --- save-hide / save-show (resurrect 保存中の退避。呼び手は tmux_resurrect_save.sh) ----
: > "$CALLS"
STUB_PANEL_PANE='%9' STUB_RENDER_PANES='%9' STUB_PANE_WINDOW='@3' "$SCRIPT" save-hide || ng "save-hide が非 0"
grep -Eq 'set-option -g @agent_panel_saving [0-9]+' "$CALLS" || ng "save-hide: saving epoch を立てない"
grep -q 'set-option -g @agent_panel_saved_window @3' "$CALLS" || ng "save-hide: 退避元 window を記録しない"
grep -q 'kill-pane -t %9' "$CALLS" || ng "save-hide: panel を kill しない"
ok "save-hide: epoch + 退避元記録 + kill (上の ✗ が無ければ)"

: > "$CALLS"
# 退避窓の間に toggle 連打等で panel が生えていても、save-show は掃討してから作る
# (作る前に必ず kill_panel の規律。敵対レビュー指摘 2026-08-08)
STUB_PANEL_ON=1 STUB_SAVED_WINDOW='@3' STUB_RENDER_PANES='%88' "$SCRIPT" save-show || ng "save-show が非 0"
grep -q 'set-option -gu @agent_panel_saving' "$CALLS" || ng "save-show: saving を降ろさない"
grep -q 'kill-pane -t %88' "$CALLS" || ng "save-show: 既存 panel を掃討しない (二重化)"
grep -q 'new-pane -d' "$CALLS" || ng "save-show: panel を復帰させない"
grep -q -- '-t @3' "$CALLS" || ng "save-show: 退避元 window に戻さない"
ok "save-show: saving 解除 + 掃討 + 退避元へ復帰 (上の ✗ が無ければ)"

# --- toggle on (resurrect 保存中): 状態だけ立てて作らない (敵対レビュー指摘 2026-08-08) ---
# 保存中に作ると実行中の save.sh のダンプに写り込み、退避の意味が無くなる
: > "$CALLS"
STUB_PANEL_ON='' STUB_SAVING="$(date +%s)" "$SCRIPT" toggle '@1' || ng "toggle on (保存中) が非 0"
grep -q 'set-option -g @agent_panel_on 1' "$CALLS" || ng "toggle on (保存中): on 状態を立てない"
grep -q 'new-pane' "$CALLS" && ng "toggle on (保存中): 保存中なのに panel を作っている" \
  || ok "toggle on (保存中): 状態のみ立てて作成しない"

: > "$CALLS"
STUB_PANEL_ON='' STUB_SAVED_WINDOW='@3' "$SCRIPT" save-show || ng "save-show (panel off) が非 0"
grep -q 'new-pane' "$CALLS" && ng "save-show (panel off): off なのに作っている" \
  || ok "save-show (panel off): 作らない (saving 解除のみ)"

# --- follow (resurrect 保存中): 作らない (save-hide の退避を follow が壊さない) ---------
: > "$CALLS"
STUB_PANEL_ON=1 STUB_SAVING="$(date +%s)" "$SCRIPT" follow '@2' || ng "follow (保存中) が非 0"
if grep -Eq 'new-pane|kill-pane' "$CALLS"; then
  ng "follow (保存中): 退避中に panel を作り直している (写り込みが復活する)"
else
  ok "follow (保存中): pane 操作なし"
fi
# stale な saving epoch (crash 残置) では follow が止まり続けない (TTL 120s)
: > "$CALLS"
STUB_PANEL_ON=1 STUB_SAVING="$(( $(date +%s) - 600 ))" "$SCRIPT" follow '@2' || ng "follow (stale saving) が非 0"
grep -q 'new-pane -d' "$CALLS" || ng "follow (stale saving): TTL 超過なのに作らない (永久停止)"
ok "follow (stale saving): TTL 超過は無視して作る"

# --- unfocus: パネルにフォーカスが乗ったら弾き返す (別マシンで dim 実発 2026-08-09) ----
# unfocus は @agent_panel_pane の記録でなく kill_panel と同じ掃討方式 (render 実行中の
# pane を全列挙) で対象を決める (記録漏れの孤児が弾かれない穴の修正 2026-08-11)
: > "$CALLS"
STUB_RENDER_PANES='%9' STUB_PANEL_ACTIVE=1 STUB_PANE_WINDOW='@1' STUB_WIN_PANES=2 "$SCRIPT" unfocus || ng "unfocus が非 0"
grep -q 'select-pane -t @1.!' "$CALLS" || ng "unfocus: 直前アクティブ (!) へ弾き返さない"
grep -q 'kill-pane' "$CALLS" && ng "unfocus: 他 pane が居るのに panel を殺している"
ok "unfocus: アクティブなら弾き返す (上の ✗ が無ければ)"

: > "$CALLS"
STUB_RENDER_PANES='%9' STUB_PANEL_ACTIVE=0 "$SCRIPT" unfocus || ng "unfocus (非アクティブ) が非 0"
grep -Eq 'select-pane|kill-pane' "$CALLS" && ng "unfocus (非アクティブ): 何かしてしまう" \
  || ok "unfocus (非アクティブ): no-op"

: > "$CALLS"
STUB_RENDER_PANES='%9' STUB_PANEL_ACTIVE=1 STUB_PANE_WINDOW='@1' STUB_WIN_PANES=1 "$SCRIPT" unfocus || ng "unfocus (独りぼっち) が非 0"
grep -q 'kill-pane -t %9' "$CALLS" || ng "unfocus (独りぼっち): 最後の window に取り残された panel を畳まない"
ok "unfocus (panel だけの window): panel を畳んで window を閉じさせる"

# 記録 (@agent_panel_pane) に無い render 孤児にもフォーカス防御が効くこと
: > "$CALLS"
STUB_PANEL_PANE='' STUB_RENDER_PANES='%77' STUB_PANEL_ACTIVE=1 STUB_PANE_WINDOW='@1' STUB_WIN_PANES=2 "$SCRIPT" unfocus || ng "unfocus (孤児) が非 0"
grep -q 'select-pane -t @1.!' "$CALLS" || ng "unfocus (孤児): 記録に無い render pane を弾き返さない"
ok "unfocus (孤児): 記録に無い render pane も弾き返す (上の ✗ が無ければ)"

if [ "$fail" -eq 0 ]; then
  echo "test_agent_panel: all ok"
else
  echo "test_agent_panel: FAILED"
  exit 1
fi
