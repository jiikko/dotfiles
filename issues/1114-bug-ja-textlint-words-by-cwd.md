# 1114 (bug): ja-textlint が語の一覧を、ファイルの repo でなく cwd から探す

起票日: 2026-11-26

## 概要

`bin/ja-textlint` は、ファイルを標準入力で `ja-writing lint` に渡す。そのため語の一覧 (`.yomiyasu-words/`) は、起動したときの cwd から探される。
cwd と別の repo のファイルに掛けると、そのファイルの repo の一覧が使われない。
同じことをする hook (`_claude/hooks/ja-textlint-on-edit.sh`) は、書いたファイルのフォルダで `ja-writing lint` を起こすので、ファイルの repo の一覧を使う。
同じ操作の入口 2 つで約束が食い違っている (issue 1113 の監査の entry-parity、N2)。

## 詳細

決めたこと (issue 1078 の進捗、2026-11-25): 「その repo が自分の `.yomiyasu-words/` を持つときはそちらを使う (repo の持ち主が決めた語を優先する)」。
hook の冒頭のコメントも「書いたファイルの repo が自分の .yomiyasu-words/ を持てばそれを使い」と書く。`bin/ja-textlint` の冒頭は「その repo が自分の .yomiyasu-words/ を持つ (ja-writing lint が git の最上位か cwd で見つける) なら」と書き、どの repo かを決めていない。

再現 (2026-11-26 に実測):

1. 使い捨ての repo R に `.yomiyasu-words/flag.txt` (`りんご` の 1 行) と、`りんごを食べた。` を含む `x.md` を置く
2. cwd を R にして `bin/ja-textlint x.md` → `project_term: 語の一覧（flag.txt）の語（りんご）の候補` が出る
3. cwd を dotfiles の中 (R の外) にして `bin/ja-textlint R/x.md` → この指摘が出ない (dotfiles は `.yomiyasu-words/` を持たないので、dotfiles の一覧 `_claude/skills/ja-writing/words/` が渡る)

発火条件: cwd と別の repo のファイルに `ja-textlint` を掛け、どちらかの repo が `.yomiyasu-words/` を持つとき。
ファイルの repo の allow の語が指摘され、flag の語が指摘されない。cwd の repo の一覧が別の repo のファイルに当たる向きもある。
エラーは出ず、rc も変わらないので、気づかないうちに違う一覧で検査される。

## 対応方針

- `bin/ja-textlint` の `lint_one` と一覧の確かめ (`printf '' | jw lint --json -`) を、ファイルのフォルダを cwd にして起こす。
  hook の `jw` は絶対パスだけを受けて、cd の後に `<"$file"` で読む。`bin/ja-textlint` は打たれた名前 (相対パスのことがある) をそのまま持つので、同じ形を写すと cd した先で相対パスを開いて読めない。
  入力のリダイレクトを cd の subshell の外に置くか、先に絶対パスに直す
- 標準入力の入力 (`--stdin`) は置き場所が無いので、今のまま cwd で探す。冒頭のコメントに、ファイルと標準入力で探す場所が違うことを書く
- 一覧の確かめは今は 1 回だけ打っている。フォルダごとに答えが変わるので、ファイルのフォルダごとに確かめる (同じフォルダは 1 回にまとめる)
- `tests/bin/test_ja_textlint.sh` に、cwd と別の repo のファイルで、ファイルの repo の一覧が使われるケースと、相対パスで渡したファイルが読めるケースを足す。直す前の形で前者が落ちることを確かめる

## 関連ファイル

- `bin/ja-textlint` (`lint_one`・一覧の確かめ)
- `_claude/hooks/ja-textlint-on-edit.sh` (`jw`。揃える先)
- `tests/bin/test_ja_textlint.sh`
- issue 1078 (一覧の決まりを決めた issue)

## 進捗

- 2026-11-26: issue 1113 の監査で見つけて起票した。反証レビュー (opus) で、hook の形をそのまま写すと相対パスで壊れることを対応方針に足した
