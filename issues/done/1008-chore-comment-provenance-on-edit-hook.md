# 1008 (chore): コメントの経緯の語を、編集した直後にそのファイルだけ検査して知らせる hook

起票日: 2026-10-30

## 概要

retro 1007 の C。コメントの経緯・出所の語 (issue の番号・以前は・レビューの指摘) は `scripts/check_comment_provenance.sh` が
root の `make test` (test-lint) と CI で落とすが、見つかるのは commit の前の一括なので、落ちたファイルの数だけ直しと回し直しが増える。
1007 のセッションでは 4 回 (load.go・tickchain・glogx・test_golangci_ruleguard_key.sh)、retro 1004 でも同じ検査で落ちた。

書いた直後に、そのファイルだけを同じ検査に通して知らせる。

## 対応方針

- 検査に `--file <path>` を足す: 1 ファイルだけを数え、一覧 (`comment_provenance_allowlist.txt`) の件数を超えたら数えた行を出して rc=1。
  git に add する前のファイルも数える (Write で作った直後のファイルが知らせの主な対象)。対象のファイルの種類の分類は関数 `classify` に切り出し、
  全体の走査と共有する (分けると、hook が知らせる範囲と CI が落とす範囲がずれる)
- PostToolUse(Write|Edit) の hook `_claude/hooks/comment-provenance-on-edit.sh`: 書いたファイルが dotfiles の checkout (本体か worktree。git の common dir で判定) の中なら
  `--file` を走らせ、rc=1 のときだけ additionalContext で数えた行を知らせる。止めない
  - 検査は hook と同じ repo (`~/dotfiles`) の scripts/ のものだけを走らせ、書いた先の repo の同名スクリプトは実行しない。一覧は書いた先の checkout のものを読む

## 検出しない形

- Bash の書き込み (sed / heredoc)・NotebookEdit・dotfiles 以外の repo
- 検査が経緯の語の数え上げ以外で終わったとき (path にタブなどの判定不能も含む。fail-open。検査そのものの誤りは make test が見る)
- 一覧の件数までの既存の違反 (件数のラチェットなので、同じファイルで 1 行直して 1 行足すのも黙る。CI の判定と同じ)
- 一覧の件数が減ったまま `--update` していない形 (全体の検査だけが落とす)
- 追跡されていない gitignore のファイル (`tmp/` など。add されないので CI も見ない。`~/.gitignore_global` の `.envrc` も同じ扱いで、add には `-f` が要る)

分かっていて受ける形: rename した直後のファイルは、一覧が旧い path のままなので知らせる (全体の検査は rename として通し、`--update` を案内する)。

## 進捗

- [x] `--file` と `classify` の切り出し (`scripts/check_comment_provenance.sh`)。1 ファイルで約 30ms
- [x] hook と settings.json の配線 (PostToolUse の Write|Edit。gofmt-on-edit.sh の後)
- [x] テスト: `tests/scripts/test_check_comment_provenance.sh` に `--file` の 6 件、`tests/claude/test_comment_provenance_on_edit.sh` (配線の検査 + 13 件)
- [x] 変異 9 本で red (うち 3 本はレビュー後の直しの分。下) (`mutate-verify`): common dir の判定を外す / 書いた先の repo のスクリプトで走らせる / repo root の直下の path の組み立てを外す /
  `--file` を git の追跡済みに限る / 一覧の件数を見ない / 走査の対象外も数える
- [x] 入口: root の CLAUDE.md (「何をどこに書くか」の下)・scripts/CLAUDE.md の検査の表
- [x] 敵対的レビュー (opus、読み取りのみ) を 2 周
  - 1 周目: 全体の検査 (引数なし・`--list`) の出力は親の commit と一致し、壊せなかった。再現した指摘のうち直したもの:
    P2 symlink 越しの書き込み (`~/.zshrc` → `_zshrc`) で黙る → `readlink -f` で実体にしてから判定 /
    P2 追跡されていない gitignore のファイル (`tmp/`) に偽の知らせ → `--file` が `git check-ignore` に当たるものを数えない /
    P3 path にタブがある (判定不能の rc=1) を違反として知らせる → 経緯の語を数えた出力のときだけ知らせる。
    記録だけ: P3 rename の直後の知らせ (上の「分かっていて受ける形」)・P3 大文字小文字の違う path で黙る (直した版では `readlink -f` が実体の綴りに直すので知らせる)。
    直した 3 か所それぞれに変異を当てて red
  - 2 周目 (直した差分): 再現できたのは P3 1 件 (hook の冒頭の「大文字小文字の違う path は黙る」が実際と合わない) だけで、その項を消した。
    dir の symlink・ignore の打ち消し (`!x`)・追跡済みで ignore に当たるファイル・worktree の .gitignore は壊せなかった。
    未確認: Claude Code の Write が symlink を辿って書くか、普通のファイルに置き換えるか (どちらでも判定は合うと推測)
- [x] `~/dotfiles` に pull した後、headless の新しい session (haiku) に worktree の中へ `# issue 123 で直した` を含むファイルを Write させ、transcript の `hook_additional_context` (PostToolUse:Write) に数えた行と案内が載ったことを確かめた (2026-10-30)
