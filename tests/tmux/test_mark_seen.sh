#!/usr/bin/env bash
# _claude/hooks/tmux-mark-seen.sh (window 選択で 🔔 input → 🔕 seen に降格) のテスト。
#
# 固定する不変条件:
#   - 対象 window の input pane だけが seen になる (working / idle は触らない)
#   - 別 window の input は触らない (window スコープ)
#   - **tmux クライアントの起動回数が pane 数に比例しない** (issue 083: window 切替ごとに
#     走る hook なので、pane 数ぶん fork していた。if-shell は競合対策で pane ごとに必要だが
#     `;` 区切りで 1 回の起動にまとめられる)
#
# ⚠️ socket 隔離: $TMUX は TMUX_TMPDIR より優先されるため必ず unset する。隔離できたことを
# 「本番セッションが見えない」で実証してからサーバを作る (rules/tmux-probe-requires-socket-isolation.md)。
set -uo pipefail
unset CDPATH
unset TMUX TMUX_PANE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/tmux-mark-seen.sh"
TMUX_BIN_PATH="${TMUX_BIN:-tmux}"
fails=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

command -v "$TMUX_BIN_PATH" >/dev/null 2>&1 || {
  printf 'SKIP: tmux が無い環境なので実行系の検査は落とした\n'
  exit 0
}
[ -x "$HOOK" ] || { ng "hook が無い/実行不可: $HOOK"; exit 1; }

TMUX_TMPDIR="$(mktemp -d)"
export TMUX_TMPDIR
# shellcheck source=tests/tmux/lib/isolate_env.sh
source "$ROOT_DIR/tests/tmux/lib/isolate_env.sh"
# socket 名は短く: mktemp の TMUX_TMPDIR が長いため、名前を伸ばすと sun_path (104 byte) を超える
SOCK="dfms-$$"
REAL_TMUX="$(command -v "$TMUX_BIN_PATH")"
T=("$REAL_TMUX" -L "$SOCK" -f /dev/null)
cleanup() { "$REAL_TMUX" -L "$SOCK" kill-server >/dev/null 2>&1; rm -rf "$TMUX_TMPDIR"; }
trap cleanup EXIT

# --- 隔離の実証: この socket に本番セッションが見えないこと -----------------------------
iso="$("$REAL_TMUX" -L "$SOCK" ls 2>&1)"
case "$iso" in
  *"no server running"*|*"error connecting"*|"") ok "隔離: 専用 socket にサーバが無い ($SOCK)" ;;
  *) ng "隔離できていない (既存サーバが見えた): $iso"; exit 1 ;;
esac

# --- 準備: 4 pane (input / working / idle / input) + 別 window の input --------------
win="$("${T[@]}" new-session -d -s ms -x 80 -y 24 -P -F '#{window_id}')" || { ng "セッションを作れない"; exit 1; }
panes=("$("${T[@]}" list-panes -t "$win" -F '#{pane_id}')")
for _ in 1 2 3; do "${T[@]}" split-window -d -t "$win" -l 3; done
mapfile -t panes < <("${T[@]}" list-panes -t "$win" -F '#{pane_id}')
[ "${#panes[@]}" -eq 4 ] || { ng "pane が 4 枚にならない (${#panes[@]} 枚)"; exit 1; }
states=('🔔 input' '⚙ working' '✓ idle' '🔔 input')
for i in 0 1 2 3; do "${T[@]}" set -p -t "${panes[$i]}" @claude_state "${states[$i]}"; done
other_win="$("${T[@]}" new-window -d -t ms -P -F '#{window_id}')"
other_pane="$("${T[@]}" list-panes -t "$other_win" -F '#{pane_id}')"
"${T[@]}" set -p -t "$other_pane" @claude_state '🔔 input'

# --- hook を走らせる (tmux は shim 経由: 起動回数を数え、socket を隔離側へ向ける) -------
BIN_DIR="$TMUX_TMPDIR/bin"
mkdir -p "$BIN_DIR"
CALLS="$TMUX_TMPDIR/calls"
: > "$CALLS"
cat > "$BIN_DIR/tmux" <<EOS
#!/bin/sh
echo "\$*" >> "$CALLS"
exec "$REAL_TMUX" -L "$SOCK" "\$@"
EOS
chmod +x "$BIN_DIR/tmux"
PATH="$BIN_DIR:$PATH" "$HOOK" "$win" >"$TMUX_TMPDIR/hook.out" 2>"$TMUX_TMPDIR/hook.err"
rc=$?

# --- 検査 ---------------------------------------------------------------------------
[ "$rc" -eq 0 ] || ng "hook が非 0 で終わった (rc=$rc)"
if [ -s "$TMUX_TMPDIR/hook.out" ] || [ -s "$TMUX_TMPDIR/hook.err" ]; then
  ng "無音契約が破れている (出力があった): $(head -2 "$TMUX_TMPDIR/hook.out" "$TMUX_TMPDIR/hook.err")"
else
  ok "無音契約 (stdout/stderr へ何も出さない)"
fi

state_of() { "${T[@]}" display-message -p -t "$1" '#{@claude_state}'; }
want=('🔕 seen' '⚙ working' '✓ idle' '🔕 seen')
for i in 0 1 2 3; do
  got="$(state_of "${panes[$i]}")"
  if [ "$got" = "${want[$i]}" ]; then
    ok "pane $i: ${states[$i]} → $got"
  else
    ng "pane $i: ${states[$i]} が $got になった (期待 ${want[$i]})"
  fi
done
got="$(state_of "$other_pane")"
if [ "$got" = '🔔 input' ]; then
  ok "別 window の input は触らない"
else
  ng "別 window の input を書き換えた ($got)"
fi

# ⚠️ ここが issue 083 の本体。pane 数 (4) に比例して起動すると 1 + 4 = 5 回になる。
# 期待は「list-panes 1 回 + まとめた if-shell 1 回」= 2 回。
calls="$(wc -l < "$CALLS" | tr -d ' ')"
if [ "$calls" -le 2 ]; then
  ok "tmux クライアントの起動が pane 数に比例しない ($calls 回 / pane 4 枚)"
else
  ng "tmux を $calls 回起動している (pane 数に比例している。期待 2 回)"
  cat "$CALLS"
fi

if [ "$fails" -gt 0 ]; then
  printf '\nFAIL: mark-seen のテストが %d 件失敗\n' "$fails"
  exit 1
fi
printf '\nAll mark-seen tests passed successfully!\n'
