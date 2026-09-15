#!/usr/bin/env zsh
# gf が行番号付きパス (file:3) の行まで飛ぶことのテスト。
# 検証内容と守っている不変条件は gf_line_jump_check.lua のヘッダ参照。

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

# 🚨 rtp は cwd 依存 (check.lua のヘッダ参照)。repo root で走らせて、この checkout の
# nvim/lua が読まれるようにする
cd "$ROOT_DIR" || exit 1

print "[test-gf-line-jump] verifying gf jumps to the trailing line number"
tt_nvim_run_check "test-gf-line-jump" "gf_line_jump_check.lua"
