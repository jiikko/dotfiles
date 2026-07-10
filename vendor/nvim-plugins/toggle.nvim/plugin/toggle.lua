------------------------------------------------------------------------------
-- Vim Toggle Plugin (Lua port) — plugin entry
-- Author: Timo Teifel / Forked: Luke Davis / Licence: GPL v2.0
------------------------------------------------------------------------------
-- [vendor 2026-07-09] 原 plugin/toggle.vim の Lua 移植。VENDOR.md 参照。
if vim.g.loaded_toggle ~= nil then return end
vim.g.loaded_toggle = 1

if vim.g.toggle_map == nil then
  vim.g.toggle_map = '<Leader>b' -- default mapping
end

-- 既定リスト (未設定時のみ)。原版 plugin/toggle.vim と同一。
local function az(a, b)
  local r = {}
  for c = string.byte(a), string.byte(b) do
    r[#r + 1] = string.char(c)
  end
  return r
end
local function concat(...)
  local r = {}
  for _, t in ipairs({ ... }) do
    for _, v in ipairs(t) do
      r[#r + 1] = v
    end
  end
  return r
end
if vim.g.toggle_chars_off == nil then
  vim.g.toggle_chars_off = concat(az('a', 'z'), { '-', '<', '&', '0' }, vim.g.toggle_consecutive_off or {})
end
if vim.g.toggle_chars_on == nil then
  vim.g.toggle_chars_on = concat(az('A', 'Z'), { '+', '>', '|', '1' }, vim.g.toggle_consecutive_on or {})
end
if vim.g.toggle_words_off == nil then
  vim.g.toggle_words_off = { 'false', 'off', 'no', 'undef', 'out', 'down', 'right', 'south', 'west' }
end
if vim.g.toggle_words_on == nil then
  vim.g.toggle_words_on = { 'true', 'on', 'yes', 'define', 'in', 'up', 'left', 'north', 'east' }
end

-- <Plug> マッピング (原版と同じキー列: normal=Cmd, visual=Esc で marks 確定後に visualmode())
vim.keymap.set('n', '<Plug>Toggle', [[<Cmd>lua require('toggle').toggle(1)<CR>]], { silent = true })
vim.keymap.set('x', '<Plug>Toggle', [[<Esc><Cmd>lua require('toggle').toggle(1, vim.fn.visualmode())<CR>]], { silent = true })

-- :Toggle は Vim の -range/<count> 展開をそのまま Lua へ渡す (原版 <line1>,<line2>call ...(0, <count>) と等価)
vim.cmd([[command! -range Toggle call luaeval("require('toggle').toggle(0, _A.r, 0, _A.a, _A.b)", {'r': <count>, 'a': <line1>, 'b': <line2>})]])

if vim.g.toggle_map ~= nil and vim.g.toggle_map ~= '' then
  vim.keymap.set('n', vim.g.toggle_map, '<Plug>Toggle', { remap = true, silent = true })
  vim.keymap.set('x', vim.g.toggle_map, '<Plug>Toggle', { remap = true, silent = true })
end
