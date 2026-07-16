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

# false-pass 防御 (pcall+cquit guard / log backstop) は lib に集約 (rationale は lib 側コメント参照)。
source "$SCRIPT_DIR/lib/check_log.sh"
GUARD_LIB="$SCRIPT_DIR/lib/guard.lua"

# 共通: headless で lua を実行し、lua が cquit で落ちたら log を出して非0終了する。
# 終了は +qall! (強制): behavior 検査で buffer を modified にしても E37 (No write since last
# change) で hang しないため。lua 側の cquit は qall! より先に走るので失敗検知は保たれる。
_run_headless_lua() {
  local lua_file="$1"
  local log_file="$2"
  if ! "$NVIM_BIN" --headless -u "$CONFIG_FILE" "+luafile $lua_file" +qall! >"$log_file" 2>&1; then
    cat "$log_file" >&2
    exit 1
  fi
  # cquit をすり抜けて stderr にだけ出るエラー (ftplugin 評価中の E 系等) の backstop。
  # 従来この検査が無く false-pass の残存経路だった (test_nvim/ambiwidth と同じ防御に揃える)。
  tt_nvim_log_backstop "$log_file" "${lua_file:t}"
}

run_check() {
  local ft="$1"
  local expected_rhs="$2"
  local lua_file="$tmp_root/check_${ft}.lua"
  local log_file="$tmp_root/check_${ft}.log"

  # +luafile のエラー握り潰し (exit 0 化) 対策は lib/guard.lua の pcall+cquit に集約。
  cat >"$lua_file" <<EOF
local guard = dofile([[$GUARD_LIB]])
guard("ftplugin check ($ft)", function()
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
EOF

  _run_headless_lua "$lua_file" "$log_file"
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
# buffer-local マッピングを張れていること (]] [[ 関数ジャンプ / af if ac ic テキストオブジェクト /
# <leader>gd GoDecls 置換)。これらは nvim-treesitter-textobjects に依存するため、プラグイン
# 除去や go.lua 削除・keymap 改名で無言に失われるのをこのテストで検知する。
# さらに ]] の capture (@function.outer) が実際に解決してカーソルが動くかまで検証する
# (mapping の存在だけだと capture 文字列 typo を見逃すため)。実 Go ファイルを edit して検証する
# (no-name バッファ + set_lines では headless で parser が motion に効かないため)。
run_check_go() {
  local lua_file="$tmp_root/check_go.lua"
  local log_file="$tmp_root/check_go.log"
  local go_file="$tmp_root/behave.go"

  cat >"$go_file" <<'GOEOF'
package main

func alpha() {
	println("a")
	println("aa")
}

func bravo() { println("b") }
GOEOF

  # go_file/guard のパスを lua に注入 (interpolated 2 行) してから本体 (quoted heredoc) を追記する。
  print -r -- "local GO_FILE = [[$go_file]]" >"$lua_file"
  print -r -- "local guard = dofile([[$GUARD_LIB]])" >>"$lua_file"
  cat >>"$lua_file" <<'EOF'
guard("go ftplugin check", function()
  vim.cmd("edit " .. vim.fn.fnameescape(GO_FILE))
  -- treesitter parser (gopls 非依存のローカル解析) が有効化されるまで待つ。
  vim.wait(3000, function()
    return vim.treesitter.highlighter.active[vim.api.nvim_get_current_buf()] ~= nil
  end, 50)

  -- (1) textobjects モジュールが実際に解決すること (プラグイン除去や環境要因の検知)。
  --     mapping 検査 (2) より先にここで検査してエラー本文を出す: go.lua は require 失敗を
  --     pcall で握りつぶして黙って mapping をスキップするため、mapping assert から先に
  --     落ちると「なぜ張られなかったか」が CI ログに一切残らない (2026-07-10 の CI 赤で実感)。
  local ok_move, move_err = pcall(require, "nvim-treesitter.textobjects.move")
  local ok_sel, sel_err = pcall(require, "nvim-treesitter.textobjects.select")
  if not (ok_move and ok_sel) then
    error(string.format(
      "nvim-treesitter-textobjects modules failed to load:\n  move: %s\n  select: %s",
      ok_move and "ok" or tostring(move_err), ok_sel and "ok" or tostring(sel_err)))
  end

  -- (2) buffer-local マッピングが張られているか (builtin ]] は buffer-local でないため、
  --     go.lua が textobjects 経由で張れていれば buffer==1 になる)。
  local function assert_buflocal(lhs, mode)
    local m = vim.fn.maparg(lhs, mode, false, true)
    if type(m) ~= "table" or not next(m) then
      error(string.format("missing %s mapping (%s) in go buffer", lhs, mode))
    end
    if m.buffer ~= 1 then
      error(string.format("%s (%s) is not buffer-local (buffer=%s)", lhs, mode, tostring(m.buffer)))
    end
  end
  assert_buflocal("]]", "n")         -- 次の関数へ (n/x のみ。o は組み込みに委譲)
  assert_buflocal("[[", "n")         -- 前の関数へ
  assert_buflocal("af", "o")         -- a function (operator-pending)
  assert_buflocal("if", "x")         -- inner function (visual)
  assert_buflocal("ac", "o")         -- a comment
  assert_buflocal("ic", "o")         -- inner comment
  assert_buflocal("<leader>gd", "n") -- GoDecls 置換 (document symbols)
  assert_buflocal("<leader>gD", "n") -- GoDeclsDir 置換 (workspace symbols)

  -- (3) capture が実解決し ]] が実際にカーソルを次関数へ動かすか (非破壊)。
  --     parser 未 install の環境 (auto_install=false の fresh 環境) では flaky を避けて skip。
  local buf = vim.api.nvim_get_current_buf()
  if vim.treesitter.highlighter.active[buf] ~= nil then
    vim.api.nvim_win_set_cursor(0, { 1, 0 })
    vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes("]]", true, false, true), "x", false)
    local row = vim.api.nvim_win_get_cursor(0)[1]
    if row == 1 then
      error("]] did not move cursor from line 1 (@function.outer capture unresolved?)")
    end
    local line = vim.api.nvim_buf_get_lines(0, row - 1, row, false)[1] or ""
    if not line:find("func") then
      error(string.format("]] landed on a non-func line %d: %q", row, line))
    end
  else
    print("[skip] go treesitter parser not active; ]] behavior assert skipped")
  end
end)
EOF

  _run_headless_lua "$lua_file" "$log_file"
}

print "[test-nvim:zsh] go ftplugin (vim-go 廃止 parity)"
run_check_go
print "[test-nvim:zsh] go ftplugin ok"
