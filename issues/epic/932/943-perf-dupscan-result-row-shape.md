# 943 (perf): index / callers の結果行の形 — doc 先頭の長さと終端行

起票日: 2026-10-28

## 概要

1 行あたりの bytes が rg -n の行より大きく、結果行に終端行が無いため、関数 1 つを読むのに Read が範囲を決められない。

(本文は作成中。epic [932](932-epic-dupscan-accuracy.md) の子 issue)
