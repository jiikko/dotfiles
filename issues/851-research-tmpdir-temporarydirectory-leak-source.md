# 851 (research): $TMPDIR に TemporaryDirectory.* が 1 日数千件溜まる出どころを調べる

起票日: 2026-10-21

## 概要

$TMPDIR の直下に空の `TemporaryDirectory.*` が 34 日で約 10 万件溜まっている。作っているプロセスを突き止めて止める。
