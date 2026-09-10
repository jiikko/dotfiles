# 散文の issue 参照は `issues/NNN-….md` のパスでなく番号で書く（パスは done 移動で腐る）

起票日: 2026-09-11
出典: [issue 348](done/348-retro-issue-done-tool-and-truecolor-guard-2026-09-10.md) 気づき 8

## 問題

`issue_done.sh` が張り直すのは markdown リンク（`](…)`）だけ。インラインコードや散文に書かれた
`` `issues/226-….md` `` のような裸のパスは対象外で、`tests/issues/test_issue_links_valid.sh` も
`issues/` 配下しか見ない。**issue を done へ送るたびに誰にも気づかれず 1 本ずつ切れる**。
2026-09-10 の全数勘定で 10 件あり（`nvim/lua/dotfiles/lsp.lua` / `src/lockman/main.go` ×2 /
`_claude/rules-rationale/` 3 本 / `_claude/rules/` 4 本）、その場で直した。

## 方針: (c) 番号で参照する

`issues/README.md` の「ファイル名は変えないので `issue 012` で安定して参照できる」が本来の意図。
(b) `issue_done.sh` を裸パスにも広げる案は却下: `issues/030-feat-a.md` のようなテスト fixture が
同じ形をしており、書き換えてはいけない対象と区別できない。

## 受け入れ条件

- [ ] `issues/README.md` に「issues/ の外（コード・rules・docs）からは番号で参照する。パスを書くなら markdown リンクにする」を 1 行足す
- [ ] `issues/` の外にある裸の `issues/NNN-…md` パスを全数勘定し、番号参照へ書き換える（前回 10 件 → 現在の件数を本文に書く）
- [ ] 再発を止める検査を入れるか判断する（`issues/` の外で `issues/[0-9]{3}-[^)]*\.md` がインラインコード内に現れたら fail。fixture のあるテストディレクトリは除外）。入れないなら理由をここに書く
