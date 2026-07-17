#!/usr/bin/env zsh
# test_nav_mode.sh — nav モード (prefix a の片手ナビ key-table) の配線を隔離サーバで検証する。
# 守る不変条件:
#   1. prefix a で nav table へ入る
#   2. 移動系バインドは末尾で -T nav に戻る (これが無いと 1 キーでモードが終わる silent 劣化)
#   3. Esc / q は root へ戻る (退場経路)
#   4. f (fzf picker) はモードを持続しない (対話 UI に入るため)
#   5. status-left に nav バッジ (client_key_table 参照) がある (入りっぱなし可視化)
set -euo pipefail
unset CDPATH

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
# 短い socket 名にする: mktemp の長い TMUX_TMPDIR と合算で macOS の sun_path 104 byte を
# 超えると "File name too long" で起動できない (dotfiles-nav-test-$$ で実際に超えた)
SOCKET_NAME="nav-$$"
log_file="$TMUX_TMPDIR/tmux.log"

# resurrect / debounce 保存を実データから隔離 (test_tmux.sh と同じ理由・同じ lib)
source "$ROOT_DIR/tests/tmux/lib/isolate_env.sh"

if ! command -v "$TMUX_BIN_PATH" >/dev/null 2>&1; then
  print -u2 "Error: tmux binary not found. Install tmux or set \$TMUX_BIN."
  exit 1
fi

TMUX_CMD=("$TMUX_BIN_PATH" -L "$SOCKET_NAME" -f "$CONF_FILE")

cleanup() {
  "$TMUX_BIN_PATH" -L "$SOCKET_NAME" kill-server >/dev/null 2>&1 || true
  rm -rf "$TMUX_TMPDIR"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

if ! "${TMUX_CMD[@]}" new-session -d -s navtest >"$log_file" 2>&1; then
  if grep -qiE "operation not permitted|permission denied" "$log_file"; then
    print -u2 "[test-nav-mode] skipped: tmux cannot create sockets in this environment"
    exit 0
  fi
  print -u2 "[test-nav-mode] server 起動に失敗:"
  cat "$log_file" >&2
  exit 1
fi

fails=0
t() { "$TMUX_BIN_PATH" -L "$SOCKET_NAME" "$@"; }
assert() { # <説明> <grep パターン> <対象文字列>
  local desc="$1" pat="$2" text="$3"
  if print -r -- "$text" | grep -qE "$pat"; then
    print -r -- "✓ $desc"
  else
    print -ru2 -- "✗ $desc (pattern: $pat)"
    fails=$((fails + 1))
  fi
}

prefix_keys=$(t list-keys -T prefix)
nav_keys=$(t list-keys -T nav)
status_left=$(t show -g status-left)

assert "prefix a で nav へ入る" 'bind-key .* -T prefix +a +switch-client -T nav' "$prefix_keys"
assert "prefix C-a でも入る (Ctrl 押しっぱなし耐性)" 'bind-key .* -T prefix +C-a +switch-client -T nav' "$prefix_keys"

for key in h j k l a s w d; do
  assert "nav $key = ペイン移動 + モード持続" "bind-key .* -T nav +$key +if-shell .*select-pane.*switch-client -T nav" "$nav_keys"
done

assert "nav n = 次 window + 持続"     'bind-key .* -T nav +n +next-window.*switch-client -T nav' "$nav_keys"
assert "nav p = 前 window + 持続"     'bind-key .* -T nav +p +previous-window.*switch-client -T nav' "$nav_keys"
assert "nav Tab = 次 window + 持続"   'bind-key .* -T nav +Tab +next-window.*switch-client -T nav' "$nav_keys"
assert "nav BTab = 前 window + 持続"  'bind-key .* -T nav +BTab +previous-window.*switch-client -T nav' "$nav_keys"
for n in 1 5 9; do
  assert "nav $n = window 直行 + 持続" "bind-key .* -T nav +$n +select-window -t :=$n.*switch-client -T nav" "$nav_keys"
done
assert "nav z = ズーム + 持続" 'bind-key .* -T nav +z +resize-pane -Z.*switch-client -T nav' "$nav_keys"

assert "nav Escape = 退場 (root へ)" 'bind-key .* -T nav +Escape +switch-client -T root' "$nav_keys"
assert "nav q = 退場 (root へ)"      'bind-key .* -T nav +q +switch-client -T root' "$nav_keys"

# f は display-popup を開くだけでモードを持続しない (persist させると popup 中の入力と競合する)
f_line=$(print -r -- "$nav_keys" | grep -E 'bind-key .* -T nav +f ' || true)
assert "nav f = fzf picker を開く" 'display-popup' "$f_line"
if print -r -- "$f_line" | grep -q 'switch-client -T nav'; then
  print -ru2 -- "✗ nav f がモードを持続している (popup と競合するため持続させない設計)"
  fails=$((fails + 1))
else
  print -r -- "✓ nav f はモードを持続しない"
fi

assert "status-left に nav バッジ (入りっぱなし可視化)" 'client_key_table.*nav.*NAV' "$status_left"

if [[ "$fails" -gt 0 ]]; then
  print -ru2 -- "[test-nav-mode] $fails 件失敗"
  exit 1
fi
print -r -- "[test-nav-mode] all assertions passed"
