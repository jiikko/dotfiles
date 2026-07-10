-- treesitter fold の「計算は expr、保持は manual」化 (FastFold 方式の最小 Lua 実装)。
--
-- なぜ: foldmethod=expr のままだと nvim はバッファを再表示するたびに全行の foldexpr を
-- 再評価する。コストの主因は treesitter でなく per-line 式評価機構 (v:lua ブリッジ +
-- 式評価) で、6000 行の lua ファイルでバッファ切替 1 回 ~3.4ms を実測 (tests/nvim/
-- bench_nvim.sh の buf_switch: 200 切替で expr=690ms / manual 凍結=17ms)。
-- manual fold は閉じ開き状態ごとバッファ切替を跨いで保持される (実測確認済み) ため、
-- expr で計算した直後に manual へ落とすと再表示コストがゼロになる。
--
-- 再計算するタイミング (= fold が stale になりうる編集後だけ):
--   - BufWinEnter: バッファ初表示、または前回計算後に編集があった場合
--   - InsertLeave / TextChanged: debounce (400ms) して再計算
-- 再計算の判定は changedtick 比較で行い、無編集の再表示では走らない。
local M = {}

local FOLDEXPR = "v:lua.vim.treesitter.foldexpr()"
local DEBOUNCE_MS = 400

-- バッファごとの「fold 計算済み changedtick」。nil = 未計算
local computed_tick = {}
local timers = {}

local function eligible(buf)
  return vim.bo[buf].buftype == "" and vim.api.nvim_buf_is_loaded(buf)
end

-- win に表示中のバッファの fold を expr で計算し、manual に凍結する。
-- foldmethod=expr を set した時点で nvim が全行を再計算し、manual へ戻すと
-- その結果が manual fold として保持される (:h fold-methods)。
local function refresh_win(win)
  if not vim.api.nvim_win_is_valid(win) then return end
  local buf = vim.api.nvim_win_get_buf(win)
  if not eligible(buf) then return end
  -- treesitter の foldexpr は非同期パース前提で、未パース時は 0 を返し「後から」
  -- foldupdate で本物に置き換える設計。manual へ凍結するとその後追い更新が効かないため、
  -- 凍結前にツリーを同期パースして foldexpr が初回から実レベルを返せるようにする。
  local ok, parser = pcall(vim.treesitter.get_parser, buf)
  if ok and parser then pcall(parser.parse, parser, true) end
  vim.api.nvim_win_call(win, function()
    vim.wo[win][0].foldexpr = FOLDEXPR
    vim.wo[win][0].foldmethod = "expr"
    -- foldmethod の set だけでは評価は次の再描画まで遅延される。zx で今すぐ全 fold を
    -- 計算させてから manual へ凍結する。副作用として再計算対象の fold の開閉状態は
    -- foldlevel 既定 (=100, 全て開く) に戻る (編集後の再計算時のみ。無編集の再表示では
    -- refresh 自体が走らないため zc した状態は保持される)。
    vim.cmd("silent! normal! zx")
    vim.wo[win][0].foldmethod = "manual"
  end)
  computed_tick[buf] = vim.api.nvim_buf_get_changedtick(buf)
end

local function refresh_buf(buf)
  for _, win in ipairs(vim.fn.win_findbuf(buf)) do
    refresh_win(win)
  end
end

local function schedule_refresh(buf)
  if not eligible(buf) then return end
  if timers[buf] then timers[buf]:stop() end
  timers[buf] = vim.defer_fn(function()
    timers[buf] = nil
    if vim.api.nvim_buf_is_loaded(buf)
      and computed_tick[buf] ~= vim.api.nvim_buf_get_changedtick(buf) then
      refresh_buf(buf)
    end
  end, DEBOUNCE_MS)
end

function M.setup()
  local group = vim.api.nvim_create_augroup("dotfiles_folds", { clear = true })

  vim.api.nvim_create_autocmd("BufWinEnter", {
    group = group,
    callback = function(args)
      if not eligible(args.buf) then return end
      -- 未計算 or 表示していない間に編集された場合のみ計算 (無編集の再表示はゼロコスト)
      if computed_tick[args.buf] ~= vim.api.nvim_buf_get_changedtick(args.buf) then
        refresh_win(vim.api.nvim_get_current_win())
      end
    end,
  })

  vim.api.nvim_create_autocmd({ "InsertLeave", "TextChanged" }, {
    group = group,
    callback = function(args)
      schedule_refresh(args.buf)
    end,
  })

  vim.api.nvim_create_autocmd("BufDelete", {
    group = group,
    callback = function(args)
      computed_tick[args.buf] = nil
      if timers[args.buf] then
        timers[args.buf]:stop()
        timers[args.buf] = nil
      end
    end,
  })
end

return M
