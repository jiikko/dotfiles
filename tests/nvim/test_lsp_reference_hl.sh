#!/usr/bin/env zsh
# LSP 参照ハイライト (documentHighlight) の視認性テスト。
# 検証内容は lsp_reference_hl_check.lua 参照 (group 名の pin / Visual 漏れ / コントラスト)。
#
# colorscheme は SUPPORT_TRUECOLOR で分岐する (_nviminit.lua 冒頭の WORKAROUND)。
# gruvbox は cterm 色を持たないため、片方だけ回すと 256色運用 (= 主環境) の検査が
# 素通りする。両方の分岐を明示的に回す。

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

print "[test-lsp-reference-hl] verifying documentHighlight contrast in both colorscheme branches"

export SUPPORT_TRUECOLOR=false
tt_nvim_run_check "test-lsp-reference-hl:256color" "lsp_reference_hl_check.lua"

export SUPPORT_TRUECOLOR=true
tt_nvim_run_check "test-lsp-reference-hl:truecolor" "lsp_reference_hl_check.lua"
