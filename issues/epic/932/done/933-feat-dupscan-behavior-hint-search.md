# 933 (feat): dupscanで呼出先・処理構造を手掛かりに既存helperを探す

起票日: 2026-10-26

## 採用した最小版（2026-10-28）

`callers -selector strings.Join Join [dir ...]`を実装した。GoのASTで単純なidentifier/selector chainを取り、既存の末尾名照合と、指定した場合の引数数条件にANDする。位置引数の名前は引き続き必須。queryのdot前後の空白/改行、sourceの空白/改行/括弧を正規化し、callの位置・囲む関数・test ownerは既存経路を保つ。コメント/文字列の一致を実callにはしない。

selector指定時はGoのみを読む。同じscope内のSwiftを除いたこと、import/alias/receiverを解決しないことをtext/jsonの0件でも明示する。`tw.Join`やdot importは追跡せず、同じ字面のshadowingは混ざる。動的receiver、indexed callee（明示型引数の`pkg.F[T]`も含む）は対象外。Go sourceのないscopeはrc=2で対象外を説明する。構文は一致しても別責務のhelperかどうかはコード確認を残す。

### search拡張との比較・採否

当初の`search -calls`案は、単位へのcall逆引き、型filterとの合成、hint-only受付、順位/根拠、925の出力予算への統合が必要になる。今回は既存callersのowner表示からhelperへ到達でき、同末尾の別calleeを除く不足だけを独立して直せるため採らなかった。925のquery必須/OR順位は変更しない。Go Callのselector metadataはJSON非公開にし、report mappingは明示したqueryのselectorだけをopt-in出力へ設定する。既定callers/index/report/gateの照合/JSON/hashを変えない。

range/加算等の本体feature、任意自然言語の意味理解、型/所属/alias解決、共通v2/budget/候補IDは今回不採用。本体の重なりが必要な実課題は895へ、agentの正しい再利用・不要な重複追加・総context/時間/費用の比較は928へ委ねる。これらが必要な具体的な不足を観測した場合に、必要な範囲だけ再起票する。下の当初案は履歴であり、本issueの未完了条件ではない。

### 固定snapshotでの比較

inputはe266c5daの`src/dupscan`と`src/tuikit/markdown`をgit archiveした同一snapshot。自身の実装/test追加を入力へ混ぜず、同じ変更後binaryでfilterなし/ありを比べた。全件確認の比較は`-limit 0`で行い、text/jsonのstdout UTF-8 bytesを数えた。両方stderrは0bytes。

| 問い合わせ / scope | call件数 前→後 | text bytes 前→後 | JSON bytes 前→後 |
|---|---:|---:|---:|
| Join / src/dupscan、selector strings.Join | 123 → 48 | 22,924 → 8,688 | 37,221 → 14,459 |
| Of / src/tuikit/markdown、selector termwidth.Of | 29 → 29 | 4,264 → 4,592 | 7,569 → 7,931 |

固定課題は「名前を知らず、[]string/戻りstringと使う字面strings.Joinが分かるとき、先頭n件と残数を表示する既存処理headListを確認する」。同じscopeの型indexは9候補でheadListは7番目、`search -query join -returns string -param-type '[]string'`は0候補。evidence strings.Joinはdoc0/test10/source36でheadListへ辿れ、Called集約の次操作はJudgeだった。selectorはheadListの2callを保持するが、通常callersと同じ4・5番目であり、到達順位を改善したとは言わない。

適切な`rg -n -F strings.Join -g '*.go' src/dupscan`も同snapshotで46行/5,494bytesと短い。evidenceは行/署名の文字数制限やpath groupingを持ち、callersとは同じ情報量ではない。比較が示すのは全件を見る場合に別構文calleeの75callを除ける局所的なfilterの価値であり、headList採用までの最小確認量やrgよりよい探索効率の証明ではない。Ofは同じ構文calleeばかりなので省略できず、注意表示分だけ増える。この例を機能の改善量に数えない。

## 方向性の再評価（2026-10-27）

対象: a5b4196e の実装と既存測定。

**判断: selectorを手掛かりにする余地はあるが、既存の到達経路との比較が着手条件。** 現行 `index -returns int -name Width -limit 0 src/tuikit/markdown` はspansWidth/tableColWidthsを返す。`callers -limit 3 Of src/tuikit/markdown` はcollectLinks/parseFence等を含む29件、`evidence -limit 1 termwidth.Of src/tuikit/markdown` はdoc0/test16/source12で、各hit行のCallsにはOfを表示するが、repo内定義名のCalled集約にはOfは入らない。候補到達経路が全く無いわけではない。evidenceの候補集約はrepo内定義名が対象なので、外部Of自体を再利用helperと確定しない。

次は名前を伏せた課題でcallers/evidence/型index/925のlexical search/適切なrgを比較し、selector-aware filterが正しい候補への到達や確認費用を改善するか測る。単なる末尾名Ofによる別receiver混入を対照にする。差がある場合だけcallee/selector構文filterを小さく追加し、alias・receiver・import・動的callの解決は未証明と表示する。

本体構造featureはさらに別の不足事例が条件。925は現在 `-query` 必須であり、用途語なしのhint-only規則は未実装の設計案。以下の全featureを一括追加することを次の作業にしない。

## 目的と未カバーだった点

書こうとしている処理の入出力や使うAPIは分かるが、既存関数の名前は知らない場面で候補を探す。grepで既存実装を見落とし、似たメソッドを新設する問題を減らす。[925](../925-feat-dupscan-helper-search.md)の初期searchはname/signature/doc先頭行のlexical検索であり、呼出先や本体構造を指定する機能は含まない。本issueはその追加材料を扱う。

## 現状の裏取り（2920a72a）

- `internal/report/index.go` のQuery.matchはname/returns/param-type/doc/fileを判定し、呼出先やループ本体の検索条件は無い。
- `internal/tree.Unit`はBodyを保持し、Go/SwiftのadapterにはCallsの収集経路がある。ただし、現在のGo callsはcalleeの末尾名を取り、receiver/import aliasの意味を解決しない。
- [931](../../../done/931-refactor-tuikit-domain-duplicates-and-discovery-check.md)のspansWidth/lineWidthは、入力の取り方が違ってもtermwidth.OfでTextの幅を加算する既存処理だった。name Widthのindexで見つけ直せたのは発見後であり、未知名からの効率向上を示したものではない。

## 当初の設計案（search統合/本体featureは今回不採用）

1. 初期版は925の検索へ呼出先hintを追加する案を比較する（例 `search -returns int -calls termwidth.Of`。現行CLIには無い）。用途語なしでも有効な型/呼出先hintがあれば検索できる規則を925と揃え、名前を推測することを必須にしない。
2. 呼出先は「末尾名」「sourceに書かれたselector/receiver」のどこを照合したかを返す。GoのCallsへ必要な構文metadataを追加する場合も、旧callers/近似数の意味を保つ。`termwidth.Of`がimport先の特定symbolに解決されたとは表示しない。alias/動的call/未知のcalleeの非対応範囲を示す。
3. 次の段階で、range/加算/呼出し等の限定した本体featureを検索材料にする。自由な自然言語から任意の動作を理解する機能とはしない。構造の類似度は[895](895-feat-dupscan-type3-candidates.md)の担当で、同じfingerprintの二重実装を避ける。
4. [924](924-perf-dupscan-exploration-latency.md)のAnalysisPlan/Viewで必要なfeatureだけ抽出し、型・語・呼出先・構造の一致根拠を候補ごとに示す。不明なfeatureを一致扱いにしない。暗黙に条件を緩めない。
5. 候補ID・予算・省略・coverageは[926](926-feat-dupscan-query-diagnostics.md)へ従う。型/責務/利用可否は未証明としてコード確認へつなぐ。構文が大きく違う同目的の実装まで必ず見つかるとは保証しない。

## 完了範囲の受け入れ条件

- [x] 名前を伏せた固定課題でcallers/evidence/型index/lexical search/適切なrgを比較し、到達可能性と出力量、改善しない例を記録した。helper採用の実課題A/Bは928に残す。
- [x] 同末尾の別receiver/alias、dynamic/indexed callee、source/queryの改行/空白/括弧、Unicode、同一行の複数call、同じ字面のshadowing、コメント/文字列をCLIで検査した。
- [x] 名前・指定時の引数数・selectorはAND。test owner/位置を保持し、Go限定/未解決の限界を0件でも表示する。unsupported-only scope、部分解析、textのlimitを検査した。
- [x] 旧JSONの内部metadata非露出を独立した期待schemaで検査し、固定Go/Swift fixtureの通常mode29条件で変更前後のstdout/stderr/rcがbyte一致した。全入力の互換を証明するものではない。
- [x] 11種類の退行を隔離worktreeで当て、構文検査/baselineの後に狙ったCLI回帰でredを確認した。lint/race/gateの結果は末尾に記録する。

## 進捗

- 2026-10-26: `issues: reserve dupscan behavior hints and source evidence search`で番号予約。既存925の対象fieldと不足する呼出先/本体hintを切り分け、設計と検証条件を記録した。実装・改善効果は未検証。

- 2026-10-26: 呼出先/本体hintの不足と925等との境界を確認。 読み取り専用Codex subagentで指摘の網羅性とcontext計測を反証レビューし、P3（現行/設計、着手条件、計測範囲）の3点を訂正した。文書の改善は、不足した指摘の担当と検証条件を明確にしたこと。実装・両効果の実証は未着手。

- 2026-10-27: 924で現行探索の比較用本体変換/正規化を省いた。933の処理構造hintは未実装で継続し、実装時には必要な構造metadataの収集を明示的に設計する。

- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`で現実装と残設計を再照合し、上記の最小作業・着手条件へ更新。改善は誤った未実装扱いと一括実装の前提を減らしたこと。コード変更、開発効率/重複追加率の改善は今回の成果に含めない。

- 2026-10-27: `docs(dupscan): correct evidence terminology and conditional acceptance`で反証レビューのP3を訂正。行CallsとCalled集約、節選択と定義選択、label指定の有無を区別し、現行評価と追加機能の受け入れ条件を分けた。P1/P2指摘なし。


- 2026-10-28: `feat(dupscan): filter Go callers by source selector`で上記の最小版を実装。新helper前にdupscan indexでcallee/selector/CallersSummaryを検索し、既存call収集/owner表示/位置/引数照合/parse警告を共用した。親の読み取り専用反証レビューはP1/P2なし、P3の「裸queryでも引数数で絞るように読める」注意文を指定時だけの条件へ直した。

### 検証と改善の評価

11変異はmetadata配線の除去、selector/名前/引数数の各filterの無効化、receiverの固定化、query空白の非正規化、Go限定走査の解除、既定JSONへのselector漏出、単純chain/識別子のvalidation除去、text限界表示の除去。すべて狙ったCLI回帰でredになった。29条件の互換比較はindex/def/refs/callersのtext/json×hit/0件、reportのtext/json/xcode、context/evidenceのhit/0件、near/explainのtext/json、searchのhit/0件。型indexやrgへ辿れる既存経路も対照に含め、検索0件を不存在の証拠にしていない。

改善は、使うcalleeの字面が既知のとき、無関係な構文calleeを明示的に除き、位置/ownerの確認情報を保って絞れること。代償はGo call metadata/query/parser/表示と回帰の追加、Goに限定すること、alias/型/動的callを解決しないこと、絞れない例では注意表示分だけ出力が増えること。候補数/全件bytesの減少を、一般的な再利用成功や不要な重複追加の減少と同一視しない。これらの効果は未検証で928に残す。

検査結果: 最終注意文を含む差分で`make -C src/dupscan lint test`のlint/全race testが成功。`dupscan gate -base e266c5da -strict-analysis src/dupscan`で新しい重複/追加除外なし。全src gateは既存の意図的な構文不正fixture `src/subproc/boundarycheck/testdata/broken/broken.go`をbase/headで同じ1件報告するため、全srcの完全解析は主張しない。Homebrew Bashでissue完了scriptを使用し、リンク/next/採番/裸path/claim/期限の6検査が成功。親のcodeレビューはP1/P2なし・P3注意文1件を修正、続くdocs反証レビューはP1/P2/P3追加指摘なし。

- 2026-10-28: 895で対象指定 `near -similar` の限定試作を完了。全体hashの異なる2歴史例を発見できたが、型/責務/利用可否は未証明。一般的な開発効率の評価は928で継続する。
