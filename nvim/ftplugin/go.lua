-- Go バッファ固有の buffer-local マッピング。
--
-- fatih/vim-go 廃止 (2026-07) に伴い、vim-go の ftplugin が Go 限定 buffer-local で
-- 提供していた挙動 (関数ジャンプ ]] [[ / テキストオブジェクト af if ac ic / GoDecls) を
-- ネイティブで再現する。global に張らないのは ]] [[ が Vim 組み込みの section motion を
-- 上書きするためで、他 filetype を巻き込まないよう vim-go の buffer-local scope を踏襲する。
-- 定義/実装/参照ジャンプ・hover・診断は lsp.lua が LspAttach で張る (Go も gopls で同一)。
--
-- 依存: nvim-treesitter-textobjects (master 固定。_nviminit.lua の treesitter dependencies)。

local ok_move, move = pcall(require, "nvim-treesitter.textobjects.move")
local ok_sel, select = pcall(require, "nvim-treesitter.textobjects.select")

-- 関数間ジャンプ ]] [[ (旧 vim-go go#textobj#FunctionJump)。normal と visual のみ。
-- jumplist への追加は configs.setup の move.set_jumps=true が担う。
-- operator-pending (o) には張らない: goto_next_start は次関数の先頭文字 (func の f) 上へ
-- 動くため、d]]/c]]/y]] が次関数の 1 文字を巻き込む (実測: d]] → "unc bravo")。関数単位の
-- 操作はテキストオブジェクト daf/vaf/yaf (下) が正しく担うので、o は組み込み ]] に委ねる。
if ok_move then
  vim.keymap.set({ "n", "x" }, "]]", function()
    move.goto_next_start("@function.outer", "textobjects")
  end, { buffer = true, silent = true, desc = "Next function start" })
  vim.keymap.set({ "n", "x" }, "[[", function()
    move.goto_previous_start("@function.outer", "textobjects")
  end, { buffer = true, silent = true, desc = "Prev function start" })
end

-- テキストオブジェクト (旧 vim-go go#textobj#Function/Comment)。x(visual)/o(operator-pending)。
--   af/if = 関数 outer/inner、ac/ic = コメント。inner (if) の linewise 化は _nviminit.lua の
--   textobjects.select.selection_modes で設定 (旧 vim-go inner は linewise だった)。
-- 差分メモ (vim-go と厳密一致しない点。いずれも軽微):
--   - af は関数の直前 doc コメントを含まない。vim-go は go_textobj_include_function_doc=1 で
--     含んでいたが、treesitter の @function.outer は (function_declaration) 単体で、after-query
--     追加では base 定義が優先され効かず、query 全 override は TSUpdate で drift するため見送り。
--   - Go の textobjects query に @comment.inner は無い (@comment.outer のみ) ため ic も outer に
--     フォールバックさせる (コメント全体を選択)。将来 query に comment.inner が入れば切替。
if ok_sel then
  local function textobj(lhs, capture, desc)
    vim.keymap.set({ "x", "o" }, lhs, function()
      select.select_textobject(capture, "textobjects")
    end, { buffer = true, silent = true, desc = desc })
  end
  textobj("af", "@function.outer", "a function")
  textobj("if", "@function.inner", "inner function")
  textobj("ac", "@comment.outer", "a comment")
  textobj("ic", "@comment.outer", "inner comment (Go query は outer のみ)")
end

-- GoDecls 置換 (<leader>gd/gD)。旧 vim-go の GoDecls は fzf/ctrlp backend 未導入で
-- エラー = 非稼働だった (headless 実測で確認)。telescope の LSP symbol picker で置換する。
-- 宣言一覧が主目的なので kind で絞る。telescope は SymbolKind 名を小文字化して照合するため
-- ここも小文字で書く。gopls の返す kind は実測: 関数=function、メソッド=method、
-- struct=struct、interface=interface、`type X string`/`type X = T` 等の named 型=class、
-- 定数=constant (LSP SymbolKind に "Type" は無く、素の named 型は Class で来る点に注意)。
local decl_kinds = { "function", "method", "struct", "interface", "class", "constant" }
vim.keymap.set("n", "<leader>gd", function()
  require("telescope.builtin").lsp_document_symbols({ symbols = decl_kinds })
end, { buffer = true, silent = true, desc = "Go declarations (document symbols)" })
-- GoDeclsDir 相当。ディレクトリ限定の厳密同等は telescope に無いため workspace 全体の
-- 動的シンボル検索で代替する (旧実装も非稼働だったため実挙動の後退はない)。
vim.keymap.set("n", "<leader>gD", function()
  require("telescope.builtin").lsp_dynamic_workspace_symbols({ symbols = decl_kinds })
end, { buffer = true, silent = true, desc = "Go declarations (workspace symbols)" })
