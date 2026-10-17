# 820 (bug): worktree を消した後、到達不能の検査が消えた worktree のパスで赤くなる

起票日: 2026-10-18

## 概要

`scripts/check_unused_excluding_tests.sh` が、消した worktree のパスを含む結果を出して赤くなる (retro 819 の 4)。本文はこの後に書く。
