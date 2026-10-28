#!/usr/bin/env bash
# _claude/hooks/inject-dupscan-candidates-on-new-func.sh (PreToolUse(Write / Edit): 新しい関数を足す直前に、名前の近い既存の定義を注入する) の unit テスト。
#
# なぜ: 注入が出なくなる退行は無音 (書き込みはそのまま走る)。出すぎる退行は毎回の Edit に雑音を足す。どちらもテストでしか見えない。
# dupscan は偽物 (index の行を名前で返す)。本物の索引の形は src/dupscan の index の text 出力に合わせてある。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/inject-dupscan-candidates-on-new-func.sh"
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い" >&2; exit 1; }

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT
export TMPDIR="$TMP_ROOT" XDG_CACHE_HOME="$TMP_ROOT/cache"

FAKE_BIN="$TMP_ROOT/bin"; mkdir -p "$FAKE_BIN"
cat > "$FAKE_BIN/dupscan" <<'FAKE'
#!/bin/sh
# 引数: index -name NAME ...  → NAME に応じた行を返す
while [ $# -gt 0 ]; do [ "$1" = "-name" ] && name="$2"; shift; done
echo "構文近似: 名前と引数ラベル/個数で照合。"
case "$name" in
  readCookies) echo "src/chromecookie/cookie.go:291-349 Workspace.ReadCookies(profile string) (Result, error) (呼び出し ~1) // doc" ;;
  loadSettings) echo "src/a/s.go:10-20 loadSettingsFile(p string) error (呼び出し ~2)"; echo "src/b/s.go:5-9 reloadSettings() (呼び出し ~0)" ;;
  TestReadCookies) echo "src/a/t_test.go:1-9 TestReadCookies(t *testing.T) (呼び出し ~0)" ;;
  read) echo "src/a/r.go:1-2 read() (呼び出し ~9)" ;;
  parseThing) echo "src/a/p.go:1-9 parse() (呼び出し ~0)" ;;
  collectWidgets) echo "src/a/c.go:1-9 collectWidgets(xs []int) []int (呼び出し ~3)" ;;
  gather) echo "src/a/g.go:1-2 gatherAll() (呼び出し ~0)" ;;
esac
# collectWidgetsFast / gatherThings は、どの語でも本物の index -name が返さない (既存の名前が引く語を含むものだけを返すため)
FAKE
chmod +x "$FAKE_BIN/dupscan"
WITH_DUPSCAN="$FAKE_BIN:$PATH"
NO_DUPSCAN_BIN="$TMP_ROOT/nodup"; mkdir -p "$NO_DUPSCAN_BIN"
for t in bash env python3 git jq dirname cat; do ln -s "$(command -v "$t")" "$NO_DUPSCAN_BIN/$t"; done

REPO=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
( cd "$REPO" && git init -q . && mkdir -p src/foo && printf 'package foo\n\nfunc existingHelper() {}\n' > src/foo/a.go && git add -A ) >/dev/null 2>&1
session=0
next_session() { session=$((session + 1)); }

# rc と stderr を捨てない: PreToolUse の hook は exit 2 で書き込みを止める。どの入力でも rc=0・stderr 空でなければならない。
run() { # $1=tool $2=file $3=new_string [$4=old_string] [$5=PATH]
  local rc
  jq -n --arg t "$1" --arg f "$2" --arg n "$3" --arg o "${4:-}" --arg s "s$session" \
    '{tool_name: $t, session_id: $s, tool_input: ({file_path: $f} + (if $t == "Edit" then {new_string: $n, old_string: $o} else {content: $n} end))}' |
    PATH="${5:-$WITH_DUPSCAN}" "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  rc=$?
  if [ "$rc" -ne 0 ] || [ -s "$TMP_ROOT/err" ]; then
    printf 'HOOK-FAILED rc=%s stderr=%s\n' "$rc" "$(head -c 200 "$TMP_ROOT/err")"
    return 0
  fi
  cat "$TMP_ROOT/out"
}

expect_has() { # $1=説明 $2=stdout $3=含むべき文字列
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if ! printf '%s' "$2" | jq -e --arg w "$3" '.hookSpecificOutput.additionalContext | contains($w)' >/dev/null 2>&1; then
    echo "✗ 注入に「$3」が無い / JSON が壊れている: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return
  fi
  if printf '%s' "$2" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ permissionDecision を返している (注入だけのはず): $1"; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_lacks() { # $1=説明 $2=stdout $3=含んではいけない文字列
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  case "$2" in *"$3"*) echo "✗ 「$3」が出ている: $1"; fail=$((fail+1)); return ;; esac
  echo "✓ $1"; ok=$((ok+1))
}
expect_silent() {
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if [ -n "$2" ]; then echo "✗ 注入が出るべきでない: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

F="$REPO/src/foo/a.go"
next_session; out=$(run Edit "$F" $'func readCookies(x int) int { return x }\n')
expect_has "Go: 大小を無視して名前が等しい既存の定義 (メソッドの接頭辞つき) を注入する" "$out" "Workspace.ReadCookies"
next_session; out=$(run Edit "$F" $'func loadSettings() {}\n')
expect_has "前方一致 (共通部分が 6 文字以上) の既存の定義を注入する" "$out" "loadSettingsFile"
expect_lacks "共通の前方が無い名前 (reloadSettings) は雑音なので出さない" "$out" "reloadSettings"
next_session; out=$(run Edit "$F" $'func collectWidgetsFast(xs []int) []int { return xs }\n')
expect_has "引く名前より短い既存の定義 (collectWidgets ⊂ collectWidgetsFast) は、語の境目の前方で引いて注入する" "$out" "collectWidgets(xs"
next_session; expect_silent "前方の語で引いて出た別の名前 (gatherAll) は、既存の名前が前方になっていないので出さない" "$(run Edit "$F" $'func gatherThings() {}\n')"
next_session; expect_silent "候補が無ければ何も出さない" "$(run Edit "$F" $'func brandNewName() {}\n')"
next_session; expect_silent "共通部分が 6 文字未満の前方一致 (parse ⊂ parseThing) は出さない" "$(run Edit "$F" $'func parseThing() {}\n')"
next_session; expect_silent "Edit の old_string に既にある関数は新しくない" "$(run Edit "$F" $'func readCookies(x int) int { return x }\n' $'func readCookies(x int) int { return 0 }\n')"
next_session; expect_silent "書き換え前のファイルに既にある関数は新しくない (Write の上書き)" "$(run Write "$F" $'package foo\n\nfunc existingHelper() {}\n')"
next_session; expect_silent "Test で始まる関数は引かない" "$(run Edit "$F" $'func TestReadCookies(t *testing.T) {}\n')"
next_session; expect_silent "Go のメソッドは引かない (型ごとに同名が並ぶのは普通)" "$(run Edit "$F" $'func (w *W) readCookies() {}\n')"
next_session; expect_silent "5 文字未満の名前は引かない" "$(run Edit "$F" $'func read() {}\n')"
next_session; expect_silent "対象でない拡張子" "$(run Edit "$REPO/src/foo/x.py" $'func readCookies() {}\n')"
next_session; expect_silent "相対パス" "$(run Edit "src/foo/a.go" $'func readCookies() {}\n')"
next_session; expect_silent "git の repo の外" "$(run Edit "$TMP_ROOT/plain/x.go" $'func readCookies() {}\n')"
next_session; expect_silent "dupscan が PATH に無い" "$(run Edit "$F" $'func readCookies() {}\n' "" "$NO_DUPSCAN_BIN")"
next_session; out=$(run Edit "$REPO/src/foo/B.swift" $'    private func readCookies() {}\n')
expect_has "Swift: 字下げのある func も新しい関数として引く" "$out" "ReadCookies"
next_session; run Edit "$F" $'func readCookies() {}\n' >/dev/null
expect_silent "同じ session × 名前の 2 回目は出さない" "$(run Edit "$F" $'func readCookies() {}\n')"
n=$(grep -c . "$TMP_ROOT/cache/claude-dupscan-inject.log" || true)
if [ "$n" -ge 5 ] && grep -q $'\treadCookies\t1$' "$TMP_ROOT/cache/claude-dupscan-inject.log"; then
  echo "✓ 注入の計測ログに「session 名前 候補数」が残る"; ok=$((ok+1))
else
  echo "✗ 計測ログが想定と違う (行数 $n)"; fail=$((fail+1))
fi

if jq -e '[.hooks.PreToolUse[] | select(.matcher == "Write|Edit") | .hooks[].command] | index("~/dotfiles/_claude/hooks/inject-dupscan-candidates-on-new-func.sh")' \
  "$ROOT_DIR/_claude/settings.json" >/dev/null; then
  echo "✓ settings.json の PreToolUse(Write|Edit) に配線されている"; ok=$((ok+1))
else
  echo "✗ settings.json の PreToolUse(Write|Edit) に配線されていない"; fail=$((fail+1))
fi

echo "---"
echo "ok=$ok fail=$fail"
[ "$fail" -eq 0 ]
