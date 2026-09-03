#!/usr/bin/env bash
#
# UserPromptSubmit フック: `issues/next/` への claim が **未コミットのまま**残っていたら、
# その事実をモデルのコンテキストへ注入し、「push してよいか」をユーザーへ伺わせる。
#
# なぜ: claim は push されて初めて他マシンから見える (規範:
# _claude/rules/claim-issue-in-next-and-push.md)。Claude 自身が Bash で移した場合は
# next-claim-push.sh (PostToolUse) が拾うが、**人が glogx の issues viewer で `n` を押した
# 移動は Go 側の rename なので Bash を通らず、どの hook にも見えない**。その claim は
# 誰も push しないまま寝てしまい、他マシンから見て「未着手の issue」に見え続ける。
# issue 187 は viewer 側に push 導線を足す案だったが、push はブランチ単位で他の未 push
# commit も飛ぶため、**押した瞬間に機械が push するのは危ない**。人に伺う形にした。
#
# 入力: UserPromptSubmit の hook JSON を stdin (中身は見ない。毎プロンプトで状態だけ見る)
# 出力: 未コミットの claim があるときだけ additionalContext を emit。無ければ無出力 exit 0。
#
# 🚨 判定は working tree の状態なので、Claude が移したのか人が移したのかは区別しない
#    (区別する必要が無い: どちらでも「未コミットの claim」は同じ事故に繋がる)。
# 🚨 本文は必ず jq --arg で組む。heredoc に状態文字列を直接埋めると実改行が JSON の文字列に
#    入り「control characters must be escaped」で壊れる (next-claim-push.sh の実測 2026-09-02)。
# 🚨 commit されるまで毎プロンプト出る。ユーザーが「今はしない」と答えたら、そのセッションでは
#    再度聞かないこと (注入は繰り返し出るが、答えは会話に残っている)。

input=$(cat)
: "$input"   # 中身は使わない (状態だけ見る)

command -v jq >/dev/null 2>&1 || exit 0

# 適用範囲は「作業中の repo に issues/next/ が実在するとき」だけ (規範と同じ opt-in)
repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
[ -n "$repo_root" ] && [ -d "$repo_root/issues/next" ] || exit 0

# claim の移動は 3 つの形で現れる:
#   git mv した     → `R  issues/x.md -> issues/next/x.md`
#   Go/素の mv した → ` D issues/x.md` + `?? issues/next/x.md`
#   claim を外した  → ` D issues/next/x.md` + `?? issues/x.md`
# いずれも **issues/next/ を含む行**が出るので、それを拾えば 3 形とも捕まる
claim_lines=$(git -C "$repo_root" status --porcelain -- issues 2>/dev/null | grep 'issues/next/' || true)
[ -n "$claim_lines" ] || exit 0

state=$(
  {
    printf -- '--- 未コミットの claim ---\n'
    printf '%s\n' "$claim_lines"
    printf -- '--- git status -sb (他の変更・未 push commit が混ざっていないか) ---\n'
    git -C "$repo_root" status -sb 2>/dev/null | head -20 || true
    printf -- '--- 未 push の commit (push すると一緒に飛ぶ) ---\n'
    unpushed=$(git -C "$repo_root" log --branches --not --remotes --format='%h %s' 2>/dev/null | head -10)
    if [ -n "$unpushed" ]; then printf '%s\n' "$unpushed"; else printf '(なし)\n'; fi
  } 2>/dev/null
)
[ -n "$state" ] || exit 0

jq -n --arg ctx "$state" '{
  hookSpecificOutput: {
    hookEventName: "UserPromptSubmit",
    additionalContext: (
      "issues/next/ への claim が**未コミット**のまま残っている (人が glogx の `n` で付けた目印は\n" +
      "どの hook にも見えないので、ここで拾っている)。claim は push されて初めて他マシンから見える。\n" +
      "ユーザーへ次を伺うこと (勝手に commit / push しない):\n" +
      "  - この claim を commit して push してよいか (移動の旧パスと新パスだけを pathspec に書く)\n" +
      "  - 未 push の commit が他にあるなら、**それも一緒に飛ぶ**ことを伝えてから聞く\n" +
      "    (push はブランチ単位なので「claim だけ push」はできない)\n" +
      "一度「しない」と答えられたら、このセッションでは再度聞かないこと。\n\n" + $ctx
    )
  },
  suppressOutput: true
}'
