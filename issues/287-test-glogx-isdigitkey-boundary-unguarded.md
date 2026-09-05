# test: `isDigitKey` の境界に検査が無く、1 文字ずれると `/` が検索語に入る

起票日: 2026-09-06
カテゴリ: test
優先度: 低（実害は「番号フィルタが空振りする」まで。変異は緑で確定）

## 何が起きているか

`issues_number_filter.go:isDigitKey` が、番号フィルタ入力の**唯一の関門**。
`issues_view.go:handleKey` は `v.numFilter.typing` の間**全キー**を `numberFilterKey` へ流し、
default が `edit(key)` を呼ぶ。

## 実測（変異検証）

`r[0] >= '0'` → `r[0] >= '/'` に変異（`go build` OK）→ **全スイート GREEN**。

境界が 1 文字ずれると `/`（= フィルタを開くキー自身）や `:` が検索語に入り、
`番号に「12/」を含む issue はありません` になる。

## 発火条件

- `isDigitKey` の境界を触る変更が入ったとき、検査が素通しする
- **silent に壊れる**

## 推奨対応

境界のテーブルテストを 1 本（`'/'` `'0'` `'9'` `':'` `'ｌ'` 全角数字）。

## 反証の試み

`issuesNumberFilter` の doc は「数字以外の印字文字は**無視**」と明記しており、
`/` を受けるのは意図に反する。`issues/` `docs/` に番号フィルタの仕様文書は無く、
テストファイルにも `isDigitKey` を名指しする記述は 0 件。

## 付随: 同じ判定の 2 実装（消さないこと）

`groupKeys` の `!f.active` ガードと `confirm()` の空クエリ→`clear()` は、
どちらも変異で**全スイート GREEN**（= 等価変異）:

- `groupKeys` の `!f.active` → 唯一の呼び出し元 `issues_view.go:refresh` が既に
  `if v.numFilter.active` で分岐している
- `confirm` の空クエリ分岐 → 唯一の到達経路 `numberFilterKey` の `case "enter"` が
  同じ判定を先に持っている

🚨 **「等価変異だから消せる」と読まないこと。** どちらも外すと残る 1 箇所が単一障害点になり、
**その 1 箇所を壊す変異を検出するテストは存在しない**。消すなら先に検出手段を用意すること
（`~/.claude/rules/list-masked-failure-modes-before-removing-guard.md`）。
ここでは「同じ判定が 2 箇所にある」という構造の記録に留める。
