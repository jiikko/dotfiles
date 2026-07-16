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

-- config_dir (dotfiles repo ルート) は vendor プラグインの dir= 解決にのみ使う。
-- rtp へは載せない: repo ルートに nvim が走査する dir (lua/ftplugin/plugin/colors 等) は
-- 一つも無く登録が不活性だった (rtp の実体は nvim_dir=repo/nvim/ のみ。2026-07-16 に除去)。
local config_dir = vim.fn.fnamemodify(vim.fn.resolve(vim.env.MYVIMRC or ""), ":p:h")
local nvim_dir = config_dir ~= "" and (config_dir .. "/nvim") or ""
if nvim_dir ~= "" and vim.fn.isdirectory(nvim_dir) == 1 then
  vim.opt.rtp:prepend(nvim_dir)
end

-- Make sure to setup `mapleader` and `maplocalleader` before
-- loading lazy.nvim so that mappings are correct.
-- This is also a good place to setup other settings (vim.opt)
vim.g.mapleader = " "
vim.g.maplocalleader = " "

-- ============================================================================
-- [WORKAROUND] truecolor 非対応端末 (macOS 標準 Terminal.app 等) でもハイライトを出す
-- ----------------------------------------------------------------------------
-- 問題: Terminal.app は truecolor (24bit色) 非対応 (256色まで)。本構成の colorscheme
--   gruvbox.nvim は GUI色しか持たない (cterm 色ゼロ) ため、truecolor の無い端末では
--   termguicolors=on でも off でもシンタックスが出ない (on=24bit が化ける / off=256色が無い)。
-- さらに tmux 内では truecolor 対応かの「自動判定」が当てにならない:
--   _tmux.conf が ",tmux*:RGB" で RGB を無条件広告し、COLORTERM は伝わらず、TERM_PROGRAM も
--   "tmux" になる。よって env からの自動検出だけでは非対応端末を見分けられない。
-- 対策: truecolor 非対応と判定したら、cterm(256色)を完備する nvim 同梱 colorscheme
--   "retrobox" (gruvbox 風) へ切り替え、termguicolors=off にして 256色でハイライトを出す。
-- 判定の優先順 (上ほど優先):
--   1. SUPPORT_TRUECOLOR=false/0 — 非対応マシンで ~/.zshenv 等に export する。シェルが
--      export するため tmux 内でも nvim へ確実に届く「唯一信頼できる」信号。
--   2. SUPPORT_TRUECOLOR=true/1  — 強制 truecolor。
--   3. COLORTERM=truecolor/24bit → 対応 / TERM_PROGRAM=Apple_Terminal → 非対応 (best-effort)。
--   4. 不明 → 対応とみなす (gruvbox 維持。新しめの端末を壊さない安全側)。
-- → 非対応マシンでは ~/.zshenv に `export SUPPORT_TRUECOLOR=false` の 1 行が必要。
-- ============================================================================
-- 注: 実際の colorscheme 分岐は下の gruvbox spec の config で行う (truecolor_supported を参照)。
-- ColorScheme autocmd 内での colorscheme 切替は再入ガードで効かないため、適用時に直接分岐する。
local function dotfiles_truecolor_supported()
  local flag = vim.env.SUPPORT_TRUECOLOR
  if flag == "false" or flag == "0" then return false end
  if flag == "true" or flag == "1" then return true end
  if vim.env.COLORTERM == "truecolor" or vim.env.COLORTERM == "24bit" then return true end
  if vim.env.TERM_PROGRAM == "Apple_Terminal" then return false end
  return true
end

require("dotfiles.basic").setup()

-- gruvbox 色の hex↔cterm ペアは palette が唯一の出典 (下の plugin spec 群の closure が参照)。
local pal = require("dotfiles.palette")

-- ============================================================================
-- [SELF-HEAL] vim.loader (luac バイトコードキャッシュ) の stale 自己修復
-- ----------------------------------------------------------------------------
-- プラグイン構成を大きく変えた直後の初回起動で、vim.loader のディスクキャッシュ
-- (~/.cache/nvim/luac) が stale になり、実在するモジュールを "module ... not found" と
-- 誤判定することがある (coc→ネイティブ LSP 移行時に nvim-treesitter.configs で実際に発生)。
-- 対策: require が "not found" で失敗し、かつ当該 .lua が rtp に実在するときだけ、luac を
-- 消して loader をリセットし一度だけ retry する (同一起動で自己修復)。
-- 真に存在しないモジュール・構文エラーは握り潰さずそのまま投げる (誤検出でエラーを隠さない)。
local function require_resilient(mod)
  local ok, m = pcall(require, mod)
  if ok then return m end
  local base = mod:gsub("%.", "/")
  local on_rtp = #vim.api.nvim_get_runtime_file("lua/" .. base .. ".lua", false) > 0
    or #vim.api.nvim_get_runtime_file("lua/" .. base .. "/init.lua", false) > 0
  if type(m) == "string" and m:find("not found", 1, true) and on_rtp then
    vim.fn.delete(vim.fn.stdpath("cache") .. "/luac", "rf")
    if vim.loader and vim.loader.reset then vim.loader.reset() end
    package.loaded[mod] = nil
    vim.schedule(function()
      vim.notify(
        ("stale module cache を検出: %s。~/.cache/nvim/luac を消去して復旧しました。"):format(mod),
        vim.log.levels.WARN
      )
    end)
    return require(mod)
  end
  error(m)
end

-- Setup lazy.nvim
require("lazy").setup({
  { "ellisonleao/gruvbox.nvim",
    priority = 1000,
    config = function()
      if dotfiles_truecolor_supported() then
        vim.cmd("colorscheme gruvbox")
      else
        -- truecolor 非対応端末 (上の WORKAROUND 参照): gruvbox は cterm 色を持たず 256色端末で
        -- 無色になるため、cterm を完備する nvim 同梱の retrobox (gruvbox 風) へ差し替える。
        vim.opt.termguicolors = false
        vim.cmd("colorscheme retrobox")
      end
      -- 選択範囲は Kraft (暖ベージュ) で強調 (hl.set = ColorScheme 再適用 + cterm 併記規律)。
      -- 長時間注視する領域なので、現在地の Coral (accent.current_accent =
      -- bufferline 選択タブ / tmux island) より一段落ち着いた色に意図的に分けている
      -- (旧ローズ #d3869b → Kraft へ。オレンジ基調テーマ 2026-07-16)。
      -- 分岐の外に置き truecolor (gruvbox) / 256色 (retrobox) の両環境で効かせる
      -- (以前は truecolor 分岐内のみで、256色主環境は retrobox 既定の青灰 109 のままだった。
      --  2026-07-16 是正。colorscheme 適用後に呼ぶこと = ColorScheme の全クリアより後に乗せる)
      require("dotfiles.hl").set("Visual", { bg = pal.accent.kraft.hex, ctermbg = pal.accent.kraft.cterm })
    end,
  },
  -- toggle.nvim は repo 内に vendor 済み (vendor/nvim-plugins/toggle.nvim、VENDOR.md 参照)。
  -- GitHub 取得でなくローカルコピーを dir で読む (トグル語彙を自分で保守するため)。
  { dir = config_dir .. "/vendor/nvim-plugins/toggle.nvim", name = "toggle.nvim",
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
      -- 既知の制限 (意図的に現状維持、2026-07-12): "protected" が on/off 両リストに載っており、
      -- toggle_cursor は off 側優先のため protected→private には到達しない (public↔protected の
      -- 往復に収束。private→protected は片道)。実用上この往復で足りるため対応しない。
      -- 3 値サイクルが必要になったら「off=遷移元 / on=遷移先」の 3 ペアへの組み替えで表現可能。
    end,
  },
  -- Terraform: hashivim/vim-terraform (Vimscript) を廃し、ネイティブ構成へ置換 (2026-07)。
  --   - ft 検出: nvim 標準 (.tf/.tfvars→terraform, .hcl→hcl)。専用プラグイン不要
  --   - 構文/fold: nvim-treesitter の terraform/hcl parser (下の ensure_installed)
  --   - 補完/診断/hover: terraform-ls (lsp.lua の ensure_installed=terraformls)
  --   - 整形 (旧 terraform_fmt_on_save + align 相当): conform の terraform_fmt を terraform ft の
  --     保存時のみ発火 (下の conform format_on_save)。terraform fmt が align も担う
  -- Go: fatih/vim-go (Vimscript 19k 行) を廃し、ネイティブ構成へ置換 (2026-07)。
  -- 削除前に実挙動を headless で棚卸しして parity を確保した (要点のみ):
  --   - 構文/ハイライト: nvim-treesitter の go/gomod/gosum parser (下の ensure_installed)。
  --     vim-go の syntax は元から非稼働 (&syntax='' で treesitter が唯一の highlighter) だった
  --   - 定義/実装/参照ジャンプ・hover: 既に native LSP + gopls (lsp.lua)。vim-go 側は
  --     go_gopls_enabled=0 / go_def_mapping_enabled=0 で無効化済みだった。K は 0.11 の
  --     native LSP 既定 (K=hover) が引き継ぐ
  --   - 保存時 gofmt+goimports (旧 go_fmt_autosave/go_imports_autosave=既定1): conform の
  --     formatters_by_ft.go=goimports を go の保存時に発火 (下の conform)。goimports は
  --     -srcdir 付きで module-aware
  --   - 関数ジャンプ ]] [[ / テキストオブジェクト af if ac ic: treesitter-textobjects へ
  --     (nvim/ftplugin/go.lua で Go 限定 buffer-local に再現。vim-go の scope を踏襲)
  --   - GoDecls (<leader>gd/gD): 元から fzf/ctrlp 未導入でエラー = 非稼働だった。
  --     telescope の lsp_document/workspace_symbols で置換 (nvim/ftplugin/go.lua)
  --   - :Go* コマンド群 (:GoTest 等) は未使用のため引き継がない (必要なら go.nvim/nvim-dap-go)
  { "andymass/vim-matchup",
    -- event を付けず eager でロードする (意図的): 作者が README で event 遅延ロードを
    -- 非推奨と明言している (起動時ロードは元々最小限で、遅延は不具合時の切り分け対象に
    -- なるだけ)。BufReadPre/BufNewFile 規約 (gitsigns 等) の対象外。
    config = function()
      vim.g.loaded_matchit = 1
      vim.g.matchup_matchparen_stopline = 400
      vim.g.matchup_matchparen_deferred = 1
      vim.g.matchup_matchparen_offscreen = { method = "popup" }
      vim.g.matchup_surround_enabled = 1
    end,
  },
  { "nvim-treesitter/nvim-treesitter",
    -- archived な master ブランチに固定する。master は凍結 = parser と query が固定の
    -- matched set なので、main (rolling) で起きた parser/query バージョン不整合
    -- (Invalid node type ...) や checker 自動更新による drift を避けられる。
    -- classic API (configs.setup) は highlight 用の autocmd を内部で張り自動有効化する
    -- ため、手動の vim.treesitter.start autocmd は不要。
    -- branch を変えたら :Lazy update nvim-treesitter + :TSUpdate で parser を再同期すること。
    -- event は付けず eager のまま (意図的): master README が「This plugin does not support
    -- lazy-loading」と明言しており、BufReadPre/BufNewFile 規約の対象外。dependencies の
    -- textobjects と下の endwise も configs.setup 実行時に rtp に載っている必要があり同様。
    -- [2026-07-10] 上流 repo 自体が archive (read-only) 化済み。上流 README いわく
    -- master は「Nvim 0.11 の後方互換のため locked のまま残す」、main rewrite は 0.12+ 必須。
    -- → nvim 0.11 で使ううちはこのピンが正解。Neovim 0.12+ へ上げる時に main 系 rewrite へ
    -- textobjects とセットで移行を再評価 (issues/010-research-nvim-plugin-rewrite-candidates-2026-07-10.md 追記節)。
    branch = "master",
    build = ":TSUpdate",
    -- textobjects も master に固定する (nvim-treesitter を master 凍結しているため。default
    -- ブランチ任せだと main を引いて require パス/統合が食い違い無言で壊れる)。standalone spec
    -- でなく dependencies に入れて configs.setup 実行時に必ず rtp に載っている状態にする。
    dependencies = { { "nvim-treesitter/nvim-treesitter-textobjects", branch = "master" } },
    config = function()
      -- stale luac cache で "nvim-treesitter.configs not found" になっても自己修復する
      require_resilient("nvim-treesitter.configs").setup({
        -- gomod/gosum は go.mod/go.sum のハイライト用 (vim-go 廃止で syntax 供給元を treesitter に
        -- 一本化。現状 parser は入っているが ensure_installed 未記載で fresh install 非再現だった)。
        -- DOTFILES_TS_SKIP_ENSURE=1 は CI 用の抜け穴 (.github/workflows/tests.yml が設定する):
        -- parser 不在の環境では ensure_installed が毎起動 31 個の非同期 DL+コンパイルジョブを
        -- 撒き、headless テストの +qall! が中途 kill する不安定要因になる (2026-07-10 の CI
        -- flake 対策)。CI のテストは parser 非依存に設計されている (go の挙動 assert は parser
        -- 不在なら skip) ため、CI ではインストール自体を止める。人間の fresh install では
        -- 未設定 = 従来どおり自動導入される。
        ensure_installed = (vim.env.DOTFILES_TS_SKIP_ENSURE == "1") and {} or { "diff", "awk", "bash", "c", "cmake", "css", "dockerfile", "elixir", "go", "gomod", "gosum", "graphql", "hcl", "html", "http", "javascript", "json", "lua", "make", "markdown", "markdown_inline", "python", "ruby", "rust", "scala", "scss", "sql", "terraform", "typescript", "vim", "yaml" },
        auto_install = false,
        highlight = { enable = true },
        endwise = { enable = true },
        -- textobjects の keymap はここで global に張らず (]] [[ は組み込み section motion を
        -- 上書きするため)、モジュール有効化と挙動オプションだけ設定する。実際の keymap は
        -- nvim/ftplugin/go.lua が Go 限定 buffer-local で張る (vim-go の scope を踏襲)。
        textobjects = {
          -- @function.inner を linewise(V) にする (旧 vim-go inner 関数は linewise で、dif が
          -- body を行ごと消した)。select は keymap を持たない (実 keymap は ftplugin/go.lua) ため
          -- この selection_modes は af/if を張る Go でのみ実質作用する。af(outer) は vim-go 同様
          -- charwise のまま。
          select = { enable = true, lookahead = true, selection_modes = { ["@function.inner"] = "V" } },
          move = { enable = true, set_jumps = true },
        },
      })
    end,
  },
  { "RRethy/nvim-treesitter-endwise",
    dependencies = { "nvim-treesitter/nvim-treesitter" }
  },
  { "folke/which-key.nvim", event = "VeryLazy" },
  { "nvim-lualine/lualine.nvim",
    dependencies = { "nvim-tree/nvim-web-devicons" },
    config = function()
      -- git root 相対でファイルパスを表示する。旧実装は vim-fugitive 由来の vim.b.git_dir を
      -- 参照していたが本構成に設定主体が無く、常に cwd 相対へフォールバックする dead branch
      -- だった (cwd ≠ git root のとき意図と食い違う)。vim.fs.root で自前解決する。
      local function relative_path_from_git_root()
        local file = vim.api.nvim_buf_get_name(0)
        local root = file ~= "" and vim.fs.root(0, ".git")
        if not root then
          return vim.fn.expand("%")
        end
        return file:gsub("^" .. vim.pesc(root) .. "/", "")
      end

      require("lualine").setup({
        options = {
          theme = "auto",
          section_separators = "",
          component_separators = "",
          icons_enabled = true,
        },
        sections = {
          lualine_a = { "mode" },
          lualine_b = { "diagnostics", "branch" },
          lualine_c = { { relative_path_from_git_root } },
          lualine_x = { "encoding", "fileformat", "filetype" },
          lualine_y = { "progress" },
          lualine_z = { "location" },
        },
      })
    end,
  },
  -- ============================================================================
  -- LSP スタック (2026-07 に coc.nvim から移行)
  --   mason        : language server / formatter / linter のバイナリ管理
  --   nvim-lspconfig: 各サーバの既定設定 (nvim 0.11 の vim.lsp.config に載る)
  --   blink.cmp    : 補完 (プリビルドバイナリ。cargo 不要 → version="*" 固定)
  --   conform.nvim : 整形 (:Format / <leader>f)   nvim-lint : sh の shellcheck
  -- キー割り当て・診断・on_attach の本体は nvim/lua/dotfiles/lsp.lua。
  --
  -- mason-lspconfig は廃止 (2026-07-11): setup() が mason レジストリ走査 + installed 検出で
  -- 初回 BufReadPre に ~13ms かかっていたが、買っていたのは実質「installed サーバへの
  -- vim.lsp.enable()」だけで、サーバ一覧は lsp.server_packages として自前で持っている。
  -- enable はここで直接呼び、バイナリ導入は mason-tool-installer (VeryLazy) に一本化した。
  -- トレードオフ: 「mason で新サーバを入れたら自動で enable」は失われる。新サーバは
  -- lsp.server_packages への 1 行追加で enable+導入の両方に効く。
  -- ============================================================================
  { "mason-org/mason.nvim", cmd = "Mason", opts = {} },
  { "neovim/nvim-lspconfig",
    event = { "BufReadPre", "BufNewFile" },
    dependencies = { "saghen/blink.cmp" },
    config = function()
      local lsp = require("dotfiles.lsp")
      -- mason bin を PATH に通す (mason.setup と同じ先頭 prepend)。mason 本体は cmd=Mason の
      -- 遅延ロードで、初回ファイルオープン時点では未ロードのため自前で通す必要がある。
      local mason_bin = vim.fn.stdpath("data") .. "/mason/bin"
      if not string.find(vim.env.PATH or "", mason_bin, 1, true) then
        vim.env.PATH = mason_bin .. ":" .. (vim.env.PATH or "")
      end
      -- blink の capabilities を先に確定させてから (契約: lsp.setup 内で vim.lsp.config("*"))、
      -- サーバを enable する (lsp/*.lua の既定設定は nvim-lspconfig が rtp 提供)。
      local ok, blink = pcall(require, "blink.cmp")
      lsp.setup(ok and blink.get_lsp_capabilities() or nil)
      -- バイナリが実在するサーバだけ enable する (~4ms)。fresh install ではまだ mason が
      -- 入れていないサーバがあり、無条件 enable だと spawn 失敗の通知が出るうえ、導入完了後も
      -- 再起動まで attach しない (codex 指摘 P2)。導入完了イベントで再 enable して同一
      -- セッション内でも拾う。
      local function enable_available()
        local ready = {}
        for name in pairs(lsp.server_packages) do
          local cmd = vim.lsp.config[name] and vim.lsp.config[name].cmd
          -- cmd が関数のサーバは実在判定できないため enable に含める (現行の一覧は全て table)
          if type(cmd) ~= "table" or vim.fn.executable(cmd[1]) == 1 then
            table.insert(ready, name)
          end
        end
        if #ready > 0 then vim.lsp.enable(ready) end
      end
      enable_available()
      vim.api.nvim_create_autocmd("User", {
        pattern = "MasonToolsUpdateCompleted",
        callback = enable_available,
      })
    end,
  },
  { "WhoIsSethDaniel/mason-tool-installer.nvim",
    event = "VeryLazy",
    dependencies = { "mason-org/mason.nvim" },
    config = function()
      -- formatter / linter に加え、LSP サーバ実体の導入もここに一本化
      -- (mason-lspconfig 廃止に伴い ensure_installed の受け皿がここになった)。
      local lsp = require("dotfiles.lsp")
      require("mason-tool-installer").setup({
        -- goimports は vim-go 廃止に伴う Go 保存時整形 (conform formatters_by_ft.go) の実体。
        ensure_installed = vim.list_extend(
          { "prettierd", "shfmt", "shellcheck", "goimports" },
          vim.tbl_values(lsp.server_packages)
        ),
      })
    end,
  },
  { "saghen/blink.cmp",
    version = "*", -- プリビルドバイナリを使う。main/build=cargo は cargo 非搭載機で起動時に死ぬ
    event = { "InsertEnter", "CmdlineEnter" },
    opts = {
      keymap = { preset = "enter" }, -- <CR> で確定 (coc#pum#confirm の踏襲。未選択時は改行にフォールバック)
      appearance = { nerd_font_variant = "mono" },
      sources = { default = { "lsp", "path", "snippets", "buffer" } },
      signature = { enabled = true },
      -- 候補選択で説明/型を自動表示 (blink 既定は auto_show=false)
      completion = { documentation = { auto_show = true, auto_show_delay_ms = 500 } },
      -- プリビルドバイナリが無い環境では Lua 実装へ自動フォールバック
      fuzzy = { implementation = "prefer_rust_with_warning" },
    },
  },
  { "stevearc/conform.nvim",
    -- BufWritePre で load する: format_on_save は conform.setup 内で BufWritePre autocmd を
    -- 張るため、conform が「最初の保存より前」に load されていないと初回保存で整形されない。
    -- cmd/keys だけの lazy だと :Format / <leader>f を一度も押さないセッションで go/terraform の
    -- 保存時整形が無言で発火しなかった (実測で判明)。lazy は load 後に発火元イベントを再送する
    -- ので、その回の保存から効く。
    event = { "BufWritePre" },
    cmd = "Format",
    keys = {
      { "<leader>f", function() require("conform").format({ async = true, lsp_format = "fallback" }) end, desc = "Format buffer" },
    },
    config = function()
      local conform = require("conform")
      conform.setup({
        -- 列挙が無い ft (ruby 等) は lsp_format="fallback" でサーバ整形に委ねる
        -- (ruby は lsp.lua の solargraph.formatting=true 前提)。
        formatters_by_ft = {
          javascript = { "prettierd" },
          javascriptreact = { "prettierd" },
          typescript = { "prettierd" },
          typescriptreact = { "prettierd" },
          json = { "prettierd" },
          yaml = { "prettierd" },
          html = { "prettierd" },
          css = { "prettierd" },
          scss = { "prettierd" },
          markdown = { "prettierd" },
          sh = { "shfmt" },
          terraform = { "terraform_fmt" }, -- vim-terraform 置換: terraform fmt (align も担う)
          hcl = { "terraform_fmt" },
          go = { "goimports" }, -- vim-go 置換: 旧 go_fmt_autosave+go_imports_autosave 相当 (gofmt整形+import増減)
        },
        -- terraform 系と go だけ保存時に整形する (terraform=旧 vim-terraform g:terraform_fmt_on_save=1、
        -- go=旧 vim-go go_fmt_autosave/go_imports_autosave を踏襲)。他の ft は従来どおり
        -- <leader>f / :Format の手動整形のまま (nil を返すと保存時整形なし)。
        format_on_save = function(bufnr)
          local ft = vim.bo[bufnr].filetype
          if ft == "go" then
            -- 初回 goimports は module-aware (-srcdir) で cold cache 時 1s を超えうるため長めに。
            return { timeout_ms = 3000 }
          elseif ft == "terraform" or ft == "hcl" then
            return { timeout_ms = 1000 }
          end
        end,
        formatters = {
          -- coc-settings.json の diagnostic-languageserver.formatters.shfmt を踏襲
          shfmt = { prepend_args = { "-i", "2", "-bn", "-ci", "-sr" } },
        },
      })
      vim.api.nvim_create_user_command("Format", function()
        conform.format({ async = true, lsp_format = "fallback" })
      end, {})
    end,
  },
  { "mfussenegger/nvim-lint",
    event = { "BufReadPost", "BufNewFile" },
    config = function()
      -- coc-diagnostic が sh に対して行っていた shellcheck を踏襲 (それ以外の言語は LSP 診断)
      require("lint").linters_by_ft = { sh = { "shellcheck" }, bash = { "shellcheck" } }
      vim.api.nvim_create_autocmd({ "BufWritePost", "BufReadPost", "InsertLeave" }, {
        group = vim.api.nvim_create_augroup("dotfiles_nvim_lint", { clear = true }),
        callback = function() require("lint").try_lint() end,
      })
    end,
  },
  { "nvim-telescope/telescope.nvim",
    cmd = "Telescope",
    keys = {
      { "<leader>ff", function() require("telescope.builtin").find_files() end, desc = "Find files" },
      { "<leader>fg", function() require("telescope.builtin").live_grep() end, desc = "Live grep" },
      { "<leader>fb", function() require("telescope.builtin").buffers() end, desc = "List buffers" },
      { "<leader>fn", function() require("telescope").extensions.notify.notify() end, desc = "Notify history" },
      -- 診断一覧 (coc: <leader>a=CocList diagnostics / <leader>fd=CocDiagnostics の踏襲)。
      -- lazy load の入口なので config 内ではなく keys に置く (config は初回ロードまで走らない)。
      { "<leader>a", function() require("telescope.builtin").diagnostics() end, desc = "Diagnostics (all)" },
      { "<leader>fd", function() require("telescope.builtin").diagnostics({ bufnr = 0 }) end, desc = "Diagnostics (buffer)" },
    },
    dependencies = {
      "nvim-lua/plenary.nvim",
      "nvim-telescope/telescope-ui-select.nvim",
      -- config 末尾の load_extension("notify") の供給元。現状は top-level spec (start plugin)
      -- として必ずロード済みだが、そちらを外す改修をしたときに壊れないよう依存を局所化しておく
      "rcarriga/nvim-notify",
    },
    config = function()
      local telescope = require("telescope")
      local actions = require("telescope.actions")
      telescope.setup({
        defaults = {
          sorting_strategy = "ascending",
          layout_strategy = "vertical",
          layout_config = { height = 0.9 },
          -- Lua パターンで評価される (telescope 内部で string.find(file, pattern))。glob ではない。
          -- `-` は量指定子・`.` は任意 1 文字なので %- %. でエスケープしないと文字どおりに
          -- マッチしない (未エスケープの package-lock.json が一切除外されない実バグがあった)。
          -- node_modules は cwd 直下 (^) と monorepo のネスト (/) の両方を除外する。
          file_ignore_patterns = {
            "^%.git/", "^node_modules/", "/node_modules/",
            "package%-lock%.json", "yarn%.lock", "yarn%-error%.log",
          },
          border = true,
          prompt_prefix = "🔍 ",
          -- ファイル名を先頭・ディレクトリを淡色で後置 (find_files / live_grep / lsp_* 全てに効く)
          path_display = { "filename_first" },
          mappings = {
            -- 既定は insert の <esc> が「閉じる」でなく picker 内 normal へ移行するだけで
            -- 「閉じ方が分からない」状態になる。insert の <esc> を即クローズに割り当てる。
            -- (<C-c> でも閉じられるのは既定のまま)
            i = { ["<esc>"] = actions.close },
          },
        },
        extensions = {
          ["ui-select"] = {
            require("telescope.themes").get_dropdown({}),
          },
        },
      })
      telescope.load_extension("ui-select")
      telescope.load_extension("notify")
    end,
  },
  -- ambiwidth.nvim (旧 rbtnn/vim-ambiwidth を Lua 移植) は repo 内に vendor 済み
  -- (vendor/nvim-plugins/ambiwidth.nvim、VENDOR.md 参照)。
  -- 起動時に setcellwidths を張るため遅延トリガは付けず eager ロード。
  { dir = config_dir .. "/vendor/nvim-plugins/ambiwidth.nvim", name = "ambiwidth.nvim" },
  { "akinsho/bufferline.nvim",
    -- BufAdd は起動処理中の最初のバッファでは発火しない (:h BufAdd) ため、単一バッファの
    -- セッションでは一切ロードされず always_show_bufferline と gt/gT 等の keymap が死んでいた。
    -- UI 常駐プラグインなので VeryLazy (起動完了直後) でロードする。
    event = "VeryLazy",
    dependencies = { "nvim-tree/nvim-web-devicons" },
    config = function()
      local ok, bufferline = pcall(require, "bufferline")
      if not ok then
        return
      end

      bufferline.setup({
        options = {
          show_close_icon = false,
          show_buffer_close_icons = false,
          always_show_bufferline = true,
          -- 選択タブの左端にインジケータバーを出す (非選択との差を強調)
          indicator = { style = "icon", icon = "▎" },
          -- タブ境界は細い縦線 (slant の三角 glyph は見た目がうるさかったため thin に変更)
          separator_style = "thin",
          -- ordinal 番号を表示 (<leader>1..9 のジャンプ先が見た目から分かる)
          numbers = "ordinal",
          color_icons = true,
        },
        -- アクティブ/非アクティブの差を強く付ける。
        --   選択 = 明るい pink 地 × 黒の太字 + 橙のインジケータバー (最も目立たせる)
        --   非選択 = 暗い地に沈んだ灰字 (存在感を弱める。地色は fill と同じ dark0_hard =
        --   タブは地に溶け、文字色とセパレータ縦線だけで区切るフラットデザイン)
        -- gui(truecolor) と cterm(256色) の両方を指定する。この環境は ~/.zshenv の
        -- SUPPORT_TRUECOLOR=false で retrobox + termguicolors=off (256色) のため、
        -- gui 色だけでは一切効かず区別が付かなかった。hex↔cterm の組は pal (dotfiles/palette) が
        -- 唯一の出典。かつて手書き併記だった頃、同じ #1d2021 に ctermbg=237 (=dark1 の対応値) が
        -- 混在し「256色でだけ非選択タブが明るく浮く」drift があった (2026-07-16 に 234 へ統一)。
        highlights = {
          fill = { bg = pal.dark0_hard.hex, ctermbg = pal.dark0_hard.cterm },
          -- 非選択バッファ (最も沈める)
          background = { fg = pal.dark3.hex, bg = pal.dark0_hard.hex, ctermfg = pal.dark3.cterm, ctermbg = pal.dark0_hard.cterm },
          modified = { fg = pal.dark3.hex, bg = pal.dark0_hard.hex, ctermfg = pal.dark3.cterm, ctermbg = pal.dark0_hard.cterm },
          -- 別ウィンドウで可視だが非アクティブ (中間トーン)
          buffer_visible = { fg = pal.light4.hex, bg = pal.dark0_hard.hex, ctermfg = pal.light4.cterm, ctermbg = pal.dark0_hard.cterm },
          modified_visible = { fg = pal.light4.hex, bg = pal.dark0_hard.hex, ctermfg = pal.light4.cterm, ctermbg = pal.dark0_hard.cterm },
          -- アクティブ (tmux の current window 島と同じ Coral 地 + 黒の太字。
          -- 「いまここ」の色言語を tmux バーと統一。palette.accent.current_accent 参照。
          -- 旧ショッキングピンク → Coral へ: オレンジ基調テーマ 2026-07-16)
          buffer_selected = { fg = pal.dark0_hard.hex, bg = pal.accent.current_accent.hex, ctermfg = pal.dark0_hard.cterm, ctermbg = pal.accent.current_accent.cterm, bold = true, italic = false },
          modified_selected = { fg = pal.dark0_hard.hex, bg = pal.accent.current_accent.hex, ctermfg = pal.dark0_hard.cterm, ctermbg = pal.accent.current_accent.cterm, bold = true },
          indicator_selected = { fg = pal.light1.hex, bg = pal.accent.current_accent.hex, ctermfg = pal.light1.cterm, ctermbg = pal.accent.current_accent.cterm }, -- クリームのバー (旧橙208は蛍光橙地202と d=40 でほぼ不可視のため変更 2026-07-16)
          -- ordinal 番号 (タブ本体と同じ地色に合わせる)
          numbers = { fg = pal.dark3.hex, bg = pal.dark0_hard.hex, ctermfg = pal.dark3.cterm, ctermbg = pal.dark0_hard.cterm },
          numbers_visible = { fg = pal.light4.hex, bg = pal.dark0_hard.hex, ctermfg = pal.light4.cterm, ctermbg = pal.dark0_hard.cterm },
          numbers_selected = { fg = pal.dark0_hard.hex, bg = pal.accent.current_accent.hex, ctermfg = pal.dark0_hard.cterm, ctermbg = pal.accent.current_accent.cterm, bold = true, italic = false },
          -- thin セパレータ: fg が縦線の色。地色と同系の沈んだ色 (dark1) にして境界だけ薄く見せる。
          separator = { fg = pal.dark1.hex, bg = pal.dark0_hard.hex, ctermfg = pal.dark1.cterm, ctermbg = pal.dark0_hard.cterm },
          separator_visible = { fg = pal.dark1.hex, bg = pal.dark0_hard.hex, ctermfg = pal.dark1.cterm, ctermbg = pal.dark0_hard.cterm },
          separator_selected = { fg = pal.dark1.hex, bg = pal.dark0_hard.hex, ctermfg = pal.dark1.cterm, ctermbg = pal.dark0_hard.cterm },
        },
      })

      local map = vim.keymap.set
      local silent = { silent = true }
      map({ "n", "v", "o" }, "<Right>", "<Cmd>BufferLineCycleNext<CR>", silent)
      map({ "n", "v", "o" }, "<Left>", "<Cmd>BufferLineCyclePrev<CR>", silent)
      map("n", "gt", "<Cmd>BufferLineCycleNext<CR>", silent)
      map("n", "gT", "<Cmd>BufferLineCyclePrev<CR>", silent)
      map("n", "<C-a><C-a>", "<Cmd>bdelete<CR>", silent)
      -- ordinal 番号 (numbers = "ordinal") へ直接ジャンプ
      for i = 1, 9 do
        map("n", "<leader>" .. i, ("<Cmd>BufferLineGoToBuffer %d<CR>"):format(i), silent)
      end
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
          -- float 有効時に効く寸法は open_win_config のみ (view.width/side は非 float 用で、
          -- ここでは dead になるため置かない。float をやめる時に改めて設定する)
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
        git = {
          -- 初回オープンの git status は同期実行でここまでブロックする (default 400ms)。
          -- timeout 超過時は git バッジなしで表示され、以降の更新は非同期で反映される
          timeout = 200,
        },
        on_attach = my_on_attach,
      })

    end,
  },
  { "lukas-reineke/indent-blankline.nvim",
    event = { "BufReadPost", "BufNewFile" },
    config = function()
      require("ibl").setup({ exclude = { filetypes = { "NvimTree", "help", "lazy" } } })
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
      local notify = require('notify')
      local stages_util = require("notify.stages.util")

      notify.setup({
        render = "minimal",
        timeout = 3000,
        max_width = 80,
        max_height = 10,
        background_colour = "#000000",
        stages = {
          function(state)
            local next_row = stages_util.available_slot(state.open_windows, state.message.height + 2, stages_util.DIRECTION.BOTTOM_UP)
            if not next_row then return nil end
            return {
              relative = "editor",
              anchor = "SW",
              width = state.message.width,
              height = state.message.height,
              col = 0,
              row = next_row,
              border = "rounded",
              style = "minimal",
              focusable = false,
            }
          end,
          function() return { time = true } end,
        },
      })

      local original_notify = notify
      local custom_notify = function(msg, log_level, opts)
        -- Invalid 'priority' エラーを握りつぶす
        if msg and type(msg) == "string" and msg:match("Invalid 'priority'") then return end
        original_notify(msg, log_level, opts)
      end

      vim.notify = custom_notify
    end
  },
  {
    "folke/noice.nvim",
    event = "VeryLazy",
    dependencies = {
      "MunifTanjim/nui.nvim",
      "rcarriga/nvim-notify",
    },
    opts = {
      lsp = {
        override = {
          ["vim.lsp.util.convert_input_to_markdown_lines"] = true,
          ["vim.lsp.util.stylize_markdown"] = true,
        },
      },
      presets = {
        bottom_search = false,         -- 検索もフロート表示（trueなら画面下部）
        command_palette = true,        -- コマンドパレットスタイル
        long_message_to_split = true,  -- 長いメッセージは分割表示
        inc_rename = false,
        lsp_doc_border = false,
      },
    },
  },
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

      -- hl.set = ColorScheme 再適用 + cterm 併記 (256色環境) の規律 (dotfiles/hl.lua 参照)
      require("dotfiles.hl").set("GitSignsCurrentLineBlame", {
        fg = pal.dark0_hard.hex,
        bg = pal.bright_yellow.hex,
        ctermfg = pal.dark0_hard.cterm,
        ctermbg = pal.bright_yellow.cterm,
        bold = true,
        italic = false,
      })

      local map = vim.keymap.set
      local opts = { silent = true, desc = "Toggle git blame" }
      map({ "n", "v" }, "<leader>gb", gitsigns.toggle_current_line_blame, opts)
    end,
  },
  { "dstein64/nvim-scrollview",
    -- BufReadPost は未存在ファイルでは発火しない。新規ファイル起点のセッションでも
    -- 有効になるよう BufNewFile を併記 (gitsigns/ibl/nvim-lint と同じ規律)
    event = { "BufReadPost", "BufNewFile" },
    config = function()
      require("scrollview").setup()
    end,
  },
  { 'echasnovski/mini.trailspace', version = '*', event = "VeryLazy",
    config = function()
      require("mini.trailspace").setup()
      -- hl.set = ColorScheme 再適用 + cterm 併記 (256色環境) の規律 (dotfiles/hl.lua 参照)。
      -- cterm が無いと主環境 (termguicolors=off) で末尾空白ハイライトが完全不可視だった。
      require("dotfiles.hl").set("MiniTrailspace", {
        fg = "NONE",
        bg = pal.bright_red.hex,
        ctermbg = pal.bright_red.cterm,
      })

      local map = vim.keymap.set
      local trim_opts = { silent = true, desc = "Trim trailing whitespace" }
      map("n", "<leader>lr", function()
        require("mini.trailspace").trim()
        vim.cmd.nohlsearch()
      end, trim_opts)
    end,
  },
  {
    "MeanderingProgrammer/render-markdown.nvim",
    -- ⚠️ ft = { "markdown" } にしないこと: lazy.nvim は ft ゲートのプラグインをロードした後
    -- FileType を再発火し、その再実行がレガシー Vimscript syntax 一式 (markdown.vim →
    -- html.vim → css.vim, ~16ms) を treesitter highlight と二重にロードする実バグがあった
    -- (issues/done/013-bug-nvim-markdown-legacy-syntax-double-load.md、A/B 実測で特定)。
    -- BufReadPre (FileType より前) でロードすれば再発火が起きず legacy source はゼロになる。
    -- 拡張子ゲートの限界: 変わり種拡張子 (.mkd 等) や modeline で ft=markdown になるファイル
    -- では render-markdown がロードされない (実運用は .md/.markdown のみで許容)。
    event = { "BufReadPre *.md", "BufNewFile *.md", "BufReadPre *.markdown", "BufNewFile *.markdown" },
    dependencies = {
      "nvim-treesitter/nvim-treesitter",
      "nvim-tree/nvim-web-devicons",
    },
    -- 挿入モードでは装飾を消して生テキストに戻す (編集中に装飾が邪魔にならない)。
    -- 読むとき (ノーマルモード) だけインライン装飾が乗る。
    opts = {
      render_modes = { "n", "c" },
    },
    keys = {
      { "<leader>mm", "<cmd>RenderMarkdown toggle<cr>", desc = "Toggle render-markdown" },
    },
  },
  {
    "TaDaa/vimade",
    event = "WinEnter",
    opts = {
      fadelevel = 0.4,
      -- アニメーションなしで軽量化
      recipe = { "default", { animate = false } },
    },
  },
  {
    "chrisgrieser/nvim-early-retirement",
    event = "VeryLazy",
    opts = {
      retirementAgeMins = 20,  -- 20分間使わなかったら自動削除
      minimumBufferNum = 4,    -- バッファが4個未満の時は削除しない
      notificationOnAutoClose = true, -- 削除時に通知（nvim-notifyが必要）
    },
  },
  {
    "folke/sidekick.nvim",
    opts = {
      cli = {
        mux = {
          enabled = true, -- tmux統合を有効化
        },
        watch = true,
        win = {
          layout = "float", -- フロートウィンドウで表示
          -- width/height は layout ごとのネスト (float.* / split.*) 配下に置く必要がある。
          -- layout の兄弟に直接置くと読まれず既定 0.9/0.9 に落ちる (sidekick c93c0cb 以降の仕様)。
          float = {
            width = 0.6,  -- 画面の60%
            height = 0.7, -- 画面の70%
          },
        },
      },
    },
    keys = {
      {
        "<C-Space>",
        function() require("sidekick.cli").toggle() end,
        desc = "Sidekick Toggle",
        mode = { "n", "t", "i", "x" },
      },
      -- ターミナルモード内でスクロール
      { "<C-u>", [[<C-\><C-n><C-u>]], mode = "t", desc = "Scroll up in terminal" },
      { "<C-d>", [[<C-\><C-n><C-d>]], mode = "t", desc = "Scroll down in terminal" },
      {
        "<leader>sc",
        function() require("sidekick.cli").toggle({ name = "claude", focus = true }) end,
        desc = "Sidekick Toggle Claude",
      },
      {
        "<leader>ss",
        function() require("sidekick.cli").select() end,
        desc = "Sidekick Select CLI",
      },
    },
  },
  {
    "b0o/incline.nvim",
    -- BufReadPre は未存在ファイルでは発火しない。新規ファイル起点のセッションでも
    -- 有効になるよう BufNewFile を併記 (gitsigns/ibl/nvim-lint と同じ規律)
    event = { "BufReadPre", "BufNewFile" },
    config = function()
      local incline = require("incline")
      incline.setup({
        hide = {
          cursorline = false,
        },
        render = function(props)
          local bufname = vim.api.nvim_buf_get_name(props.buf)
          local filename = vim.fn.fnamemodify(bufname, ":t")
          if filename == "" then
            filename = "[No Name]"
          end

          -- 一般的なファイル名の場合は親ディレクトリも表示
          local common_names = {
            "index.tsx", "index.ts", "index.jsx", "index.js",
            "page.tsx", "page.ts", "layout.tsx", "layout.ts",
            "main.go", "main.rs", "main.py", "main.rb",
            "test.rb", "test.py", "test.js", "test.ts",
            "spec.rb", "spec.ts", "spec.js",
            "config.rb", "config.ts", "config.js",
            "README.md", "package.json", "tsconfig.json"
          }
          local show_parent = false
          for _, name in ipairs(common_names) do
            if filename == name then
              show_parent = true
              break
            end
          end

          local display_name = filename
          if show_parent and bufname ~= "" then
            local parent = vim.fn.fnamemodify(bufname, ":h:t")
            if parent ~= "" and parent ~= "." then
              display_name = parent .. "/" .. filename
            end
          end

          local modified = vim.bo[props.buf].modified

          -- 色の設定（inactive時は薄く）。incline は render の戻り値を :highlight コマンドに
          -- 素通しするため、gui* と cterm* の併記が必要 (256色環境の規律は dotfiles/hl.lua 参照)
          local fg_color, cterm_fg = pal.light1.hex, pal.light1.cterm
          local bg_color, cterm_bg = pal.dark1.hex, pal.dark1.cterm

          if not props.focused then
            -- inactive時は存在感を弱める
            fg_color, cterm_fg = pal.dark3.hex, pal.dark3.cterm
            bg_color, cterm_bg = pal.dark0.hex, pal.dark0.cterm
          end

          local res = {
            { display_name, guifg = fg_color, guibg = bg_color, ctermfg = cterm_fg, ctermbg = cterm_bg },
          }

          if modified then
            table.insert(res, { " ●", guifg = pal.bright_orange.hex, guibg = bg_color, ctermfg = pal.bright_orange.cterm, ctermbg = cterm_bg })
          end

          return res
        end,
      })

    end,
  },
}, {
  -- ベンチマーク用 seam: DOTFILES_NVIM_DISABLE=plugin1,plugin2 で指定プラグインを無効化して
  -- 起動できる (tests/nvim/bench_nvim.sh の A/B 比較が使う)。通常運用では未設定 = 全て有効。
  defaults = {
    cond = function(plugin)
      local disabled = vim.env.DOTFILES_NVIM_DISABLE
      if not disabled or disabled == "" then return true end
      for name in string.gmatch(disabled, "[^,]+") do
        if plugin.name == name then return false end
      end
      return true
    end,
  },
  -- lazy.nvim は既定 (performance.rtp.reset=true) で runtimepath をリセットし、
  -- 上で prepend した dotfiles のパスを消してしまう (nvim/ftplugin が丸ごと死んだ実バグ)。
  -- reset は維持しつつ、残すべきパスを明示する。
  performance = {
    rtp = {
      paths = vim.tbl_filter(function(p) return p ~= "" end, { nvim_dir }),
      -- 使っていない標準 plugin を起動から外す。zip/tar/gz をエディタで直接開きたく
      -- なったら該当行を外して再有効化する (netrwPlugin は nvim <dir> の挙動が変わるため外していない)
      disabled_plugins = { "gzip", "tarPlugin", "zipPlugin", "tutor" },
    },
  },
  install = { colorscheme = { "gruvbox" } },
  checker = { enabled = true, frequency = 86400 },  -- 起動毎チェックはローカル fs のみ。定期 git fetch を 1時間→1日に間引き、更新通知ノイズと background 通信を抑制
})

-- プラグインロードトラッカー (:PluginLoadStats で棚卸し。off は DOTFILES_PLUGIN_LOAD_TRACKER=0。
-- docs/nvim-plugin-load-tracker.md 参照)
require("dotfiles.plugin_load_tracker").setup()

-- <C-u>/<C-d> スムーズスクロール (neoscroll.nvim の自作置換。単発=アニメ /
-- 押しっぱなし=素通しで、リピート時のカーソル乱れを構造的に回避。モジュール冒頭コメント参照)
require("dotfiles.smooth_scroll").setup()

-- 折り畳みの設定
-- foldmethod は既定 manual のまま、計算は dotfiles.folds (expr で計算 → manual へ凍結、
-- FastFold 方式) が担う。expr を常時セットするとバッファ再表示のたびに全行再評価が走る
-- (6000 行で切替 ~3.4ms) ため、ここでは foldmethod/foldexpr を set しない。
vim.opt.foldlevel = 100
require("dotfiles.folds").setup()
function Foldtext()
  local line = vim.fn.getline(vim.v.foldstart)
  local count = vim.v.foldend - vim.v.foldstart + 1
  return string.format("%s (%d lines folded)", line, count)
end
vim.opt.foldtext = "v:lua.Foldtext()"
vim.opt.fillchars = { fold = " " } -- 折りたたんだ際のあまりの部分をスペースにする
-- ⚠️ <Tab> と <C-i> は端末では同一キーコード (Apple Terminal + tmux は拡張キー報告で
-- 区別しない) ため、このマップで <C-i> (jumplist 前進) は fold open に化けて失われる。
-- fold 開閉を <Tab> に置く利便を優先した意図的なトレードオフ。<C-i> が必要になったら
-- fold を za/zo 系や <leader> 配下へ移して再評価する。
vim.keymap.set("n", "<Tab>", "zo")
vim.keymap.set("n", "<S-Tab>", "zc")
vim.keymap.set("n", "<Leader><Tab>", "zR")
vim.keymap.set("n", "<Leader><S-Tab>", "zM")
