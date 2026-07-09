#!/usr/bin/env zsh

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

tmp_root=$(mktemp -d)
cleanup() {
  rm -rf "$tmp_root"
}
trap cleanup EXIT

run_check() {
  local ft="$1"
  local expected_rhs="$2"
  local lua_file="$tmp_root/check_${ft}.lua"
  local log_file="$tmp_root/check_${ft}.log"

  # 注意: +luafile のエラーは exit code に伝わらず後続の +qall が exit 0 にする (nvim の仕様)。
  # assert 相当を pcall で捕捉し、失敗時は cquit で明示的に非0終了させないと false-pass になる。
  cat >"$lua_file" <<EOF
local ok, err = pcall(function()
  local ft = "$ft"
  local expected_rhs = "$expected_rhs"
  vim.cmd("enew")
  vim.cmd("set ft=" .. ft)
  -- vim.keymap.get は存在しない API (旧テストはこれで常時 error し false-pass に隠れていた)。
  -- 実在する vim.fn.maparg で buffer-local マッピングの rhs を取得する。
  local rhs = vim.fn.maparg("<leader>bi", "n")
  if rhs == nil or rhs == "" then
    error(string.format("missing <leader>bi mapping for %s", ft))
  end
  rhs = rhs:gsub("\27", "<Esc>")
  if rhs ~= expected_rhs then
    error(string.format("unexpected rhs for %s: %q (expected %q)", ft, rhs, expected_rhs))
  end
end)
if not ok then
  vim.api.nvim_err_writeln(tostring(err))
  vim.cmd("cquit 1")
end
EOF

  if ! "$NVIM_BIN" --headless -u "$CONFIG_FILE" "+luafile $lua_file" +qall >"$log_file" 2>&1; then
    cat "$log_file" >&2
    exit 1
  fi
}

print "[test-nvim:zsh] ftplugin mappings"
run_check ruby 'obinding.pry<Esc>'
run_check eruby 'o<% binding.pry %><Esc>'
run_check javascript 'odebugger<Esc>'
run_check typescript 'odebugger<Esc>'
run_check javascriptreact 'odebugger<Esc>'
run_check typescriptreact 'odebugger<Esc>'
print "[test-nvim:zsh] ftplugin mappings ok"
