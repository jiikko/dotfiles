#!/usr/bin/env bash
#
# Stop フック: このセッションで関わった issue に「todolist / 進捗 / 結果 / 残タスク」の追記が
# 無いまま応答を終えようとしていたら、Claude を止めて書かせる。
#
# なぜ: 実装が終わった後に issue を更新し忘れる漏れは、リマインドすると本当に漏れていることが多い
# (実測 2026-09-06 obaket 730: 関連 issue 3 本が未更新のまま「完了」を報告していた)。書くのは Claude の
# 手順 (~/.claude/CLAUDE.md「Issue管理」) で、この hook はその取りこぼしを出口で止める側。
#
# 判定 (2 段):
#   1. 変更の有無 — 基準点 (issue-progress-start.sh が記録した開始時 HEAD) から今までの commit と
#      作業ツリーで、その issue ファイルが 1 度も変わっていないか
#   2. 構造 — 変わっていても、`[x]` の数が増えておらず、進捗 / 結果 / 残タスク / 対応 の見出しも
#      増えていなければ「進捗が書かれていない」とみなす (中身の正しさは判定しない)
# 「関わった issue」= 基準点以降の commit subject の `(NNN)` / 変更パスの `issues/.../NNN-` / next/ の claim。
# 加えて、その番号を参照する open issue (残課題として指しているもの) が未変更なら列挙する。
#
# 検出しないもの (承知の上): 未 commit で番号も出していない作業 / 本文の記述が古いだけの漏れ /
# `git pull` で混ざった他セッションの commit に付いた番号 (誤報になりうるが、1 セッション 1 回で黙る)。
#
# 入力: Stop の hook JSON (stdin: session_id / cwd / stop_hook_active)。
# 出力: 指摘があれば {"decision":"block","reason":...} (Claude が続きを処理する)。同じ指摘は 1 セッション 1 回。
# stop_hook_active が true (block からの続き) のときは何もしない (無限ループ防止)。

set -u

lib="$(dirname "$0")/lib/issue-hooks.sh"
# shellcheck source=_claude/hooks/lib/issue-hooks.sh
if ! . "$lib" || ! command -v issue_hook_resolve_dir >/dev/null 2>&1; then
  exit 0
fi

input=""
[ -t 0 ] || input=$(cat 2>/dev/null || true)
[ -n "$input" ] || exit 0
[ "$(issue_progress_json_field "$input" stop_hook_active)" = "true" ] && exit 0
issue_hook_resolve_dir <<<"$input" || exit 0
root="$ISSUE_HOOK_ROOT"

session_id=$(issue_progress_json_field "$input" session_id)
[ -n "$session_id" ] || exit 0
state_dir="${CLAUDE_ISSUE_PROGRESS_DIR:-$HOME/.cache/claude-issue-progress}"
state="$state_dir/$session_id.head"
[ -f "$state" ] || exit 0
base_root=$(sed -n 1p "$state"); base=$(sed -n 2p "$state")
[ "$base_root" = "$root" ] || exit 0
git -C "$root" cat-file -e "$base^{commit}" 2>/dev/null || exit 0

# --- 変更集合 (root 相対パス、1 行 1 パス) ---
changed=$( {
  git -C "$root" diff --name-only "$base" HEAD 2>/dev/null
  git -C "$root" status --porcelain --untracked-files=all 2>/dev/null | sed -E 's/^.{3}//; s/^.* -> //'
} | sort -u)
is_changed() { grep -qxF "$1" <<<"$changed"; }

# --- 関わった issue 番号 ---
subjects=$(git -C "$root" log --format=%s "$base..HEAD" 2>/dev/null || true)
# primary = 作業対象と明示された番号 (commit subject の `(NNN)` / next/ の claim)。構造と関連 issue まで見る。
# path 由来 (issue ファイル自体を変更した番号) は「変更あり」が既に事実なので、関連 issue の列挙だけに使わない
# (関連 issue に 1 行足しただけの番号を「作業対象」に格上げすると、その issue 自身に進捗を要求する誤報になる)。
primary=$( {
  grep -oE '\(([0-9]{3}([,/ ]+[0-9]{3})*)\)' <<<"$subjects" | grep -oE '[0-9]{3}'
  while IFS= read -r d; do
    [ -d "$d" ] || continue
    find "$d" -path '*/next/*' -name '[0-9][0-9][0-9]-*.md' 2>/dev/null | sed -E 's#.*/([0-9]{3})-.*#\1#'
  done <<<"$ISSUE_HOOK_DIRS"
} | sort -u)
[ -n "$primary" ] || exit 0
nums="$primary"

# issue_file <NNN>: 実体ファイル (symlink 除外) を 1 つ返す
issue_file() {
  local d
  while IFS= read -r d; do
    find "$d" -type f -name "$1-*.md" 2>/dev/null | head -1 | grep . && return 0
  done <<<"$ISSUE_HOOK_DIRS"
  return 1
}
rel() { printf '%s' "${1#"$root"/}"; }
count_done_boxes() { grep -cE '^\s*- \[x\]' 2>/dev/null || true; }
count_headings() { grep -cE '^#+ .*(進捗|結果|残タスク|残課題|対応|todo|TODO)' 2>/dev/null || true; }

findings=""
add() { findings="${findings}${findings:+$'\n'}$1"; }

for n in $nums; do
  f=$(issue_file "$n") || continue
  r=$(rel "$f")
  if ! is_changed "$r"; then
    add "- $r: このセッションで 1 度も変更されていない (todolist / 進捗 / 結果 / 残タスクの追記が無い)"
  else
    old=$(git -C "$root" show "$base:$r" 2>/dev/null || true)
    if [ -n "$old" ]; then
      ob=$(count_done_boxes <<<"$old"); nb=$(count_done_boxes <"$f")
      oh=$(count_headings <<<"$old"); nh=$(count_headings <"$f")
      if [ "${nb:-0}" -le "${ob:-0}" ] && [ "${nh:-0}" -le "${oh:-0}" ]; then
        add "- $r: 変更はあるが、完了チェック ([x]) も進捗 / 結果 / 残タスクの見出しも増えていない"
      fi
    fi
  fi
  # 関連: この番号を参照する open issue (done/ pending/ next/ の外) で未変更のもの
  while IFS= read -r d; do
    [ -d "$d" ] || continue
    while IFS= read -r other; do
      [ -n "$other" ] || continue
      [ "$other" = "$f" ] && continue
      case "$other" in */done/*|*/pending/*|*/next/*) continue ;; esac
      ro=$(rel "$other")
      is_changed "$ro" && continue
      add "- $ro: issue $n を参照している open issue だが未変更 (残課題として指しているなら「$n で解消 / 継続」を 1 行追記する)"
    done < <(grep -rlwE "$n" "$d" --include='*.md' 2>/dev/null)
  done <<<"$ISSUE_HOOK_DIRS"
done
[ -n "$findings" ] || exit 0

# 同じ指摘は 1 セッション 1 回 (block で Claude が対応した後の Stop で再指摘しない。
# 指摘の集合が変わったら (別の issue に関わった等) 改めて出す)
sig=$(printf '%s' "$findings" | cksum | cut -d' ' -f1)
marker="$state_dir/$session_id.reported"
[ -f "$marker" ] && grep -qxF "$sig" "$marker" && exit 0
printf '%s\n' "$sig" >>"$marker"

reason="関わった issue の更新漏れの疑い (Stop hook issue-progress-check)。各行を確認し、必要なら todolist / 進捗 / 結果 / 残タスクを追記して commit する。更新不要ならその理由を 1 行で述べて終える:
$findings"
if command -v jq >/dev/null 2>&1; then
  jq -n --arg r "$reason" '{decision: "block", reason: $r}'
else
  printf '{"decision":"block","reason":%s}' "\"$(printf '%s' "$reason" | sed 's/["\\]/\\&/g; s/$/\\n/' | tr -d '\n')\""
fi
exit 0
