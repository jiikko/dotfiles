# nvim での Ruby: 定義ジャンプの仕組みと、2026-09-08 の高速化

対象: `nvim/lua/dotfiles/lsp.lua` / `nvim/lua/dotfiles/refs_usage.lua` / `_nviminit.lua` の lualine。
経緯と実測の全量は [`issues/332`](../issues/332-ruby-lsp-selection-by-probe.md) /
[`issues/334`](../issues/334-ruby-references-call-site-index.md)。
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

**nvim 側の前提**: `workspace.didChangeWatchedFiles.dynamicRegistration` は nvim 0.11.5 で
**macOS と Windows だけ true** (`protocol.lua:565`。Linux/BSD は backend が貧弱なので false)。
この repo は macOS 専用なので差分更新は効く。Linux で使うなら「保存しても索引が古いまま」に
なることを織り込む必要がある。

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
- 🚨 **ruby-lsp を mason で入れてはいけない**。mason の ruby で走るため同じ ABI ミスマッチを
  再生産する。[公式ドキュメント](https://shopify.github.io/ruby-lsp/editors.html)も
  「C 拡張が Ruby ABI に依存するため」明確に非推奨としている。導入は
  `RBENV_VERSION=<v> gem install ruby-lsp` (required_ruby_version >= 3.0)

**② `<C-k>` を対象で振り分け** — 参照が速くなった本体。

| カーソル下 | 経路 | 実測 |
|---|---|---|
| Ruby のメソッド / ローカル (小文字始まり) | `telescope.grep_string -w` | 0.104 秒 |
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

- **`~/dotfiles` へ pull しても、開きっぱなしの nvim には効かない**。`vim.lsp.enable` は起動時に
  走るので、終了して開き直すまで古い設定のまま動く (この件で 2 往復した)
- **索引中は要求が返らない**。「押しても無反応」に見えたら、まずステータスラインの
  `Ruby LSP: indexing files: NN%` を見る
- **`excludedPatterns` は索引にしか効かない**。`references` は別経路 (生 `Dir.glob`) なので、
  ここを増やしても参照検索は速くならない
- **gem 除外は効果 15%** (A-B 実測: 大きい gem 10 個を外して索引 10.7 秒 → 9.1 秒)。
  gem へのジャンプを失う対価に見合わない

## 8. 未解決 / 判断待ち

- **定数の `<C-k>` は 11.5 秒のまま**。`vendor/bundle` を repo 外へ出せば約 5 秒になる
  (参照の再パース対象の 88% がそれ)。sidecar 化するかは `:DotfilesRefsStats` の数字で決める
- **プローブは起動の証明ではない**。`exe/ruby-lsp` の `--version` は OptionParser のブロックで
  即 `exit(0)` するので、composed bundle の解決経路を通らない。project の bundle が壊れていると
  「プローブは通るが server は起動しない」になり、solargraph も抑止済みなので LSP が無言で消える
- **`.erb` は solargraph の filetypes に無い**ので、solargraph を選んだ project では
  どのサーバも attach しない
- **`RBENV_VERSION` を export した端末から起動すると**、プローブが全 project で同じ答えを返す
