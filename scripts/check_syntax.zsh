#!/usr/bin/env zsh

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
tmp_log=""
tmux_tmpdir=""

cleanup() {
  [[ -n "$tmp_log" && -f "$tmp_log" ]] && rm -f "$tmp_log"
  [[ -n "$tmux_tmpdir" && -d "$tmux_tmpdir" ]] && rm -rf "$tmux_tmpdir"
}
trap cleanup EXIT

print "[syntax] checking _zshrc"
zsh -n "$ROOT_DIR/_zshrc"

print "[syntax] checking _zlogin"
zsh -n "$ROOT_DIR/_zlogin"

if command -v bash >/dev/null 2>&1; then
  print "[syntax] checking setup.sh with bash -n"
  bash -n "$ROOT_DIR/setup.sh"
fi

print "[syntax] checking _nviminit.lua via Neovim"
if ! command -v nvim >/dev/null 2>&1; then
  print -u2 "Neovim not found; skipping _nviminit.lua syntax check"
else
  tmp_log=$(mktemp)
  if ! nvim --headless -u "$ROOT_DIR/_nviminit.lua" "+lua vim.cmd('qa')" >"$tmp_log" 2>&1; then
    cat "$tmp_log" >&2
    exit 1
  fi
fi

print "[syntax] checking _tmux.conf"
tmux_tmpdir=$(mktemp -d)
export TMUX_TMPDIR="$tmux_tmpdir"
socket="syntax-check-$$"
if ! TMUX_TMPDIR="$tmux_tmpdir" tmux -L "$socket" -f "$ROOT_DIR/_tmux.conf" new-session -d -s syntax_check "exit" >"$tmux_tmpdir/tmux.log" 2>&1; then
  if grep -qiE "operation not permitted|permission denied" "$tmux_tmpdir/tmux.log"; then
    print -u2 "[syntax] skipped tmux check: insufficient permissions to create tmux socket"
  else
    cat "$tmux_tmpdir/tmux.log" >&2
    exit 1
  fi
else
  tmux -L "$socket" kill-server >/dev/null 2>&1 || true
fi
