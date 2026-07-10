-- nvim 操作レイテンシのマイクロベンチマーク本体 (tests/nvim/bench_nvim.sh から dofile される)。
-- 各計測は「metric=<name> ms=<value>」を stderr に 1 行ずつ出す (シェル側がパースする)。
--
-- 計測の設計:
--   - headless では描画そのものは走らないが、CursorMoved/WinScrolled/TextChangedI 等の
--     autocmd ハンドラと decoration provider (プラグインの実コスト) は実行される。
--     ここで測るのは「イベントハンドラ側のコスト」で、体感レイテンシの主因を代理する。
--   - 相対比較 (full vs clean / プラグイン A/B / CI での経時トレンド) 用。絶対値は環境依存。

local function measure(name, iterations, fn)
  -- ウォームアップ 1 回 (初回だけの遅延ロード・キャッシュを除外)
  fn()
  local t0 = vim.uv.hrtime()
  for _ = 1, iterations do
    fn()
  end
  local ms = (vim.uv.hrtime() - t0) / 1e6
  io.stderr:write(string.format("metric=%s ms=%.1f\n", name, ms))
end

-- ベンチ対象ファイル (シェル側が生成した lua ファイル) を開く。
-- buffer-load はウォームアップなしの 1 発を測りたいので measure() を使わない。
local target = vim.env.BENCH_FILE
assert(target and target ~= "", "BENCH_FILE not set")

local t0 = vim.uv.hrtime()
vim.cmd.edit(target)
io.stderr:write(string.format("metric=bufload ms=%.1f\n", (vim.uv.hrtime() - t0) / 1e6))

-- ファイル読み込み直後の遅延ロード (BufReadPre 系) と LSP 起動要求を落ち着かせる
vim.wait(300)

-- 1) スクロール: j 連打 (CursorMoved / WinScrolled ハンドラ + fold 計算が走る)
vim.fn.cursor(1, 1)
measure("scroll_j_x2000", 1, function()
  for _ = 1, 2000 do
    vim.cmd("keepjumps normal! j")
  end
  vim.fn.cursor(1, 1)
end)

-- 2) 半ページスクロール: C-d 相当 (WinScrolled が確実に発火する)
vim.fn.cursor(1, 1)
measure("scroll_ctrl_d_x200", 1, function()
  for _ = 1, 200 do
    vim.cmd([[execute "normal! \<C-d>"]])
  end
  vim.fn.cursor(1, 1)
end)

-- 3) 挿入タイピング: TextChangedI / CursorMovedI ハンドラ (補完・matchup 等) が走る
--    nvim_feedkeys の 'x' フラグで同期実行する
measure("insert_120chars_x10", 10, function()
  vim.cmd("normal! Go")
  local keys = vim.api.nvim_replace_termcodes(
    "i" .. string.rep("local x = 10 -- abc ", 6) .. "<Esc>", true, false, true)
  vim.api.nvim_feedkeys(keys, "x", false)
  vim.cmd("normal! dd")
end)

-- 4) ウィンドウ切替: WinEnter/WinLeave (vimade / cursorline / ZenkakuSpace match が走る)
vim.cmd("vsplit")
measure("win_switch_x200", 1, function()
  for _ = 1, 200 do
    vim.cmd("wincmd w")
  end
end)
vim.cmd("only")

-- 5) バッファ切替: BufEnter (checktime / bufferline / incline が反応する)
vim.cmd("edit " .. target .. ".alt.lua")
measure("buf_switch_x200", 1, function()
  for _ = 1, 200 do
    vim.cmd("buffer #")
  end
end)

io.stderr:write("bench done\n")
