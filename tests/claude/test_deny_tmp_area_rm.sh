#!/usr/bin/env bash
# _claude/hooks/deny-tmp-area-rm.sh (一時領域の浅い場所を直接消す・移すコマンドの deny) の unit テスト。
# 合成した hook JSON を stdin で流し、deny / allow の判定を pin する。配線 (settings.json) も見る。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/deny-tmp-area-rm.sh"
SETTINGS="$ROOT_DIR/_claude/settings.json"
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1

[ -x "$HOOK" ] || { printf '✗ フックが無いか実行権限が無い: %s\n' "$HOOK"; exit 1; }
command -v jq >/dev/null 2>&1 || { printf '✗ jq が無いため判定を検査できない (フックも jq 不在時は無効)\n'; exit 1; }
jq -e '[.hooks.PreToolUse[]? | select((.matcher // "") | test("Bash")) | .hooks[]?.command // ""]
       | map(test("deny-tmp-area-rm")) | any' "$SETTINGS" >/dev/null \
  || { printf '✗ PreToolUse(Bash) の配下にフックが配線されていない\n'; exit 1; }
printf '✓ settings.json の PreToolUse(Bash) に配線されている\n'

decision() {  # $1=コマンド → deny / allow / timeout / error / harness-error (失敗を allow に畳まない)
  local out rc payload json
  payload="$(mktemp)"
  json="$(mktemp)"
  printf '%s' "$1" >"$payload"
  if ! jq -n --rawfile c "$payload" '{tool_name:"Bash",tool_input:{command:$c}}' >"$json" 2>/dev/null; then
    rm -f "$payload" "$json"
    echo harness-error
    return
  fi
  out="$("$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" <"$json" 2>/dev/null)"
  rc=$?
  rm -f "$payload" "$json"
  if [ -n "$out" ]; then
    printf '%s' "$out" | jq -r '.hookSpecificOutput.permissionDecision // "error"' 2>/dev/null || echo error
    return
  fi
  case "$rc" in
    0) echo allow ;;
    124) echo timeout ;;
    *) echo error ;;
  esac
}

FAIL_COUNT=0
expect() {  # $1=期待 $2=説明 $3=コマンド
  local got
  got="$(decision "$3")"
  if [ "$got" != "$1" ]; then
    printf '✗ %s\n  期待=%s 実際=%s\n  cmd: %.200s\n' "$2" "$1" "$got" "$3"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  printf '✓ %s (%s)\n' "$2" "$1"
}

S=/private/tmp/claude-501
T=/private/var/folders/hw/abc/T
# --- deny: 浅い場所を直接消す・移す ---
expect deny "scratchpad の根" "rm -rf $S"
expect deny "project dir" "rm -rf $S/-Users-me-src-x"
expect deny "セッション dir" "rm -rf $S/-Users-me-src-x/11111111-2222-3333-4444-555555555555"
expect deny "/private 無しの書き方" "rm -rf /tmp/claude-501/-p/11111111-2222-3333-4444-555555555555"
expect deny "末尾の / と . を畳む" "rm -rf $S/-p/./"
expect deny ".. で浅い所へ戻る" "rm -rf $S/-p/sid/scratchpad/../.."
expect deny "glob で project を一括" "rm -rf $S/*/*"
expect deny "glob で他のセッションの中まで (深くても浅い所に glob)" "rm -rf $S/*/*/tasks"
expect deny "引用符で囲んだ" "rm -rf \"$S/-p\""
expect deny "TMPDIR の直下 (実パス)" "rm -rf $T/go-build123"
expect deny "TMPDIR の直下 (/var の形)" "rm -rf /var/folders/hw/abc/T/tmp.x"
expect deny "\$TMPDIR の直下" 'rm -rf "$TMPDIR"/treefiler-config-*'
expect deny "\${TMPDIR} の直下" 'rm -rf ${TMPDIR}/x'
expect deny "find -delete" "find $T -maxdepth 1 -name 'go-build*' -delete"
expect deny "xargs rm" "find $T -maxdepth 1 -name 'x*' -print0 | xargs -0 rm -rf"
expect deny "mv で外へ出す" "mv $S/-p/11111111-2222-3333-4444-555555555555 ~/x"
expect deny "rmdir" "rmdir $T/empty"
# --- allow: 普段の作業 ---
expect allow "自分の scratchpad の中のファイル" "rm -f $S/-p/11111111-2222-3333-4444-555555555555/scratchpad/out.log"
expect allow "TMPDIR の深い所" "rm -rf $T/go-build1/b001"
expect allow "引用符で区切った \$TMPDIR の深い所" 'rm -f "$TMPDIR"/go-build1/b001/x'
expect allow "数えるだけ" "find $S -maxdepth 2 | wc -l"
expect allow "tmpclean を使う" "bin/tmpclean apply -plan $S/x/plan.json -approve abc"
expect allow "消す語が無い" "ls -la $T"
expect allow "似た名前のコマンド (grm)" "grm -rf $T/x"
expect allow "関係ない場所を rm" "rm -rf /Users/me/src/x/tmp/a"
expect allow "変数経由 (検出しない形)" 'D=$(mktemp -d); rm -rf "$D"'

if ((FAIL_COUNT > 0)); then
  printf '\n%d 件失敗\n' "$FAIL_COUNT"
  exit 1
fi
printf '\nAll deny-tmp-area-rm tests passed successfully!\n'
