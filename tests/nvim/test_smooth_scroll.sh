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
# qa! 必須: check スクリプトが無名バッファへ書き込み modified になるため、qa (! なし) だと
# 未保存拒否で headless nvim が終了せず永久にハングする (2026-07-12 実測)
out=$("$NVIM_BIN" --headless -u "$CONFIG_FILE" \
  "+lua vim.wait(300)" \
  "+lua dofile('$SCRIPT_DIR/smooth_scroll_check.lua')" \
  "+lua vim.cmd('qa!')" 2>&1) || {
  print -u2 "$out"
  exit 1
}
# FAIL: は check スクリプトの assert 失敗。Error executing / stack traceback は
# scheduled callback 内の lua 例外 (assert を通り抜けて OK が出てしまうため個別に検査する)
if print -r -- "$out" | grep -qE "FAIL:|Error executing|stack traceback"; then
  print -u2 "$out"
  exit 1
fi
if ! print -r -- "$out" | grep -q "^OK"; then
  print -u2 "[test-smooth-scroll] expected OK marker, got:"
  print -u2 "$out"
  exit 1
fi
print "[test-smooth-scroll] $out"
