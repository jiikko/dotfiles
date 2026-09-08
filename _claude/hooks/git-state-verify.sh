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
    # 🚨 **HEAD を明示する** (issue 311)。`--branches` はローカルブランチの先端しか見ないので
    # **detached HEAD の commit を 1 件も返さない**。この repo は worktree-per-session.md で
    # detached worktree を規範として要求しているため、規範どおりに運用すると
    # 「(none — すべて push 済み)」という**積極的な偽の全クリア**を注入していた。
    # 正解実装は同じ repo にある (`src/glogx/gitlog.go` の `UnpushedSHAs` は `HEAD --not --remotes`)。
    # `--all` は stash と他 worktree の commit まで拾うので採らない。
    #
    # 🚨 **判定不能を「push 済み」に丸めない**。remote 未設定なら「どこにも push していない」が
    # 正しく、それを「すべて push 済み」と出すのは逆向きの嘘になる。
    if ! git rev-parse --git-dir >/dev/null 2>&1; then
      printf '(判定不能: git リポジトリではない)\n'
    elif [ -z "$(git remote 2>/dev/null)" ]; then
      printf '(判定不能: remote が設定されていない — push 先が無いので「push 済み」とは言えない)\n'
    else
      unpushed=$(git log HEAD --branches --not --remotes --oneline 2>/dev/null | head -20 || true)
      if [ -n "$unpushed" ]; then
        printf '%s\n' "$unpushed"
      else
        printf '(none — すべて push 済み)\n'
      fi
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
