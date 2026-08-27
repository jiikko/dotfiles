# 117 test: bench の metric 振り分けが「兄弟 benchmark 遮蔽」に対して無防備

起票日: 2026-08-27 / 出典: lint-from-done 監査 / priority: low

## 事実

`tests/glogx/bench_glogx.sh` の awk 振り分けは**完全一致** (`bench == "BenchmarkViewSteady"`) に
なっている。これは `issues/done/051-perf-glogx-bench-gates-time-only.md` の敵対的レビュー R1-5
「awk の前方一致」で直された形。

**だがこの修正を守るテストが無い。** 前方一致 (`bench ~ /^BenchmarkViewSteady/`) に戻す変異を
当てると、合成入力で **5 件が誤 emit** される (監査が実測):

```
ViewSteadyJA      → view_steady        (本来 JA 対照で metric を持たない)
ViewWithPanelJA   → view_panel
CursorMoveViewJA  → cursor_move_view
ViewWithDiffJA    → view_diff
StatusViewFrame2000 → status_view_frame  (2000 版が無印を上書き)
```

現行の完全一致版に同じ入力を流すと出力 0 行。

## 発火条件

JA 対照 benchmark (`*JA`) や桁違いの版 (`StatusViewFrame2000`) が既存 metric を上書きし、
**予算ゲートが別の benchmark の数字で判定する**。数値は出るので気づけない (silent)。

## 対応

新しい仕組みは不要。**既存の `tests/glogx/test_bench_glogx_metrics.sh` に節を 1 つ足す**
(`GLOGX_BENCH_INPUT` seam をそのまま使える)。

benchmark 名は `grep '^func Benchmark' src/glogx/*_test.go` から**導出**し、

- `-bench` 正規表現に載っていない名前 → metric 行が 1 行も出ないこと
- 載っている名前 → 自分の metric だけが出ること

を assert する。導出なので新しい兄弟が増えても自動で覆う。偽陽性は 0 件 (未登録 5 本を流して
全て出力 0 行、`StatusViewFrame2000` は `status_view_2000` のみ、を実測済み)。

## この検査でも止められないもの

- 振り分け表そのものの誤り (metric 名の取り違え)
- **benchmark を新設して `bench_glogx.sh` に配線し忘れる**方向。`check_bench_budgets.sh` は
  「予算にあるのに出力に無い」しか見ないので、こちらは現状どの検査も鳴らない。
  ただし done 45 本の中に実バグが無いため、今回は規約化の根拠が足りないと判断した
  (次に配線漏れを踏んだらここが起点)
