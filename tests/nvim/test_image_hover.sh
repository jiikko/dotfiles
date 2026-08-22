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
# qa! 必須: check スクリプトが無名バッファへ書き込み modified になる (test_smooth_scroll.sh と同じ罠)
out=$("$NVIM_BIN" --headless -u "$CONFIG_FILE" \
  "+lua vim.wait(300)" \
  "+lua dofile('$SCRIPT_DIR/image_hover_check.lua')" \
  "+lua vim.cmd('qa!')" 2>&1) || {
  print -u2 "$out"
  exit 1
}
if grep -qE "FAIL:|Error executing|stack traceback" <<< "$out"; then
  print -u2 "$out"
  exit 1
fi
if ! grep -q "OK" <<< "$out"; then
  print -u2 "[test-image-hover] expected OK marker, got:"
  print -u2 "$out"
  exit 1
fi
print "[test-image-hover] $out"
