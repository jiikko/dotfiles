-- ネイティブ LSP (vim.lsp) のキーマップ・診断・サーバ設定。
-- plugin spec (nvim-lspconfig の config、_nviminit.lua) から setup(capabilities) を呼ぶ。
--   - capabilities: blink.cmp が広告する補完 capability (nil 可)
--   - サーバの有効化 (vim.lsp.enable) は _nviminit.lua の enable_available() が
--     server_packages を見て行う。ここは "全サーバ共通の設定 (capabilities / on_attach) と、
--     サーバ固有 settings" だけを持つ。
-- キー割り当ては coc 時代の muscle memory を踏襲する (gd / gD / <C-j> / <C-k> / t / [g / ]g など)。
local M = {}

-- サーバ固有の設定 (settings / on_attach)。空テーブルのサーバは共通設定のみで足りるので列挙しない。
-- inlay hints は既定 off で、<leader>ih トグル (M.setup) で有効化する。サーバ側で hint 種別を
-- 明示 on にしないと出ないためここで settings を渡す。仮想テキスト描画なので termguicolors=off の
-- 256色端末でも表示できる (色は淡くなる)。
-- TS/JS は同一の inlay hint 設定なので共通 table を1つ参照する (片方だけ変えて drift する事故を防ぐ)。
-- lspconfig は settings を読むだけで mutate しないため table 共有で挙動不変。
local ts_js_inlay_hints = {
  includeInlayParameterNameHints = "all",
  includeInlayVariableTypeHints = true,
  includeInlayFunctionParameterTypeHints = true,
  includeInlayFunctionLikeReturnTypeHints = true,
}

-- Ruby は project ごとに 1 サーバだけ attach する (ruby_lsp と solargraph を両方 attach すると
-- rubocop 由来の診断が二重に出る)。どちらを使うかは project root ごとに実測して決める:
-- root に Gemfile があり、かつ **その project の ruby** で ruby-lsp が起動できるなら ruby_lsp、
-- それ以外は solargraph (= 従来の挙動へ落ちる)。
--
-- 🚨 vim.fn.executable("ruby-lsp") では判定できない。rbenv の shim は「どれか 1 つの ruby に
--    gem が入っていれば」存在するので、未導入の project でも常に 1 を返す (実測 2026-09-08)。
--    起動できるかは、その project の cwd で実際に走らせる以外に確かめようがない。
-- 🚨 ruby-lsp を mason で入れてはいけない。mason の ruby で走るため、solargraph が
--    ubiregi-server で壊れていたのと同じ ABI ミスマッチ (project 3.1.6 / server 3.2.2 →
--    GEM_PATH が存在しない vendor/bundle/ruby/3.2.0 を指し bundle の gem が全滅 → 索引が
--    汚染されて自前コードの定義解決まで外す) を再生産する。加えて mason bin は PATH 先頭に
--    入る (_nviminit.lua) ので、mason 版が入った瞬間に下のプローブごと mason 版へ倒れる。
--    導入は rbenv 側で人が行う: RBENV_VERSION=<v> gem install ruby-lsp
--    (required_ruby_version >= 3.0 なので 2.x の project は solargraph のまま)。
-- 経緯と A-B は issues/done/332-ruby-lsp-selection-by-probe.md。

-- 環境への問い合わせ。テストが差し替えられるよう M のフィールドに出す (root ごとに 1 回しか
-- 呼ばれないので、production 側の複雑さはこの 2 関数に収まる)。
function M.has_gemfile(dir)
  return vim.uv.fs_stat(dir .. "/Gemfile") ~= nil
end

-- mason が入れたバイナリの置き場。_nviminit.lua が PATH 先頭へ prepend し、M.ruby_env が
-- Ruby のサーバ向けに**その 1 エントリだけ**を外す。両者の文字列が 1 文字でも違うと外れないので、
-- 出典をここ 1 か所にする (参照側が stdpath から組み直すと、片方を変えたときに無言でズレる)。
function M.mason_bin()
  return vim.fn.stdpath("data") .. "/mason/bin"
end

-- プローブと実サーバの**両方**へ渡す環境。同じ関数を両方が使うことが不変条件で、片方だけに
-- 渡すと「プローブは通るが server は別の ruby / 別のバイナリで走る」= 332 の病気に戻る。
--
-- 🚨 RBENV_VERSION="" : `rbenv shell 3.1.6` した端末から nvim を開くと、rbenv は project の
--    .ruby-version より RBENV_VERSION を優先するため、**全 project がその ruby で判定される**。
--    実測 2026-09-09 (.ruby-version=2.6.6 の dir で `ruby-lsp --version`):
--      素                     → rc=127 (shim が「3.1.6 にはある」と言うだけ) → solargraph ✅
--      RBENV_VERSION=3.1.6    → rc=0 / stdout "0.26.11"                      → ruby_lsp ❌
--      RBENV_VERSION=""       → rc=127 (.ruby-version へ戻る)                → solargraph ✅
--    rbenv は空文字を未設定として扱う (`[ -z "$RBENV_VERSION" ]`。rbenv 1.3.2 で
--    `env RBENV_VERSION= rbenv version-name` → 2.6.6 を実測)。**unset はできない**:
--    vim.system の env は base_env への上書き (_system.lua の setup_env が
--    tbl_extend("force", base_env(), env)) なので、キーを消す手段が無い。
-- 🚨 PATH から mason bin を外す : mason は ensure_installed から外してもアンインストールしない。
--    既に mason/bin/ruby-lsp が入っているマシンでは、_nviminit.lua が PATH 先頭へ入れる mason bin が
--    rbenv shim に勝ち、mason の ruby で走って ABI がズレる (332 の再生産)。**検出して警告する**の
--    ではなく PATH から外すのは、警告は人が対処するまで壊れたままだが、外せば発生しないため。
--    このマシンには mason/bin/ruby-lsp は無い (実測 2026-09-09。solargraph 等 16 本のみ)。
--: (string?, string?) -> table<string,string>
function M.ruby_env(path, mason_bin)
  path = path or vim.env.PATH or ""
  mason_bin = mason_bin or M.mason_bin()
  local kept = {}
  for entry in vim.gsplit(path, ":", { plain = true }) do
    -- 空エントリ (":" の連続 / 先頭末尾の ":") は POSIX 上 cwd を意味する。project の cwd で
    -- 走らせる判定なので、拾うと project 直下の実行ファイルに解決されうる。ここで落とす。
    if entry ~= "" and entry ~= mason_bin then table.insert(kept, entry) end
  end
  return { RBENV_VERSION = "", PATH = table.concat(kept, ":") }
end

-- その project の ruby で ruby-lsp が起動できるか (実測 0.13s/root)。
-- 🚨 rc / stdout の参照まで pcall の中に入れる。vim.system は spawn 失敗 (PATH に無い) で error を
--    投げ、SystemObj:wait は timeout と割り込みで **nil を返す** (runtime の _system.lua)。外で
--    res.code に触ると nil デリファレンスになり、それは root_dir の呼び出し元まで抜ける。
--    nvim の lsp_enable_callback は root_dir を pcall せずサーバ名順に回すので、ここで投げると
--    後ろに並ぶサーバ (solargraph 以下) がまとめて起動しなくなる。
-- 🚨 wait の上限は指定値そのものではない。timeout すると SIGKILL を送って**同じ上限でもう一度**
--    待つので最悪は 2 倍 (2s 指定 → 4s)。しかも fast_only なので待機中は再描画もメッセージも
--    出ず、ハングと区別がつかない。実測 0.13s に対して 2s は十分な余裕がある。
function M.ruby_lsp_runnable(dir)
  local ok, runnable = pcall(function()
    local res = vim.system(
      { "ruby-lsp", "--version" },
      { cwd = dir, text = true, env = M.ruby_env() }
    ):wait(2000)
    -- rc だけでは足りない: rbenv shim は未導入だと rc=127 + stderr "command not found" を返すが、
    -- バージョン文字列は stdout にしか出ない。逆に出力だけでも足りない (異常終了しても途中まで
    -- 出ることがある)。両方を見る (実測 2026-09-08)。
    -- res ~= nil は pcall と冗長 (nil を触っても外の pcall が拾って false になる。変異で確認済み)。
    -- 明示しているのは、nil が wait の**正常な戻り値**であって例外ではないため。
    return res ~= nil and res.code == 0 and (res.stdout or ""):match("%d") ~= nil
  end)
  return (ok and runnable) and true or false
end

-- 🚨 この判定が証明するのは「その cwd で rbenv が選ぶ ruby に ruby-lsp gem が load できる」まで。
--    exe/ruby-lsp の --version は OptionParser のブロックで即 exit(0) するので、実際の起動経路
--    (BUNDLE_GEMFILE 未設定 → SetupBundler → composed bundle → bundle exec ruby-lsp) を通らない
--    (実測 2026-09-08 / 追試 2026-09-09: ruby-lsp 0.26.11 の exe/ruby-lsp:13 が exit(0)、
--    起動経路は同ファイル 55-85 行の `if ENV["BUNDLE_GEMFILE"].nil?` ブロック)。
--    **起動経路まで通すプローブは作らない**: そこを通る唯一のフラグは --doctor で、これは
--    SetupBundler → composed bundle の bundle install を実際に走らせる。初回は分単位かかり、
--    root へ .ruby-lsp/ を書く。BufReadPre から同期で呼ぶ判定には使えない。
--    → 近似で選び、**外したら倒す** (下の M.ruby_lsp_failed)。

-- root ごとの判定結果。プローブは同期実行なので、同じ root の 2 つ目以降のバッファでは走らせない。
M.ruby_server_cache = {}

-- ruby_lsp のプロセスが落ちたとき、solargraph へ倒すべきか。倒すなら人へ見せる理由を返す。
-- 純関数にしているのは on_exit が fast event context だから (ここで notify も autocmd も呼べない)。
--
-- 🚨 **「意図した停止か」を code / signal から推測しない。** `Client:stop()` は LSP の
--    shutdown → exit を送るので、サーバは自分で終了する。実測 2026-09-09 (ruby-lsp 0.26.11):
--    `:LspStop` 相当の停止で **exit=1 signal=0**。SIGTERM が飛ぶのは graceful shutdown が
--    timeout して `rpc.terminate()` へ落ちたときだけ。つまり signal だけを見ると
--    **`:LspStop` と `:RubyLspReset` が自分でフォールバックを焚く** (reset は cache を空にした
--    直後に solargraph を書き戻され、「選び直す」コマンドがその root を固定してしまう)。
--    判定には呼び出し側が渡す stopping (= Client._is_stopping) を使う。
-- 🚨 `client:is_stopped()` は使えない。`rpc.is_closing() or _is_stopping` なので**どんな終了でも
--    真**になる (実測 2026-09-09: クラッシュ側 `_is_stopping=false / is_stopped()=true`、
--    意図した停止 `_is_stopping=true / is_stopped()=true`)。
--    signal 15 のガードは残す (graceful が timeout して terminate された場合と、外から殺された場合)。
--: (integer, integer, boolean?) -> string?
function M.ruby_lsp_fallback_reason(code, signal, stopping)
  if stopping then return nil end
  if signal == 15 then return nil end
  if code == 0 and signal == 0 then return nil end
  -- 78 は exe/ruby-lsp の SetupBundler::BundleNotLocked (Gemfile はあるが Gemfile.lock が無い)。
  -- プローブが必ず素通りする形なので、名指しで対処方法を出す。
  if code == 78 then return "Gemfile.lock がありません (bundle install が必要です)" end
  return ("異常終了しました (exit=%d signal=%d)"):format(code, signal)
end

-- 倒す本体。root ごとに 1 回だけ効く (2 回目以降はキャッシュが既に solargraph なので何もしない)。
-- 再 attach は nvim 自身の経路を借りる: vim.lsp.enable が「既存バッファへ効かせる」のに使っている
-- `doautoall nvim.lsp.enable FileType` がそれで、判定 (root_dir) を全バッファで引き直す。
-- 自前でバッファを走査して vim.lsp.start を呼ぶと、同じ判定の 2 実装目になる。
--: (string?, integer, integer, table?) -> boolean
function M.ruby_lsp_failed(root, code, signal, deps, stopping)
  local reason = M.ruby_lsp_fallback_reason(code, signal, stopping)
  if not reason then return false end
  if not root or M.ruby_server_cache[root] == "solargraph" then return false end
  M.ruby_server_cache[root] = "solargraph"
  deps = deps or {}
  local notify = deps.notify or vim.notify
  local reattach = deps.reattach or function() vim.cmd.doautoall("nvim.lsp.enable FileType") end
  -- 🚨 「solargraph に切り替えます」と言う前に、solargraph が本当に起動しうるか見る。
  --    その窓で倒すと「切り替えます」と言いながら何も起動せず、**嘘の説明つきで
  --    Ruby の LSP が消える**。
  --
  -- 🚨 **`is_enabled` だけに預けない** (issue 341)。`is_enabled` が「起動しうる」を意味するのは
  --    `_nviminit.lua` の `enable_available` が **cmd が table のサーバをバイナリ実在で絞る**
  --    副作用があるからで、`cmd` が関数のサーバ (ruby_lsp / ts_ls 等) では常に true になる。
  --    フォールバック先を関数 cmd のサーバへ変えた日に**この通知が黙って嘘に戻る**ので、
  --    判定を `enable_available` と同じ述語 (`M.server_binary_available`) と **and** で取る。
  local binary_ok = deps.server_binary_available or M.server_binary_available
  local fallback_ok = (deps.solargraph_enabled or vim.lsp.is_enabled)("solargraph")
    and binary_ok("solargraph", deps)
  notify(("ruby-lsp が%s\n%s は %s"):format(reason, root, fallback_ok
      and "solargraph に切り替えます (:RubyLspInfo で内訳 / :RubyLspReset で選び直し)"
      or "solargraph も使えません (:Mason で solargraph を入れるか、bundle を直して :RubyLspReset)"),
    vim.log.levels.WARN)
  reattach()
  return true
end

-- project root を担当するサーバ名を返す。返り値が常に 1 つであることが ruby_lsp / solargraph の
-- 排他の担保で、判定点をここ 1 か所に閉じている (root_dir 側に条件を 2 本書くと、片方の更新漏れが
-- そのまま二重 attach = 診断の二重表示になる)。
function M.ruby_server_for(dir)
  local cached = M.ruby_server_cache[dir]
  if cached then return cached end
  local server = "solargraph"
  if M.has_gemfile(dir) and M.ruby_lsp_runnable(dir) then server = "ruby_lsp" end
  M.ruby_server_cache[dir] = server
  return server
end

-- want が担当サーバのときだけ on_dir を呼ぶ (呼ばなければ attach しない)。root は lspconfig
-- 既定の root_markers と同じ { Gemfile, .git } で決めるが、**ruby_lsp を選ぶのは root が git repo
-- の root そのものであるときだけ**。
--
-- 🚨 vim.fs.root は marker を順に、それぞれ上方向へ全探索する (runtime の fs.lua)。Gemfile が
--    先頭なので「一番近い Gemfile」が勝ち、近い .git は見られない。gem は自分の Gemfile を
--    同梱していることがあり (実測 2026-09-08: ubiregi-server の vendor/bundle 配下に 156 件、
--    rbenv 3.1.6 の gems 配下に 152 件)、そのソースへ gd で飛ぶと **その gem のディレクトリが
--    root** になる (実測: vendor/.../gems/json-2.3.1/lib/json.rb → root=json-2.3.1)。
--    ruby-lsp は root へ composed bundle (.ruby-lsp/) を掘って bundle install を走らせるので、
--    vendor ツリーや rbenv の gems ツリーへの書き込みとネットワークが発生する。
--    solargraph は PATH のバイナリ 1 本で何も書かないので、repo の外はそちらに任せる。
--    (monorepo のサブ project は **その Gemfile のディレクトリが root のまま solargraph** になる。
--     repo root へ丸めているのではない。allowlist 時代も全 project が solargraph だったので退行ではない)
--
-- root が gem のチェックアウトか。**「root が git repo の root か」だけでは gem を弾けない**。
-- 🚨 bundler は `git:` 指定の gem を **clone** するので、チェックアウト先に `.git` が実在し、
--    Gemfile も同梱している → `dir == git_root` が成立してゲートを通る。
--    実測 2026-09-09: `~/.rbenv/versions/*/lib/ruby/gems/*/bundler/gems/*` と
--    `~/src/ubiregi-server/vendor/bundle/ruby/*/bundler/gems/*` の **21/21 件**が
--    `.git` と `Gemfile` を両方持つ。うち axlsx-d6a4a9cd21a2 は上位の .ruby-version (3.1.6) に
--    解決されるためプローブも rc=0 で通り、ruby_lsp が gem ディレクトリを root に選んでいた。
--    そこは Gemfile.lock が無いので ruby-lsp は exit 78 で死に、フォールバックの通知が
--    **「vendor ツリーで bundle install しろ」という嘘**を出す。Gemfile.lock を持つ git gem なら
--    今度は .ruby-lsp/ を掘って bundle install が走る (setup_bundler.rb の raise が mkpath より
--    前にあるおかげで助かっているだけで、ゲートが止めているのではない)。
-- パスに `gems` セグメントがあるものを外す。gem のツリーは rubygems も bundler も必ず
-- `.../gems/<name>-<version>/` の形を通るので、この 1 条件で両方に効く。
-- 誤って弾いた場合の劣化は solargraph (従来の挙動) で、壊れるより安い。
--: (string) -> boolean
function M.is_gem_checkout(dir)
  for seg in vim.gsplit(dir or "", "/", { plain = true }) do
    if seg == "gems" then return true end
  end
  return false
end

-- 判定の本体はここ 1 か所 (M.ruby_root_decision)。:RubyLspInfo も同じ関数を呼んで内訳を出す。
-- 表示側が式を写すと、片方だけ変えたときに **「なぜそう選ばれたか」の説明だけが嘘になる** ため。
--   opts.cache_only : キャッシュに無ければプローブせず「未判定」を返す (:RubyLspInfo が状態を
--                     書き換えないため。報告が観測対象を変えるのを避ける)
--   opts.explain    : 表示用の内訳 (gemfile / reason) も詰める。BufReadPre から呼ぶ経路では
--                     渡さない (fs_stat が 1 回増えるだけの用途なので hot path に足さない)
--: (string|integer, table?) -> table
function M.ruby_root_decision(target, opts)
  opts = opts or {}
  local dir = vim.fs.root(target, { "Gemfile", ".git" })
  if not dir then
    return { reason = "root が決まらない (上方向に Gemfile も .git も無い)" }
  end
  local git_root = vim.fs.root(target, { ".git" })
  local d = { root = dir, git_root = git_root }
  if M.is_gem_checkout(dir) then
    d.server = "solargraph"
    d.reason = "root が gem のチェックアウト (パスに gems セグメントがある)"
    return d
  end
  if dir ~= git_root then
    d.server = "solargraph"
    d.reason = "root が git repo の root ではない (Gemfile 同梱の gem など)"
    return d
  end
  d.cached = M.ruby_server_cache[dir] ~= nil
  if not d.cached and opts.cache_only then
    d.reason = "未判定 (この root のバッファをまだ開いていない)"
    return d
  end
  d.server = M.ruby_server_for(dir)
  if opts.explain then
    d.gemfile = M.has_gemfile(dir)
    if d.server == "ruby_lsp" then
      d.reason = "Gemfile があり、この project の ruby で ruby-lsp が起動できた"
    elseif d.gemfile then
      d.reason = "Gemfile はあるが、この project の ruby で ruby-lsp を起動できなかった"
    else
      d.reason = "root に Gemfile が無い"
    end
  end
  return d
end

local function ruby_root_dir(want)
  return function(bufnr, on_dir)
    local name = vim.api.nvim_buf_get_name(bufnr)
    local target = name ~= "" and name or bufnr
    local ok, d = pcall(M.ruby_root_decision, target)
    -- 判定が落ちたら従来の挙動 (solargraph) へ倒す。error を上へ抜かさないのは上記の理由。
    if not ok then return end
    if d.root and d.server == want then on_dir(d.root) end
  end
end

-- :RubyLspInfo が出す行。**表示の組み立てだけ**を持ち、判定は M.ruby_root_decision に任せる
-- (テストがこの返り値を読めるよう、print でなく行の配列を返す)。
--: (integer) -> string[]
function M.ruby_info_lines(bufnr)
  local name = vim.api.nvim_buf_get_name(bufnr)
  local d = M.ruby_root_decision(name ~= "" and name or bufnr, { explain = true, cache_only = true })
  local attached = {}
  for _, n in ipairs({ "ruby_lsp", "solargraph" }) do
    for _, c in ipairs(vim.lsp.get_clients({ bufnr = bufnr, name = n })) do
      table.insert(attached, ("%s (id=%d)"):format(c.name, c.id))
    end
  end
  local env = M.ruby_env()
  return {
    "Ruby の LSP サーバ選択 (root ごとに 1 回判定してキャッシュ)",
    "  root      : " .. tostring(d.root),
    "  git root  : " .. tostring(d.git_root),
    "  Gemfile   : " .. (d.gemfile == nil and "-" or (d.gemfile and "あり" or "なし")),
    "  選択      : " .. tostring(d.server),
    "  理由      : " .. tostring(d.reason),
    "  キャッシュ: " .. (d.cached and "済み (:RubyLspReset で捨てる)" or "未"),
    "  attach 中 : " .. (#attached > 0 and table.concat(attached, ", ") or "なし"),
    "  RBENV_VERSION : " .. ("%q (project の .ruby-version を優先させるため空にする)"):format(env.RBENV_VERSION),
    "  mason bin : PATH から除外済み (" .. M.mason_bin() .. ")",
  }
end

-- gem を入れた / bundle を直した後に選び直す。
-- 🚨 キャッシュを捨てて attach し直すだけでは足りない。nvim の can_start は **root_dir を評価
--    しない** (runtime lsp.lua の can_start は filetypes と config の妥当性しか見ない) ので、
--    既に attach 済みの solargraph は「もう選ばれない」ことを検出できずに残り、新しく起動する
--    ruby_lsp と二重 attach = rubocop の診断が二重に出る。**先に止めてから**選び直す。
--    止め方は :help lsp-restart の作法 (vim.lsp.enable(name, false) → true)。
-- 🚨 止まるのを壁時計で待たない。「Ruby のクライアントが 0 になったか」を上限つきでポーリングする。
--: (table?) -> boolean
function M.ruby_reset(deps)
  deps = deps or {}
  local enable = deps.enable or vim.lsp.enable
  local wait = deps.wait or vim.wait
  local running = deps.running or function()
    return #vim.lsp.get_clients({ name = "ruby_lsp" }) + #vim.lsp.get_clients({ name = "solargraph" })
  end
  local names = { "ruby_lsp", "solargraph" }
  enable(names, false)
  local gone = wait(2000, function() return running() == 0 end, 50) and true or false
  -- 🚨 キャッシュを捨てるのは**止め終わった後**。先に捨てると、停止で走る on_exit が
  --    「空にしたばかりのキャッシュ」へ solargraph を書き戻し、選び直すコマンドがその root を
  --    固定してしまう。stopping の判定 (M.ruby_lsp_fallback_reason) と二重の守りにする
  --    — あちらが nvim の private フィールドに依存しているため、順序でも塞いでおく。
  M.ruby_server_cache = {}
  enable(names, true)
  return gone
end

M.servers = {
  -- ruby-lsp / solargraph とも rbenv に gem install したものを PATH 経由で使う。
  -- 整形は ruby-lsp 内蔵の formatter (lspconfig 既定 init_options.formatter = "auto" が bundle の
  -- rubocop を検出) が担うので、solargraph と同じく <leader>F の lsp_format="fallback" で効く。
  ruby_lsp = {
    root_dir = ruby_root_dir("ruby_lsp"),
    -- lspconfig の lsp/ruby_lsp.lua と同形 (cmd は関数のまま) だが、**env を渡すために上書きする**。
    -- 🚨 cmd が関数のとき nvim は cmd_env / cmd_cwd を使わない。runtime client.lua は
    --    `if type(config.cmd) == 'function' then self.rpc = config_cmd(dispatchers, config)` と
    --    分岐し、spawn params (cwd / env / detached) を渡すのは table の枝だけ。関数の枝では
    --    それらは関数側の責任になる。実測 2026-09-09 (nvim 0.11.5、偽 ruby-lsp に env を吐かせた):
    --    この形で子プロセスに RBENV_VERSION="" と mason を除いた PATH が届き、cwd も root になる。
    -- 🚨 cmd_cwd を先に見るのは lspconfig の reuse_client が config.cmd_cwd を書くため。落とすと
    --    cwd が変わり → rbenv が読む .ruby-version が変わり → 走る ruby が無言で変わる。
    -- 🚨 cmd を table 形式にしない。_nviminit.lua の enable_available は
    --    `type(cmd) ~= "table" or executable(cmd[1]) == 1` で enable 対象を決めており、table に
    --    すると「PATH に shim があるか」の判定に入る (shim はどれか 1 つの ruby にあれば存在する
    --    ので、この判定は Ruby では意味を持たない)。
    cmd = function(dispatchers, config)
      return vim.lsp.rpc.start({ "ruby-lsp" }, dispatchers, {
        cwd = config and (config.cmd_cwd or config.root_dir),
        env = M.ruby_env(),
      })
    end,
    -- プローブは起動の証明にならないので (上の 🚨)、落ちたら solargraph へ倒す。
    -- ここは fast event context なので判断も通知も vim.schedule の中でやる。
    on_exit = function(code, signal, client_id)
      local c = vim.lsp.get_client_by_id(client_id)
      -- root_dir は on_dir が渡した文字列そのもの (runtime client.lua が `root_dir =
      -- config.root_dir` で素通しする) なので、M.ruby_server_cache の鍵と一致する。
      local root = c and c.root_dir
      -- 意図した停止 (:LspStop / :RubyLspReset / nvim 終了) かは Client._is_stopping で見る。
      -- private だが、code / signal からは区別できない (上の 🚨)。nvim を上げたときに
      -- 消えていないかは tests/nvim/lsp_ruby_server_select_check.lua が runtime を静的に pin する。
      local stopping = (c and c._is_stopping) and true or false
      vim.schedule(function() M.ruby_lsp_failed(root, code, signal, nil, stopping) end)
    end,
    -- 索引から外すパス。ruby-lsp の server.rb (process_indexing_configuration) が
    -- initializationOptions.indexing を camelCase → snake_case に直して
    -- RubyIndexer::Configuration#apply_config へ渡す。
    -- 既定で外れているものはここに書かない: vendor/bundle (Bundler.settings["path"] から自動生成)、
    -- トップレベルの tmp / node_modules / sorbet、dotdir 全般 (include が Dir.glob("*") 起点なので
    -- .claude/worktrees 等は最初から入らない)、`*_test.rb` / `*_spec.rb` / fixtures。
    -- ここに書くのは **入れ子** と **BUNDLE_PATH を設定していない repo** のための保険で、
    -- 実測 2026-09-08 (ubiregi-server): 既定で 18468/18478 件が既に外れており、
    -- 追加で外れるのは 10 件 (vendor/embedded_gem)。索引時間の主因は project でなく gem 側。
    init_options = {
      indexing = {
        excludedPatterns = {
          "**/vendor/**/*.rb",
          "**/node_modules/**/*.rb",
          "**/tmp/**/*.rb",
          "**/coverage/**/*.rb",
        },
      },
    },
  },
  -- solargraph は PATH のバイナリを直接使う (useBundler=false)。project の Gemfile 側
  -- solargraph を bundle exec で使いたい場合のみ true にする (Gemfile に無い project では起動失敗)。
  -- formatting=true は coc-settings の solargraph.formatting: true を踏襲 (これが無いと Ruby の
  -- <leader>F/:Format が lsp_format fallback 先の solargraph 既定 off で no-op になる)。
  -- 🚨 PATH 側が >= 0.56 で project の bundle が < 0.54.2 を pin していると無限ループする:
  --   0.56 は gem doc のキャッシュを `solargraph cache <gem>` の子プロセスへ投げる際 (library.rb の
  --   cache_next_gemspec) GEM_HOME を project の bundle へ書き換えるため、子は bundle 側の古い
  --   solargraph を起動する。0.52 に cache サブコマンドは無く即死するが親は exit status を見ず、
  --   同じ gem を選び直して毎秒 spawn し続ける (CPU 60% と "Caching gem" 通知が出っぱなし)。
  --   回避はその project の bundle の solargraph を >= 0.54.2 に上げること。
  -- 🚨 filetypes に eruby を足さない (lspconfig 既定は { "ruby" }、ruby_lsp は { "ruby", "eruby" })。
  --   つまり solargraph を選んだ project の .erb には**どのサーバも attach しない**。
  --   受け入れる判断であって見落としではない: solargraph に ERB のパーサは無く (0.60.2 の lib/ を
  --   走査しても ERB を使っているのは自分のドキュメント HTML 生成 page.rb だけ)、.erb を Ruby として
  --   渡すと先頭の `<` で構文エラーになる (実測 2026-09-09: `ruby -c` が rc=1 /
  --   "syntax error, unexpected '<'")。足すと補完も定義ジャンプも増えないまま、ファイル全体に
  --   偽の診断が出る。
  --   再評価の trigger: solargraph が ERB を受けるようになったとき、または .erb で補完が要ると
  --   言われたとき (そのときの解は「その project の ruby に ruby-lsp を入れる」= ruby_lsp を選ばせる
  --   ことで、eruby を solargraph へ渡すことではない)。issues/337 の 2。
  solargraph = {
    settings = { solargraph = { useBundler = false, diagnostics = true, formatting = true } },
    root_dir = ruby_root_dir("solargraph"),
  },
  ts_ls = {
    settings = {
      typescript = { inlayHints = ts_js_inlay_hints },
      javascript = { inlayHints = ts_js_inlay_hints },
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
  -- nvim-lspconfig (rolling) の terraformls.lua on_attach は nvim 0.12 専用の
  -- vim.lsp.codelens.enable を無条件呼びし、0.11 では ON_ATTACH_ERROR が出るため、存在ガード付き
  -- on_attach で丸ごと上書きする (元の on_attach は該当 1 行のみなので機能欠落なし)。
  -- nvim 0.12+ へ上げたら削除してよい。
  terraformls = {
    on_attach = function(_, bufnr)
      if vim.lsp.codelens.enable then
        vim.lsp.codelens.enable(true, { bufnr = bufnr })
      end
    end,
  },
}

-- 使用サーバの単一真実源: lspconfig 名 → mason パッケージ名。
--   - enable (_nviminit.lua の vim.lsp.enable) は key (lspconfig 名) を使う
--   - バイナリ導入 (mason-tool-installer) は value (mason パッケージ名) を使う
-- 新サーバはここへ 1 行足せば enable と導入の両方に効く。
-- coc の LSP 系 extension (tsserver/eslint/pyright/go/solargraph/html/css/json/yaml/sh/docker/tailwind/sql) を踏襲。
-- Ruby だけは project ごとに ruby_lsp / solargraph を出し分ける (両方ここに載せ、M.servers の root_dir で排他)。
-- 意図的に移行しなかった coc 機能 (欠落ではなく意図した縮退。パリティ台帳としてここに明記):
--   - spell-checker / 色プレビュー / markdownlint / swagger (ユーザー確認済み。必要時に cspell(nvim-lint) / nvim-colorizer / markdownlint(nvim-lint) を足す)
--   - coc-html-css-support (HTML 内の CSS クラス名補完): ネイティブに直等価なし。html/cssls で部分カバー
--   - <C-s> range-select (coc-range-select): treesitter incremental_selection 等で代替可 (未設定)
--   - <C-f>/<C-b> の float スクロール: 0.11 は hover 窓を再フォーカスしてスクロールできるため未マップ
-- 値が false のサーバは mason で入れない (enable はする)。ruby_lsp がそれで、実体は rbenv 側の
-- gem を PATH 経由で使う (理由は上の 🚨 を参照)。mason へ渡すのは M.mason_packages()。
-- 注意: このテーブルの参照元は _nviminit.lua の enable_available (キー) と、
-- mason-tool-installer の ensure_installed (M.mason_packages())。
M.server_packages = {
  ts_ls = "typescript-language-server",
  eslint = "eslint-lsp",
  pyright = "pyright",
  gopls = "gopls",
  ruby_lsp = false, -- mason では入れない (rbenv の gem を使う)
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

-- server_binary_available は「そのサーバの実行ファイルが在るか」を返す。
--
-- 🚨 **`enable_available` (_nviminit.lua) と同じ述語をここへ寄せた** (issue 341)。
-- 以前は判定が `_nviminit.lua` にだけ在り、`lsp.lua` は `vim.lsp.is_enabled` を
-- 「起動しうるか」の proxy として読んでいた。proxy が成り立つのは
-- **cmd が table のサーバに限る**ので、依存が 2 ファイルに跨ったまま暗黙になっていた。
--
-- 🚨 **cmd が関数のサーバは判定できないので true を返す** (ruby_lsp / ts_ls / eslint /
-- html / cssls / jsonls / yamlls / tailwindcss)。呼び出し側はこれを「起動する」ではなく
-- 「**起動しないとは言えない**」として扱うこと。
function M.server_binary_available(name, deps)
  deps = deps or {}
  local configs = deps.lsp_config or vim.lsp.config
  local executable = deps.executable or vim.fn.executable
  local cmd = configs and configs[name] and configs[name].cmd
  if type(cmd) ~= "table" then return true end
  return executable(cmd[1]) == 1
end

-- mason-tool-installer へ渡すパッケージ名。false のサーバ (mason 管理外) を落とす。
-- フィルタをテーブルの隣に置くのは、_nviminit.lua 側で書くと「false を渡さない」規則が
-- 参照側へ散り、サーバを足すときに片方だけ更新される形になるため。
function M.mason_packages()
  local pkgs = {}
  for _, pkg in pairs(M.server_packages) do
    if pkg then table.insert(pkgs, pkg) end
  end
  table.sort(pkgs) -- pairs の順は不定。ensure_installed の並びを安定させる
  return pkgs
end

-- 索引などの進捗 (LSP の $/progress)。ruby-lsp は大きな Rails project で数分かかることがあり、
-- 表示が無いと「押しても無反応」に見える (実測 2026-09-08: ubiregi-server の初回索引で
-- ruby-lsp が CPU 100% のまま 10 分以上、その間 gd の応答が返らなかった)。
-- 🚨 vim.lsp.status() は client.progress (vim.ringbuf) を **pop しながら** 読むので、
--    1 回の再描画で 2 回呼ぶと 2 回目は必ず空になる。呼ぶのはこの autocmd の中だけにして、
--    結果を保持する。statusline 側は M.progress_status() で保持した文字列を読むこと。
local progress_text = ""
-- 実際に statusline へ出している文字列。変わったときだけ再描画する (下記 refresh)

-- 実行中の要求 (LspRequest)。索引と違い $/progress は飛ばないので、こちらは自分で組む。
-- ruby-lsp の references は毎回ワークスペース全体を Prism で再パースするため、大きな Rails
-- project では 10 秒級かかる (実測 2026-09-08 ubiregi-server: references 11.2s。うち parse が
-- 7.0s で、その 88% が vendor/bundle の 18468 ファイル)。速くはできないので、せめて
-- 「押したのに何も起きない」に見えないようにする。
local pending = {} -- "client_id:request_id" -> ラベル
local shown = ""   -- いま statusline に出ている文字列

-- 索引 (progress) を優先する: 索引中はどのみち要求が返らないので、原因の方を出す。
local function compute()
  if progress_text ~= "" then return progress_text end
  local label
  for _, l in pairs(pending) do
    label = label or l
  end
  return label and ("LSP: " .. label) or ""
end

-- 🚨 表示が変わったときだけ redrawstatus を呼ぶ。lualine の statusline は関数評価なので、
--    再描画のたびに lualine_c の relative_path_from_git_root が走り、その中の vim.fs.root が
--    上方向へ fs_stat を撃つ。索引中の $/progress は高頻度で飛ぶので、無条件に呼ぶと
--    「通知 1 本 = 全ウィンドウの statusline 再評価」になる (敵対レビュー P2-5)。
local function refresh()
  local text = compute()
  if text == shown then return end
  shown = text
  vim.cmd.redrawstatus()
end

-- statusline から読む。vim.lsp.status() を直接呼ばせないための入口。
function M.progress_status()
  return shown
end

-- <C-k> (参照一覧) の振り分け。Ruby の **メソッド / ローカル**だけ ripgrep へ回し、
-- 定数・クラスと Ruby 以外は LSP の references を使う。
--
-- 🚨 ruby-lsp の references は索引を使わない。requests/references.rb:63 が索引の除外設定を
--    無視した生の `Dir.glob(workspace/**/*.rb)` で全ファイルを毎回 Prism で再パースする。
--    索引 (ruby_indexer の Entry) が持っているのは**宣言だけ**で、呼び出し側の逆引きが無いため。
--    しかもメソッドの一致条件は reference_finder.rb:285 の
--    `node.name.to_s == @target.method_name` で **名前一致だけ** (レシーバの型解析は無い) なので、
--    11 秒かけて得られる精度は単語一致の grep とほぼ変わらない。
--    実測 2026-09-08 (ubiregi-server, base_loader.rb の wrap_error):
--      LSP references 11.2s (2 回とも。キャッシュ無し) / rg -w 0.17s = 約 65 倍
--      🚨 rg の 0.17s は 2026-09-09 の追試値 (production と同条件 = ファイルタイプを絞らない)。
--      当初書いた 0.104s は再現せず、`--type ruby` で絞った条件 (0.03s) の測り残しと思われる
--      Dir.glob の対象 21148 件のうち 18468 件 (88%) が vendor/bundle
--    上流も既知 (Shopify/ruby-lsp#3051 "Find references in nvim takes about 35 seconds") で
--    **closed as not planned**。直る見込みが無いので client 側で回避する。
-- 定数・クラスを LSP に残すのは、collect_constant_references が index.resolve で名前空間を
-- 解決しており、grep より正確なため (同名の定数を別 namespace から区別できる)。
-- rg 側の欠点はコメント・文字列・シンボル (:wrap_error) も拾うこと。
-- vendor/bundle は rg が .gitignore を尊重するので自動的に外れる (~/.gitignore_global:14)。
M.ripgrep_reference_filetypes = { ruby = true, eruby = true }

-- ripgrep へ回すべきカーソル下の語かを判定する。公開しているのはテストが真の出典として
-- 読めるようにするため (判定を写すと、片方だけ変えたときにテストが古い前提で緑になる)。
function M.use_ripgrep_references(filetype, word)
  if not M.ripgrep_reference_filetypes[filetype] then return false end
  if word == nil or word == "" then return false end
  -- 大文字始まり = 定数 / クラス。
  -- `::` を含む形は判定しない: Ruby バッファの iskeyword は `@,48-57,_,192-255` で `:` を含まず、
  -- <cword> は `Loaders::BaseLoader` の上でも `BaseLoader` しか返さない (実測 2026-09-08)。
  -- 判定を足しても到達せず、変異で外しても緑のままだった (= 何も守らない分岐)。
  if word:match("^%u") then return false end
  return true
end

-- 参照検索の検索範囲。**on_attach が受け取った client をそのまま使わない**:
-- キーマップは LspAttach のたびに貼り直されるので、後から attach した client の root を
-- 掴んでしまう。実例: lspconfig の solargraph の filetypes は { "ruby" } で eruby を含まず、
-- tailwindcss は { ... "erb", "eruby" ... } を含むため、.erb では **tailwind の root**
-- (postcss.config.* / tailwind.config.* の場所) が入りうる (敵対レビュー P2-1)。
-- Ruby のサーバを名前で選び、無ければ repo 境界へ落とす。
--: (integer) -> string
function M.ruby_root_for(bufnr)
  for _, name in ipairs({ "ruby_lsp", "solargraph" }) do
    local c = vim.lsp.get_clients({ bufnr = bufnr, name = name })[1]
    local root = c and (c.root_dir or (c.config and c.config.root_dir))
    if root then return root end
  end
  local file = vim.api.nvim_buf_get_name(bufnr)
  return vim.fs.root(file ~= "" and file or bufnr, { ".git", "Gemfile" }) or vim.fn.getcwd()
end

-- <C-k> の行き先を **返り値で表明する**純関数。テストはこの返り値を見る。
-- 🚨 分岐をマッピングの中に埋めない: 「文字列が在るか」の静的 pin では、rg と LSP を
--    入れ替える / word_match を落とす / cwd を nvim の cwd に固定する、といった変異が
--    すべて緑で通ってしまう (敵対レビュー P1-2)。
--: (string, string, string) -> table
function M.references_action(filetype, word, root)
  if not M.use_ripgrep_references(filetype, word) then
    return { route = "lsp" }
  end
  return {
    route = "ripgrep",
    search = word,
    word_match = "-w", -- 部分一致にすると別メソッドを大量に拾う
    cwd = root,
    prompt_title = ("参照 (ripgrep): %s"):format(word),
  }
end

-- 実行中を表示する要求。**ユーザーが明示的に起こす操作に限る**。
-- CursorHold ごとに飛ぶ documentHighlight や、編集のたびに飛ぶ semanticTokens / codeLens を
-- 入れると、ステータスラインが点滅するだけで情報にならない。
M.request_labels = {
  ["textDocument/definition"] = "定義を検索中…",
  ["textDocument/references"] = "参照を検索中…",
  ["textDocument/implementation"] = "実装を検索中…",
  ["textDocument/typeDefinition"] = "型定義を検索中…",
  ["textDocument/hover"] = "ホバー取得中…",
  ["textDocument/rename"] = "リネーム中…",
  ["textDocument/formatting"] = "整形中…",
  ["textDocument/codeAction"] = "コードアクション取得中…",
  ["workspace/symbol"] = "シンボルを検索中…",
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
  map("n", "gd", function() tb().lsp_definitions() end, "定義へジャンプ (LSP definitions)")
  map("n", "gD", function() tb().lsp_implementations() end, "interface の実装一覧へ (LSP implementations)")
  -- 参照検索は rg 経路と LSP 経路の両方を記録する (issue 334 段階 1)。
  -- 記録の失敗は握り潰される (refs_usage.append の pcall)。ここで参照を握らず都度 require
  -- するのは、起動時に state ディレクトリへ触らないため。
  local function usage() return require("dotfiles.refs_usage") end

  -- <leader>K: rg 経路を迂回して LSP の references を引く。
  -- rg の結果で足りなかったときの逃げ道であり、同時に「困った回数」の観測点でもある
  -- (rg の直後に同じ語をこれで引き直すと fallback として記録される)。
  map("n", "<leader>K", function()
    usage().record("lsp", vim.fn.expand("<cword>"), vim.bo[bufnr].filetype)
    tb().lsp_references()
  end, "参照元一覧を LSP で引き直す (rg の結果で足りないとき)")

  map("n", "<C-k>", function()
    local ft = vim.bo[bufnr].filetype
    local word = vim.fn.expand("<cword>")
    local action = M.references_action(ft, word, M.ruby_root_for(bufnr))
    usage().record(action.route == "ripgrep" and "ripgrep" or "lsp", word, ft)
    if action.route == "lsp" then return tb().lsp_references() end
    tb().grep_string({
      search = action.search,
      word_match = action.word_match,
      cwd = action.cwd,
      prompt_title = action.prompt_title,
    })
  end, "参照元一覧 (Ruby のメソッドは ripgrep、定数と他言語は LSP)")

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
  end, "実装へ、無ければ定義へ (impl or definition)")

  -- K: nvim 0.11 が LspAttach で張る既定の hover (desc が "vim.lsp.buf.hover()" のまま) を
  -- 説明付きで張り直す。t (global) と同じ動作で、チートシートに意図が出るようにするため
  map("n", "K", vim.lsp.buf.hover, "ホバー: 型とドキュメントを表示 (hover)")

  -- コードアクション (coc: <leader>ac=cursor / <leader>as=source / <leader>qf=quickfix)
  map("n", "<leader>ac", vim.lsp.buf.code_action, "カーソル位置のコードアクション (code action)")
  map("n", "<leader>as", function()
    vim.lsp.buf.code_action({ context = { only = { "source" } } })
  end, "import 整理など (source action)")
  map("n", "<leader>qf", function()
    vim.lsp.buf.code_action({ context = { only = { "quickfix" } }, apply = true })
  end, "quickfix を即適用 (quickfix)")

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

  -- coc 時代のサイン配色 (エラー=白字/赤地・警告=黒字/橙地) を踏襲。色は palette.diag が出典。
  -- hl.set = ColorScheme 再適用 + cterm 併記 (256色環境) の規律 (dotfiles/hl.lua 参照)
  local hl = require("dotfiles.hl")
  local diag = require("dotfiles.palette").diag
  hl.set("DiagnosticSignError", { fg = diag.error_fg.hex, bg = diag.error_bg.hex, ctermfg = diag.error_fg.cterm, ctermbg = diag.error_bg.cterm })
  hl.set("DiagnosticSignWarn", { fg = diag.warn_fg.hex, bg = diag.warn_bg.hex, ctermfg = diag.warn_fg.cterm, ctermbg = diag.warn_bg.cterm })

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
  require("dotfiles.refs_usage").setup()

  -- 全サーバ共通の capabilities (blink.cmp)。nil なら素の capability。
  vim.lsp.config("*", { capabilities = capabilities or vim.lsp.protocol.make_client_capabilities() })

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

  -- Ruby のサーバ選択は無言で決まり (root ごとに 1 回)、gem を入れても再起動まで反映されない。
  -- 「どちらがなぜ選ばれたか」と「選び直す手段」を出す入口。docs/nvim-ruby-lsp.md に記載。
  vim.api.nvim_create_user_command("RubyLspInfo", function()
    vim.notify(table.concat(M.ruby_info_lines(vim.api.nvim_get_current_buf()), "\n"))
  end, { desc = "Ruby の LSP サーバ選択の内訳 (root / 理由 / attach 中) を表示" })
  vim.api.nvim_create_user_command("RubyLspReset", function()
    local gone = M.ruby_reset()
    vim.notify(gone
      and "Ruby の LSP 選択キャッシュを破棄し、開いているバッファを判定し直しました"
      or "既存の Ruby クライアントが 2 秒で止まりませんでした。二重 attach していたら :e で開き直してください",
      gone and vim.log.levels.INFO or vim.log.levels.WARN)
  end, { desc = "Ruby の LSP サーバ選択を捨てて選び直す (gem 導入後 / bundle 修復後)" })

  -- 進捗をステータスラインへ出すために保持する。end が来たら空へ戻す
  -- (複数サーバが同時に走っているときは、片方の end で一旦空になり、もう片方の次の
  --  通知で戻る。頻度が高いので実用上は瞬きしない)。
  local pgrp = vim.api.nvim_create_augroup("dotfiles_lsp_progress", { clear = true })
  vim.api.nvim_create_autocmd("LspProgress", {
    group = pgrp,
    callback = function(args)
      local value = args.data and args.data.params and args.data.params.value
      progress_text = (value and value.kind == "end") and "" or vim.lsp.status()
      refresh()
    end,
  })

  -- 実行中の要求を出す。LspRequest は 1 要求につき pending → complete (または cancel) の
  -- 順で飛ぶ (doc/lsp.txt の LspRequest。payload は { client_id, request_id, request } で
  -- request = { type, bufnr, method })。id は client ごとなので鍵は client_id と両方で作る。
  vim.api.nvim_create_autocmd("LspRequest", {
    group = pgrp,
    callback = function(args)
      local data = args.data
      local req = data and data.request
      if not req then return end
      local key = tostring(data.client_id) .. ":" .. tostring(data.request_id)
      -- ラベルの無いメソッド (documentHighlight 等) は nil になり、表示にも pending にも残らない。
      -- 再描画の抑止は refresh() が持つ (表示が変わらないなら呼ばない)
      pending[key] = (req.type == "pending") and M.request_labels[req.method] or nil
      refresh()
    end,
  })

  -- 🚨 client が死ぬと in-flight の要求は **complete が飛ばないまま**消える (nvim は client 終了時に
  --    残った要求へ complete を投げない)。索引の $/progress も end が来ない。掃除しないと
  --    「LSP: 参照を検索中…」や「indexing NN%」が永久に残る (敵対レビュー P2-2)。
  --    progress_text も落とす: その進捗は今出ていった client のもので、別 client が索引中なら
  --    次の通知で戻る (kind == "end" のときと同じ扱い)。
  vim.api.nvim_create_autocmd("LspDetach", {
    group = pgrp,
    callback = function(args)
      local prefix = tostring(args.data and args.data.client_id) .. ":"
      for key in pairs(pending) do
        if key:sub(1, #prefix) == prefix then pending[key] = nil end
      end
      progress_text = ""
      refresh()
    end,
  })

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
