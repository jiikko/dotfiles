# 955 (research): dupscan を使って vlc-multi-video-player のテストの重複を解消したときの所見 (2026-10-28)

起票日: 2026-10-28

## 概要

vlc-multi-video-player で「dupscan を使ってテストの重複を解消する」作業をしたときの、dupscan の所見の記録。
作業の結果は vlc commit `test: 保存が失敗する UserDefaults の test double を TestUserDefaultsStore に寄せる`。

- 測った環境: dupscan は dotfiles `e9a07e7f` (`src/dupscan` の最後の commit)、vlc の HEAD は `3112c09`
- **観察の記録で、仕様や設計の主張ではないので、反証レビューは通していない** (ユーザー指示 2026-10-28 の skill の規定)
- 既存の issue との重複照合は、954 を読んだだけ。F1 は **954 の P3 (読めない構文の原因の特定) の答えの 1 つ**
- 起票したセッションでは `src/dupscan` を直していない (別のセッション「Dupscan epic 890 continuation」が dupscan を触っているため)

## 集計

- dupscan を使った回数: 9 (report 3・sweep 1・def 2・callers 1・gate 1・`-v` の誤用 1)
- grep に戻した回数: 2。どちらも Swift のシンボル (conformer の一覧 `: UserDefaultsStoring`・key の定義 `selectedTab`) で、
  `dupscan refs` / `def` で引けた。理由は習慣 (hook が `dupscan def selectedTab` を案内したが、先に grep を打っていた)
- 役に立った場面: 2
  - `sweep` の節 2 (下限 12 節) が、既定の report で 0 組のテストから、`TestUserDefaultsStore` の写しの test double を見つけた。
    寄せた結果、写しの方が使った UserDefaults の suite を後片付けしていなかったことも直せた
  - `def VideoGridViewModelTestFactory.createForTest` で、テストの factory の違い (player が Stub か Mock か) を 1 回で読めた
- 所見: 4 件 (見逃し 1・雑音 1・使い方が分かりにくい 2)

## F1 (見逃し): 関数の中の `nonisolated(unsafe) let` が 1 行あると、ファイル全体が「読めない構文」になり比較から外れる

- **場面**: `dupscan -v .` (vlc の root)。「読めない構文があるファイル 10」と出た
- **原因の特定**: 10 ファイルのうち 9 ファイルは、関数の中で `nonisolated(unsafe) let` / `var` を宣言している
  (例: `nonisolated(unsafe) let callback = block`)。型の直下の `nonisolated(unsafe) private var` しか持たないファイル
  (`AppLogger.swift`・`ShortcutsSettingsViewModel.swift`) は読めている
- **最小の再現** (6 行。git の repo に入れて `dupscan -v .`):

  ```swift
  final class A {
      func run(_ block: @escaping () -> Void) {
          nonisolated(unsafe) let callback = block
          callback()
      }
  }
  ```

  → `読めない構文があるファイル: a/Local.swift`。同じ本体から `nonisolated(unsafe)` を外した関数と、型の直下の
  `nonisolated(unsafe) private var` を持つファイルは読める
- **残る 1 ファイル** (`VideoGridLoadingCoordinator.swift`) は `nonisolated(unsafe)` を含まない。原因は未調査
- **影響**: そのファイルの単位はすべて比較の外になる (1 行のせいで、そのファイルの他の関数も重複の判定から消える)。
  vlc では `MockVideoPlayer`・`MainActorTestCase`・`TestStorageFixture` など、テストの共通の部品の 3 ファイルが入っていた
- **出力**: `-v` は「読めない構文があるファイル: <path>」だけで、行も理由も出ない (1,352 B)。954 の P3 の obaket では「読めない:」の行が
  単位ごとに出ていたので、出方が違う (理由は未確認)
- **提案**: Swift の文法 (tree-sitter) が `nonisolated(unsafe)` を局所の宣言の修飾子として扱えるかを見る。
  直せないなら、ファイルの丸ごとではなく、読めない文を含む関数だけを外す。`-v` に行と構文の種類を出す

## F2 (雑音): `sweep` の節 2 (下限 12 節) の 20 組のうち、寄せる価値があったのは 1 か所 (3 組) だけ

- **場面**: `dupscan sweep VLCMultiVideoPlayerTests DomainTestSupport` (6,881 B)
- **実際**: 節 2 に 20 組。寄せたのは `TestUserDefaultsStore` の写しの `load` / `hasValue` / `remove` の 3 組 (同じ 1 つの型の写し)
- 残り 17 組の内訳 (どれも寄せない):
  - setUp の中の 1 行の closure (L1m)
  - builder の `with` が 8 個
  - 2 つの protocol の要件をそれぞれ VM へ転送する stub
  - 違う mock の `init` (値を受けて `super.init()` を呼ぶだけ)
  - 1 行の `makeGridViewModel`
  - 2 行の `tearDown`
- **提案**: 型の単位で「メンバーの多くが別の型の写し」と出せると、今回の 3 組は 1 つの候補 (「この test double は
  `TestUserDefaultsStore` の写し」) になり、上に並ぶ。今は 3 組が、1 行の closure の組と同じ並びに混ざる

## F3 (使い方が分かりにくい): `sweep` の節 3 の先頭が、2 文の setUp どうしの対 (`1.000 w=144`)

- **場面**: 同じ `sweep` の節 3 (部分木が似ている対) は 1 対だけで、`IndexMappingConsistencyTests.mainActorSetUp` ~
  `SwapSlotsSynchronousRegressionTests.mainActorSetUp` (`1.000 w=144`)
- **実際**: どちらも `viewModel = VideoGridViewModelTestFactory.createForTest()` と `viewModel.updateLayout(.twoByTwo)` の 2 文で、寄せる意味が無い
- `w=144` が何の重みかが出力にも `--help` にも見当たらず、2 文の本体で 144 になる理由が分からなかった (未確認: 部分木の節の数か)
- **提案**: `w` の意味を節の見出しに書く。同じ型の setUp の 2 文のような、文の数が少ない対は下げる
- 2026-10-28 追記: `w` は commit `feat(dupscan): sweep の節 3 を共有している重みの大きい順に並べる。946 に節 3 の上位を読んだ結果を記録する` (26ce1b2a) で入った
  「共有している重み」で、節 3 の並びの鍵だった。意味が出力と `--help` に出ないことは変わらない

## F4 (使い方が分かりにくい): `dupscan . -v` が「`-v` は git の repo の中ではない」と出て rc=2

- **場面**: 読めないファイルの一覧を見たくて `dupscan . -v` と打った
- **実際**: `dupscan: -v は git の repo の中ではない: git rev-parse --show-toplevel: exit status 128: fatal: cannot change to '-v'` (153 B、rc=2)
- skill dupscan には「flag は名前・パスの前に書く」とある。こちらの誤用だが、エラーは `-v` をパスとして読んだことしか言わず、flag の位置を直せばよいことが分からない
- **提案**: パスの位置に `-` で始まる語が来たら「flag はパスの前に書く (`dupscan -v .`)」と出す (926 で後置 flag を直した範囲に入っていない形かもしれない。未確認)

## 進捗

- [x] 起票 (2026-10-28)
- [x] F1 の残り 1 ファイル (`VideoGridLoadingCoordinator.swift`) の原因 (2026-10-29。958 で解消済み)
- [x] F1 の文法を直す (`nonisolated(unsafe) let` は 954、残り 1 ファイルの形は 958 で回避済み)
- [x] F2 (2026-10-29。sweep の節 6「型の写し」。ユーザーの判断で、作る前に精度を測った)
- [x] F3 (2026-10-29。節 3 の見出しで w の意味を言う。文の数が少ない対を下げる案は採っていない)
- [x] F4 (2026-10-29)

- 2026-10-29: 954 を done にした。F1 の `nonisolated(unsafe) let` は 954 で回避済み (関数の中の出現を読み直す。読めないのはその関数だけで、ファイル全体が外れるわけではない)。同じ仕組みで空のタプルの引数 `f(())` も読めるようにした。
- 2026-10-29: F4 を直した (commit `fix(dupscan): 位置引数を取るどのサブコマンドも後置の flag を受ける (issue 955 F4)`)。
  - 原因: 後置の flag を受ける解釈 (`index` / `callers` などで使っていた) と、受けない解釈 (`flag.Parse` のまま) の 2 つがあり、report・gate・sweep・search・near・capabilities は後者だった。
    `dupscan . -v` は `-v` をパスとして読み、「-v は git の repo の中ではない」になっていた。エラーの文言を足すのではなく、受ける方 1 つ (`parseFlags`) にまとめた
  - 直した後の実測 (dotfiles の worktree): `dupscan . -v` / `sweep . -v` / `gate . -v` は、前置と同じ出力で rc は前置と同じ。ついでに `near <file>:<name> -v`・`explain ... -compare <x>`・`dupscan . -h` も直った (どれも変更前は rc=2)
  - 変わる挙動: `-` で始まる名前・パスを 2 つ目以降に置くと flag と読む (`dupscan a -v` は `-v` という dir を見ず verbose になる)。`--` の後ろに書けば以前どおり。README に書いた。doctor・agent-guide は位置引数を受けないので対象外
  - テスト: `TestTrailingFlagsMatchLeadingFlags` (report・gate・sweep・search・near で、前置と後置の出力が同じで、flag の無い run とは違う)
  - 変異: 5 つの command をそれぞれ旧来の解釈へ戻す 5 本と、位置引数より後ろの flag を捨てる 1 本が、すべて想定のサブテストで赤
  - 敵対的レビュー (opus) 1 周: Makefile・workflow・skill の呼び方は壊れない。P2 (`-` で始まる dir) は README に `--` を書いて受容、P3 (report と sweep のケースが flag の効きを見ていない) はテストを強めて直した
  - 検査: dupscan の test・lint、root の `make test-lint`・`make test-dupscan-gate`、`bin/skill-eval dupscan` (PASS) がいずれも rc=0
- 2026-10-29: F1 の残り 1 ファイルの原因を特定した。vlc を clone せず、`VideoGridLoadingCoordinator.swift` (vlc `3112c09`、267 行) だけを GitHub API で取り、1 ファイルの repo で `dupscan -v .` を比べた。
  - 起票時の版 (`e9a07e7f`): 「読めない構文があるファイル: a/C.swift」・比べなかった単位 5。今の版 (origin/master): 読めないファイル 0・20 単位
  - 原因の形: 151 行の `continuation.resume(returning: ())` (ラベル付きの引数に空のタプル)。5 行の最小の再現 (`c.resume(returning: ())` を持つ関数) も、旧版は読めない・今の版は読める。`c.resume()` の版はどちらでも読める
  - 直した commit: `git bisect` (src/dupscan の 21 commit) で `fix(dupscan): Swift の読み違え 4 種を読み直しで回避する (issue 958 マイルストーン 1)` (181335ff5)。ラベル付きの形 `g(value: ())` は `internal/lang/swift/isolation_test.go` の `TestMisparse958Repairs` の `A_labeled` が既に守っているので、テストは足していない
- 2026-10-29: F3 を直した (commit `feat(dupscan): sweep の節 3 の見出しで w の意味を言う (issue 955 F3)`)。見出しに「行頭は Dice と w = 共有している部分木の重みの合計 (寄せたときに減る量の目安) で、w の大きい順」を足した (README は既に書いていた)
  - 変異: 見出しから説明を消すと `TestSweepSimilarRanksByIsSharedWeightNotOnlyScore` だけが赤 (mutate-verify rc=0)
  - 敵対的レビューは省略 (出力の見出しの文言だけで、判定・並びは変えていない)
  - 検査: dupscan の test・lint、root の `make test-lint`・`make test-dupscan-gate` がいずれも rc=0
  - 提案の後半 (2 文の setUp のような文の数が少ない対を下げる) は採っていない。並びは 946 で w の大きい順に決めたもので、2 文で w=144 になる対を下げるなら w の数え方の見直しになる。再提起するなら、節 3 の上位に寄せる価値の無い小さい対が並ぶ実例を添える
- 2026-10-29: F2 を作った (commit `feat(dupscan): sweep の節 6 に、メンバーを写し合う型を出す (issue 955 F2)`)。作る前に、作ると精度が下がらないかを測った (ユーザーの判断: 「測定から始めて」)
  - 作る前の計測: 節 1・2 と同じ条件 (`-min-nodes 12 -levels L1,L2,L1m`) の report の JSON を型の対で数えた。母集合は dotfiles の src (Go)・flickorder-client (Swift 840 ファイル)・swift-smbee (Swift)・esa-cli (Go)
    - 素朴に「同じ組に入る対が 2 つ以上」で数えると、候補は 9 / 242 / 72 / 0。flickorder は、クロージャの大きい組 1 つが型の数の掛け算で対を増やし (TableController が多くの型と 17 組ずつ)、L1m の組の中の名前違いのメソッド (`touchTag` ~ `touchDetail`) が全部対になっていた
    - クロージャと L1m を除き、対でなく一致した自分のメンバーの数で数え直すと、8 / 46 / 21 / 0
    - 上位を読んで分けると、flickorder は本物の写し 11・`tableView` の準拠 5・setUp / tearDown 6・init 1、smbee は本物 10・transport の protocol の fake 4、dotfiles は 3 件とも本物 (FilerView ~ LanView は転送の包みの写しで判断が分かれる)。vlc の `TestUserDefaultsStore` と同じ形 (dotfiles の syncBuffer など 4 つの型) は対で数えると 6 行に割れる
  - 作り方 (敵対的レビュー 4 周で変えた):
    - 1 周目の実装 (同じ組の集合で結ばれた対をまとめる) → 2 周目 (全員が互いに写しの集まり) → 3 周目から (写しの対でつながる型 = 連結成分)。1 つの型が 2 つの候補に入ると、同じ組の重みを 2 度数えた (1・2 周目の P1)
    - 下限はメンバーの数 (両側に 2 つ以上)。組の数にすると、L2 で 1 つの組になる 2 メソッドの丸写しが落ちた (2 周目の P1)
    - 重みは 節 × (候補の中の型の数 − 1)。メンバーの数で数えると、1 つの型の中の写しまで重みに入って順位が入れ替わった (3 周目の P2)
    - 並びは 重み ÷ (型の数 − 1)。重みの和で並べると、`tableView` の準拠しか共通点の無い 7 つの型の候補 (節 588) が先頭に来た。比で並べると 7 番目で、上位 4 件は本物の写し。代わりに 5 つの型の TimerRuns の写しは 18 番目に下がる
    - 20 人以上の組は束ねるのをやめるが、その中の小さい組は束ねる。3 周目の P2 で「大きい群の部分の組が戻る」と指摘されて、大きい群に部分集合を覆わせたら、4 周目に「片方のメソッドが大きい群に入るだけで本物の丸写しが消える」と再現された。10 人の L1 の組はそれ自体が本物の写しなので、覆わせない側を採り、見出しと README に「その中の小さい組は束ねる」と書いた。再提起には、大きい群の中の小さい組が決まった形で上位を埋める実 repo の例を添える
  - 受容した制約 (README に記載): 同じ dir の同名の別の型 (Go の外部テスト package `p_test`・Swift の別ファイルの private の型) は 1 つの型に数える (単位が package を持たない) / 別の dir の extension は別の型として、型の中の写しを型の写しとして出す / A~B と B~C は 1 件で、3 つに共通するメンバーが無いこともある / 多くの型の写しと、決まった形で多くの型とつながる丸写しは下がる
  - 記録だけで直していないもの: 比が同じときの 2 番目の鍵 (重みの大きい順) を守るテストが無い (合成の入力でしか到達しない。3 周目)。判定「組が別の組に含まれるか」が group.covered・coveredByAny・sweepTypedGroups の 3 か所にある (4 周目のぼやき)
  - 実測 (直した後): 候補は dotfiles の src 3・flickorder 19・smbee 12・esa-cli 0。sweep の所要と RSS は節 6 の無い版と差が無い (flickorder 2.7〜2.9 秒 対 2.8 秒・約 250MB、smbee 2.1 秒・約 295MB。2 回ずつ)
  - テスト 24 本 (sweep_types_test.go)。変異 22 本がすべて、狙ったテストで赤 (mutate-verify-list)。途中で緑のまま通った変異 (closure・onetype・typesort・noessort) は、テストが観測していなかった形を足して赤にした。noessort は推測で fixture を 2 回動かしても緑で、変異の版を走らせて、ずれるのが Bb の行だと観測してから直した
  - 敵対的レビュー (opus) 4 周。4 周目の後の直し (大きい群に覆わせない) は 3 周目に読まれた扱いへ戻すもので、5 周目は回していない
  - 検査: dupscan の test・lint、root の `make test-lint`・`make test-dupscan-gate` が rc=0。skill dupscan の 1 行を変えたので、`bin/skill-eval dupscan` (PASS・2/2) と整合チェック 3 回 (skill の中・外の文書・使う人の目。食い違いなし)
