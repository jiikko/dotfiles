# 937 (feat): Goのテスト呼び出し元を重複比較単位と分けて保持する

起票日: 2026-10-26

## 問題と今回の実例

[936](../../done/936-refactor-doctor-domain-duplicate-methods.md)でdoctorを共通化した後、次を実行した（実装commit 449fc99d）。

```sh
bin/dupscan callers sortedKeys src/doctor
bin/dupscan callers -format json sortedKeys src/doctor
bin/dupscan index -file src/doctor/internal/displaycheck/displaycheck_test.go src/doctor
```

実測: sortedKeysは製品外の検査本体checkから2箇所、TestSortedKeysPreservesDisplayCheckOrderから4箇所呼ばれる。callersは計6出現を返し、後者4件はtextが「(単位の外) [test]」、JSONのcallerは省略される。1行の2出現もline/colの異なる別件であり、同一行が2回表示されること自体は誤検出ではない。index -fileは同じファイルのgoodSpec/problemsOf/wantOneProblemだけを返し、Test関数は載らない。結果のfile/line/colとソースを手で照合して、どのテストから呼ぶかを補った。

これは呼び出しの取りこぼしやGo generic推論の不具合ではなく、テスト本体を比較から除く現行設計の制約。テストがあることは見えるが、その所有者が分からず、再利用後にどのテストを読むべきかを絞りにくい。doctor全体のテストやcontext効率が改善したという実測はまだ無い。

## 現コードの根拠

- internal/lang/golang/golang.goのisTestFunc/funcDeclは、go testの入口とtestify相当のTestメソッドを判定し、比較用Unitsへ追加せず早期returnする。テスト内closureも比較から外す。
- Callの抽出は別に全ASTを読むので、呼び出し自体は残る。
- internal/report/callers.goのBuildCallers/enclosingはUnitsから囲む名前を探す。比較から除かれたTest関数と、本当にtop-levelの呼び出しが、どちらもcallerなしになる。CallerEntryのコメントもテストを単位の外に含めている。
- READMEの「テスト関数そのものと内部closureは比べない」は維持すべき契約。比較除外を単純に解除すると、テストの似たassertでreport/gateを埋めるおそれがある。

## 設計案

1. **比較対象と呼び出しの所有者を分ける。** Go parserは除外対象のテストにも名前・receiver・signature・file・開始/終了位置・test分類を保持する。既存の宣言metadataを再利用できるか確認し、使えなければcall owner用の軽い構造を追加する。hashの正規化やclone用Unitsへの追加はしない。
2. callersは名前のある最内側の所有者を位置範囲から探す。通常関数・Test/Benchmark/Fuzz/Example・testify形のメソッドを扱う。t.Run等の匿名closureの内側も、外側の名前つきテストへ帰属させる。名前を確定できないときだけcaller不明を維持する。
3. 同一行・複数出現を混ぜないよう、callとownerに必要なcolumn/offset範囲を保持する。別スコープの同名テストはfile/位置を含む識別子で区別する。構文エラーをまたぐ曖昧なownerを確定表示しない。
4. 初期対応はcallersの所有者表示に絞る。indexの既定結果やGo def/refsの対応範囲を黙って広げない。テストoutlineを返す拡張は、既存[927](927-feat-dupscan-agent-context.md)のcontext取得や[934](934-feat-dupscan-source-evidence-search.md)のテスト証拠探索からこのmetadataを利用する段階で検討する。
5. textでは名前つきテスト内をそのsignatureで表示する。旧JSONのcallerが空から埋まる変更をREADME/fixtureに明記する。新しいkind/reason等のschemaが必要なら[926](926-feat-dupscan-query-diagnostics.md)の版管理に従う。report/gateの比較対象と既存の違反判定は変えない。

## 既存issueとの分担

- [930](930-feat-dupscan-candidate-call-evidence.md)は引数省略・variadic/曖昧さによる照合/数値の根拠。本件は取得済みcallの囲むテスト名を扱う。
- [895](895-feat-dupscan-type3-candidates.md)は部分重複の候補抽出。936の最長prefixの検出漏れは895に既知例として記録済みで、本件では実装しない。
- 927/934は候補に渡すcontext/補助証拠の取得。本件はその前提となるGo test ownerのmetadataとcallers表示。

## 検証・受け入れ条件

- [ ] fixtureを手で分類し、Test/Benchmark/Fuzz/Example、receiver付きTest、通常helper、top-level、t.Run内、同一行の複数callのownerを期待表にする。
- [ ] doctorの上記6出現が維持され、4件のTest呼び出しが正しいテスト名を持つ。ownerを表示しても意味上の型/overload解決済みとは扱わない。
- [ ] parse errorのあるowner、名前不明、同名別scopeを誤って確定表示しない。既存の解析不完全通知を保つ。
- [ ] report/gateのUnits数・group・新規違反が変更前と一致。テスト本体/内部closureを重複比較へ混入させる変異を検出する回帰テストを置く。
- [ ] 同じhelperの関連テストを特定する課題で、必要な追加ソース読取回数・転送bytes・誤ったowner採用を比較する。token/window削減は別途計測し、表示が増えただけで効率改善と判定しない。

## 進捗・改善評価

- 2026-10-26: `issues: reserve Go test caller ownership improvement`で番号予約。936の実装後に実CLIとGo adapter/reportを照合した。現在は設計のみで未実装。今回の改善は、実作業で必要だった手作業の補完箇所を再現可能な課題へ分離したこと。テスト関数の比較除外方針は継続し、効率改善は未検証。

- 2026-10-26: 読み取り専用Codex subagentで反証レビューし、6出現/4件のowner欠落、現行仕様、既存課題との分担を再照合した。追加指摘なし。`docs(doctor): complete consolidation and specify dupscan test owners`で設計本文を記録する。実装は未着手。
