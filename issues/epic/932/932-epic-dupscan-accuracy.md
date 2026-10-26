# 932 (epic): dupscan の精度を上げる — 既存の実装へ正しく辿り着き、見落としと誤判断を減らす

起票日: 2026-10-26

> epic 932 の親 issue。[890](../890/done/890-epic-dupscan.md) は動くもの (索引・gate・Swift と Go の adapter) を作ることがゴールで、こちらはその精度を上げる (ユーザー指示 2026-10-26)。

## 目的

書く前に既存のヘルパーを探す・gate で写しを止める、の 2 つで、次を減らす。

- 見つかるはずの既存の実装を見落とす
- 「無い」と「読めなかった・問い合わせが合っていない」を取り違える
- 候補を見つけても使えるかを誤判断する
- 正しい再利用につながらない大量の検索結果/ソース閲覧でコンテキストを消費し、不要な重複メソッドを追加する

## 子 issue

| 番号 | 何をするか |
|---|---|
| [895](895-feat-dupscan-type3-candidates.md) | 小さい関数を含む構文の少し違う重複候補を出す |
| [899](899-research-dupscan-lsp-integration.md) | 型/シンボル解決の範囲と負担を非LSPの解析から調査する |
| [918](918-feat-dupscan-target-and-literal-table.md) | 所属/利用可否とリテラル差分の根拠を扱う |
| [924](done/924-perf-dupscan-exploration-latency.md) | 探索コマンド (`index` / `def` / `refs`) の待ち時間を減らす |
| [925](925-feat-dupscan-helper-search.md) | 用途語・署名から、名前の分からない既存ヘルパーの候補を探す |
| [926](926-feat-dupscan-query-diagnostics.md) | 問い合わせの形式と、検索 0 件のときの診断を揃える |
| [927](927-feat-dupscan-agent-context.md) | agent 向けに、定義・参照・呼び出しをまとめて返す |
| [928](928-feat-dupscan-agent-onboarding.md) | Claude / Codex 共通の導入案内と能力診断 |
| [929](done/929-bug-dupscan-gate-lost-file-parse-errors.md) | gate が、単位を抽出できない解析失敗を通知せずに成功する |
| [930](930-feat-dupscan-candidate-call-evidence.md) | 候補の呼び出し数に、省略引数・曖昧さの根拠を付ける |
| [933](933-feat-dupscan-behavior-hint-search.md) | 呼出先・処理構造から名前不明の既存helperを探す |
| [934](934-feat-dupscan-source-evidence-search.md) | doc全文・テスト使用例・元ソースを補助証拠として探す |
| [935](done/935-test-doctor-duplicate-discovery-effect.md) | doctorの重複検出・探索出力量をgrep/併用で比較する |
| [937](done/937-feat-dupscan-go-test-call-owner.md) | 比較から除くGoテストにも呼び出し元名を保持する |
| [938](938-ux-dupscan-agent-friendly.md) | agent のコンテキスト消費と発見性を良くするアイデア (既存 issue との関係つき) |

## 指摘の対応範囲

完全一致/サイズ閾値は895、型と所属の限界は899/918、探索の不要な解析は924、未知名の候補検索は925/933/934、解析漏れ/誤った不存在は926/929、呼出し数は930、必要なcontextの段階取得は927、コンテキスト消費と不要な重複追加の同時評価は928で追跡する。895/899/918は同番号の既存pendingを移し、重複起票を避けた。

## 状態

924の探索latency改善、929のgate解析漏れ通知/strict判定、935のdoctor限定測定、937のGo test呼び出し元表示を完了。927のcontextと934のevidenceは最小版を実装済みで、詳細設計の残りをopenで継続する。ほかの課題は各子issueで追跡する。

- 2026-10-26: 指摘の未カバー分を933/934へ切り出し、895/899/918を未着手のopen課題へ戻した。context消費と重複追加の削減は928で評価する。実装・効果は未検証。

- 2026-10-26: 子issue 12件のopen位置と担当範囲を照合し、相対リンク・diff・Bash 5の適用テストが成功。反証レビューの3件の表現を修正済み。今回の改善は、未カバーの検索材料と二つの目的の計測条件を追跡可能にしたこと。機能/効果の実証は未着手。

- 2026-10-26: 929で解析不能を重複なしと誤認させない土台を実装した。正常入力の互換性、両言語の回帰、変異5本を確認。探索の発見率/context効率の改善は他の子issueで継続。

- 2026-10-26: [935](done/935-test-doctor-duplicate-discovery-effect.md)でdoctorの検出・探索出力量を比較。併用で完全重複を検出し、探索転送量は平均48.7%少なかったが、grepは別の部分重複を発見。実context/新規重複追加率と機能改善は895/928ほかで継続。

- 2026-10-26: 936の共通化作業で確認したGo testのcall owner欠落を937へ切り出した。現行の比較除外仕様を維持する設計で、機能実装は未着手。

- 2026-10-27: 937でGoのtest ownerを比較単位から分離した。doctorの6出現を保ち、欠けていた4件のテスト名を表示。互換性5条件・4変異検出・lint/raceを確認。固定例ではソース補完読取が1→0回だが、一般的なcontext効率は未検証。

- 2026-10-27: 924で探索の比較用本体変換/正規化を省いた。固定大規模Swiftの中央値27〜46%短縮、outline追加測定でも約31%短縮。小規模の改善は未確定。rc/出力200条件、lint/race、5変異検出を確認し、cacheや意味解決は追加していない。
