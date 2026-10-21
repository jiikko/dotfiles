# 850 (bug): treefiler のテストが途中で死ぬと、$TMPDIR に空の treefiler-config-* が溜まる

起票日: 2026-10-21

## 概要

`src/treefiler/filer` の `TestMain` が作る `$TMPDIR/treefiler-config-*` が、テストのプロセスが途中で死ぬと消えずに残り、1 万件を超えて溜まった。本文は反証レビューの後に書く。
