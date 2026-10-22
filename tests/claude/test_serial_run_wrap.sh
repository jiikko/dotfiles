#!/usr/bin/env bash
# _claude/hooks/serial-run-wrap.sh (重いコマンドの前に serial-run -- を差し込む PreToolUse hook) のテスト。
# 書き換えの規則そのものは src/serialrun/hook_test.go が持つ。ここは hook の入口 (配線・fail-open・PATH の照合) を見る。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
HOOK="$ROOT_DIR/_claude/hooks/serial-run-wrap.sh"
SETTINGS="$ROOT_DIR/_claude/settings.json"
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
# 先に 1 回ビルドを済ませる (hook の中の初回ビルドで時間を食わない)
"$ROOT_DIR/bin/serial-run" __ready </dev/null || { echo "✗ serial-run をビルドできない"; exit 1; }

[ -x "$HOOK" ] || { echo "✗ フックが無いか実行権限が無い: $HOOK"; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無いため検査できない"; exit 1; }
jq -e '[.hooks.PreToolUse[]? | select((.matcher // "") | test("Bash")) | .hooks[]?.command // ""]
       | map(test("serial-run-wrap")) | any' "$SETTINGS" >/dev/null \
  || { echo "✗ PreToolUse(Bash) の配下にフックが配線されていない"; exit 1; }
echo "✓ settings.json の PreToolUse(Bash) に配線されている"

FAIL_COUNT=0
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

# rewritten <コマンド> [env…]: hook が返した command (書き換えなしなら空) を出す。hook の rc≠0 は失敗にする
rewritten() {
  local cmd="$1" out rc
  shift
  jq -n --arg c "$cmd" '{tool_name:"Bash",tool_input:{command:$c,timeout:1234}}' >"$TMP_DIR/in.json"
  set +e
  out="$(env "$@" "$RUNTIMEOUT" 20 "$HOOK" <"$TMP_DIR/in.json")"
  rc=$?
  set -e
  [ "$rc" -eq 0 ] || { echo "HOOK-RC-$rc"; return; }
  [ -n "$out" ] || return 0
  printf '%s' "$out" | jq -r '.hookSpecificOutput.updatedInput.command + " timeout=" + (.hookSpecificOutput.updatedInput.timeout|tostring)'
}

expect() {  # $1=期待 $2=説明 $3=コマンド [env…]
  local want="$1" desc="$2" cmd="$3" got
  shift 3
  got="$(rewritten "$cmd" "$@")"
  if [ "$got" = "$want" ]; then
    echo "✓ $desc"
  else
    echo "✗ $desc: got [$got] want [$want]"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

P="PATH=$ROOT_DIR/bin:$PATH"
expect "serial-run -- make test timeout=1234" "重いコマンドの前に差し込み、他の欄を残す" "make test" "$P"
expect "cd a && serial-run -- go test ./... && git push timeout=1234" "重い部分だけに差し込む" "cd a && go test ./... && git push" "$P"
expect "" "重くないコマンドは書き換えない" "git status" "$P"
expect "" "引数の中の make test は書き換えない" 'grep -rn "make test" issues/' "$P"
expect "" "SERIAL_RUN_HOOK_DISABLE=1 で止まる" "make test" "$P" SERIAL_RUN_HOOK_DISABLE=1

# PATH 上の serial-run がこの checkout のものでないなら差し込まない (差し込むと実行時に別物か「見つからない」になる)
mkdir -p "$TMP_DIR/other"
printf '#!/bin/sh\nexit 0\n' >"$TMP_DIR/other/serial-run"
chmod +x "$TMP_DIR/other/serial-run"
expect "" "PATH 上の serial-run が別物なら差し込まない" "make test" "PATH=$TMP_DIR/other:/usr/bin:/bin"
expect "" "PATH に serial-run が無ければ差し込まない" "make test" "PATH=/usr/bin:/bin"

# jq が使えなくても包む (jq に頼っていた頃は、jq が無いと直列化が黙って全部止まった)。
# macOS には /usr/bin/jq があり PATH から外しにくいので、必ず失敗する jq を先頭に置く
mkdir -p "$TMP_DIR/badjq"
printf '#!/bin/sh\nexit 127\n' >"$TMP_DIR/badjq/jq"
chmod +x "$TMP_DIR/badjq/jq"
expect "serial-run -- make test timeout=1234" "jq が使えなくても包む" "make test" "PATH=$TMP_DIR/badjq:$ROOT_DIR/bin:$PATH"

# isHeavy の対象のコマンドの例が、入口の手がかりの語 (fast path) を全部通り抜ける (isHeavy にだけ語を足すと、入口で黙って落ちる)
for c in "make test" "gmake lint" "go test ./..." "go -C src/x test ./..." "swift build" "xcodebuild -scheme X build" "mutate-verify --file x" "mutate-verify-list l" "bin/runtimeout 600 make test"; do
  expect "serial-run -- $c timeout=1234" "入口を通り抜ける: $c" "$c" "$P"
done

if [ "$FAIL_COUNT" -gt 0 ]; then
  echo "✗ $FAIL_COUNT 件失敗"
  exit 1
fi
echo "✓ serial-run-wrap: すべて通過"
