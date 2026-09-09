# nvim での Ruby: 定義ジャンプの仕組みと、2026-09-08 の高速化

対象: `nvim/lua/dotfiles/lsp.lua` / `nvim/lua/dotfiles/refs_usage.lua` / `_nviminit.lua` の lualine。
経緯と実測の全量は [`issues/332`](../issues/done/332-ruby-lsp-selection-by-probe.md) /
[`issues/334`](../issues/done/334-ruby-references-call-site-index.md)。
ここに置くのは**触る前に知らないと壊す前提**と、**なぜその設計にしたか**。

## 1. 何が壊れていて、何が遅かったか

症状は「Rails の `.rb` で `gd` も `<C-k>` も効かない」。原因は**別々の 2 つ**だった。

**(a) サーバが project と違う Ruby で走っていた。** mason の solargraph 0.60.2 が rbenv 3.2.2 で
起動し、project (ruby 3.1.6 / bundle は `vendor/bundle/ruby/3.1.0`) の `GEM_PATH` として
**存在しない `vendor/bundle/ruby/3.2.0`** を見ていた。bundle の gem 解決が全滅し、その壊れた
gem マップが索引を汚染して、**自前コードの定義解決まで外していた**
(`ApplicationRecord` が自分自身に戻る)。

**(b) 参照検索は原理的に遅い。** ruby-lsp の `textDocument/references` は索引を使わず、
要求のたびにワークスペース全体を Prism で再パースする。

この 2 つを混ぜて考えると直せない。(a) は設定で直り、(b) は直らないので経路を替えた。

## 2. 定義ジャンプの仕組み (なぜ 0.4 ms なのか)

`ruby_lsp/listeners/definition.rb` は `@index` しか触らない。`Dir.glob` も再パースも無い。

### 索引の形

`RubyIndexer::Index` の実体は **`@entries: Hash[String, Array[Entry]]`**。鍵は名前
(`add` が `@entries[entry.name] << entry`)。メソッドならメソッド名、クラス・定数なら完全修飾名。
`Entry` は `Class` / `Module` / `Constant` / `Member` (メソッド) / `Accessor` / パラメータで、
**宣言だけ**。呼び出し側は入っていない ← これが 4 節の遅さの根。

### レシーバの型を先に決める (`TypeInferrer#infer_receiver_type`)

| レシーバ | 決め方 |
|---|---|
| `self` / レシーバ無し | 囲んでいるクラス |
| リテラル (`"x"` `:x` `[]` `{}` 数値 正規表現 `nil` `true` 範囲 lambda) | その型のクラス |
| 定数 (`Foo.bar`) | `Foo` の特異クラス |
| インスタンス変数 / クラス変数 | 所有クラスから解決 |
| **それ以外 (`account.name`)** | **変数名から推測** (下記) |

`guess_type` は生テキストを snake_case → CamelCase して定数として索引を引くだけ
(`account` → `Account`)。**データフロー解析ではない**ので `GuessedType` という別クラスになっている。

### 型が決まったら継承チェーンで絞る (`Index#resolve_method`)

`linearized_ancestors_of` が `prepend → self → include → superclass` の順に線形化した祖先を返し、
`entry.owner&.name == ancestor` で最初に当たった祖先で確定する。**ここが grep との本質的な差**で、
`ApplicationRecord` 経由で `ActiveRecord::Base` のメソッドへ正しく飛べるのはこれのおかげ。

### 決まらなければ名前一致・最大 10 件

`MAX_NUMBER_OF_DEFINITION_CANDIDATES_WITHOUT_RECEIVER = 10`。`gd` で候補が複数出るのはこれで、
ありふれた名前だと**先頭 10 件で打ち切られる**。

## 3. 索引はいつ作られ、いつ更新され、どこに在るのか

🚨 **ディスク上に索引ファイルは存在しない。** メモリ上の `@entries` / `@uris_to_entries` /
prefix tree がすべて。gem 全体で `Marshal` を使っているのは `setup_bundler.rb` のエラー保存
1 箇所だけで、索引の永続化は無い。**project 直下の `.ruby-lsp/` は索引ではなく
composed bundle** (ruby-lsp 自身を project の Gemfile に足した `Gemfile` / `Gemfile.lock`)。

| タイミング | 何が起きるか |
|---|---|
| **server 起動時** | `perform_initial_indexing` が別スレッドで `index_all(uris: configuration.indexable_uris)`。`index_all` は **1 プロセス 1 回だけ** (2 度目は `IndexNotEmptyError`)。実測 **9.6〜17 秒** (ubiregi-server) |
| **編集中 (`didChange`)** | `@store.push_edits` のみ。**索引は触らない**。書きかけのメソッドは索引に載らない |
| **保存・外部変更 (`didChangeWatchedFiles`)** | ファイル単位で差分更新。開いていれば `index.handle_change(uri, content)`、閉じていれば `index_single(uri, content)`、削除なら `index.delete(uri)` |

**nvim 側の前提**: 差分更新は client がファイル監視を登録して初めて効く。nvim 0.11.5 は
`workspace.didChangeWatchedFiles.dynamicRegistration` を **macOS では true** で広告する
(`protocol.lua:565`)。この repo は macOS 専用なので (CLAUDE.md「対象プラットフォーム」/ issue 133)、
差分更新は効く前提で書いてよい。

**帰結**: nvim を起動するたびに 10 秒級の索引が走る。永続キャッシュが無い以上、
現実的な緩和は「**nvim の起動回数を減らす**」だけ (gem 除外は効果 15% で割に合わない。4 節)。

## 4. 参照検索だけが桁違いに遅い理由

`requests/references.rb:63` が**索引の除外設定を無視した生の
`Dir.glob(workspace/**/*.rb)`** で全ファイルを毎回 Prism で再パースする。索引に呼び出し側が
無いので、そうするしかない。しかもメソッドの一致条件は `reference_finder.rb:285` の
`node.name.to_s == @target.method_name` で **名前一致だけ** (レシーバの型解析なし)。

つまり **11 秒かけて得られる精度は、単語一致の grep とほぼ同じ**。
定数・クラスは別で、`collect_constant_references` が `index.resolve` で名前空間を解決するため
grep より正確。

上流も既知で [Shopify/ruby-lsp#3051](https://github.com/Shopify/ruby-lsp/issues/3051) は
**closed as not planned**。直る見込みが無いので client 側で回避した。

### 実測 (ubiregi-server / ruby 3.1.6 / ruby-lsp 0.26.11)

索引完了後のリクエスト別レイテンシ (2 回目の値):

```
hover                   0.4 ms      completion            0.2 ms
definition              0.4 ms      codeAction            0.5 ms
documentSymbol          0.2 ms      documentHighlight     0.7 ms
foldingRange            0.1 ms      formatting            9.7 ms
semanticTokens/full     0.3 ms      workspace/symbol    190.9 ms
references          11563.8 ms  ← ここだけ桁が違う
```

`Dir.glob` の対象 21148 件のうち **18468 件 (88%) が `vendor/bundle`**。
Prism パースは全体 7.04 秒 / vendor を除くと 0.81 秒。

## 5. 高速化として実際にやったこと (3 つ、原因別)

**① サーバ選択を実測プローブへ** — `gd` / `<C-j>` が直った本体。
allowlist を廃止し、「root に Gemfile があり、かつ**その project の ruby**で `ruby-lsp --version`
が rc=0 かつ stdout にバージョンを返すなら ruby_lsp、それ以外は solargraph」。root ごとに 1 回
(実測 0.13 秒) 測ってキャッシュする。

- 🚨 `vim.fn.executable("ruby-lsp")` は使えない。rbenv の shim は**どれか 1 つの ruby に
  入っていれば存在する**ので、未導入の project でも常に 1 を返す
- 🚨 **ruby_lsp を選ぶのは root が git repo の root そのもののときだけ**。`vim.fs.root` は
  marker を順に上方向へ探索するので `{ "Gemfile", ".git" }` では「一番近い Gemfile」が勝ち、
  Gemfile 同梱の gem のソースへ飛ぶと **gem ディレクトリが root** になる
  (実測: `vendor/.../gems/json-2.3.1/lib/json.rb` → root=`json-2.3.1`。同梱 gem は vendor に
  156 件 / rbenv 3.1.6 の gems に 152 件)。ruby-lsp はそこへ `.ruby-lsp/` を掘って
  `bundle install` を走らせるので、放置すると vendor ツリーに書き込みが発生していた
- 🚨 **しかし「git repo の root か」だけでは gem を弾けない**。bundler は `git:` 指定の gem を
  **clone** するので、チェックアウト先に `.git` が実在し Gemfile も同梱している → ゲートを通る。
  実測 2026-09-09: rbenv と ubiregi-server の vendor を合わせて `bundler/gems/` 配下の
  **21/21 件**が `.git` + `Gemfile` を両方持ち、`axlsx-d6a4a9cd21a2` は上位の `.ruby-version`
  (3.1.6) に解決されるためプローブも rc=0 で通っていた。
  → **パスに `gems` セグメントがある root は ruby_lsp から外す** (`M.is_gem_checkout`)。
  gem のツリーは rubygems も bundler も必ず `.../gems/<name>-<version>/` を通るので 1 条件で足りる。
  誤って弾いたときの劣化は solargraph (従来の挙動) なので、通すより安い
- 🚨 **ruby-lsp を mason で入れてはいけない**。mason の ruby で走るため同じ ABI ミスマッチを
  再生産する。[公式ドキュメント](https://shopify.github.io/ruby-lsp/editors.html)も
  「C 拡張が Ruby ABI に依存するため」明確に非推奨としている。導入は
  `RBENV_VERSION=<v> gem install ruby-lsp` (required_ruby_version >= 3.0)

#### プローブと実サーバは同じ env で走らせる (`M.ruby_env`)

**片方だけに渡すと「プローブは通るが server は別の ruby で走る」**= 上の (a) に戻る。
渡しているのは 2 つだけ:

| 何を | なぜ | 実測 (2026-09-09) |
|---|---|---|
| `RBENV_VERSION=""` | `rbenv shell 3.1.6` した端末から nvim を開くと、rbenv は project の `.ruby-version` より `RBENV_VERSION` を優先するので**全 project がその ruby で判定される** | `.ruby-version`=2.6.6 の dir で `ruby-lsp --version` が 素:rc=127 → `RBENV_VERSION=3.1.6`:**rc=0** → `RBENV_VERSION=""`:rc=127。rbenv 1.3.2 は空文字を未設定として扱う (`env RBENV_VERSION= rbenv version-name` → 2.6.6) |
| PATH から mason bin を除外 | mason は `ensure_installed` から外してもアンインストールしない。残っていると `_nviminit.lua` が PATH 先頭へ入れる mason bin が rbenv shim に勝つ | このマシンには `mason/bin/ruby-lsp` は無い (16 本中に不在)。他マシン用の予防 |

🚨 **`RBENV_VERSION` を unset はできない**。`vim.system` の env は `base_env()` への上書き
(`_system.lua` の `setup_env` が `tbl_extend("force", …)`) なので、キーを消す手段が無い。空文字で代用する。

🚨 **`cmd` が関数のとき nvim は `cmd_env` / `cmd_cwd` を使わない**。`client.lua` は
`type(config.cmd) == 'function'` なら `config_cmd(dispatchers, config)` を呼ぶだけで、spawn params を
渡すのは table の枝だけ。だから lspconfig と同形の関数 `cmd` を自分で持ち、
`vim.lsp.rpc.start(…, { cwd = …, env = M.ruby_env() })` で渡している。
**table 形式にはしない**: `_nviminit.lua` の `enable_available` が `executable(cmd[1])` の枝へ移り、
Ruby では意味を持たない判定 (shim はどれか 1 つの ruby にあれば存在する) が挟まる。

#### プローブは起動の証明ではないので、落ちたら倒す

`--version` は `exe/ruby-lsp:13` の OptionParser ブロックで即 `exit(0)` するため、実際の起動経路
(`BUNDLE_GEMFILE` 未設定 → `SetupBundler` → composed bundle → `bundle exec ruby-lsp`。同ファイル
55-85 行) を通らない。**起動経路まで通すプローブは作れない**: そこを通る唯一のフラグ `--doctor` は
composed bundle の `bundle install` を実際に走らせるので初回は分単位かかり、root へ `.ruby-lsp/` を
書く。`BufReadPre` から同期で呼ぶ判定には使えない。

→ 近似で選び、**外したら倒す**。`servers.ruby_lsp.on_exit` が異常終了 (正常終了と SIGTERM は除く)
を見て、その root のキャッシュを `solargraph` へ書き換え、`doautoall nvim.lsp.enable FileType`
(= `vim.lsp.enable` 自身が既存バッファへ効かせるのに使う経路) で選び直させる。
`exit 78` は `Gemfile.lock` が無いときの `SetupBundler::BundleNotLocked` なので、名指しで
`bundle install` を案内する。

**② `<C-k>` を対象で振り分け** — 参照が速くなった本体。

| カーソル下 | 経路 | 実測 |
|---|---|---|
| Ruby のメソッド / ローカル (小文字始まり) | `telescope.grep_string -w` | 0.17 秒 |
| Ruby の定数 / クラス (大文字始まり) | LSP references | 11.5 秒 |
| Ruby 以外 (go / ts) | LSP references | 型解析つきで速い |

定数を LSP に残すのは `index.resolve` が名前空間を解決するため。`<leader>K` で LSP に引き直せる。

**③ 待ちを可視化** — 速度ではなく体感。索引の `$/progress` と実行中の要求 (`LspRequest`) を
lualine に出す。🚨 `vim.lsp.status()` は `client.progress` (`vim.ringbuf`) を **pop しながら**
読むので、1 回の再描画で 2 回評価すると 2 回目は必ず空になる。呼ぶのは autocmd の中だけ。

## 6. 手で書いたコード (何を書き、何を書かなかったか)

**高速化そのものを実装したコードは無い。** 書いたのは繋ぎ・振り分け・計測・テスト。

```
nvim/lua/dotfiles/lsp.lua        +245 行  (コメント 118 / 空行 14 → 実コード 113 行)
nvim/lua/dotfiles/refs_usage.lua   93 行  (計測。速度には無関係)
nvim/ruby-refs-index/measure.rb   148 行  (プロトタイプ。どこにも繋がっていない)
tests/nvim/*                      674 行  ← 一番多い
```

| 関数 | 責務 | 速度への効き方 |
|---|---|---|
| `M.has_gemfile` / `M.ruby_lsp_runnable` / `M.ruby_server_for` / `ruby_root_dir` | サーバ選択 | ①の本体。速い探索を書いたのではなく「正しい ruby で動くサーバを選ぶ」だけ |
| `M.use_ripgrep_references` + `<C-k>` | 参照の振り分け | ②の本体。検索は telescope + ripgrep がやる。行き先を決めているだけ |
| `M.progress_status` + `LspProgress` / `LspRequest` の autocmd | 待ちの表示 | 体感のみ |
| `refs_usage.lua` | 使用実績の記録 | 無関係 (判断材料を集める) |
| `nvim/ruby-refs-index/measure.rb` | 呼び出し側索引のプロトタイプ | **0**。計測専用で production には繋がっていない |

唯一アルゴリズムらしいのは `measure.rb` (Prism の visitor で呼び出し側の逆引きを作る) だが、
これは「呼び出し側索引が現実的か」を数字で出すための計測で、エディタには繋がっていない
(構築 1.07 秒 / 37 MB / クエリ 3〜7 µs。詳細は
[`../nvim/ruby-refs-index/README.md`](../nvim/ruby-refs-index/README.md))。

## 7. 触るときの注意

### 選択を見る / 選び直す

| コマンド | 何をするか | いつ使うか |
|---|---|---|
| `:RubyLspInfo` | root / git root / Gemfile / 選ばれたサーバ / **理由** / attach 中の client / 渡している env を出す。**状態は書き換えない** (未判定ならプローブせず「未判定」と出す) | 「なぜ solargraph なのか」を知りたいとき |
| `:RubyLspReset` | 判定キャッシュを捨て、Ruby のクライアントを**止めてから**選び直す | `gem install ruby-lsp` した後 / `bundle install` で bundle を直した後 |

🚨 `:RubyLspReset` が「止めてから」なのは、nvim の `can_start` が **`root_dir` を評価しない**ため。
キャッシュを消して attach し直すだけだと、既に付いている solargraph は「もう選ばれない」ことを
検出できずに残り、新しく起動する ruby_lsp と二重 attach = rubocop の診断が二重に出る。

- **`~/dotfiles` へ pull しても、開きっぱなしの nvim には効かない**。`vim.lsp.enable` は起動時に
  走るので、終了して開き直すまで古い設定のまま動く (この件で 2 往復した)
- **索引中は要求が返らない**。「押しても無反応」に見えたら、まずステータスラインの
  `Ruby LSP: indexing files: NN%` を見る
- **`excludedPatterns` は索引にしか効かない**。`references` は別経路 (生 `Dir.glob`) なので、
  ここを増やしても参照検索は速くならない
- **gem 除外は効果 15%** (A-B 実測: 大きい gem 10 個を外して索引 10.7 秒 → 9.1 秒)。
  gem へのジャンプを失う対価に見合わない

## 8. 未解決 / 意図的にそうしていること

- **定数の `<C-k>` は 11.5 秒のまま**。`vendor/bundle` を repo 外へ出せば約 5 秒になる
  (参照の再パース対象の 88% がそれ)。sidecar 化するかは `:DotfilesRefsStats` の数字で決める
- **`.erb` は solargraph の filetypes に無い**ので、solargraph を選んだ project では `.erb` に
  どのサーバも attach しない。**受け入れている**: solargraph 0.60.2 に ERB のパーサは無く
  (`lib/` で ERB を使っているのは自分のドキュメント HTML 生成 `page.rb` だけ)、`.erb` を Ruby として
  渡すと先頭の `<` で構文エラーになる (実測 2026-09-09: `ruby -c` が rc=1 /
  `syntax error, unexpected '<'`)。`filetypes` に `eruby` を足すと補完も定義ジャンプも増えないまま
  ファイル全体に偽の診断が出る。
  **再評価の trigger**: solargraph が ERB を受けるようになったとき / `.erb` で補完が要ると
  言われたとき。後者の解は「その project の ruby へ `gem install ruby-lsp`」であって
  eruby を solargraph へ渡すことではない (ruby_lsp の filetypes は `{ ruby, eruby }`)
