#!/usr/bin/env bash
# _claude/hooks/ratelimit-warn.sh (UserPromptSubmit: 5h 枠が閾値を超えていたら 1 行注入する) の unit テスト。
#
# 見るのは mod `_claude/mods/ratelimit-warn` (issue 658) との分担: mod が判定できたプロンプトでは、env の印
# `DOTFILES_MOD_RATELIMIT_WARN=<session_id>:<epoch ms>` で hook が黙る。印の判定が壊れると、二重に注入する (緩い側) か、
# mod が落ちた後も hook が黙り続ける (危険な側) のどちらかになる。両腕を固定する。
# bin/ratelimit は HOME を偽の dir にして差し替える (hook は $HOME/dotfiles/bin/ratelimit を絶対パスで呼ぶ)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/ratelimit-warn.sh"
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook の印の判定は jq 前提なので検査できない" >&2; exit 1; }

fail=0; ok=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT
FAKE_HOME="$TMP_ROOT/home"; mkdir -p "$FAKE_HOME/dotfiles/bin"
cat > "$FAKE_HOME/dotfiles/bin/ratelimit" <<'FAKE'
#!/bin/sh
printf 'claude 5h 90%% (22:00 にリセット)\n'
exit 1
FAKE
chmod +x "$FAKE_HOME/dotfiles/bin/ratelimit"

# run <印 (空なら unset)> → stdout を $OUT に
run() {
  local mark="$1"
  if [ -n "$mark" ]; then
    OUT=$(printf '{"session_id":"sess-A","prompt":"x"}' | HOME="$FAKE_HOME" DOTFILES_MOD_RATELIMIT_WARN="$mark" bash "$HOOK")
  else
    OUT=$(printf '{"session_id":"sess-A","prompt":"x"}' | HOME="$FAKE_HOME" env -u DOTFILES_MOD_RATELIMIT_WARN bash "$HOOK")
  fi
}
expect_warn() { # $1=説明
  if grep -q '5h 枠が閾値を超えている' <<<"$OUT"; then ok=$((ok+1)); echo "✓ $1 → 注入"; else fail=$((fail+1)); echo "✗ $1 → 注入されない: [$OUT]"; fi
}
expect_silent() {
  if [ -z "$OUT" ]; then ok=$((ok+1)); echo "✓ $1 → 無言"; else fail=$((fail+1)); echo "✗ $1 → 注入された: [$OUT]"; fi
}

now_ms=$(( $(date +%s) * 1000 ))
run ""; expect_warn "印なし"
run "sess-A:$now_ms"; expect_silent "同じ session の今の印"
run "sess-A:$(( now_ms - 5000 ))"; expect_silent "同じ session の 5 秒前の印"
run "sess-A:$(( now_ms - 9000 ))"; expect_silent "同じ session の 9 秒前の印 (窓の内側)"
run "sess-A:$(( now_ms - 11000 ))"; expect_warn "同じ session の 11 秒前の印 (窓の外側)"
run "sess-A:$(( now_ms - 20000 ))"; expect_warn "同じ session の 20 秒前の印"
run "sess-B:$now_ms"; expect_warn "別 session の印"
run "sess-A:$(( now_ms - 60000 ))"; expect_warn "同じ session の 60 秒前の印 (mod が止まった後)"
run "sess-A:garbage"; expect_warn "時刻が読めない印"
run "sess-A:anything:$now_ms"; expect_warn "要素が 3 つある印 (形が違う)"
run "1"; expect_warn "永続の印 (旧形式)"

echo "ok=$ok fail=$fail"
[ "$fail" -eq 0 ]
