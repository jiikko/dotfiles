# 930 (feat): dupscan の候補呼び出し数に照合条件と限界を表示する

起票日: 2026-10-26

## 目的・採用範囲

候補の「呼び出し0」を未使用の証拠と取り違えないよう、indexと単体callersのtextに構文近似の条件を1行表示する。既存のJSON、照合結果、index順位は維持する。contextは既に近似条件と既定引数省略の注意を表示している。

2026-10-27の再評価に従い、説明表示とmatcher拡張を分離した。実際に誤った再利用判断が起きたことや、説明追加で開発効率が上がることは未実証。本issueは出力の説明不足を扱い、default引数の新matcherは採用しない。

## 裏取りと再現

9d2e801fで確認した現象を09e983fcでも維持している。使い捨てgit repoに次のSwiftを保存しgit addした上で実行する。

```swift
struct Worker { func run(_ x: Int = 1) {} }
func use(_ w: Worker) { w.run(); w.run(2) }
```

```sh
dupscan index -format json -name run
dupscan callers -format json run
dupscan callers -format json 'run(_:)'
```

indexのcalls_approxは1、裸のcallersは2件、label指定は1件。`internal/report/index.go`のcallCountsは全LabelsをCallQueryへ渡し、`internal/report/callers.go`のmatchesは通常callでlabel個数一致を要求する。裸のcallersはlabelで絞らず、省略callも拾う。

これは既知の近似仕様であり、exact count契約への違反ではない。同名の別型/overloadの過大カウントもあるため、真の呼び出し数の上限・下限は保証しない。0件も未使用の証拠ではない。

## 実装した表示

- index: 名前と引数ラベル/個数で照合すること、型未解決による同名混入、0件の限界をtext先頭に1回表示する。
- 単体callers: 裸の名前は「引数ラベル・個数で絞らない」、label指定は「名前と引数ラベル/個数」と区別する。0件/非0件とも条件を表示する。
- Swiftファイルを読むscopeでlabel比較する場合だけ、既定引数省略の取りこぼしを説明する。Goだけのscopeや裸のcallersにSwiftの制約を適用しない。
- 一覧の行、数値、並び順、JSONはそのまま。新たな型解決、呼び出し候補生成や自動編集は追加しない。

## 検討案の決着と境界

- default metadata・省略可能引数のmatcher・possible候補一覧は不採用。現時点の問題は既知近似の説明不足であり、925のsearch順位もcallsを使わない。拡張には同名/同labelでdefaultの異なる定義の識別、partial omission、繰り返しlabel、曖昧な対応の保持が要る。説明追加のためにこの負担を導入しない。**再検討条件**: 説明を理解した上でも省略callの候補取得が必要になった具体例が出たとき。そのとき別途実装範囲と正解表を定める。
- Go variadic/spread・Swift trailing closureの対応拡張は行わない。現行の構文近似のままで、今回の文言は非対応要因を全列挙した完全な能力保証ではない。既存READMEの限界説明を維持する。
- resolution/name/labels/scope/limitations等の構造化metadataは、必要性が出た場合の条件付き候補として[926](926-feat-dupscan-query-diagnostics.md)で追跡し、本issueでは旧JSONへ欄を追加しない。
- 実作業での誤判断/不要な重複追加の効果測定は[928](928-feat-dupscan-agent-onboarding.md)で継続。対象課題にdefault引数helperを含める案は同issueへ渡し、数値が増えただけを成功としない。
- module/targetの実際の利用可否は[918](918-feat-dupscan-target-and-literal-table.md)の課題。呼び出し数で代用しない。

## 受け入れ条件・検証

- [x] 0件/非0件、index、裸/label指定callers、Swift/Goの表示をCLIから検証する10条件を追加。既存の一覧/順位テストは新headerの後に同じ行を期待する。
- [x] Swiftの1/2/1、同名別型の混入をJSONの回帰テストで固定し、意味上のexact countや上下界は保証しない。
- [x] 09e983fcと実CLIの48条件（Swift/Go/mixed × 8照会 × text/json）を比較。rc/stderr一致、JSONはbyte一致、textもheader1行以外byte一致。0件・limit・同名混入を含む。
- [x] 標準module lintは0 issues、race全テスト成功。strict gateはchanged_units=3、違反/解析失敗/新規除外なし。別worktreeで5変異を検出（index/callers配線削除、裸queryのlabel比較化、label説明分岐削除、GoへのSwift注意誤適用）。いずれも予測したCLIケースの説明/行assertが失敗した。
- [x] root agentによる読み取り専用の反証レビューでP1/P2なし。P3のREADME旧記述との矛盾を訂正し、926 metadataの継続を条件付き候補と明記した。

## 進捗・改善評価

- 2026-10-26: `issues: reserve dupscan coverage and call evidence follow-ups`で番号予約。CLIで既定引数の省略による過小カウントを再現し、既知仕様と誤解の危険を区別した。反証レビューとissue関連検査を完了。実装・利用効果は未検証だった。
- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`で説明追加を優先し、default matcherを別段階にした。裸のcallersには省略引数の制約を適用しないよう条件を訂正した。
- 2026-10-27: index/単体callersのtextへ照合条件と0件の限界を追加。改善は、READMEやcontextへ移動せず各出力で数値の意味を確認できること。代償はtextの説明1行とscope内拡張子の確認。数値精度、開発効率、重複追加率の改善は主張しない。

- 2026-10-27: `fix(dupscan): explain syntactic call counts in query text`で実装・回帰テストを保存。48条件の実CLI互換比較と5変異の検出を完了。構造化metadataは926の条件付き候補、実作業評価は928で継続し、採用しないmatcher拡張の理由と再検討条件を上に記録した。

- 2026-10-27: `docs(dupscan): complete call evidence explanations`でREADMEの新text契約、検証・反証結果と採用範囲を保存して完了。index/単体callersが近似条件を直接説明する一方、default matcherは今回採用せず、再検討条件を明示した。

- 完了移動後にBash 5でissueの相対リンク・番号一意性・nextリンク・外部からの旧パス参照検査が成功。dupscanの除外追加はない。
