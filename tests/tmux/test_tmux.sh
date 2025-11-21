#!/usr/bin/env zsh

set -euo pipefail

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
SOCKET_NAME="dotfiles-test-$$"
log_file="$TMUX_TMPDIR/tmux.log"

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

print "[test-tmux:zsh] dumping global options"
run_with_check "$log_file" "show-options failed" "fail" \
  "${TMUX_CMD[@]}" show-options -g

print "[test-tmux:zsh] verifying custom key bindings can be listed"
run_with_check "$log_file" "list-keys failed" "fail" \
  "${TMUX_CMD[@]}" list-keys

print "[test-tmux:zsh] done"
