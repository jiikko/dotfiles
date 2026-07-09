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

# vim-go 廃止 (2026-07) の parity 回帰ガード: nvim/ftplugin/go.lua が vim-go 相当の
# buffer-local マッピングを張れていること (]] [[ 関数ジャンプ / af if テキストオブジェクト /
# <leader>gd GoDecls 置換)。これらは nvim-treesitter-textobjects に依存するため、プラグイン
# 除去や go.lua 削除・keymap 改名で無言に失われるのをこのテストで検知する。
run_check_go() {
  local lua_file="$tmp_root/check_go.lua"
  local log_file="$tmp_root/check_go.log"
  cat >"$lua_file" <<'EOF'
local ok, err = pcall(function()
  vim.cmd("enew")
  vim.cmd("set ft=go")
  -- buffer-local で張られているか (builtin ]] は buffer-local でないため、
  -- 我々の go.lua が textobjects 経由で張れていれば buffer==1 になる)。
  local function assert_buflocal(lhs, mode)
    local m = vim.fn.maparg(lhs, mode, false, true)
    if type(m) ~= "table" or not next(m) then
      error(string.format("missing %s mapping (%s) in go buffer", lhs, mode))
    end
    if m.buffer ~= 1 then
      error(string.format("%s (%s) is not buffer-local (buffer=%s)", lhs, mode, tostring(m.buffer)))
    end
  end
  assert_buflocal("]]", "n")      -- 次の関数へ
  assert_buflocal("[[", "n")      -- 前の関数へ
  assert_buflocal("af", "o")      -- a function (operator-pending)
  assert_buflocal("if", "x")      -- inner function (visual)
  assert_buflocal("ac", "o")      -- a comment
  assert_buflocal("<leader>gd", "n") -- GoDecls 置換 (document symbols)
  assert_buflocal("<leader>gD", "n") -- GoDeclsDir 置換 (workspace symbols)
  -- textobjects モジュールが実際に解決すること (プラグイン除去の検知)
  local ok_move = pcall(require, "nvim-treesitter.textobjects.move")
  local ok_sel = pcall(require, "nvim-treesitter.textobjects.select")
  if not (ok_move and ok_sel) then
    error("nvim-treesitter-textobjects modules failed to load (move=" ..
      tostring(ok_move) .. " select=" .. tostring(ok_sel) .. ")")
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

print "[test-nvim:zsh] go ftplugin (vim-go 廃止 parity)"
run_check_go
print "[test-nvim:zsh] go ftplugin ok"
