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
vim.g.mapleader = "\\"
vim.g.maplocalleader = "\\"

-- グローバル変数の設定
vim.g.omni_sql_no_default_maps = 1  -- omni_sqlのデフォルトマッピングを無効化
-- マウスを無効にする
vim.opt.mouse = ""

-- オプション設定
vim.opt.backup = false        -- バックアップファイルを作成しない
vim.opt.swapfile = false      -- スワップファイルを作成しない

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
  ui = false,
  { "morhetz/gruvbox",
    config = function()
      vim.cmd("colorscheme gruvbox")
    end,
  },
  { "tpope/vim-rails" },
  { "lukelbd/vim-toggle",
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
    config = function()
      vim.g.terraform_align = 1
      vim.g.terraform_fold_sections = 1
      vim.g.terraform_fmt_on_save = 1
    end
  },
  { "fatih/vim-go",
    build = ":GoUpdateBinaries",
    config = function()
      vim.g.go_null_module_warning = 0
    end
  },
  { "nvim-lua/popup.nvim" },
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
      keymap("n", "<leader>cf", ':let @+ = expand("%:~:.")<CR>:echo "\\"".expand("%:~:.")."をコピーしました\\""<CR>', opts)
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
      keymap("n", "<C-j>", "<Plug>(coc-definition)", opts)
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

      vim.keymap.set('n', '<leader>fn', function()
        telescope.extensions.notify.notify()
      end)

      local builtin = require("telescope.builtin")
      vim.keymap.set("n", "<leader>fd", "<Cmd>CocDiagnostics<CR>")
      vim.keymap.set("n", "<leader>ff", "<cmd>Telescope find_files<CR>", { silent = true })
      vim.keymap.set("n", "<leader>fg", "<cmd>Telescope live_grep<CR>", { silent = true })
      vim.keymap.set("n", "<leader>fb", "<cmd>Telescope buffers<CR>", { silent = true })
    end,
  },
  { "rbtnn/vim-ambiwidth" },
  { "romgrk/barbar.nvim",
    config = function()
      require("barbar").setup({ auto_hide = false })
    end,
  },
  {
    "nvim-tree/nvim-tree.lua",
    lazy = false,
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

      local opts = { silent = false }
      -- <C-e> マッピング
      vim.api.nvim_set_keymap("n", "<C-e>", ":NvimTreeToggle<CR>", opts)
      vim.api.nvim_set_keymap("v", "<C-e>", "<Esc>:NvimTreeToggle<CR>", opts)
      vim.cmd('omap <silent> <C-e> :NvimTreeToggle<CR>')
      vim.api.nvim_set_keymap("i", "<C-e>", "<Esc>:NvimTreeToggle<CR>", opts)
      vim.api.nvim_set_keymap("c", "<C-e>", "<C-u>:NvimTreeToggle<CR>", opts)
      -- <leader>nt, <leader>nf マッピング
      vim.api.nvim_set_keymap("n", "<leader>nt", ":NvimTreeToggle<CR>", { silent = true, noremap = true })
      vim.api.nvim_set_keymap("n", "<leader>nf", ":NvimTreeFindFile!<CR>", { noremap = true, silent = true })
    end,
  },
  { "nvim-tree/nvim-web-devicons", lazy = false },
  { "lukas-reineke/indent-blankline.nvim",
    config = function()
      require("ibl").setup()
    end
  },
  { "rcarriga/nvim-notify",
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
  { "APZelos/blamer.nvim",
    config = function()
      vim.g.blamer_enabled = 0
      vim.g.blamer_date_format = "%Y/%m/%d"
      vim.g.blamer_show_in_insert_modes = 0
      vim.g.blamer_template = "<commit-short> <committer-time> <committer>:  <summary>"
      vim.api.nvim_set_keymap("n", "<leader>gb", "<cmd>BlamerToggle<CR>", { noremap = true, silent = true })
      vim.api.nvim_set_keymap("v", "<leader>gb", "<cmd>BlamerToggle<CR>", { noremap = true, silent = true })
    end,
  },
  { "petertriho/nvim-scrollbar",
    config = function()
      require("scrollbar").setup({ handlers = { cursor = false } })
    end,
  },
  { "psliwka/vim-smoothie" },
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
  { 'echasnovski/mini.nvim', version = '*',
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

    end,
  },
  { "ntpeters/vim-better-whitespace" },
  { "MeanderingProgrammer/render-markdown.nvim" },
}, {
  install = { colorscheme = { "habamax" } },
  checker = { enabled = true },
})

vim.cmd("source /Users/koji/dotfiles/basic.vim")


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
