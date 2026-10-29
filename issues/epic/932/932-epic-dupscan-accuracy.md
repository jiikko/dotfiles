# 932 (epic): dupscan の精度を上げる — 既存実装の見落としと誤判断を減らす

起票日: 2026-10-26

既存helperの発見、誤った「不存在」判断の防止、候補の確認費用を改善する。意味上の同一性や別moduleからの利用可否は、構文一致だけでは証明しない。

## 現在の状態（2026-10-29）

子issue44件中35件完了、8件open、1件pending。928は共通能力診断/表示専用案内を限定採用し、環境検証を補完して完了。一般的な効率改善と未実施比較は948で継続する。他担当の941〜944/946/948の内容・claimは保持する。

## 使われやすさと context 消費（2026-10-28 追加）

「定義を探すときは grep でなく dupscan を使わせる」入口（案内 hook）の取りこぼしと、「dupscan は grep より context を食わない」前提が狭い検索では成り立たない点を、dotfiles の `src` で実測して起票した（名前3つ・repo 1つの測定で、一般の効果とは扱わない）。広い名前では桁違いに軽い（`Join`: rg 446,145 B / index 2,900 B）一方、定義1件の検索は rg 340 B / index 738 B。

| issue | 内容 |
|---|---|
| [941](941-ux-dupscan-nudge-definition-search-gaps.md) | hook が Go の定義検索の典型形（`func.*Name`・receiver付き・名前のalternation）で無音。言語の手掛かりがない検索でGoの名前にSwift用 `def` を勧める |
| [942](done/942-perf-dupscan-fixed-notice-overhead.md) | 毎回出る注意書き（root 実行で index 471 B / callers 427 B）が狭い検索の過半。0件・空の全文は保つ。hit 側の通知を弱めるなら926の24ケースを改定する |
| [943](943-perf-dupscan-result-row-shape.md) | 結果行は rg の1.6倍（173 B 対 111 B）で、docが44〜48%。index/callers の行に終端行が無く Read の範囲を決められない（search は範囲つき read_argv を既に出す）。打ち切り時の件数ヘッダと rg の提示も含む |
| [944](pending/944-feat-dupscan-go-def.md)（保留） | Go の `def`。943の後も全体 Read が多いときに再開 |
| [946](done/946-feat-dupscan-recall-from-real-modules.md) | 4 module に回して寄せた 21 件のうち、既定の report が出したのは 4 件。小さい単位を比べていない事実を report に出す・メンバー名 1 語だけが違う写しの段 `L1m`（opt-in。Swift で組が倍増するので既定にしない）を実装済み。一括で掘る入口・名前の束は提案。Swift 固有の所見（既定 213 組の 98% がテスト・読めない構文 3.4%）もここ |
| [948](948-research-dupscan-meets-its-aims.md) | 一般的な重複防止/探索/消費量の効果は未実証。928から未実施の比較研究を引継ぎ、候補注入A1等の測定を継続 |
| [955](done/955-research-dupscan-dogfood-vlc-test-dedup-2026-10-28.md) | vlc のテストの重複の解消で使った所見。関数の中の `nonisolated(unsafe) let` 1 行でファイル全体が「読めない構文」になる (954 の P3 の答えの 1 つ。6 行で再現)・sweep の節 2 は 20 組中 1 か所だけが寄せる価値・節 3 の `w` の意味・後置 flag のエラー。done: F1 は 954・958 で解消、F2 は sweep の節 6 (型の写し)、F3・F4 は直した |
| [965](done/965-research-dupscan-dogfood-treefiler-dedup-2026-10-29.md) | src/treefiler の重複の統合で使った所見。sweep の節 5 は Test 関数の中を見ない (仕様だが出力に出ない)・同じ単位の 2 つ目の allow-group が黙って効かない・単位の allow は組の全員に要る・Go の `callers New` が `errors.New` を拾う |
| [966](done/966-ux-dupscan-sweep-silent-allow-marks-2026-10-29.md) | src/tuikit の sweep の 3 組を閉じたときの所見。sweep が印で消した組の数と、人数の合わない allow-group を出すようにした (起点を絞ったときは L1 で多い印だけ)。反証レビューの P2 2 件 (起点を絞ると印が粗い段の別の組に効く・levels を省いたパスの除外が L1m に効かない) を記録 |
| [970](done/970-refactor-dupscan-group-containment-one-check.md) | 「組が別の組に含まれるか」の判定が 3 か所 (組分け・sweep の節 2・節 6) にあったのを group.Subset に寄せた。done |
| [972](done/972-research-dupscan-dogfood-glogx-tui-split-2026-10-29.md) | glogx の tui.go の分割 (971) で使った所見。雑音 3 件: Go の index -file に Swift の注意書き / -file でも無関係な testdata の「解析不完全」 / tmp/ の使い捨ての道具にも候補の注入 |

## 完了した子issue

- [959](done/959-research-dupscan-dogfood-feedback-obaket-1307-part3-2026-10-29.md): obaket 1307 の所見。gate の違反の寄せる側に出どころ (`origins`)、解析が不完全でも 1 行目で「新しい重複 0」。寄せ先を縮んだ組なら通す提案は採らない (L1 の写しは実際に増える)。
- [963](done/963-research-dupscan-feedback-obaket-1309-2026-10-29.md): obaket 1309 の所見。組のメンバーに終端行、`-v` に除外の理由を書く前のメンバーの違いの確かめ方 (L1 の注記・`explain -compare` の argv)。除外を書く CLI と target の取り込みは見送り (再開の条件つき)。
- [928](done/928-feat-dupscan-agent-onboarding.md): 共通診断/表示専用案内/no-buildを維持し、探索は候補確認に限定採用。CLI不在・SDK/未対応言語・cold compiler buildを追加検証。本番比較とClaude/Swift未検証は948へ理由/再開条件付きで引継ぎ。

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

2026-10-28の承認後pilotは928に記録。固定binary 5c89f75bの結果を、最新masterのsweep/L1m/打切りstderr通知等へ事後差替えしない。別セッション追加の946/948と既存claimは保持し、948の「未承認/探索+Read未測定」を今回の限定観測へ更新した。一般的な効率改善は未実証のまま。

928をrefreshし、残作業を環境検証・未実施のagent組合せ/比較計画の決着・用途別の採否判断へ整理した。既存pilotは完了、本番12課題は未実施。今回は研究の縮小/移管やdone移動を決めていない。

- `docs(dupscan): close onboarding with bounded adoption and explicit research handoff`で928を完了。最新環境検証・限定採用と、948で継続する未実施研究を区別し、実ファイルから22子=16done/5open/1pendingを再計数した。

- [952](done/952-bug-dupscan-swift-escaped-identifier-matching.md): 有効なSwiftのバッククォート付き識別子で定義/参照/呼出とindex近似数が分断する未修正の不具合を再現し、記録した。最新sourceでも確認済み。

- [957](done/957-research-dupscan-dogfood-feedback-obaket-1307-part2-2026-10-29.md): 所見を現行版で裏どりし、既存compare/JSON集計の導線とstale groupの一意な更新候補表示を実装。Q3の書込みCLIは編集が律速になる実測を再開条件とし、Q4/Q6の専用filterは全体JSON集計でも律速になる実例まで保留。preset拡張・自動分類・stale一括削除は不採用。P3/P8は既存修正を実ファイル/実hookで確認。

- Obaket1301の中間評価を948/917へ追記。後続提案: [gate終了結果の記録](976-feat-dupscan-gate-record-final-outcome.md)、[CI導入費用の診断](977-research-dupscan-ci-install-and-gate-cost.md)、[resolvedの人手検証支援](978-research-dupscan-resolved-review-evidence.md)。既存の重複検出・探索・一般効果を混同せず、提案の実装は未着手。
