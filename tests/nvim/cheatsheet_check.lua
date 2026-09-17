-- dotfiles.cheatsheet (右下の常駐チートシート) の headless 検証。
-- test_cheatsheet.sh から dofile される。検証項目:
--   1. desc 付きの buffer-local マッピングが float に出る
--   2. which-key が prefix 乗っ取り用に張るトリガー (desc に "which-key-trigger" を含む)
--      は出ない。これは人間向けの説明ではなく which-key の内部マーカーで、which-key 自身も
--      同じ文字列で自己識別している (which-key/triggers.lua:32 / buf.lua:125)
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local cheatsheet = require("dotfiles.cheatsheet")

-- should_hide の下限 (MIN_MAIN_WIDTH=120 / MIN_MAIN_HEIGHT=24) を満たす画面にする
vim.o.columns = 200
vim.o.lines = 40

local buf = vim.api.nvim_get_current_buf()
vim.api.nvim_buf_set_lines(buf, 0, -1, false, { "sample" })

-- 実物と同じ形で張る: 人間向け 1 本と、which-key のトリガー 1 本
vim.keymap.set("n", "gd", function() end, { buffer = buf, desc = "Goto definition" })
vim.keymap.set("n", "[", function() end, { buffer = buf, desc = "which-key-trigger" })

cheatsheet.refresh()
vim.wait(200, function() return cheatsheet._state().win ~= nil end)

local state = cheatsheet._state()
if not (state.win and vim.api.nvim_win_is_valid(state.win)) then
  fail("cheatsheet float が開いていない")
end

local text = table.concat(vim.api.nvim_buf_get_lines(state.buf, 0, -1, false), "\n")

if not text:find("Goto definition", 1, true) then
  fail("desc 付きマッピングが出ていない:\n" .. text)
end
if text:find("which-key-trigger", 1, true) then
  fail("which-key の内部トリガーが出ている (除外されていない):\n" .. text)
end

print("OK cheatsheet: desc 付きを表示し、which-key トリガーを除外")
