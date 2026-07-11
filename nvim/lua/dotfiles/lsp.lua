-- ネイティブ LSP (vim.lsp) のキーマップ・診断・サーバ設定。
--
-- 2026-07 に coc.nvim からネイティブ LSP へ移行した際に新設。plugin spec
-- (nvim-lspconfig の config、_nviminit.lua) から setup(capabilities) を呼ぶ。
--   - capabilities: blink.cmp が広告する補完 capability (nil 可)
--   - サーバの有効化 (vim.lsp.enable) は _nviminit.lua の enable_available() が
--     server_packages を見て行う (mason-lspconfig は 2026-07-11 廃止)。
--     ここは "全サーバ共通の設定 (capabilities / on_attach) と、サーバ固有 settings" だけを持つ。
-- キー割り当ては coc 時代の muscle memory を踏襲する (gd / gD / <C-j> / <C-k> / t / [g / ]g など)。
local M = {}

-- サーバ固有 settings。空テーブルのサーバは共通設定のみで足りるので列挙しない。
-- inlay hints は既定 off で、<leader>ih トグル (M.setup) で有効化する。サーバ側で hint 種別を
-- 明示 on にしないと出ないためここで settings を渡す。仮想テキスト描画なので termguicolors=off の
-- 256色端末でも表示できる (色は淡くなる)。
M.servers = {
  -- solargraph は mason が入れたバイナリを直接使う (useBundler=false)。project の Gemfile 側
  -- solargraph を bundle exec で使いたい場合のみ true にする (Gemfile に無い project では起動失敗)。
  -- formatting=true は coc-settings の solargraph.formatting: true を踏襲 (これが無いと Ruby の
  -- <leader>f/:Format が lsp_format fallback 先の solargraph 既定 off で no-op になる)。
  solargraph = {
    settings = { solargraph = { useBundler = false, diagnostics = true, formatting = true } },
  },
  ts_ls = {
    settings = {
      typescript = { inlayHints = {
        includeInlayParameterNameHints = "all",
        includeInlayVariableTypeHints = true,
        includeInlayFunctionParameterTypeHints = true,
        includeInlayFunctionLikeReturnTypeHints = true,
      } },
      javascript = { inlayHints = {
        includeInlayParameterNameHints = "all",
        includeInlayVariableTypeHints = true,
        includeInlayFunctionParameterTypeHints = true,
        includeInlayFunctionLikeReturnTypeHints = true,
      } },
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
}

-- 使用サーバの単一真実源: lspconfig 名 → mason パッケージ名。
--   - enable (_nviminit.lua の vim.lsp.enable) は key (lspconfig 名) を使う
--   - バイナリ導入 (mason-tool-installer) は value (mason パッケージ名) を使う
-- 新サーバはここへ 1 行足せば enable と導入の両方に効く。
-- coc の LSP 系 extension (tsserver/eslint/pyright/go/solargraph/html/css/json/yaml/sh/docker/tailwind/sql) を踏襲。
--
-- 意図的に移行しなかった coc 機能 (欠落ではなく意図した縮退。パリティ台帳としてここに明記):
--   - spell-checker / 色プレビュー / markdownlint / swagger (ユーザー確認済み・2026-07。
--     必要時に cspell(nvim-lint) / nvim-colorizer / markdownlint(nvim-lint) を足す)
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

-- バッファに attach 中の client (除外 id を除く) に documentHighlight 対応者がいるか
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
  map("n", "gd", function() tb().lsp_definitions() end, "LSP definitions")
  map("n", "gD", function() tb().lsp_implementations() end, "LSP implementations")
  map("n", "<C-k>", function() tb().lsp_references() end, "LSP references")

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
  end, "LSP implementation or definition")

  -- コードアクション (coc: <leader>ac=cursor / <leader>as=source / <leader>qf=quickfix)
  map("n", "<leader>ac", vim.lsp.buf.code_action, "Code action")
  map("n", "<leader>as", function()
    vim.lsp.buf.code_action({ context = { only = { "source" } } })
  end, "Source action")
  map("n", "<leader>qf", function()
    vim.lsp.buf.code_action({ context = { only = { "quickfix" } }, apply = true })
  end, "Quickfix")

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

  -- coc 時代のサイン配色 (エラー=白字/赤地・警告=黒字/橙地) を踏襲。
  -- hl.set = ColorScheme 再適用 + cterm 併記 (256色環境) の規律 (dotfiles/hl.lua 参照)
  local hl = require("dotfiles.hl")
  hl.set("DiagnosticSignError", { fg = "#ffffff", bg = "#ff0000", ctermfg = 231, ctermbg = 196 })
  hl.set("DiagnosticSignWarn", { fg = "#000000", bg = "#d78700", ctermfg = 16, ctermbg = 172 })

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

  -- サーバ固有 settings のマージ
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
