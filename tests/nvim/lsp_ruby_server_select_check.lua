-- Ruby の LSP サーバ選択 (nvim/lua/dotfiles/lsp.lua の M.ruby_server_for / servers.*.root_dir /
-- M.mason_packages) の headless 検証。test_lsp_ruby_server_select.sh から dofile される。
-- 守っている不変条件:
--
--   1. 1 つの project root に対し ruby_lsp / solargraph の **ちょうど一方だけ** が on_dir を呼ぶ。
--      両方呼ぶと rubocop 由来の診断が二重に出る。どちらも呼ばないと Ruby の補完・定義ジャンプ・
--      整形が丸ごと消える。どちらも無言で起きるので、人間が気づくのは大抵ずっと後になる。
--   2. ruby_lsp を選ぶのは「Gemfile がある」かつ「その project の ruby で ruby-lsp が起動できる」
--      の両方が成立するときだけ。片方でも欠けたら solargraph へ落ちる (未導入の project で
--      LSP が丸ごと消えるのを防ぐ)。
--   3. 起動判定は rc と stdout の両方を見る。rbenv の shim は未導入でも存在し、その場合
--      rc=127 + stderr にだけメッセージを出す (実測 2026-09-08)。rc だけ / 出力の有無だけでは
--      「未導入の project を ruby_lsp と判定する」形で外れる。
--   4. プローブは root ごとに 1 回だけ走る。root_dir は BufReadPre から同期で呼ばれるので、
--      バッファを開くたびに 0.13s の subprocess を足すと編集が止まる。
--   6. プローブは判定対象の root を cwd にして `ruby-lsp --version` を走らせる。cwd を渡さないと
--      nvim の cwd の ruby を測ることになり、この判定の存在意義が消える (しかも無言で)。
--   7. ruby_lsp を選ぶのは root が **git repo の root そのもの** のときだけ。vim.fs.root は marker を
--      順に上方向探索するので Gemfile 先頭では「一番近い Gemfile」が勝ち、Gemfile を同梱した gem の
--      ソースへ飛ぶと gem ディレクトリが root になる。ruby-lsp はそこへ .ruby-lsp/ を掘って
--      bundle install を走らせるので、vendor ツリーや rbenv の gems ツリーに書き込みが起きる。
--   5. ruby_lsp は mason の ensure_installed に入らない。mason の ruby で走ると project と
--      ABI がズレて索引が壊れる (issues/332)。ただし enable 対象 (server_packages のキー) には
--      残る — 落とすと attach しなくなる。
--
-- filesystem にも subprocess にも触らない。vim.fs.root と、lsp.lua が公開している
-- 環境問い合わせ (has_gemfile / ruby_lsp_runnable) を差し替えて判定だけを固定する。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local lsp = require("dotfiles.lsp")

-- 差し替えたものを必ず戻してから fail する (戻さないと後続テストへ漏れる)
local orig = {
  root = vim.fs.root,
  system = vim.system,
  has_gemfile = lsp.has_gemfile,
  runnable = lsp.ruby_lsp_runnable,
}
local function restore()
  vim.fs.root = orig.root
  vim.system = orig.system
  lsp.has_gemfile = orig.has_gemfile
  lsp.ruby_lsp_runnable = orig.runnable
  lsp.ruby_server_cache = {}
end
local function fail_restoring(msg)
  restore()
  fail(msg)
end

-- 3. 起動判定が rc と stdout の両方を見ているか (本物の M.ruby_lsp_runnable を vim.system 越しに見る)
local system_cases = {
  { rc = 0, stdout = "0.26.11\n", stderr = "", want = true, why = "導入済み" },
  { rc = 127, stdout = "", stderr = "rbenv: ruby-lsp: command not found\n", want = false, why = "shim はあるが gem 未導入" },
  { rc = 0, stdout = "", stderr = "", want = false, why = "rc=0 だがバージョンを名乗らない" },
  { rc = 1, stdout = "0.26.11\n", stderr = "", want = false, why = "出力はあるが異常終了" },
  { spawn_error = true, want = false, why = "PATH に無く spawn 自体が失敗 (vim.system が error を投げる)" },
  { wait_nil = true, want = false, why = "timeout / 割り込みで wait が nil を返す (_system.lua)" },
}
local seen -- スタブが受け取った引数。捨てると「cwd を渡さない」変異が緑で通る
for _, c in ipairs(system_cases) do
  seen = nil
  vim.system = function(cmd, opts)
    seen = { cmd = cmd, opts = opts }
    if c.spawn_error then error("ENOENT: no such file or directory") end
    return {
      wait = function(_, timeout)
        seen.timeout = timeout
        if c.wait_nil then return nil end
        return { code = c.rc, stdout = c.stdout, stderr = c.stderr }
      end,
    }
  end
  local got = lsp.ruby_lsp_runnable("/tmp/probe")
  if got ~= c.want then
    fail_restoring(("ruby_lsp_runnable: rc=%s stdout=%q -> %s, want %s (%s)"):format(
      tostring(c.rc), c.stdout or "", tostring(got), tostring(c.want), c.why))
  end
  -- 6. プローブは「その project の ruby」で走らなければ意味がない。cwd を渡さない / nvim の cwd で
  --    走らせる変異は、この assert が無いとスイート全体緑で通る (この変更の唯一の前提)。
  if not seen then
    fail_restoring("vim.system が呼ばれていない。プローブが実行されていない")
  end
  if not vim.deep_equal(seen.cmd, { "ruby-lsp", "--version" }) then
    fail_restoring(("プローブのコマンドが %s。{\"ruby-lsp\", \"--version\"} であること (引数を落とすと実サーバが起動して stdin 待ちになる)"):format(vim.inspect(seen.cmd)))
  end
  if not seen.opts or seen.opts.cwd ~= "/tmp/probe" then
    fail_restoring(("プローブの cwd が %s。判定対象の root であること (nvim の cwd で測ると全 project で同じ答えになる)"):format(
      vim.inspect(seen.opts and seen.opts.cwd)))
  end
  if not c.spawn_error and type(seen.timeout) ~= "number" then
    fail_restoring("wait に上限が渡されていない。BufReadPre から同期で呼ばれるので無制限に待たせない")
  end
end
vim.system = orig.system

-- 2. Gemfile の有無 × 起動可否の 4 象限
local cases = {
  { dir = "/tmp/p-both", gemfile = true, runnable = true, want = "ruby_lsp", why = "Gemfile あり + 起動できる" },
  { dir = "/tmp/p-no-gem", gemfile = true, runnable = false, want = "solargraph", why = "Gemfile あるが ruby-lsp 未導入" },
  { dir = "/tmp/p-no-gemfile", gemfile = false, runnable = true, want = "solargraph", why = "Gemfile が無い (.git だけの root)" },
  { dir = "/tmp/p-neither", gemfile = false, runnable = false, want = "solargraph", why = "どちらも無い" },
}

local probe_calls = {}
lsp.has_gemfile = function(dir)
  for _, c in ipairs(cases) do
    if c.dir == dir then return c.gemfile end
  end
  return false
end
lsp.ruby_lsp_runnable = function(dir)
  probe_calls[dir] = (probe_calls[dir] or 0) + 1
  for _, c in ipairs(cases) do
    if c.dir == dir then return c.runnable end
  end
  return false
end

lsp.ruby_server_cache = {}
for _, c in ipairs(cases) do
  local got = lsp.ruby_server_for(c.dir)
  if got ~= c.want then
    fail_restoring(("ruby_server_for(%q) = %q, want %q (%s)"):format(c.dir, got, c.want, c.why))
  end
end

-- 4. 同じ root を何度引いてもプローブは 1 回だけ
for _ = 1, 3 do
  for _, c in ipairs(cases) do lsp.ruby_server_for(c.dir) end
end
for _, c in ipairs(cases) do
  if c.gemfile and probe_calls[c.dir] ~= 1 then
    fail_restoring(("root=%q のプローブが %d 回走った。root ごとに 1 回であること (同期 subprocess)"):format(
      c.dir, probe_calls[c.dir] or 0))
  end
  if not c.gemfile and probe_calls[c.dir] then
    fail_restoring(("root=%q は Gemfile が無いのにプローブが走った (先に落とすこと)"):format(c.dir))
  end
end

-- 1. root_dir 経由の排他
local buf = vim.api.nvim_create_buf(false, true)
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

restore()

-- 7. 実ファイルシステムでの root ゲート。ここだけは vim.fs.root を差し替えない
-- (marker の順序そのものが load-bearing なので、定数関数に置き換えると検査できない)。
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
-- macOS の一時領域は /var -> /private/var の symlink 配下にある。vim.fs.root は実パスを返すので、
-- 期待値も実パスへ揃える (揃えないと「ゲートは効いているのに文字列だけ食い違う」赤になる)
tmp = vim.uv.fs_realpath(tmp) or tmp
local function mk(rel, is_dir)
  local path = tmp .. "/" .. rel
  vim.fn.mkdir(vim.fs.dirname(path), "p")
  if is_dir then vim.fn.mkdir(path, "p") else vim.fn.writefile({ "" }, path) end
  return path
end
local function fail_fs(msg)
  restore()
  vim.fn.delete(tmp, "rf")
  fail(msg)
end

mk("repo/.git", true)
mk("repo/Gemfile")
local in_repo = mk("repo/app/models/x.rb")
-- vendor 配下の gem。gem 自身が Gemfile を同梱している形 (実測: ubiregi-server の vendor に 156 件)
mk("repo/vendor/bundle/ruby/3.1.0/gems/json-1.0/Gemfile")
local in_vendor = mk("repo/vendor/bundle/ruby/3.1.0/gems/json-1.0/lib/json.rb")
-- git repo の外にある gem ツリー (rbenv の gems 相当。実測 152 件が Gemfile 同梱)
mk("loose/gems/foo-1.0/Gemfile")
local outside = mk("loose/gems/foo-1.0/lib/foo.rb")

-- プローブは常に成功させる (ここで見たいのは root ゲートであって起動可否ではない)
lsp.ruby_lsp_runnable = function() return true end
lsp.has_gemfile = orig.has_gemfile
lsp.ruby_server_cache = {}

local fs_cases = {
  { file = in_repo, want = "ruby_lsp", root = tmp .. "/repo", why = "repo root (Gemfile と .git が同じ場所)" },
  { file = in_vendor, want = "solargraph", root = tmp .. "/repo/vendor/bundle/ruby/3.1.0/gems/json-1.0",
    why = "vendor 配下の gem。root が repo root でないので ruby_lsp を選ばない" },
  { file = outside, want = "solargraph", root = tmp .. "/loose/gems/foo-1.0",
    why = "git repo の外の gem ツリー。.git が無いので ruby_lsp を選ばない" },
}
for _, c in ipairs(fs_cases) do
  local b = vim.api.nvim_create_buf(false, false)
  vim.api.nvim_buf_set_name(b, c.file)
  local called = {}
  for _, name in ipairs({ "ruby_lsp", "solargraph" }) do
    lsp.servers[name].root_dir(b, function(d) called[name] = d end)
  end
  local n = (called.ruby_lsp and 1 or 0) + (called.solargraph and 1 or 0)
  if n ~= 1 then
    fail_fs(("file=%q で on_dir を呼んだサーバが %d 個 (ruby_lsp=%s solargraph=%s)。ちょうど 1 個であること (%s)"):format(
      c.file, n, tostring(called.ruby_lsp), tostring(called.solargraph), c.why))
  end
  if called[c.want] ~= c.root then
    fail_fs(("file=%q は %s が root=%q を受け取るはず (%s)。実際: ruby_lsp=%s solargraph=%s"):format(
      c.file, c.want, c.root, c.why, tostring(called.ruby_lsp), tostring(called.solargraph)))
  end
end
vim.fn.delete(tmp, "rf")
restore()

-- 5. mason には渡さない / enable 対象には残る
-- キーの有無を先に見る (後ろに置くと ~= false 側が先に発火して、この行は永久に実行されない)
if lsp.server_packages.ruby_lsp == nil then
  fail("server_packages から ruby_lsp のキーが消えている。enable_available が見るのはキーなので attach しなくなる")
end
if lsp.server_packages.ruby_lsp ~= false then
  fail(("server_packages.ruby_lsp = %s。mason 管理外を表す false であること (mason 版は ABI がズレる)"):format(
    tostring(lsp.server_packages.ruby_lsp)))
end
local mason = lsp.mason_packages()
-- 期待集合はテーブルから導出する (定数で焼くと、2 つ目の mason 管理外サーバを足したときに
-- フィルタが正しいのにテストが落ち、次の人が定数を +1 する方へ誘導される)
local want_pkgs = {}
for _, pkg in pairs(lsp.server_packages) do
  if pkg then table.insert(want_pkgs, pkg) end
end
table.sort(want_pkgs)
if not vim.deep_equal(mason, want_pkgs) then
  fail(("mason_packages() = %s, want %s (false のサーバだけを落とし、昇順で返すこと)"):format(
    vim.inspect(mason), vim.inspect(want_pkgs)))
end
for _, pkg in ipairs(mason) do
  if pkg == "ruby-lsp" then
    fail("mason_packages() に ruby-lsp が入っている。mason の ruby で走ると索引が壊れる (issues/332)")
  end
  if type(pkg) ~= "string" then
    fail(("mason_packages() に文字列でない要素がある: %s"):format(vim.inspect(pkg)))
  end
end
if not vim.tbl_contains(mason, "solargraph") then
  fail("mason_packages() に solargraph が無い。フィルタが落としすぎている")
end

print(("OK ruby lsp server select: %d root で排他を確認 (Gemfile × 起動可否の 4 象限 / 起動判定 %d ケース / プローブの cmd・cwd・キャッシュ / 実 fs の root ゲート %d ケース / mason %d 件から ruby-lsp を除外)"):format(
  checked, #system_cases, #fs_cases, #mason))
