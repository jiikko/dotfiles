-- dotfiles.cheatsheet (右下の常駐チートシート) の headless 検証。
-- test_cheatsheet.sh から dofile される。検証項目:
--   1. desc 付きの buffer-local マッピングが float に出る
--   2. which-key が prefix 乗っ取り用に張るトリガー (desc に "which-key-trigger" を含む)
--      は出ない。これは人間向けの説明ではなく which-key の内部マーカーで、which-key 自身も
--      同じ文字列で自己識別している (which-key/triggers.lua:32 / buf.lua:125)
--   3. 実設定の global_prefixes (_nviminit.lua) で telescope の global キー (<leader>ff 等) が
--      出て、言語キーより先頭に並ぶ。prefix に当たらない global (<leader>k 等) は出ない
--   4. buffer-local が同じ lhs を張ったら global 側は出さない (効くのは buffer-local)。
--      buffer-local の <leader> 始まりは素のキー (gd) より後ろに並ぶ
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

local lines = vim.api.nvim_buf_get_lines(state.buf, 0, -1, false)
local text = table.concat(lines, "\n")

-- prefix に当たらない global (<leader>k / gx 等) が混ざると、ここに無い行が出る。
-- 混ざると MAX_LINES で gd が押し出されて下の検査も落ちるので、こちらを先に見る
for _, l in ipairs(lines) do
  if not (l:match("^ <leader>[fa]") or l:match("^ gd ")) then
    fail("global_prefixes に当たらない行が出ている (" .. l .. "):\n" .. text)
  end
end
for _, desc in ipairs({ "Find files", "Live grep", "List buffers", "Diagnostics (all)" }) do
  if not text:find(desc, 1, true) then
    fail("telescope の global キーが出ていない (" .. desc .. "):\n" .. text)
  end
end
if not (lines[1] or ""):find("^ <leader>") then
  fail("先頭行が global の束 (<leader>f…) ではない:\n" .. text)
end

if not text:find("Goto definition", 1, true) then
  fail("desc 付きマッピングが出ていない:\n" .. text)
end
if text:find("which-key-trigger", 1, true) then
  fail("which-key の内部トリガーが出ている (除外されていない):\n" .. text)
end

-- buffer-local で <leader>fg を上書きすると、global の Live grep は消えて buffer-local 側が出る
vim.keymap.set("n", "<leader>fg", function() end, { buffer = buf, desc = "Local grep override" })
cheatsheet.refresh()
text = table.concat(vim.api.nvim_buf_get_lines(state.buf, 0, -1, false), "\n")
if text:find("Live grep", 1, true) or not text:find("Local grep override", 1, true) then
  fail("buffer-local が同じ lhs を張っても global 側が出ている:\n" .. text)
end
-- buffer-local の <leader> 始まりは、素のキー (gd) の後ろにまとめる
if text:find("Local grep override", 1, true) < text:find("Goto definition", 1, true) then
  fail("buffer-local の <leader> キーが素のキーより前に並んでいる:\n" .. text)
end

print("OK cheatsheet: desc 付きを表示し、which-key トリガーを除外し、telescope の global キーを先頭に出す")
