-- カスタム highlight の一元管理 (規律の一次情報はこのファイル)。
--
-- set() を通すことで 2 つの規律を一括担保する (issues/nvim-config-bugs-2026-07-10 #3/#12):
--
--  1. ColorScheme 再適用: :colorscheme は highlight を全クリアしてから再構築するため、
--     setup 時に 1 回だけ nvim_set_hl したカスタム色はテーマ切替で既定に戻る。
--     set() は registry に登録し、ColorScheme autocmd で毎回再適用する。
--
--  2. cterm 併記: 主環境は ~/.zshenv の SUPPORT_TRUECOLOR=false → termguicolors=off の
--     256色運用 (_nviminit.lua 冒頭の WORKAROUND 参照)。termguicolors=off では gui 色
--     (fg/bg) は無視されるため、ctermfg/ctermbg を併記しないと「truecolor 端末でだけ
--     効く」無言の欠落になる。set() は gui 色のみの定義を WARN で検知する。
--
-- プラグイン設定 API 経由で色を渡す場所 (bufferline の highlights テーブル、incline の
-- render 戻り値など) はここを通せないため、各所で cterm を手で併記する (該当箇所に
-- 本ファイルへの参照コメントを置く)。
local M = {}

local registry = {}

local group = vim.api.nvim_create_augroup("dotfiles_custom_highlights", { clear = true })
vim.api.nvim_create_autocmd("ColorScheme", {
  group = group,
  callback = function()
    for name, attrs in pairs(registry) do
      vim.api.nvim_set_hl(0, name, attrs)
    end
  end,
})

--- カスタム highlight を登録する: 即時適用し、以降の ColorScheme でも再適用する。
--- attrs は nvim_set_hl() の val と同じ。
function M.set(name, attrs)
  if (attrs.fg or attrs.bg) and not (attrs.ctermfg or attrs.ctermbg) then
    vim.notify(
      ("dotfiles.hl: %s は gui 色のみで cterm 併記が無い (256色環境で無効になる)"):format(name),
      vim.log.levels.WARN
    )
  end
  registry[name] = attrs
  vim.api.nvim_set_hl(0, name, attrs)
end

return M
