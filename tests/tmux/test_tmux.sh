#!/usr/bin/env zsh

set -euo pipefail
unset CDPATH

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
SOCKET_NAME="dotfiles-test-$$"
log_file="$TMUX_TMPDIR/tmux.log"

# resurrect / debounce 保存を実データから隔離する。
# _tmux.conf には window-linked hook（scripts/tmux_resurrect_debounced_save.sh を
# 走らせる debounce 保存）と continuum autosave があり、テストで conf をロードして
# セッションを作ると保存が走り得る。resurrect の保存先（helpers.sh）は
#   1) @resurrect-dir / 2) ~/.tmux/resurrect が在ればそれ / 3) $XDG_DATA_HOME/...
# の順で解決されるため、XDG_DATA_HOME だけ差し替えても実 HOME に ~/.tmux/resurrect が
# 在る環境では本物を触り得る。そこで HOME ごと temp に隔離して全候補を temp に倒す。
# DOTFILES_DIR は明示固定する（conf の plugin パスは ${DOTFILES_DIR:-$HOME/dotfiles}
# なので HOME を temp にすると $HOME/dotfiles が壊れるため）。
export HOME="$TMUX_TMPDIR/home"
export DOTFILES_DIR="$ROOT_DIR"
export XDG_DATA_HOME="$HOME/.local/share"
export TT_DEBOUNCE_STATE_DIR="$HOME/.cache/tt-debounce"
mkdir -p "$HOME" "$XDG_DATA_HOME" "$TT_DEBOUNCE_STATE_DIR"

if ! command -v "$TMUX_BIN_PATH" >/dev/null 2>&1; then
  print -u2 "Error: tmux binary not found. Install tmux or set \$TMUX_BIN."
  exit 1
fi

if [[ ! -f "$CONF_FILE" ]]; then
  print -u2 "Error: tmux config $CONF_FILE not found."
  exit 1
fi

TMUX_CMD=("$TMUX_BIN_PATH" -L "$SOCKET_NAME" -f "$CONF_FILE")

cleanup() {
  "$TMUX_BIN_PATH" -L "$SOCKET_NAME" kill-server >/dev/null 2>&1 || true
  rm -rf "$TMUX_TMPDIR"
}
trap cleanup EXIT

handle_result() {
  local log="$1"
  local desc="$2"
  local allow_skip="$3"
  if grep -qiE "operation not permitted|permission denied" "$log"; then
    if [[ "$allow_skip" == "skip" ]]; then
      print -u2 "[test-tmux:zsh] skipped: tmux cannot create sockets in this environment"
      cat "$log" >&2
      exit 0
    fi
  fi
  print -u2 "[test-tmux:zsh] $desc"
  cat "$log" >&2
  exit 1
}

run_with_check() {
  local log="$1"
  local desc="$2"
  local allow_skip="$3"
  shift 3
  if ! "$@" >"$log" 2>&1 || grep -qi "error" "$log"; then
    handle_result "$log" "$desc" "$allow_skip"
  fi
}

probe_dir=$(mktemp -d)
probe_log="$probe_dir/probe.log"
probe_socket="dotfiles-probe-$$"
run_with_check "$probe_log" "probe session failed" "skip" \
  env TMUX_TMPDIR="$probe_dir" "$TMUX_BIN_PATH" -L "$probe_socket" new-session -d -s dotfiles_probe "tail -f /dev/null"
"$TMUX_BIN_PATH" -L "$probe_socket" kill-server >/dev/null 2>&1 || true
rm -rf "$probe_dir"

print "[test-tmux:zsh] starting server with $CONF_FILE"
run_with_check "$log_file" "failed to create test session" "skip" \
  "${TMUX_CMD[@]}" new-session -d -s dotfiles_test "tail -f /dev/null"

# conf ロード時に tmux が出す設定警告 (invalid option / unknown ...) を検出する。
# これらは run_with_check の grep "error" では拾えない
# (例: "invalid option: pane-scrollbars" は "error" を含まない)。
# また -f での起動時ロード (上の new-session) は警告を呼び出し元の stderr へ返さない
# (サーバ側に記録されるだけ) ため、source-file を明示実行して呼び出しの出力に警告を
# 捕捉する (実測: source-file は呼び出し元へ返す / -f 起動は返さない)。
# 古い tmux でバージョンガード無しに新オプションを足すと壊れる回帰
# (pane-scrollbars は tmux 3.6+ 専用) を防ぐのが目的。
print "[test-tmux:zsh] checking config load for tmux warnings"
"${TMUX_CMD[@]}" source-file "$CONF_FILE" >"$log_file" 2>&1 || true
conf_warnings=$(grep -niE 'invalid option|unknown option|unknown command|unknown key|invalid or unknown' "$log_file" || true)
if [[ -n "$conf_warnings" ]]; then
  print -u2 "[test-tmux:zsh] tmux reported config warnings while loading $CONF_FILE:"
  print -u2 "$conf_warnings"
  exit 1
fi

print "[test-tmux:zsh] dumping global options"
run_with_check "$log_file" "show-options failed" "fail" \
  "${TMUX_CMD[@]}" show-options -g

print "[test-tmux:zsh] verifying custom key bindings can be listed"
run_with_check "$log_file" "list-keys failed" "fail" \
  "${TMUX_CMD[@]}" list-keys

print "[test-tmux:zsh] done"
