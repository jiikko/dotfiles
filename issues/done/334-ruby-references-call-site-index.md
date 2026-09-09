# Ruby の参照検索: 使用実績の計測と、呼び出し側索引プロトタイプ

種別: research / perf
起票: 2026-09-08
前提: [332](332-ruby-lsp-selection-by-probe.md) の追補 3（`<C-k>` を Ruby のメソッドだけ ripgrep へ振り分けた）

## 背景

ruby-lsp の `textDocument/references` は索引を使わず、要求のたびにワークスペース全体を
Prism で再パースする（実測 11.2 秒 / ubiregi-server）。索引 (`ruby_indexer::Entry`) が
持っているのは**宣言だけ**で、呼び出し側の逆引きが無いため。上流は
[#3051](https://github.com/Shopify/ruby-lsp/issues/3051) を closed as not planned にしている。

332 で `<C-k>` を「Ruby のメソッドは ripgrep（0.104 秒）／定数と他言語は LSP」に振り分けた。
🚨 この 0.104 秒は 2026-09-09 の追試で**再現しなかった**（production 条件では 0.17 秒。
末尾の追補 ③）。結論は変わらないが、数字を引用するときは追補の値を使うこと。
rg は速いが、AST を見ていないのでコメント・文字列・シンボルを拾う。

## 決めたこと（2 段階）

### 段階 1: 実運用でどれだけ困るかを数える

「rg の false positive で実際に困った回数」が分からないまま、重い仕組みを作らない。
`<C-k>` が rg 経路を通った回数と、その直後にユーザーが LSP 版へフォールバックした回数を
記録し、`:DotfilesRefsStats` で見られるようにする。困らないなら段階 2 は不要。

### 段階 2: 呼び出し側索引が現実的かを実測するプロトタイプ

**🚨 addon では references を差し替えられない**（実測 2026-09-08、ruby-lsp 0.26.11）:

- `RubyLsp::Addon` の公開フックは `create_code_lens_listener` / `create_hover_listener` /
  `create_document_symbol_listener` / `create_semantic_highlighting_listener` /
  `create_definition_listener` / `create_completion_listener` /
  `create_discover_tests_listener` の 7 つだけで、**references 用が無い**
- `server.rb:799` は `Requests::References.new(...).perform` を直接呼んでおり、addon の
  ディスパッチを通らない
- 索引側の `RubyIndexer::Enhancement` は「索引にエントリを足す」ためのもので、
  references はそのエントリを見ない

したがって「addon で O(1) にする」は公開 API では**不可能**。monkey patch は事実上の fork に
なるので採らない（upstream への PR も今回はやらない方針）。

代わりに、**判断に必要な数字だけを取るプロトタイプ**を書く:

- ワークスペースを 1 回だけ Prism でパースし、呼び出しノード名 → 位置の逆引きを構築する
- 測るのは ①構築時間 ②メモリ増分 ③クエリ時間 ④rg に対する精度差
  （rg のヒットのうち、AST 上は呼び出しでないもの＝コメント・文字列・シンボルの件数）
- この数字が良ければ「sidecar として常駐させ、nvim から叩く」が選択肢になる。
  悪ければ rg のままで確定し、この issue を閉じる

## 置き場所

- 段階 1（Lua）: `nvim/lua/dotfiles/refs_usage.lua`
- 段階 2（Ruby）: `nvim/ruby-refs-index/`（nvim 設定ツリーの下。Ruby なので `lua/` には置けない）

## todolist

- [x] 段階 1: 使用実績の記録と `:DotfilesRefsStats`
- [x] 段階 1: LSP 版へのフォールバック用マッピング（`<leader>K`。これが「困った」の観測点）
- [x] 段階 2: 逆引き索引プロトタイプ（`nvim/ruby-refs-index/measure.rb`）
- [x] 段階 2: ①構築時間 ②メモリ ③クエリ時間 ④精度差 の実測
- [x] 本文の断定を追試し、違っていた数字を書き戻す（2026-09-09。末尾の追補）
- [ ] 実測を受けての判断（sidecar 化 / rg のまま確定）← **人の判断待ち**
      → [issue 344](../344-human-decide-ruby-refs-sidecar-or-rg.md) へ切り出した（2026-09-09）。
      判定基準を数値で固定してあるので、データが溜まれば機械的に決まる

## 進捗: 段階 1 (commit `feat(334): 参照検索の使用実績を記録する`)

`nvim/lua/dotfiles/refs_usage.lua`。`<C-k>` が rg 経路 / LSP 経路のどちらを通ったかを
state ディレクトリの JSONL へ追記し、`:DotfilesRefsStats` で集計を出す。

「困った」の観測点は **`<leader>K`（LSP で引き直す）**。rg で引いた直後に**同じ語**を
これで引き直したときだけ fallback として数える（語が違う / 30 秒より離れているものは
通常の LSP 利用として数える。混ぜると率が水増しされて判断を誤る）。

記録の失敗は握り潰す（書けない環境でも `<C-k>` は動く）。ログは repo に入れない。

変異検証 6 本すべて red（語の一致を外す / 窓を外す / fallback へ書き換えない /
pcall を外す / `<C-k>` の記録を外す / `<leader>K` を消す）。

## 進捗: 段階 2 (commit `feat(334): 呼び出し側索引の実測プロトタイプ`)

`nvim/ruby-refs-index/measure.rb` + README。ワークスペースを 1 回 Prism でパースし、
呼び出しノードと宣言の逆引きを作って、①構築時間 ②メモリ ③クエリ ④rg との精度差を測る。

### 実測 (2026-09-08 / ubiregi-server / ruby 3.1.6 / prism 1.9.0 / 対象 2682 ファイル)

構築 **1.05〜1.11 秒** / メモリ **+36〜41 MB** /
索引は呼び出し 6729 種・315369 箇所、宣言 4219 種・7028 箇所。

| シンボル | クエリ | AST 箇所 | rg 行 | rg のゴミ |
|---|---|---|---|---|
| `wrap_error` | 3 µs | 24 | 24 | 0 |
| `symbolize_keys` | 4 µs | 403 | 404 | 1 |
| `perform` | 5 µs | 779 | 858 | 79 (9%) |
| `account` | 7 µs | 2372 | 22547 | **20282 (90%)** |

比較対象: ruby-lsp の references は同じプロジェクトで **11.2 秒**、rg は **0.104 秒**
（🚨 rg は追試で 0.17 秒。末尾の追補 ③）。

### 読み取れること

- **構築 1 秒・37 MB で O(1) 参照になる**。ruby-lsp が毎回 11 秒かけているのは、
  索引に呼び出し側を持たせていないからで、コストの問題ではない
- **rg 単体運用の弱点は「ありふれた名前」に出る**。`account` で 90% がゴミ。
  逆に固有名 (`wrap_error` / `symbolize_keys`) ではほぼ差が無い
- したがって段階 1 の計測で「よく使う語で困る」が出るなら sidecar 化の価値がある。
  固有名しか引かないなら rg のままでよい

### 実測中に踏んだもの (どちらも計測を静かに壊す形)

- **宣言を索引に入れないと rg の誤検出を過大に見積もる**。最初は呼び出しだけを集めたため
  `def wrap_error` の行が「rg だけに出る」に化けた。`references` は `includeDeclaration` で
  宣言も返すので土俵を合わせる必要がある
- **rg をパス引数なしで呼ぶと stdin を読む**。`Open3` 越しだと 0 バイト検索になり、
  黙って 0 件を返す (`bytes_searched: 0` で気づいた)

### 次の判断材料

sidecar 化するなら、未実装なのは ①常駐と IPC ②保存のたびの差分更新
③定数・名前空間の解決 (今は名前一致のみで、ruby-lsp のメソッド一致と同じ粒度)。

## 追補: 残りの最適化余地を実測した (2026-09-08)

索引完了後のリクエスト別レイテンシ (ubiregi-server / 2 回目の値):

```
hover                   0.4 ms      completion            0.2 ms
definition              0.4 ms      codeAction            0.5 ms
documentSymbol          0.2 ms      documentHighlight     0.7 ms
foldingRange            0.1 ms      formatting            9.7 ms
semanticTokens/full     0.3 ms      workspace/symbol    190.9 ms
references          11563.8 ms  ← ここだけ桁が違う
```

**references 以外は全部 1 ミリ秒未満**。触る価値がない。

### 索引時間は gem 除外ではほぼ削れない (A-B 実測)

大きい gem 10 個 (rubocop / brakeman / solargraph / yard / language_server-protocol /
fog-aws / mongo / capybara / rr / newrelic_rpm) を `excludedGems` で外した:

```
baseline  索引 10.7 秒
除外 10   索引  9.1 秒   ← -1.6 秒 (15%) だけ
```

15% のために gem へのジャンプを失うのは割に合わない (多くが開発グループ専用で既定の
`initial_excluded_gems` に入っていたのが理由と思われる)。永続キャッシュも上流に無いので、
現実的な緩和は「nvim の起動回数を減らす」だけ。

### 残っている唯一の体感課題: 定数・クラスの `<C-k>`

332 でメソッドは rg へ回したが、**定数は LSP 経路のまま = 11.5 秒**。判断材料は段階 1 の
記録で集まる (`:DotfilesRefsStats` の「LSP N 回」が定数を引いた回数)。多ければ sidecar 化の
価値があり、少なければ現状維持でよい。

`vendor/bundle` を repo 外へ出せば、この経路も 11.5 秒 → 約 5 秒になる (references の
再パース対象の 88% が vendor/bundle のため)。索引には効かない (既に除外済み)。

### やらない方がいいと分かったもの

- `enabledFeatures` で機能を減らす: 各リクエストが 1ms 未満なので効果ゼロ。索引は機能フラグと
  無関係に全部作るので索引時間も減らない
- `excludedPatterns` を増やす: 既定で `vendor/bundle` / `tmp` / `node_modules` / dotdir が
  外れており、残りは 10 ファイル規模 (追補 1 の実測)

## 追補: 敵対的レビューの P1 2 件を直した (2026-09-08)

commit `fix(334): 参照検索の記録に filetype を残し、<C-k> の行き先を純関数へ切り出す`。

**P1-1: 記録の分母が「定数を引いた回数」になっていなかった。**
`record` が `{kind, word, at}` しか残しておらず、`kind="lsp"` に **Ruby 以外の全 filetype の
`<C-k>`** が混ざっていた (`use_ripgrep_references` が false を返す経路がそのまま
`record("lsp", ...)` を呼ぶため)。本 issue は「`:DotfilesRefsStats` の LSP 回数 = 定数を
引いた回数」で sidecar 化を判断すると決めているので、Go/TS も触る環境では判断が無関係な数字の
上に乗っていた。しかも ft がログに無いので**事後に分離もできない**。

→ `record(kind, word, ft)` にして entry へ `ft` を残し、`stats()` が `by_ft` で層別する。
`format_stats()` は **Ruby の行を先に出す** (判断に使う数字がどれかを取り違えないため)。
ft を持たない古い行は `no_ft` として別に数える。時刻も `%z` 付きに変えた。

**P1-2: `<C-k>` の分岐を丸ごと反転させても全テストが緑だった。**
配線の固定が「マッピングのブロックに文字列が存在するか」しか見ておらず、
①rg と LSP の入れ替え ②`word_match = "-w"` の削除 ③`cwd` を nvim の cwd に固定、が
すべて素通りしていた。

→ 行き先を **`M.references_action(filetype, word, root)` の返り値で表明する**純関数へ切り出し、
テーブル駆動で返り値そのものを assert する。静的 pin は「その関数を通っているか」の 1 点に縮めた。

**あわせて P2-1 も直した** (切り出した当の行だったため): `cwd` を `on_attach` が受け取った
`client` から取ると、キーマップが LspAttach ごとに貼り直される都合で**後から attach した
client の root** を掴む。lspconfig の solargraph の filetypes は `{ "ruby" }` で eruby を
含まず、tailwindcss は `erb` / `eruby` を含むので、`.erb` では tailwind の root が入りうる。
→ `M.ruby_root_for(bufnr)` が Ruby のサーバを**名前で**選び、無ければ repo 境界へ落とす。

### 変異検証 (7 本すべて red)

| 変異 | 結果 |
|---|---|
| `references_action` の分岐を反転 | RED |
| `word_match = "-w"` を落とす | RED |
| `cwd` を nvim の cwd に固定 | RED |
| `ruby_root_for` が client 名で絞らない | RED |
| `record` が ft を捨てる | RED |
| `stats` が層別しない | RED |
| `<C-k>` が record に ft を渡さない | RED |

上 4 本はレビュー前は**すべて緑で通っていた**もの。

### 残り (旧「未着手」— 2026-09-09 に全件解消を確認)

**この節は 2026-09-09 時点で解消済み。** ここに並んでいた 4 件は、すぐ下の
「追補: 敵対的レビューの P2 / P3 を直した」(commit `fix(334): statusline の掃除・…`) で
全部直っている。実体を数え直した結果 (2026-09-09):

| 旧「残り」 | 現況 | 実体 |
|---|---|---|
| P2-2 statusline が固まる | 解消 | `nvim/lua/dotfiles/lsp.lua:588,613` の `LspDetach` / 検査は `tests/nvim/lsp_progress_check.lua` の「5. client が死んだら」節 (別 client を巻き添えにしない対照つき) |
| P2-3/P2-4/P3-5 `measure.rb` の土俵ずれ | 解消 | `measure.rb:112` が `rc >= 2` で raise / `:108` の比較側から `--type ruby` が外れ `-F` が付いた / `:149` が `.uniq` で突合 |
| P3-2 pin の射程 | 解消 | `lsp_progress_check.lua:210` が `_nviminit.lua` を全行走査 (コメント行を除く) |
| P3-6 ログのローテーション | 解消 | `refs_usage.lua:21` の `max_bytes` + `rotate_if_needed` / `:DotfilesRefsReset` |

残っているのは **todolist 最後の 1 行 (人の判断) だけ**。判断には `:DotfilesRefsStats` の
Ruby 行が要るので、実運用でログが溜まるまで閉じられない。

## 追補: 敵対的レビューの P2 / P3 を直した (2026-09-08)

commit `fix(334): statusline の掃除・再描画の抑止・ログの上限と、計測の土俵ずれを直す`。

**P2-2 (statusline が固まる)**: nvim は client 終了時に in-flight の要求へ complete を投げず、
索引の `$/progress` も end が来ない。掃除していなかったので「LSP: 参照を検索中…」や
「indexing NN%」が永久に残った。→ `LspDetach` でその client の pending を消し、progress も落とす
（別 client が索引中なら次の通知で戻る。`kind == "end"` と同じ扱い）。

**P2-5 (再描画の駆動源が増えた)**: lualine の statusline は関数評価で、再描画のたびに
`lualine_c` の `relative_path_from_git_root`（中で `vim.fs.root`）が走る。索引中の `$/progress` は
高頻度なので、無条件 `redrawstatus` は「通知 1 本 = 全ウィンドウの再評価」になる。
→ **表示が変わったときだけ**呼ぶ。あわせて「ラベルの無いメソッドで早期 return」の分岐を削除した
（再描画の抑止が refresh 側に移り、この分岐は観測できない冗長物になったため）。

**P3-2 (pin の射程)**: `vim.lsp.status` 直呼び禁止の検査が `lualine_x` の中しか見ておらず、
`lualine_c` へ移す書き換えが素通りした。→ `_nviminit.lua` 全体（コメント行を除く）で禁止。
🚨 素の部分一致だと**この禁止事項を説明した自分のコメントに当たる**ので、コメント行は除く。

**P3-6 (ログが際限なく伸びる)**: 記録には「押した語」= 仕事の repo の識別子が平文で残る。
→ 512 KB を超えたら古い半分を捨てる（判定は `fs_stat` のサイズだけで行い、超えたときだけ読む）。
判断が出たら消せるよう `:DotfilesRefsReset` を足した。

**P2-3 / P2-4 / P3-5 (計測が誤った数字を出す形)**: `measure.rb` が
①rg の rc=2（エラー）を「0 件」に畳んでいた ②production の `telescope.grep_string` は
ファイルタイプを絞らないのに計測側だけ `--type ruby` で絞っていた ③「行数」と「出現数」を
並べていた。→ rc>=2 は raise、比較側の type 絞りを外す、`-F` を付ける、突合はユニーク行で行う。

### 訂正: rg のゴミは過小評価だった

| シンボル | 旧 (誤: `--type ruby`) | 新 (production と同条件) |
|---|---|---|
| `wrap_error` | rg 24 行 / ゴミ **0** | rg 59 行 / ゴミ **35 (59%)** |
| `symbolize_keys` | rg 404 / ゴミ 1 | rg 445 / ゴミ 42 (9%) |
| `perform` | rg 858 / ゴミ 79 (9%) | rg 927 / ゴミ 148 (16%) |
| `account` | rg 22547 / ゴミ 20282 (90%) | rg 26662 / ゴミ 24397 (92%) |

**判断への影響**: 「固有名なら rg でほぼ同じ」は誤りだった。`wrap_error` でも 59% がゴミ
（`.erb` のビューや YAML に出る語を拾う）。sidecar 化の価値は当初の見立てより**高い**。
ただし決めるのは段階 1 の実測（`:DotfilesRefsStats` の Ruby 行）であって、この表ではない。

## 追補: 本文の断定を追試した (2026-09-09)

段階 1・段階 2 は実装済みだったので、実装ではなく **本文の断定を実コード・実環境に当てて
追試**した。結果は 3 値（**一致** / **違った** / **未実測**）で書き分ける。

### ① 「addon では references を差し替えられない」→ **一致**（実測）

実体は `~/src/ubiregi-server/vendor/bundle/ruby/3.1.0/gems/ruby-lsp-0.26.11`
（issue が名指しした版そのもの）。

- `lib/ruby_lsp/addon.rb` の `create_*_listener` は **7 個ちょうど**
  （`:245,250,255,259,264,269,274`）。references 用は無い
- `lib/ruby_lsp/server.rb:799` が `Requests::References.new(...).perform` を直接呼ぶ（行番号まで一致）
- 加えて `lib/ruby_lsp/requests/references.rb:63` が `Dir.glob(workspace/**/*.rb)` を回して
  `:69` で `Prism.parse_lex_file` している。**索引を一切見ずに毎回パースし直す**のがコードで直接読める

### ② 「references は 11.2 秒」→ **整合するが、LSP 経由は未実測**

ruby-lsp を起動せず、`references.rb:63-72` の支配的コスト（glob + `Prism.parse_lex_file`）だけを
同じ ruby 3.1.6 / prism 1.9.0 で再現して測った:

```
files=21290  vendor_bundle=18598 (87%)  glob=0.36-0.71s  parse_lex=7.98-9.40s  total=8.33-10.10s
```

`collect_references` の visitor と LSP の往復を足せば **11.2 秒は整合する**。
ただし ruby-lsp を実起動しての 11.2 秒そのものは **未実測**。

### ③ 「rg は 0.104 秒」→ **違った**（production 条件では 0.17 秒）

production は `telescope.grep_string` = **ファイルタイプを絞らない**（`references_action` が
渡すのは `-w` だけ）。同条件で 7 回ずつ測った（ubiregi-server / ripgrep 14.1.0）:

| 条件 | wrap_error | account | 行数 (wrap_error) |
|---|---|---|---|
| production（絞りなし `-w -F`） | min 0.150 / median 0.17 秒 | min 0.150 / median 0.17 秒 | 59 |
| `--type ruby`（旧・計測側だけの条件） | min 0.029 / median 0.031 秒 | min 0.034 秒 | 24 |

**0.104 秒はどちらの条件でも出ない。** 行数の側は決定的で、`--type ruby` の 24 行は上の
「訂正」表の**旧列**と一致し、絞りなしの 59 行は**新列**と一致する。つまり 0.104 秒は
**土俵ずれを直したときに測り直されずに残った数字**である可能性が高い。

判断への影響は無い: 0.17 秒でも LSP の 11.2 秒に対して **約 65 倍速い**ので、332 の
振り分けの前提は変わらない。

### ④ 「vendor/bundle を出せば 11.5 秒 → 約 5 秒」→ **否定できない**（内訳は一致、総和は未実測）

同じ再現で vendor と app を分けて測った:

```
vendor/bundle        files=18598  bytes=97.8MB  parse_lex=7.078s
app (vendor 以外)    files= 2692  bytes=14.1MB  parse_lex=1.016s
```

`lsp.lua:253` の注記「references 11.2s。うち parse が 7.0s で、その 88% が vendor/bundle の
18468 ファイル」は、**vendor 側のパース 7.078 秒とぴたり一致**した（ファイル数 18468 → 18598 は
その後の repo の増分）。パース費用は**バイト数にほぼ比例**している（app は全体の 12.6% の
バイトで 12.6% の時間）。

🚨 **一度ここで「1 秒台まで落ちる」と書きかけたが誤り**。落ちるのは**パースの内訳**であって
リクエスト全体ではない。11.2 秒から vendor のパース 7.078 秒を引くと **約 4.1 秒**で、
「約 5 秒」はむしろよく合う。

ただし**上限と下限のどちらに寄るかは未実測**: `references.rb:71` の `collect_references` は
パース結果ごとに走る visitor なので、これも vendor を外せば減る。減るぶんまで含めれば
1.5 秒前後まで行きうるし、固定費が大きければ 4 秒台で止まる。**LSP を実起動して測るまで
確定しない**（このセッションでは測っていない）。

なお vendor/bundle の実測比は **87%**（本文の 88% はほぼ一致）。

### ⑤ 未確認のまま残したもの

- 上流 [#3051](https://github.com/Shopify/ruby-lsp/issues/3051) の「closed as not planned」:
  **未確認**（ネットワークで確かめていない）
- 段階 2 の構築時間・メモリ・クエリ時間（`measure.rb` の数字）: **再実行していない**。
  ただし rg の行数 59 / 445 / 927 は今回の実測と**完全に一致**したので、訂正後の表の土俵は
  合っている（`account` だけ 26662 に対し生の行数は 27488 = 同一行に複数回出るぶん）

### 検査が走った証拠（`make test-nvim`、rc=0 / stderr 0 バイト）

```
[test-lsp-progress] OK lsp progress: …・LspDetach の掃除・lualine の配線
[test-lsp-references-dispatch] OK lsp references dispatch: 判定 10 ケース / 行き先の返り値 5 ケース / root の選択 3 ケース / <C-k> の配線
[test-refs-usage] OK refs usage: 記録 / fallback の窓と語の一致 / filetype の層別 / ログのローテーション / 書き込み失敗の握り潰し / <C-k>・<leader>K の配線
```

## 追補: 判断できるデータがまだ無い (2026-09-09 実測)

`:DotfilesRefsStats` が動くこと自体は確認した（`lsp.setup()` 経由で登録される。headless では
lazy.nvim の config が走る前だと `E492` になるが、対話セッションでは登録される）。

現在の中身:

```
Ruby: ripgrep 1 / LSP (定数など) 0 / rg の直後に LSP へ引き直し 0 (0.0%)  ← 判断に使う数字
全体: ripgrep 11 / LSP 0 / 引き直し 1  (Ruby 以外の <C-k> も含む)
ft 未記録 11 件 (層別に使えない古い行)
```

🚨 **12 行すべて 2026-09-08（実装した当日）のもので、実運用のデータではない**。
`word` は全行 `wrap_error` = 動作確認に使った語。しかも `ft` を記録するようになったのは
その後なので、**11 行は層別に使えない**。

つまり判断材料は実質ゼロ。**この issue は「作った機構が使われるのを待つ」状態**なので、
待ち方を [issue 344](../344-human-decide-ruby-refs-sidecar-or-rg.md) に切り出し、
判定基準（どの数字がいくつなら sidecar 化するか）を数値で固定した。

## 決着 2026-09-10: 実装は完了。残る「人の判断」は issue 344 が持つので done へ送る

todolist の未チェックは最後の 1 行（実測を受けての判断）だけで、それは 2026-09-09 に
[issue 344](../344-human-decide-ruby-refs-sidecar-or-rg.md) へ切り出し済み（判定基準を数値で固定してある）。
`issues/README.md` の「切り出し先が決まったら決着」に従い、この issue は done へ送る。

**判断が出た後の実装（sidecar 化を選んだ場合の常駐・IPC・差分更新・定数解決）は新規 issue を
立てる**。この issue に戻さない（本文が長く、段階 1/2 の実測記録が主体のため）。

### 移動にあたって実測したもの

- 参照元 4 本（`docs/nvim-ruby-lsp.md` / `nvim/ruby-refs-index/README.md` /
  `issues/335-*` / `issues/344-*`）。うち **2 本は `issues/` の外**で、
  `tests/issues/test_issue_links_valid.sh` の射程外 = 切れても CI は緑のまま
- そのため移動には `scripts/issue_done.sh`（issue 347 で新設）を使った。
  repo 全体を走査して参照を張り直し、「移動前のパスを指す参照が 0 件」を事後条件として確認する
- 🚨 同じ経路で切れていた実例が既にあった: `docs/nvim-ruby-lsp.md:4` の `../issues/332-…md`
  （332 を done へ送ったときの取りこぼし）。347 の対応で一緒に直した
