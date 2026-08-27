# 118 chore: golangci の指摘が既定 3 件で打ち切られ、一括退行の修正に往復が要る

起票日: 2026-08-27 / 出典: lint-from-done 監査 / priority: low

## 事実

`src/glogx/.golangci.yml` の `issues:` セクションは `uniq-by-line: false` を
**「せっかくの強制が別 linter の陰で false green になるのを防ぐ」**という理由つきで設定している。
一方 `max-same-issues` は未設定で、golangci の既定 **3** が効いている。

**実測 (監査が変異検証)**: `issues/done/038-refactor-glogx-push-anim-use-timenow-seam.md` の退行を
再現するため `tui.go` の `timeNow()` 呼び出し **20 箇所**を `time.Now()` に戻して forbidigo を回すと:

| config | 報告件数 |
|---|---|
| 現行 (既定 3) | **3 件** |
| `max-same-issues: 0` | **20 件** |

## ⚠️ これは false green ではない (実験で確認済み)

除外ルールは**上限の前**に適用される。「forbidigo 除外の `_test.go` に 5 件 + `tui.go` に本物 1 件」
を作った probe では、本物 1 件が正しく報告された。**`make lint` は赤のまま落ちる。**

実害は「20 箇所直すのに lint を 7 往復させられる」= **報告の不完全さ**であって、退行の素通りではない。
だから priority は low。

## 対応

`issues:` に `max-same-issues: 0` を 1 行 (必要なら `max-issues-per-linter: 0` も)。
`uniq-by-line: false` の隣に、同じ理由づけのコメントを添える。

---

## 併せて: `padSpaces` への置換残りが 2 件 (lint ルール化は見送り)

`issues/done/047-perf-glogx-frame-alloc-amplification.md` が
`strings.Repeat(" ", n)` → `padSpaces(n)` (無確保ヘルパ) の置換を 3 箇所で行ったが、
**package main の production に 2 件残っている**:

- `src/glogx/render.go` (SGR 復元の pad)
- `src/glogx/tui.go` (dim 描画の左詰め)

**ruleguard ルール化は見送った**。理由: repo 全体の `strings.Repeat(" "` は 12 件で、
うち真の対象は上記 2 件のみ。残り 10 件は `padSpaces` の実装本体 (1) / テスト (2、うち 1 件は
`padSpaces ≡ strings.Repeat` の同値テスト) / `padSpaces` を参照できない別パッケージ (6)。
**例外設定 4 エントリを足して 2 件を守る**費用対効果が見合わない。
なお ruleguard で書けば元の 3 行を実際に flag できることは監査が検証済み
(この判断を覆すなら、その実験結果は再利用できる)。

→ 2 件はこの issue のついでに手で直せば済む。
