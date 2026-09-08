#!/usr/bin/env zsh
# 参照検索の使用実績記録のテスト。
# 検証内容と守っている不変条件は refs_usage_check.lua のヘッダ参照。

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

export DOTFILES_LSP_LUA="$ROOT_DIR/nvim/lua/dotfiles/lsp.lua"

print "[test-refs-usage] verifying <C-k> usage recording (issue 334 stage 1)"
tt_nvim_run_check "test-refs-usage" "refs_usage_check.lua"
