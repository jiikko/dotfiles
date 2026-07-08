-- ネイティブ LSP (vim.lsp) のキーマップ・診断・サーバ設定。
--
-- 2026-07 に coc.nvim からネイティブ LSP へ移行した際に新設。plugin spec
-- (mason-lspconfig の config) から setup(capabilities) を呼ぶ。
--   - capabilities: blink.cmp が広告する補完 capability (nil 可)
--   - サーバの有効化自体は mason-lspconfig の automatic_enable が vim.lsp.enable() で行う。
--     ここは "全サーバ共通の設定 (capabilities / on_attach) と、サーバ固有 settings" だけを持つ。
-- キー割り当ては coc 時代の muscle memory を踏襲する (gd / gD / <C-j> / <C-k> / t / [g / ]g など)。
local M = {}

-- サーバ固有 settings。空テーブルのサーバは共通設定のみで足りるので列挙しない。
M.servers = {
  -- solargraph は project の Gemfile 側 gem を使う (coc-settings の solargraph.useBundler=true 相当)。
  -- Gemfile に solargraph が無い project では bundle 実行が失敗しうる。その場合は false にする。
  solargraph = {
    settings = { solargraph = { useBundler = true, diagnostics = true } },
  },
}

-- mason-lspconfig / vim.lsp.enable に渡すサーバ名 (lspconfig の名前)。
-- coc の LSP 系 extension (tsserver/eslint/pyright/go/solargraph/html/css/json/yaml/sh/docker/tailwind/sql) を踏襲。
--
-- 意図的に移行しなかった coc 機能 (ユーザー確認済み・2026-07): spell-checker / 色プレビュー /
-- markdownlint / swagger。ネイティブに綺麗な等価が無く、必要になった時点で cspell(nvim-lint) /
-- nvim-colorizer / markdownlint(nvim-lint) を足す方針。欠落ではなく意図した縮退なのでここに明記する。
M.ensure_installed = {
  "ts_ls", "eslint", "pyright", "gopls", "solargraph",
  "html", "cssls", "jsonls", "yamlls", "bashls", "dockerls", "tailwindcss", "sqlls",
}

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

  -- ホバー (coc: t)。vim/help は :help にフォールバック
  map("n", "t", function()
    local ft = vim.bo[bufnr].filetype
    if ft == "vim" or ft == "help" then
      vim.cmd("help " .. vim.fn.expand("<cword>"))
    else
      vim.lsp.buf.hover()
    end
  end, "Hover / help")

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
    local grp = vim.api.nvim_create_augroup("dotfiles_lsp_highlight_" .. bufnr, { clear = true })
    vim.api.nvim_create_autocmd({ "CursorHold", "CursorHoldI" }, {
      group = grp,
      buffer = bufnr,
      callback = vim.lsp.buf.document_highlight,
    })
    vim.api.nvim_create_autocmd({ "CursorMoved", "CursorMovedI" }, {
      group = grp,
      buffer = bufnr,
      callback = vim.lsp.buf.clear_references,
    })
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

  -- coc 時代のサイン配色 (エラー=白字/赤地・警告=黒字/橙地) を踏襲
  vim.api.nvim_set_hl(0, "DiagnosticSignError", { fg = "#ffffff", bg = "#ff0000" })
  vim.api.nvim_set_hl(0, "DiagnosticSignWarn", { fg = "#000000", bg = "#d78700" })

  -- 診断の前後移動 (coc: [g / ]g)。0.11 で goto_prev/goto_next は jump に統合された。
  vim.keymap.set("n", "[g", function()
    vim.diagnostic.jump({ count = -1, float = true })
  end, { silent = true, desc = "Prev diagnostic" })
  vim.keymap.set("n", "]g", function()
    vim.diagnostic.jump({ count = 1, float = true })
  end, { silent = true, desc = "Next diagnostic" })
end

-- plugin spec (mason-lspconfig の config) から呼ぶ。
-- 呼び出し順の契約: mason-lspconfig.setup() の automatic_enable より前に呼ぶこと。
--   automatic_enable は installed サーバに対して vim.lsp.enable() を実行するため、
--   その前に vim.lsp.config("*") で共通 capabilities を確定させておく必要がある。
function M.setup(capabilities)
  setup_diagnostics()

  -- 全サーバ共通の capabilities (blink.cmp)。nil なら素の capability。
  vim.lsp.config("*", { capabilities = capabilities or vim.lsp.protocol.make_client_capabilities() })

  -- サーバ固有 settings のマージ
  for name, cfg in pairs(M.servers) do
    vim.lsp.config(name, cfg)
  end

  -- キーマップは attach したサーバ種別に依らずバッファへ張る
  vim.api.nvim_create_autocmd("LspAttach", {
    group = vim.api.nvim_create_augroup("dotfiles_lsp_attach", { clear = true }),
    callback = function(args)
      on_attach(vim.lsp.get_client_by_id(args.data.client_id), args.buf)
    end,
  })
end

return M
