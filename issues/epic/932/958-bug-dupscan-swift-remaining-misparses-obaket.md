# 958 (bug): dupscan の Swift で、obaket の残り 26 ファイルが読めない原因 5 種を読めるようにする

> 🚨 **担当中: c6 (dupscan)**（2026-10-29〜）

起票日: 2026-10-29

## 概要

954 で `nonisolated(unsafe)` と `f(())` を読み直しで回避した後も、obaket (`~/src/my-products/apps/obaket`、HEAD 4815fbdb8) の **26 ファイル・72 単位**が「読めない構文」のまま残る。tree-sitter-swift (82bb3a5。upstream の main でも同じ) の読み違えで、同じ仕組み (`internal/lang/swift` の `reparseKnownMisparses`: 読み違えの箇所だけを同じバイト長の字面に置き換えて読み直す) で回避できるものを足す。

## 原因 (2026-10-29、Go の実装で ERROR と欠けた節を全部出して分類。50 箇所)

| 種類 | ファイル | 例 | 方針 |
|---|---|---|---|
| A. 空のタプル `()` が呼び出しの唯一の引数以外にある (欠けた `!`) | 13 | `resume(returning: ())`・`(value: (), bytes: 0)`・`SnapshotMulticaster(initial: (), …)`・`await x == ()`・`super.init(toMemory: ())` | `()` を `[]` に (同じ 2 バイト)。既存の `f(())` の規則を優先する |
| D. 条件の中の `await` / `try` | 9 | `switch await task.value {`・`if await …`・`for … where await …`・`switch try #require(…)`・`if let gate = await …` | 素の `await` / `try` (後ろに `!` / `?` が無い) の演算子のキーワードを空白に |
| E. クロージャの中の `;` 区切り | 2 | `{ Issue.record("…") ; return client }` | 改行を足さずに `;` を左に寄せる形を試す。読めなければ対象外 |
| B. 関数の属性の `#if` / `#else` / `#endif` | 1 | `#if swift(>=6.4) @Test(…) #else @Test(…) #endif func f()` | **対象外**。区切りを消すと排他の 2 属性が同じ宣言に付き、読めても正しくない |
| C. 変数名 `final` | 1 | `final = resolution` (後ろの ERROR 5 箇所は同じ関数からの連鎖) | 代入の側の `final` だけを同じ長さの別名に。宣言の修飾子 (`final class`) は触らない |

- D は影響が大きい: `TransferTracking.swift` は 187 行目の `if await …` 1 つで構文木の根が ERROR になり、ファイル全体 (27 単位) が読めない。954 で書いた「読めないのはその関数だけ」はこの形には当てはまらない

## 対応方針

- 規則ごとに「何を・どの節の条件 (ERROR / 欠けた節の位置と親子の種類) で・何に置き換えるか」を決める。字面の検索の範囲を広げない。置き換えは ASCII の字句だけで、バイト長と改行の位置を変えない
- **採用条件を強める**: 今は ERROR (と欠けた節) の合計が減れば新しい木を採る。規則が増えると、ある箇所の修復で別の箇所の悪化が相殺される。**元の木で ERROR を含まなかったトップレベルの子は、新しい木でも同じバイト範囲・同じ kind・ERROR なしで残る**ことも採用の条件にする
- 構造投資は最小 (既存の仕組みに規則を足す)。規則が増えて読みにくくなるなら、規則ごとの小さな関数に分ける

## 受け入れ条件

- [x] obaket の同じ HEAD で前後を比べ、読めないファイル・単位が減り、**読めない単位が増えたファイルが 0** (ファイルごとに比べる)。組の数の変化と理由を書く
- [x] 規則ごとのテスト: その形が読める / 直した単位の L1 の形が、同じ意味を手で書いた読めるコードの L1 と一致する / `try!`・`try?`・名前としての `await`・宣言の `final`・`g(())` を書き換えない / 隣の正常な関数を ERROR に飲み込む置き換えを捨てる / 別の構文エラーを隠さない。954 のテストは緑のまま
- [x] 規則ごとに、規則を外す変異でその規則のテストが赤
- [x] `make -C src/dupscan test lint` が rc=0。`make test-lint` は別のセッションの `tests/tmux/test_tmux.sh` (`test-pipefail-grep-q`) だけが赤 (この差分とは無関係)
- [x] README・docs (`dupscan-detection.md`・`dupscan-overview.md`) の読み直しの説明を揃える

## 反証レビュー

本文の設計を codex (sol-high、読み取り) の敵対レビュー 1 本に通した (codex-drive の D3)。採用 4 件: D の `try!` を語だけ空白にすると否定 (`!`) に化け ERROR も出ない (P1) / 合計の ERROR の数だけの採用条件は規則の間の相殺を許す (P1) / A が既存の `f(())` の規則と重なり `g(())` の形が変わる (P2) / B は式の途中ではなく属性の分岐 (P2)。C・E・方式全体は反証できなかった。上の方針と受け入れ条件はこれを反映した後のもの。

## 関連

- [954](done/954-research-dupscan-dogfood-feedback-obaket-1307-2026-10-28.md) (`nonisolated(unsafe)` と `f(())` の回避)・[955](955-research-dupscan-dogfood-vlc-test-dedup-2026-10-28.md)・[932](932-epic-dupscan-accuracy.md) (epic)
- `src/dupscan/internal/lang/swift/swift.go` の `reparseKnownMisparses` / `isEmptyTupleArgument`、同じ dir の `isolation_test.go`

## 進捗

- 2026-10-29: 起票、D3 の反証レビューを反映。codex-drive で実装する
- 2026-10-29 (マイルストーン 1): codex (astra-mid) が A・C・D・E の規則と採用条件 I5 (`preservesTopLevel`) とテストを実装した。codex は obaket の計測の途中で利用枠の上限で止まり (編集は全部残っていた)、計測・変異・敵対レビューは Claude と読み取り専用のサブエージェント (opus) で続けた
  - **obaket (HEAD 4815fbdb8、前後を同じ時点で取得)**: 読めないファイル **26 → 6**、読めない単位 **72 → 10**、**読めない単位が増えたファイル 0**、組 149 → 150 (増えた 1 組は `MoveReconcileExecutor` の、パスの降順に並べる同じクロージャ 2 つ。読めるようになって見えた正当な写し)
  - **変異**: 規則 A・C・D・E をそれぞれ外すと、その規則のテストが赤。I5 を外す変異は最初は緑のままだった (I5 を通るテストが無い)。obaket でも I5 で捨てた置き換えは 0 件。判定の関数を直接検査するテスト (`TestPreservesTopLevelRejectsChangedNeighbor`) を足し、判定を常に通す変異で赤を確かめた
  - **敵対レビュー 1 周目 (サブエージェント 2 体。codex の枠が切れていたため)**: 採用 2 件 (どちらも Go のテストで再現してから直した): ①D が、本体にだけエラーがある文 (`if let v = try await load() { g(()) }`・`if try check() { g(value: ()) }`・`for x in xs where try check(x) { g(()) }`) で、正しく読めている条件の `try` / `await` まで消していた → 条件の側にエラーがあるときだけ発火 (`conditionHasError`) ②既存の `f(())` の規則が字面で照合していて、空白だけ違う `f( ())` が別の規則 (`[]`) に落ちて別の形になっていた → 節の形 (`isSoleEmptyTupleArgument`) で判定し、内側の `()` を空白にする。どちらも、直す前の形に戻す変異で新しいテストが赤
  - 却下・記録: 「条件の後ろの `try save()` / `await flush()` まで消す」は再現せず (その組み合わせでは関数が読めないまま残る。誤った形にはならない)。「型の中では I5 が効かない」は示された入力 (`[() -> Void]()`) が元から読めて規則が発火しないので、**未確認リスク**。「新しい木の根が ERROR でも採る」はコードの事実だが、根が ERROR になる入力を小さく作れなかったので、防御は足さず**未確認リスク** (obaket の `TransferTracking.swift` の生の木は根が ERROR だった)
  - **敵対レビュー 2 周目 (直した差分。サブエージェント 1 体)**: 採用 2 件 (Go のテストで再現してから直した): ①D の発火の条件「条件の側の子に ERROR があるか」は、条件の中に別の読み違え (`if let v = try await send(()) {`) があるだけで真になり、try / await を消していた → D の読み違えの印 (本体の `{` が文の直下に無い・欠けている、または条件の式の末尾が呼び出しの末尾クロージャ) で判定する (`conditionHasError` / `endsWithClosure`)。括弧の中の引数のクロージャ (`contains(where: { … })`) は数えない ②引数の中のコメントを、唯一の引数かの数に入れていた (`c.yield(\n () // done\n)` が `[]` の規則に落ちる) → コメントを数えない (`namedNonComment`)。どれも、直す前の形に戻す変異で新しいテストが赤。記録のみ: `f(( ))` (内側の空白) は直らない (直す前から)
  - **敵対レビュー 3 周目 (上限。サブエージェント 1 体)**: 採用 1 件: 条件の文字列の補間の中の末尾クロージャ (`if try s == "\(xs.map { $0 })" { g(()) }`) で D が発火し、正しい try を消していた (Go のテストで再現) → `endsWithClosure` が文字列・補間・括弧・配列の中へ降りない。補間の入力のテストを足し、降りる版に戻す変異で赤。再現しなかった: 括弧の中の末尾クロージャ (`try await (store.first { $0.ok })`) は同じ形のまま。記録のみ: `while await …` / `guard` の条件は D の対象外 (直す前から)・`x[()]` が `x[]` と同じ形になる (未確認)。上限に達したので 4 周目は回していない
  - **残り 6 ファイル**: B (属性の `#if`、対象外) 1 / D の形なのに直らない 4 (`if await x { return … }` の 1 行の形・`switch await cleanup.value {`・`if await … >= count { return true }`・`for … where await …`。規則が発火しないか、読み直しても ERROR が減らない) / C の連鎖が残る 1 (`MoveReconcileExecutor` の `switch` の `case`)。次のマイルストーンで codex に原因を調べさせる
  - 検証: `make -C src/dupscan test lint` rc=0。`make test-lint` は、別のセッションの commit (`tests/tmux/test_tmux.sh`) の `test-pipefail-grep-q` だけが赤 (この差分とは無関係。この検証はそれを含まない)
- 未確認リスク (発火する入力を示せなかった。防御は足していない): 新しい木の根が ERROR でも採る / 型の中では I5 が効かない
- 次 (マイルストーン 2。codex の枠が戻ってから): 残る 5 ファイル (D の形 4・C の連鎖 1) の原因を調べて直す。B (属性の `#if`) は対象外
