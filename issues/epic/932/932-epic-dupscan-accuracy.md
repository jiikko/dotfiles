# 932 (epic): dupscan の精度を上げる — 既存の実装へ正しく辿り着き、見落としと誤判断を減らす

> 🚨 **担当中: air5 (glogx)**（2026-10-27〜）

起票日: 2026-10-26

> epic 932 の親 issue。[890](../890/done/890-epic-dupscan.md) は動くもの (索引・gate・Swift と Go の adapter) を作ることがゴールで、こちらはその精度を上げる (ユーザー指示 2026-10-26)。

## 目的

書く前に既存のヘルパーを探す・gate で写しを止める、の 2 つで、次を減らす。

- 見つかるはずの既存の実装を見落とす
- 「無い」と「読めなかった・問い合わせが合っていない」を取り違える
- 候補を見つけても使えるかを誤判断する
- 正しい再利用につながらない大量の検索結果/ソース閲覧でコンテキストを消費し、不要な重複メソッドを追加する

## 次の作業の優先順位（2026-10-27再評価）

open子issue 9件をa5b4196eの実装と照合した。全件openを維持するが、残る詳細設計の全項目を実装必須とはしない。各再評価節は旧設計・受け入れ条件・残作業より優先する。条件付き機能は、現行版の評価で不足がなければ理由を記録して不採用にできる。ただし評価や具体的な誤診断修正の未完了は残し、未採用案があるだけで完了扱いにも実装必須扱いにもしない。

| 優先する作業 | 子issue | 判断 |
|---|---|---|
| 再現する誤診断の小修正 | 926 | qualified refs・後置flag・従来探索のparse error通知を先行 |
| 近似数の説明 | 930 | contextの既存注意を踏まえ、index/callersを改善。matcher拡張は別段階 |
| 現行機能の品質/導入効果を評価 | 925 / 927 / 934 / 928 | search/context/evidence/能力案内は実装済み。正解・誤誘導・確認量を比較して追加機能を選ぶ |
| 具体的な不足が確認された範囲だけ試作 | 933 / 895 / 918 | selector filter、対象限定near、所属/literalを小さく分ける。全repo類似検索・LSH・3 build system等を先行しない |

改善は「存在する機能を未実装として再開する」「検証前に大設計を全実装する」という計画上の無駄を減らしたこと。今回は文書監査で、開発効率・context消費・重複追加率の改善は未実証。根拠と着手条件は各子issueの再評価節へ記録した。

## 子 issue

| 番号 | 何をするか |
|---|---|
| [895](done/895-feat-dupscan-type3-candidates.md) | 小さい関数を含む構文の少し違う重複候補を出す |
| [899](done/899-research-dupscan-lsp-integration.md) | 型/シンボル解決の範囲と負担を非LSPの解析から調査する |
| [918](done/918-feat-dupscan-target-and-literal-table.md) | 所属/利用可否とリテラル差分の根拠を扱う |
| [924](done/924-perf-dupscan-exploration-latency.md) | 探索コマンド (`index` / `def` / `refs`) の待ち時間を減らす |
| [925](925-feat-dupscan-helper-search.md) | 用途語・署名から、名前の分からない既存ヘルパーの候補を探す |
| [926](done/926-feat-dupscan-query-diagnostics.md) | 問い合わせの形式と、検索 0 件のときの診断を揃える |
| [927](927-feat-dupscan-agent-context.md) | agent 向けに、定義・参照・呼び出しをまとめて返す |
| [928](928-feat-dupscan-agent-onboarding.md) | Claude / Codex 共通の導入案内と能力診断 |
| [929](done/929-bug-dupscan-gate-lost-file-parse-errors.md) | gate が、単位を抽出できない解析失敗を通知せずに成功する |
| [930](done/930-feat-dupscan-candidate-call-evidence.md) | 候補の呼び出し数に、省略引数・曖昧さの根拠を付ける |
| [933](933-feat-dupscan-behavior-hint-search.md) | 呼出先・処理構造から名前不明の既存helperを探す |
| [934](934-feat-dupscan-source-evidence-search.md) | doc全文・テスト使用例・元ソースを補助証拠として探す |
| [935](done/935-test-doctor-duplicate-discovery-effect.md) | doctorの重複検出・探索出力量をgrep/併用で比較する |
| [937](done/937-feat-dupscan-go-test-call-owner.md) | 比較から除くGoテストにも呼び出し元名を保持する |
| [938](done/938-ux-dupscan-agent-friendly.md) | agent のコンテキスト消費と発見性を良くするアイデア (既存 issue との関係つき) |
| [939](done/939-ux-dupscan-go-grep-nudge.md) | Go指定grepをGoで対応する探索コマンドへ案内する |

## 指摘の対応範囲

完全一致/サイズ閾値は895、型と所属の限界は899/918、探索の不要な解析は924、未知名の候補検索は925/933/934、解析漏れ/誤った不存在は926/929、呼出し数は930、必要なcontextの段階取得は927、コンテキスト消費と不要な重複追加の同時評価は928で追跡する。895/899/918は同番号の既存pendingを移し、重複起票を避けた。

## 状態

899の非LSP補完調査、924の探索latency改善、929のgate解析漏れ通知/strict判定、935のdoctor限定測定、937のGo test呼び出し元表示と939のGo grep案内を完了。925のsearch、927のcontext、934のevidence、928の能力案内は最小版を実装済み。残設計は再評価節の条件で選び、openで評価を継続する。ほかの課題は各子issueで追跡する。

- 2026-10-26: 指摘の未カバー分を933/934へ切り出し、895/899/918を未着手のopen課題へ戻した。context消費と重複追加の削減は928で評価する。実装・効果は未検証。

- 2026-10-26: 子issue 12件のopen位置と担当範囲を照合し、相対リンク・diff・Bash 5の適用テストが成功。反証レビューの3件の表現を修正済み。今回の改善は、未カバーの検索材料と二つの目的の計測条件を追跡可能にしたこと。機能/効果の実証は未着手。

- 2026-10-26: 929で解析不能を重複なしと誤認させない土台を実装した。正常入力の互換性、両言語の回帰、変異5本を確認。探索の発見率/context効率の改善は他の子issueで継続。

- 2026-10-26: [935](done/935-test-doctor-duplicate-discovery-effect.md)でdoctorの検出・探索出力量を比較。併用で完全重複を検出し、探索転送量は平均48.7%少なかったが、grepは別の部分重複を発見。実context/新規重複追加率と機能改善は895/928ほかで継続。

- 2026-10-26: 936の共通化作業で確認したGo testのcall owner欠落を937へ切り出した。現行の比較除外仕様を維持する設計で、機能実装は未着手。

- 2026-10-27: 937でGoのtest ownerを比較単位から分離した。doctorの6出現を保ち、欠けていた4件のテスト名を表示。互換性5条件・4変異検出・lint/raceを確認。固定例ではソース補完読取が1→0回だが、一般的なcontext効率は未検証。

- 2026-10-27: 924で探索の比較用本体変換/正規化を省いた。固定大規模Swiftの中央値27〜46%短縮、outline追加測定でも約31%短縮。小規模の改善は未確定。rc/出力200条件、lint/race、5変異検出を確認し、cacheや意味解決は追加していない。

- 2026-10-27: 899でGo/Swiftの補完範囲・所要・前提を実験と一次資料で比較した。通常構文経路とLSP不採用を維持し、typed/indexedの将来候補とunknown契約を決定。899は調査完了で、本体統合や918/926/930の機能実装は継続する。

- 2026-10-27: 938案1を939へ独立させ、Goを明示したgrepの案内を実装した。938の残りの案、928のregistryと効果検証は継続。

- 2026-10-27: 938案2〜4をcontext/evidenceへ実装し、案5は任意summaryを採用、既定短縮は見送って決着。局所の出力量比較と制約を938へ記録。926/927/928の残機能と開発全体の効果検証は継続。

- 2026-10-27: 928の先頭 `-no-build` 起動を実装。未build/stale/指紋取得不能ではbuildも旧版execもしない。registry/capabilities/doctor/guideと効果評価は継続し、928はopenのまま。

- 2026-10-27後半: 928のregistry/capabilities/doctor/agent-guideとGo通知のno-build接続を実装。旧出力16条件の互換、module lint/race、hook89条件と各変異を検証。Codex実CLIは成功、Claude側のBash許可と両agentのA/B等は残し928はopen。

- 2026-10-27: 925の名前/署名/doc先頭行を横断するtext searchを実装。型だけの索引の23番目だった固定helperへ1候補で到達したが、ORの無関係候補も記録。json-v2と実課題A/Bは残し925はopen。

- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`でopen9件を再評価。誤診断/注意表示と効果評価を優先し、大規模拡張は具体的な不足を着手条件とした。

- 2026-10-27: `docs(dupscan): correct evidence terminology and conditional acceptance`で3観点のread-only subagent反証レビューを反映。P2 1件（条件付き設計と旧完了条件の矛盾）とP3 4件（Called/Calls、節/定義選択、条件付き受け入れ、labelなし照会）を訂正。監査を終え親のclaimを解除、全open子issueの位置は維持。

- 2026-10-28: 895で対象指定 `near -similar` の限定試作を完了。全体hashの異なる2歴史例を発見できたが、型/責務/利用可否は未証明。本issueの役割は継続する。
