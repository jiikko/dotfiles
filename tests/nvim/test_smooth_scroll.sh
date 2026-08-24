#!/usr/bin/env zsh
# dotfiles.smooth_scroll (<C-u>/<C-d> スムーズスクロール) の headless テスト。
# 検証内容は smooth_scroll_check.lua 参照 (単発=アニメで &scroll 行 / 連打=素通しで重複なし)。

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

print "[test-smooth-scroll] verifying single-press animation and key-repeat passthrough"

# headless nvim の実行と 3 つの検査 (異常終了 / FAIL・lua 例外 / 行頭の OK) は
# tests/nvim/lib/check_log.sh に一元化してある。個別に grep を手書きしないこと —
# 3 本へコピペしていた結果、うち 1 本が `grep -q "OK"` (アンカー無し) へ drift して
# いた (issue 081)。
source "$SCRIPT_DIR/lib/check_log.sh"
tt_nvim_run_check "test-smooth-scroll" "smooth_scroll_check.lua"
