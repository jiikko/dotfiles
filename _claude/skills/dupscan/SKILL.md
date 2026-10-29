---
name: dupscan
version: 1.5.0
description: Swift か Go の repo で、新しいメソッド (ヘルパー・初期化・値を組み立てる関数) を書く前に同じものが既にないかを dupscan index で引き、実装が終わったセルフレビューで dupscan gate を回して増えた写しを寄せる。関連の実装 (定義・参照・ファイルの中身・呼び出し元) を grep より先に探すときにも使う (def / refs は Swift だけ。callers と index -file は Go も)。「dupscan」「重複したメソッド」「類似メソッドの統合」「既にあるヘルパーを探して」「セルフレビュー」「定義元」「参照」「呼び出し元」「似た関数」で発火。寄せるときはドメインの境界を確かめる (4 節)。Swift と Go 以外の repo では使わない (対応する言語は Swift と Go)。
---

# dupscan (書く前に引く・セルフレビューで写しを寄せる)

`dupscan` (実体は dotfiles の `bin/dupscan`。PATH 上にあるので、どの repo でもそのまま呼べる) は Swift と Go のメソッド (関数・メソッド・クロージャ) を構文木で読む。調べる repo には何も書かない。
詳しい使い方と限界は `~/dotfiles/src/dupscan/README.md`、検出の方法は `~/dotfiles/docs/dupscan-detection.md`。

## 1. 書く前に引く (気軽に)

写しが生まれやすいもの (ヘルパー・初期化・値を組み立てる関数・テストの fixture) を書く前に、同じものが既にないかを 1 回引く。
全部の定義の前に引く必要はない。上位 20 件を 1 行ずつ返す (DualNoteApp の 1130 単位で 0.5 秒。初回だけ build に時間がかかる)。

```sh
dupscan search -query "connection bucket" -returns StorageLocation -min-matched-terms 2 # 名前不明なら用途語をfield横断で探す (textのみ、字句候補・再利用未証明)
dupscan index -returns StorageLocation        # 作ろうとしている値の型 (戻り値の型の一部)
dupscan index -name location                  # 名前の候補の一部
dupscan index -param-type ConnectionID        # 受け取る型
dupscan index -doc bucket                     # doc comment の語 (最初の行だけ)
dupscan evidence bucket                       # 上で見つからないとき: doc の全文・テスト・ソースの行にある語から、doc の付いた宣言と、その語の行で呼ばれている名前を出す
```

**寄せ先を新設するとき (重複を直している最中) も引く**。「直している側」に居ると、新しい関数を書く場面だと気づかない。戻り値の型で引くと、本番側に同じ役の関数が既にあることが多い
(obaket の 1307 で `StorageLocation` を組む test helper の寄せ先を新設し、`StorageConnection.location(at:)` が本番にあるのに気づかず、後から寄せ直した。`index -returns StorageLocation` は 173 件で 7 番目、`-name location` を足すと 118 件の 1 番目に出た。issue 954 の P1)。

使えるものがあれば使う (repo が `.dupscan.json` の `shared` に書いた共通の置き場所の定義は `[shared]` と出て先に並ぶ。
他のファイルから使えないものは `[private]` か `[fileprivate]`、Go の別の package から使えない小文字の関数は `[unexported]` と出る。使うなら共通の置き場所へ寄せる)。無ければ書く。

Claude Code では、Go / Swift のファイルへ新しい `func` や型を書く直前に、hook (`inject-dupscan-candidates-on-new-func.sh`) が
名前の近い既存の関数と、同じ名前の型の宣言を注入する。型の注入が出たら、衝突する範囲 (Go は同じ package、Swift は同じ module の top-level どうしか同じ型の中。Swift の private / fileprivate の型は同じファイルの中だけ) なら build で落ちるので、名前を変えるか既存の型を使う。
関数の注入は名前が近いだけのことがあるので、役割を確かめてから使う (署名で引き直すなら上の `index -returns` / `search`)。

## 2. 調べもの (grep の前に)

Swift の定義・参照を探すときは grep より先にこちらを使う。構文木から引くので、コメント・文字列・呼び出し側の行が混ざらず、返る量が少ない
(LSP の「定義へ移動」「参照を検索」の代わり。Swift の 4 repo の 7 日で grep 2,882 回に対し dupscan 13 回だった。issue 921)。
文字列・コメント・設定ファイルを探すなら grep のほうが向く。Swift の repo でシンボルを grep すると、hook がこの節のコマンドを 1 行案内する。

```sh
dupscan def StorageClient                     # 定義を本体つきで (型は宣言とメンバーの一覧: protocol の要件・格納 property・enum の case を含む)
dupscan def StorageClient.upload              # 型を絞る (generic の引数と where 句は外して照合する)。make(x:_:) で引数ラベルも絞れる
dupscan refs StorageClient                    # 参照の一覧 ([型の位置] / [式の位置] と、それを囲む関数)。宣言名・引数名・局所の束縛・ラベルは数えない
dupscan callers 'make(x:_:)'                  # 呼び出しの箇所と、それを囲む関数 (名前だけでも引ける)
dupscan context StorageClient.upload          # 変更の前に: 上の def・refs・callers を 1 回の走査でまとめて (text だけ。参照・呼び出しは名前だけで当てる)
dupscan index -file Sources/Foo.swift         # このファイルに何があるか (outline)
```

型を解決しないので、同名の別の定義・参照も当たる (近似)。flag は名前・パスの前後どちらに書いてもよい。
`def` / `refs` / `context` は Swift だけを読む (Go のファイルを読まなかったことは stderr に出る)。Go の定義・参照は LSP (gopls) で引く。
`callers` と `index -file` は Go のファイルにも使える (Go に引数ラベルは無いので、`callers` は名前と引数の数で当てる)。
**関数を置き換える・消す前の呼び出し元の列挙は `dupscan callers -limit 0 <名前>` で上限なしに取る**(既定は 20 件で打ち切る)。出力の「… 他 N 件」の行や先頭の件数の行を、`grep '^src'` のような絞り込みで捨てない(取りこぼした呼び出しは、コンパイラで初めて気づく)。
Claudeの既存grep案内hookは、Goを明示した検索には `index -name` / `callers` を案内する。hook自身は解析/buildを起こさず、GoとSwiftの通知間隔を分ける。Codexで同hookが自動発火することは未検証。

## 3. セルフレビューで gate を回す

実装が終わったら、commit の前に回す。repo が `make test` や CI に `gate` を組み込んでいれば同じ判定がそこでも走るので、先に回せば手戻りが減る (組み込んでいない repo では、ここで回すのが唯一の確かめ)。

```sh
dupscan gate                    # upstream との merge-base から作業ツリーまでに増えた写し。無ければ 1 行で rc=0
```

**dir を書かずに打つ**。cwd がサブディレクトリでも repo 全体を走査する (別の dir にある同じ形の相手を見逃さない)。dir を明示すると、その配下だけを走査し、1 行目に「repo の一部だけを見ている (起点 …)」と出る。これが出たら、別の dir の相手は見えていない
(obaket で `macOS/` から打つと読めない構文のファイルが 17、root からだと 57 と、範囲が縮んでいた。issue 954 の P4)。

- **rc=1 (違反)**: 「新しい」に出た単位を、「同じ形」に出た既存の単位へ寄せる (呼び出しに置き換える・共通の置き場所へ移す)。寄せる前に 4 節で境界を確かめる。寄せてからもう一度回す
- **誤検知・意図して分けている写し**: 寄せずに除外を書く。単位の直前に `// dupscan:allow <false-positive|intentional> <理由>`、
  組ごとなら、メンバーの 1 つの直前に `// dupscan:allow-group <人数> <false-positive|intentional> <理由>` (人数が合う間だけ効き、写しが増えれば効かなくなって増えた写しが出る)、
  または違反の `-v` に出る「除外の 1 行」に `kind` と `reason` を埋めて repo の root の `.dupscan.json` に貼る。
  まとめて書くなら `-format json` の `exclude` (report の各組・gate の各違反) を使う。釘に本体の hash まで入っているので、text の `-v` を切り出さない
  (見出しで切ると別の組のメンバーを拾う)。組に「印: 全員の本体が 1 つの呼び出しを 1 回するだけ」(json は `single_call`) が付いていたら、
  共通の道具へ寄せた後に残る 1 行の委譲のことが多い。寄せる先が呼んでいる道具そのものなら、理由にそう書いて除外する (印だけでは除外しない。同じ形でまだ寄せられる写しもある)。
  **除外を書いたら、ユーザーへの報告に書く** (除外は gate を緩めるので、人の目に届けるのが唯一の歯止め。`gate` の 1 行目にも「新しく足された除外」と出る)
- **寄せる作業の途中で「自分が生んだ組・消した組」を見る**なら、まず `dupscan gate -v`。「新しい」の違反が新しい写しを含む組、「消えた組・縮んだ組」が寄せた結果、
  「新しく足された除外」が自分で足した除外の要素 (釘・glob など。黙らせた組の一覧ではない)。gate は前後の hash の差と同じではない
  (前からある写しに同じ修正を入れて形が移った組は違反にならず、組の除外は違反を数える前にかかる)。それも見たいときだけ report の json を前後で突き合わせる
- **rc=2 (判定不能)**: base が決まらない (upstream が無い) なら `-base origin/master` のように渡す
- 初めて入る module や、まとめて重複を寄せる前は `dupscan sweep <dir>` (組・下限を下げた組・部分木の対・名前の束・既存の helper の本体を手で展開した所・メンバーを写し合う型を 1 回の走査で。節 2 以降は雑音が混ざるので、1 組ずつ「同じ規則か」を読んでから寄せる)
- 1 つの関数について「同じ形のものがあるか・なぜ同じと判定したか」は `dupscan near <file>:<関数名>` / `dupscan explain <file>:<関数名>`

## 4. 寄せる前に境界を確かめる (ドメインを混ぜない)

dupscan が見るのは本体の形だけで、ドメインの境界は見ない。同じ形でも同じ規則とは限らない。gate の違反を寄せるときも、
report の既存の組をまとめて統合するときも、1 組ずつ次を確かめてから寄せる (ユーザー指示 2026-10-25)。

- **同じ規則か、偶然同じ形か**: 「片方の仕様が変わったら、もう片方も同じく変わるべきか」を問う。yes なら寄せる。
  no (provider ごとの規約・層ごとの責務がたまたま同じ書き方になっている) なら寄せず、`intentional` と理由を書いて除外する
- **寄せ先はその概念の持ち主に置く**: 使う側のどれもが既に依存してよい層・モジュールのうち、その規則を持つ所に置く。
  寄せるために、別のドメインの知識 (特定の provider の規約・上の層の型) を共通の層へ持ち込まない。依存の向きを逆にしない
- **何でも入る置き場所を作らない**: `Util` / `Helper` / `Common` のような、ドメインの語で名前の付かない置き場所に寄せない。
  寄せた先の型・関数の名前をドメインの語で付けられないなら、境界をまたいでいる兆候として寄せ方を考え直す
- **置き場所を取り込む全 target が、移す型の依存を持っているか**: 全 test target が取り込む共通の置き場所 (obaket の `shared/Tests/SharedTestSupport/`)
  へ移す前に、その型が参照する module (`import` と、シグネチャに出る型の持ち主) を洗う。まず取り込む target を数える
  (obaket では各 test target の `Helpers/SharedTestSupport` の symlink と `project.yml` の path)。次にその全部が `Package.swift` / `project.yml` でその module に依存しているか確かめる。
  1 つでも依存していなければそこは compile で落ちるので、その module に依存する target の中に置き、他の target が要るなら個別のパスで取り込む (`project.yml` の path)。
  上の「使う側のどれもが依存してよい層」は層の話で、これは target の話。Go の repo では `go.mod` と package の import で同じ確認をする。
  dupscan は型を解決しないので落ちる前には見つけられない (obaket 1307 で 2 回落ちた。dotfiles 959)
- 寄せない判断も成果。除外の理由には「どのドメインのどの規則が、なぜ別に変わりうるか」を書く (形が違うから、では書かない)
