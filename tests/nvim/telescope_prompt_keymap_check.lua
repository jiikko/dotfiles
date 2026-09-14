-- telescope の prompt に張られる insert マッピング (_nviminit.lua の defaults.mappings.i) の
-- headless 検証。守っている不変条件:
--
--   1. **<C-u> に telescope のマップを張らない**。既定は preview_scrolling_up で、入力を
--      消せない。false を渡して telescope に張らせないことで、挿入モード既定の <C-u>
--      (カーソル前を削除 = 入力クリア) が効くようになる。
--   2. **プレビューの上スクロールは <C-b> で残す**。1 を入れただけだと <C-d> の下スクロールと
--      対にならず、機能が片方だけ消える。
--   3. 1 と 2 は **実際に開いた picker のバッファ**で見る。setup に渡した table を読み返す形は、
--      telescope 側の解釈 (false の扱い) が変わっても気づけない。
--
-- 🚨 「<C-u> を押したら入力が消える」ところまでは headless で測れない (feedkeys では
--    prompt が insert モードにならず、入力が 1 文字も入らない = 有無で結果が変わらない観測に
--    なる。実測 2026-09-14)。ここで固定するのは**配線**までで、挙動の確認は人が 1 度見る。
-- 🚨 rtp は cwd 依存 (basic_checktime_check.lua のヘッダ参照)。ランナーが repo root へ cd する。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

-- 🚨 外部プロセスを起こす picker (find_files = rg) を使わないこと。finder は非同期なので、
-- 検査が終わって片付けた後に spawn へ入り、telescope が警告とスタックトレースを吐く
-- (ランナーの backstop は "stack traceback" を失敗として拾うので false red の種になる)。
-- builtin() は telescope 自身の picker 一覧で、外部コマンドを起動しない。
require("telescope.builtin").builtin()
vim.wait(2000, function() return vim.bo.filetype == "TelescopePrompt" end)
if vim.bo.filetype ~= "TelescopePrompt" then
  fail(("picker が開いていない (filetype=%q)。ハーネスの失敗であって合格ではない"):format(vim.bo.filetype))
end

local maps = {}
for _, m in ipairs(vim.api.nvim_buf_get_keymap(vim.api.nvim_get_current_buf(), "i")) do
  maps[m.lhs] = true
end
-- canary: マッピングが 1 件も無いなら、下の「<C-U> が無い」は何も主張していない
if vim.tbl_isempty(maps) then
  fail("prompt バッファに insert マッピングが 1 件も無い。telescope の設定が届いていない")
end
-- <C-d> は既定のまま残っているはず (マッピング機構そのものが効いている証拠)
if not maps["<C-D>"] then
  fail("<C-D> (プレビュー下スクロール) が無い。既定のマッピングごと消えている")
end
if maps["<C-U>"] then
  fail("<C-U> に telescope のマップが張られている。素の <C-u> (入力クリア) が効かない")
end
if not maps["<C-B>"] then
  fail("<C-B> が無い。<C-u> を明け渡した代わりのプレビュー上スクロールが消えている")
end

print("OK telescope prompt keymap: <C-U> は素のまま / <C-B> に上スクロール / <C-D> は既定のまま")
