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
--   8. プローブと実サーバは **同じ env** (M.ruby_env) で走る。RBENV_VERSION を空にしないと
--      `rbenv shell` した端末では全 project が同じ ruby で判定され、mason bin を PATH から
--      外さないと mason 版 ruby-lsp が rbenv shim に勝つ。片方だけに渡すと「プローブは通るが
--      server は別の ruby で走る」= 332 の病気に戻る (issues/337 の 3 と 5)。
--   9. ruby_lsp が異常終了したら solargraph へ倒す。プローブ (`--version`) は起動経路を通らない
--      ので「プローブは通るが server は起動しない」が起きうる。倒さないと Ruby の LSP が消える。
--      :LspStop / nvim 終了 (SIGTERM) と正常終了では倒さない (issues/337 の 1)。
--  10. 選択の内訳 (:RubyLspInfo) は判定関数そのものを呼ぶ。表示側が式を写すと、片方だけ変えた
--      ときに「なぜそう選ばれたか」の説明だけが嘘になる。表示は状態を書き換えない (issues/337 の 4)。
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
  ruby_env = lsp.ruby_env,
  notify = vim.notify,
  cmd = vim.cmd,
  get_client_by_id = vim.lsp.get_client_by_id,
  schedule = vim.schedule,
  rpc_start = vim.lsp.rpc.start,
}
local function restore()
  vim.fs.root = orig.root
  vim.system = orig.system
  lsp.has_gemfile = orig.has_gemfile
  lsp.ruby_lsp_runnable = orig.runnable
  lsp.ruby_env = orig.ruby_env
  vim.notify = orig.notify
  vim.cmd = orig.cmd
  vim.lsp.get_client_by_id = orig.get_client_by_id
  vim.schedule = orig.schedule
  vim.lsp.rpc.start = orig.rpc_start
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
-- 8. プローブが M.ruby_env() を通した env で走ることを、値でなく**出所**で固定する。
--    ここで実 PATH を検査すると CI の PATH 次第で意味が変わるので、ruby_env を目印に差し替えて
--    「その戻り値がそのまま vim.system へ渡るか」を見る (env を渡さない変異はここで red)。
lsp.ruby_env = function() return { RBENV_VERSION = "", PATH = "/marker/probe" } end
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
  local penv = seen.opts and seen.opts.env
  if not penv or penv.PATH ~= "/marker/probe" or penv.RBENV_VERSION ~= "" then
    fail_restoring(("プローブの env が %s。M.ruby_env() の戻り値をそのまま渡すこと (渡さないと `rbenv shell` した端末で全 project が同じ ruby で判定され、mason 版 ruby-lsp が shim に勝つ)"):format(
      vim.inspect(penv)))
  end
end
vim.system = orig.system
lsp.ruby_env = orig.ruby_env

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
-- 🚨 bundler の git ソース gem。**自分の .git を持つ** (bundler が clone するため) ので、
--    「root が git repo の root か」だけのゲートは通ってしまう。実測 2026-09-09: rbenv と
--    ubiregi-server の vendor を合わせて bundler/gems 配下の 21/21 件が .git と Gemfile を
--    両方持つ。ここに .git を置いていなかったので、この退行は fixture から見えなかった。
mk("repo/vendor/bundle/ruby/3.1.0/bundler/gems/axlsx-abc123/.git", true)
mk("repo/vendor/bundle/ruby/3.1.0/bundler/gems/axlsx-abc123/Gemfile")
local git_gem = mk("repo/vendor/bundle/ruby/3.1.0/bundler/gems/axlsx-abc123/lib/axlsx.rb")

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
  { file = git_gem, want = "solargraph",
    root = tmp .. "/repo/vendor/bundle/ruby/3.1.0/bundler/gems/axlsx-abc123",
    why = "bundler の git ソース gem。**自分の .git を持つ**ので git-root ゲートは通る。gems セグメントで弾くこと (通すと vendor ツリーに .ruby-lsp/ を掘り、bundle install が走る)" },
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

-- 8. M.ruby_env 本体。PATH は合成値で見る (実 PATH に依存させると CI で意味が変わる)
local mason_bin = lsp.mason_bin()
if type(mason_bin) ~= "string" or mason_bin == "" then
  fail(("mason_bin() = %s。_nviminit.lua が PATH へ入れる文字列と同じ出典であること"):format(vim.inspect(mason_bin)))
end
local env_cases = {
  { path = mason_bin .. ":/usr/bin:/bin", want = "/usr/bin:/bin", why = "先頭の mason bin を落とす" },
  { path = "/usr/bin:" .. mason_bin .. ":/bin", want = "/usr/bin:/bin", why = "途中の mason bin も落とす" },
  { path = "/usr/bin:/bin", want = "/usr/bin:/bin", why = "mason bin が無ければ素通し" },
  { path = "/usr/bin::/bin:", want = "/usr/bin:/bin", why = "空エントリ (= cwd) は落とす" },
  { path = mason_bin .. "/other:/bin", want = mason_bin .. "/other:/bin", why = "前方一致では落とさない" },
}
for _, c in ipairs(env_cases) do
  local got = lsp.ruby_env(c.path, mason_bin)
  if got.PATH ~= c.want then
    fail(("ruby_env(%q).PATH = %q, want %q (%s)"):format(c.path, got.PATH, c.want, c.why))
  end
  if got.RBENV_VERSION ~= "" then
    fail(("ruby_env(%q).RBENV_VERSION = %s。空文字であること (rbenv は空を未設定として扱い project の .ruby-version へ戻る。unset は vim.system の env では表現できない)"):format(
      c.path, vim.inspect(got.RBENV_VERSION)))
  end
end
-- 🚨 production の呼び出しは 3 箇所とも **引数ゼロ** (ruby_lsp_runnable / servers.ruby_lsp.cmd /
--    ruby_info_lines)。上の env_cases は必ず 2 引数で呼ぶので、既定値の配線
--    (`mason_bin or M.mason_bin()` / `path or vim.env.PATH`) が丸ごと無検査になる。
--    `or ""` へ変異すると mason 除外が死ぬ / 子プロセスの PATH が空になるのに全部緑。
local real_path = vim.env.PATH
vim.env.PATH = mason_bin .. ":/usr/bin:/bin"
local defaulted = lsp.ruby_env()
vim.env.PATH = real_path
if defaulted.PATH ~= "/usr/bin:/bin" then
  fail(("引数ゼロの ruby_env().PATH = %q, want \"/usr/bin:/bin\" (既定値の配線が切れている: vim.env.PATH を読み、M.mason_bin() を除外すること)"):format(
    tostring(defaulted.PATH)))
end
if defaulted.RBENV_VERSION ~= "" then
  fail("引数ゼロの ruby_env() が RBENV_VERSION を空にしていない")
end

-- _nviminit.lua が mason bin を自前で組み立て直していないこと。組み立て直すと文字列が
-- ズレたときに除外が無言で効かなくなる (2 ファイルに同じ判断が生える)。
-- 🚨 コメント行は落としてから見る。落とさないと (a) 説明コメントの "lsp.mason_bin()" だけで
--    後半の検査が満たされ (vacuous)、(b) どこかのコメントに "mason/bin" と書いた瞬間に
--    前半が偽陽性で落ちる。検査したいのは**コード**であってコメントではない。
local init_code = {}
for _, line in ipairs(vim.fn.readfile(vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":p:h:h:h") .. "/_nviminit.lua")) do
  if not line:match("^%s*%-%-") then table.insert(init_code, line) end
end
init_code = table.concat(init_code, "\n")
if init_code:find("mason/bin", 1, true) then
  fail("_nviminit.lua のコードが \"mason/bin\" を直接組み立てている。lsp.mason_bin() を使うこと (ruby_env の除外と文字列が一致しなくなる)")
end
if not init_code:find("mason_bin()", 1, true) then
  fail("_nviminit.lua のコードが lsp.mason_bin() を呼んでいない。PATH へ入れる文字列と除外する文字列の出典が分かれる")
end

-- 8b. 実サーバの cmd も同じ env / cwd で起動する (プローブだけ直しても意味がない)
local ruby_cfg = lsp.servers.ruby_lsp
if type(ruby_cfg.cmd) ~= "function" then
  fail(("servers.ruby_lsp.cmd が %s。関数であること (table にすると nvim は cmd_env を使うが、_nviminit.lua の enable_available が executable() 判定の枝に入る)"):format(type(ruby_cfg.cmd)))
end
lsp.ruby_env = function() return { RBENV_VERSION = "", PATH = "/marker/server" } end
local spawned
vim.lsp.rpc.start = function(cmd, _, extra) spawned = { cmd = cmd, extra = extra } end
ruby_cfg.cmd({}, { root_dir = "/tmp/p-both" })
lsp.ruby_env = orig.ruby_env
vim.lsp.rpc.start = orig.rpc_start
if not spawned then
  fail_restoring("servers.ruby_lsp.cmd が vim.lsp.rpc.start を呼んでいない")
end
if not vim.deep_equal(spawned.cmd, { "ruby-lsp" }) then
  fail_restoring(("cmd が %s。{\"ruby-lsp\"} であること"):format(vim.inspect(spawned.cmd)))
end
if not spawned.extra or spawned.extra.cwd ~= "/tmp/p-both" then
  fail_restoring(("サーバの cwd が %s。root であること (cwd が変わると rbenv が読む .ruby-version が変わる)"):format(
    vim.inspect(spawned.extra and spawned.extra.cwd)))
end
if not spawned.extra.env or spawned.extra.env.PATH ~= "/marker/server"
  or spawned.extra.env.RBENV_VERSION ~= "" then
  fail_restoring(("サーバの env が %s。M.ruby_env() を渡すこと (cmd が関数のとき nvim は cmd_env を使わないので、ここで渡さなければプローブと違う ruby で走る)"):format(
    vim.inspect(spawned.extra.env)))
end
-- cmd_cwd が来たら優先する (lspconfig の reuse_client が書く)
vim.lsp.rpc.start = function(_, _, extra) spawned = { extra = extra } end
ruby_cfg.cmd({}, { root_dir = "/tmp/p-both", cmd_cwd = "/tmp/reused" })
vim.lsp.rpc.start = orig.rpc_start
if spawned.extra.cwd ~= "/tmp/reused" then
  fail_restoring(("cmd_cwd があるのに cwd が %s。cmd_cwd を優先すること (reuse_client が書く)"):format(
    vim.inspect(spawned.extra.cwd)))
end

-- 9. 起動に失敗したら solargraph へ倒す
local reason_cases = {
  { code = 0, signal = 0, want = false, why = "正常終了" },
  { code = 0, signal = 15, want = false, why = "graceful が timeout して terminate された" },
  { code = 143, signal = 15, want = false, why = "SIGTERM で殺されたときの exit code" },
  { code = 78, signal = 0, want = true, why = "Gemfile.lock が無い (exe/ruby-lsp の BundleNotLocked)" },
  { code = 1, signal = 0, want = true, why = "その他の異常終了" },
  { code = 0, signal = 6, want = true, why = "SIGABRT" },
  -- 🚨 これが本命。client:stop() 経由の停止では ruby-lsp は **exit=1 signal=0** で終わる
  --    (実測 2026-09-09)。signal だけを見ていると :LspStop と :RubyLspReset が自分で
  --    フォールバックを焚き、「選び直す」コマンドが root を solargraph に固定してしまう。
  { code = 1, signal = 0, stopping = true, want = false, why = ":LspStop / :RubyLspReset / nvim 終了" },
  { code = 78, signal = 0, stopping = true, want = false, why = "意図した停止は理由によらず倒さない" },
}
for _, c in ipairs(reason_cases) do
  local got = lsp.ruby_lsp_fallback_reason(c.code, c.signal, c.stopping)
  if (got ~= nil) ~= c.want then
    fail_restoring(("ruby_lsp_fallback_reason(%d, %d, stopping=%s) = %s, 倒す=%s であること (%s)"):format(
      c.code, c.signal, tostring(c.stopping), vim.inspect(got), tostring(c.want), c.why))
  end
end
-- nvim の private フィールドに依存しているので、runtime 側が変わったらここで落とす。
-- 落ちなくなると「意図した停止でも倒す」へ無言で戻る (テストが守る対象は挙動でなく前提)。
local client_src = table.concat(vim.fn.readfile(vim.env.VIMRUNTIME .. "/lua/vim/lsp/client.lua"), "\n")
if not client_src:find("self._is_stopping = true", 1, true) then
  fail_restoring("nvim runtime の Client:stop が _is_stopping を立てなくなった。意図した停止の判定 (lsp.lua の on_exit) を見直すこと")
end
if not client_src:find("rpc.is_closing() or self._is_stopping", 1, true) then
  fail_restoring("nvim runtime の is_stopped の実装が変わった。is_stopped() が使えるようになったなら private フィールド依存をやめられる (lsp.lua の 🚨 を再評価)")
end
if not (lsp.ruby_lsp_fallback_reason(78, 0) or ""):find("bundle install", 1, true) then
  fail_restoring("exit 78 の理由に bundle install の案内が無い。プローブが必ず素通りする形なので対処法まで出す")
end

-- 倒したら **ruby_server_for が読む鍵と同じ鍵** が書き変わること。鍵がズレると
-- 「誰も読まないキーへ書いて全部緑」になる (プローブも走り直さない = 1 回で確定)
local fb_root = "/tmp/p-both"
lsp.ruby_server_cache = {}
lsp.has_gemfile = function() return true end
local fb_probes = 0
lsp.ruby_lsp_runnable = function() fb_probes = fb_probes + 1 return true end
if lsp.ruby_server_for(fb_root) ~= "ruby_lsp" then
  fail_restoring("前提が崩れている: 倒す前は ruby_lsp が選ばれているはず")
end
local notes, reattaches
notes, reattaches = {}, 0
local deps = {
  notify = function(msg) table.insert(notes, msg) end,
  reattach = function() reattaches = reattaches + 1 end,
}
if lsp.ruby_lsp_failed(fb_root, 0, 0, deps) ~= false then
  fail_restoring("正常終了で倒している (手で止めたら別サーバが起動することになる)")
end
if lsp.ruby_server_for(fb_root) ~= "ruby_lsp" then
  fail_restoring("正常終了なのにキャッシュが書き変わった")
end
if lsp.ruby_lsp_failed(fb_root, 78, 0, deps) ~= true then
  fail_restoring("exit 78 で倒していない")
end
if lsp.ruby_server_for(fb_root) ~= "solargraph" then
  fail_restoring(("倒した後の ruby_server_for(%q) = %q。solargraph であること (書いた鍵を ruby_server_for が読んでいない可能性)"):format(
    fb_root, lsp.ruby_server_for(fb_root)))
end
if fb_probes ~= 1 then
  fail_restoring(("倒した後にプローブが %d 回走った。1 回であること (倒した結果が確定していない)"):format(fb_probes))
end
if #notes ~= 1 or not notes[1]:find(fb_root, 1, true) then
  fail_restoring(("通知が %s。root を含めて 1 回出すこと (黙って倒すと、なぜサーバが変わったか分からない)"):format(vim.inspect(notes)))
end
if reattaches ~= 1 then
  fail_restoring(("再 attach を %d 回呼んだ。1 回であること (呼ばないと開いているバッファは LSP 無しのまま)"):format(reattaches))
end
-- solargraph が enable されていない窓 (mason 未導入 / 導入中) では「切り替えます」と言わない。
-- 言うと、何も起動しないのに嘘の説明がついて Ruby の LSP が消える
lsp.ruby_server_cache = {}
if lsp.ruby_server_for("/tmp/p-both") ~= "ruby_lsp" then fail_restoring("前提が崩れている (9c)") end
local msg2 = {}
lsp.ruby_lsp_failed("/tmp/p-both", 78, 0, {
  notify = function(m) table.insert(msg2, m) end,
  reattach = function() end,
  solargraph_enabled = function() return false end,
})
if #msg2 ~= 1 or msg2[1]:find("solargraph に切り替えます", 1, true) then
  fail_restoring(("solargraph が enable されていないのに「切り替えます」と言っている: %s"):format(vim.inspect(msg2)))
end
if not msg2[1]:find("solargraph も使えません", 1, true) then
  fail_restoring(("solargraph 不在のときの案内が出ていない: %s"):format(vim.inspect(msg2)))
end
lsp.ruby_server_cache = {}
if lsp.ruby_server_for(fb_root) ~= "ruby_lsp" then fail_restoring("前提が崩れている (9c-2)") end
notes, reattaches = {}, 0
lsp.ruby_lsp_failed(fb_root, 78, 0, deps)

-- 2 回目は何もしない (別のバッファで同じ root の client が落ちても通知は増えない)
if lsp.ruby_lsp_failed(fb_root, 78, 0, deps) ~= false or #notes ~= 1 or reattaches ~= 1 then
  fail_restoring("同じ root で 2 回倒している (通知と再 attach が積み上がる)")
end

-- 9b. on_exit の配線。client.root_dir を鍵にしていること
lsp.ruby_server_cache = {}
if lsp.ruby_server_for(fb_root) ~= "ruby_lsp" then fail_restoring("前提が崩れている (9b)") end
if type(ruby_cfg.on_exit) ~= "function" then
  fail_restoring("servers.ruby_lsp.on_exit が無い。プローブは起動の証明ではないので、落ちたことを検出する経路が要る")
end
local stub_client = { root_dir = fb_root, _is_stopping = false,
  -- 🚨 実物の is_stopped() は on_exit の時点で **クラッシュでも true** (rpc.is_closing() を
  --    見るため。実測 2026-09-09)。ここを true 固定にしておかないと、「_is_stopping の代わりに
  --    is_stopped() を使う」変異が「メソッドが無い」エラーで落ち、判定の誤りを検出したことに
  --    ならない (nil 呼び出しの赤とすり替わる)。
  is_stopped = function() return true end }
vim.lsp.get_client_by_id = function(id) return id == 7 and stub_client or nil end
vim.schedule = function(fn) fn() end -- on_exit は fast event context。中身をここで走らせる
local notified = 0
vim.notify = function() notified = notified + 1 end
local doautoall, doautoall_arg = 0, nil
-- 引数まで見る。"nvim.lsp.enable FileType" の **group 指定そのもの**が契約で、落とすと
-- 全 FileType autocmd を全バッファで焚くことになる (回数だけ数えると変異が緑で通る)
vim.cmd = setmetatable({ doautoall = function(a) doautoall = doautoall + 1; doautoall_arg = a end }, {})
-- 意図した停止 (:LspStop / :RubyLspReset) では倒さない。ruby-lsp はこの経路でも
-- exit=1 signal=0 を返すので、signal しか見ていないとここが緑にならない
stub_client._is_stopping = true
ruby_cfg.on_exit(1, 0, 7)
if lsp.ruby_server_cache[fb_root] ~= "ruby_lsp" or notified ~= 0 or doautoall ~= 0 then
  vim.lsp.get_client_by_id = orig.get_client_by_id; vim.schedule = orig.schedule
  vim.notify = orig.notify; vim.cmd = orig.cmd
  fail_restoring(("意図した停止 (_is_stopping=true) で倒している: cache=%s notify=%d doautoall=%d。:RubyLspReset が自分で root を solargraph に固定してしまう"):format(
    vim.inspect(lsp.ruby_server_cache[fb_root]), notified, doautoall))
end
stub_client._is_stopping = false
ruby_cfg.on_exit(78, 0, 7)
vim.lsp.get_client_by_id = orig.get_client_by_id
vim.schedule = orig.schedule
vim.notify = orig.notify
vim.cmd = orig.cmd
if lsp.ruby_server_cache[fb_root] ~= "solargraph" then
  fail_restoring(("on_exit 後のキャッシュが %s。client.root_dir を鍵に solargraph へ倒すこと"):format(
    vim.inspect(lsp.ruby_server_cache[fb_root])))
end
if notified ~= 1 or doautoall ~= 1 then
  fail_restoring(("on_exit の既定の副作用が notify=%d doautoall=%d。通知と再 attach を 1 回ずつ"):format(notified, doautoall))
end
if doautoall_arg ~= "nvim.lsp.enable FileType" then
  fail_restoring(("doautoall の引数が %s。\"nvim.lsp.enable FileType\" であること (group を落とすと全 FileType autocmd を全バッファで焚く)"):format(
    vim.inspect(doautoall_arg)))
end

-- 10. :RubyLspInfo の内訳は判定関数そのものを呼び、状態を書き換えない
lsp.ruby_server_cache = {}
local info_probes = 0
lsp.ruby_lsp_runnable = function() info_probes = info_probes + 1 return true end
vim.fs.root = function(_, markers)
  -- Gemfile 先頭でも .git 単独でも同じ dir = repo root そのもの
  return markers and "/tmp/p-both" or "/tmp/p-both"
end
local info_buf = vim.api.nvim_create_buf(false, true)
local lines = table.concat(lsp.ruby_info_lines(info_buf), "\n")
if info_probes ~= 0 then
  fail_restoring("RubyLspInfo がプローブを走らせた。報告が観測対象を書き換えている (cache_only)")
end
if not lines:find("未判定", 1, true) then
  fail_restoring(("未判定の root で %q。「未判定」と出すこと"):format(lines))
end
lsp.ruby_server_for("/tmp/p-both") -- ここで初めて判定させる
lines = table.concat(lsp.ruby_info_lines(info_buf), "\n")
if not lines:find("ruby_lsp", 1, true) or not lines:find("/tmp/p-both", 1, true) then
  fail_restoring(("内訳に選択と root が出ていない: %q"):format(lines))
end
if not lines:find("起動できた", 1, true) then
  fail_restoring(("内訳に理由が出ていない: %q (判定関数から導出すること)"):format(lines))
end
-- 判定が solargraph に倒れたら理由もそちらになる (表示が判定と別実装なら、ここでズレる)
lsp.ruby_server_cache = { ["/tmp/p-both"] = "solargraph" }
lines = table.concat(lsp.ruby_info_lines(info_buf), "\n")
if not lines:find("solargraph", 1, true) or lines:find("起動できた", 1, true) then
  fail_restoring(("倒した後の内訳が古い: %q"):format(lines))
end
vim.fs.root = orig.root

-- 10b. :RubyLspReset は「止めてから」選び直す (can_start は root_dir を見ないので、
--      止めずに選び直すと solargraph が残ったまま ruby_lsp が起動して二重 attach になる)
lsp.ruby_server_cache = { ["/tmp/p-both"] = "ruby_lsp" }
local calls, alive, alive2 = {}, 2, 2
local reset_ok = lsp.ruby_reset({
  enable = function(names, on)
    table.insert(calls, { names = names, on = on })
    -- 停止すると on_exit が走る。倒す判定が外れた場合にここで solargraph が書き戻される
    -- (実際に起きた: ruby-lsp は client:stop() でも exit=1 signal=0 を返す)。
    -- キャッシュを捨てるのが停止より**前**だと、この書き戻しが生き残って root が固定される。
    if on == false then alive = 0; lsp.ruby_server_cache["/tmp/p-both"] = "solargraph" end
  end,
  wait = function(_, cond) return cond() end,
  running = function() return alive end,
})
if next(lsp.ruby_server_cache) ~= nil then
  fail_restoring("ruby_reset がキャッシュを捨てていない")
end
if #calls ~= 2 or calls[1].on ~= false or calls[2].on ~= true then
  fail_restoring(("ruby_reset の enable 呼び出しが %s。false (停止) → true (選び直し) の順であること"):format(vim.inspect(calls)))
end
for _, call in ipairs(calls) do
  if not vim.tbl_contains(call.names, "ruby_lsp") or not vim.tbl_contains(call.names, "solargraph") then
    fail_restoring(("ruby_reset が %s を対象にしている。両方止めないと二重 attach が残る"):format(vim.inspect(call.names)))
  end
end
if reset_ok ~= true then
  fail_restoring("ruby_reset が停止を確認できていない (running が 0 になったのに false)")
end
-- 待機の述語そのものを pin する。上の 2 ケースは cond() の戻り値に依存しないので、
-- `running() == 0` を true に置き換える変異が緑で通ってしまう (= 「消えるまで待つ」が無検査)。
local probed = {}
lsp.ruby_reset({
  enable = function() end,
  wait = function(ms, cond, interval)
    probed.ms, probed.interval = ms, interval
    probed.first = cond()            -- まだ 2 本生きている → false であること
    alive2 = 0
    probed.second = cond()           -- 消えた → true であること
    return probed.second
  end,
  running = function() return alive2 end,
})
if probed.first ~= false or probed.second ~= true then
  fail_restoring(("ruby_reset の待機条件が running()==0 を見ていない (生存中=%s / 消滅後=%s)"):format(
    tostring(probed.first), tostring(probed.second)))
end
if type(probed.ms) ~= "number" or probed.ms <= 0 then
  fail_restoring(("ruby_reset の待機に上限が無い (%s)。無制限に待つと :RubyLspReset が固まる"):format(vim.inspect(probed.ms)))
end
-- 既定の running クロージャ (ruby_lsp と solargraph を数える式) は注入で一度も走らない。
-- 名前を片方落とす変異を捕まえるため、ここだけ実物を呼ぶ (headless では 0 本 = 0)。
local default_running_ok = pcall(function() lsp.ruby_reset({ enable = function() end, wait = function(_, cond) return cond() end }) end)
if not default_running_ok then
  fail_restoring("ruby_reset の既定 running クロージャが動かない (get_clients の呼び方が壊れている)")
end
-- 止まらなかったら false を返す (呼び出し側が「開き直して」と案内するため)
if lsp.ruby_reset({ enable = function() end, wait = function(_, cond) cond() return false end,
                    running = function() return 1 end }) ~= false then
  fail_restoring("クライアントが止まらないのに ruby_reset が true を返した")
end

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

-- --- issue 341: 「起動しうるか」の述語を 1 か所へ寄せた ------------------------------------
--
-- 🚨 以前は判定が _nviminit.lua にだけ在り、lsp.lua は vim.lsp.is_enabled を「起動しうるか」の
-- proxy として読んでいた。proxy が成り立つのは **cmd が table のサーバに限る**ので、
-- フォールバック先を関数 cmd のサーバへ変えた日に通知が黙って嘘に戻る形だった。
local binary_cases = {
  -- { 名前, cmd, executable の戻り, 期待 }
  { "table cmd + 実在", { "solargraph" }, 1, true },
  { "table cmd + 不在", { "solargraph" }, 0, false },
  { "関数 cmd は判定できないので true", function() end, 0, true },
  { "config が無いサーバも true", nil, 0, true },
}
for _, c in ipairs(binary_cases) do
  local name, cmd, exe, want = c[1], c[2], c[3], c[4]
  local got = lsp.server_binary_available("solargraph", {
    lsp_config = cmd ~= nil and { solargraph = { cmd = cmd } } or {},
    executable = function() return exe end,
  })
  if got ~= want then
    fail(("server_binary_available: %s → %s (want %s)"):format(name, tostring(got), tostring(want)))
  end
end

-- 🚨 **_nviminit.lua が同じ述語を呼んでいること**を静的に固定する。
-- seam が無い (enable_available は lazy.nvim の config 内のローカル関数で、外から呼べない) ので
-- ここだけはソースを読む。**行頭に固定**するのが要点 — 部分一致だとコメントアウトを素通しする
-- (issue 310 で実際に踏んだ形)。
local init_path = vim.env.DOTFILES_INIT or (vim.env.HOME .. "/dotfiles/_nviminit.lua")
local init_src = table.concat(vim.fn.readfile(init_path), "\n")
if not init_src:find("\n%s*if lsp%.server_binary_available%(name%) then") then
  fail(("%s の enable_available が lsp.server_binary_available を呼んでいない " ..
    "(判定が 2 実装に戻ると、片方だけ変わって通知が黙って嘘になる)"):format(init_path))
end

-- 🚨 **and で取っていること**。is_enabled だけを見る形へ戻すと、バイナリ不在でも
-- 「solargraph に切り替えます」と言う (= 嘘の説明つきで Ruby の LSP が消える)。
local notices = {}
lsp.ruby_server_cache["/tmp/p341"] = "ruby_lsp"
lsp.ruby_lsp_failed("/tmp/p341", 78, 0, {
  notify = function(msg) table.insert(notices, msg) end,
  reattach = function() end,
  solargraph_enabled = function() return true end,   -- enable はされている
  executable = function() return 0 end,              -- が、バイナリは不在
  lsp_config = { solargraph = { cmd = { "solargraph" } } },
}, false)
if #notices ~= 1 then
  fail(("通知が %d 件 (want 1)"):format(#notices))
elseif not notices[1]:find("solargraph も使えません", 1, true) then
  fail(("enable 済みでもバイナリ不在なら「使えません」と言うこと。実際: %s"):format(notices[1]))
end
lsp.ruby_server_cache["/tmp/p341"] = nil

print(("OK ruby lsp server select: %d root で排他を確認 (Gemfile × 起動可否の 4 象限 / 起動判定 %d ケース / プローブの cmd・cwd・env・キャッシュ / 実 fs の root ゲート %d ケース / mason %d 件から ruby-lsp を除外 / env %d ケース + 実サーバ cmd の env・cwd / 異常終了の判定 %d ケース + 倒す副作用と on_exit 配線 / :RubyLspInfo の内訳と :RubyLspReset の停止順)"):format(
  checked, #system_cases, #fs_cases, #mason, #env_cases, #reason_cases))
