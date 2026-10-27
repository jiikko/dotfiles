# 937 (feat): Goのテスト呼び出し元を重複比較単位と分けて保持する

起票日: 2026-10-26

## 問題と今回の実例

[936](../../../done/936-refactor-doctor-domain-duplicate-methods.md)でdoctorを共通化した後、次を実行した（実装commit 449fc99d）。

```sh
bin/dupscan callers sortedKeys src/doctor
bin/dupscan callers -format json sortedKeys src/doctor
bin/dupscan index -file src/doctor/internal/displaycheck/displaycheck_test.go src/doctor
```

実測: sortedKeysは製品外の検査本体checkから2箇所、TestSortedKeysPreservesDisplayCheckOrderから4箇所呼ばれる。callersは計6出現を返し、後者4件はtextが「(単位の外) [test]」、JSONのcallerは省略される。1行の2出現もline/colの異なる別件であり、同一行が2回表示されること自体は誤検出ではない。index -fileは同じファイルのgoodSpec/problemsOf/wantOneProblemだけを返し、Test関数は載らない。結果のfile/line/colとソースを手で照合して、どのテストから呼ぶかを補った。

これは呼び出しの取りこぼしやGo generic推論の不具合ではなく、テスト本体を比較から除く現行設計の制約。テストがあることは見えるが、その所有者が分からず、再利用後にどのテストを読むべきかを絞りにくい。doctor全体のテストやcontext効率が改善したという実測はまだ無い。

## 起票時のコードの根拠

- internal/lang/golang/golang.goのisTestFunc/funcDeclは、go testの入口とtestify相当のTestメソッドを判定し、比較用Unitsへ追加せず早期returnする。テスト内closureも比較から外す。
- Callの抽出は別に全ASTを読むので、呼び出し自体は残る。
- internal/report/callers.goのBuildCallers/enclosingはUnitsから囲む名前を探す。比較から除かれたTest関数と、本当にtop-levelの呼び出しが、どちらもcallerなしになる。CallerEntryのコメントもテストを単位の外に含めている。
- READMEの「テスト関数そのものと内部closureは比べない」は維持すべき契約。比較除外を単純に解除すると、テストの似たassertでreport/gateを埋めるおそれがある。

## 対応方針（実装済み）

1. **比較対象と呼び出しの所有者を分ける。** Go parserは除外対象のテストにも名前・receiver・signature・file・開始/終了位置・test分類を保持する。既存の宣言metadataを再利用できるか確認し、使えなければcall owner用の軽い構造を追加する。hashの正規化やclone用Unitsへの追加はしない。
2. callersは名前のある最内側の所有者を位置範囲から探す。通常関数・Test/Benchmark/Fuzz/Example・testify形のメソッドを扱う。t.Run等の匿名closureの内側も、外側の名前つきテストへ帰属させる。名前を確定できないときだけcaller不明を維持する。
3. 同一行・複数出現を混ぜないよう、callとownerに必要なcolumn/offset範囲を保持する。別スコープの同名テストはfile/位置を含む識別子で区別する。構文エラーをまたぐ曖昧なownerを確定表示しない。
4. 初期対応はcallersの所有者表示に絞る。indexの既定結果やGo def/refsの対応範囲を黙って広げない。テストoutlineを返す拡張は、既存[927](../927-feat-dupscan-agent-context.md)のcontext取得や[934](../934-feat-dupscan-source-evidence-search.md)のテスト証拠探索からこのmetadataを利用する段階で検討する。
5. textでは名前つきテスト内をそのsignatureで表示する。旧JSONのcallerが空から埋まる変更をREADME/fixtureに明記する。新しいkind/reason等のschemaが必要なら[926](../926-feat-dupscan-query-diagnostics.md)の版管理に従う。report/gateの比較対象と既存の違反判定は変えない。

## 既存issueとの分担

- [930](930-feat-dupscan-candidate-call-evidence.md)は引数省略・variadic/曖昧さによる照合/数値の根拠。本件は取得済みcallの囲むテスト名を扱う。
- [895](895-feat-dupscan-type3-candidates.md)は部分重複の候補抽出。936の最長prefixの検出漏れは895に既知例として記録済みで、本件では実装しない。
- 927/934は候補に渡すcontext/補助証拠の取得。本件はその前提となるGo test ownerのmetadataとcallers表示。

## 検証・受け入れ条件

- [x] fixtureを手で分類し、Test/Benchmark/Fuzz/Example、receiver付きTest、通常helper、top-level、t.Run内、同一行の複数callのownerを期待表にする。
- [x] doctorの上記6出現が維持され、4件のTest呼び出しが正しいテスト名を持つ。ownerを表示しても意味上の型/overload解決済みとは扱わない。
- [x] parse errorのあるowner、名前不明、同名別scopeを誤って確定表示しない。既存の解析不完全通知を保つ。
- [x] report/gateのUnits数・group・新規違反が変更前と一致。テスト本体/内部closureを重複比較へ混入させる変異を検出する回帰テストを置く。
- [x] 同じhelperの関連テストを特定する課題で、必要な追加ソース読取回数・転送bytes・誤ったowner採用を比較する。token/window削減は別途計測し、表示が増えただけで効率改善と判定しない。

## 進捗・改善評価

- 2026-10-26: `issues: reserve Go test caller ownership improvement`で番号予約。936の実装後に実CLIとGo adapter/reportを照合した。現在は設計のみで未実装。今回の改善は、実作業で必要だった手作業の補完箇所を再現可能な課題へ分離したこと。テスト関数の比較除外方針は継続し、効率改善は未検証。

- 2026-10-26: 読み取り専用Codex subagentで反証レビューし、6出現/4件のowner欠落、現行仕様、既存課題との分担を再照合した。追加指摘なし。`docs(doctor): complete consolidation and specify dupscan test owners`で設計本文を記録する。実装は未着手。

## 実装・検証結果（2026-10-27）

`feat(dupscan): name Go test callers without comparing test bodies`で実装。Go adapterはCalls要求時だけ、比較用Unitsとは独立したCallOwnerを構築し、callへ共有pointerを付ける。所有者はfile（Call側）/開始・終了line/column/receiver/署名/test/unreadableを持ち、終了位置はexclusive。Goの名前つき関数は入れ子に宣言できないため、位置範囲に属する宣言を照合する。top-level/不明は空のownerを明示し、旧来の行だけのUnits検索へ戻らない。SwiftはOwner=nilの従来経路を維持する。

既存Definitionはdef専用で終了columnを持たず、GoのSymbols対応範囲を広げずに使える軽いCallOwnerを追加した。一般関数の署名生成は共通関数へ寄せ、clone用の引数型・ラベルを従来どおり保持する。追加コストはCalls要求時のowner確保と位置照合。性能の改善は主張しない。

- doctorのsortedKeysは6出現を維持。checkから2件は従来どおり、テスト内4件のcallerはすべて `TestSortedKeysPreservesDisplayCheckOrder(t *testing.T)` になった。file/line/col/test/textは変更なし。
- 固定したdoctorを入力に、変更前後のバイナリでreport(text/json/xcode)、index(json)、strict gate(json)の5条件を照合し、rc/stdout/stderrがbyte単位で一致した。64 files / 359 units / groups=0も維持。
- CLI fixtureの13出現を手作業期待表で検証。Test/Benchmark/Fuzz/Example、2 receiverの同名メソッド、通常helper、t.Run内、同一行の2出現・2関数・top-levelを区別した。閉じないEOF関数はunknown、同ファイルの先行する正常なテストは名前を保持する。
- Callsあり/なしでUnitsが同一で、テスト本体/内部closureを含めずhelper1個だけとなる回帰テストを追加した。既存のGo除外テスト・Swift callerテストも成功。
- `make -C src/dupscan lint test`が成功（lint 0 issues、race付き全package）。dupscan自身に対するstrict gateは新規違反/解析エラーなし。gateの除外追加はない。
- ソースとテストを別担当へ読み取り専用で反証レビュー。ソース担当は境界/構文エラー/Swift互換を含め重大指摘なし。テスト担当は13出現/clone除外に修正必須の欠落なしと報告後、利用上限で終了した。残る構文エラー/Swift範囲はソース担当と全テストでも確認した。

### 固定した既知課題の再演

課題は「doctorのsortedKeysを呼ぶ名前つきテストを特定する」。共通の `callers sortedKeys src/doctor` に加え、変更前だけ `sed -n 110,140p src/doctor/internal/displaycheck/displaycheck_test.go` で名前を補う同じ手順を再演した。変更後はcallersだけで同じ1テスト名・4出現を特定した。

| 観測 | 変更前 | 変更後 |
|---|---:|---:|
| callersのstdout bytes | 962 | 1122 |
| 追加ソース読取 | 1回 / 752 bytes | 0回 / 0 bytes |
| 合計stdout bytes | 1714 | 1122 |
| 名前不明のtest call | 4 | 0 |
| 誤ったowner採用 | 0 | 0 |

これは既知例の固定手順の再演で、独立agentによる盲検比較ではない。旧版でも別の検索手順なら読取量は変わり、将来の課題全体の効率を保証しない。caller出力単体は160 bytes増えるが、この手順では752 bytesのソース補完が不要になった。実context token/window・一般的な速度・新規重複追加率は未測定。

一時原本は `/private/tmp/937-before-callers.{text,json}`、`937-after-callers.{text,json}`、`937-compatibility.json`、`937-known-task-replay.json`、`937-required-checks.log`。存続を保証しないため、結果と条件は上に転記した。

- 2026-10-27: 改善は「呼び出し自体があるのにテスト名だけをソースから補う」手作業を、この固定例で不要にしたこと。比較除外と既存index/report/gateの結果を維持した。型解決・Swiftテストのowner表示・Go test outline・一般的なcontext効果は対象外/未検証として既存課題で継続する。

- 2026-10-27: 本体を触らない一時コピーで、テストをclone単位へ混入・owner名を落とす・column境界を無視・壊れたownerを命名、の4変異を入れ、各回帰テストがすべて失敗して検出した。src全体の通常gateは新規違反なしだが、既知の意図的な壊れたfixture src/subproc/boundarycheck/testdata/broken/broken.goにより解析不完全。dupscan対象のstrict gate成功と区別し、全srcの完全解析成功とは扱わない。

- 2026-10-27: 追加処理の待ち時間を固定binary・doctor・warm-up後5回・前後交互順で補助観測。callersの中央値は35.13→36.60 ms、index(json)は38.40→38.51 msだった。CPU/OS cacheを制御しておらず統計的な回帰/改善判定ではない。calls metadataの構築にコストはあり、コマンド単体が高速化したとは扱わない。原本 `/private/tmp/937-latency-observation.json`。
- 2026-10-27: 実装と回帰は `feat(dupscan): name Go test callers without comparing test bodies`、README/検証・完了記録は `docs(dupscan): document test caller ownership and complete 937`へ分けた。最終文書も読み取り専用Codex subagentで反証レビューし、数値・契約・限定評価に修正必須の指摘なし。

- 2026-10-27: Markdownに適用されるissue整合検査、dupscanの適用lint/race、移動後の相対リンク検査、diff検査が成功。937の受け入れ条件を満たし完了へ移動した。

- 2026-10-27: `docs(dupscan): record verification after context and evidence integration`で最新origin/masterのcontext/evidence最小版との統合を記録。親issueの937/938行を両方保持して競合を解消した。統合後にlint/race全テスト、origin 4709b3bb基準のstrict gate（変わった7単位、新規違反なし）、doctor互換性5条件と6出現/4テスト名を再検証して成功。
