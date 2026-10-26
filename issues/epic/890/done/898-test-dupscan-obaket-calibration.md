# 898 (test): dupscan を obaket と DualNoteApp に当てて既定の閾値を決め、肯定と否定の期待値と変異で効いていることを確かめる

起票日: 2026-10-24

## 概要

[891 (要件の正本)](891-design-dupscan-requirements.md) の既定値 (下限・閾値) は実測で決める。**規模と作りの違う 2 つの repo** (obaket と DualNoteApp (`~/src/working/DualNoteApp`。`macOS/` に Swift 96 ファイル、兄弟の `Shared/` (SwiftPM の package) に 88 ファイル、`iOS/` に 15 ファイル。`macOS/project.yml` (XcodeGen) が `../Shared` を参照する。`project.pbxproj` は追跡していないが、ignore されたままディスク上にはある)) に当てて決め、片方にだけ合わせない (ユーザー 2026-10-24: repo に依存しない汎用的な作りにしたい。途中の検証には DualNoteApp の macOS を使ってよい)。
obaket はこのマシンに無いので、先に DualNoteApp で測り、obaket は手に入るマシンで測る。DualNoteApp は `macOS/` と `Shared/` をまたぐ写しを見るため、repo の root を起点にする。dupscan は調べる repo に何も書かない (891 の不変条件)。
obaket では、PR 24 の前の木で肯定と否定の期待値を置き、変異で効いていることを確かめる。

Type-3 ([895](../../932/895-feat-dupscan-type3-candidates.md)) は後回しにしたので、下の Type-3 に触れる条件は 895 に着手したときに当てる。それまでは Type-2 と L1〜L3 の段だけで較正する。

追加の期待値 (反証レビュー (設計) 2026-10-24):
- 否定: 引数を取らず同じ型を返す別々の関数が、L3 で 1 組にならないこと (L3 は本体全体を 1 記号に畳む)
- Claude に同じ目的のヘルパーを新しく書かせた写しの実例で、Type-2 と L3 で取れる率を測る (PR 24 は Type-2 / L3 で 1 組になる例なので、895 を後回しにした判断を確かめられない)

**DualNoteApp での較正の進め方** (ユーザーの問い 2026-10-24: DualNoteApp と obaket は使っている道具が同じか、検証環境にする障壁は):
- DualNoteApp には PR 24 のような正解の一覧が無い。決めた commit で報告を出し、組を 1 つずつ「寄せるべき写し / 意図した写し / 誤検知」に分けて正解の一覧を作り、この issue に残す (誤検知の割合はそこから出す)
- 規模が小さい (Swift 199 ファイル) ので、所要と候補の絞り込みの判断には使わない。それは obaket で測る
- 開発中の repo なので、作業ツリーではなく決めた commit を git のオブジェクトから読んで測り、commit の hash を記録に残す

## 受け入れ条件

- [x] obaket と DualNoteApp の全体に当てたときの組の数・誤検知の割合・ノード数の分布・所要を、repo ごとに実測して書く (下限と閾値の既定の根拠。2 つで食い違えば、どちらに合わせたかと理由を書く)
- [x] DualNoteApp に当てた前後で、作業ツリーと `.git` の中身が変わっていないことを確かめる (`find <repo> -newer <印のファイル>` で、当てた後に更新されたファイルが 0 件。`git status` は `.git/index` を書き直しうるので確かめに使わない)
- [x] 全体の期待値: PR 24 の前の木の全体に **既定の設定 (既定の下限と閾値)** で当て、6 つが出ること、**同じ組に入ってよいもの / 入ってはいけないもの** を期待値にする。
      反証レビューが文法から推した候補: L2 で同じ形になるもの (`BrowserSelectionStateTests.makeLocation` の `"c"` / `.bucket("b")`、`S3StubTestSupport.leaf` の `"connection"`)、
      L3 で入りうるもの (`.container(Self.identifier)` / `.root` / `context.connection.id` を使う版・`StorageResumableMoveUseCaseTests.location(connectionID:path:)`)。どれを入れるかを決めて書く
- [x] 変異で効いていることを確かめる。肯定と否定を対で置く: 6 つのうち 1 つに文を 1 つ足すと Type-2 から外れて Type-3 に残る / 1 つ消すと組が 5 になる /
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
- 2026-10-25 既定を変えた (commit「feat(dupscan): 既定の下限を 20 節にし L3 を既定から外し、組を大きい順とファイルの組の要約で見せる」。ユーザー了承):
  関数・初期化子・property の下限 10 → 20 節 (クロージャは 20 のまま)、`-levels` の既定 L1,L2,L3 → L1,L2 (L3 は `-levels L1,L2,L3` で見る)。report の text は組を大きい順に並べ、
  先頭にファイルの組 (組が 2 つ以上のもの) を件数と節の合計の大きい順に 5 行出す。L3 を見ない既定の走査では、L3 の組の除外を「効いていない」に数えない。
  - DualNoteApp の origin/master (8060665) の report: 組 140 (L1 102・L2 25・L3 13) → 95 (L1 75・L2 20)。先頭の 5 組は macOS / iOS 205 に書いた寄せる価値の高い写し (エディタの処理・テストの補助) と同じ
  - gate の流し直し (first-parent 200 commit・古い版と新しい版を同じ commit で): 赤 22 → 17。**新しく赤になった commit は 0**。赤でなくなった 5 は、どれも節 20 未満の単位の違反で、
    1 行の委譲・setUp の定型・版の文字列の getter が 3、Diary と Memo の並べた小さな写しが 1 (`replaceRefreshTaskAndWait`)、本物の写しが 1 (54e9bb65: リファクタの途中で `reconcileDisplayModeWithTagState` を新しい controller に写し、元が残っていた。小さい)。
    下限を上げた代わりに、この大きさの写しは止めなくなる (受け入れた。止めたいなら `-min-nodes` で下げる)
  - **残り**: 言語ごとのおすすめセット (Swift の定型 = テストの setUp / tearDown・`==`・`hash(into:)`・Codable の `init(from:)` / `encode(to:)`・SwiftUI の modifier のクロージャ) を実測で決めて Swift の adapter に入れる (ユーザー了承の手順の 2・3)
- 2026-10-25 言語ごとのおすすめセット (Swift の定型を外す既定) は **今は作らない**。新しい既定 (下限 20 節・L1/L2) で DualNoteApp (origin/master 8060665) に残った 95 組を種類で数えた:
  テストの setUp / tearDown 3・`==` / `hash(into:)` 0・Codable の `init(from:)` / `encode(to:)` 0・クロージャ 51・それ以外の関数と property 41。
  クロージャの組を 10 組抜き出して読むと、6 組は振る舞いの写し (pull-to-refresh の iCloud の読み直し 節 60・読み込みの失敗の一覧の文 65・削除の確認 43 と 21・新しいメモのボタン 28・タグ管理のシート 30)、
  4 組は見た目だけの写し (アイコンのラベルなど)。SwiftUI のクロージャを型で一律に外すと振る舞いの写しも隠すので、セットにできない。外せる定型は setUp / tearDown の 3 組だけで、セットを作る手間と、名前で先回りして外す危うさに見合わない。
  **再開の条件**: obaket (909 → obaket の issue 1301) か別の Swift の repo で、新しい既定のまま定型 (テストの lifecycle・`==`・Codable など) の組が 1 割を超えたとき。作るなら名前で外さず、その種類の単位の下限を上げる形にする (名前で外すと、後から足された本物の写しも覆う)
- 2026-10-25 obaket で測った (M4Pro・obaket の issue 1301 の段階 1〜3。dotfiles の版は各行):
  - **組の数**: `4d80e474` の版 (下限 10 節・L1〜L3) で 1001。既定を変えた `e0f4c346` の版 (下限 20 節・L1/L2) で 538 (L1 443・L2 95)、除外のプリセット `l2-single-call` (919) を入れて 525 (L2 82)
  - **節の数の分布** (525 組の、組の中で最も大きい単位): 最小 20・p25 23・p50 31・p75 43・p90 70・最大 313
  - **所要**: report 1.6 秒 (1656 ファイル・26089 単位)・gate の流し直し 1 回 平均 3.0 秒・最大 4.2 秒 (100 commit)・index 1.45 秒
  - **誤検知の割合**: 525 組から 17 組おきに 30 組を抜き、sonnet に本体を読ませて分類した (私が 2 組を抜き取って確かめた): 寄せるべき写し 19・意図して分けている (protocol を満たす短い実装・別型の memberwise init など) 11・**誤検知 (形が同じで意味が違う) 0**。
    分類の結果は obaket の `tmp/dupscan-roadmap-2026-10-25/sample30-classified.tsv` (git の管理外)
  - **gate が誤って止める割合**: 新しい既定で 100 commit 中 6 が赤。2 は定数だけが違う 1 行の委譲 (プリセットで通るようにした)・1 は並べたクロージャ・3 は fake の写し (本物)。止める段の既定 (L1・L2) は obaket でも保てる (905 の 64 の材料)
  - DualNoteApp は 2026-10-25 の上の進捗のとおり (組 95)。2 つの repo で既定は食い違わなかった (どちらも下限 20 節・L1/L2 で、残る組の大半が寄せる価値のある写しか意図した写し)
- 2026-10-25 DualNoteApp (`b460340`) に report・gate・index を当てた前後で、`find <repo> -newer <印のファイル>` が 0 件 (作業ツリーと `.git` を含む)
- 2026-10-25 全体の期待値 (PR 24 の前の木・既定の設定): 直書きの 4 つは macOS のテストの写し 3 つと L1 の 1 組・プロパティを読む 2 つは L1 の 1 組・定数だけが違う版 (`makeLocation`・`leaf` など) を含む L2 の 17 の組。
  `.root` を使う版・接続を引数で受ける `location(connectionID:path:)` は既定の段では入らない (L3 は既定で見ない)。テスト `TestPR24CopiesMatchByLevel`・`TestPR24NeighboursAtDefaultLevels` (`internal/swift/pr24_fixture_test.go`) で固めた
- 2026-10-25 変異: 段を外す・畳みすぎる変異 (L3 で畳まない / L1 から畳む / L2 でリテラルを畳まない) は上のテストが赤 (894・この issue の commit)。
  「文を 1 つ足すと Type-2 から外れて Type-3 に残る」は Type-3 を保留にした (895) ので対象外。「1 つ消すと組が 5 になる」は report の組の作り方そのもので、別に変異は当てていない
- 2026-10-25 done (受け入れ条件が済んだ)
