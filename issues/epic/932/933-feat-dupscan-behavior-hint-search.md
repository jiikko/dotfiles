# 933 (feat): dupscanで呼出先・処理構造を手掛かりに既存helperを探す

起票日: 2026-10-26

## 目的と未カバーだった点

書こうとしている処理の入出力や使うAPIは分かるが、既存関数の名前は知らない場面で候補を探す。grepで既存実装を見落とし、似たメソッドを新設する問題を減らす。[925](925-feat-dupscan-helper-search.md)の初期searchはname/signature/doc先頭行のlexical検索であり、呼出先や本体構造を指定する機能は含まない。本issueはその追加材料を扱う。

## 現状の裏取り（2920a72a）

- `internal/report/index.go` のQuery.matchはname/returns/param-type/doc/fileを判定し、呼出先やループ本体の検索条件は無い。
- `internal/tree.Unit`はBodyを保持し、Go/SwiftのadapterにはCallsの収集経路がある。ただし、現在のGo callsはcalleeの末尾名を取り、receiver/import aliasの意味を解決しない。
- [931](../../done/931-refactor-tuikit-domain-duplicates-and-discovery-check.md)のspansWidth/lineWidthは、入力の取り方が違ってもtermwidth.OfでTextの幅を加算する既存処理だった。name Widthのindexで見つけ直せたのは発見後であり、未知名からの効率向上を示したものではない。

## 設計案（未実装）

1. 初期版は925の検索へ呼出先hintを追加する案を比較する（例 `search -returns int -calls termwidth.Of`。現行CLIには無い）。用途語なしでも有効な型/呼出先hintがあれば検索できる規則を925と揃え、名前を推測することを必須にしない。
2. 呼出先は「末尾名」「sourceに書かれたselector/receiver」のどこを照合したかを返す。GoのCallsへ必要な構文metadataを追加する場合も、旧callers/近似数の意味を保つ。`termwidth.Of`がimport先の特定symbolに解決されたとは表示しない。alias/動的call/未知のcalleeの非対応範囲を示す。
3. 次の段階で、range/加算/呼出し等の限定した本体featureを検索材料にする。自由な自然言語から任意の動作を理解する機能とはしない。構造の類似度は[895](895-feat-dupscan-type3-candidates.md)の担当で、同じfingerprintの二重実装を避ける。
4. [924](done/924-perf-dupscan-exploration-latency.md)のAnalysisPlan/Viewで必要なfeatureだけ抽出し、型・語・呼出先・構造の一致根拠を候補ごとに示す。不明なfeatureを一致扱いにしない。暗黙に条件を緩めない。
5. 候補ID・予算・省略・coverageは[926](926-feat-dupscan-query-diagnostics.md)へ従う。型/責務/利用可否は未証明としてコード確認へつなぐ。構文が大きく違う同目的の実装まで必ず見つかるとは保証しない。

## 検証・受け入れ条件

- [ ] nameを与えず、入出力/呼出先から既存helperへ到達する正解表を作る。doc欠落、alias、同名別receiver、動的call、同じ呼出先だが別責務を含める。
- [ ] 925のquery必須/OR候補規則と、hintのみの検索時の候補条件・順位・0件診断を同時に更新し、意図した条件が全て表示される。
- [ ] 旧index/callers/gateを維持し、supported/unsupportedとparse不完全を区別する。
- [ ] 検索1回の速度だけでなく、[928](928-feat-dupscan-agent-onboarding.md)で正しい再利用・不要な重複追加・候補確認までのコンテキスト消費を比較する。

## 進捗

- 2026-10-26: `issues: reserve dupscan behavior hints and source evidence search`で番号予約。既存925の対象fieldと不足する呼出先/本体hintを切り分け、設計と検証条件を記録した。実装・改善効果は未検証。

- 2026-10-26: 呼出先/本体hintの不足と925等との境界を確認。 読み取り専用Codex subagentで指摘の網羅性とcontext計測を反証レビューし、P3（現行/設計、着手条件、計測範囲）の3点を訂正した。文書の改善は、不足した指摘の担当と検証条件を明確にしたこと。実装・両効果の実証は未着手。

- 2026-10-27: 924で現行探索の比較用本体変換/正規化を省いた。933の処理構造hintは未実装で継続し、実装時には必要な構造metadataの収集を明示的に設計する。
