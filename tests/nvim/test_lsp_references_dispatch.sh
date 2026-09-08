#!/usr/bin/env zsh
# <C-k> (参照一覧) の振り分けのテスト。
# 検証内容と守っている不変条件は lsp_references_dispatch_check.lua のヘッダ参照。

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

# check 側は「今の repo の lsp.lua」を読む (worktree でもチェックアウト先を見るため)
export DOTFILES_LSP_LUA="$ROOT_DIR/nvim/lua/dotfiles/lsp.lua"

print "[test-lsp-references-dispatch] verifying <C-k> ruby method -> ripgrep dispatch"
tt_nvim_run_check "test-lsp-references-dispatch" "lsp_references_dispatch_check.lua"
