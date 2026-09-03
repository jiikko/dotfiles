-- ネイティブ LSP (vim.lsp) のキーマップ・診断・サーバ設定。
-- plugin spec (nvim-lspconfig の config、_nviminit.lua) から setup(capabilities) を呼ぶ。
--   - capabilities: blink.cmp が広告する補完 capability (nil 可)
--   - サーバの有効化 (vim.lsp.enable) は _nviminit.lua の enable_available() が
--     server_packages を見て行う。ここは "全サーバ共通の設定 (capabilities / on_attach) と、
--     サーバ固有 settings" だけを持つ。
-- キー割り当ては coc 時代の muscle memory を踏襲する (gd / gD / <C-j> / <C-k> / t / [g / ]g など)。
local M = {}

-- サーバ固有の設定 (settings / on_attach)。空テーブルのサーバは共通設定のみで足りるので列挙しない。
-- inlay hints は既定 off で、<leader>ih トグル (M.setup) で有効化する。サーバ側で hint 種別を
-- 明示 on にしないと出ないためここで settings を渡す。仮想テキスト描画なので termguicolors=off の
-- 256色端末でも表示できる (色は淡くなる)。
-- TS/JS は同一の inlay hint 設定なので共通 table を1つ参照する (片方だけ変えて drift する事故を防ぐ)。
-- lspconfig は settings を読むだけで mutate しないため table 共有で挙動不変。
local ts_js_inlay_hints = {
  includeInlayParameterNameHints = "all",
  includeInlayVariableTypeHints = true,
  includeInlayFunctionParameterTypeHints = true,
  includeInlayFunctionLikeReturnTypeHints = true,
}

-- Ruby は project ごとに 1 サーバだけ attach する (ruby_lsp と solargraph を両方 attach すると
-- rubocop 由来の診断が二重に出る)。既定は従来どおり solargraph で、下の allowlist に挙げた
-- project root 配下でだけ ruby_lsp へ切り替える。全体を ruby_lsp に倒さないのは、既存 project が
-- solargraph 前提で運用されており一斉切替の影響を確認していないため。
-- 増やすときはここへ 1 行足す (ruby-lsp gem は使う ruby version ごとに `gem install ruby-lsp`
-- が要る。現状 rbenv 3.2.2 にのみ導入済み)。
-- allowlist の要素を比較可能な形に正規化する (~ 展開 + 末尾スラッシュ除去)。末尾スラッシュが
-- 残ると M.ruby_server_for の dir == root も dir .. "/" の前方一致も両方外れ、その project だけ
-- 無言で solargraph に落ちる (エラーも警告も出ない)。
function M.normalize_project_root(path)
  return (vim.fn.expand(path):gsub("/+$", ""))
end

-- 公開しているのはテストがここを真の出典として読めるようにするため (テスト側へ project パスを
-- 写すと、allowlist を増減したときにテストだけ古い前提のまま緑になる)。
M.ruby_lsp_projects = vim.tbl_map(M.normalize_project_root, {
  "~/src/the-rss-reader",
})

-- project root を担当するサーバ名を返す。返り値が常に 1 つであることが ruby_lsp / solargraph の
-- 排他の担保で、判定点をここ 1 か所に閉じている (root_dir 側に条件を 2 本書くと、片方の更新漏れが
-- そのまま二重 attach = 診断の二重表示になる)。
function M.ruby_server_for(dir)
  for _, root in ipairs(M.ruby_lsp_projects) do
    -- "/" 境界を要求する。素の前方一致だと ~/src/the-rss-reader-old のような兄弟が誤爆する
    if dir == root or vim.startswith(dir, root .. "/") then return "ruby_lsp" end
  end
  return "solargraph"
end

-- want が担当サーバのときだけ on_dir を呼ぶ (呼ばなければ attach しない)。root は lspconfig
-- 既定の root_markers と同じ { Gemfile, .git } で決める。
local function ruby_root_dir(want)
  return function(bufnr, on_dir)
    local name = vim.api.nvim_buf_get_name(bufnr)
    local dir = vim.fs.root(name ~= "" and name or bufnr, { "Gemfile", ".git" })
    if dir and M.ruby_server_for(dir) == want then on_dir(dir) end
  end
end

M.servers = {
  -- ruby-lsp / solargraph とも rbenv に gem install したものを PATH 経由で使う。
  -- 整形は ruby-lsp 内蔵の formatter (lspconfig 既定 init_options.formatter = "auto" が bundle の
  -- rubocop を検出) が担うので、solargraph と同じく <leader>F の lsp_format="fallback" で効く。
  ruby_lsp = {
    root_dir = ruby_root_dir("ruby_lsp"),
  },
  -- solargraph は PATH のバイナリを直接使う (useBundler=false)。project の Gemfile 側
  -- solargraph を bundle exec で使いたい場合のみ true にする (Gemfile に無い project では起動失敗)。
  -- formatting=true は coc-settings の solargraph.formatting: true を踏襲 (これが無いと Ruby の
  -- <leader>F/:Format が lsp_format fallback 先の solargraph 既定 off で no-op になる)。
  -- ⚠️ PATH 側が >= 0.56 で project の bundle が < 0.54.2 を pin していると無限ループする:
  --   0.56 は gem doc のキャッシュを `solargraph cache <gem>` の子プロセスへ投げる際 (library.rb の
  --   cache_next_gemspec) GEM_HOME を project の bundle へ書き換えるため、子は bundle 側の古い
  --   solargraph を起動する。0.52 に cache サブコマンドは無く即死するが親は exit status を見ず、
  --   同じ gem を選び直して毎秒 spawn し続ける (CPU 60% と "Caching gem" 通知が出っぱなし)。
  --   回避はその project の bundle の solargraph を >= 0.54.2 に上げること。
  solargraph = {
    settings = { solargraph = { useBundler = false, diagnostics = true, formatting = true } },
    root_dir = ruby_root_dir("solargraph"),
  },
  ts_ls = {
    settings = {
      typescript = { inlayHints = ts_js_inlay_hints },
      javascript = { inlayHints = ts_js_inlay_hints },
    },
  },
  gopls = {
    settings = { gopls = { hints = {
      parameterNames = true,
      assignVariableTypes = true,
      constantValues = true,
      functionTypeParameters = true,
      rangeVariableTypes = true,
      compositeLiteralTypes = true,
      compositeLiteralFields = true,
    } } },
  },
  pyright = {
    settings = { python = { analysis = { inlayHints = {
      variableTypes = true,
      functionReturnTypes = true,
      callArgumentNames = true,
    } } } },
  },
  -- nvim-lspconfig (rolling) の terraformls.lua on_attach は nvim 0.12 専用の
  -- vim.lsp.codelens.enable を無条件呼びし、0.11 では ON_ATTACH_ERROR が出るため、存在ガード付き
  -- on_attach で丸ごと上書きする (元の on_attach は該当 1 行のみなので機能欠落なし)。
  -- nvim 0.12+ へ上げたら削除してよい。
  terraformls = {
    on_attach = function(_, bufnr)
      if vim.lsp.codelens.enable then
        vim.lsp.codelens.enable(true, { bufnr = bufnr })
      end
    end,
  },
}

-- 使用サーバの単一真実源: lspconfig 名 → mason パッケージ名。
--   - enable (_nviminit.lua の vim.lsp.enable) は key (lspconfig 名) を使う
--   - バイナリ導入 (mason-tool-installer) は value (mason パッケージ名) を使う
-- 新サーバはここへ 1 行足せば enable と導入の両方に効く。
-- coc の LSP 系 extension (tsserver/eslint/pyright/go/solargraph/html/css/json/yaml/sh/docker/tailwind/sql) を踏襲。
-- Ruby だけは project ごとに ruby_lsp / solargraph を出し分ける (両方ここに載せ、M.servers の root_dir で排他)。
-- 意図的に移行しなかった coc 機能 (欠落ではなく意図した縮退。パリティ台帳としてここに明記):
--   - spell-checker / 色プレビュー / markdownlint / swagger (ユーザー確認済み。必要時に cspell(nvim-lint) / nvim-colorizer / markdownlint(nvim-lint) を足す)
--   - coc-html-css-support (HTML 内の CSS クラス名補完): ネイティブに直等価なし。html/cssls で部分カバー
--   - <C-s> range-select (coc-range-select): treesitter incremental_selection 等で代替可 (未設定)
--   - <C-f>/<C-b> の float スクロール: 0.11 は hover 窓を再フォーカスしてスクロールできるため未マップ
-- 注意: このテーブルの参照元は 2 箇所とも _nviminit.lua (enable_available と
-- mason-tool-installer の ensure_installed)。lsp.lua 内には参照が無い
M.server_packages = {
  ts_ls = "typescript-language-server",
  eslint = "eslint-lsp",
  pyright = "pyright",
  gopls = "gopls",
  ruby_lsp = "ruby-lsp",
  solargraph = "solargraph",
  html = "html-lsp",
  cssls = "css-lsp",
  jsonls = "json-lsp",
  yamlls = "yaml-language-server",
  bashls = "bash-language-server",
  dockerls = "dockerfile-language-server",
  tailwindcss = "tailwindcss-language-server",
  sqlls = "sqlls",
  terraformls = "terraform-ls", -- vim-terraform 置換 (2026-07): 補完/診断/hover を terraform-ls に委譲
}

-- documentHighlight 用の単一 augroup。バッファ毎に augroup を作ると空グループ名が
-- 累積する (バッファ削除後も名前が残る) ため 1 グループに集約し、attach 毎に当該バッファの
-- autocmd を貼り直す (再 attach / LSP 再起動時の重複登録を回避)。
local hl_augroup = vim.api.nvim_create_augroup("dotfiles_lsp_document_highlight", { clear = true })

-- documentHighlight 用の autocmd をバッファへ貼り直す (attach / detach 後の再登録で共用)。
local function register_document_highlight(bufnr)
  -- 再 attach でも重複しないよう、このバッファ分の既存 autocmd を消してから貼り直す
  vim.api.nvim_clear_autocmds({ group = hl_augroup, buffer = bufnr })
  vim.api.nvim_create_autocmd({ "CursorHold", "CursorHoldI" }, {
    group = hl_augroup,
    buffer = bufnr,
    callback = vim.lsp.buf.document_highlight,
  })
  vim.api.nvim_create_autocmd({ "CursorMoved", "CursorMovedI" }, {
    group = hl_augroup,
    buffer = bufnr,
    callback = vim.lsp.buf.clear_references,
  })
end

local function has_highlight_client(bufnr, exclude_id)
  for _, c in ipairs(vim.lsp.get_clients({ bufnr = bufnr, method = "textDocument/documentHighlight" })) do
    if c.id ~= exclude_id then
      return true
    end
  end
  return false
end

-- LspAttach 時にバッファローカルで張るキーマップ (coc 時代の割り当てを踏襲)
local function on_attach(client, bufnr)
  if not client then return end
  local function map(mode, lhs, rhs, desc)
    vim.keymap.set(mode, lhs, rhs, { buffer = bufnr, silent = true, desc = desc })
  end
  -- telescope は遅延ロード。attach 時に require すると全コードバッファで telescope が
  -- 先読みされ起動が重くなるため、キー押下時に取得する (coc 時代も jump 押下で初めて載っていた)。
  local function tb() return require("telescope.builtin") end

  -- ジャンプ (coc: gd=定義 / gD=実装 / <C-k>=参照)
  map("n", "gd", function() tb().lsp_definitions() end, "定義へジャンプ (LSP definitions)")
  map("n", "gD", function() tb().lsp_implementations() end, "interface の実装一覧へ (LSP implementations)")
  map("n", "<C-k>", function() tb().lsp_references() end, "参照元一覧 (LSP references)")

  -- <C-j>: interface 上なら実装へ、無ければ定義へフォールバック。
  -- coc 時代の <C-j> の意図 (実装優先 → 無ければ従来の定義ジャンプ) をネイティブで再現する。
  -- telescope の lsp_implementations 単体では「無ければ定義」の分岐が無いため、
  -- 先に implementation を probe して結果の有無で picker を切り替える。
  map("n", "<C-j>", function()
    -- implementation 対応 client だけを対象にする。0 件なら probe せず定義へ
    -- (非対応 method だと buf_request_all の handler が呼ばれずフォールバックが漏れるため)。
    -- offset_encoding も implementation 対応 client のものを使う。
    local impl_clients = vim.lsp.get_clients({ bufnr = bufnr, method = "textDocument/implementation" })
    if vim.tbl_isempty(impl_clients) then
      return tb().lsp_definitions()
    end
    local params = vim.lsp.util.make_position_params(0, impl_clients[1].offset_encoding)
    vim.lsp.buf_request_all(bufnr, "textDocument/implementation", params, function(results)
      for _, res in pairs(results) do
        if res.result and not vim.tbl_isempty(res.result) then
          return tb().lsp_implementations()
        end
      end
      tb().lsp_definitions()
    end)
  end, "実装へ、無ければ定義へ (impl or definition)")

  -- K: nvim 0.11 が LspAttach で張る既定の hover (desc が "vim.lsp.buf.hover()" のまま) を
  -- 説明付きで張り直す。t (global) と同じ動作で、チートシートに意図が出るようにするため
  map("n", "K", vim.lsp.buf.hover, "ホバー: 型とドキュメントを表示 (hover)")

  -- コードアクション (coc: <leader>ac=cursor / <leader>as=source / <leader>qf=quickfix)
  map("n", "<leader>ac", vim.lsp.buf.code_action, "カーソル位置のコードアクション (code action)")
  map("n", "<leader>as", function()
    vim.lsp.buf.code_action({ context = { only = { "source" } } })
  end, "import 整理など (source action)")
  map("n", "<leader>qf", function()
    vim.lsp.buf.code_action({ context = { only = { "quickfix" } }, apply = true })
  end, "quickfix を即適用 (quickfix)")

  -- インポート整理 (coc: :OR)
  vim.api.nvim_buf_create_user_command(bufnr, "OR", function()
    vim.lsp.buf.code_action({ context = { only = { "source.organizeImports" } }, apply = true })
  end, { desc = "Organize imports" })

  -- カーソル下シンボルのハイライト (coc: CursorHold で highlight)。
  -- サーバが documentHighlight を持つときだけ張り、CursorMoved で消す。
  if client:supports_method("textDocument/documentHighlight") then
    register_document_highlight(bufnr)
  end
end

-- 診断表示 (サイン・仮想テキスト・移動)。coc の CocErrorSign / CocWarningSign の色を踏襲。
local function setup_diagnostics()
  vim.diagnostic.config({
    severity_sort = true,
    update_in_insert = false,
    float = { border = "rounded", source = true },
    virtual_text = { spacing = 2, prefix = "●" },
    signs = {
      text = {
        [vim.diagnostic.severity.ERROR] = "E",
        [vim.diagnostic.severity.WARN] = "W",
        [vim.diagnostic.severity.INFO] = "I",
        [vim.diagnostic.severity.HINT] = "H",
      },
    },
  })

  -- coc 時代のサイン配色 (エラー=白字/赤地・警告=黒字/橙地) を踏襲。色は palette.diag が出典。
  -- hl.set = ColorScheme 再適用 + cterm 併記 (256色環境) の規律 (dotfiles/hl.lua 参照)
  local hl = require("dotfiles.hl")
  local diag = require("dotfiles.palette").diag
  hl.set("DiagnosticSignError", { fg = diag.error_fg.hex, bg = diag.error_bg.hex, ctermfg = diag.error_fg.cterm, ctermbg = diag.error_bg.cterm })
  hl.set("DiagnosticSignWarn", { fg = diag.warn_fg.hex, bg = diag.warn_bg.hex, ctermfg = diag.warn_fg.cterm, ctermbg = diag.warn_bg.cterm })

  -- 診断の前後移動 (coc: [g / ]g)。0.11 で goto_prev/goto_next は jump に統合された。
  vim.keymap.set("n", "[g", function()
    vim.diagnostic.jump({ count = -1, float = true })
  end, { silent = true, desc = "Prev diagnostic" })
  vim.keymap.set("n", "]g", function()
    vim.diagnostic.jump({ count = 1, float = true })
  end, { silent = true, desc = "Next diagnostic" })
end

-- plugin spec (nvim-lspconfig の config、_nviminit.lua) から呼ぶ。
-- 呼び出し順の契約: _nviminit.lua の enable_available() (vim.lsp.enable) より前に呼ぶこと。
--   enable 済みサーバに後から vim.lsp.config("*") を変えても既起動クライアントには
--   効かないため、共通 capabilities を先に確定させておく必要がある。
function M.setup(capabilities)
  setup_diagnostics()

  -- 全サーバ共通の capabilities (blink.cmp)。nil なら素の capability。
  vim.lsp.config("*", { capabilities = capabilities or vim.lsp.protocol.make_client_capabilities() })

  for name, cfg in pairs(M.servers) do
    vim.lsp.config(name, cfg)
  end

  -- ホバー (coc の t は global マップだった)。vim/help は :help、それ以外は LSP hover。
  -- global にすることで help/vim バッファでも :help が効く (coc 時代の挙動を踏襲)。
  vim.keymap.set("n", "t", function()
    local ft = vim.bo.filetype
    if ft == "vim" or ft == "help" then
      vim.cmd("help " .. vim.fn.expand("<cword>"))
    else
      vim.lsp.buf.hover()
    end
  end, { silent = true, desc = "Hover / help" })

  -- inlay hints の opt-in トグル (既定 off)。現在バッファに対して有効/無効を切り替える。
  -- inlayHint 対応クライアントが無いバッファでは何も起きない (no-op)。
  vim.keymap.set("n", "<leader>ih", function()
    local on = vim.lsp.inlay_hint.is_enabled({ bufnr = 0 })
    vim.lsp.inlay_hint.enable(not on, { bufnr = 0 })
    vim.notify("Inlay hints: " .. (on and "off" or "on"))
  end, { silent = true, desc = "Toggle inlay hints" })

  -- キーマップは attach したサーバ種別に依らずバッファへ張る
  local grp = vim.api.nvim_create_augroup("dotfiles_lsp_attach", { clear = true })
  vim.api.nvim_create_autocmd("LspAttach", {
    group = grp,
    callback = function(args)
      on_attach(vim.lsp.get_client_by_id(args.data.client_id), args.buf)
    end,
  })
  -- client が detach したら、そのバッファに残った highlight autocmd を止める
  -- (server 再起動等でバッファは開いたまま detach しても CursorHold が空振りし続けないように)。
  -- ただし他に documentHighlight 対応 client が残っていれば貼り直す: JS/TS は ts_ls (対応) と
  -- eslint (非対応) が同時 attach するため、無条件 clear だと eslint 側の detach だけで
  -- 生きている ts_ls のハイライトまで無言で消えていた。
  vim.api.nvim_create_autocmd("LspDetach", {
    group = grp,
    callback = function(args)
      if has_highlight_client(args.buf, args.data.client_id) then
        register_document_highlight(args.buf)
      else
        vim.api.nvim_clear_autocmds({ group = hl_augroup, buffer = args.buf })
      end
    end,
  })
end

return M
