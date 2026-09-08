-- <C-k> (参照一覧) の振り分け (nvim/lua/dotfiles/lsp.lua の M.use_ripgrep_references と、
-- on_attach の <C-k> マッピング) の headless 検証。test_lsp_references_dispatch.sh から dofile。
-- 守っている不変条件:
--
--   1. Ruby の **メソッド / ローカル** (小文字始まり) だけ ripgrep へ回す。ruby-lsp の
--      references は索引を使わず全ファイルを再パースし (references.rb:63)、しかも一致条件は
--      名前一致だけ (reference_finder.rb:285) なので、11 秒かけて grep と同じ精度しか出ない。
--   2. **定数・クラス (大文字始まり / :: を含む) は LSP に残す**。こちらは index.resolve で
--      名前空間を解決するので grep より正確。ここを取り違えると精度が落ちる。
--   3. **Ruby 以外は LSP のまま**。gopls / ts_ls の references は型解析つきで速いので、
--      grep へ回すのは劣化にしかならない。
--   4. **行き先は M.references_action の返り値で表明される**。分岐をマッピングの中に埋めると、
--      「文字列が在るか」の静的 pin しか書けず、rg と LSP を入れ替える / word_match を落とす /
--      cwd を nvim の cwd に固定する、といった変異が全部緑で通る (敵対レビュー P1-2)。
--   5. 検索範囲は **Ruby のサーバの root**。on_attach が受け取った client をそのまま使うと、
--      後から attach した client (例: .erb の tailwindcss) の root を掴む (敵対レビュー P2-1)。
--   6. <C-k> のマッピングがこの判定を実際に通る。判定関数だけ正しくても、マッピングから
--      呼ばれていなければ 1 mm も効かない。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local lsp = require("dotfiles.lsp")

if type(lsp.use_ripgrep_references) ~= "function" then
  fail("M.use_ripgrep_references が無い。<C-k> の振り分けが消えている")
end

local cases = {
  { ft = "ruby", word = "wrap_error", want = true, why = "Ruby のメソッド" },
  { ft = "ruby", word = "account", want = true, why = "Ruby のローカル変数" },
  { ft = "ruby", word = "_private_helper", want = true, why = "_ 始まりのメソッド" },
  { ft = "ruby", word = "ApplicationRecord", want = false, why = "定数 (index.resolve の方が正確)" },
  -- <cword> は :: を含まない (iskeyword に : が無い。実測) ので、名前空間つきの参照でも
  -- 判定に来るのは末尾の定数名だけ。大文字始まりの規則がそれを覆う
  { ft = "ruby", word = "BaseLoader", want = false, why = "名前空間つき参照でも <cword> は定数名だけ" },
  { ft = "ruby", word = "", want = false, why = "カーソル下に語が無い" },
  { ft = "eruby", word = "wrap_error", want = true, why = "eruby も Ruby と同じ" },
  { ft = "go", word = "wrapError", want = false, why = "gopls は型解析つきで速い" },
  { ft = "typescript", word = "wrapError", want = false, why = "ts_ls も同上" },
  { ft = "", word = "wrap_error", want = false, why = "filetype なし" },
}

local checked = 0
for _, c in ipairs(cases) do
  local got = lsp.use_ripgrep_references(c.ft, c.word)
  if got ~= c.want then
    fail(("use_ripgrep_references(%q, %q) = %s, want %s (%s)"):format(c.ft, c.word, tostring(got), tostring(c.want), c.why))
  end
  checked = checked + 1
end

-- 4. 行き先の表明 (返り値) をテーブル駆動で固定する
local action_cases = {
  { ft = "ruby", word = "wrap_error", root = "/r", want = {
      route = "ripgrep", search = "wrap_error", word_match = "-w", cwd = "/r",
      prompt_title = "参照 (ripgrep): wrap_error" }, why = "Ruby のメソッド" },
  { ft = "eruby", word = "render_row", root = "/r", want = {
      route = "ripgrep", search = "render_row", word_match = "-w", cwd = "/r",
      prompt_title = "参照 (ripgrep): render_row" }, why = "eruby も同じ" },
  { ft = "ruby", word = "ApplicationRecord", root = "/r", want = { route = "lsp" },
    why = "定数は LSP (index.resolve が名前空間を解決する)" },
  { ft = "go", word = "wrapError", root = "/r", want = { route = "lsp" },
    why = "Ruby 以外は LSP (型解析つきで速い)" },
  { ft = "ruby", word = "", root = "/r", want = { route = "lsp" }, why = "カーソル下に語が無い" },
}
for _, c in ipairs(action_cases) do
  local got = lsp.references_action(c.ft, c.word, c.root)
  if not vim.deep_equal(got, c.want) then
    fail(("references_action(%q, %q, %q) = %s, want %s (%s)"):format(
      c.ft, c.word, c.root, vim.inspect(got):gsub("%s+", " "), vim.inspect(c.want):gsub("%s+", " "), c.why))
  end
end

-- 5. 検索範囲は Ruby のサーバの root
local orig_get_clients = vim.lsp.get_clients
local function fail_clients(msg) vim.lsp.get_clients = orig_get_clients; fail(msg) end

-- ruby_lsp が居ればその root
vim.lsp.get_clients = function(opts)
  if opts and opts.name == "ruby_lsp" then return { { name = "ruby_lsp", root_dir = "/ruby/root" } } end
  return {}
end
if lsp.ruby_root_for(0) ~= "/ruby/root" then
  fail_clients(("ruby_root_for = %q。ruby_lsp の root を使うこと"):format(lsp.ruby_root_for(0)))
end

-- solargraph しか居なければそちら
vim.lsp.get_clients = function(opts)
  if opts and opts.name == "solargraph" then return { { name = "solargraph", root_dir = "/sg/root" } } end
  return {}
end
if lsp.ruby_root_for(0) ~= "/sg/root" then
  fail_clients(("ruby_root_for = %q。solargraph の root を使うこと"):format(lsp.ruby_root_for(0)))
end

-- 🚨 Ruby 以外の client (tailwindcss 等) の root は使わない。名前で絞らない実装だとここで落ちる
vim.lsp.get_clients = function(opts)
  if opts and opts.name then return {} end -- Ruby のサーバは居ない
  return { { name = "tailwindcss", root_dir = "/tailwind/root" } }
end
local got_root = lsp.ruby_root_for(0)
if got_root == "/tailwind/root" then
  fail_clients("ruby_root_for が tailwindcss の root を返した。Ruby のサーバを名前で選ぶこと")
end
vim.lsp.get_clients = orig_get_clients

-- 6. マッピング側がこの判定を通ることを静的に固定する。
-- 実際に <C-k> を押す形にはしない: client の attach と telescope のロードが要り、
-- 押した結果は picker が開くかどうかでしか観測できないため (どちらの経路でも picker は開く)。
local src = table.concat(vim.fn.readfile(vim.env.DOTFILES_LSP_LUA), "\n")
local ck = src:match('map%("n", "<C%-k>".-\n  end, "[^"]*"%)')
if not ck then
  fail("lsp.lua に <C-k> のマッピングが見つからない (書き方が変わった?)")
end
-- 静的 pin は「純関数を通っているか」の 1 点だけに縮める (分岐の中身は上の返り値テストが見る)
if not ck:find("references_action", 1, true) then
  fail("<C-k> のマッピングが M.references_action を通っていない。判定が効かない")
end
if not ck:find("ruby_root_for", 1, true) then
  fail("<C-k> のマッピングが M.ruby_root_for を通っていない。検索範囲が別 client の root になる")
end

print(("OK lsp references dispatch: 判定 %d ケース / 行き先の返り値 %d ケース / root の選択 3 ケース / <C-k> の配線"):format(
  checked, #action_cases))
