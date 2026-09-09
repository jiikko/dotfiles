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
#       **保持は既定 14 日** ($CLAUDE_ISSUE_PROGRESS_TTL_DAYS で変更)。SessionStart のたびに
#       期限切れを掃除する。SessionEnd に置かないのは、クラッシュ・強制終了で走らないため
#       (実測 2026-09-09: 導入 3 日で 174 ファイル。1 プロジェクトあたり 1 日約 100 セッション)

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
# パス構成要素として安全な形だけを通す (issue 302 ②。`/` や `..` が来ると書き先が外れる)
issue_progress_valid_session_id "$session_id" || exit 0
head=$(git -C "$ISSUE_HOOK_ROOT" rev-parse HEAD 2>/dev/null || true)
[ -n "$head" ] || exit 0

state_dir="${CLAUDE_ISSUE_PROGRESS_DIR:-$HOME/.cache/claude-issue-progress}"
mkdir -p "$state_dir" 2>/dev/null || exit 0

# --- 期限切れの状態ファイルを掃除する (issue 302 ①) -------------------------------------------
#
# 🚨 **消す対象を拡張子で必ず絞る**。`$state_dir` は env で上書きでき (テストが実際に
# 差し替える)、`-type f -delete` のような広い形にすると**差し替え先の中身を巻き込む**。
# ここが消してよいのは自分が書いた 2 種類だけ。
# 🚨 **失敗を握り潰さない** (`adversarial-review-own-safeguards.md` 節 2)。消せなかった件数を
# 数えて、0 でなければ stderr に出す (沈黙 = 成功にしない)。掃除の失敗で hook 自体は止めない
# (基準点の記録の方が主目的なので、そちらへ進む)。
issue_progress_sweep() {
  local dir="$1" days="$2" f removed=0 failed=0
  # find は「そこに在るもの」を列挙するだけ。削除は 1 件ずつ rc を見る
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    if rm -f -- "$f" 2>/dev/null; then removed=$((removed + 1)); else failed=$((failed + 1)); fi
  done <<EOF
$(find "$dir" -maxdepth 1 -type f \( -name '*.head' -o -name '*.reported' \) -mtime "+$days" 2>/dev/null)
EOF
  [ "$failed" -eq 0 ] || printf 'issue-progress: 状態ファイルを %d 件消せなかった (%s)\n' "$failed" "$dir" >&2
  printf '%s' "$removed"
}

ttl_days="${CLAUDE_ISSUE_PROGRESS_TTL_DAYS:-14}"
case "$ttl_days" in ''|*[!0123456789]*) ttl_days=14 ;; esac
[ "${#ttl_days}" -le 4 ] || ttl_days=14
swept=$(issue_progress_sweep "$state_dir" "$ttl_days")
# 掃除した件数は「0 件を成功にしない」ための観測点。テストはこの行を見る
[ "${swept:-0}" -eq 0 ] || printf 'issue-progress: %d 日より古い状態ファイルを %s 件掃除した\n' "$ttl_days" "$swept" >&2
# 同じセッションで別 repo に移ったときは最初の repo の基準点を保つ (最初に開いた repo が作業対象)
[ -f "$state_dir/$session_id.head" ] && exit 0
printf '%s\n%s\n' "$ISSUE_HOOK_ROOT" "$head" >"$state_dir/$session_id.head"
exit 0
