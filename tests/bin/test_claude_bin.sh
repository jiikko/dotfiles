#!/usr/bin/env bash
# bin/lib/claude_bin.sh の resolve_claude が、PATH の先頭にある「動かない claude」(版管理の shim) を飛ばして、
# 実際に動く実体を選ぶことを固定する (issue 639)。
#
# なぜ: nodenv の shim は、どれかの版に claude が入っているだけで PATH の先頭に現れ、今の版に無ければ実行時に
# rc=127 で終わる。`command -v claude` はそれを選び、tests/claude/test_claude_mods.sh が毎回落ちていた。
# 本物の claude には触らない (PATH を偽の dir と /usr/bin:/bin だけにする)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LIB="$ROOT_DIR/bin/lib/claude_bin.sh"
fails=0
ok()   { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/claude-bin.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

SYS_PATH="/usr/bin:/bin"
# 前提: 偽の dir 以外から claude が見つかると、どのケースも判定できない
if PATH="$SYS_PATH" type -P claude >/dev/null 2>&1; then
  echo "✗ 前提: $SYS_PATH に claude がある (偽の PATH が組めない)" >&2
  exit 1
fi

mkdir -p "$WORK/shim" "$WORK/real" "$WORK/eater" "$WORK/counted" "$WORK/hang"
# 版管理の shim: 今の版に無いと言って 127 で終わる
cat > "$WORK/shim/claude" <<'EOF'
#!/bin/sh
echo "nodenv: claude: command not found" >&2
exit 127
EOF
cat > "$WORK/real/claude" <<'EOF'
#!/bin/sh
[ "$1" = "--version" ] && { echo "9.9.9 (Claude Code)"; exit 0; }
exit 2
EOF
# stdin を読み切ってから失敗する候補: 候補の一覧を食うと、後ろの候補が試されずに消える (レビューで再現した形)
cat > "$WORK/eater/claude" <<'EOF'
#!/bin/sh
cat >/dev/null
exit 127
EOF
# 起動した回数を数える、動かない候補 (PATH の重複で 2 回起動しないかを見る)
cat > "$WORK/counted/claude" <<EOF
#!/bin/sh
echo x >> "$WORK/count"
exit 127
EOF
# --version が終わらない候補: 自分の pid を残してから止まり続ける (打ち切り後に残っていないかを pid で見る)
# sleep-ok: stub: 終わらない候補そのもの。resolve_claude が上限で kill する対象 (待つための sleep ではない)
cat > "$WORK/hang/claude" <<EOF
#!/bin/sh
echo \$\$ > "$WORK/hang.pid"
exec sleep 30
EOF
chmod +x "$WORK/shim/claude" "$WORK/real/claude" "$WORK/eater/claude" "$WORK/counted/claude" "$WORK/hang/claude"

# run <PATH>: resolve_claude を偽の PATH で 1 回呼ぶ。stdout / stderr / rc を別々に $WORK/out|err|rc に置く
run() {
  local rc=0
  # shellcheck source=bin/lib/claude_bin.sh
  (PATH="$1"; source "$LIB"; resolve_claude) > "$WORK/out" 2> "$WORK/err" || rc=$?
  echo "$rc" > "$WORK/rc"
}

run "$WORK/shim:$WORK/real:$SYS_PATH"
if [ "$(cat "$WORK/rc")" = 0 ] && [ "$(cat "$WORK/out")" = "$WORK/real/claude" ]; then
  ok "shim が先にあっても、動く実体を選ぶ"
else
  fail "shim が先: rc=$(cat "$WORK/rc") out=$(cat "$WORK/out") (期待 rc=0 out=$WORK/real/claude)"
fi

run "$WORK/real:$WORK/shim:$SYS_PATH"
if [ "$(cat "$WORK/rc")" = 0 ] && [ "$(cat "$WORK/out")" = "$WORK/real/claude" ]; then
  ok "動く実体が先にあれば、それを選ぶ"
else
  fail "実体が先: rc=$(cat "$WORK/rc") out=$(cat "$WORK/out")"
fi

run "$WORK/shim:$SYS_PATH"
if [ "$(cat "$WORK/rc")" = 2 ] && [ ! -s "$WORK/out" ] && grep -qF "$WORK/shim/claude" "$WORK/err"; then
  ok "shim しか無ければ rc=2 (在るが動かない) で、試した候補を stderr に出す"
else
  fail "shim だけ: rc=$(cat "$WORK/rc") out=$(cat "$WORK/out") err=$(cat "$WORK/err")"
fi

run "$SYS_PATH"
if [ "$(cat "$WORK/rc")" = 1 ] && [ ! -s "$WORK/out" ] && [ ! -s "$WORK/err" ]; then
  ok "claude が無ければ rc=1 で何も出さない"
else
  fail "claude 無し: rc=$(cat "$WORK/rc") out=$(cat "$WORK/out") err=$(cat "$WORK/err")"
fi

run "$WORK/eater:$WORK/real:$SYS_PATH"
if [ "$(cat "$WORK/rc")" = 0 ] && [ "$(cat "$WORK/out")" = "$WORK/real/claude" ]; then
  ok "stdin を読む候補が先にあっても、後ろの実体まで試す"
else
  fail "stdin を読む候補が先: rc=$(cat "$WORK/rc") out=$(cat "$WORK/out") err=$(cat "$WORK/err")"
fi

rm -f "$WORK/count"
run "$WORK/counted:$WORK/counted:$WORK/real:$SYS_PATH"
count="$(wc -l < "$WORK/count" 2>/dev/null | tr -d ' ' || true)"
if [ "$(cat "$WORK/rc")" = 0 ] && [ "$count" = 1 ]; then
  ok "PATH の重複で同じ候補を 2 回起動しない"
else
  fail "重複: rc=$(cat "$WORK/rc") 起動回数=$count (期待 1)"
fi

# 上限は 1 秒にして速く回す (既定の 10 秒は待たない)
export CLAUDE_BIN_VERSION_TIMEOUT=1
rm -f "$WORK/hang.pid"
run "$WORK/hang:$WORK/real:$SYS_PATH"
hang_pid="$(cat "$WORK/hang.pid" 2>/dev/null || true)"
if [ "$(cat "$WORK/rc")" = 0 ] && [ "$(cat "$WORK/out")" = "$WORK/real/claude" ]; then
  ok "応答しない候補が先にあっても、上限で打ち切って後ろの実体を選ぶ"
else
  fail "応答しない候補が先: rc=$(cat "$WORK/rc") out=$(cat "$WORK/out") err=$(cat "$WORK/err")"
fi
if [ -n "$hang_pid" ] && ! ps -p "$hang_pid" >/dev/null 2>&1; then
  ok "打ち切った候補のプロセスが残らない"
else
  fail "打ち切った候補: pid=$hang_pid が残っている (または pid を読めない)"
  [ -n "$hang_pid" ] && kill -9 "$hang_pid" 2>/dev/null || true
fi

run "$WORK/hang:$SYS_PATH"
if [ "$(cat "$WORK/rc")" = 2 ] && [ ! -s "$WORK/out" ] && grep -qF "$WORK/hang/claude (応答なし)" "$WORK/err"; then
  ok "応答しない候補しか無ければ rc=2 で、応答なしと出す"
else
  fail "応答しない候補だけ: rc=$(cat "$WORK/rc") out=$(cat "$WORK/out") err=$(cat "$WORK/err")"
fi
unset CLAUDE_BIN_VERSION_TIMEOUT

if [ "$fails" -gt 0 ]; then
  echo "✗ $fails 件失敗" >&2
  exit 1
fi
echo "✓ resolve_claude: 9 件"
