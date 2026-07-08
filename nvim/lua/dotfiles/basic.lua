local M = {}

local function set_globals()
  vim.g.omni_sql_no_default_maps = 1
  vim.opt.mouse = ""
end

local function set_options()
  local opt = vim.opt

  opt.swapfile = false
  opt.shortmess:append("I")

  opt.wildignore:append({ ".git", ".svn" })
  opt.wildignore:append({ "*.jpg", "*.bmp", "*.gif", "*.png", "*.jpeg" })
  opt.wildignore:append("*.sw?")
  opt.wildignore:append(".DS_Store")
  opt.wildignore:append({ "node_modules", "bower_components", "elm-stuff" })

  opt.synmaxcol = 200
  opt.grepprg = [[git grep -nI --no-color $*]]
  opt.grepformat = "%f:%l:%m"

  opt.number = true
  opt.expandtab = true
  opt.tabstop = 2
  opt.shiftwidth = 2
  opt.scrolloff = 5
  opt.formatoptions = "lmoq"
  opt.clipboard = { "unnamed", "unnamedplus" }
  opt.smartindent = true
  opt.showbreak = "↪"
  opt.showmatch = true
  opt.title = true
  -- CursorHold/CursorHoldI は basic.lua の checktime(disk stat) と LSP の document_highlight を
  -- 同時発火させる。300→500ms で発火密度を下げてタイピング中の連打を緩和 (体感優先)
  opt.updatetime = 500
  opt.signcolumn = "yes"
  opt.showmode = false -- モード表示は lualine が担うため、コマンドラインの -- INSERT -- は二重表示になる
end

local function set_user_commands()
  local function create_cmd(name, command)
    vim.api.nvim_create_user_command(name, command, {})
  end

  create_cmd("Q", "quit")
  create_cmd("W", "write")
  create_cmd("Wq", "wq")
  create_cmd("WQ", "wq")
  create_cmd("Vs", "vs")
  create_cmd("VS", "vs")
  create_cmd("Sp", "sp")
  create_cmd("SP", "sp")
  create_cmd("Tabe", "tabe")
  create_cmd("TAbe", "tabe")
  create_cmd("TABe", "tabe")
  create_cmd("TABE", "tabe")
end

local function set_keymaps()
  local map = vim.keymap.set
  local silent = { silent = true }

  map("n", "Q", "<Nop>", { noremap = true })
  map("n", ";", [[:<C-u>call append(line('.'), '')<CR>j]], silent)
  map("n", "<CR><CR>", "<C-w><C-w>", silent)
  map("n", "<C-p>", ":cprevious<CR>", silent)
  map("n", "<C-n>", ":cnext<CR>", silent)
  map("i", "<C-]>", "<Esc>", silent)
  map("n", "<C-]>", "<Esc>", silent)
  map("n", "<Esc><Esc>", [[:<C-u>set nohlsearch<CR>]], silent)
  map("n", "/", [[:<C-u>set hlsearch<CR>/]], { silent = false })
  map("n", "?", [[:<C-u>set hlsearch<CR>?]], { silent = false })
  map("n", "<leader>aa", ":enew<CR>", silent)
  map("i", "<C-y><C-w>", "<Esc>:w<CR>", silent)
  map("n", "<C-y><C-w>", ":w<CR>", silent)
  map("n", "<leader>sp", ":sp<CR>", silent)
  map("n", "<leader>vs", ":vs<CR>", silent)
  -- 現在のファイル名をクリップボードにコピー
  map("n", "<leader>n", function()
    local filepath = vim.fn.expand("%:~:.")
    vim.fn.setreg("+", filepath)
    print(string.format('"%s" をコピーしました', filepath))
  end, silent)
  -- ハイライト検索時にカーソルを次の候補に移動しない
  map("n", "*", "*N", { noremap = true, silent = true })
  map("n", "#", "#N", { noremap = true, silent = true })
end

local function set_highlights()
  local function apply()
    -- termguicolors 有効時は cterm 値が無視されるため gui 属性で指定
    vim.api.nvim_set_hl(0, "ZenkakuSpace", { underline = true, bg = "darkgray" })
    vim.cmd([[match ZenkakuSpace /　/]])
  end

  local group = vim.api.nvim_create_augroup("dotfiles_basic_highlights", { clear = true })
  vim.api.nvim_create_autocmd("ColorScheme", {
    group = group,
    callback = apply,
  })

  apply()
end

local function set_autocmds()
  local group = vim.api.nvim_create_augroup("dotfiles_basic_autocmds", { clear = true })

  vim.api.nvim_create_autocmd("WinLeave", {
    group = group,
    callback = function()
      vim.opt_local.cursorline = false
    end,
  })

  vim.api.nvim_create_autocmd({ "WinEnter", "BufRead" }, {
    group = group,
    callback = function()
      vim.opt_local.cursorline = true
    end,
  })

  vim.api.nvim_create_autocmd({ "BufRead", "BufNewFile" }, {
    group = group,
    pattern = "db/Schemafile",
    command = "set filetype=ruby",
  })

  vim.api.nvim_create_autocmd({ "BufRead", "BufNewFile" }, {
    group = group,
    pattern = "*.sql.erb",
    command = "set filetype=sql",
  })

  vim.api.nvim_create_autocmd({ "BufRead", "BufNewFile" }, {
    group = group,
    pattern = "*.Schemafile",
    command = "set filetype=ruby",
  })

  vim.api.nvim_create_autocmd("BufReadPost", {
    group = group,
    pattern = "*",
    callback = function()
      local mark = vim.api.nvim_buf_get_mark(0, '"')
      local lcount = vim.api.nvim_buf_line_count(0)
      if mark[1] > 0 and mark[1] <= lcount then
        vim.api.nvim_win_set_cursor(0, mark)
      end
    end,
  })

  vim.api.nvim_create_autocmd("QuickFixCmdPost", {
    group = group,
    pattern = "*grep*",
    callback = function()
      if not vim.tbl_isempty(vim.fn.getqflist()) then
        vim.cmd("cwindow")
      end
    end,
  })

  vim.api.nvim_create_autocmd({ "FocusGained", "BufEnter", "CursorHold", "CursorHoldI" }, {
    group = group,
    pattern = "*",
    callback = function()
      if vim.fn.mode() ~= "c" then
        vim.cmd("checktime")
      end
    end,
  })

  vim.api.nvim_create_autocmd("FileChangedShellPost", {
    group = group,
    pattern = "*",
    callback = function()
      vim.notify("File changed on disk. Buffer reloaded.", vim.log.levels.WARN)
    end,
  })
end

function M.setup()
  set_globals()
  set_options()
  set_user_commands()
  set_keymaps()
  set_highlights()
  set_autocmds()
end

return M
