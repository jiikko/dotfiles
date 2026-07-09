------------------------------------------------------------------------------
-- vim-ambiwidth (Lua port) — plugin entry
-- Author: Naruhiko Nishino (rbtnn/vim-ambiwidth) / Licence: MIT
------------------------------------------------------------------------------
-- [vendor 2026-07-09] 原 plugin/ambiwidth.vim の Lua 移植。VENDOR.md 参照。
if vim.g.loaded_ambiwidth ~= nil then
  return
end
vim.g.loaded_ambiwidth = 1

-- utf-8 かつ setcellwidths が使えるときだけ適用 (原版 plugin/ambiwidth.vim の条件を踏襲)。
-- lazy で eager ロードされるため起動時に一度だけ走る (原版の has('vim_starting') 相当は
-- g:loaded_ambiwidth ガードで担保)。
if vim.o.encoding == "utf-8" and vim.fn.exists("*setcellwidths") == 1 then
  require("ambiwidth").setup()
end
