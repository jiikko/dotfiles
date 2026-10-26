# 930 (feat): dupscan の候補呼び出し数に省略引数・曖昧さの根拠を付ける

起票日: 2026-10-26

## 目的

grepで既存実装を見落とし、似たメソッドを新設する問題を減らす。候補を見つけても「呼び出し0だから使われていない」「呼び出しが多いから他moduleでも再利用できる」と誤判断すれば目的を達成できない。近似数の条件と取りこぼしを表示し、既存候補を正しく評価できるようにする。

## 裏取りと再現（9d2e801f）

使い捨てgit repoに次のSwiftを保存しgit addした上で、build済みCLIを実行する。

```swift
struct Worker { func run(_ x: Int = 1) {} }
func use(_ w: Worker) { w.run(); w.run(2) }
```

```sh
dupscan index -format json -name run
dupscan callers -format json run
dupscan callers -format json 'run(_:)'
```

実測: indexのcalls_approxは1、裸のcallers runは2件、ラベル指定のcallersは1件。`internal/report/index.go` のcallCountsは定義の全LabelsをCallQueryへ渡し、`internal/report/callers.go` のmatchesは通常callのラベル個数一致を要求する。省略した既定引数を照合していない。

これはREADMEに書かれた既知の近似仕様であり、「正確な呼び出し数を返す契約への違反」とは扱わない。同名の別型/overloadが混ざる過大評価もあるため、現在値は意味上の真の呼び出し数の下限でも上限でもない。JSONのapproxという名前だけでは、どの取りこぼしがあるかは分からない。

## 設計案と段階

1. [926](926-feat-dupscan-query-diagnostics.md) のjson-v2に、call countのresolution（syntactic）、照合した条件（name/labels）、scope、limitationsを持たせる。0でも未使用の証拠にならない旨をtextにも短く表示する。既存JSON/順位/数値は維持する。
2. Swift adapterの引数metadataへdefaultの有無を追加し、省略可能な引数と実際のlabelsを照合するpure functionを作る。複数の対応付けが成立するoverloadや同名の別型は、確定した呼び出しとして一本化しない。
3. v2に現行shape一致と追加のpossible candidateを分けて出す案を比較する。候補にはfile/line/colと `default_argument_omission` 等の理由を付ける。同じ出現が複数候補に当たった場合も曖昧さを保持する。possibleを単純加算した値を「正しい呼び出し数」と呼ばない。大きな一覧の予算は926へ従う。
4. Goのvariadic/spread・Swiftのtrailing closure等は対応済みと推測しない。能力/limitationsへ明記し、各言語のfixtureで対応を確認してから範囲を広げる。LSP/型解決の追加は範囲外。

[925](925-feat-dupscan-helper-search.md) の初期search順位は近似呼び出し数を使わない方針を維持する。本issueでindexの既存順位を黙って変えない。候補の実際の再利用可否・module/targetの所属は[918](../890/pending/918-feat-dupscan-target-and-literal-table.md)とコード確認の課題であり、数値で代用しない。

## 検証・受け入れ条件

- [ ] defaultを全省略/一部省略/省略なし、必須label不一致、同名別型、overload、複数対応付け、trailing closure、Go variadic/spreadを含む期待表を人がコードから作る。
- [ ] 初期metadataのみの段階でも、0件を未使用・再利用不可と断定する表示が無い。対応を広げた段階では上の省略callがpossible候補として見える。
- [ ] 意味上のexact countや上下界を保証しない。候補表示と解析不完全/非対応を区別し、旧modeの結果を保つ。
- [ ] [928](928-feat-dupscan-agent-onboarding.md) の実作業評価に、default引数を使う既存helperを見落とさず再利用する課題を含める。呼び出し数が大きくなっただけで成功としない。

## 進捗

- 2026-10-26: `issues: reserve dupscan coverage and call evidence follow-ups` で番号予約。CLIで現行の過小カウントを再現し、既知仕様と誤解の危険を区別した。改善は、出力の根拠・曖昧さ・検査の設計を記録したこと。実装・利用効果は未検証。

- 2026-10-26: 出力の正確性について読み取り専用Codex subagentで反証レビューし、P1/P2/P3の追加指摘なし。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。
