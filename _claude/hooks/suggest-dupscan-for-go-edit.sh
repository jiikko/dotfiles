#!/usr/bin/env bash
# PreToolUse(Write / Edit) フック: repo の root の src/ の下の .go を書こうとしたら、dupscan の「書く前に引く」と
# 「commit の前に gate」を 1 行案内する (session に 1 回)。
#
# なぜ: skill や CLAUDE.md に「書く前に引く」と書くだけでは、書く瞬間に思い出されない (suggest-dupscan-for-symbol-grep.sh の冒頭の実測)。
# 書く瞬間に 1 行だけ出す。
#
# 🚨 **止めない (additionalContext だけ)**。permissionDecision を返さない。
# 🚨 **出しすぎない**: session に 1 回だけ。印は他の hook と共有の lib (hooks/lib/session_marks.py) で ${XDG_CACHE_HOME:-~/.cache}/claude-dupscan-hooks.marks の 1 ファイルに持つ
#    ($TMPDIR に session ごとのファイルを作らない: macOS では数日たっても片付かず溜まる)。
#
# 判定:
#   - tool_input.file_path が .go で、そのファイルの属する git repo の root から見て src/ の下 (cwd の repo では判定しない。
#     別の repo の src/ を書くこともある)。まだ無いファイル・dir (Write の新規作成) は、在る祖先の dir で repo を引く
#   - dupscan が PATH に在る (無ければ案内しても打てない)
#
# 検出しない形 (silent): Bash の書き込み (sed / heredoc)・NotebookEdit・src/ 以外の .go・jq / git が無い環境 (fail-open)・
#   大文字小文字の違う `Src/` (macOS では同じ dir だが、書かれた綴りで比べる)
# 分かっていて受ける形: 途中に無い dir を挟んだ `..` (`src/nope/../../other/x.go`) は src の下と読んで案内する (Write 自体が失敗する)。
#   255 文字を超える session_id は印のファイルを作れず毎回案内する (実際の session_id は UUID)

set -uo pipefail
unset CDPATH

command -v jq >/dev/null 2>&1 || exit 0
command -v git >/dev/null 2>&1 || exit 0
command -v dupscan >/dev/null 2>&1 || exit 0
command -v python3 >/dev/null 2>&1 || exit 0
marks="$(cd "$(dirname "$(readlink -f "$0")")" && pwd -P)/lib/session_marks.py"

input=$(cat)
file=$(printf '%s' "$input" | jq -r '.tool_input.file_path // ""' 2>/dev/null) || exit 0
session=$(printf '%s' "$input" | jq -r '.session_id // "unknown"' 2>/dev/null) || exit 0
case "$file" in
  /*.go) ;;
  *) exit 0 ;;
esac

dir=$(dirname "$file")
while [ ! -d "$dir" ] && [ "$dir" != / ]; do
  dir=$(dirname "$dir")
done
# git の --show-toplevel は実パス (symlink を解いた形) を返すので、書くファイルの dir も実パスにそろえてから前方一致で比べる
# (/var と /private/var のような symlink を挟んだ file_path で外れないように)
root=$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null) || exit 0
real_dir=$(cd "$dir" && pwd -P) || exit 0
rel="${real_dir}/${file#"$dir"/}"
case "$rel" in
  "$root"/src/*) ;;
  *) exit 0 ;;
esac

# 印を読めない・書けないときは案内を出す側に倒す (lib が理由を cache の claude-hook-marks.log に残す)
python3 -I "$marks" last claude-dupscan-hooks.marks "$session" go-edit >/dev/null 2>&1 && exit 0
python3 -I "$marks" put claude-dupscan-hooks.marks "$session" go-edit 1 >/dev/null 2>&1

msg="src の Go を書いている: 新しい関数 (helper・値を組み立てる関数・テストの helper) を書く前に、\`dupscan index -name <語>\` / \`-returns <型>\` / \`-param-type <型>\` で同じものが既にないかを引く。commit の前に \`make test-dupscan-gate\` (make と CI と同じ下限 12 節の \`dupscan gate\`。素の \`dupscan gate\` は既定の下限 20 で、小さい写しを通す) で、この変更で増えた写しが無いかを見る (skill dupscan の 1・3 節。Go の def / refs は無いので gopls で引く)"
jq -n --arg c "$msg" '{hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $c}}'
