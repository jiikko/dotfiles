# 932 (epic): dupscan の精度を上げる — 既存実装の見落としと誤判断を減らす

起票日: 2026-10-26

既存helperの発見、誤った「不存在」判断の防止、候補の確認費用を改善する。意味上の同一性や別moduleからの利用可否は、構文一致だけでは証明しない。

## 現在の状態（2026-10-28）

子issue20件中15件完了、4件open、1件pending。今回対象だった925・927・934の局所機能と確認導線を完了し、その対象では928の実agent評価が未完了。別セッション追加の941〜944は下節の記録・状態を保持する。限定機能の完了と一般的なagent効率の実証を区別する。

| open | 残作業 |
|---|---|
| [928](928-feat-dupscan-agent-onboarding.md) | sourceを読む実agentの比較は明示許可待ち。ローカルwrapper/guide/config/非repoは追加確認済みだが、cold compiler cacheや全SDK/環境matrix、検索の実利用による再利用/重複追加/実contextの因果効果は未検証 |

## 使われやすさと context 消費（2026-10-28 追加）

「定義を探すときは grep でなく dupscan を使わせる」入口（案内 hook）の取りこぼしと、「dupscan は grep より context を食わない」前提が狭い検索では成り立たない点を、dotfiles の `src` で実測して起票した（名前3つ・repo 1つの測定で、一般の効果とは扱わない）。広い名前では桁違いに軽い（`Join`: rg 446,145 B / index 2,900 B）一方、定義1件の検索は rg 340 B / index 738 B。

| issue | 内容 |
|---|---|
| [941](941-ux-dupscan-nudge-definition-search-gaps.md) | hook が Go の定義検索の典型形（`func.*Name`・receiver付き・名前のalternation）で無音。言語の手掛かりがない検索でGoの名前にSwift用 `def` を勧める |
| [942](942-perf-dupscan-fixed-notice-overhead.md) | 毎回出る注意書き（root 実行で index 471 B / callers 427 B）が狭い検索の過半。0件・空の全文は保つ。hit 側の通知を弱めるなら926の24ケースを改定する |
| [943](943-perf-dupscan-result-row-shape.md) | 結果行は rg の1.6倍（173 B 対 111 B）で、docが44〜48%。index/callers の行に終端行が無く Read の範囲を決められない（search は範囲つき read_argv を既に出す）。打ち切り時の件数ヘッダと rg の提示も含む |
| [944](pending/944-feat-dupscan-go-def.md)（保留） | Go の `def`。943の後も全体 Read が多いときに再開 |
| [946](946-feat-dupscan-recall-from-real-modules.md) | 4 module に回して寄せた 21 件のうち、既定の report が出したのは 4 件。小さい単位を比べていない事実を report に出す（実装済み）。「1 語だけ違う写し」の段・一括で掘る入口は提案 |

## 完了した子issue

- [925](done/925-feat-dupscan-helper-search.md): text検索と明示回復。hard条件候補0で無益な語数緩和を省略。固定alias例で3検索/3867bytes→2検索/2568bytes。
- [927](done/927-feat-dupscan-agent-context.md): qualified照会の一覧省略時だけ型名evidenceを追加し、従来の取得actionを保持。C2の型構築/テストへ回復する局所導線を確認。
- [934](done/934-feat-dupscan-source-evidence-search.md): evidenceの先頭actionを原文確認にし、従来の追跡を2番目に残す。否定コメントやowner不明のGoテストの実sourceへ到達。

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

今回の追加は、無益な検索往復の削減、型名からの回復、採用前の原文確認という局所的な改善。934の否定例は必要な原文へ届く一方、合計出力量は増えた。これを一般的なtoken削減やagent誤採用率改善へ拡大しない。ソースを外部AIへ送る新実験は行っていない。実測・反証・変異の結果は各子issueへ記録した。

最終統合でもmodule lint/race全件とstrict module gateを通過（変わった単位6、新しい重複/除外なし）。全srcは既存の不正fixtureだけbase/head各1件でpartial。context→型名evidence→原文の実argv連鎖も同scope/別cwdで実行できたが、agentによる採用判断の評価とは扱わない。

最終のread-only反証レビューで追加指摘なし。今回対象の15done/928未完了と担当解除を確認し、issue標準テストもBash 5で成功した。

別セッションの941〜944・942/943の出力改善を保持してrebaseした。これら追加課題の完了判断や担当目印には介入せず、最新コードで統合検証をやり直す。

最新master取込み後もlint/raceとstrict gateは成功。後から追加された943の927参照をdoneへ張り直し、追加941〜944の内容・claimは保持した。
