#!/usr/bin/env zsh
# telescope の prompt の insert マッピングのテスト。
# 検証内容と守っている不変条件は telescope_prompt_keymap_check.lua のヘッダ参照。

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

source "$SCRIPT_DIR/lib/check_log.sh"

# 🚨 rtp は cwd 依存 (check.lua のヘッダ参照)
cd "$ROOT_DIR" || exit 1

print "[test-telescope-prompt-keymap] verifying <C-u> is left to insert-mode default"
tt_nvim_run_check "test-telescope-prompt-keymap" "telescope_prompt_keymap_check.lua"
