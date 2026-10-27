# 932 (epic): dupscan の精度を上げる — 既存実装の見落としと誤判断を減らす

起票日: 2026-10-26

既存helperの発見、誤った「不存在」判断の防止、候補の確認費用を改善する。意味上の同一性や別moduleからの利用可否は、構文一致だけでは証明しない。

## 現在の状態（2026-10-28）

子issue16件中12件完了、4件open。今回895・918・926・930・933の限定実装を完了し、925・927・928・934は現行版の固定課題評価と残作業を更新した。条件付きの旧詳細設計は一括実装しない。

| open | 現状と次の確認 |
|---|---|
| [925](925-feat-dupscan-helper-search.md) | text searchは実装済み。固定正例へ到達する一方alias/ORの取りこぼしあり。実際の見落とし課題で検索を使った差を評価する |
| [927](927-feat-dupscan-agent-context.md) | context/summary/節選択は実装済み。3問い合わせを1回に集約できるが、同名appendの461件で目的が埋もれる。型名探索への回復を評価する |
| [928](928-feat-dupscan-agent-onboarding.md) | 能力案内/doctor/guideは実装済み。Codex実編集1対は両群とも既存ルールを再利用し、検索機能を使った効率差は未実証。外部への追加ソース送信は承認待ち |
| [934](934-feat-dupscan-source-evidence-search.md) | evidenceはdoc後半/source正例へ到達するが、否定コメントからの誤誘導も残る。実際の候補確認を評価する |

## 完了した子issue

- [895](done/895-feat-dupscan-type3-candidates.md): opt-inの対象指定 `near -similar`。幅計算とdoctorの歴史例を発見。全体group化/LSHは不採用。
- [918](done/918-feat-dupscan-target-and-literal-table.md): `explain -compare`で2対象の直接literal/未解決式を比較。initializer/manifest解決は不採用。
- [926](done/926-feat-dupscan-query-diagnostics.md): qualified refsの誤診断、後置flag/`--`、従来探索のparse error通知を修正。共通v2等は不採用。
- [930](done/930-feat-dupscan-candidate-call-evidence.md): index/callersへ照合条件と近似数の注意を表示。default matcher拡張は不採用。
- [933](done/933-feat-dupscan-behavior-hint-search.md): Goの `callers -selector` でsource構文をAND。search統合/本体featureは不採用。
- 既完了: [899](done/899-research-dupscan-lsp-integration.md) 非LSP補完調査、[924](done/924-perf-dupscan-exploration-latency.md) 探索latency、[929](done/929-bug-dupscan-gate-lost-file-parse-errors.md) gate解析漏れ、[935](done/935-test-doctor-duplicate-discovery-effect.md) doctor比較、[937](done/937-feat-dupscan-go-test-call-owner.md) Go test owner、[938](done/938-ux-dupscan-agent-friendly.md) 段階取得、[939](done/939-ux-dupscan-go-grep-nudge.md) Go案内。

## 今回の改善と残る限界

895は全体hashが異なる2歴史例へ到達し、918は既知6helperの照合出力を15,491→3,487 bytesにした。933はJoin全123callからstrings.Joinの48callへ絞れたが、適切なrgより小さくも、正解へ到達する順位が高くもなかった。類似候補は追加時間/RSSを使い、同形別責務も返す。単なる出力短縮や利用回数を、正しい再利用・token削減・重複追加率改善と扱わない。

各topicは専用worktreeで実装・反証レビュー・回帰/変異検証を行い、実測と採用しない理由を子issueへ記録した。共通CLIの構造化metadata、cursor、cache等は具体的な必要性が出た範囲で再検討する。今回の評価で一般的な開発効率が改善したとはまだ言えない。

統合検証: module lint/race全件成功。strict module gateは新重複/新規除外なし。全src gateは既存の意図的不正fixtureがbase/head各1件で解析不完全を通知した（完全な解析成功とは扱わない）。親の反証レビューはP1/P2/P3追加なし。done子末尾の曖昧な継続表現を928の効果評価へ限定した。issueテストは規約どおりBash 5をPATH先頭にして実行する。
