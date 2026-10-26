#!/usr/bin/env bash
# _claude/hooks/suggest-dupscan-for-go-edit.sh (PreToolUse(Write / Edit): src/ の下の .go を書くときに dupscan を案内する) の unit テスト。
#
# なぜ: 案内が出なくなる退行は無音 (書き込みはそのまま走る)。逆に出すぎる退行は毎回の Edit に 1 行ずつ文脈を足す。どちらもテストでしか見えない。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/suggest-dupscan-for-go-edit.sh"
# 本番の配線と同じ上限 (timeout に殺されると無出力 = 案内が消える)
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT
export TMPDIR="$TMP_ROOT"   # 出しすぎ防止の状態ファイルをテストの中に閉じる

# dupscan の代わり (hook は PATH に在るかだけを見る)。本物を起こさない
FAKE_BIN="$TMP_ROOT/bin"; mkdir -p "$FAKE_BIN"
printf '#!/bin/sh\nexit 0\n' > "$FAKE_BIN/dupscan"; chmod +x "$FAKE_BIN/dupscan"
WITH_DUPSCAN="$FAKE_BIN:$PATH"
# dupscan の無い PATH: jq と git だけを置く
NO_DUPSCAN_BIN="$TMP_ROOT/nodup"; mkdir -p "$NO_DUPSCAN_BIN"
for t in bash jq git dirname cat; do ln -s "$(command -v "$t")" "$NO_DUPSCAN_BIN/$t"; done

REPO=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
( cd "$REPO" && git init -q . && mkdir -p src/foo other && printf 'package foo\n' > src/foo/a.go && git add -A ) >/dev/null 2>&1
session=0
next_session() { session=$((session + 1)); }

# 🚨 rc と stderr を捨てない: PreToolUse の hook は exit 2 で書き込みを止め、stderr はそのまま Claude に見える。
# 案内だけの hook は、どの入力でも rc=0・stderr は空でなければならない (外れたら stdout の代わりに印を返して落とす)
run() { # $1=tool $2=file_path [$3=PATH] → stdout
  local rc
  jq -n --arg t "$1" --arg f "$2" --arg s "s$session" '{tool_name: $t, session_id: $s, cwd: "/", tool_input: {file_path: $f}}' |
    PATH="${3:-$WITH_DUPSCAN}" "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  rc=$?
  if [ "$rc" -ne 0 ] || [ -s "$TMP_ROOT/err" ]; then
    printf 'HOOK-FAILED rc=%s stderr=%s\n' "$rc" "$(head -c 200 "$TMP_ROOT/err")"
    return 0
  fi
  cat "$TMP_ROOT/out"
}

expect_nudge() { # $1=説明 $2=stdout
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if ! printf '%s' "$2" | jq -e '.hookSpecificOutput.additionalContext | contains("dupscan index") and contains("dupscan gate")' >/dev/null 2>&1; then
    echo "✗ 案内が出ない / JSON が壊れている: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return
  fi
  # 🚨 止めない: permissionDecision を返したら書き込みが止まる
  if printf '%s' "$2" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ permissionDecision を返している (案内だけのはず): $1"; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_silent() { # $1=説明 $2=stdout
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if [ -n "$2" ]; then echo "✗ 案内が出るべきでない: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

next_session; expect_nudge "src の下の既存の .go を Edit" "$(run Edit "$REPO/src/foo/a.go")"
expect_silent "同じ session の 2 回目は出さない" "$(run Edit "$REPO/src/foo/a.go")"
expect_silent "同じ session なら別のファイルでも出さない" "$(run Write "$REPO/src/foo/b.go")"
next_session; expect_nudge "まだ無い dir の新しい .go を Write (在る祖先で repo を引く)" "$(run Write "$REPO/src/bar/baz/new.go")"
next_session; expect_nudge "テストの .go も案内する (テストの helper も写される)" "$(run Edit "$REPO/src/foo/a_test.go")"
next_session; expect_silent "src の外の .go" "$(run Edit "$REPO/other/x.go")"
next_session; expect_silent "src で始まる別の dir (srcfoo) の .go" "$(run Write "$REPO/srcfoo/x.go")"
next_session; expect_silent "repo の root でない src (other/src) の .go" "$(run Write "$REPO/other/src/x.go")"
next_session; expect_silent "src の下でも .go でない" "$(run Edit "$REPO/src/foo/README.md")"
next_session; expect_silent "git の repo の外" "$(run Write "$TMP_ROOT/plain/src/x.go")"
next_session; expect_silent "dupscan が PATH に無い" "$(run Edit "$REPO/src/foo/a.go" "$NO_DUPSCAN_BIN")"
next_session; expect_silent "相対パス (Write / Edit は絶対パスを渡す。読めない形は黙る)" "$(run Edit "src/foo/a.go")"
# symlink を挟んだパス (macOS の /var → /private/var のような実パスのずれ) でも前方一致を外さない
ln -s "$REPO" "$TMP_ROOT/link"
next_session; expect_nudge "symlink を挟んだパス" "$(run Edit "$TMP_ROOT/link/src/foo/a.go")"

# src/ 自体がまだ無い repo で新しい .go を Write する (遡った先が repo の root になり、root からの残りのパスで判定する)
REPO2=$(mktemp -d "$TMP_ROOT/repo2.XXXXXX")
( cd "$REPO2" && git init -q . ) >/dev/null 2>&1
next_session; expect_nudge "src がまだ無い repo の新しい .go" "$(run Write "$REPO2/src/pkg/new.go")"
next_session; expect_silent "src がまだ無い repo の src 以外の新しい .go" "$(run Write "$REPO2/cmd/new.go")"

# 配線: settings.json の PreToolUse(Write|Edit) にこの hook が在る (消えると無音で効かなくなる)
if jq -e '[.hooks.PreToolUse[] | select(.matcher == "Write|Edit") | .hooks[].command] | index("~/dotfiles/_claude/hooks/suggest-dupscan-for-go-edit.sh")' \
  "$ROOT_DIR/_claude/settings.json" >/dev/null; then
  echo "✓ settings.json の PreToolUse(Write|Edit) に配線されている"; ok=$((ok+1))
else
  echo "✗ settings.json の PreToolUse(Write|Edit) に配線されていない"; fail=$((fail+1))
fi

echo "---"
echo "ok=$ok fail=$fail"
[ "$fail" -eq 0 ]
