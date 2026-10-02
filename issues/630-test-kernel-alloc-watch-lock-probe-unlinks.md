# 630 (test): test_kernel_alloc_watch.sh のロック待ちの判定が `-k` 無しの lockf で、`make test` の中でだけ落ちる

起票日: 2026-10-02

## 概要

`tests/bin/test_kernel_alloc_watch.sh` の「ロックを握る側が 20 秒たってもロックを取れない (判定できない)」が、並列の `make test` の中で落ちることがある。本文は後で書く。
