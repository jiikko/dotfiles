# 918 (feat): dupscan の利用可否の未解決性を明示し、literalの構造差分を比較する

起票日: 2026-10-25

## 今回の採用範囲（2026-10-28）

2対象のL1構造差分とliteral tokenのbounded表示を実装した。target/initializerの意味解決は今回不採用とし、directoryや可視性だけで利用可能と断定しない。以下の2026-10-27再評価と当初案は経緯として残し、現在の仕様・検証・再検討条件は末尾「2対象比較の最小実装」を正本とする。

## 方向性の再評価（2026-10-27）

対象: a5b4196e の実装と既存測定。

**判断: 利用可否の断定を避ける価値はあるが、target解析とliteral比較を独立した小さな作業に分ける。** a5b4196e当時のUnit/UnitRef/GroupJSON/IndexEntryにtarget membershipはなく、ExplainSummaryは単位ごとの形式でliteral比較表もなかった。今回の実装は末尾の2対象比較に限り、target membershipは未解決のまま。925のsearchは全候補に `reuse=not_proven` を表示済み。一方、別targetの候補を実際に誤用した事例は未確認であり、3種のbuild system対応を急ぐ根拠にはしない。

- 所属: まず「非privateだからそのまま使える」と読める説明を見直し、unknownを保つ。1実プロジェクトのdirectory hintを検証し、それでも判断できない具体例が出たらmanifestを1形式だけ扱う。所属が判明しても依存/import・可視性・test-only・条件付きcompileを満たすとは限らない。Package.swiftは実行可能なSwiftであることも費用へ含める。
- literal: `internal/lang/swift/pr24_fixture_test.go` の歴史fixtureは6 helper中4つが直接literal、2つがstored property参照で、L3でのみ同群になる。最初は選択groupのL1構造差分として直接値と未解決の式を示す。property initializerの値解決は、変更・shadowing・継承・computed/default overrideを扱う別段階とし、実行時値と呼ばない。

所属側はunknown/複数所属/非対応manifest、literal側は直接値変更/未解決参照を別々に検証する。各々がレビュー時の誤判断や確認量を減らすか測ってから範囲を広げる。以下の一括設計は条件付き案であり、全項目の同時実装を次の作業にはしない。

> 2026-10-25に保留した所属/値の判定の案を、2026-10-26にepic 932へ戻した。現在は上記の小範囲を実装済みで、initializer/manifestの意味解決は不採用。旧案の全項目は実装要件ではなく、末尾の再検討条件に従う。

## 概要

epic 890 を閉じるときに、まだ作っていなかった 2 つの機能をここにまとめて保留にした。どちらも今の挙動から失われるものは無い (足されないだけ)。
Type-3 (少し違う写し) の[895](895-feat-dupscan-type3-candidates.md)も当時同じ理由で保留した（対象指定の試作は895で完了）。

## 1. 所属ターゲットの解決 (894・896・897 から移した)

- 各ファイルが属するターゲットを `Package.swift`・XcodeGen の `project.yml`・追跡されている `project.pbxproj` から解き、組のメンバー・索引の 1 行に出す (近似の印つき)。
  どれも無ければ「不明」。追跡されていない `project.pbxproj` は読まない (手元と CI で結果が割れないように)。`project.yml` の `include:`・templates など読めない書き方に当たったら、その target は「不明」
- 作っていない理由: `project.yml` を読むには YAML の parser の依存が要る (dupscan は Go の標準だけで作る方針。設定ファイルを JSON にしたのと同じ理由。897 の 2026-10-25 の進捗)。
  directoryは所属のhintに限る。obaketではshared配下の同じhelperをmacOSのtest targetも明示的に取り込む実例があり、パスだけで単一所属や利用可否は確定しない（末尾の386e2876検証）。
- **優先着手の判断材料**: 索引で、別のターゲットから使えないヘルパーを「使える」と誤って引いた事例が出たとき。または組の寄せ先を決めるのにターゲットが要る事例が出たとき

## 2. リテラルの違いの表 (896 から移した当初案)

- 当初案: 組のメンバー間で違うリテラルを表にし、プロパティから値を読むメンバーは初期値を 1 段解決して並べる (寄せてよいかの判断の材料)。近似の値には近似の印を付ける (人が読む形と JSON の欄の両方)
- 当初の受け入れ例（initializer解決は今回不採用）: PR 24 の 6 つで、プロパティから値を読む 2 つ (`BuildMoveManifestUseCaseTests` / `MovePathMappingTests`) の `connectionID` / `namespace` の初期値が解決され、直書きの 4 つの `"conn"` / `"bucket"` と並ぶ
- 当初の代替: `dupscan explain <file>:<関数名>` で段ごとの形を 1 つずつ見ていた。今回追加した2対象の`-compare`では、直接tokenと未解決式の差分を並べる。
- **優先着手の判断材料**: obaket で組を寄せる作業 (obaket の issue 1298) をしていて、メンバーごとの値の違いを 1 つずつ explain で見るのが手間になったとき

## 利用可能性の誤判断を避ける境界

候補のpublic/internal/unexportedやshared表示だけで、別module/targetから再利用できるとは判断しない。ビルド構成と依存関係を解く範囲を明示し、不明なら不明を返す。候補が実際に利用可能だと判定する機能は今回も実装しない。READMEとindexの説明は、非privateやshared表示だけで利用可能と読めない表現へ訂正した。既存のexplain・hash・group判定を維持し、同じ形が同じドメイン責務を持つという自動判断は追加しない。

## 進捗

- 2026-10-25 起票 (保留)。894 の所属ターゲット、896 のリテラルの違いの表と近似の印、897 の所属ターゲットの近似の印をここへ移した

- 2026-10-26: 未起票/未着手の指摘を管理する依頼を受け、同番号のままepic 932へ戻し、参照を張り直した。重複issueは作らず、実装・効果検証は未着手。

- 2026-10-26: 利用可能性の限界を追記し、再開条件の表現を優先着手の判断材料に修正。 読み取り専用Codex subagentで指摘の網羅性とcontext計測を反証レビューし、P3（現行/設計、着手条件、計測範囲）の3点を訂正した。文書の改善は、不足した指摘の担当と検証条件を明確にしたこと。実装・両効果の実証は未着手。

- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`で現実装と残設計を再照合し、上記の最小作業・着手条件へ更新。改善は誤った未実装扱いと一括実装の前提を減らしたこと。コード変更、開発効率/重複追加率の改善は今回の成果に含めない。

## 2対象比較の最小実装（2026-10-28）

再評価に従い、`explain -compare <file>:<unit|line>`で同scopeの2単位のL1構造を比較する。既定のexplain/near/report出力やhash/group/gateを変えず、独立schema `dupscan.explain.compare.v1`とtextを追加した。上の6例すべてのinitializer解決・3種manifest対応を今回の実装条件にはしない。

- native ASTのchildren境界を保持し、既存L1 writerを使ってheaderとunit全体の局所番号を作る。フォームの文字列を汎用S式として読み直さない。匿名tokenのKind自体が括弧/quoteになり得るため。
- 同じ子位置を比較し、異なるnode・追加・削除を構造pathで表示する。このpathはsource位置ではなく、編集対応の最適化・同一動作・再利用可否の証明ではない。宣言挿入による後続局所番号ずれは残す。
- 各差分でliteral tokenの字面を別欄へ出し、property/memberは未解決の式のまま残す。AST snippetが切れても短い`conn`/`bucket`を確認できる。initializerや実行時値を解決したとは呼ばない。
- 差分limitは1〜50（既定20）、snippet/token各160runes、literal tokenは各側最大8個。各対象source fileとL1表示は各1MiB、AST深さは256まで。入力/深さは新view生成前にも検査し、容量は生成中に数える。上限超過・対象の解析失敗・異言語はrc=2。scope内parse-error file数、全差分数、返却数、省略と予算を表示する。

### 固定例の結果・改善と限界

`internal/lang/swift/pr24_fixture_test.go`の6つの歴史helperをそのまま使い捨てrepoへ保存し、既存の直書き1つを基準に残り5つを比較した。直書きの他3つは差分0、property版2つは各2差分。後者で`conn`/`bucket`のliteral tokenと、未解決の`connectionID`/`namespace`を同時に確認できた。長いAST snippetは省略を表示した。

同じ6入力の従来explainを6回読むtextは15,491 bytes、2対象比較を5回読むtextは3,487 bytesだった。これは既知例の問い合わせ/出力量の観測であり、共通化判断の正しさ、runtime値の一致、agentの総時間/context token、一般的な効率の改善は未検証。比較のための新しい構造view・差分処理と予算検査が増えるコストがある一方、従来は別々の全形式を照合していた既知例で、差分と未解決点へ直接到達できる。

2894b806の実CLIと比較し、6helperの既定explain/near（text/json）と2条件report（text/json）の計28条件でrc/stdout/stderrがbyte一致した。新CLI以外の数値・hash・group判定は変更していない。

### 広い案の採否

- property initializerの値解決は今回不採用。同名のshadowing・変更・継承・computed値などの責務が増え、現時点では未解決の式と直接tokenを示すだけで固定例を確認できる。再検討条件は、未解決表示だけでは実際の共通化レビューを完了できない例が出たとき。値の解決範囲と正解表を先に定める。
- targetの3形式parserと、directoryだけによる所属確定は今回不採用。obaket 386e2876の`macOS/project.yml:209`は`shared/Tests/ObaketInfrastructureTests/Helpers/HTTP/URLRequestBodyDrain.swift`をmacOSテストにも含める。同じpathは`shared/Package.swift`の`ObaketInfrastructureTests`にも所属し、directoryから単一targetや利用可否を確定できない。現実装ではtarget/依存は未解決のまま扱う。manifestを実装する場合も、所属とimport/依存・可視性・test-only・条件付きcompileの利用条件は分ける。
- 選択group全員の一括表・source位置対応・意味解決は条件付きの後段案。今回の2対象比較を使用し、問い合わせ回数や未解決値の確認が実作業の障害になった場合に範囲を選ぶ。

### 検証

- [x] 直接値変更/未解決property、同一入力、追加/削除、括弧/quote token、空body、局所番号ずれ、UTF-8 snippet、差分/token/byte/depth予算、対象の解析失敗/異言語を回帰テストで検証。
- [x] 標準module lint 0 issues / race全テスト成功。strict gateはchanged_units=9、違反/解析失敗/新規除外なし。
- [x] 旧CLI28条件のbyte互換と、上記歴史6helperの表示を確認。
- [x] 別worktreeで13変異を検出。CLI dispatch、L1からL2への退行、引数宣言順、深さ/serialized-byte/source-file予算、異言語除外、literal evidence配線、子差分の再帰、差分件数/UTF-8 snippet/token個数、parse-error metadataをそれぞれ壊し、予測した新assertでredを確認した。復元後の変異worktreeはclean。
- [x] root agentの読み取り専用反証でP1/P2なし。P3のREADME/indexコメントの「非privateならそのまま使える」を訂正し、sharedタグも利用可否の証明ではない旨を明記した。旧案より現在の採用範囲・不採用判断を優先することを明記した。

- 2026-10-28: `feat(dupscan): compare normalized ASTs for literal review`で2対象のnative L1比較と回帰を追加。ここまでの改善・追加コスト・未検証範囲を上に記録した。target/initializerの意味解決を実装済みとは扱わない。

- 2026-10-28: `test(dupscan): distinguish comparison view budget and parameter order`で、局所参照の出現順が宣言順と逆のfixtureと、node数は小さいがserialized bytesが大きいliteralのfixtureを追加。別々の制約を取り違える変異を区別できるようにした。

- 2026-10-28: 隔離worktreeの13変異をすべて検出し、元ファイルを復元した。新CLIは元のL1正規化writerを使い、既存hash経路は変更していない。残る完了条件は反証レビューと指摘への対応。

- 2026-10-28: `docs(dupscan): complete bounded literal comparison scope`でroot反証P3を反映し、誤った利用可能性の説明を訂正。追加fixture込み標準lint/raceとissue整合4検査も成功した。2対象比較を実装し、広い意味解決を不採用とする判断まで決着したため完了とする。一般的な作業効率や意味解決の効果は未検証のまま。

- 完了移動後にBash 5で相対リンク・番号一意性・旧パス参照・nextリンクの4検査が成功し、strict duplicate gateも再成功した。除外追加はない。旧案へのリンクは標準done scriptで張り直した。

- 2026-10-28: 895で対象指定 `near -similar` の限定試作を完了。全体hashの異なる2歴史例を発見できたが、型/責務/利用可否は未証明。本issueの役割は継続する。
