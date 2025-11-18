-- Bootstrap lazy.nvim
local lazypath = vim.fn.stdpath("data") .. "/lazy/lazy.nvim"
if not (vim.uv or vim.loop).fs_stat(lazypath) then
  local lazyrepo = "https://github.com/folke/lazy.nvim.git"
  local out = vim.fn.system({ "git", "clone", "--filter=blob:none", "--branch=stable", lazyrepo, lazypath })
  if vim.v.shell_error ~= 0 then
    vim.api.nvim_echo({
      { "Failed to clone lazy.nvim:\n", "ErrorMsg" },
      { out, "WarningMsg" },
      { "\nPress any key to exit..." },
    }, true, {})
    vim.fn.getchar()
    os.exit(1)
  end
end
vim.opt.rtp:prepend(lazypath)

-- Make sure to setup `mapleader` and `maplocalleader` before
-- loading lazy.nvim so that mappings are correct.
-- This is also a good place to setup other settings (vim.opt)
vim.g.mapleader = " "
vim.g.maplocalleader = " "

-- グローバル変数の設定
vim.g.omni_sql_no_default_maps = 1  -- omni_sqlのデフォルトマッピングを無効化
-- マウスを無効にする
vim.opt.mouse = ""

-- オプション設定
vim.opt.backup = false        -- バックアップファイルを作成しない
vim.opt.swapfile = false      -- スワップファイルを作成しない
vim.opt.shortmess:append("I") -- 起動時のメッセージを非表示

-- wildignore の設定（指定したパターンのファイルを無視）
vim.opt.wildignore:append({ ".git", ".svn" })
vim.opt.wildignore:append({ "*.jpg", "*.bmp", "*.gif", "*.png", "*.jpeg" })
vim.opt.wildignore:append("*.sw?")
vim.opt.wildignore:append(".DS_Store")
vim.opt.wildignore:append({ "node_modules", "bower_components", "elm-stuff" })

-- シンタックスハイライトの列数上限
vim.opt.synmaxcol = 200

-- grepコマンドの設定（git grepを使用）
vim.opt.grepprg = [[git grep -nI --no-color $*]]
vim.opt.grepformat = "%f:%l:%m"

-- ユーザーコマンドの定義用ヘルパー関数
local create_cmd = function(name, command)
  vim.api.nvim_create_user_command(name, command, {})
end

-- ユーザーコマンドの定義
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
vim.keymap.set("n", "Q", "<Nop>", { noremap = true })  -- Qを無効化するマッピング

-- Setup lazy.nvim
require("lazy").setup({
  { "morhetz/gruvbox",
    config = function()
      vim.cmd("colorscheme gruvbox")
    end,
  },
  { "tpope/vim-rails", ft = { "ruby", "eruby" } },
  { "lukelbd/vim-toggle",
    event = "VeryLazy",
    init = function()
      vim.g.toggle_map = '+'
      vim.g.toggle_words_on = {
        "and", "if", "unless", "elsif", "it", "specify", "describe",
        "true", "yes", "on", "public", "protected", "&&", "ある", "はい", "とき", "なし", "する"
      }
      vim.g.toggle_words_off = {
        "or", "unless", "if", "else", "specify", "it", "context",
        "false", "no", "off", "protected", "private", "||", "ない", "いいえ", "時", "あり", "しない"
      }
    end,
  },
  { "vim-jp/vimdoc-ja" },
  { "kana/vim-operator-user" },
  { "tyru/operator-camelize.vim", lazy = true },
  { "hashivim/vim-terraform",
    ft = { "terraform", "tf", "hcl" },
    config = function()
      vim.g.terraform_align = 1
      vim.g.terraform_fold_sections = 1
      vim.g.terraform_fmt_on_save = 1
    end
  },
  { "fatih/vim-go",
    ft = { "go" },
    build = ":GoUpdateBinaries",
    config = function()
      vim.g.go_null_module_warning = 0
      -- GoDecls のキーマッピング
      vim.api.nvim_set_keymap('n', '<leader>gd', ':GoDecls<CR>', { noremap = true, silent = true })
      vim.api.nvim_set_keymap('n', '<leader>gD', ':GoDeclsDir<CR>', { noremap = true, silent = true })
    end
  },
  { "andymass/vim-matchup",
    config = function()
      vim.g.loaded_matchit = 1
      vim.g.matchup_matchparen_stopline = 400
      vim.g.matchup_matchparen_deferred = 1
      vim.g.matchup_matchparen_offscreen = { method = "popup" }
      vim.g.matchup_surround_enabled = 1
    end,
  },
  { "windwp/nvim-ts-autotag" },
  { "nvim-treesitter/nvim-treesitter",
    build = ":TSUpdate",
    config = function()
      require("nvim-treesitter.configs").setup({
        auto_install = true,
        ensure_installed = { "diff", "awk", "bash", "c", "cmake", "css", "dockerfile", "elixir", "go", "graphql", "html", "http", "javascript", "json", "lua", "make", "markdown", "markdown_inline", "python", "ruby", "rust", "scala", "scss", "sql", "typescript", "vim", "yaml" },
        highlight = { enable = true },
        matchup = { enable = true },
        endwise = { enable = true },
        indent = { enable = true },
      })
    end,
  },
  { "RRethy/nvim-treesitter-endwise",
    dependencies = { "nvim-treesitter/nvim-treesitter" }
  },
  { "folke/which-key.nvim" },
  { "itchyny/lightline.vim",
    config = function()
      vim.cmd([[
        function! RelativePathFromGitRoot()
          if exists("b:git_dir")
            let l:root = fnamemodify(b:git_dir, ':h')
          else
            let l:root = system("git -C " . expand('%:p:h') . " rev-parse --show-toplevel")
            let l:root = substitute(l:root, '\n$', '', '')
          endif
          if v:shell_error
            return expand('%')
          endif
          let l:relative_path = substitute(expand('%:p'), '^' . l:root . '/', '', '')
          return l:relative_path
        endfunction
      ]])
      vim.g.lightline = {
        colorscheme = "wombat",
        active = { left = { { "mode", "paste" }, { "cocstatus", "currentfunction", "readonly", "filename", "modified" } } },
        component_function = {
          cocstatus = "coc#status",
          currentfunction = "CocCurrentFunction",
          filename = "RelativePathFromGitRoot",
        },
      }
    end,
  },
  { "github/copilot.vim" },
  { "neoclide/coc.nvim", branch = "release",
    config = function()
      vim.g.coc_global_extensions = {
        "coc-actions",
        "coc-cspell-dicts",
        "coc-html",
        "coc-css",
        "coc-html-css-support",
        "coc-docker",
        "coc-diagnostic",
        "coc-dictionary",
        "coc-eslint",
        "coc-git",
        "coc-go",
        "coc-pyright",
        "@yaegassy/coc-tailwindcss3",
        "coc-highlight",
        "coc-json",
        "coc-markdownlint",
        "coc-prettier",
        "coc-spell-checker",
        "coc-tslint-plugin",
        "coc-tsserver",
        "coc-yaml",
        "coc-solargraph",
        "coc-sh",
        "coc-sql",
        "coc-webview",
        "coc-swagger",
      }

      local keymap = vim.api.nvim_set_keymap
      local opts = { noremap = true, silent = true }
      local expr_opts = { noremap = true, silent = true, expr = true }
      vim.api.nvim_set_keymap(
        "i",
        "<CR>",
        'coc#pum#visible() ? coc#pum#confirm() : "\\<CR>"',
        { expr = true, silent = true }
      )
      -- 更新間隔を短縮
      vim.o.updatetime = 300
      -- signcolumn を常に表示
      vim.wo.signcolumn = "yes"
      -- CocActionAsyncを呼び出してバッファ整形を実行する
      vim.api.nvim_create_user_command('Format', function()
        -- Cocの非同期フォーマットアクションを実行
        vim.fn.CocActionAsync('format')
      end, {})
      vim.api.nvim_set_keymap('n', '<leader>f', ':Format<CR>', { noremap = true, silent = true })
      -- `:OR` コマンドを追加 (インポート整理)
      vim.api.nvim_create_user_command("OR", function()
        vim.fn.CocActionAsync("runCommand", "editor.action.organizeImport")
      end, { nargs = 0 })
      -- カーソルをホールドするとシンボルをハイライト
      vim.api.nvim_create_autocmd("CursorHold", {
        pattern = "*",
        callback = function()
          vim.fn.timer_start(500, function()
            vim.fn.CocActionAsync("highlight")
          end)
        end,
      })
      -- ステータスラインに Coc の状態を表示
      vim.o.statusline = "%{coc#status()}%{get(b:,'coc_current_function','')}"
      -- 現在のファイル名をクリップボードにコピー
      vim.keymap.set("n", "<leader>n", function()
        local filepath = vim.fn.expand("%:~:.")
        vim.fn.setreg("+", filepath)
        print(string.format('"%s" をコピーしました', filepath))
      end, opts)
      -- ケース変換
      keymap("n", "<leader>c", "<Plug>(operator-camelize)", opts)
      keymap("n", "<leader>C", "<Plug>(operator-decamelize)", opts)
      -- カーソル位置のドキュメント表示
      function _G.show_documentation()
        local filetype = vim.bo.filetype
        if vim.tbl_contains({ "vim", "help" }, filetype) then
          vim.cmd("help " .. vim.fn.expand("<cword>"))
        elseif vim.fn.eval('coc#rpc#ready()') == 1 then
          vim.fn.CocActionAsync("doHover")
        else
          print("No documentation available")
        end
      end
      keymap("n", "t", "<Cmd>lua show_documentation()<CR>", opts)
      -- 診断リストを開く
      keymap("n", "<leader>a", ":CocList diagnostics<CR>", opts)
      -- 選択範囲を指定 (CTRL-S)
      keymap("n", "<C-s>", "<Plug>(coc-range-select)", opts)
      keymap("x", "<C-s>", "<Plug>(coc-range-select)", opts)
      -- **Coc の浮動ウィンドウスクロール**
      keymap("n", "<C-f>", 'coc#float#has_scroll() ? coc#float#scroll(1) : "\\<C-f>"', expr_opts)
      keymap("n", "<C-b>", 'coc#float#has_scroll() ? coc#float#scroll(0) : "\\<C-b>"', expr_opts)
      keymap("i", "<C-f>", 'coc#float#has_scroll() ? "\\<c-r>=coc#float#scroll(1)\\<cr>" : "\\<Right>"', expr_opts)
      keymap("i", "<C-b>", 'coc#float#has_scroll() ? "\\<c-r>=coc#float#scroll(0)\\<cr>" : "\\<Left>"', expr_opts)
      keymap("v", "<C-f>", 'coc#float#has_scroll() ? coc#float#scroll(1) : "\\<C-f>"', expr_opts)
      keymap("v", "<C-b>", 'coc#float#has_scroll() ? coc#float#scroll(0) : "\\<C-b>"', expr_opts)
      -- 診断メッセージの前後移動
      keymap("n", "[g", "<Plug>(coc-diagnostic-prev)", opts)
      keymap("n", "]g", "<Plug>(coc-diagnostic-next)", opts)
      -- ハイライト検索時にカーソルを次の候補に移動しない
      keymap("n", "*", "*N", opts)
      keymap("n", "#", "#N", opts)
      -- コードアクション
      keymap("n", "<leader>ac", "<Plug>(coc-codeaction-cursor)", opts)
      keymap("n", "<leader>as", "<Plug>(coc-codeaction-source)", opts)
      keymap("n", "<leader>qf", "<Plug>(coc-fix-current)", opts)
      -- 定義ジャンプと参照リスト（便利キーバインド）
      -- スマートジャンプ: 実装があれば実装へ、なければ定義へ
      function _G.smart_go_to_definition()
        vim.fn.CocActionAsync('jumpImplementation', function(err, result)
          if err or not result or vim.tbl_isempty(result) then
            -- 実装が見つからない場合は定義へジャンプ
            vim.fn.CocActionAsync('jumpDefinition')
          end
        end)
      end
      keymap("n", "<C-j>", "<Cmd>lua smart_go_to_definition()<CR>", opts)
      keymap("n", "<C-k>", "<Plug>(coc-references)", opts)

      -- NOTE: 必要か？
      -- Diagnosticsの、左横のアイコンの色設定
      -- CocErrorSign の設定: 前景色 15、背景色 196
      vim.api.nvim_set_hl(0, "CocErrorSign", { ctermfg = 15, ctermbg = 196 })
      -- CocWarningSign の設定: 前景色 0、背景色 172
      vim.api.nvim_set_hl(0, "CocWarningSign", { ctermfg = 0, ctermbg = 172 })
    end,
  },
  { "nvim-telescope/telescope.nvim",
    cmd = "Telescope",
    keys = {
      { "<leader>ff", function() require("telescope.builtin").find_files() end, desc = "Find files" },
      { "<leader>fg", function() require("telescope.builtin").live_grep() end, desc = "Live grep" },
      { "<leader>fb", function() require("telescope.builtin").buffers() end, desc = "List buffers" },
      { "<leader>fn", function() require("telescope").extensions.notify.notify() end, desc = "Notify history" },
    },
    dependencies = {
      "nvim-lua/plenary.nvim",
      "fannheyward/telescope-coc.nvim",
      "nvim-telescope/telescope-ui-select.nvim",
    },
    config = function()
      local telescope = require("telescope")
      telescope.setup({
        defaults = {
          sorting_strategy = "ascending",
          layout_strategy = "vertical",
          layout_config = { height = 0.9 },
          file_ignore_patterns = { "^.git/", "^node_modules/", "package-lock.json", "yarn.lock", "yarn-error.log" },
          border = true,
          prompt_prefix = "🔍 ",
        },
        extensions = {
          coc = {
            theme = "ivy",
            prefer_locations = true,
          },
          ["ui-select"] = {
            require("telescope.themes").get_dropdown({}),
          },
        },
      })
      telescope.load_extension("coc")
      telescope.load_extension("ui-select")
      telescope.load_extension("notify")
      vim.keymap.set("n", "<leader>fd", "<Cmd>CocDiagnostics<CR>")
    end,
  },
  { "rbtnn/vim-ambiwidth" },
  { "akinsho/bufferline.nvim",
    event = "BufAdd",
    dependencies = { "nvim-tree/nvim-web-devicons" },
    config = function()
      local ok, bufferline = pcall(require, "bufferline")
      if not ok then
        return
      end

      bufferline.setup({
        options = {
          diagnostics = "nvim_lsp",
          show_close_icon = false,
          show_buffer_close_icons = false,
          always_show_bufferline = true,
        },
        highlights = {
          buffer_selected = {
            ctermfg = 0,   -- 黒（読みやすさ重視）
            ctermbg = 205, -- 落ち着いたピンク（ターミナルカラー）
          },
        },
      })

      local map = vim.keymap.set
      local silent = { silent = true }
      map({ "n", "v", "o" }, "<Right>", "<Cmd>BufferLineCycleNext<CR>", silent)
      map({ "n", "v", "o" }, "<Left>", "<Cmd>BufferLineCyclePrev<CR>", silent)
      map("n", "gt", "<Cmd>BufferLineCycleNext<CR>", silent)
      map("n", "gT", "<Cmd>BufferLineCyclePrev<CR>", silent)
      map("n", "<C-a><C-a>", "<Cmd>bdelete<CR>", silent)
    end,
  },
  {
    "nvim-tree/nvim-tree.lua",
    cmd = { "NvimTreeToggle", "NvimTreeFocus", "NvimTreeFindFile", "NvimTreeFindFileToggle" },
    keys = {
      { "<C-e>", "<cmd>NvimTreeToggle<CR>", desc = "Toggle file tree", mode = { "n" } },
      { "<C-e>", "<Esc>:NvimTreeToggle<CR>", desc = "Toggle file tree", mode = "v" },
      { "<C-e>", "<Esc>:NvimTreeToggle<CR>", desc = "Toggle file tree", mode = "i" },
      { "<C-e>", ":NvimTreeToggle<CR>", desc = "Toggle file tree", mode = "o" },
      { "<C-e>", "<C-u>:NvimTreeToggle<CR>", desc = "Toggle file tree", mode = "c" },
      { "<leader>nt", "<cmd>NvimTreeToggle<CR>", desc = "Toggle file tree" },
      { "<leader>nf", "<cmd>NvimTreeFindFile!<CR>", desc = "Reveal file" },
    },
    config = function()
      -- https://github.com/nvim-tree/nvim-tree.lua/blob/70825f23db61ecd900c4cfea169bffe931926a9d/doc/nvim-tree-lua.txt#L158
      local function my_on_attach(bufnr)
        local api = require("nvim-tree.api")
        local function opts(desc)
          return { desc = "nvim-tree: " .. desc, buffer = bufnr, noremap = true, silent = true, nowait = true }
        end
        api.config.mappings.default_on_attach(bufnr)
        vim.keymap.del("n", "<C-E>", { buffer = bufnr })
        vim.keymap.set("n", "i", api.node.open.horizontal, opts("Open in horizontal split"))
        vim.keymap.set("n", "s", api.node.open.vertical, opts("Open in vertical split"))
      end

      require("nvim-tree").setup({
        view = {
          width = 50,
          side = "left",
          float = {
            enable = true,
            open_win_config = {
              width = 50,
              height = 60,
            }
          },
        },
        renderer = {
          indent_markers = {
            enable = true,
          },
        },
        on_attach = my_on_attach,
      })

    end,
  },
  { "nvim-tree/nvim-web-devicons" },
  { "lukas-reineke/indent-blankline.nvim",
    config = function()
      require("ibl").setup()
    end
  },
  { "rcarriga/nvim-notify",
    init = function()
      local default_notify = vim.notify
      vim.notify = function(...)
        vim.notify = default_notify
        require("lazy").load({ plugins = { "nvim-notify" } })
        return vim.notify(...)
      end
    end,
    config = function()
      -- TODO: これいる？
      vim.api.nvim_set_option('termguicolors', false)
      local notify = require('notify')
      notify.setup({
        render = "minimal",
        stages = "fade_in_slide_out",
        dismissed = {},
      })

      local original_notify = notify
      local custom_notify = function(msg, log_level, opts)
        -- FIXME: ターミナルがtrue colorに対応していないので無視する
        if msg:match("Opacity changes require termguicolors to be set.") then
          return
        end
        original_notify(msg, log_level, opts)
      end

      vim.notify = custom_notify
    end
  },
  { "numToStr/Comment.nvim" },
  { "lewis6991/gitsigns.nvim",
    event = { "BufReadPre", "BufNewFile" },
    config = function()
      local gitsigns = require("gitsigns")
      gitsigns.setup({
        current_line_blame = false,
        current_line_blame_opts = {
          virt_text = true,
          virt_text_pos = "eol",
          delay = 500,
        },
        current_line_blame_formatter = "<abbrev_sha> <author_time:%Y/%m/%d> <author>: <summary>",
        signs = {
          add = { text = "▎" },
          change = { text = "▎" },
          delete = { text = "" },
          topdelete = { text = "" },
          changedelete = { text = "▎" },
          untracked = { text = "▎" },
        },
      })

      vim.api.nvim_set_hl(0, "GitSignsCurrentLineBlame", {
        fg = "#1d2021",
        bg = "#fabd2f",
        ctermfg = 234,
        ctermbg = 214,
        bold = true,
        italic = false,
      })

      local map = vim.keymap.set
      local opts = { silent = true, desc = "Toggle git blame" }
      map({ "n", "v" }, "<leader>gb", gitsigns.toggle_current_line_blame, opts)
    end,
  },
  { "dstein64/nvim-scrollview",
    event = "BufReadPost",
    config = function()
      require("scrollview").setup()
    end,
  },
  { "karb94/neoscroll.nvim",
    event = "VeryLazy",
    config = function()
      require("neoscroll").setup({
        mappings = { "<C-u>", "<C-d>" },
        hide_cursor = true,
        respect_scrolloff = true,
        easing_function = "cubic",
        performance_mode = false,
      })
    end,
  },
  {
    "folke/flash.nvim",
    event = "VeryLazy",
    ---@type Flash.Config
    opts = {},
    opts = {
      modes = {
        char = {
          keys = {},
        },
      },
    },
    -- stylua: ignore
    keys = {
      { "s", mode = { "n", "x", "o" }, function() require("flash").jump() end, desc = "Flash" },
      { "S", mode = { "n", "x", "o" }, function() require("flash").treesitter() end, desc = "Flash Treesitter" },
      { "r", mode = "o", function() require("flash").remote() end, desc = "Remote Flash" },
      { "R", mode = { "o", "x" }, function() require("flash").treesitter_search() end, desc = "Treesitter Search" },
      { "<c-s>", mode = { "c" }, function() require("flash").toggle() end, desc = "Toggle Flash Search" },
    },
  },
  { 'echasnovski/mini.nvim', version = '*', event = "VeryLazy",
    config = function()
      local animate = require("mini.animate")
      animate.setup({
        cursor = {
          enable = false,
          timing = animate.gen_timing.exponential({ easing = "out", duration = 800, unit = "total" }),
          path = animate.gen_path.line({ predicate = function() return true end }),
        },
        scroll = { enable = false, },
        resize = { enable = false, },
        open = { enable = false, },
        close = { enable = false, },
      })

      require("mini.trailspace").setup()
      vim.api.nvim_set_hl(0, "MiniTrailspace", {
        fg = "NONE",
        bg = "#fb4934",
        ctermbg = 160,
      })
    end,
  },
  { "MeanderingProgrammer/render-markdown.nvim",
    ft = { "markdown", "md", "mdx", "rmd" },
  },
}, {
  install = { colorscheme = { "habamax" } },
  checker = { enabled = true },
})

-- Basic editor settings (ported from legacy basic.vim)
local opt = vim.opt
opt.backspace = { "indent", "eol", "start" }
opt.number = true
opt.history = 10000
opt.ruler = true
opt.showcmd = true
opt.incsearch = true
opt.laststatus = 2
opt.hlsearch = true
opt.wrap = true
opt.expandtab = true
opt.tabstop = 2
opt.shiftwidth = 2
opt.softtabstop = 0
opt.scrolloff = 5
opt.formatoptions = "lmoq"
opt.showmode = true
opt.clipboard = { "unnamed", "unnamedplus" }
opt.smarttab = true
opt.smartindent = true
opt.showbreak = "↪"
opt.wildmenu = true
opt.showmatch = true
opt.title = true
opt.lazyredraw = true
opt.vb = true
pcall(function() -- newer Neovim versions removed this termcap option
  opt.t_vb = ""
end)
opt.wildchar = 9

local map = vim.keymap.set
local silent = { silent = true }
map("n", ";", [[:<C-u>call append(expand('.'), '')<CR>j]], silent)
map("n", "<CR><CR>", "<C-w><C-w>", silent)
map("n", "<C-p>", ":cprevious<CR>", silent)
map("n", "<C-n>", ":cnext<CR>", silent)
map("n", "<C-f>", "<Right>", silent)
map("n", "<C-b>", "<Left>", silent)
map("i", "<C-f>", "<Right>", silent)
map("i", "<C-b>", "<Left>", silent)
map("i", "<C-]>", "<Esc>", silent)
map("n", "<C-]>", "<Esc>", silent)
map("n", "<Esc><Esc>", [[:<C-u>set nohlsearch<CR>]], silent)
map("n", "/", [[:<C-u>set hlsearch<CR>/]], { silent = false })
map("n", "?", [[:<C-u>set hlsearch<CR>?]], { silent = false })
map("n", "*", [[:<C-u>set hlsearch<CR>*]], { silent = false })
map("n", "#", [[:<C-u>set hlsearch<CR>#]], { silent = false })
map("n", "<leader>rw", [[obegin; raise; rescue => e; File.write("/tmp/ruby_caller", e.backtrace.join("\n")) && raise; end<Esc>]], silent)
map("n", "<leader>rr", [[:cfile /tmp/ruby_caller<CR>:cw<Esc>]], silent)
map("n", "<leader>re", ":e /tmp/ruby_caller<Esc>", silent)
map("n", "<leader>ds", ":e db/schema.rb<Esc>", silent)
map("n", "<leader>yr", "o@return []<Esc>", silent)
map("n", "<leader>yp", "o@param []<Esc>", silent)
map("n", "<leader>aa", ":enew<CR>", silent)
map("n", "<leader>lr", function()
  local ok, trail = pcall(require, "mini.trailspace")
  if not ok then
    return
  end
  trail.trim()
  vim.cmd.nohlsearch()
end, { silent = true, desc = "Trim trailing whitespace" })
map("i", "<C-y><C-w>", "<Esc>:w<CR>", silent)
map("n", "<C-y><C-w>", ":w<CR>", silent)
map("n", "<leader>sp", ":sp<CR>", silent)
map("n", "<leader>vs", ":vs<CR>", silent)

local function buf_leader_bi(text)
  return function(event)
    map("n", "<leader>bi", text, { buffer = event.buf, silent = true })
  end
end

local ft_group = vim.api.nvim_create_augroup("dotfiles_basic_filetype", { clear = true })
vim.api.nvim_create_autocmd("FileType", {
  group = ft_group,
  pattern = "ruby",
  callback = buf_leader_bi("obinding.pry<Esc>"),
})
vim.api.nvim_create_autocmd("FileType", {
  group = ft_group,
  pattern = { "javascript", "typescript", "typescriptreact", "javascriptreact" },
  callback = buf_leader_bi("odebugger<Esc>"),
})
vim.api.nvim_create_autocmd("FileType", {
  group = ft_group,
  pattern = "eruby",
  callback = buf_leader_bi("o<% binding.pry %><Esc>"),
})

vim.cmd([[highlight ZenkakuSpace cterm=underline ctermfg=lightblue guibg=darkgray]])
vim.cmd([[match ZenkakuSpace /　/]])
vim.cmd([[highlight Comment ctermfg=DarkCyan]])

local cch = vim.api.nvim_create_augroup("dotfiles_cch", { clear = true })
vim.api.nvim_create_autocmd("WinLeave", {
  group = cch,
  callback = function()
    vim.opt_local.cursorline = false
  end,
})
vim.api.nvim_create_autocmd({ "WinEnter", "BufRead" }, {
  group = cch,
  callback = function()
    vim.opt_local.cursorline = true
  end,
})


vim.api.nvim_create_autocmd({ "BufRead", "BufNewFile" }, { pattern = "db/Schemafile", command = "set filetype=ruby", })
vim.api.nvim_create_autocmd({ "BufRead", "BufNewFile" }, { pattern = "*.sql.erb", command = "set filetype=sql", })
vim.api.nvim_create_autocmd({ "BufRead", "BufNewFile" }, { pattern = "*.Schemafile", command = "set filetype=ruby", })
vim.api.nvim_create_autocmd({ "BufRead", "BufNewFile" }, { pattern = "*.yml", command = "set filetype=yaml", })


-- 折り畳みの設定
vim.opt.foldmethod = "expr"
vim.opt.foldlevel = 100
vim.opt.foldexpr = "nvim_treesitter#foldexpr()"
function Foldtext()
  local line = vim.fn.getline(vim.v.foldstart)
  local count = vim.v.foldend - vim.v.foldstart + 1
  return string.format("%s (%d lines folded)", line, count)
end
vim.opt.foldtext = "v:lua.Foldtext()"
vim.opt.fillchars = { fold = " " } -- 折りたたんだ際のあまりの部分をスペースにする
vim.keymap.set("n", "<Tab>", "zo")
vim.keymap.set("n", "<S-Tab>", "zc")
vim.keymap.set("n", "<Leader><Tab>", "zR")
vim.keymap.set("n", "<Leader><S-Tab>", "zM")



-- カーソル位置を記憶して復元する設定
vim.api.nvim_create_autocmd('BufReadPost', {
  pattern = '*',
  callback = function()
    local mark = vim.api.nvim_buf_get_mark(0, '"')
    local lcount = vim.api.nvim_buf_line_count(0)
    if mark[1] > 0 and mark[1] <= lcount then
      vim.api.nvim_win_set_cursor(0, mark)
    end
  end,
})

-- :grep 実行後に QuickFix ウィンドウを自動的に開く
vim.api.nvim_create_autocmd('QuickFixCmdPost', {
  pattern = '*grep*',
  callback = function()
    if not vim.tbl_isempty(vim.fn.getqflist()) then
      vim.cmd('cwindow')
    end
  end,
})
