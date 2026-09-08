#!/usr/bin/env zsh
# LSP の索引進捗をステータスラインへ出す配線のテスト。
# 検証内容と守っている不変条件は lsp_progress_check.lua のヘッダ参照。

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

# check 側は「今の repo の _nviminit.lua」を読む (worktree でもチェックアウト先を見るため)
export DOTFILES_INIT="$CONFIG_FILE"

print "[test-lsp-progress] verifying LSP progress -> statusline wiring"
tt_nvim_run_check "test-lsp-progress" "lsp_progress_check.lua"
