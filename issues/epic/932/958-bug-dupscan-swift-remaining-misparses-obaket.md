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

- [ ] obaket の同じ HEAD で前後を比べ、読めないファイル・単位が減り、**読めない単位が増えたファイルが 0** (ファイルごとに比べる)。組の数の変化と理由を書く
- [ ] 規則ごとのテスト: その形が読める / 直した単位の L1 の形が、同じ意味を手で書いた読めるコードの L1 と一致する / `try!`・`try?`・名前としての `await`・宣言の `final`・`g(())` を書き換えない / 隣の正常な関数を ERROR に飲み込む置き換えを捨てる / 別の構文エラーを隠さない。954 のテストは緑のまま
- [ ] 規則ごとに、規則を外す変異でその規則のテストが赤
- [ ] `make -C src/dupscan test lint` と `make test-lint` が rc=0
- [ ] README・docs (`dupscan-detection.md`・`dupscan-overview.md`) の読み直しの説明を揃える

## 反証レビュー

本文の設計を codex (sol-high、読み取り) の敵対レビュー 1 本に通した (codex-drive の D3)。採用 4 件: D の `try!` を語だけ空白にすると否定 (`!`) に化け ERROR も出ない (P1) / 合計の ERROR の数だけの採用条件は規則の間の相殺を許す (P1) / A が既存の `f(())` の規則と重なり `g(())` の形が変わる (P2) / B は式の途中ではなく属性の分岐 (P2)。C・E・方式全体は反証できなかった。上の方針と受け入れ条件はこれを反映した後のもの。

## 関連

- [954](done/954-research-dupscan-dogfood-feedback-obaket-1307-2026-10-28.md) (`nonisolated(unsafe)` と `f(())` の回避)・[955](955-research-dupscan-dogfood-vlc-test-dedup-2026-10-28.md)・[932](932-epic-dupscan-accuracy.md) (epic)
- `src/dupscan/internal/lang/swift/swift.go` の `reparseKnownMisparses` / `isEmptyTupleArgument`、同じ dir の `isolation_test.go`

## 進捗

- 2026-10-29: 起票、D3 の反証レビューを反映。codex-drive で実装する
