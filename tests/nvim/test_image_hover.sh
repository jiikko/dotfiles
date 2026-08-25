#!/usr/bin/env zsh
# dotfiles.image_hover (画像 URL ホバー → Quick Look プレビュー) の headless テスト。
# 検証内容は image_hover_check.lua 参照 (URL 抽出 / 1 window 契約 / 同一 URL no-op)。
# 実プロセス (curl / qlmanage) は stub し、macOS 以外の CI でも走る。

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

print "[test-image-hover] verifying url extraction and single-window quick look contract"

# headless nvim の実行と 3 つの検査 (異常終了 / FAIL・lua 例外 / 行頭の OK) は
# tests/nvim/lib/check_log.sh に一元化してある。個別に grep を手書きしないこと —
# 3 本へコピペしていた結果、うち 1 本が `grep -q "OK"` (アンカー無し) へ drift して
# いた (issue 081)。
source "$SCRIPT_DIR/lib/check_log.sh"
tt_nvim_run_check "test-image-hover" "image_hover_check.lua"
