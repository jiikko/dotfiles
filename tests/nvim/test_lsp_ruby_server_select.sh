#!/usr/bin/env zsh
# Ruby の LSP サーバ選択 (ruby_lsp / solargraph の排他) のテスト。
# 検証内容と守っている不変条件は lsp_ruby_server_select_check.lua のヘッダ参照。
#
# LSP サーバのバイナリも fixture repo も要らない (vim.fs.root を差し替えて判定だけを見る)ので、
# CI でも実機と同じ検査が走る。

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

print "[test-lsp-ruby-server-select] verifying ruby_lsp / solargraph exclusivity"
tt_nvim_run_check "test-lsp-ruby-server-select" "lsp_ruby_server_select_check.lua"
