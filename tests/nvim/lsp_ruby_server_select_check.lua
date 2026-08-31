-- Ruby の LSP サーバ選択 (nvim/lua/dotfiles/lsp.lua の M.ruby_server_for / servers.*.root_dir) の
-- headless 検証。test_lsp_ruby_server_select.sh から dofile される。守っている不変条件:
--
--   1. 1 つの project root に対し ruby_lsp / solargraph の **ちょうど一方だけ** が on_dir を呼ぶ。
--      両方呼ぶと rubocop 由来の診断が二重に出る。どちらも呼ばないと Ruby の補完・定義ジャンプ・
--      整形が丸ごと消える。どちらも無言で起きるので、人間が気づくのは大抵ずっと後になる。
--   2. allowlist の前方一致が "/" 境界を要求する。素の startswith だと ~/src/foo-old のような
--      兄弟が巻き込まれ、solargraph 運用の project が黙って ruby_lsp に切り替わる。
--   3. allowlist の要素が正規化される (~ 展開 + 末尾スラッシュ除去)。末尾スラッシュ付きで 1 行
--      足すと dir == root も前方一致も両方外れ、その project だけ無言で solargraph に落ちる。
--
-- filesystem には触らない。vim.fs.root を差し替えて root 解決だけを固定する。root_dir が on_dir を
-- 呼ぶかどうかが nvim 側の attach 判断そのものなので、そこを pin すれば LSP サーバのバイナリも
-- fixture repo も要らない (実 attach の確認は導入時に手で済ませており、ここで守るのは選択規則)。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local lsp = require("dotfiles.lsp")

-- allowlist が空だと 1 も 2 も自明に真になり、このテストが何も守らなくなる
if type(lsp.ruby_lsp_projects) ~= "table" or #lsp.ruby_lsp_projects == 0 then
  fail("M.ruby_lsp_projects が空。全 project が solargraph になり、本テストは何も検査しない")
end

-- 3. 正規化 (allowlist の実要素と、正規化関数そのものの両方を見る)
for _, root in ipairs(lsp.ruby_lsp_projects) do
  if root:match("/$") then
    fail(("allowlist の要素に末尾スラッシュが残っている: %q"):format(root))
  end
  if root:match("^~") then
    fail(("allowlist の要素の ~ が展開されていない: %q"):format(root))
  end
end
if lsp.normalize_project_root("~/x/") ~= lsp.normalize_project_root("~/x") then
  fail("normalize_project_root が末尾スラッシュを吸収していない")
end
if lsp.normalize_project_root("~/x"):match("^~") then
  fail("normalize_project_root が ~ を展開していない")
end

-- 1/2. 判定と、root_dir 経由の排他
local inside = lsp.ruby_lsp_projects[1]
local cases = {
  { dir = inside, want = "ruby_lsp", why = "allowlist の root そのもの" },
  { dir = inside .. "/server", want = "ruby_lsp", why = "allowlist 配下のサブディレクトリ" },
  { dir = inside .. "-old", want = "solargraph", why = "同じ prefix を持つ兄弟 (/ 境界)" },
  { dir = "/tmp/not-in-the-allowlist", want = "solargraph", why = "allowlist 外" },
}

for _, c in ipairs(cases) do
  local got = lsp.ruby_server_for(c.dir)
  if got ~= c.want then
    fail(("ruby_server_for(%q) = %q, want %q (%s)"):format(c.dir, got, c.want, c.why))
  end
end

local buf = vim.api.nvim_create_buf(false, true)
local orig_root = vim.fs.root
-- 差し替えた vim.fs.root を必ず戻してから fail する (戻さないと後続テストへ漏れる)
local function fail_restoring(msg)
  vim.fs.root = orig_root
  fail(msg)
end

local checked = 0
for _, c in ipairs(cases) do
  vim.fs.root = function() return c.dir end
  local called = {}
  for _, name in ipairs({ "ruby_lsp", "solargraph" }) do
    local cfg = lsp.servers[name]
    if not cfg or type(cfg.root_dir) ~= "function" then
      fail_restoring(("M.servers.%s.root_dir が関数でない。排他は root_dir 経由でしか効かない"):format(name))
    end
    cfg.root_dir(buf, function(d) called[name] = d end)
  end
  local n = (called.ruby_lsp and 1 or 0) + (called.solargraph and 1 or 0)
  if n ~= 1 then
    fail_restoring(("root=%q で on_dir を呼んだサーバが %d 個 (ruby_lsp=%s solargraph=%s)。ちょうど 1 個であること"):format(
      c.dir, n, tostring(called.ruby_lsp), tostring(called.solargraph)))
  end
  if called[c.want] ~= c.dir then
    fail_restoring(("root=%q は %s が %s を受け取るはず (実際: ruby_lsp=%s solargraph=%s)"):format(
      c.dir, c.want, c.dir, tostring(called.ruby_lsp), tostring(called.solargraph)))
  end
  checked = checked + 1
end

-- root が決まらない (Gemfile も .git も無い) ときはどちらも attach しない
vim.fs.root = function() return nil end
local none = {}
for _, name in ipairs({ "ruby_lsp", "solargraph" }) do
  lsp.servers[name].root_dir(buf, function(d) none[name] = d end)
end
if none.ruby_lsp or none.solargraph then
  fail_restoring(("root 未解決なのに on_dir が呼ばれた (ruby_lsp=%s solargraph=%s)"):format(
    tostring(none.ruby_lsp), tostring(none.solargraph)))
end
vim.fs.root = orig_root

print(("OK ruby lsp server select: %d root で排他を確認 (allowlist %d 件。正規化 / \"/\" 境界 / root 未解決も検査)"):format(
  checked, #lsp.ruby_lsp_projects))
