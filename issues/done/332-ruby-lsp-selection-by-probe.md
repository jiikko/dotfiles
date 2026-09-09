# Ruby の LSP サーバ選択を allowlist から「Gemfile + 実起動プローブ」へ移す

種別: bug / refactor
起票: 2026-09-08

## 症状

ubiregi-server (Rails / ruby 3.1.6) で `gd` / `<C-k>` が効かない。
telescope は `No LSP References found` を出す。

## 原因 (実測)

solargraph の実体は mason の 0.60.2 で、**rbenv 3.2.2** で走っている
(`_nviminit.lua` が mason の bin を PATH 先頭へ入れるため rbenv shim より勝つ)。
一方 ubiregi-server は `.ruby-version` = 3.1.6 で bundle は `vendor/bundle/ruby/3.1.0`。
そのため solargraph は存在しない `GEM_PATH=.../vendor/bundle/ruby/3.2.0` を見にいき、
bundle の gem 解決が全滅する (`~/.local/state/nvim/lsp.log`):

```
Could not find 'rubocop' (>= 0) among 71 total gem(s)
Checked in 'GEM_PATH=/Users/koji/src/ubiregi-server/vendor/bundle/ruby/3.2.0'
[WARN] Gem <ほぼ全部> from bundle not found: Gem::MissingSpecError
```

この壊れた gem マップが索引を汚染し、**自前コードの定義解決まで外す**。

### A-B (`app/models/account_label.rb` の `ApplicationRecord` / `AccountLabel`)

| 条件 | definition | references |
|---|---|---|
| 現状 (mason 0.60.2 / ruby 3.2.2 / project の Gemfile) | `account_label.rb:1` (自分自身) | 1 件 (自分自身のみ) |
| 同じバイナリで `BUNDLE_GEMFILE` を空 Gemfile に差し替え | `application_record.rb:1` ✅ | — |
| `bundle exec solargraph` (0.50.0 / ruby 3.1.6) | `application_record.rb:1` ✅ | — |
| `~/.rbenv/shims/solargraph` (0.55.1 / ruby 3.1.6) | `application_record.rb:1` ✅ | 13 件 ✅ |
| `~/.rbenv/shims/ruby-lsp` (0.26.11 / ruby 3.1.6) | `application_record.rb:1` ✅ | 13 件 ✅ |

変えた変数は 1 つ (bundle の gem マップが解決できるか) で、他は固定。
なお現状の誤答は再現率 100% ではない (6 分ポーリングで誤答のままの run と、
正答を返した run が 1 回ずつある)。索引の状態依存。

`belongs_to` はどの条件でも 0 件 (Rails の DSL は solargraph 単体では解決できない。別問題)。

## 決めたこと

サーバ選択を「project の allowlist」から「**Gemfile があり、かつその project の ruby で
`ruby-lsp` が実際に起動できるか**」の実測プローブへ移す。

- `vim.fn.executable("ruby-lsp")` はゲットに使えない (実測: rbenv shim はどれか 1 つの
  ruby に入っていれば存在するので、どの project でも常に 1 を返す)
- 未導入の ruby では今と同じ solargraph に落ちる (劣化しない)
- プローブのコストは実測 0.12〜0.13s。root ごとに 1 回だけ実行してキャッシュする
- mason の ruby-lsp は使わない (mason の ruby で走るので同じ ABI ミスマッチになる)。
  実際 `ensure_installed` に載っているのに `mason/packages/` に入っておらず、
  `cmd` が関数のサーバは実在判定されないため**通知も出ていなかった**

gem の導入は rbenv 側で人が行う (本 issue のスコープ外):

```sh
for v in $(rbenv versions --bare); do
  case "$v" in 2.*) continue ;; esac   # ruby-lsp の required_ruby_version は >= 3.0 (実測)
  RBENV_VERSION="$v" gem install ruby-lsp --no-document
done
```

## todolist

- [x] `M.ruby_server_for` を Gemfile + 実起動プローブへ差し替え (allowlist を廃止)
- [x] `M.server_packages` から ruby_lsp の mason 導入を外す (enable は残す)
- [x] `tests/nvim/lsp_ruby_server_select_check.lua` を新しい軸へ書き直す
- [x] 変異検証 (15 本中 14 本 red。下記)
- [x] 敵対的レビュー (read-only / opus) と、その指摘への対応
- [x] 実 project (ubiregi-server) で attach 先が ruby_lsp になることを確認 → [335](../335-human-ruby-lsp-jump-verification.md) で追跡 (期限 2026-09-15)

## 進捗

commit `feat(332): Ruby の LSP サーバ選択を Gemfile + 実起動プローブへ移す`。

### 敵対的レビューで出て、直したもの

- **root が「一番近い Gemfile」になる (P1)**。`vim.fs.root` は marker を**順に**上方向へ全探索
  するので、`{ "Gemfile", ".git" }` では近い `.git` より先に Gemfile が当たる。Gemfile を同梱した
  gem のソースへ `gd` で飛ぶと **その gem のディレクトリが root** になる。
  実測 2026-09-08: `vendor/.../gems/json-2.3.1/lib/json.rb` → root=`json-2.3.1`
  (`.git` 先頭なら repo root)。Gemfile 同梱の gem は ubiregi-server の vendor 配下に **156 件**、
  rbenv 3.1.6 の gems 配下に **152 件**。ruby-lsp は root へ composed bundle (`.ruby-lsp/`) を掘って
  `bundle install` を走らせるので、vendor ツリー / rbenv の gems ツリーへの書き込みが起きていた。
  → **ruby_lsp を選ぶのは root が git repo の root そのもののときだけ**に限定。
  それ以外は solargraph (PATH のバイナリ 1 本。何も書かない) に任せる。
  🚨 レビューの「activerecord へ飛ぶと gem が root」は外れ (activerecord は Gemfile を同梱して
  いないので root は repo root)。発火するのは同梱している gem に限る。
- **`res.code` が pcall の外だった (P2)**。`SystemObj:wait` は timeout / 割り込みで **nil を返す**
  (runtime `_system.lua`)。nil デリファレンスは `root_dir` の呼び出し元へ抜け、nvim の
  `lsp_enable_callback` は `root_dir` を pcall せずサーバ名順に回すため、**solargraph 以下が
  まとめて起動しなくなる**。→ 参照を pcall の中へ入れ、`ruby_root_dir` 側も pcall で包んで
  失敗時は従来の挙動 (solargraph) へ倒す。
- **wait の上限は指定値の 2 倍** (timeout 後に SIGKILL を送って同じ上限でもう一度待つ)。
  しかも `fast_only` なので待機中は再描画もされない。→ 5s → **2s** (実測 0.13s に対して十分)、
  コメントに実際の上限を明記。
- **テストのスタブが引数を捨てていた (P1)**。`vim.system` のスタブが `cmd` / `opts` を受け取って
  いなかったため、「cwd を渡さない」「`--version` を落とす」変異が**スイート全体緑**で通った
  (= この変更の唯一の前提に検査が無かった)。→ 引数を記録して assert。
- **`vim.fs.root` を定数関数へ差し替えていたので marker が無検査だった (P1)**。marker の順序
  そのものが load-bearing なのに、空配列にする変異まで緑だった。→ 実ファイルシステムの fixture
  (repo / vendor 配下の gem / repo 外の gem ツリー) を作り、実物の `vim.fs.root` で検査する。
- 到達不能だった `== nil` チェックの順序、`table.sort` の無検査、`-1` を焼いた件数比較、
  `_nviminit.lua` の allowlist を指す stale コメント。

### 変異検証 (15 本)

前半 7 本 (Gemfile 判定を外す / rc だけ / stdout だけ / キャッシュを読まない / pcall を外す /
mason フィルタを外す / ruby_lsp を mason 管理へ戻す) と、レビュー対応で足した 8 本
(cwd を渡さない / `--version` を落とす / root ゲートを外す / marker 順を `.git` 先頭へ /
marker を空へ / wait の上限を外す / mason の sort を消す) がいずれも狙った assert を red にする。

**1 本だけ green**: 「`res ~= nil` を外す」。外側の pcall が nil デリファレンスを拾って同じ
`false` になるため、観測上の差が無い (冗長な守り)。nil は wait の**正常な戻り値**であって例外では
ないので、明示は残してコメントで冗長だと書いた。pcall ごと外す変異 (前半 5 本目) は red。

### 未解決 → [337](337-bug-ruby-lsp-selection-has-five-open-holes.md) へ分離 (2026-09-08) → 337 で全件決着 (2026-09-09)

レビューが出して本 commit で閉じていない 5 件 (プローブは起動の証明ではない / `.erb` の非対称 /
`RBENV_VERSION` / 選択結果が不可視 / mason の残留バイナリ) は、本文が長く残件が埋もれるので
337 へ移した。内容は転記で、新しい調査はしていない。

## 追補 (2026-09-08、同日の続き)

commit `feat(332): 索引の進捗をステータスラインへ出し、索引対象から入れ子の vendor 等を外す`。

**索引中に何も出ないのを直した。** ruby-lsp は索引中に `$/progress`
(`server.rb:330` の `begin_progress("indexing-progress", "Ruby LSP: indexing files")`) を送っており、
nvim 0.11.5 は `window.workDoneProgress = true` を広告している (`protocol.lua:574`) ので通知は
届いていたが、**表示する側が無かった** (dotfiles に `LspProgress` も `vim.lsp.status` も 0 件。
`lualine_y = { "progress" }` はファイル内の位置 % で LSP とは無関係)。
`LspProgress` で受けて保持し、lualine の `lualine_x` から読む形にした。

🚨 `vim.lsp.status()` は `client.progress` (`vim.ringbuf(50)`) を **pop しながら**読む
(`lsp.lua:774` / `client.lua:405` / `shared.lua:1177` の `__call = pop`)。1 回の再描画で 2 回
評価すると 2 回目は必ず空になるので、呼ぶのは autocmd の中だけにし、statusline は保持した
文字列を読む。この罠は実行時には「たまに消える」形でしか出ないため、テストで静的に固定した。

**索引の除外は、このプロジェクトではほぼ効かないことが実測で分かった。**

| トップレベル | .rb 件数 |
|---|---|
| vendor | 18478 (うち vendor/bundle 18468 / embedded_gem 10 / assets 0) |
| app | 1159 |
| test | 1124 |
| db | 152 |

`.bundle/config` に `BUNDLE_PATH: vendor/bundle` があるため、ruby-lsp は既定で
`vendor/bundle/**/*.rb` を除外する (`configuration.rb:33-37` が `Bundler.settings["path"]` から
自動生成)。`tmp` / `node_modules` / `sorbet` と dotdir (include が `Dir.glob("*")` 起点なので
`.claude/worktrees` を含む) も既定で対象外。**追加で外れるのは 10 件**。
索引時間の主因は project ではなく **gem 側**だった。
それでも「BUNDLE_PATH を設定していない repo」と「入れ子の node_modules / vendor / tmp」には
効くので、汎用パターンとして `init_options.indexing.excludedPatterns` に 4 本入れた。

gem の除外 (`excludedGems`) は**採らなかった**: その索引は gem へジャンプするためだけのもの
ではなく、`belongs_to` や ActiveRecord のメソッドの定義・hover・補完の出どころそのもので、
全部外すと solargraph が返せなかった状態 (Rails の DSL が 0 件) に自分から戻ることになる。

変異検証 5 本 (end で空へ戻さない / status() の結果を入れない / autocmd を張らない /
lualine が status() を直呼びする / lualine から進捗を外す) がいずれも red。
`excludedPatterns` にはテストを付けていない (設定値の再掲にしかならないため。
[`refuse-low-value-coverage.md`](../../_claude/rules/refuse-low-value-coverage.md))。

## 追補 2 (2026-09-08): 参照検索が遅い件と、実行中の表示

commit `feat(332): 実行中の LSP 要求をステータスラインに出す`。

**症状**: メソッドの定義行で `<C-k>` (参照一覧) を押すと、無反応のまま数十秒経ってから
telescope が開く。

**原因は telescope ではなくサーバ側** (実測 2026-09-08、ubiregi-server の
`app/models/loaders/base_loader.rb:35 def wrap_error`):

```
索引完了まで        11.1 s
references 1 回目   11303 ms  n=33
references 2 回目   11153 ms  n=33   ← キャッシュされない
```

`ruby-lsp-0.26.11/lib/ruby_lsp/requests/references.rb:63` は**索引の除外設定を使わず**、
生の `Dir.glob(File.join(workspace_path, "**/*.rb"))` で全ファイルを毎回 Prism で再パースする:

| 対象 | 件数 | Prism パース実測 |
|---|---|---|
| `Dir.glob(**/*.rb)` 全体 | 21148 | 7.04 s |
| うち `vendor/bundle` | 18468 | (差分 6.2 s = **88%**) |
| `vendor/bundle` を除く | 2680 | 0.81 s |

つまり参照検索のたびに gem 18468 ファイルを含めて舐め直している。
**追補 1 で入れた `excludedPatterns` はここには効かない** (索引用の設定で、references は別経路)。

**やったこと**: 速くはできないので、「押しても無反応」に見えないようにした。
nvim 0.11 の `LspRequest` autocmd (`doc/lsp.txt:656`。payload は
`{ client_id, request_id, request = { type, bufnr, method } }`) で pending / complete / cancel を
受け、`M.request_labels` に載せた**ユーザーが明示的に起こす操作だけ**をステータスラインに出す。
索引 (`$/progress`) が走っているときはそちらを優先する。

🚨 `documentHighlight` / `semanticTokens` / `codeLens` は載せない。CursorHold や編集のたびに
飛ぶので、出すと点滅するだけで情報にならない。この絞り込みは**表示の有無では検査できない**
(ラベル表に無いメソッドはどのみち nil になるので、絞り込みを外しても表示は空のまま = 変異が
green だった)。実際に効いているのは `redrawstatus` を呼ばないことなので、テストは
**再描画の回数**で見ている。

変異検証 5 本すべて red (対象メソッドの絞り込みを外す / complete で消さない / 索引より
実行中の要求を優先する / references をラベル表から外す / autocmd を張らない)。

**やらなかったこと (判断と根拠)**:

- `vendor/bundle` を repo の外へ出す (`BUNDLE_PATH` を絶対パスへ) と参照検索は
  11.2 s → 約 5 s になる見込み (実測の 6.2 s 削減)。ただし仕事の repo の `bundle install`
  やり直しと、CI / スクリプトが `vendor/bundle` 前提でないかの確認が要るので**人の判断待ち**
- ripgrep ベースの高速な代替 (`telescope.grep_string`) を別キーに割り当てる案は保留
  (意味解析ではないので同名メソッドを拾う。要望が出たら足す)

**索引 (`index_all`) は生の全走査ではない** — `configuration.indexable_uris` を使うので
include/exclude が効き、`vendor/bundle` は除外される。代わりに bundle の gem を gemspec 経由で
全部索引するので、そこが時間の主因。ディスクへの永続化は無い (`Marshal` の使用は
`setup_bundler.rb` のエラー保存 1 箇所だけ) ため、**nvim を起動するたびに作り直す**。
実測: 2 回目以降の索引は **9.6 秒** (初回のみ composed bundle の `bundle install` で数分)。

## 追補 3 (2026-09-08): <C-k> を Ruby のメソッドだけ ripgrep へ回す

commit `feat(332): <C-k> を Ruby のメソッドだけ ripgrep へ振り分ける`。

**なぜ索引が効かないのか** (ソース確認):

- `ruby_indexer` の `Entry` は `Class` / `Module` / `Constant` / `Member` (メソッド) /
  `Accessor` / パラメータ…と**宣言だけ**。呼び出し側の逆引きテーブルが無い。
  だから参照検索は索引から答えられず、全ファイルの AST を歩き直すしかない
- さらにメソッドの一致条件は `reference_finder.rb:285` の
  `node.name.to_s == @target.method_name` で **名前一致のみ**（レシーバの型解析なし）。
  11 秒かけて得られる精度は単語一致の grep とほぼ同じ
- **定数・クラスは別**: `collect_constant_references` が `index.resolve` で名前空間を解決する
  ので、こちらは grep より正確

**上流の状況**: [Shopify/ruby-lsp#3051](https://github.com/Shopify/ruby-lsp/issues/3051)
「Find references in nvim takes about 35 seconds」(40,190 ファイル / v0.23.5) は
**closed as not planned**。直る見込みが無いので client 側で回避する。

**公式ドキュメントの確認** ([Editors | Ruby LSP](https://shopify.github.io/ruby-lsp/editors.html)):

- **mason での導入は明確に非推奨**「Using Mason to manage your installation of the Ruby LSP may
  cause errors」— 依存に C 拡張があり、Ruby ABI ごとに分ける必要があるため。
  推奨は version manager で ruby version ごとに `gem install`。**本 issue の実装と一致**
- nvim 0.11+ 向けの推奨形は `vim.lsp.config` / `vim.lsp.enable` (本 repo は既にその形)
- `init_options` で渡せるのは `enabledFeatures` / `featuresConfiguration` / `indexing` /
  `formatter` / `linters` / `experimentalFeaturesEnabled` / `addonSettings`。
  **references を無効化する項目は無い** (enabledFeatures の一覧に references は載っていない)

**やったこと**: `<C-k>` を対象で振り分ける。

| カーソル下 | 経路 | 実測 |
|---|---|---|
| Ruby のメソッド / ローカル (小文字始まり) | ripgrep (`telescope.grep_string`, `-w`) | 0.104 s |
| Ruby の定数 / クラス (大文字始まり) | LSP references (index.resolve が効く) | — |
| Ruby 以外 (go / ts など) | LSP references (型解析つきで速い) | — |

`vendor/bundle` は rg が `.gitignore` を尊重するので自動的に外れる (`~/.gitignore_global:14`)。
rg 側の欠点はコメント・文字列・シンボル (`:wrap_error`) も拾うこと。

変異検証 4 本すべて red (大文字始まりの判定 / filetype の絞り込み / 空語の判定 /
`<C-k>` の配線)。🚨 当初は `::` を含む語も定数扱いする分岐を入れていたが、変異で外しても
green だった。調べると Ruby バッファの `iskeyword` は `@,48-57,_,192-255` で `:` を含まず、
**`<cword>` は `Loaders::BaseLoader` の上でも `BaseLoader` しか返さない** (実測)。
到達しない死に分岐だったので削除した。

## 残タスク (スコープ外)

- 他の Rails project (ubipay 3.1.2 / marshmallow 3.2.2 / filetree-meta-manager 3.2.2) は
  各 ruby version へ `gem install ruby-lsp` するまで solargraph のまま。
  monolink-server は ruby 2.6.6 なので ruby-lsp を入れられない (>= 3.0 要求)
- `rbenv-default-gems` の導入 (新しい ruby を入れたときの自動追従)。未導入
- ubiregi-server 直下に 7 月上旬の Claude Code worktree が 2 つ残っており、
  tailwindcss LSP がその中まで舐めている (solargraph の索引対象は 2670 件で
  `max_files: 5000` 未満なので solargraph 側は無害。実測済み)
