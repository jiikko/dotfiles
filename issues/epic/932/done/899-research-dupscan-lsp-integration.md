# 899 (research): dupscanの型・参照情報を非LSPで補完する方式を調査する

起票日: 2026-10-24

> 2026-10-24の「LSPは使わない」方針は維持する。2026-10-26の依頼に基づき、構文近似の限界と補完方式の調査をepic 932の未着手課題へ戻す。まずLSPを使わない静的解析の実現範囲・費用を比較し、LSP採用・常駐server導入を決定したとは扱わない。

## 結論（2026-10-27）

> 2026-10-30: Go の型の解決 (明示実行の go/packages) は、ユーザー決定で 989 として進める (986)。LSP・常駐 server を入れないことと、Swift の扱いは変えていない。

調査範囲は完了。**通常探索は現在の構文近似を維持し、LSP・常駐server・自動buildを導入しない。** 強い根拠が必要な場面の将来候補は、Goが明示実行のgo/packages、Swiftが既存の新鮮なindex storeのread-only読取。ただし今回決定したのは比較結果と候補/非採用範囲であり、本体への統合やoptional modeを実装・採用済みとは扱わない。

同名メソッドの静的な宣言先は非LSPでも区別できたが、interface/protocolの実行時dispatch・任意の変数値・全構成の参照数・再利用可否は保証できない。typed/indexed結果が無い/不完全/古いときはunknownを残し、構文近似をtypedへ格上げしない。表示契約の実装は926/930、所属/利用可能性は918で継続する。

## 概要

[891 (要件の正本)](../../890/done/891-design-dupscan-requirements.md) の「LSP は使わない」節 (以前は「構想: LSP と組み合わせる」節)。dupscan の出力のうち構文木だけでは不正確なもの (呼び出し元の数・プロパティの型と値・所属ターゲット) を、
LSP (sourcekit-lsp・gopls) か LSP ではない言語の解析の道具 (Swift の index store・Go の `go/packages`) から取れるかを調べる。
書く前に探す手順は [903](../../890/done/903-docs-dupscan-search-before-write-procedure.md) (dupscan の索引と、索引ができるまでの git grep。LSP は使わない)。
obaket で LSP を使う手順 (初回の待ち・worktree ごとの索引・結果の絞り方・マクロの重なり) は obaket issue 1299。

2026-10-24 の調査 ([901](../../890/done/901-research-dupscan-existing-tools.md)) で、名前で引く手段は Claude Code の LSP ツール (組み込み) と Serena (MCP。Swift・Go・Bash を含む 40 以上の言語) にあると分かった。
この issue は「dupscan の出力を補う値をどこから取るか」に絞り、名前で引く手段そのものは作らない。

## 受け入れ条件

- [x] 取る手段ごとに、取れるもの・所要・前提 (索引の有無・ビルドの要否) を表にして書く
- [x] 採る手段と、取れないときに構文木での近似と分かる形で出す方法を決める

## 起票時の調査方針

同名symbol/型別名/receiver/所属moduleを構文だけで正確に解決することは現在の設計範囲外。LSPは問い合わせの仕組みであり、型解決そのものを追加するのにLSP採用が必須とは限らない。非LSPの解析から始め、外部SDK・module依存・build条件を読む場合の待ち時間/メモリ/設定負担と正確さを比較する。

最初はGo/Swiftの具体的な誤解を避けるために必要な情報を列挙し、未解決を明示する現行近似との比較表を作る。静的に解けない呼出しやビルド情報不足はunknownとして残し、正確な参照数・共通化可否を保証しない。LSP不採用の現行方針、925のlexical検索、930の構文上の候補数は維持し、調査だけで方式採用や実装を完了扱いにしない。

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 名前で引く手段は既にあることを書き、範囲を絞った (901)
- 2026-10-24 保留にして pending/ へ移した (ユーザー決定: dupscan だけで依存も使い方もシンプルになるなら LSP は使わなくてよい)
- 2026-10-24 反証レビューの指摘で、本文の古い参照 (節名・903 の手順) と再開の条件を直した

- 2026-10-26: 未起票/未着手の指摘を管理する依頼を受け、同番号のままepic 932へ戻し、参照を張り直した。重複issueは作らず、実装・効果検証は未着手。

- 2026-10-26: 構文解析の限界と非LSP補完の調査条件を追記。LSP採用は決定しない。 読み取り専用Codex subagentで指摘の網羅性とcontext計測を反証レビューし、P3（現行/設計、着手条件、計測範囲）の3点を訂正した。文書の改善は、不足した指摘の担当と検証条件を明確にしたこと。実装・両効果の実証は未着手。

## 比較結果と前提

| 手段 | 得られる根拠 | 条件・費用 | 境界/今回の判断 |
|---|---|---|---|
| 現行dupscanの構文解析 | 宣言位置・署名・字面上の呼出し候補 | compiler/indexなし。未commit/untrackedも読む | build tagの有効側やreceiver型を解決しない。通常の探索はこれを維持 |
| Go標準go/typesを直接利用 | 選ばれたGoファイルの型・識別子のobject・selectorの宣言先・定数 | file選択/importer/build条件を別に用意。単独時間は未測定 | 今回go/packagesが返すTypesInfoで実証。module解決を手組みする方式は採らない |
| Go go/packages LoadSyntax + NeedModule | 選択構成のfile集合、PkgPath/module、TypesInfo/Selections/Uses等 | Go環境、go.mod/go.work、依存解決、GOOS/GOARCH/tagsが必要。依存export/cache生成の可能性あり | 型検査エラーはIllTyped/Errors。アプリの実行やリンクは不要だが、ビルド環境から独立ではない。将来の明示的な補完候補 |
| Swift compilerによるraw index store生成 | 宣言/参照USR・occurrence位置・roles・relations | toolchain/SDK/module/target/compile引数を揃え、index生成が必要 | 自動buildはしない。既存indexが無ければ索引未提供/unknown。今回は単一fileをtypecheckして生成 |
| libIndexStoreで既存raw store読取 | 上記のrecordをUSRで区別 | compilerに対応するlibraryと既存storeが必要。生record読取はLSP不要 | 内容・構成・toolchain一致が確認できる場合だけ候補として使う。今回は既存storeを直接読むprobe |
| IndexStoreDB | raw store上のsymbol/occurrence/relation queryをDBで高速化 | libIndexStoreに加えDB作成/更新/保持。今回はbuildも所要測定もしていない | CLIへ追加する利益は未検証、今回は採用しない |
| gopls / SourceKit-LSP | 定義/参照/型などのeditor向け問い合わせ | server接続、workspace設定/索引/依存・build条件の管理が必要。今回は起動/計測していない | LSP不採用の現行方針を維持。protocolへ換えれば情報が自動的に完全になるわけではない |

一次資料: [go/packages](https://pkg.go.dev/golang.org/x/tools/go/packages)のLoadMode/Configと[go/types](https://pkg.go.dev/go/types)のInfoを参照した。Swiftの[公式IndexStoreDB](https://github.com/swiftlang/indexstore-db)はraw storeのcompiler生成とlibIndexStore利用、DB層を説明している。[C API定義](https://github.com/swiftlang/llvm-project/blob/next/clang/include/indexstore/indexstore.h)のsymbol/occurrence/rolesをprobeに使用した。[gopls navigation](https://go.dev/gopls/features/navigation)にはbuild構成ごとの参照範囲、[SourceKit-LSP README](https://github.com/swiftlang/sourcekit-lsp)にはbuild/index状態によるglobal情報の制約が記載されている。資料は2026-10-27に確認した。

## 実コードでの裏どり

### Go: receiver・alias・build tag・定数

3fileの小さなmoduleを/tmpへ作成。go1.26.0、キャッシュ済みgolang.org/x/tools v0.52.0を固定し、`packages.LoadSyntax | packages.NeedModule`で`./...`を読むprobeを別moduleへbuildした。GOPROXY=off/GOSUMDB=offで実行し、dotfilesのgo.mod/go.sumは変更していない。このx/tools版はgo1.26.0を要求するため、dupscanのgo1.25.0最低条件へそのまま追加できると判断しない。

入力はA.Save/B.Save、`type Alias = A`、Saver interface、`const Limit = 3 + 4`、Runtime変数、Use内のa/b/interface呼出し、Choose内のChoice.Save。Choiceは`!alt`でA、`alt`でBにaliasする。

- 構文callers Saveは4出現。名前だけではA/B/interface/Choiceを区別しない。
- default構成のSelectionsはUseのAlias→A.Save、B→B.Save、interface→Saver.Save（宣言は分かるが実行時の具体実装はunknown）、Choice→A.Saveを区別した。`-tags=alt`ではChoiceだけがB.Saveへ変わった。
- PkgPath/moduleはresearchfixture、選択されたファイルは2個。構文の全file探索と、build条件で選択したtyped集合は母集団が異なるため、両方の件数を同じ意味で足さない。
- Limitは7として取得できた。Runtimeの任意の実行時点の値はunknownのままにした。型情報を値の実行結果と取り違えない。
- 未定義名を持つ別fileを足すとIllTyped=trueと診断を取得した。probeはこのpackageのtyped targetを採用しない。部分情報を正常な0件として返す案は採らない。

### Swift: USRと索引の鮮度

Swift 6.4（swiftlang-6.4.0.34.1、arm64 macOS）で、A/BがSaver protocolを実装し、Alias=Aとする6行のfixtureを使用。`swiftc -typecheck -module-name Probe899 -index-store-path <store> Sample.swift`で索引を作り、Xcode同梱libIndexStoreのC APIをPython ctypesから読むprobeを使った。IndexStoreDB/LSPは起動していない。

- 構文callers saveは3出現。索引では宣言3+呼出し3の計6 occurrenceを取得し、名前が同じでもA/B/SaverのUSRを区別した。
- line6のAlias/A呼出しは`s:8Probe8991AV4saveyyF`、Bは`s:8Probe8991BV4saveyyF`、protocolは`s:8Probe8995SaverP4saveyyF`だった。protocol呼出しにはdynamic roleがあり、具体的なruntime実装の確定とは扱わない。
- `b.save()`を`a.save()`へソースだけ書き換えても、未再生成storeは旧BのUSRを返した。新鮮さを確認しない索引は現在ソースの証拠にならない。確認後は入力を元へ戻した。
- 存在しないmoduleをimportしたtypecheckは、書込可能な `-module-cache-path /private/tmp/899-failure-module-cache` と別storeを指定し、rc=1かつ `no such module 'MissingResearchModule'` の診断を確認した。失敗後も標準library側の35 filesがstoreへ残ったため、ファイルがあることを成功/完全性とみなさない。生成失敗や部分storeを「参照なし」扱いにしない。
- 取得API version 16、raw format 5。今回のstoreは35 files / logical 1,373,408 bytes（標準library側も含む）。一般的なprojectのindexサイズではない。

## 所要・memoryの限定測定

macOS arm64/Apple M5。fixture・build済みprobe・dupscan binaryを固定し、各条件warm-up1回を除いた5回。下表は別々の仕事であり、同等機能の速度競争ではない。Swift index生成は毎回storeを削除して作り直すがcompiler module cacheはwarm。Goもmodule/build cacheが既にある。command順は表順に固定、CPU/OS cacheは制御せず、cold build・実project・server初期化は未測定。

`/usr/bin/time -l`のreported peak RSSを記録した。process tree全体の同時memory合計を保証する値ではない。libIndexStore読取はPython起動/ファイル列挙も含み、純粋なC API queryだけの時間ではない。

| command | wall中央値秒 | reported RSS中央値bytes | 5回の秒数 |
|---|---:|---:|---|
| go-syntax | 0.016503 | 11,829,248 | 0.016801 / 0.015940 / 0.017378 / 0.016503 / 0.015202 |
| go-packages | 0.247867 | 16,941,056 | 0.244554 / 0.247867 / 0.244102 / 0.257116 / 0.257588 |
| swift-syntax | 0.016382 | 12,943,360 | 0.016382 / 0.017858 / 0.016222 / 0.018084 / 0.016191 |
| swift-emit-index | 0.232270 | 140,083,200 | 0.234510 / 0.232201 / 0.232270 / 0.229219 / 0.234491 |
| swift-read-index | 0.024863 | 18,661,376 | 0.025565 / 0.026640 / 0.024251 / 0.024863 / 0.024307 |

Goの小fixtureでもtyped補完には環境解決の待ちがあり、通常探索へ常時載せる根拠にはならない。Swiftも既存index読取と生成では前提・費用が違う。profile924の探索高速化や、一般的な開発効率を実証するA/Bとは混同しない。

## 取れないときの契約と今後の境界

以下は926/930へ引き渡す**設計上の契約**であり、現在のJSONに実装済みのfieldではない。legacyの出力/数値を今回変更しない。

- 情報源をsyntactic / typed / indexedで区別し、候補ごとにfile/位置・静的な宣言先・使用した構成を持たせる。typed/indexedでもruntimeのcall回数や実装先、再利用可否をexactと呼ばない。
- unknownの理由をtypecheck_failed / missing_dependency / missing_index / stale_index / configuration_mismatch / dynamic_dispatch等で残す。typed診断があるpackageの部分解決を、完全なcoverageと一緒に表示しない。
- 型情報のkeyにはroot/worktree、ファイル内容、Go環境/tags/tests/overlay、依存graph・解析器版を含める。Swiftはsource内容、compile引数/target/SDK/module/toolchainとindexの対応が確認できなければ失効。存在するだけのstoreやmtimeだけで一致としない。
- typed/indexedが使えなくてもsyntax結果は消さず、別の根拠として残す。scopeと対象構成を示し、0を未使用・存在しない・利用できないと断定しない。構文数と型別の候補を単純合算しない。
- 新しいoptional mode、index更新/DB導入、LSP採用、cache設計を実装する場合は別途採否判断と実projectの検証が必要。今回のresearch完了は、それらの機能完了を意味しない。

## 再実行資料と改善評価

一時資料は `/private/tmp/899-go-fixture/` / `899-swift-fixture/`（入力）、`899-probe/`（Go module/probe）、`899-read-index.py`、`899-measure.py`、`899-evidence.json`、`899-measure-results.json`、`899-store-meta.json`。存続を保証しないため、fixtureは上の構成・呼出し順、flags/version、結果と全latency値を本文へ転記した。probeのC API利用はこのtoolchainでの小実験で、本体へ組み込む品質・ABI互換性を保証するものではない。

比較表とunknown契約を具体化し、型解決にLSPが必須という前提や、索引があれば現在のruntime情報まで正しいという誤判断を避けられるようにした。追加の本体コード/依存/常駐processは0。実projectの初回費用、全構成の網羅、動的dispatchの実際のtarget、開発効率/context・重複追加率の改善は未検証として残す。

- 2026-10-27: `docs(dupscan): compare non-LSP semantic evidence sources`で非LSP候補の実測、一次資料、通常経路を変えない判断とunknown契約を記録。899の調査を完了し、型情報統合や出力契約の実装は918/926/930で継続する。

- 2026-10-27: 読み取り専用Codex subagentの反証レビューでP2を1件修正。初回の依存欠落probeはmodule cacheの権限エラーで先に停止していたため、明示した一時module cacheで再実行し、狙ったmissing module診断をassertした。再レビューで解消済み。他の数値・USR・構成別宣言先・限定評価は原本と一致した。

<details>
<summary>再現用fixture全文とhash</summary>

Goは `module researchfixture` / `go 1.26.0` のgo.modを置き、a.goを次とする。

```go
package fixture

type A struct{}
func (A) Save() {}
type B struct{}
func (B) Save() {}
type Alias = A
type Saver interface { Save() }
const Limit = 3 + 4
var Runtime int
func Use(a Alias, b B, s Saver) { a.Save(); b.Save(); s.Save() }
func Choose(c Choice) { c.Save() }
```

default.go / alt.goはそれぞれ次の内容。

```go
//go:build !alt
package fixture
type Choice = A
```

```go
//go:build alt
package fixture
type Choice = B
```

SwiftのSample.swiftは次の内容。

```swift
protocol Saver { func save() }
struct A: Saver { func save() {} }
struct B: Saver { func save() {} }
typealias Alias = A
let constant = 3 + 4
func use(_ a: Alias, _ b: B, _ s: any Saver) { a.save(); b.save(); s.save() }
```

| file | SHA256 |
|---|---|
| a.go | `c4c4a8fbc8fc085d7f8f8c39930f21051adf7c650cacb138410101436f6bb787` |
| default.go | `769f66795b73ce102ad8a24b2dc6f508a83cbc4f81ca9a011a63c282933a04fd` |
| alt.go | `84744942cb092e3c3b93ee3905da1dbe00ad2febaadd2b7ee13bc8b1cc6965cf` |
| Sample.swift | `55b580fd4c10ff06bdd4c06e5baae64944b7c2c172a6bf67e3580c383c66f946` |

Go probeは別moduleでx/tools v0.52.0を使い、`packages.Config{Dir: fixtureDir, Mode: packages.LoadSyntax | packages.NeedModule}`（Tests=false）へdefaultまたはBuildFlags=`-tags=alt`を渡す。各SelectorExprをTypesInfo.Selectionsから引き、Recv/Obj/宣言位置を取得する。interfaceのUnderlyingを持つreceiverは具体runtime targetとして扱わない。Const.Valは7、Varのruntime値は推定しない。IllTypedでは候補採用を止める。

Swiftは上記compiler commandでstoreを生成し、libIndexStoreのstore_create → record_reader_create → record_reader_occurrences_apply_f → occurrence_get_symbol/roles/line_col → symbol_get_name/usrでSample.swiftのrecordを読む。今回のprobeはrecord名をsource basenameで列挙して絞る小実験であり、任意projectのquery実装ではない。一般化する実装ではunit/dependency metadataから対象recordを特定する。

</details>

- 2026-10-27: issueに適用される整合検査、移動後の相対リンク検査、diff検査が成功した。研究用probeと固定fixtureは/tmpのみで、本体Goコード/依存の変更はない。
