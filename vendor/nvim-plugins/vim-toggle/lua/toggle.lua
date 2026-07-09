------------------------------------------------------------------------------
-- Vim Toggle Plugin (Lua port)
-- Author: Timo Teifel (timo at teifel-net dot de)
-- Forked: Luke Davis (lukelbd at gmail dot com)
-- Licence: GPL v2.0
------------------------------------------------------------------------------
-- [vendor 2026-07-09] 上流 Vimscript (autoload/toggle.vim) の Lua 移植。
-- 文字列/正規表現/バッファ操作は Vim builtin (vim.fn.*) をそのまま使い、原版と挙動を一致させる
-- (原 Vimscript との A/B テストで出力一致を確認済み)。VENDOR.md 参照。
local fn = vim.fn
local M = {}

-- Vim の empty() 相当 (0 / '' / 空リスト / nil を真)
local function empty(x)
  if x == nil then return true end
  local t = type(x)
  if t == 'number' then return x == 0 end
  if t == 'string' then return x == '' end
  if t == 'table' then return next(x) == nil end
  if t == 'boolean' then return not x end
  return false
end

-- Vim の真偽 (0/'' 以外を真)
local function truthy(x)
  return not empty(x)
end

-- g: のトグルリストを文字列化し、on/off の長さを揃える (長い方を切り詰め)。
-- 原版 s:toggle_validate 相当。呼び出しは M.toggle で invocation ごと 1 回。
local function toggle_validate()
  for _, name in ipairs({ 'toggle_chars', 'toggle_words' }) do
    local name0, name1 = name .. '_off', name .. '_on'
    local opts0 = vim.g[name0] or {}
    local opts1 = vim.g[name1] or {}
    local function stringify(t)
      local r = {}
      for i = 1, #t do
        local v = t[i]
        r[i] = type(v) == 'string' and v or fn.string(v)
      end
      return r
    end
    opts0, opts1 = stringify(opts0), stringify(opts1)
    if #opts0 > #opts1 then
      name0, name1, opts0, opts1 = name1, name0, opts1, opts0
    end
    if #opts1 > #opts0 then
      local delta = #opts1 - #opts0
      vim.api.nvim_echo(
        { { 'Warning: Truncating ' .. name1 .. ' (has ' .. delta .. ' more entries than ' .. name0 .. ')', 'WarningMsg' } },
        true, {}
      )
      local trunc = {}
      for i = 1, #opts0 do trunc[i] = opts1[i] end
      opts1 = trunc
    end
    vim.g[name0] = opts0
    vim.g[name1] = opts1
  end
end

-- 原版 s:toggle_cursor(expand, strict)。戻り値: 0=成功 / 1=非空白で失敗 / -1=空白で失敗
local function toggle_cursor(expand_flag, strict)
  local line = fn.getline('.')
  local lnum, cnum = fn.line('.'), fn.col('.')

  -- (1) カーソル下の整数/小数の符号トグル (0/1 の羅列や非小数はスキップ)
  if empty(strict) then
    local idx0, idx1 = 0, 0
    local regex = [[\([+-]\s*\)\?\(\<[0-9_]\+\(\.[0-9_]*\)\?\|\.[0-9_]\+\>\)]]
    while idx0 ~= -1 do
      local m = fn.matchstrpos(line, regex, idx1)
      local float_str = m[1]
      idx0, idx1 = m[2], m[3]
      if not (cnum < idx0 + 1 or cnum > idx1) then
        if not (empty(float_str) or fn.match(float_str, [[\C^[01]\+$]]) ~= -1) then
          local first = float_str:sub(1, 1)
          local sign = (first == '-') and '+' or '-'
          local head = fn.strpart(line, 0, idx0)
          local has_sign = (first == '+' or first == '-') and 1 or 0
          local tail = fn.strpart(line, idx0 + has_sign)
          local offset = 2 * fn.len(head .. sign .. tail) - fn.len(float_str)
          fn.setline(lnum, head .. sign .. tail)
          fn.cursor(lnum, cnum + offset + 1)
          return 0
        end
      end
    end
  end

  -- (2) カーソル下キーワードのトグル (true/false yes/no on/off 等)
  local char = fn.strcharpart(line, fn.charidx(line, cnum - 1), 1)
  if empty(strict) and fn.match(char, [[\C\k]]) ~= -1 then
    local word = fn.expand('<cword>')
    local ion = fn.index(vim.g.toggle_words_on, word, 0, 1)   -- 大小無視
    local ioff = fn.index(vim.g.toggle_words_off, word, 0, 1)
    local other
    if ioff ~= -1 then
      other = vim.g.toggle_words_on[ioff + 1]   -- Vim index は 0 始まり → Lua は +1
    elseif ion ~= -1 then
      other = vim.g.toggle_words_off[ion + 1]
    else
      other = ''
    end
    if not empty(other) then
      if fn.match(word, [[\C^\u\+$]]) ~= -1 then        -- UPPER
        other = fn.substitute(other, [[\(.\)]], [[\u\1]], 'g')
      elseif fn.match(word, [[\C^\u]]) ~= -1 then       -- Title
        other = fn.substitute(other, [[^\(.\)\(.*\)$]], [[\u\1\l\2]], '')
      else                                              -- lower
        other = fn.substitute(other, [[\(.\)]], [[\l\1]], 'g')
      end
      vim.cmd('normal! ciw' .. other)
      local offset = 2 * fn.len(other) - fn.len(word)
      fn.cursor(lnum, cnum + offset + 1)
      return 0
    end
  end

  -- (3) カーソル下の連続 on-off 文字トグル (&/|/+/-/0/1 等)
  local other, expand2 = '', expand_flag
  local ioff = fn.index(vim.g.toggle_chars_off, char, 0, 0)   -- 大小区別
  local ion = fn.index(vim.g.toggle_chars_on, char, 0, 0)
  if ioff ~= -1 then
    other = fn.strcharpart(vim.g.toggle_chars_on[ioff + 1], 0, 1)
    expand2 = truthy(expand_flag) and (fn.index(vim.g.toggle_chars_off, char, 26) ~= -1)
  elseif ion ~= -1 then
    other = fn.strcharpart(vim.g.toggle_chars_off[ion + 1], 0, 1)
    expand2 = truthy(expand_flag) and (fn.index(vim.g.toggle_chars_on, char, 26) ~= -1)
  end
  if not empty(other) then
    local regex0 = '[' .. char .. other .. ']'
    local regex = [[\%]] .. cnum .. 'c' .. regex0
    if truthy(expand2) then
      regex = regex0 .. '*' .. regex .. [[\+]]
    end
    local m = fn.matchstrpos(line, [[\C]] .. regex)
    local chars_m, idx0, idx1 = m[1], m[2], m[3]
    local others = fn['repeat'](other, fn.strchars(chars_m))
    local head = fn.strpart(line, 0, idx0)
    local tail = fn.strpart(line, idx1)
    fn.setline(lnum, head .. others .. tail)
    fn.cursor(lnum, idx1 + 1)   -- idx1 はマッチ末尾+1
    return 0
  end
  fn.cursor(lnum, cnum + 1)
  return (fn.match(char, [[\C\_s]]) ~= -1) and -1 or 1
end

-- 原版 toggle#toggle(...) range。
-- 引数: repeat_ (0/1), region (0=カーソル / 'v'/'V'/"\<C-v>"=ビジュアル / 非0数値=コマンドrange),
--       strict (0/1), firstline/lastline (コマンドrange 用。省略時は現在行)
function M.toggle(repeat_, region, strict, firstline, lastline)
  repeat_ = repeat_ or 0
  region = region or 0
  strict = strict or 0
  local count0, count1, count2 = 0, 0, 0   -- 成功 / 非空白失敗 / 空白失敗
  local lnums
  if empty(region) then
    lnums = { fn.line('.'), fn.line('.') }
  elseif type(region) == 'string' then      -- visual selection
    lnums = { fn.line("'<"), fn.line("'>") }
  else                                       -- command range
    lnums = { firstline or fn.line('.'), lastline or fn.line('.') }
  end

  -- リスト正規化は invocation ごと 1 回 (原版は s:toggle_cursor 内で列ごとに呼んでいた)
  toggle_validate()

  local winview = fn.winsaveview()
  for lnum = lnums[1], lnums[2] do
    local col1, col2, expand2
    if empty(region) then                                    -- カーソルのみ
      local c = fn.col('.'); col1, col2, expand2 = c, c, 1
    elseif type(region) == 'number' or region == 'V' then    -- visual line / command range
      col1, col2, expand2 = 1, fn.col({ lnum, '$' }), 1
    elseif region == 'v' then                                -- visual (行単位でない)
      col1 = (lnum == lnums[1]) and fn.col("'<") or 1
      col2 = (lnum == lnums[2]) and fn.col("'>") or fn.col({ lnum, '$' })
      expand2 = (lnum ~= lnums[1] and lnum ~= lnums[2]) and 1 or 0
    else                                                     -- visual block
      col1 = math.min(fn.col("'<"), fn.col("'>"))
      col2 = math.max(fn.col("'<"), fn.col("'>"))
      expand2 = 0
    end
    local cnum = -1
    fn.cursor(lnum, col1)
    while fn.col('.') > cnum and fn.col('.') <= col2 do
      cnum = fn.col('.')
      local status = toggle_cursor(expand2, strict)
      count0 = count0 + ((status ~= 0) and 0 or 1)
      count1 = count1 + ((status > 0) and status or 0)
      count2 = count2 - ((status < 0) and status or 0)
    end
  end

  if truthy(repeat_) and fn.exists('*repeat#set') == 1 then
    fn['repeat#set'](vim.api.nvim_replace_termcodes('<Plug>Toggle', true, false, true))
  end
  fn.winrestview(winview)
  local label = (count0 ~= 0) and 'Warning' or 'Error'
  local icount = (count1 ~= 0) and count1 or count2
  local ncount = count0 + icount
  local status = (count1 ~= 0 or count0 == 0) and icount or 0
  if status ~= 0 then
    local msg = label .. ': Toggle failed for '
    msg = msg .. ((ncount > 1) and (icount .. '/' .. ncount .. ' items') or 'item')
    msg = msg .. (empty(region) and ' under the cursor.' or ' in the range.')
    vim.cmd('redraw')
    vim.api.nvim_echo({ { msg, label .. 'Msg' } }, true, {})
  end
  return status
end

return M
