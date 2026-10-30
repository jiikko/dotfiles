#!/usr/bin/env bash
# PostToolUse(Write / Edit) フック: dotfiles の checkout (本体か worktree) の中のファイルを書いた直後に、そのファイルだけを
# scripts/check_comment_provenance.sh --file に通し、コメントに経緯・出所の語 (issue の番号など) が一覧を超えて増えていたら、数えた行を知らせる。
#
# なぜ: 同じ検査は root の `make test` (test-lint) と CI でも走るが、commit の前の一括で見つかると、落ちたファイルの数だけ
# 直しと回し直しが増える。書いた直後なら、そのファイルを開いている間に直せる。
#
# 🚨 **止めない (additionalContext だけ)**。違反が無いときは何も出さない。
# 🚨 **検査は、この hook と同じ repo の scripts/ のものだけを走らせる**。書いた先の repo の同名スクリプトは実行しない
#   (他の repo の中身を、編集のたびに実行することになる)。書いた先が同じ repo (git の common dir が一致) のときだけ検査し、
#   一覧 (allowlist) は書いた先の checkout のものを読む。
#
# 検出しない形 (silent): Bash の書き込み (sed / heredoc)・NotebookEdit・dotfiles 以外の repo・jq / git が無い環境・
#   検査が違反の数え上げ以外で終わったとき (fail-open。検査の誤りは make test が見る)・検査の対象外の種類のファイル (scripts/check_comment_provenance.sh の冒頭)
# 分かっていて受ける形: rename した直後のファイルは、一覧が旧い path のままなので知らせる (全体の検査は rename として通し、--update を案内する)

set -uo pipefail
unset CDPATH

command -v jq >/dev/null 2>&1 || exit 0
command -v git >/dev/null 2>&1 || exit 0

self=$(readlink -f "$0" 2>/dev/null) || exit 0
hook_repo=$(cd "$(dirname "$self")/../.." && pwd -P) || exit 0
check="$hook_repo/scripts/check_comment_provenance.sh"
[ -x "$check" ] || exit 0

input=$(cat)
file=$(printf '%s' "$input" | jq -r '.tool_input.file_path // ""' 2>/dev/null) || exit 0
case "$file" in
  /*) ;;
  *) exit 0 ;;
esac
[ -f "$file" ] || exit 0
# 書いた path が symlink なら実体で判定する (~/.zshrc → dotfiles/_zshrc のように、repo の外から repo のファイルを編集する)
file=$(readlink -f "$file" 2>/dev/null) || exit 0

dir=$(cd "$(dirname "$file")" && pwd -P) || exit 0
root=$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null) || exit 0
common=$(git -C "$root" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || exit 0
hook_common=$(git -C "$hook_repo" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || exit 0
[ "$common" = "$hook_common" ] || exit 0

# git の --show-toplevel は実パスを返すので、書いたファイルの dir も実パスにそろえてから root を外す
rel="${dir#"$root"/}/${file##*/}"
[ "$dir" = "$root" ] && rel="${file##*/}"
case "$rel" in /*) exit 0 ;; esac

out=$(CHECK_COMMENT_PROVENANCE_ROOT="$root" "$check" --file "$rel" 2>/dev/null)
[ $? -eq 1 ] || exit 0
# rc=1 は判定不能 (path にタブ) も兼ねる。経緯の語を数えた形のときだけ知らせる
case "$out" in *"コメントに経緯・出所の語が"*) ;; *) exit 0 ;; esac

msg="$(printf '%s\n' "$out" | head -n 21)
コメントは Why not だけを書き、経緯・出所 (issue の番号・レビューの指摘・以前は〜) はコミットログへ移す (CLAUDE.md の「何をどこに書くか」)。同じ検査は make test-lint の test-comment-provenance でも落ちる"
jq -n --arg c "$msg" '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $c}}'
