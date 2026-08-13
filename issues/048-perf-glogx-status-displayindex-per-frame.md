# glogx: status viewer の displayIndex が変更ファイル数に比例して毎フレーム確保する

起票日: 2026-08-14
種別: perf (実測駆動)
優先度: **P3** (通常のファイル数では小さい。大きな merge / 大量 untracked のときだけ効く)

## 観測した事実

`statusView.displayIndex` は毎フレーム `make([]statusDisplayLine, 0, len(v.rows)+4)` を確保し、
全行を走査して見出し + 行の並びを組み立てる。文字列は作らない (整形は可視の窓だけ = 既に対処済み。
`displayIndex` の doc が一次情報) が、**index スライス自体は件数に比例する**。

ベンチ別 alloc_space の内訳:

| ベンチ | `displayIndex` の flat | 全体に対して |
|---|---|---|
| `StatusViewFrame` (40 ファイル) | 46.6 MB | **2.4%** |
| `StatusViewFrame2000` (2000 ファイル) | 1438.4 MB | **47.1%** |

ベースライン (count=6) の B/op でも整合する:

| ベンチ | B/op | allocs/op |
|---|---|---|
| `StatusViewFrame` (40) | 53,903 | 359 |
| `StatusViewFrame2000` (2000) | 105,219 | 364 |

+1960 行で **+51KB / allocs は +5 だけ** = 増えているのは「1 本の大きなスライス」であり、
1 エントリ ≒ 26 B。2000 ファイルで 47KB/frame、12.5fps なら約 590 KB/s のゴミ。

## なぜ P3 か (誇張しない)

- 40 ファイル (現実的な作業ツリー) では 54KB フレームのうち **2.4%** にすぎない
- alloc の**回数**は件数に依存しない (+5) ので、GC の回転数への寄与は小さい
- 2000 ファイルは「大きな merge・初回 clone 直後の untracked 大量」で実際に起こりうるが、
  常時ではない

**混合プロファイルだと誤読する**: 4 ベンチを 1 つのプロファイルに混ぜると `displayIndex` が
17.8% に見え、フレームコストの第 2 位のように読めてしまった。実際は `StatusViewFrame2000`
だけが持ち込んでいた重み。ベンチ別に採り直して初めて 2.4% と分かった
(**教訓: 複数ベンチを 1 プロファイルに混ぜて順位を付けない**)。

## 対応方針

`v.rows` / `v.cursor` / セクション構成が変わらない間は index を再利用する (memo)。
無効化のキーは「rows の世代 + cursor」。cursor は `cursorAt` にしか影響しないので、
index 本体と `cursorAt` を分けて持てば cursor 移動で index を作り直す必要はない。

## 未確認 / 注意

- **memo の効果は「同じ状態で複数フレーム描く」ことに依存する**。status viewer は 1.5 秒周期で
  `git status` を読み直すので、その間に描くフレーム数だけ効く。ベンチは同一状態を回し続けるため
  **memo の効果を過大評価する**。実運用に近い評価は「1.5 秒あたり何フレーム描くか」を
  併記して行うこと
- 先に 046 / 047 を入れてから再測定する (フレームコストの分母が変わる)

## 検証条件

- `AllocsPerRun` / B/op で 2000 件時の減少を示す
- 40 件時に**悪化していない**ことも示す (memo の管理コストが小さいケースを食わないこと)
- 変異検証: 無効化キーを壊す (rows が変わっても memo を返す) と、
  ファイルを stage/unstage した後の表示がずれて red になるテストを置く

## 測定ログ

- `tmp/glogx-perf/perbench/StatusViewFrame_mem.prof` / `StatusViewFrame2000_mem.prof`
- `tmp/glogx-perf/baseline_bench.txt`
