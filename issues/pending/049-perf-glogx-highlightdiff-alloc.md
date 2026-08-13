# glogx: HighlightDiff が 1 回の diff で 302MB / 428 万 alloc を出す (現実的なコーパスで測り直してから判断)

起票日: 2026-08-14
種別: perf (実測駆動)
状態: **pending** — 着手条件を満たすまで手を付けない (下記)

## 観測した事実

`BenchmarkHighlightDiff` (`maxDiffLines` = 5000 行が全部 Go コード) の実測 (count=6):

```
BenchmarkHighlightDiff-14   1   417979875 ns/op   302863296 B/op   4281150 allocs/op
```

**1 回の呼び出しで約 418ms / 302MB / 428 万 alloc。** 内訳 (alloc_space):

| 関数 | flat | 割合 |
|---|---|---|
| `regexp2.newMatchText` | 1620 MB | **51.2%** |
| `chroma.matchRules` | 532 MB | 16.8% |
| `regexp2.newMatch` | 380 MB | 12.0% |
| `regexp2.(*Match).Groups` | 118 MB | 3.7% |
| `regexp2.(*Runner).initMatch` | 100 MB (cum 480) | 3.2% |

CPU 側も同じ経路 (`regexp2.(*Runner).scan` cum 28.2%、`executeDefault` cum 18.3%)。
つまり **chroma が行ごとに regexp2 でトークナイズし、マッチ試行ごとに Match オブジェクトを
確保している**のが本体で、glogx 側のコードではない (2026-08-13 のエスケープ表メモ化
`hlEscCache` で 886→413ms まで下げた分は既に入っている)。

## 着手条件 (trigger) — なぜ今やらないか

### 1. 既存ベンチのコーパスが「同一行 5000 本」で、測定が信用できない

`highlight_test.go` の入力は

```go
for range maxDiffLines {
    lines = append(lines, `+func f(x int) string { return fmt.Sprintf("%d", x) } // comment`)
}
```

**5000 行すべてが同一文字列**。したがって `(lexer, code) → 結果` のメモを入れると
**約 5000 倍の「改善」が出るが、実運用の diff では行はほぼ全て異なるので嘘になる**。

→ 着手前に **実 repo の大きなコミットの `git show` を取り込んだコーパス**へ差し替えること。
そのうえで「行の重複率」を実測し、メモ化の期待効果を見積もってから判断する。

### 2. レイテンシとしては体感経路に乗っていない

`HighlightDiff` の呼び出しは `LoadCommitDiff` (gitlog.go) の 1 箇所だけで、
`tea.Cmd` の非同期 + スピナー付き。つまり 418ms は**フレーム予算ではない**。
問題の性質は「レイテンシ」ではなく **ヒープの瞬間的な山と GC 圧**。

soak (実バイナリ) の実測では、diff popup を 20 回開閉 (= 20 コミット分のハイライト) しても
`HeapInuse` は 9.4MB → 12.9MB で収まり、`heap_objects` も 11.5k → 19.0k。
**常駐ヒープの増加としては現れていない** (302MB は短命で GC が回収している)。
→ 「放置すると膨らむ」類の不具合ではない。

### 3. 構造変更が必要で、切り捨てやすさの設計と衝突する

削るには「見えている範囲だけ段階的にハイライトする」等の構造変更が要る。
`highlight.go` 冒頭は「実験的機能・切り捨てやすさ優先で chroma への依存をこのファイルに
閉じている」と明記しており、段階適用は diff overlay 側 (スクロール位置) との結合を生む。
**この結合を受け入れる判断が要る** ので、実測の裏付けなしに入れない。

## 着手してよくなる条件

以下のいずれかが観測されたとき:

- 実コーパスで測って、なお 1 回の diff が 200ms 以上 or 100MB 以上を出す
- diff popup を開くのが体感で遅いというユーザー報告が出る
- soak で `HeapInuse` が diff の開閉回数に比例して増える (= 短命でなくキャッシュに残っている)

## 未確認 (推測として明記)

- 「実 diff での行重複率」を測っていない (空行・`}`・import 等でどれだけ重複するか不明)
- chroma / regexp2 の新しいバージョンで alloc が改善しているかを確認していない
  (go.mod は chroma v2.27.0 / regexp2 v2.5.2)
- 「見えている範囲だけ段階適用」の実装コストと、スクロール時の再ハイライトの見た目
  (色が遅れて付く) が許容されるかは未評価

## 測定ログ

- `tmp/glogx-perf/baseline_bench.txt` (count=6)
- `tmp/glogx-perf/hl_cpu.prof` / `hl_mem.prof` (+ `glogx.test` でシンボル化)
- soak のヒープ推移: `tmp/glogx-perf/soak/memstats.log`
