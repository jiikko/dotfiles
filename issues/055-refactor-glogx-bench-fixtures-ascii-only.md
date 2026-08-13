# glogx: 描画ベンチのフィクスチャが ASCII 固定で、日本語混在の実運用を測れていない

起票日: 2026-08-14
種別: refactor
優先度: **P3** (測定の代表性の問題。046 で一部だけ塞いだ)

## 観測した事実

`tui_bench_test.go` の `benchBrowse` は commit subject も author もパスも **ASCII 固定**。
ところが glogx を使う repo の実データはそうではない:

```
$ git log --format='%s' -100 | grep -cP '[^\x00-\x7f]'
97
```

**直近 100 commit のうち 97 件**の subject が非 ASCII (直近 300 では 98.0%、
表示幅の中位は 75 セル)。

これが実際に測定を誤らせた。046 (`dispWidth` の fast-path) の効果は:

| フィクスチャ | 倍率 |
|---|---|
| ASCII subject | ×2.74 |
| 日本語 subject (77 セル) | **×2.07** |
| R3 が実 commit subject 20 本で測ったもの | ×2.01 |

**ASCII だけで測ると 30% 以上の過大評価**になる (CJK 行は fast-path を通れず
ライブラリへ落ちるため)。046 のレビュー R2 の指摘で `BenchmarkViewSteadyJA` を
足して初めて分かった。

## 残っている穴

046 で足したのは `BenchmarkViewSteadyJA` の 1 本だけ。**他は今も ASCII 固定**:

- `BenchmarkViewWithPanel` / `BenchmarkStatusViewFrame` / `BenchmarkStatusViewFrame2000` /
  `BenchmarkCursorMoveView` / `BenchmarkViewWithDiff` / `BenchmarkModelInit200`
- `benchStatusBrowse` のファイルパスも ASCII (`src/glogx/internal/deeply/...`)。
  日本語のファイル名は稀なのでこちらは代表性の問題が小さい
- `BenchmarkRenderLinesLargePatch` の patch 内容 (diff 本文は ASCII が普通なので低優先)

## 対応方針 (案)

`benchBrowseSubjects(tb, n, w, h, ja bool)` は既にあるので、代表性が問題になる
ベンチだけ ja 版を対で足す。**CI の予算 (`bench_budgets.ci`) には足さない**
(metric が倍に増えるだけで、回帰検出は片方で足りる) — ローカルで
「効果が内容に依存するか」を見るための対照として持つ。

⚠️ 全部に足すのは過剰。**幅計算が効くベンチ**だけでよい
(046 の効果が出たのは View 系。`ModelInit200` は行構築が主なので効果が薄い)。

## 未確認

- status viewer のパスに日本語が入る頻度 (日本語ファイル名の repo をどれだけ扱うか)
- diff 本文に日本語が入る頻度 (コメントや文字列リテラル経由では普通にある)

## 関連

- `issues/done/046-perf-glogx-dispwidth-fastpath-dead.md` (代表性の実測が一次情報)
- `src/glogx/tui_bench_test.go` の `benchBrowseSubjects` の doc
