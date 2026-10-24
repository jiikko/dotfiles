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
