#!/usr/bin/env bash
#
# SessionStart フック: issue-progress-check.sh (Stop) の基準点を記録する。
#
# なぜ: 「作業が終わった時に、関わった issue が更新されているか」を見るには、作業前の状態が要る。
# git があれば md5 は要らない — 開始時の HEAD さえ控えれば、差分 (`git diff <HEAD>..` + `git status`) と
# チェックボックスの増減 (`git show <HEAD>:<path>` との比較) で「触ったか」「進捗を書いたか」が読める。
# 記録は session_id ごと (並行セッションで基準点を上書きしない)。
#
# 入力: SessionStart の hook JSON (stdin: session_id / cwd)。出力: なし (黙って記録する)。
# 状態: $CLAUDE_ISSUE_PROGRESS_DIR (既定 ~/.cache/claude-issue-progress)/<session_id>.head

set -u

lib="$(dirname "$0")/lib/issue-hooks.sh"
# shellcheck source=_claude/hooks/lib/issue-hooks.sh
. "$lib" 2>/dev/null || exit 0
command -v issue_hook_resolve_dir >/dev/null 2>&1 || exit 0

input=""
[ -t 0 ] || input=$(cat 2>/dev/null || true)
[ -n "$input" ] || exit 0
issue_hook_resolve_dir <<<"$input" || exit 0

session_id=$(issue_progress_json_field "$input" session_id)
[ -n "$session_id" ] || exit 0
head=$(git -C "$ISSUE_HOOK_ROOT" rev-parse HEAD 2>/dev/null || true)
[ -n "$head" ] || exit 0

state_dir="${CLAUDE_ISSUE_PROGRESS_DIR:-$HOME/.cache/claude-issue-progress}"
mkdir -p "$state_dir" 2>/dev/null || exit 0
# 同じセッションで別 repo に移ったときは最初の repo の基準点を保つ (最初に開いた repo が作業対象)
[ -f "$state_dir/$session_id.head" ] && exit 0
printf '%s\n%s\n' "$ISSUE_HOOK_ROOT" "$head" >"$state_dir/$session_id.head"
exit 0
