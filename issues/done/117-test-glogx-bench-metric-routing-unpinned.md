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

---

## 対応 (2026-08-27)

既存の `tests/glogx/test_bench_glogx_metrics.sh` に節を 1 つ足した (新しい仕組みは作っていない。
`GLOGX_BENCH_INPUT` seam をそのまま使う)。

### benchmark 名は列挙せず導出する

```sh
all_names=$(grep -h '^func Benchmark' "$SRC_DIR"/*_test.go | sed ... | sort -u)
registered=$(sed -n "s/.*-bench '^(\(.*\))\$'.*/\1/p" "$BENCH" | tr '|' ' ')
```

⚠️ **列挙すると、兄弟を足したときに追随を忘れる = まさにこの検査が守りたい事故を検査自身が踏む。**
実測で未登録 5 本 / 登録済み 12 本を自動で拾えている (監査の数と一致)。

主張は 3 つ:

1. 未登録の benchmark は metric を **1 行も出さない**
2. 登録済みの benchmark は **必ず出す** (予算に載らず黙って未計測になるのを防ぐ)
3. 桁違いの版 (`StatusViewFrame2000`) が無印の metric を**上書きしない**

走査 0 件も fail にした (導出が壊れたら赤にする)。

### 変異検証 4/4 red

`ViewSteady` を前方一致へ (→ `BenchmarkViewSteadyJA` が metric を出したと名指し) /
`StatusViewFrame` を前方一致へ / 登録済みの emit を落とす / 導出を壊す。

⚠️ 2 つ目は**既存の assert (節 1 の実 bench) が先に捕まえた**ので、自分の新しい assert に
検知力があるかを別途確かめた: `PATH` から go を外して節 1 を skip させた状態で変異を当て、
`✗ StatusViewFrame2000 が無印の metric まで出した (前方一致の遮蔽)` が出ることを確認。
**「テスト全体が red」を自分の assert の検知力の証拠にしない。**

### 敵対的レビューは回していない (判断)

production コードを 1 行も変えていない (テストのみ)。検査自身の検知力は上記のとおり
変異 4 本 + 独立確認で測った。

### 残課題 (この検査でも止められない)

- 振り分け表そのものの誤り (metric 名の取り違え)
- **benchmark を新設して `bench_glogx.sh` に配線し忘れる**方向。`check_bench_budgets.sh` は
  「予算にあるのに出力に無い」しか見ないので、こちらは今もどの検査も鳴らない。
  done 45 本の中に実バグが無いため規約化の根拠が足りず、今回は入れていない
