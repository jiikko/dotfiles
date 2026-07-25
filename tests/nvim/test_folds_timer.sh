#!/usr/bin/env zsh
# dotfiles.folds の debounce timer が libuv handle を漏らさないことの headless テスト。
# 検証内容は folds_timer_check.lua 参照 (TextChanged 連打で生存 timer が増えない)。

set -euo pipefail
unset CDPATH

NVIM_BIN=${NVIM:-nvim}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONFIG_FILE="$ROOT_DIR/_nviminit.lua"

if ! command -v "$NVIM_BIN" >/dev/null 2>&1; then
  print -u2 "Error: nvim binary not found. Install Neovim or set \$NVIM."
  exit 1
fi

print "[test-folds-timer] verifying debounce timers are closed, not just stopped"
# qa! 必須: check スクリプトがバッファを modified にするため、qa (! なし) では
# 未保存拒否で headless nvim が終了せずハングする (test_smooth_scroll.sh と同じ理由)
out=$("$NVIM_BIN" --headless -u "$CONFIG_FILE" \
  "+lua vim.wait(300)" \
  "+lua dofile('$SCRIPT_DIR/folds_timer_check.lua')" \
  "+lua vim.cmd('qa!')" 2>&1) || {
  print -u2 "$out"
  exit 1
}
if print -r -- "$out" | grep -qE "FAIL:|Error executing|stack traceback"; then
  print -u2 "$out"
  exit 1
fi
if ! print -r -- "$out" | grep -q "^OK"; then
  print -u2 "[test-folds-timer] expected OK marker, got:"
  print -u2 "$out"
  exit 1
fi
print "[test-folds-timer] $out"
