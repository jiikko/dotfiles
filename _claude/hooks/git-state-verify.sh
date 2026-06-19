#!/usr/bin/env bash
#
# PostToolUse(Bash) フック: git commit / push の直後に「実際の git state」を
# モデルのコンテキストへ注入する。
#
# なぜ: コミット/プッシュ成功を、ヘルパー関数や heredoc の出力（壊れていても
# 「成功」と表示されうる）ではなく ground truth で検証させるため。誤った成功報告を
# 構造で潰す。出典: ~/.claude/CLAUDE.md「Git 禁止操作」/ insights 2026-06-20。
#
# 入力: PostToolUse の hook JSON を stdin で受け取る (.tool_input.command を見る)
# 出力: git commit/push のときだけ hookSpecificOutput.additionalContext を emit。
#       それ以外のコマンドでは何も出さず exit 0（全 Bash 呼び出しで安全に no-op）。

input=$(cat)

# jq が無い環境では静かに諦める（誤動作させない）
command -v jq >/dev/null 2>&1 || exit 0

cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // ""')

# git commit / git push を含むコマンドのときだけ作動する。
# `git -C dir commit` や `... && git push` のような形も拾う。
printf '%s' "$cmd" | grep -Eq 'git([[:space:]]+-[^[:space:]]+)*[[:space:]]+(commit|push)' || exit 0

# git state を収集する。各コマンドは失敗しても全体を止めない（|| true）。
state=$(
  {
    branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)
    [ -n "$branch" ] && printf 'branch: %s\n' "$branch"
    printf -- '--- git status -sb ---\n'
    git status -sb 2>&1 | head -30 || true
    printf -- '--- last commit (git log -1 --stat) ---\n'
    git log -1 --stat 2>&1 | head -40 || true
    printf -- '--- unpushed commits (local not on any remote) ---\n'
    unpushed=$(git log --branches --not --remotes --oneline 2>/dev/null | head -20 || true)
    if [ -n "$unpushed" ]; then
      printf '%s\n' "$unpushed"
    else
      printf '(none — すべて push 済み)\n'
    fi
  } 2>&1
)

# 何も取れなければ（git リポジトリ外など）注入しない
[ -n "$state" ] || exit 0

jq -n --arg ctx "$state" '{
  hookSpecificOutput: {
    hookEventName: "PostToolUse",
    additionalContext: ("git commit/push 直後の実 git state（成功報告の前にこれで検証すること）:\n" + $ctx)
  },
  suppressOutput: true
}'
