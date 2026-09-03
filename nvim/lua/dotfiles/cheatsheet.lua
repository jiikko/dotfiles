-- 常駐チートシート: 画面右下の小さな float に「このバッファで使えるキー」を出し続ける。
--
-- 中身は buffer-local な normal マッピング (desc 付き) を動的に集めたもの。言語固有キーは
-- 全て buffer-local に寄せてある (nvim/ftplugin/*.lua と lsp.lua の on_attach) ので、
-- 「buffer-local を並べる」だけで「今の言語で使えるキー」になる。手書きの一覧を持たない
-- のは、キーを足す/改名するたびにここも直す二重管理を避けるため (desc が唯一の出典)。
--
-- split でなく float なのは、フォーカスに入らず (focusable=false)、<C-w> 巡回にも出ず、
-- 本文のレイアウトを動かさないため。incline.nvim がファイル名を右上に出しているのと同じ手法。
-- which-key の <leader>? (押した瞬間の popup) とは補完関係で、こちらは常に見えている側。
--
-- 出さない条件: buffer-local マッピングが無いバッファ (プレーンテキスト等) / 本文の window が
-- 小さすぎる / float・特殊 buftype (telescope, nvim-tree, trouble 等) にいるとき。
-- トグル: <leader>k (setup で張る)。状態はセッション内で保持する (既定 on)。
local M = {}

local ns_hl_title = "DotfilesCheatsheetTitle"
local ns_hl_key = "DotfilesCheatsheetKey"
local ns_hl_desc = "DotfilesCheatsheetDesc"
local ns_hl_bg = "DotfilesCheatsheetBg"
local ns_hl_border = "DotfilesCheatsheetBorder"

local state = {
  enabled = true,
  win = nil,
  buf = nil,
}

local MAX_LINES = 18
local MIN_MAIN_HEIGHT = 24 -- 本文の window がこれより低いときは出さない (本文が隠れる)
local MIN_MAIN_WIDTH = 120

-- lhs の表示用整形。mapleader (空白) は見えないので SPC に置き換え、termcode は <> 表記のまま。
local function pretty_lhs(lhs)
  local leader = vim.g.mapleader or "\\"
  local s = lhs
  if leader ~= "" then
    s = s:gsub("^" .. vim.pesc(leader), "SPC ")
  end
  s = s:gsub(" $", "")
  return s
end

-- 現在バッファの buffer-local normal マッピング (desc 付き) を集める。
-- desc の無いものは意図を表示できないので出さない (出すなら desc を付けるのが正)。
local function collect(bufnr)
  local items = {}
  for _, m in ipairs(vim.api.nvim_buf_get_keymap(bufnr, "n")) do
    if m.desc and m.desc ~= "" then
      table.insert(items, { lhs = pretty_lhs(m.lhs), desc = m.desc })
    end
  end
  -- 並びを固定する (LspAttach の順序で揺れないように)。素のキー (gd / ]] / <C-k>) を先、
  -- SPC 始まりを後にまとめ、それぞれ大文字小文字を無視した辞書順
  local function rank(lhs) return lhs:match("^SPC") and 2 or 1 end
  table.sort(items, function(a, b)
    local ra, rb = rank(a.lhs), rank(b.lhs)
    if ra ~= rb then return ra < rb end
    return a.lhs:lower() < b.lhs:lower()
  end)
  return items
end

-- 表示すべきでない window/buffer か
local function should_hide(win)
  if not state.enabled then return true end
  if vim.api.nvim_win_get_config(win).relative ~= "" then return true end -- float 上にいる
  local buf = vim.api.nvim_win_get_buf(win)
  if vim.bo[buf].buftype ~= "" then return true end -- nofile/terminal/prompt/quickfix 等
  if vim.o.columns < MIN_MAIN_WIDTH or vim.o.lines < MIN_MAIN_HEIGHT then return true end
  return false
end

local function close()
  if state.win and vim.api.nvim_win_is_valid(state.win) then
    vim.api.nvim_win_close(state.win, true)
  end
  state.win = nil
end

local function ensure_buf()
  if state.buf and vim.api.nvim_buf_is_valid(state.buf) then return state.buf end
  local buf = vim.api.nvim_create_buf(false, true)
  vim.bo[buf].buftype = "nofile"
  vim.bo[buf].bufhidden = "hide"
  vim.bo[buf].swapfile = false
  vim.bo[buf].filetype = "dotfiles-cheatsheet"
  state.buf = buf
  return buf
end

-- items を行と highlight 範囲に変換する。幅は最長行に合わせる (上限は画面の 1/3)。
local function render_lines(items, title)
  local key_w = 0
  for _, it in ipairs(items) do key_w = math.max(key_w, vim.fn.strdisplaywidth(it.lhs)) end
  local lines, marks = {}, {}
  local max_w = math.floor(vim.o.columns / 3)
  for i, it in ipairs(items) do
    if i > MAX_LINES then
      table.insert(lines, string.format("  … 他 %d 件 (SPC ? で全部)", #items - MAX_LINES))
      table.insert(marks, { line = #lines - 1, col = 0, len = -1, hl = ns_hl_desc })
      break
    end
    local pad = key_w - vim.fn.strdisplaywidth(it.lhs)
    local line = " " .. it.lhs .. string.rep(" ", pad) .. "  " .. it.desc
    if vim.fn.strdisplaywidth(line) > max_w then
      line = vim.fn.strcharpart(line, 0, max_w - 1) .. "…"
    end
    table.insert(lines, line)
    table.insert(marks, { line = #lines - 1, col = 1, len = #it.lhs, hl = ns_hl_key })
    table.insert(marks, { line = #lines - 1, col = 1 + #it.lhs + pad + 2, len = -1, hl = ns_hl_desc })
  end
  local width = vim.fn.strdisplaywidth(title) + 2
  for _, l in ipairs(lines) do width = math.max(width, vim.fn.strdisplaywidth(l)) end
  return lines, marks, math.min(width, max_w)
end

local ns = vim.api.nvim_create_namespace("dotfiles_cheatsheet")

function M.refresh()
  local win = vim.api.nvim_get_current_win()
  if should_hide(win) then
    close()
    return
  end
  local src = vim.api.nvim_win_get_buf(win)
  local items = collect(src)
  if #items == 0 then
    close()
    return
  end

  local ft = vim.bo[src].filetype
  local title = " " .. (ft ~= "" and ft or "keys") .. " keys "
  local lines, marks, width = render_lines(items, title)
  local height = #lines

  local buf = ensure_buf()
  vim.bo[buf].modifiable = true
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
  for _, mk in ipairs(marks) do
    local end_col = mk.len == -1 and #lines[mk.line + 1] or (mk.col + mk.len)
    vim.api.nvim_buf_set_extmark(buf, ns, mk.line, mk.col, { end_col = end_col, hl_group = mk.hl })
  end

  -- 右下。laststatus/cmdheight の分だけ上に寄せる (statusline の上に乗せない)
  local row = vim.o.lines - vim.o.cmdheight - (vim.o.laststatus > 0 and 1 or 0) - height - 2
  local col = vim.o.columns - width - 2
  local cfg = {
    relative = "editor",
    anchor = "NW",
    row = math.max(row, 0),
    col = math.max(col, 0),
    width = width,
    height = height,
    style = "minimal",
    focusable = false,
    border = "rounded",
    title = title,
    title_pos = "left",
    zindex = 20, -- telescope/which-key (50) より下、通常の float より控えめ
    noautocmd = true,
  }
  if state.win and vim.api.nvim_win_is_valid(state.win) then
    cfg.noautocmd = nil -- set_config では受け付けない
    vim.api.nvim_win_set_config(state.win, cfg)
  else
    state.win = vim.api.nvim_open_win(buf, false, cfg)
    vim.wo[state.win].winhighlight = table.concat({
      "NormalFloat:" .. ns_hl_bg,
      "FloatBorder:" .. ns_hl_border,
      "FloatTitle:" .. ns_hl_title,
    }, ",")
    vim.wo[state.win].winblend = 10
    vim.wo[state.win].wrap = false
  end
end

function M.toggle()
  state.enabled = not state.enabled
  M.refresh()
  vim.notify("Cheatsheet: " .. (state.enabled and "on" or "off"))
end

-- テストが読むための状態公開 (window が出ているか / 中身)
function M._state() return state end

function M.setup()
  local hl = require("dotfiles.hl")
  local pal = require("dotfiles.palette")
  -- 本文の邪魔にならない淡色。キーだけ明るくして目が拾えるようにする
  hl.set(ns_hl_bg, { bg = pal.dark0_hard.hex, ctermbg = pal.dark0_hard.cterm })
  hl.set(ns_hl_border, { fg = pal.dark3.hex, bg = pal.dark0_hard.hex, ctermfg = pal.dark3.cterm, ctermbg = pal.dark0_hard.cterm })
  hl.set(ns_hl_title, { fg = pal.light4.hex, bg = pal.dark0_hard.hex, ctermfg = pal.light4.cterm, ctermbg = pal.dark0_hard.cterm, bold = true })
  hl.set(ns_hl_key, { fg = pal.bright_yellow.hex, bg = pal.dark0_hard.hex, ctermfg = pal.bright_yellow.cterm, ctermbg = pal.dark0_hard.cterm, bold = true })
  hl.set(ns_hl_desc, { fg = pal.light4.hex, bg = pal.dark0_hard.hex, ctermfg = pal.light4.cterm, ctermbg = pal.dark0_hard.cterm })

  local grp = vim.api.nvim_create_augroup("dotfiles_cheatsheet", { clear = true })
  -- LspAttach は on_attach で keymap が張られた後に走るよう schedule で 1 tick 遅らせる
  vim.api.nvim_create_autocmd({ "BufEnter", "WinEnter", "VimResized", "FileType" }, {
    group = grp,
    callback = function() vim.schedule(M.refresh) end,
  })
  vim.api.nvim_create_autocmd({ "LspAttach", "LspDetach" }, {
    group = grp,
    callback = function() vim.schedule(M.refresh) end,
  })
  -- cmdline 入力中や補完中に被らないよう、insert では消して normal に戻ったら出す
  vim.api.nvim_create_autocmd("InsertEnter", { group = grp, callback = close })
  vim.api.nvim_create_autocmd("InsertLeave", { group = grp, callback = function() vim.schedule(M.refresh) end })

  vim.keymap.set("n", "<leader>k", M.toggle, { silent = true, desc = "Toggle cheatsheet float" })
  vim.api.nvim_create_user_command("Cheatsheet", M.toggle, { desc = "Toggle cheatsheet float" })
end

return M
