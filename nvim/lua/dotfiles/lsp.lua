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
  -- solargraph は mason が入れたバイナリを直接使う (useBundler=false)。project の Gemfile 側
  -- solargraph を bundle exec で使いたい場合のみ true にする (Gemfile に無い project では起動失敗)。
  solargraph = {
    settings = { solargraph = { useBundler = false, diagnostics = true } },
  },
}

-- mason-lspconfig / vim.lsp.enable に渡すサーバ名 (lspconfig の名前)。
-- coc の LSP 系 extension (tsserver/eslint/pyright/go/solargraph/html/css/json/yaml/sh/docker/tailwind/sql) を踏襲。
--
-- 意図的に移行しなかった coc 機能 (欠落ではなく意図した縮退。パリティ台帳としてここに明記):
--   - spell-checker / 色プレビュー / markdownlint / swagger (ユーザー確認済み・2026-07。
--     必要時に cspell(nvim-lint) / nvim-colorizer / markdownlint(nvim-lint) を足す)
--   - coc-html-css-support (HTML 内の CSS クラス名補完): ネイティブに直等価なし。html/cssls で部分カバー
--   - <C-s> range-select (coc-range-select): treesitter incremental_selection 等で代替可 (未設定)
--   - <C-f>/<C-b> の float スクロール: 0.11 は hover 窓を再フォーカスしてスクロールできるため未マップ
M.ensure_installed = {
  "ts_ls", "eslint", "pyright", "gopls", "solargraph",
  "html", "cssls", "jsonls", "yamlls", "bashls", "dockerls", "tailwindcss", "sqlls",
}

-- documentHighlight 用の単一 augroup。バッファ毎に augroup を作ると空グループ名が
-- 累積する (バッファ削除後も名前が残る) ため 1 グループに集約し、attach 毎に当該バッファの
-- autocmd を貼り直す (再 attach / LSP 再起動時の重複登録を回避)。
local hl_augroup = vim.api.nvim_create_augroup("dotfiles_lsp_document_highlight", { clear = true })

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

  -- キーマップは attach したサーバ種別に依らずバッファへ張る
  local grp = vim.api.nvim_create_augroup("dotfiles_lsp_attach", { clear = true })
  vim.api.nvim_create_autocmd("LspAttach", {
    group = grp,
    callback = function(args)
      on_attach(vim.lsp.get_client_by_id(args.data.client_id), args.buf)
    end,
  })
  -- client が detach したら、そのバッファに残った highlight autocmd を止める
  -- (server 再起動等でバッファは開いたまま detach しても CursorHold が空振りし続けないように)
  vim.api.nvim_create_autocmd("LspDetach", {
    group = grp,
    callback = function(args)
      vim.api.nvim_clear_autocmds({ group = hl_augroup, buffer = args.buf })
    end,
  })
end

return M
