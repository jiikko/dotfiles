# 852 (bug): treefiler の TestFrameAllocBudget が、TMPDIR を変えると確保の回数の上限を超えて落ちる

起票日: 2026-10-21

## 概要

`src/treefiler/filer` の `TestFrameAllocBudget` が、使い捨ての `TMPDIR` で回すと「1 フレームの確保 262 回 (上限 258)」で落ちる。既定の `TMPDIR` では通る (本文は反証レビューの後に入れる)。
