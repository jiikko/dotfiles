# human: Ruby の参照検索を sidecar 索引にするか、ripgrep のまま確定するか決める

起票日: 2026-09-09
カテゴリ: human（人間しかできない判断。データが溜まるまで決められない）
期限: 2026-10-09
（**1 か月使ってから**見る。それより早く見ても行が溜まっていない）
出典: [issue 334](done/334-ruby-references-call-site-index.md) の todolist 最後の 1 行

## 何を決めてほしいか

`<C-k>`（参照検索）の Ruby メソッドを **ripgrep のまま確定する**か、
**呼び出し側索引の sidecar 化に進む**か。

## いつ・どうやって

nvim で **ファイルを 1 つ開いてから** `:DotfilesRefsStats` を実行し、**「Ruby:」の行**を見る。

🚨 **ファイルを開く前はコマンドが存在しない**。登録は `refs_usage.lua` の `M.setup()` で、
それを呼ぶ `dotfiles.lsp` の `M.setup` が `event = { "BufReadPre", "BufNewFile" }` の
遅延ロード（`_nviminit.lua`）配下にあるため（issue 354）。

```
Ruby: ripgrep N 回 / LSP (定数など) M 回 / rg の直後に LSP へ引き直し K 回 (X%)
```

## 判定基準（数字で決める。迷わないように固定しておく）

| 条件 | 判断 |
|---|---|
| **Ruby の ripgrep が 20 回未満** | 下の「母数が集まらない場合」を見る（延長は 1 回まで） |
| 引き直し **X < 10%** | **ripgrep のまま確定**。334 を done へ送り、`refs_usage` の記録機構も消す（`DotfilesRefsReset` でログを消してから） |
| 引き直し **X >= 10%** かつ LSP（定数）が 20 回以上 | **sidecar 化を検討する**。334 の「次の判断材料」節（常駐と IPC / 差分更新 / 定数の解決）から着手 |
| 引き直し X >= 10% だが LSP が少ない | メソッドの誤ヒットだけが問題。sidecar より **rg のパターンを絞る**方が安い |

## 🚨 決めたら記録機構を片付けること

`refs_usage` は**押した語をログに平文で残す**（仕事の repo の識別子が入る）。
段階 1 の判断が出たら `:DotfilesRefsReset` でログを消し、記録自体を外すか判断する
（`nvim/lua/dotfiles/refs_usage.lua` のコメントにもその旨がある）。

## 母数が集まらない場合（2026-09-11 追加。issue 354）

**「母数が集まらないこと」自体を答えとして扱う。** 延長は 1 回まで（= 2026-11-09 まで）で、
そこでも 20 回未満なら **ripgrep のまま確定して閉じる**。

理由: rg の false positive で困った回数が測れないのは、その機能が使われていないから。
**使われない機能のために sidecar（常駐 + IPC + 差分更新 + 定数の解決）を作る理由は無い。**
issue 334 段階 1 の目的は「重い仕組みを作る前に数字で決める」ことなので、
「数字が集まらない = 作らない」は目的に沿った決着であって判断の放棄ではない。

🚨 **確定の前に「使っていないもの」を分ける**: 母数が集まっていないとき、それが
**Ruby の参照検索を使っていない**からなのか、**nvim 自体をあまり使っていない**からなのかを
分ける。前者なら sidecar 不要で確定できるが、後者は母数不足の理由が別なので確定の根拠に
ならない。分け方はログ全体の `kind:"lsp"` の件数（他言語の `<C-k>` と `<leader>K` も
ここに入る）が増えているかを見る（issue 354）。

確定したときにやること（判定基準の表の「X < 10%」の行と同じ）: 334 を done へ送り、
`refs_usage` の記録機構を片付ける（`:DotfilesRefsReset` でログを消してから）。

### 2026-09-11 の調査（issue 354）

記録が 2 日間で 1 行も増えていないのを見て、記録側のバグを疑って実装を全部追った。
**記録側は健全**だった（呼び出し側 2 経路が実在 / `append` は mkdir + append writefile /
`setup` に依存しない / 上限に達していない / `<C-k>` のマッピングは LSP attach 時に張られ、
仕事の repo で solargraph 0.55.1 と ruby-lsp 0.26.11 が rc=0 で起動する /
`_tmux.conf:248` の `C-k` は prefix テーブルなので素の `<C-k>` は nvim に届く）。

つまり残るのは「全 filetype の `<C-k>` と `<leader>K` がどちらも走っていない」だけ
（`<leader>K` も記録するので、Ruby 以外で押していれば `kind:"lsp"` の行が増える）。
🚨 ただし `<C-k>` は buffer-local なので **LSP が attach していないバッファでは存在しない**。
記録の生死を確かめるときは `get_clients` と `:map <C-k>` を先に見る。上の条項はこれを踏まえたもの。
詳細は [issue 354](done/354-bug-refs-usage-log-not-growing.md)。

## 2026-09-09 時点の状態（ここから増えた分が判断材料）

```
Ruby: ripgrep 1 / LSP 0 / 引き直し 0 (0.0%)
全体: ripgrep 11 / LSP 0 / 引き直し 1
ft 未記録 11 件 (層別に使えない古い行)
```

12 行すべて実装当日（2026-09-08）の動作確認によるもので、**実運用のデータは実質ゼロ**。

## 2026-09-14 の実測 — 「レシーバを考慮できないのか」への回答

ユーザーからの問い（`Loaders::StockEventLoader#load` の参照を引いたら同名の別レシーバが大量に出た）。
**3 経路のうちレシーバを見るのは solargraph だけで、その solargraph は現実的な時間で返らない。**

| 経路 | レシーバを見るか | 根拠 / 実測 |
|---|---|---|
| rg（現行の `<C-k>`） | **原理的に不可能** | テキスト検索。`word_match = "-w"` は語境界であって受け手ではない |
| ruby-lsp 0.26.11（`<leader>K`） | **見ていない** | `reference_finder.rb:285` が `node.name.to_s == @target.method_name` の名前一致だけ。receiver を触るのは `Prism::SelfNode` かの判定（`:241` / `:248`）のみ |
| solargraph 0.55.1 | **見ている** | `library.rb#references_from` が候補ごとに `definitions_at()` を引き直し `referenced&.path == pin.path` で篩う（定義パス一致 = 実質的な型解決） |

🚨 **ubiregi-server で選ばれるのは ruby_lsp**。`M.ruby_server_for` の判定を再現した（Gemfile あり +
`RBENV_VERSION= ruby-lsp --version` が rc=0 / stdout `0.26.11`）。つまり
**`<leader>K` で引き直しても精度は上がらず、11 秒待つだけ**。

### 精度の実測（ubiregi-server / `load` / 2682 ファイル）

| 手段 | 件数 | 備考 |
|---|---|---|
| 現行 rg（Ruby 系に絞り + `-w`） | **243 行** | 最多は `@form.load(attributes)` 15 行（= 別クラス） |
| `\.load\b`（レシーバ付きだけ） | 161 行 | 同クラス内の `load(x)` / `self.load` / `&.load` を落とす |
| AST 索引 prototype（`measure.rb`） | **187 箇所**（呼び出し 168 + 宣言 19） | 落ちたのはコメント・文字列・シンボルだけ |

🚨 **AST 索引でもレシーバは分からない**。`@form.load` と `@loader.load` は両方残る。
prototype にレシーバ絞り込みを期待しないこと（「呼び出し元のレシーバ表記を索引に持つ」のは新規スコープ）。

### 速度の実測（solargraph 0.55.1 / `references_from` を直接叩く）

| 対象 | build | query | 結果 |
|---|---|---|---|
| `load`（rg 243 行） | 44.0s（source_maps 2682） | **40 分で打ち切り（rc=124）** | 返らず |
| `wrap_error`（rg 59 行 / AST 24） | 34.9s（source_maps 2682） | **19 分で打ち切り（rc=124）** | 返らず |

（同じ repo で ruby-lsp の references は 11.2 秒、rg は 0.17 秒）

候補 1 件ごとに `api_map.clip` + `clip.define` を回す構造なので、候補が少ない `wrap_error` でも返らない。

🚨 **結論は「実用外」ではなく「この条件では返らない。キャッシュが効いた状態は未実測」**。
下の confound のとおり gem の型情報が一切無い状態で `clip.define` を回しており、
**測ったのは degraded な solargraph** であって production の solargraph とは限らない
（`verify-execution-not-just-exit-code.md`「隔離環境での失敗も本番の失敗ではない」）。
**再測の trigger**: solargraph 経路を本気で検討するときは、先に gemspec cache が
成功する条件（親と `workspace.command_path` が解決する solargraph の版を一致させる）で測り直す。

🚨 **測り方で 2 回外した**（後続が同じ穴に落ちないように残す）:

1. `lib.map!` を呼ばないと `source_map_hash` が 1 ファイルしか持たず、`definitions_at` が
   **197 回とも縮退**して「28 秒で 1 件（宣言だけ）」という偽の結果が出た。
   LSP host は catalog 経路で全ソースを map するので、そこに合わせること
2. `references_from` は 0.55.1 で**クラッシュする条件を持つ**。候補がコメント内に落ちると
   `definitions_at` が nil を返し `library.rb:257` の `.first` が `NoMethodError`。
   計測では nil を `[]` に畳む wrapper を噛ませた（畳んだ回数を stderr に出して握り潰さない）

**未解消の confound**: 親（0.55.1）が `Process.spawn(workspace.command_path, 'cache', ...)` で
gemspec を caching しようとするが、rbenv shim が 0.56.2 を解決して `Could not find command "cache"` で
失敗し続ける。spawn 自体は **Thread 内の spawn + wait** なのでメインのクエリをブロックしないが、
**キャッシュが永久に完了しないため `cache_next_gemspec` が `sync_catalog` のたびに再突入し、
かつ gem の pin が無いまま型推論が走る**。後者が上の時間にどれだけ効いているかは分かっていない。
**gem cache が効いた状態での追試はしていない**。

### 判定基準の表への当てはめ

本件は表の最終行「引き直し X >= 10% だが LSP（定数）が少ない → sidecar より **rg のパターンを絞る**方が安い」に当たる。
ただし `-w` は「部分一致にすると別メソッドを大量に拾う」という既存判断で選ばれているので、
変えるなら `references_action` の純関数テスト + 変異検証が付く（安い改善ではない）。**未着手**。

🚨 **solargraph 経路は「落ちた」ではなく「保留」**。上の速度は degraded な条件での測定なので、
選択肢から外して判断を進めないこと。外すなら先に再測する。

残タスク:

- [ ] rg のパターンを絞るか、ノイズを受容するかの判断（ユーザー待ち）
- [ ] solargraph の再測（gemspec cache が成功する条件で）。やるかどうかも含めて未定
- [ ] `nvim/ruby-refs-index/README.md:49` の「production の `telescope.grep_string` はタイプを絞らない」が
      e61fc71e（rg を Ruby 系に絞った変更）で古い。`measure.rb` の rg baseline も土俵がずれている（406 行 vs production 243 行）

## 関連

- [issue 334](done/334-ruby-references-call-site-index.md) — 実測の記録（索引の構築時間 / メモリ / クエリ時間 / 精度差）
- `nvim/lua/dotfiles/refs_usage.lua` — 記録の実体
