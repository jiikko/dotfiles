#!/bin/bash
# UserPromptSubmit: Claude の利用枠 (5h / weekly) が閾値を超えていたら、1 行だけ注入する。
# 何を「大きな作業」とみなして控えるかは _claude/rules/subagent-model-tiering.md が持つ。
#
# - キャッシュだけを読む (-cached)。古ければ bin/ratelimit が裏で取り直し、この回は手元の値で答える
#   (プロンプトを待たせない)
# - 超過なし・判定不能は無言。判定不能を警告にしないのは、この hook が助言であって
#   ゲートではないから (取れないときに毎プロンプト騒ぐ方が害が大きい)
# - codex の枠は見ない。codex 系 skill が起動時に `ratelimit -source codex -check` で見る
# - rc は常に 0。UserPromptSubmit の rc=2 はプロンプトを止めてしまう
# - 🚨 既知の穴: ビルド済みのバイナリが無い初回 (新しいマシン / git clean の後) は、bin/ratelimit が
#   --async でも同期でビルドし、失敗しても backoff が無い (bin/lib/go_autobuild.zsh の初回分岐)。
#   glogx がビルドできない間は、毎プロンプト最大 timeout (15 秒) 待つ。直すなら初回分岐に失敗記録を持たせる
bin="$HOME/dotfiles/bin/ratelimit"
[ -x "$bin" ] || exit 0
out=$("$bin" -source claude -check -cached)
rc=$?
if [ "$rc" -eq 1 ] && [ -n "$out" ]; then
  printf '🚨 Claude の利用枠が閾値を超えている:\n%s\n大きな作業に入る前に、控える・縮小する・リセット後に回す案をユーザーへ提案すること (基準は subagent-model-tiering.md の「枠の残量」)。\n' "$out"
fi
exit 0
