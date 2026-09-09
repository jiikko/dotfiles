# Ruby の LSP サーバ選択に残る 5 つの穴

種別: bug
起票: 2026-09-08
出典: [332](332-ruby-lsp-selection-by-probe.md) の「未解決 (レビューが出したが、この commit では閉じていない)」節を分離したもの

332 で選択方式を allowlist から「Gemfile + 実起動プローブ」へ移した。実装と検証は閉じたが、
敵対レビューが出した 5 件は未対応のまま 332 の本文に残っていた。332 は 291 行あり、
本文を読まないと残件が見えないので独立させる。**新しい調査ではなく、332 で確認済みの事実の転記**。

## todolist

- [x] 1. プローブが起動の証明になっていない → **直した** (異常終了を検出して solargraph へ倒す)
- [x] 2. `.erb` にどのサーバも attach しない → **意図した挙動として受け入れた** (理由をコード直近と docs へ)
- [x] 3. `RBENV_VERSION` の漏れ → **再現を実測してから直した** (プローブと実サーバの両方で無効化)
- [x] 4. 選択理由が見えない / キャッシュを落とせない → **直した** (`:RubyLspInfo` / `:RubyLspReset`)
- [x] 5. mason の残留バイナリが shim に勝つ → **直した** (PATH から mason bin を外す。検出+警告は採らない)
- [x] 変異検証 15 本すべて red
- [x] `make test-nvim` / `make test-lint` を通す

## 1. プローブは「起動できる」の証明になっていない (実害あり) → 直した

`exe/ruby-lsp` の `--version` は OptionParser のブロックで即 `exit(0)` するので、実際の起動経路
(BUNDLE_GEMFILE 未設定 → launcher → composed bundle の解決 → `bundle install`) を通らない。

**project の bundle が壊れていると「プローブは通るが server は起動しない」になる。**
このとき solargraph は既に抑止されているので、**Ruby の LSP が無言で消える**。
起動失敗を検出して solargraph へ戻す仕組みは無い。

### 追試 (2026-09-09)

- ✅ **`exe/ruby-lsp:13` の `exit(0)` は実在した** (ruby-lsp 0.26.11 / rbenv 3.1.6)。
  起動経路は同ファイル **55-85 行**の `if ENV["BUNDLE_GEMFILE"].nil?` ブロック
  (`SetupBundler#setup!` → `exec "#{bundle} exec ruby-lsp"`)。issue の主張どおり
- 🚨 **「無言で消える」は言いすぎだった**。nvim 自身が
  `Client %s quit with exit code %s and signal %s` を WARN で出す
  (runtime `lua/vim/lsp.lua` の `on_client_exit`。`code ~= 0 or (signal ~= 0 and signal ~= 15)` のとき)。
  実際に欠けていたのは **①フォールバックが無い ②何をすれば直るか分からない** の 2 点で、
  完全な沈黙ではない
- **`--doctor` なら起動経路を通る** (`SetupBundler` を経由する) が、composed bundle の
  `bundle install` を実際に走らせるので初回は分単位かかり、root へ `.ruby-lsp/` を書く。
  `BufReadPre` から同期で呼ぶ判定には使えない → **「実起動へ寄せる」案は却下**
  (`adversarial-review-own-safeguards.md` §0-A の「発生させない構造」を問うた結果。
  次の監査が同じ提案を再生成しないようここに残す)

### やったこと

`M.ruby_lsp_fallback_reason(code, signal)` (純関数) + `M.ruby_lsp_failed(root, …)` +
`servers.ruby_lsp.on_exit`。

- 倒す: 異常終了。倒さない: 正常終了 / **signal 15** (`:LspStop` / nvim 終了 / `:RubyLspReset` が
  SIGTERM で止めるため。倒すと「手で止めたら勝手に別サーバが起動する」になる)
- `exit 78` = `SetupBundler::BundleNotLocked` (Gemfile はあるが Gemfile.lock が無い) は
  プローブが必ず素通りする形なので、名指しで `bundle install` を案内する
- 鍵は `client.root_dir`。runtime `client.lua` が `root_dir = config.root_dir` で素通しするので
  `M.ruby_server_cache` の鍵と**同じ文字列**になる (ズレると誰も読まない鍵へ書いて全部緑になるため、
  テストで「倒した後の `ruby_server_for` が solargraph を返す」まで見ている)
- 再 attach は自前でバッファを走査せず `doautoall nvim.lsp.enable FileType`
  (= `vim.lsp.enable` が既存バッファへ効かせるのに使っている経路) を借りる。
  自前で `vim.lsp.start` を回すと同じ判定の 2 実装目になる

## 2. `.erb` (eruby) にどのサーバも attach しない project がある → 受け入れる

lspconfig の filetypes は ruby_lsp が `{ruby, eruby}`、solargraph が `{ruby}` (実測)。
solargraph を選んだ project の `.erb` には**どのサーバも attach しない**。
allowlist 時代から同じ挙動だが、今後は「その ruby に gem を入れたか」で無言に反転する。

### 追試 (2026-09-09)

- ✅ filetypes の主張は正しい (`lsp/solargraph.lua` = `{ 'ruby' }` / `lsp/ruby_lsp.lua` = `{ 'ruby', 'eruby' }`)
- **solargraph 0.60.2 に ERB のパーサは無い**。`lib/` 配下で ERB を使っているのは自分の
  ドキュメント HTML 生成 (`page.rb`) だけで、source の読み取り経路には無い
- `.erb` を Ruby として渡すと**先頭の `<` で構文エラー**になる
  (実測: `ruby -c t.erb` → rc=1 / `syntax error, unexpected '<'`)

### 判断

**`filetypes` に `eruby` を足さない。** 足しても補完も定義ジャンプも増えず、ファイル全体に
偽の診断が出るだけ。理由は `nvim/lua/dotfiles/lsp.lua` の `solargraph` 定義の直上と
`docs/nvim-ruby-lsp.md` §8 に残した。
**再評価の trigger**: solargraph が ERB を受けるようになったとき / `.erb` で補完が要ると
言われたとき (後者の解は「その project の ruby に `gem install ruby-lsp`」= ruby_lsp を選ばせること)。

## 3. `RBENV_VERSION` を export した shell から起動すると判定が全 project で同じになる → 再現。直した

`rbenv shell 3.1.6` した端末から nvim を開くと、プローブが常にその ruby で走るため、
ruby 2.6 の project まで ruby_lsp に倒れると考えられる。**未実測**。

### 追試 (2026-09-09) — 再現した

`.ruby-version` = `2.6.6` / `Gemfile` だけの一時 dir で `ruby-lsp --version`:

| 条件 | rc | stdout | 判定 |
|---|---|---|---|
| 素 | 127 | (空。stderr に `rbenv: ruby-lsp: command not found`) | solargraph ✅ |
| `RBENV_VERSION=3.1.6` | **0** | `0.26.11` | **ruby_lsp ❌** |
| `RBENV_VERSION=""` | 127 | (空) | solargraph ✅ |

rbenv 1.3.2 は空文字を未設定として扱う (`env RBENV_VERSION= rbenv version-name` → `2.6.6`)。
**unset はできない**: `vim.system` の env は `base_env()` への上書き
(`_system.lua` の `setup_env` が `tbl_extend("force", base_env(), env)`) で、キーを消す手段が無い。

### 🚨 プローブだけ直すと**新しい穴が開く** (受け側のガードを先に洗った結果)

`.ruby-version` = 3.1.6 (gem あり) / shell が `RBENV_VERSION=3.2.2` (gem なし) の組み合わせでは:

- 現状: プローブが 3.2.2 で走り rc=127 → solargraph。動く
- プローブだけ `RBENV_VERSION=""` にすると: プローブは 3.1.6 で rc=0 → **ruby_lsp を選ぶ**が、
  実サーバは shell の 3.2.2 で起動して `command not found` → **1 の「無言で消える」を自分で作る**

→ **プローブと実サーバの両方に同じ env を渡す** (`M.ruby_env`)。片方だけの修正は commit しない。

🚨 **`cmd` が関数のとき nvim は `cmd_env` / `cmd_cwd` を使わない** (runtime `client.lua` は
`type(config.cmd) == 'function'` なら `config_cmd(dispatchers, config)` を呼ぶだけで、spawn params を
渡すのは table の枝だけ)。なので lspconfig と同形の関数 `cmd` を自分で持ち、
`vim.lsp.rpc.start(…, { cwd = …, env = M.ruby_env() })` で渡す。
**実測 2026-09-09** (nvim 0.11.5 / 偽 `ruby-lsp` に env を吐かせた): 子プロセスに
`RBENV_VERSION=` (空) と絞った PATH が届き、cwd も root になる。
`cmd` を table 形式にはしない — `_nviminit.lua` の `enable_available` が
`type(cmd) ~= "table" or executable(cmd[1]) == 1` で分岐しており、table にすると Ruby では
意味を持たない判定 (shim はどれか 1 つの ruby にあれば存在する) の枝へ移るため。

## 4. どちらのサーバがなぜ選ばれたかを見る手段が無い → 直した

選択結果が不可視で、gem を入れた後はキャッシュの無効化が無いため nvim の再起動が要る。

### やったこと

| コマンド | 何をするか |
|---|---|
| `:RubyLspInfo` | root / git root / Gemfile / 選択 / **理由** / attach 中の client / 渡している env |
| `:RubyLspReset` | キャッシュを捨て、Ruby のクライアントを**止めてから**選び直す |

- 内訳は `M.ruby_root_decision` (判定の本体) をそのまま呼ぶ。表示側が式を写すと
  「なぜそう選ばれたか」の説明だけが嘘になるため
- **`:RubyLspInfo` は状態を書き換えない** (`cache_only`)。未判定の root ではプローブせず
  「未判定」と出す。報告が観測対象を変えないようにする
- 🚨 `:RubyLspReset` が「止めてから」なのは、nvim の `can_start` が **`root_dir` を評価しない**
  ため (filetypes と config の妥当性しか見ない)。キャッシュを消して attach し直すだけだと、
  既に付いている solargraph が残ったまま ruby_lsp が起動して**二重 attach = 診断の二重表示**になる。
  止め方は `:help lsp-restart` の作法 (`vim.lsp.enable(name, false)` → `true`)。
  停止は壁時計で待たず「Ruby のクライアントが 0 になったか」を上限 2 秒でポーリングし、
  止まらなければ false を返して「`:e` で開き直して」と案内する

## 5. mason の残留バイナリが PATH 先頭で shim に勝つ → 直した (検出+警告は採らない)

`ensure_installed` から外しても mason はアンインストールしない。既に `mason/bin/ruby-lsp` が
入っているマシンでは、それが PATH 先頭で rbenv shim に勝つ。
**このマシンでは不在を実測済み (実害なし)**。他のマシンで踏む可能性がある。

### 追試 (2026-09-09)

- ✅ `~/.local/share/nvim/mason/bin/` は 16 本 (solargraph / gopls / pyright …) で
  **`ruby-lsp` は不在**。issue の記述どおり
- ✅ `_nviminit.lua` が `mason/bin` を PATH **先頭**へ prepend しているのも実在

### 判断: 警告ではなく **PATH から外す**

警告は人が対処するまで壊れたままだが、外せばそもそも発生しない
(`adversarial-review-own-safeguards.md` §0-A)。`M.ruby_env` が PATH からその 1 エントリだけを
落とす。mason bin のパスは `M.mason_bin()` を唯一の出典にし、`_nviminit.lua` もそこから取る
(2 ファイルで組み立てると文字列がズレて除外が無言で効かなくなる。テストが
「`_nviminit.lua` が `mason/bin` を直接組み立てていない」を検査する)。
副次: PATH の空エントリ (POSIX 上 cwd) も落とす — project の cwd で走らせる判定なので、
拾うと project 直下の実行ファイルに解決されうる。

## 結果 (実測)

- `make test-nvim` rc=0。集約経路の出力に検査行が出ることを確認:
  `[test-lsp-ruby-server-select] OK ruby lsp server select: 4 root で排他を確認 (… / env 5 ケース +
  実サーバ cmd の env・cwd / 異常終了の判定 6 ケース + 倒す副作用と on_exit 配線 /
  :RubyLspInfo の内訳と :RubyLspReset の停止順)`
- `make test-lint` rc=0
- 変異検証 **15 本すべて red** (baseline green を先に確認 / 各変異は `luac -p` を通したことを
  確認してから判定 / 落ちた assert 名を照合):

| 変異 | red になった assert |
|---|---|
| M1 `ruby_env` が `RBENV_VERSION` を返さない | `ruby_env(…).RBENV_VERSION = nil。空文字であること` |
| M2 `ruby_env` が mason bin を落とさない | `ruby_env(…).PATH = …, want "/usr/bin:/bin"` |
| M3 プローブが env を渡さない | `プローブの env が nil` |
| M4 実サーバの cmd が env を渡さない | `サーバの env が nil` |
| M5 実サーバの cmd が `cmd_cwd` を無視する | `cmd_cwd があるのに cwd が "/tmp/p-both"` |
| M6 SIGTERM でも倒す | `ruby_lsp_fallback_reason(0, 15) = …, 倒す=false であること` |
| M7 倒した後の二重発火を止めない | `同じ root で 2 回倒している` |
| M8 倒しても再 attach しない | `再 attach を 0 回呼んだ` |
| M9 `on_exit` が nvim の cwd を鍵にする | `on_exit 後のキャッシュが "ruby_lsp"` |
| M10 `:RubyLspInfo` がプローブを走らせる | `RubyLspInfo がプローブを走らせた` |
| M11 内訳の理由を判定から導かず固定する | `倒した後の内訳が古い` |
| M12 `:RubyLspReset` が止めずに選び直す | `ruby_reset の enable 呼び出しが …` |
| M13 `:RubyLspReset` がキャッシュを捨てない | `ruby_reset がキャッシュを捨てていない` |
| M14 `_nviminit.lua` が mason bin を自前で組み立てる | `_nviminit.lua が "mason/bin" を直接組み立てている` |
| M15 `ruby_lsp` の `on_exit` を外す | `servers.ruby_lsp.on_exit が無い` |

## 本物の ruby-lsp で穴を再現して、塞がったことを A-B で確認した (2026-09-09)

変異検証はスタブ越しなので、**本物のバイナリで「プローブは通るが server は起動しない」を
作って**通した。fixture: `.git` + `.ruby-version`=3.1.6 (ruby-lsp 導入済み) + `Gemfile`
**Gemfile.lock 無し**。

```
$ ruby-lsp --version   → rc=0  stdout "0.26.11"     ← プローブは通る
$ ruby-lsp </dev/null  → rc=78 stderr "Project contains a Gemfile, but no Gemfile.lock."
```

その fixture の `foo.rb` を本物の `_nviminit.lua` で headless に開いた結果:

| | 今回の実装 | `on_exit` を外した版 (A-B) |
|---|---|---|
| 判定 (プローブ直後) | `ruby_lsp` | `ruby_lsp` |
| 30 秒後の選択 | **`solargraph`** | `ruby_lsp` のまま |
| attach 中の client | **`solargraph` (id=2)** | **なし** ← 穴の再現 |
| `:RubyLspInfo` の理由 | 「Gemfile はあるが、この project の ruby で ruby-lsp を起動できなかった」 | 「…起動できた」(嘘のまま) |

**この観測は機構の有無で結果が変わる** (外すと Ruby の LSP がゼロになる) ので、
「倒れた」の証拠として数えてよい。fixture に `.ruby-lsp/` は作られなかった (副作用なし)。

### 🚨 上の A-B は「失敗する経路」しか通っていない → 成功側も測った

`exit 78` で落ちる fixture では、ruby-lsp は `bundle exec ruby-lsp` へ re-exec する**手前**で
死ぬ。つまり **絞った PATH で `bundle` が引けるか**は上の run では確かめられていない
(attach したのは solargraph で、そちらの `cmd` は table なので**素の PATH** を使う)。
ここを測らないと、`M.ruby_env` の PATH フィルタが起動を壊していても
「この project の ruby で ruby-lsp を起動できなかった」= もっともらしい診断に化けて、
フォールバックが自分のバグを隠す。

`Gemfile.lock` を足した fixture (`bundle lock` rc=0) で測り直した:

```
ENV RBENV_VERSION=[]                        ← 空文字が実サーバへ届いている
ENV PATH に mason bin が残っているか: false  ← 除外が効いている
PHASE1 選択=ruby_lsp
PHASE2 attach=true clients=[ruby_lsp]        ← 絞った PATH で SetupBundler → bundle exec が通った
PHASE3 選択(その後)=ruby_lsp                  ← 健全なサーバを誤って倒していない
```

fixture に `.ruby-lsp/` (composed bundle) が作られたので、re-exec 経路まで到達している。
これで PATH の空エントリ落としを含めて `M.ruby_env` は**成功側でも**確認済み。

### 倒す条件について明記しておくこと

`ruby_lsp_fallback_reason` は **「長時間正常に動いた後のクラッシュ」でも倒す**。
実測したのは起動失敗 (exit 78) だけで、こちらは意図した設計:
落ちた以上そのサーバは使えておらず、solargraph の方がまだ答えを返す。
副作用として **その root は `:RubyLspReset` するまで solargraph に固定される**。
再評価の trigger: 「一度クラッシュしただけで ruby_lsp に戻らないのが不便」と感じたとき
(そのときの解は「起動直後の失敗だけ倒す」= `on_init` 到達の有無で分けること)。

## 敵対的レビュー (opus / read-only) と、その後の 2 周目 (2026-09-09)

新設したのが「安全機構」(フォールバック / PATH フィルタ / ゲート) なので自己レビューで閉じず、
観点を分けた敵対的レビューを最終ゲートに通した。**採用した指摘は自分で裏を取ってから直した。**

### 自分の実験で先に見つけた P1: 意図した停止を code/signal から推測していた

`client:stop()` は LSP の shutdown → exit を送るので、ruby-lsp は **exit=1 signal=0** で終わる
(実測)。SIGTERM は graceful が timeout したときだけ。つまり `signal == 15` のガードは
意図した停止を 1 件も捕まえていなかった。実害は `:LspStop` だけでなく **`:RubyLspReset` が
自分でフォールバックを焚き、空にしたばかりのキャッシュへ solargraph を書き戻して
その root を固定する** こと (「選び直す」コマンドの正反対)。
→ `Client._is_stopping` を渡して判定。`is_stopped()` は使えない
(`rpc.is_closing() or _is_stopping` なので**どんな終了でも true**。実測: クラッシュ側も true)。
private フィールドなので **nvim runtime を静的に pin するテスト**を足した (更新で消えたら落ちる)。
加えて `ruby_reset` のキャッシュ破棄を**停止完了後**へ移した (順序でも塞ぐ二重の守り)。

### レビューが出した P1: root ゲートは git ソースの gem で破れる (裏取り済み)

bundler は `git:` 指定の gem を **clone** するので、チェックアウト先に `.git` が実在する
→ `root == git_root` が成立してゲートを通る。**自分で数え直した**: rbenv と ubiregi-server の
vendor を合わせて `bundler/gems/` 配下 **21/21 件**が `.git` + `Gemfile` を両方持つ
(レビューの「17 件」は rbenv 側だけの数で、project 側 4 件と合わせて一致)。
`axlsx-d6a4a9cd21a2` は上位の `.ruby-version` (3.1.6) に解決されプローブ rc=0 → **ruby_lsp が
gem ディレクトリを root に選んでいた**。そこは `Gemfile.lock` が無いので exit 78 で死に、
**私が入れたばかりの通知が「vendor ツリーで bundle install しろ」という嘘を出す**。
書き込みが起きていなかったのは `setup_bundler.rb` の `raise` が `mkpath` より前にあるからで、
ゲートのおかげではない。
→ `M.is_gem_checkout` (パスに `gems` セグメント) で外し、**`.git` を持つ gem の fixture**を足した
(元の fixture は `.git` を置いていなかったので、この退行は構造的に見えなかった)。

### レビューが出した P2 で採用したもの

- **solargraph が enable されていない窓**(mason 未導入 / 導入中は table cmd の実在判定で落ちる)で
  倒すと、「solargraph に切り替えます」と言いながら何も起動しない = 嘘の説明つきで LSP が消える。
  → 倒す前に `vim.lsp.is_enabled("solargraph")` を見て、駄目なら正直に案内を変える
- `M.ruby_env()` の**引数ゼロ形**(production の 3 呼び出しすべて)が無検査だった → 既定値の配線を検査
- `ruby_reset` の待機述語が無検査 (`running()==0` を true にしても緑) → 述語そのものを pin
- `doautoall` スタブが引数を捨てていた → `"nvim.lsp.enable FileType"` の文字列を assert
- `_nviminit.lua` の grep 検査が**コメント行だけで満たされる** (vacuous) / 逆にコメントに
  `mason/bin` と書くと偽陽性 → コメントを落としてからコードだけを見る

### 採用しなかったもの

- **`BUNDLE_GEMFILE` / `GEM_HOME` 等も同クラス**という指摘 (P2-7)。正しいが、`""` では
  `ENV[...].nil?` を偽にできず `M.ruby_env` の位置では原理的に閉じられない。
  レビュー自身が発火条件を「未確認」としており、この repo の zsh 設定はどれも export していない
  (grep 済み)。**未確認リスクとして記録**し、`bundle exec nvim` / direnv の `layout ruby` で
  Ruby の LSP がおかしくなったらここを疑う
- P3-9 `kill -9` も倒す件 / P3-13 末尾スラッシュ付き mason bin / P3-14 未判定 root の表示が
  `nil` — いずれも劣化先が solargraph か表示だけなので現状維持

### レビューが「壊せなかった」と明記した観点

env の吹き飛ばし (`_system.lua` が親環境へマージする) / `root_dir` の鍵ズレ (素通しで一致) /
on_exit 時点の client 消失 (削除は後段の schedule) / フォールバックの無限ループ・二重 attach /
`cmd` が関数のとき `cmd_env` が使われないという主張 / `.erb` の受け入れ。

### 2 周目の変異検証 (レビュー対応で新設した分)

指摘への修正は**新しい安全機構**なのでもう 1 周当てた。11 本中 **10 本 red**:

| 変異 | 結果 |
|---|---|
| stopping の判定を落とす / `is_stopped()` に替える | red / red |
| reset のキャッシュ破棄を停止より**前へ移動** | red |
| gem チェックアウトのゲートを外す / `is_gem_checkout` を常に false | red / red |
| `ruby_env` の `mason_bin` 既定値を潰す / `PATH` 既定値を潰す | red / red |
| `doautoall` の group 指定を落とす | red |
| reset の待機条件を常に真にする | red |
| solargraph の enable 確認を外す | red |
| `_nviminit` 検査のコメント除去をやめる | **green (受け入れ)** |

🚨 最後の 1 本が green なのは、コメント除去を戻しても**今の repo では両側とも成立する**ため
(緩めるだけの変異で、退行は作らない)。実質的な守りは「`mason_bin` 既定値を潰す」が red で担保している。

🚨 途中で **2 本の変異を作り直した**。①`is_stopped()` 変異が stub にメソッドが無くて
**TIMEOUT** (red でも green でもない第 3 の結果) になったので、stub に実物と同じ
`is_stopped() = true` を持たせてから当て直した。②reset の順序の変異が「移動」でなく
「挿入」になっており、後段のクリアが残るので **green** になっていた。どちらも
「変異の diff を読む」まででしか気づけない形。

## 残タスク

なし (5 件すべて判定済み: 4 件は修正、1 件 (`.erb`) は理由と再評価 trigger を付けて受け入れ)。
332 から引き継いだ別件 (定数の `<C-k>` が 11.5 秒 / `rbenv-default-gems` 未導入) は
[332](332-ruby-lsp-selection-by-probe.md) の残タスク節が正本で、本 issue のスコープ外。

## 関連

- [332](332-ruby-lsp-selection-by-probe.md) — 実装本体。「未解決」節がこの issue の出典
- [335](../335-human-ruby-lsp-jump-verification.md) — 実 project での動作確認 (人間のタスク)
- [`docs/nvim-ruby-lsp.md`](../../docs/nvim-ruby-lsp.md) — 触る前に読む前提。`:RubyLspInfo` /
  `:RubyLspReset` の入口もここ
