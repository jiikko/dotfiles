# 1107 (perf): CI の Tests の rest の job が上限 20 分に近い

起票日: 2026-11-24

## 概要

`.github/workflows/tests.yml` の `dotfiles-tests (rest)` が 11〜17 分かかり、`timeout-minutes: 20` に対して余裕が 3 分ほどしかない。遅くなっただけで落ちる。
