#!/usr/bin/env bash
# _claude/hooks/gofmt-on-edit.sh のテスト。一時 dir のファイルだけを相手にする。
#
# 守るもの: Edit / Write の後に .go を整形する (素通りすると未整形のコードが CI をすり抜ける。hook 冒頭の実例)・
# .go 以外と無いファイルには触らず rc=0 で抜ける (編集を妨げない)・Write の応答にしか path が無い形も拾う。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/gofmt-on-edit.sh"
fails=0
ng() { echo "NG: $*"; fails=$((fails + 1)); }

command -v gofmt >/dev/null 2>&1 || { echo "NG: gofmt が無く、このテストは何も検査できない"; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/gofmt-on-edit.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

unformatted() { printf 'package x\nfunc  F( ) {\nreturn\n}\n'; }

# run <json>: hook を走らせ、rc を RC、stdout と stderr を OUT / ERR に入れる
run() {
  OUT=$(printf '%s' "$1" | "$HOOK" 2>"$WORK/stderr")
  RC=$?
  ERR=$(cat "$WORK/stderr")
}

# 1. Edit の .tool_input.file_path の .go を整形する
f="$WORK/a.go"; unformatted > "$f"
[ -n "$(gofmt -l "$f")" ] || ng "1: 前提が崩れている (fixture が既に整形済みで、このケースは何も検査していない)"
run "$(jq -n --arg f "$f" '{tool_name:"Edit", tool_input:{file_path:$f}}')"
[ "$RC" -eq 0 ] || ng "1: rc=$RC"
[ -z "$(gofmt -l "$f")" ] || ng "1: .go が整形されていない"

# 2. tool_input に path が無く .tool_response.filePath にだけある形も拾う
f="$WORK/b.go"; unformatted > "$f"
run "$(jq -n --arg f "$f" '{tool_name:"Write", tool_input:{}, tool_response:{filePath:$f}}')"
[ -z "$(gofmt -l "$f")" ] || ng "2: tool_response.filePath の .go が整形されていない"

# 3. .go 以外は書き換えない
f="$WORK/c.txt"; unformatted > "$f"; before=$(cat "$f")
run "$(jq -n --arg f "$f" '{tool_name:"Edit", tool_input:{file_path:$f}}')"
[ "$RC" -eq 0 ] || ng "3: rc=$RC"
[ "$(cat "$f")" = "$before" ] || ng "3: .go 以外を書き換えた"

# 4. 無いファイル・構文の壊れた .go でも rc=0 で、何も出さない (編集を妨げない)
run "$(jq -n --arg f "$WORK/nope.go" '{tool_name:"Edit", tool_input:{file_path:$f}}')"
[ "$RC" -eq 0 ] && [ -z "$OUT$ERR" ] || ng "4: 無いファイルで rc=$RC / out=$OUT / err=$ERR"
f="$WORK/broken.go"; printf 'package x\nfunc {\n' > "$f"; before=$(cat "$f")
run "$(jq -n --arg f "$f" '{tool_name:"Edit", tool_input:{file_path:$f}}')"
[ "$RC" -eq 0 ] && [ -z "$OUT$ERR" ] || ng "4: 構文の壊れた .go で rc=$RC / out=$OUT / err=$ERR"
[ "$(cat "$f")" = "$before" ] || ng "4: 構文の壊れた .go を書き換えた"

if [ "$fails" -eq 0 ]; then
  echo "OK: gofmt-on-edit.sh (4 ケース)"
else
  echo "✗ gofmt-on-edit.sh: $fails 件の失敗"
  exit 1
fi
