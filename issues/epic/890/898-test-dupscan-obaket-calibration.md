# 898 (test): dupscan を obaket と DualNoteApp に当てて既定の閾値を決め、肯定と否定の期待値と変異で効いていることを確かめる

起票日: 2026-10-24

## 概要

[891 (要件の正本)](891-design-dupscan-requirements.md) の既定値 (下限・閾値) は実測で決める。**規模と作りの違う 2 つの repo** (obaket と DualNoteApp (`~/src/working/DualNoteApp`。`macOS/` に Swift 96 ファイル、兄弟の `Shared/` (SwiftPM の package) に 88 ファイル、`iOS/` に 15 ファイル。`macOS/project.yml` (XcodeGen) が `../Shared` を参照する。`project.pbxproj` は追跡していないが、ignore されたままディスク上にはある)) に当てて決め、片方にだけ合わせない (ユーザー 2026-10-24: repo に依存しない汎用的な作りにしたい。途中の検証には DualNoteApp の macOS を使ってよい)。
obaket はこのマシンに無いので、先に DualNoteApp で測り、obaket は手に入るマシンで測る。DualNoteApp は `macOS/` と `Shared/` をまたぐ写しを見るため、repo の root を起点にする。dupscan は調べる repo に何も書かない (891 の不変条件)。
obaket では、PR 24 の前の木で肯定と否定の期待値を置き、変異で効いていることを確かめる。

Type-3 ([895](895-feat-dupscan-type3-candidates.md)) は後回しにしたので、下の Type-3 に触れる条件は 895 に着手したときに当てる。それまでは Type-2 と L1〜L3 の段だけで較正する。

追加の期待値 (反証レビュー (設計) 2026-10-24):
- 否定: 引数を取らず同じ型を返す別々の関数が、L3 で 1 組にならないこと (L3 は本体全体を 1 記号に畳む)
- Claude に同じ目的のヘルパーを新しく書かせた写しの実例で、Type-2 と L3 で取れる率を測る (PR 24 は Type-2 / L3 で 1 組になる例なので、895 を後回しにした判断を確かめられない)

**DualNoteApp での較正の進め方** (ユーザーの問い 2026-10-24: DualNoteApp と obaket は使っている道具が同じか、検証環境にする障壁は):
- DualNoteApp には PR 24 のような正解の一覧が無い。決めた commit で報告を出し、組を 1 つずつ「寄せるべき写し / 意図した写し / 誤検知」に分けて正解の一覧を作り、この issue に残す (誤検知の割合はそこから出す)
- 規模が小さい (Swift 199 ファイル) ので、所要と候補の絞り込みの判断には使わない。それは obaket で測る
- 開発中の repo なので、作業ツリーではなく決めた commit を git のオブジェクトから読んで測り、commit の hash を記録に残す

## 受け入れ条件

- [ ] obaket と DualNoteApp の全体に当てたときの組の数・誤検知の割合・ノード数の分布・所要を、repo ごとに実測して書く (下限と閾値の既定の根拠。2 つで食い違えば、どちらに合わせたかと理由を書く)
- [ ] DualNoteApp に当てた前後で、作業ツリーと `.git` の中身が変わっていないことを確かめる (`find <repo> -newer <印のファイル>` で、当てた後に更新されたファイルが 0 件。`git status` は `.git/index` を書き直しうるので確かめに使わない)
- [ ] 全体の期待値: PR 24 の前の木の全体に **既定の設定 (既定の下限と閾値)** で当て、6 つが出ること、**同じ組に入ってよいもの / 入ってはいけないもの** を期待値にする。
      反証レビューが文法から推した候補: L2 で同じ形になるもの (`BrowserSelectionStateTests.makeLocation` の `"c"` / `.bucket("b")`、`S3StubTestSupport.leaf` の `"connection"`)、
      L3 で入りうるもの (`.container(Self.identifier)` / `.root` / `context.connection.id` を使う版・`StorageResumableMoveUseCaseTests.location(connectionID:path:)`)。どれを入れるかを決めて書く
- [ ] 変異で効いていることを確かめる。肯定と否定を対で置く: 6 つのうち 1 つに文を 1 つ足すと Type-2 から外れて Type-3 に残る / 1 つ消すと組が 5 になる /
      段を外すと期待どおりに組が割れる / 「入ってはいけないもの」が入るように閾値を緩める・畳みすぎる変異で否定の期待値が赤になる (`mutate-verify` で当てる)

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 反証レビュー (設計) の指摘で、L3 の否定の期待値と、Claude に書かせた写しでの実測を足した
- 2026-10-24 895 を後回しにしたので、依存から外し、Type-3 の条件を 895 の着手時に回した
- 2026-10-24 較正の相手に DualNoteApp を足し、2 つの repo で既定を決めることにした (ユーザー 2026-10-24: repo に依存しない汎用的な作りにしたい。途中の検証には DualNoteApp の macOS を使ってよい)
- 2026-10-24 DualNoteApp での較正の進め方 (正解の一覧の作り方・規模の限界・決めた commit で測る) を書いた (ユーザーの問い 2026-10-24: DualNoteApp と obaket は使っている道具が同じか、検証環境にする障壁は)
- 2026-10-25 DualNoteApp の重複の一覧 (report) の実用性を確かめた (ユーザーの問い: 重複一覧は使い物になるか。DualNoteApp 26860a4・cdd68f08 以降の版。書き込み 0):
  - 既定 (関数の下限 10 節・クロージャ 20 節・L1〜L3) で組 133 (L1 115・L2 10・L3 8)・199 ファイル・2225 単位、1.8 秒。組の 83 が iOS と macOS にまたがり、31 が Shared の中、17 が 1 つの app の中。
  - 関数と property の組 72 を本体まで読んで分けた (見積もり): **寄せる価値があるもの約 40** と **雑音約 32**。
    - 価値がある側の大半は iOS と macOS の画面の処理の写し: `pastYearSection` (節 608)・App の `init` (364)・`editRow` (205)・`newTagForm` (169)・`applySearchQuery` (149)・`selectedSearchTags` (98)・`toggleTagInSearch` (82)・カレンダーの `cellBackground` (65) など。Shared へ寄せる候補として使える。Diary と Memo の ViewModel の補助 (`withEditingState` など 24 節) もここに数えた
    - 雑音: 1 行の委譲 (`DiaryTagStore.save` と `TagStore.save`)・名前で意味を分けている同じ本体 (`createPermission` / `deletePermission`、`reset` / `applyLoadSuccess`)・テストの setUp / tearDown の定型・L3 の偶然の一致 (`shiftMonth` / `shiftYear`・`resolvedEditorTextColor` / `resolvedEditorBackgroundColor`)。節 20 未満の関数・property の組 23 は、見た限りほぼ全部この側
  - クロージャの組 61 の多くは、同じ画面の写し (`MemoTabView.swift` ↔ `MemoColumn.swift` が 27 組・`MemoColumnDialogsModifier.swift` ↔ `MemoTabView.swift` が 7 組) の部品で、組ごとに並ぶと同じ事実が細切れに 30 行近く出る
  - 下限を上げたときの組の数: 関数の下限 20 で 110・30 で 89。L3 を外すと (下限 20 で) 104
  - **結論**: 一覧は使い物になる (寄せる価値のある写しを、特に iOS と macOS の画面の間で拾えている)。ただし既定のままでは約 4 分の 1 が雑音で、同じ画面の写しが細切れに並ぶ。
    改善の候補 (未実装・ユーザーの判断待ち): ①関数・property の下限を 20 節に上げる (雑音の大半が消える。gate の誤って止める割合も同時に見直す) ②既定の段から L3 を外す ③組の一覧の前に「ファイルの組ごとの件数と節の合計」を出し、大きい写しから並べる (画面ごとの写しが 1 行で分かる)
