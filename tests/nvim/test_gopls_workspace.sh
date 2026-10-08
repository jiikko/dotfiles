#!/usr/bin/env zsh
# gopls の dotfiles workspace 判定・root callback・client ごとの初期化を固定する。
set -euo pipefail
unset CDPATH

NVIM_BIN=${NVIM:-nvim}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

if ! command -v "$NVIM_BIN" >/dev/null 2>&1; then
  print -u2 "Error: nvim binary not found. Install Neovim or set \$NVIM."
  exit 1
fi

print "[test-gopls-workspace] verifying pure decisions and per-client wiring"
out=$("$NVIM_BIN" --headless -u NONE -i NONE -n \
  --cmd "set rtp^=$ROOT_DIR/nvim" \
  "+lua dofile('$SCRIPT_DIR/gopls_workspace_check.lua')" \
  "+qa!" 2>&1) || {
  print -u2 -- "$out"
  exit 1
}
if grep -qE 'FAIL:|Error executing|stack traceback' <<< "$out"; then
  print -u2 -- "$out"
  exit 1
fi
if ! grep -q '^OK' <<< "$out"; then
  print -u2 -- "[$0] expected OK marker, got:"
  print -u2 -- "$out"
  exit 1
fi
print "[test-gopls-workspace] $out"
