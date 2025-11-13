#!/usr/bin/env bash

set -euo pipefail

NVIM_BIN=${NVIM:-nvim}
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v "$NVIM_BIN" >/dev/null 2>&1; then
  echo "Error: nvim binary not found. Install Neovim or set \$NVIM." >&2
  exit 1
fi

tmp_root="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_root"
}
trap cleanup EXIT

CONFIG_FILE="$ROOT_DIR/_nviminit.lua"

echo "[test-nvim] verifying config loads headlessly"
test_log="$tmp_root/nvim-test.log"
"$NVIM_BIN" --headless -u "$CONFIG_FILE" "+lua vim.cmd('qa')" >"$test_log" 2>&1 || {
  cat "$test_log" >&2
  exit 1
}

lazy_check="$tmp_root/lazy_check.lua"
cat <<'EOF' > "$lazy_check"
local ok, lazy = pcall(require, 'lazy')
if not ok then
  error('lazy.nvim not available')
end
local stats = lazy.stats()
if not stats or stats.count == 0 then
  error('lazy.nvim returned empty stats')
end
EOF

echo "[test-nvim] checking lazy.nvim availability"
lazy_log="$tmp_root/nvim-lazy.log"
"$NVIM_BIN" --headless -u "$CONFIG_FILE" "+lua dofile([[$lazy_check]])" +qall >"$lazy_log" 2>&1 || {
  cat "$lazy_log" >&2
  exit 1
}

echo "[test-nvim] done"
