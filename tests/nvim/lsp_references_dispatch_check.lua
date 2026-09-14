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
--   7. **rg の検索対象が Ruby 系のファイルに絞られている**。絞らないと README.md / *.yml /
--      *.js の単純一致まで参照候補に並ぶ。絞り込みは実際に rg を走らせて確かめる: テスト側に
--      同じ配列を書き写す形だと、タイプ名の綴り違いや --type-add の書式ミスが「リストが在る」
--      で緑になる (rg は未知のタイプ名を rc=2 のエラーにするので、写しでは検出できない)。
--   8. 絞り込みが **grep_string へ転送されている**。references_action が返すだけで
--      マッピングが捨てていても、7 の検査は返り値を直接見るので緑のまま通る。
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
      additional_args = lsp.ripgrep_reference_args,
      prompt_title = "参照 (ripgrep): wrap_error" }, why = "Ruby のメソッド" },
  { ft = "eruby", word = "render_row", root = "/r", want = {
      route = "ripgrep", search = "render_row", word_match = "-w", cwd = "/r",
      additional_args = lsp.ripgrep_reference_args,
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
-- 🚨 絞り込みの転送。references_action が additional_args を返していても、マッピングが
-- grep_string へ渡さなければ rg はタイプ無指定のまま走る (返り値テストは緑のまま通る)。
if not ck:find("additional_args = action.additional_args", 1, true) then
  fail("<C-k> のマッピングが action.additional_args を grep_string へ渡していない。rg の絞り込みが効かない")
end

-- 7. 絞り込みが **実際に rg で効く**か。fixture を作り、production と同じ引数 (返り値から
-- 組み立てる) で走らせて、Ruby 系だけが拾われることを見る。
local rg = vim.fn.exepath("rg")
if rg == "" then
  fail("rg が見つからない。telescope の grep_string は rg 必須なので、CI の依存 (Makefile の CI_COMMANDS_REST) を見ること")
end

local dir = vim.fn.tempname()
-- 同じ語を同じ形で全ファイルへ置く。出る / 出ないを分けるのはタイプ指定だけ
local fixtures = {
  { "app/models/loader.rb",         "  wrap_error do",          true },
  { "lib/tasks/import.rake",        "  wrap_error { }",         true },
  { "app/views/show.json.jbuilder", "json.x wrap_error",        true },
  { "app/views/show.html.erb",      "<%= wrap_error %>",        true },
  { "app/views/show.html.haml",     "  = wrap_error",           true },
  { "app/views/show.html.slim",     "  = wrap_error",           true },
  { "Rakefile",                     "wrap_error",               true },
  { "README.md",                    "`wrap_error` の使い方",    false },
  { "config/locales/ja.yml",        "note: wrap_error",         false },
  { "app/assets/app.js",            "// wrap_error",            false },
  { "package.json",                 '{ "x": "wrap_error" }',    false },
}
local want_hits, want_n = {}, 0
for _, f in ipairs(fixtures) do
  local full = dir .. "/" .. f[1]
  vim.fn.mkdir(vim.fs.dirname(full), "p")
  vim.fn.writefile({ f[2] }, full)
  if f[3] then
    want_hits[f[1]] = true
    want_n = want_n + 1
  end
end

local action = lsp.references_action("ruby", "wrap_error", dir)
local cmd = { rg, "--no-heading", "--line-number", "--color=never" }
vim.list_extend(cmd, action.additional_args or {})
cmd[#cmd + 1] = action.word_match
-- 🚨 パス引数を省くと rg は stdin を読む (tty でないと 0 バイト検索になり、黙って 0 件を返す)
vim.list_extend(cmd, { "--", action.search, "." })
local shown = table.concat(cmd, " ")
local res = vim.system(cmd, { cwd = action.cwd, text = true }):wait()
-- rc: 0=マッチあり / 1=マッチ無し / 2=エラー。1 も 2 も「絞り込みが壊れている」
if res.code ~= 0 then
  fail(("rg が rc=%d。タイプ名の綴り / --type-add の書式を見ること [%s] %s"):format(
    res.code, shown, ((res.stderr or "") .. (res.stdout or "")):gsub("%s+", " ")))
end

local got_hits, got_n = {}, 0
for line in (res.stdout or ""):gmatch("[^\n]+") do
  local file = line:match("^%./([^:]+):%d+:")
  if not file then fail(("rg の出力を読めない: %q [%s]"):format(line, shown)) end
  if not got_hits[file] then
    got_hits[file] = true
    got_n = got_n + 1
  end
end
for rel in pairs(want_hits) do
  if not got_hits[rel] then
    fail(("rg が %s を拾わない。Ruby 系のファイルが検索対象から落ちている [%s]"):format(rel, shown))
  end
end
for rel in pairs(got_hits) do
  if not want_hits[rel] then
    fail(("rg が %s を拾った。Ruby 以外のファイルが参照候補に混ざる [%s]"):format(rel, shown))
  end
end
-- 🚨 fixture が縮むと「拾わないこと」の主張だけが残って緑になる。数を固定して気づけるようにする
if want_n ~= 7 or got_n ~= 7 then
  fail(("fixture の件数が想定と違う (want %d / got %d、期待 7)。fixture を減らすと検出力が落ちる"):format(want_n, got_n))
end
vim.fn.delete(dir, "rf")

print(("OK lsp references dispatch: 判定 %d ケース / 行き先の返り値 %d ケース / root の選択 3 ケース / <C-k> の配線 / rg の絞り込み %d ファイル中 %d 件"):format(
  checked, #action_cases, #fixtures, got_n))
