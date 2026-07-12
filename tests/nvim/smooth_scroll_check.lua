-- smooth_scroll (dotfiles.smooth_scroll) の headless 検証。test_smooth_scroll.sh から
-- dofile される。検証項目:
--   1. 単発 <C-u>: アニメ完了後にカーソルがちょうど &scroll 行上がる (native と同量)
--   2. 連打 (キーリピート相当): エラーなく素通しスクロールされ、進行中アニメの残
--      フレームが重複加算されない (2*scroll 〜 3*scroll の範囲に収まる)
--   3. アニメ中のウィンドウ切替: 残フレームは押下したウィンドウに適用され、切替先の
--      ウィンドウは動かない (押下時ウィンドウ捕捉の回帰テスト、2026-07-12 修正)
--   4. 押下直後のウィンドウクローズ: 捕捉したウィンドウが scheduled callback 実行前に
--      閉じられてもエラーにならず、残ったウィンドウも動かない (codex 指摘 P2 の回帰テスト)
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

-- 3. アニメ中にウィンドウを切り替える: 押下した win1 だけが &scroll 行スクロールし、
--    切替先 win2 は 1 行も動かない (修正前は残フレームがカレントウィンドウ = win2 に
--    適用され、win2 が部分スクロールしていた)
local win1 = vim.api.nvim_get_current_win()
vim.cmd("vsplit")
local win2 = vim.api.nvim_get_current_win() -- vsplit 直後は新ウィンドウがカレント
local buf2 = vim.api.nvim_create_buf(true, false)
vim.api.nvim_win_set_buf(win2, buf2)
vim.api.nvim_buf_set_lines(buf2, 0, -1, false, lines)
vim.api.nvim_win_set_cursor(win2, { 150, 0 })
vim.api.nvim_set_current_win(win1)
vim.api.nvim_win_set_cursor(win1, { 150, 0 })
-- 直前の連打からリピート判定 (REPEAT_MS) を確実に跨いでから押す
vim.wait(200, function() return false end)
press_ctrl_u()
vim.api.nvim_set_current_win(win2) -- アニメ完了を待たずフォーカス移動
wait_animation()
local win1_moved = 150 - vim.api.nvim_win_get_cursor(win1)[1]
local win2_moved = 150 - vim.api.nvim_win_get_cursor(win2)[1]
if win2_moved ~= 0 then
  fail(("window switch: inactive win2 scrolled %d lines (expected 0)"):format(win2_moved))
end
if win1_moved ~= scroll then
  fail(("window switch: pressed win1 moved %d lines (expected %d)"):format(win1_moved, scroll))
end

-- 4. 押下直後にウィンドウを閉じる: animate は schedule 実行時に win が invalid なら
--    何もせず打ち切る。エラー (Error executing vim.schedule lua callback) が出れば
--    test_smooth_scroll.sh 側の出力検査で落ちる
vim.wait(200, function() return false end) -- リピート判定 (REPEAT_MS) を跨ぐ
vim.api.nvim_set_current_win(win1)
local win2_before_close = vim.api.nvim_win_get_cursor(win2)[1]
press_ctrl_u()
vim.api.nvim_win_close(win1, true) -- scheduled callback 実行前に押下ウィンドウを閉じる
wait_animation()
local win2_after_close = vim.api.nvim_win_get_cursor(win2)[1]
if win2_after_close ~= win2_before_close then
  fail(("window close: remaining win2 scrolled %d lines (expected 0)"):format(win2_before_close - win2_after_close))
end

print(("OK (scroll=%d, single=%d, rapid=%d, winswitch=%d/%d)"):format(scroll, moved, moved3, win1_moved, win2_moved))
