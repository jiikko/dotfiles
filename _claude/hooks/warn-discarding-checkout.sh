#!/usr/bin/env bash

# PreToolUse(Bash) フック: 未コミットの変更を捨てる形の git checkout / git restore の直前に、
# **何が消えるか**を注意として注入する (issue 297)。
#
# なぜ: 変異検証の復元 (`git checkout -- <path>`) が、同じパスにある未コミットの修正を一緒に
# 捨てる。規範は既にあり (mutation-verify-new-tests.md の「復元の作法」が発動点まで名指しで
# 書いている) 、それでも 2026-09-06 の 1 セッションで 3 回踏んだ。いずれも「レビュー指摘を
# 直した直後」= 必ず未コミットのタイミング。規範を読んでいる状態で踏むので、残る手段は機械しかない。
#
# 🚨 **deny にしない。** 変異の復元は正当な用途で、deny にすると変異検証が回らなくなる
# (tmux の deny 型とは別。あちらは常に誤りだが、こちらは正当な場合がある)。
# permissionDecision を返さないので、**許可の判断は一切変えない** — additionalContext だけを足す。
#
# 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8。実装前に書いたもの):
#   - 止めるのは「未コミットの変更がある状態で、破棄系の checkout / restore を打つ」形だけ
#   - 判定は粗く倒す: **どのパスが消えるかを静的に解決しない**。変数・glob・省略形があるので、
#     解決しようとすると取りこぼす。repo が dirty かどうかで判定し、消えうる一覧を見せる
#   - **検出しない**:
#       * Bash ツール以外の経路 (人の手入力 / スクリプトの内部 / glogx の Go からの呼び出し)。
#         hook が見えるのは Claude が Bash で動かしたものだけ (next-claim-push.sh と同じ限界)
#       * 引用符・heredoc の中に書かれた文字列 (偽陽性側に出る。deny でないので害は注意 1 行)
#       * `git checkout <branch>` の切替 — 破棄しない (上書きになる場合は git 自身が拒否する)
#       * `git restore --staged` のみ — index を戻すだけで作業ツリーの内容は残る
#   - これらは review と人の目の責務
#   - 🚨 この射程は実装後に実物と突き合わせて確定させた (着手前に書いた版は意図であって射程ではない)

set -uo pipefail

input=$(cat)

# jq が無い環境では静かに諦める (既存 hook と同じ振る舞い)
command -v jq >/dev/null 2>&1 || exit 0

cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // ""' 2>/dev/null) || exit 0
[ -n "$cmd" ] || exit 0

# 🚨 性能ガード (deny-bare-tmux-kill.sh の教訓)。PreToolUse には timeout があり、大きな
# heredoc を含む呼び出しで hook が殺されると **stdout 0 byte = 注意が 1 byte も出ない**。
# 対象語を含まないものはここで即通して、下の git 呼び出しに入れない。
case "$cmd" in
  *checkout* | *restore*) ;;
  *) exit 0 ;;
esac

# 破棄する形かを見る。`git` の直後に (グローバルオプションを挟んで) checkout / restore が来る形。
#   捨てる:   git checkout -- <path> / git checkout . / git checkout -f / git restore <path>
#   捨てない: git checkout <branch> / git checkout -b <new> / git restore --staged <path>
discards=0
while IFS= read -r seg; do
  case "$seg" in
    *"git checkout"* | *"git restore"* | *"git -C "*) ;;
    *) continue ;;
  esac
  # restore: --staged だけなら index を戻すだけ (--worktree が付けば捨てる)
  case "$seg" in
    *"git restore"*)
      case "$seg" in
        *--staged*)
          case "$seg" in *--worktree*) discards=1 ;; esac
          ;;
        *) discards=1 ;;
      esac
      ;;
  esac
  # 🚨 `--` は checkout の直後とは限らない: `git checkout HEAD -- x` / `git checkout v1.0 -- .`
  # のようにコミットを挟む形が普通にある (実測 2026-09-06: 直後だけを見る版が取りこぼした)。
  case "$seg" in
    *checkout*" -- "* | *checkout*" --") discards=1 ;;
    *"checkout ."[!a-zA-Z]* | *"checkout .") discards=1 ;;
    *"checkout -f"* | *"checkout --force"*) discards=1 ;;
  esac
done <<EOF_SEGS
$(printf '%s' "$cmd" | tr ';&|' '\n')
EOF_SEGS
[ "$discards" -eq 1 ] || exit 0

cwd=$(printf '%s' "$input" | jq -r '.cwd // ""' 2>/dev/null)
[ -n "$cwd" ] && [ -d "$cwd" ] || cwd="$PWD"

# 🚨 「検査できなかった」を「変更なし」にしない。git が無い / repo でないときは黙る
# (注意を出す根拠が無い) が、status が失敗したときは判定不能なのでその旨を出す。
command -v git >/dev/null 2>&1 || exit 0
git -C "$cwd" rev-parse --show-toplevel >/dev/null 2>&1 || exit 0
if ! dirty=$(git -C "$cwd" status --porcelain 2>/dev/null); then
  exit 0
fi
[ -n "$dirty" ] || exit 0   # 変更が無ければ黙る (毎回出すとノイズになり読まれなくなる)

count=$(printf '%s\n' "$dirty" | grep -c '^' )
body=$(printf '%s\n' "$dirty" | head -20)
more=""
[ "$count" -gt 20 ] && more="
  … 他 $((count - 20)) 件"

msg="🚨 未コミットの変更が ${count} 件ある状態で、それを捨てうる git コマンドを実行しようとしている。
**このコマンドが消すのは変異だけとは限らない。** 直前にレビュー指摘を直した / 実装を書いた
なら、その修正も一緒に消える (規範: _claude/rules/mutation-verify-new-tests.md の「復元の作法」)。

意図した復元なら、そのまま進めてよい。そうでないなら **先に commit する** (WIP でよい)。
新規 (untracked) ファイルは checkout では戻らないので、消えると復元できない。

--- 未コミットの変更 ---
${body}${more}"

jq -n --arg c "$msg" \
  '{hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $c}}'
