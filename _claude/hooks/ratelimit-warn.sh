#!/bin/bash
# UserPromptSubmit: Claude の 5h 枠が閾値を超えていたら、1 行だけ注入する。
# weekly は見ない (`ratelimit -check` が 5h だけを判定する。理由は src/ratelimit/main.go 冒頭)。
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
#   src/ratelimit (と replace 先) がビルドできない間は、毎プロンプト最大 timeout (15 秒) 待つ。直すなら初回分岐に失敗記録を持たせる
# - mod `_claude/mods/ratelimit-warn` (issue 658) が同じ注入をする。mod はこのプロンプトを判定できたときだけ、
#   env DOTFILES_MOD_RATELIMIT_WARN に `<session_id>:<epoch ms>` の印を打つ (settings の hook は mod の後に走る)。
#   印が「この session で、10 秒以内」のときだけ黙る。永続の印にしないのは、mod が後で落ちた・classic.* が
#   素通しになった・子の claude -p に継承された、のどれでもこの fallback まで黙らないため。
#   印が読めない (jq が無い・形が違う) ときは黙らず、今までどおり判定する
if [ -n "${DOTFILES_MOD_RATELIMIT_WARN:-}" ] && command -v jq >/dev/null 2>&1; then
  mark_sid=${DOTFILES_MOD_RATELIMIT_WARN%%:*}
  mark_ms=${DOTFILES_MOD_RATELIMIT_WARN##*:}
  sid=$(jq -r '.session_id // empty' 2>/dev/null || true)
  # 形は `<session_id>:<epoch ms>` の 2 要素だけ。それ以外 (要素が多い・時刻が数字でない) は印として読まない
  case "$DOTFILES_MOD_RATELIMIT_WARN" in *:*:*) mark_ms= ;; esac
  case "$mark_ms" in ''|*[!0-9]*) mark_ms= ;; esac
  if [ -n "$sid" ] && [ -n "$mark_ms" ] && [ "$sid" = "$mark_sid" ]; then
    delta=$(( $(date +%s) * 1000 - mark_ms ))
    if [ "$delta" -ge -10000 ] && [ "$delta" -le 10000 ]; then
      exit 0
    fi
  fi
fi
bin="$HOME/dotfiles/bin/ratelimit"
[ -x "$bin" ] || exit 0
out=$("$bin" -source claude -check -cached)
rc=$?
if [ "$rc" -eq 1 ] && [ -n "$out" ]; then
  printf '🚨 Claude の 5h 枠が閾値を超えている:\n%s\n大きな作業に入る前に、控える・縮小する・リセット後に回す案をユーザーへ提案すること (基準は subagent-model-tiering.md の「枠の残量」)。\n' "$out"
fi
exit 0
