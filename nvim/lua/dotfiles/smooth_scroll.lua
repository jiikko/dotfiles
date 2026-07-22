-- <C-u>/<C-d> のスムーズスクロール (neoscroll.nvim の置換)。
--
-- 置換理由の詳細 (A/B 検証・既製アニメ系プラグインの限界): docs/nvim-plugins.md の karb94/neoscroll.nvim 行を参照。
--
-- 挙動:
--   単発押下   → ease-out のアニメで半ページスクロール (現在位置を見失わない)
--   押しっぱなし → アニメを諦めて素の <C-u>/<C-d> に素通し (乱れゼロ)
--   判定: 前回押下からの間隔 < REPEAT_MS、またはアニメ進行中の再押下
--
-- 制約 (意図的な非対応):
--   count (5<C-u> 等) はアニメせず素通し扱いにする。本モジュールは n/x のみ触る。
local M = {}

local REPEAT_MS = 150 -- これ未満の押下間隔はキーリピートとみなす
local FRAME_MS = 15
-- ease-out の各フレームの移動割合 (合計 1.0、残りは最終フレームで清算)
local FRAMES = { 0.30, 0.24, 0.18, 0.13, 0.09, 0.06 }

local CTRL_D = "\4"  -- <C-d>
local CTRL_U = "\21" -- <C-u>

local last_press = 0
local generation = 0 -- 押下ごとに進める世代。古いアニメフレームは自分の世代と比べて自殺する
local animating = false

-- n 行だけ view とカーソルを一緒にスクロールする。'scroll' を一時的に n へ倒して
-- 素の <C-u>/<C-d> を使う (境界処理・fold・カーソル位置の規律を native に任せるため)。
local function scroll_step(key, n)
  if n < 1 then return end
  local saved = vim.wo.scroll
  vim.wo.scroll = n
  pcall(vim.cmd, "normal! " .. key)
  vim.wo.scroll = saved
end

local function animate(key, win)
  generation = generation + 1
  local gen = generation
  -- 押下直後 (schedule 実行前) にウィンドウが閉じられることがある。vim.wo[win] は
  -- invalid id で例外になるため、frame 内のガードとは別にここでも先に検証する
  if not vim.api.nvim_win_is_valid(win) then return end
  animating = true
  local total = vim.wo[win].scroll -- 半ページ (native <C-u>/<C-d> と同じ量)
  local moved = 0
  local i = 1
  local function frame()
    if gen ~= generation then return end -- 新しい押下に追い越された
    -- フレームは押下時のウィンドウ (win) へ nvim_win_call で適用する。defer_fn の実行時点
    -- ではフォーカスが別ウィンドウへ移っていることがあり、暗黙のカレントウィンドウ参照だと
    -- 押下と無関係なウィンドウがスクロールされる (headless 再現済み)。閉じられていたら打ち切る。
    if not vim.api.nvim_win_is_valid(win) then
      animating = false
      return
    end
    local step
    if i <= #FRAMES then
      step = math.min(math.max(1, math.floor(total * FRAMES[i] + 0.5)), total - moved)
    else
      step = total - moved -- 丸め誤差の清算
    end
    -- バッファ端の検出は view とカーソルの両方を見る (先頭付近では view (w0=1) が
    -- 動かなくてもカーソルは 1 行目まで動き続けるのが native <C-u> の挙動のため、
    -- view だけで判定すると途中で止まってしまう)
    local stalled
    vim.api.nvim_win_call(win, function()
      local view_before = vim.fn.line("w0")
      local cursor_before = vim.api.nvim_win_get_cursor(0)[1]
      scroll_step(key, step)
      stalled = vim.fn.line("w0") == view_before
        and vim.api.nvim_win_get_cursor(0)[1] == cursor_before
    end)
    if stalled then
      animating = false -- 完全に動かなくなった = バッファ端: 残りフレームは空撃ちなので打ち切る
      return
    end
    moved = moved + step
    i = i + 1
    if moved >= total then
      animating = false
      return
    end
    vim.defer_fn(frame, FRAME_MS)
  end
  frame()
end

local function handler(key)
  return function()
    local now = vim.uv.hrtime() / 1e6
    local held = (now - last_press) < REPEAT_MS or animating
    last_press = now
    if held or vim.v.count > 0 then
      -- リピート中 (またはアニメ追い越し・count 付き): 素通し。進行中のアニメは
      -- 世代を進めて打ち切る (残フレームが native スクロールに重ならないように)
      generation = generation + 1
      animating = false
      return key
    end
    local win = vim.api.nvim_get_current_win() -- 押下時のウィンドウを捕捉 (animate 冒頭コメント参照)
    vim.schedule(function() animate(key, win) end) -- expr 評価中は textlock のため遅延実行
    return ""
  end
end

function M.setup()
  local opts = { expr = true, silent = true, desc = "Smooth scroll (hold = instant)" }
  vim.keymap.set({ "n", "x" }, "<C-u>", handler(CTRL_U), opts)
  vim.keymap.set({ "n", "x" }, "<C-d>", handler(CTRL_D), opts)
end

return M
