-- smooth_scroll (dotfiles.smooth_scroll) の headless 検証。test_smooth_scroll.sh から
-- dofile される。検証項目:
--   1. 単発 <C-u>: アニメ完了後にカーソルがちょうど &scroll 行上がる (native と同量)
--   2. 連打 (キーリピート相当): エラーなく素通しスクロールされ、進行中アニメの残
--      フレームが重複加算されない (2*scroll 〜 3*scroll の範囲に収まる)
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local function press_ctrl_u()
  local key = vim.api.nvim_replace_termcodes("<C-u>", true, false, true)
  vim.api.nvim_feedkeys(key, "mx", false) -- m: マッピング適用 / x: typeahead を即実行
end

local function wait_animation()
  vim.wait(500, function() return false end) -- defer_fn のフレームを pump し切る
end

-- 200 行のバッファを用意して下方にカーソルを置く
local lines = {}
for i = 1, 200 do lines[i] = ("line %d"):format(i) end
vim.api.nvim_buf_set_lines(0, 0, -1, false, lines)
vim.api.nvim_win_set_cursor(0, { 150, 0 })
local scroll = vim.wo.scroll

-- 1. 単発押下: scroll 行ちょうど上がる
press_ctrl_u()
wait_animation()
local moved = 150 - vim.api.nvim_win_get_cursor(0)[1]
if moved ~= scroll then
  fail(("single press: expected %d lines, moved %d"):format(scroll, moved))
end

-- 2. 3 連打 (間隔ほぼ 0ms = リピート判定): 1 打目のアニメは打ち切られ、
--    2-3 打目は素通し。合計は 2*scroll 以上 (素通し分) 3*scroll 以下 (重複加算なし)
local before = vim.api.nvim_win_get_cursor(0)[1]
for _ = 1, 3 do press_ctrl_u() end
wait_animation()
local moved3 = before - vim.api.nvim_win_get_cursor(0)[1]
if moved3 < 2 * scroll or moved3 > 3 * scroll then
  fail(("rapid presses: moved %d, expected within [%d, %d]"):format(moved3, 2 * scroll, 3 * scroll))
end

print(("OK (scroll=%d, single=%d, rapid=%d)"):format(scroll, moved, moved3))
