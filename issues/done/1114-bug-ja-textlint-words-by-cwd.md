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
- 2026-11-26: 直した (commit はまだ)。変えたファイルは `bin/ja-textlint` と `tests/bin/test_ja_textlint.sh`
  - ファイルを検査するとき (`lint_one`) と一覧を確かめるときに、ファイルのフォルダを cwd にして起こす (`jw_in`)。入力のリダイレクトは `jw_in` を呼ぶ行に書くので、cd の前に開かれ、相対パスの名前も読める。フォルダは絶対パスに直してから cd に渡す (`dir_of`。相対のままだと CDPATH で別のフォルダに入る)
  - 一覧があるかは、フォルダごとに 1 回確かめる (`words_for`。同じフォルダの 2 件目からは前の答えを使う)
  - 標準入力の文は、今までどおり起動した cwd で探す。冒頭のコメントに、ファイルと標準入力で探す場所が違うことを書いた
  - テストを 4 件足した
    - cwd と別の repo のファイルに、ファイルの repo の一覧を使う
    - cwd の repo の一覧を別のフォルダのファイルに当てず、dotfiles の一覧を渡す
    - 相対パスのファイルを読む
    - 標準入力は cwd の一覧で検査する
  - 直した後は `bash tests/bin/test_ja_textlint.sh` が rc 0 (✓ 46 件)
  - 直す前の `bin/ja-textlint` (`git show HEAD:bin/ja-textlint`) に同じテストを掛けると rc 1 で、足したうちの 3 件が落ちる。落ちるのは、別の repo のファイル・cwd の一覧と別のフォルダのファイル・相対パスのファイル。標準入力の 1 件は直す前も通る (挙動を変えていない)
  - リダイレクトを cd の subshell の中に書く形 (hook の形の写し) にわざと壊すと、相対パスのファイルの 1 件だけが rc 3 で落ちる
  - `shellcheck -S warning` と `make test-lint` は rc 0。`make test` の全体は回していない
- 2026-11-26: 敵対的レビュー (opus) の指摘 2 件を直した
  - `dir_of` が `dirname` のコマンド置換でフォルダを取っていて、改行で終わる名前のフォルダが別のフォルダの名前になっていた。文字列の展開 (`${1%/*}`) で取る形に直した
  - テストは冒頭で CDPATH を外すので、`dir_of` が絶対パスに直す行を消しても通っていた。CDPATH を付けて起こすケースを足した
  - 足したテストは 2 件 (改行で終わる名前のフォルダ・CDPATH と相対パス)。`bash tests/bin/test_ja_textlint.sh` は rc 0 (✓ 48 件)
  - `dirname` のコマンド置換に戻した写しでは、改行のケースの 1 件だけが落ちる。絶対パスに直す行を消した写しでは、CDPATH のケースの 1 件だけが落ちる
- 2026-11-26: 敵対的レビューの 2 周目 (opus) の指摘 1 件を直した
  - cwd が symlink のフォルダの中で `../x.md` を渡すと、textlint は物理の cwd (symlink を辿った先) からファイルを読むのに、`dir_of` は論理の `$PWD` から `..` を作り、論理の cd で別のフォルダ (symlink の置き場の親) に入って、そこの一覧で検査していた
  - `jw_in` の cd を `cd -P` にした (物理のパスで `..` を解く)。テストに、cwd が symlink のフォルダの中で `../x.md` を渡すケースを足した。
    `mutate-verify` で `cd -P` を論理の cd に戻すと、このケースだけが落ちる (dotfiles の一覧が渡り project_term が出ない) ことを確かめた
  - 直していないもの: hook (`_claude/hooks/ja-textlint-on-edit.sh`) の `jw` も論理の cd だが、hook は絶対パスだけを受け、`..` を含む形は Claude Code の道具から来ないので触らない
- 2026-11-27: セルフレビューで、hook (`_claude/hooks/ja-textlint-on-edit.sh` の `jw`) に、この issue で bin/ja-textlint から直した 2 つの形が残っていたのを直した
  - フォルダを `$(dirname "$file")` で取っていた (末尾の改行が落ちる) → 文字列の展開 (`${file%/*}`) で取る
  - 論理の cd だった (symlink のフォルダの中のファイルで、ja-writing が symlink の置き場のパスから一覧を探し、bin/ja-textlint と答えが食い違う) → `cd -P`
  - tests/claude/test_ja_textlint_on_edit.sh に、symlink のフォルダの中のファイルで実体の repo の一覧 (allow.txt の「土台」) を使うケースを足した。`mutate-verify` で論理の cd に戻すと、このケースだけが落ちる (「3 slop_vocabulary」が出る)。改行で終わるフォルダ名は、hook の入力 (Claude Code の道具のパス) で作りにくいのでテストを足していない
