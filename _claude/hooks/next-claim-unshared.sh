#!/usr/bin/env bash
#
# UserPromptSubmit フック: `issues/next/` または `issues/epic/<name>/next/` への claim が
# **他マシンから見えない状態**
# (未コミット、または commit 済みだが未 push) なら、その事実をモデルのコンテキストへ注入し、
# 「push してよいか」をユーザーへ伺わせる。
#
# なぜ: claim は push されて初めて他マシンから見える (規範:
# _claude/rules/claim-issue-in-next-and-push.md)。Claude 自身が Bash で移した場合は
# next-claim-push.sh (PostToolUse) が拾うが、**人が glogx の issues viewer で `n` を押した
# 移動は Go 側の rename なので Bash を通らず、どの hook にも見えない**。その claim は
# 誰も push しないまま寝てしまい、他マシンから見て「未着手の issue」に見え続ける。
# issue 187 は viewer 側に push 導線を足す案だったが、push はブランチ単位で他の未 push
# commit も飛ぶため、**押した瞬間に機械が push するのは危ない**。人に伺う形にした。
#
# 🚨 **commit 済みだが未 push の claim も拾う** (issue 249 の項目 4)。以前は未コミットだけを
# 見ており、「claim を commit したが push を忘れた」窓が誰にも見えなかった。claim の意味は
# 「他マシンから見えること」なので、コミットしただけでは何も宣言していない。
# git-state-verify.sh も未 push commit を出すが、あちらは **git commit/push を打った直後**
# だけで、寝てしまった claim には届かない (こちらは毎プロンプト見る)。
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

# 適用範囲は「作業中の repo に global または group 内の next/ が 1 つでも実在するとき」だけ
# (規範と同じ opt-in)
repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
# 🚨 入れ子の issue dir (`<root>/*/issues`) も見る (issue 276)。obaket は macOS/issues/ を
# 正式に持っており、そこの next/ の claim がどの hook にも見えなかった。深さは 1 段に限る。
#
# 🚨 見つけた next/ の**相対パスを集めて、下の突き合わせにそのまま使う** (敵対的レビュー
# 2026-09-06)。以前は「ゲートは深さ 1 段 / 突き合わせの grep は深さ無制限」という非対称で、
# Next.js の `web/pages/issues/next/index.tsx` のような**claim と無関係なパス**を
# 「未共有の claim」として毎プロンプト誤報していた (実測。親コミットでは無音だった)。
# 実在する dir だけを見れば非対称は消える — claim があるならその dir は必ず実在する。
next_alt=""
for next_dir in "$repo_root/issues/next" "$repo_root/issues/epic"/*/next \
  "$repo_root"/*/issues/next "$repo_root"/*/issues/epic/*/next; do
  [ -d "$next_dir" ] || continue
  # 依存ディレクトリ配下は無視する (lib/issue-hooks.sh の除外と同じ理由)
  case "${next_dir#"$repo_root"/}" in
    node_modules/* | vendor/* | Pods/* | Carthage/* | .build/* | target/*) continue ;;
  esac
  # 正規表現メタ文字を落としてから alternation へ足す
  esc=$(printf '%s/' "${next_dir#"$repo_root"/}" | sed 's/[][\.*^$+?(){}|]/\\&/g')
  next_alt="${next_alt}${next_alt:+|}${esc}"
done
[ -n "$next_alt" ] || exit 0

# claim の移動は 3 つの形で現れる:
#   git mv した     → `R  issues/x.md -> issues/next/x.md`
#   Go/素の mv した → ` D issues/x.md` + `?? issues/next/x.md`
#   claim を外した  → ` D issues/next/x.md` + `?? issues/x.md`
# いずれも **global または group 内の next/ を含む行**が出るので、それを拾えば 3 形とも捕まる。
#
# 🚨 pathspec で絞らないこと (issue 276)。`*/issues/next` のような入れ子ぶんを pathspec へ
# 足すと、**そのパスが存在しない repo では git が pathspec エラーで空を返す**ので、
# root 直下の claim まで丸ごと見えなくなる (実測: それで 4 ケースが無出力になった)。
# 絞り込みは下の grep が担う。**アンカーを付けないこと** — porcelain は `?? issues/next/` の
# ように空白が前に来るので `(^|/)` を足すと 1 件も拾わなくなる (これも実測で踏んだ)。
# 無アンカーなら入れ子 (`macOS/issues/next/`) もそのまま拾える。
claim_lines=$(
  cd "$repo_root" || exit 0
  git status --porcelain 2>/dev/null |
    grep -E "(^|[ >\"])(${next_alt})" || true
)

# commit 済みだが未 push の claim。**リモートに無い commit のうち global または group 内の next/ を触ったもの**を見る
# (`--branches --not --remotes` は未 push の commit 全部。そこから claim を触るものだけ絞る)
unpushed_claims=$(
  cd "$repo_root" || exit 0
  # 🚨 commit ヘッダの目印に SOH (%x01) を置くこと (敵対的レビュー 2026-09-06)。
  # `%h %s` だけだと awk の `/^[0-9a-f]+ /` が**ファイル名と区別できず**、
  # `face detection.py` のような 16 進文字だけの語で始まる名前がヘッダとして採用され、
  # 「どの commit を push すればいいか」が消えていた (実測。親コミットは正しく出していた)。
  git log --branches --not --remotes --format='%x01%h %s' \
    --name-only 2>/dev/null |
    awk -v soh=$'\001' -v pat="^(${next_alt})" '
      substr($0,1,1)==soh { h=substr($0,2); next }
      $0 ~ pat { if (h!="") { print h; h="" } }
    ' |
    head -10 || true
)

[ -n "$claim_lines" ] || [ -n "$unpushed_claims" ] || exit 0

state=$(
  {
    if [ -n "$claim_lines" ]; then
      printf -- '--- 未コミットの claim ---\n'
      printf '%s\n' "$claim_lines"
    fi
    if [ -n "$unpushed_claims" ]; then
      printf -- '--- commit 済みだが未 push の claim ---\n'
      printf '%s\n' "$unpushed_claims"
    fi
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
      "issues/next/ または issues/epic/<name>/next/ への claim が**他マシンから見えない状態**で残っている (未コミット、または\n" +
      "commit 済みだが未 push)。claim は push されて初めて claim になる — commit しただけでは\n" +
      "他マシンからは「未着手の issue」に見え続ける。人が glogx の `n` で付けた目印は\n" +
      "どの hook にも見えないので、ここで拾っている。\n" +
      "ユーザーへ次を伺うこと (勝手に commit / push しない):\n" +
      "  - この claim を commit して push してよいか (移動の旧パスと新パスだけを pathspec に書く)\n" +
      "  - 未 push の commit が他にあるなら、**それも一緒に飛ぶ**ことを伝えてから聞く\n" +
      "    (push はブランチ単位なので「claim だけ push」はできない)\n" +
      "一度「しない」と答えられたら、このセッションでは再度聞かないこと。\n\n" + $ctx
    )
  },
  suppressOutput: true
}'
