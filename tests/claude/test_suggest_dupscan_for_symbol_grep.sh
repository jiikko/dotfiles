#!/usr/bin/env bash
# _claude/hooks/suggest-dupscan-for-symbol-grep.sh (PreToolUse(Bash / Grep): dupscan を組み込んだ repo で Swift のシンボルを grep したら
# dupscan を案内する) の unit テスト。
#
# なぜ: 案内が出なくなる退行は無音 (grep はそのまま走る)。逆に出すぎる退行は毎回の Bash に 1 行ずつ文脈を足す。どちらもテストでしか見えない。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/suggest-dupscan-for-symbol-grep.sh"
# 本番の配線と同じ上限 (timeout に殺されると無出力 = 案内が消える)
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "✗ python3 が無い。hook は python3 前提なので検査できない" >&2; exit 1; }

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT
export TMPDIR="$TMP_ROOT"   # 出しすぎ防止の状態ファイルをテストの中に閉じる

new_repo() { # $1=1 なら .dupscan.json を置く → $REPO
  REPO=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
  ( cd "$REPO" && git init -q . ) >/dev/null 2>&1
  [ "$1" = 1 ] && printf '{"version":1}\n' > "$REPO/.dupscan.json"
  return 0
}
session=0
next_session() { session=$((session + 1)); }

run_bash() { # $1=command → stdout
  jq -n --arg c "$1" --arg d "$REPO" --arg s "s$session" '{tool_name: "Bash", session_id: $s, cwd: $d, tool_input: {command: $c}}' |
    "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true
}
run_grep() { # $1=pattern $2=glob → stdout
  jq -n --arg p "$1" --arg g "$2" --arg d "$REPO" --arg s "s$session" '{tool_name: "Grep", session_id: $s, cwd: $d, tool_input: {pattern: $p, glob: $g}}' |
    "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true
}

expect_nudge() { # $1=説明 $2=stdout $3=案内に入るべき名前
  if ! printf '%s' "$2" | jq -e --arg n "dupscan def $3" '.hookSpecificOutput.additionalContext | contains($n)' >/dev/null 2>&1; then
    echo "✗ 案内が出ない / 名前が違う / JSON が壊れている: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return
  fi
  # 🚨 止めない: permissionDecision を返したら grep が止まる
  if printf '%s' "$2" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ permissionDecision を返している (案内だけのはず): $1"; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_silent() { # $1=説明 $2=stdout
  if [ -n "$2" ]; then echo "✗ 案内が出るべきでない: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

new_repo 1
next_session; expect_nudge "宣言の語つきの grep" "$(run_bash 'grep -rn "func executeMutation" shared/Sources')" executeMutation
next_session; expect_nudge "PascalCase の rg" "$(run_bash "rg -n 'StorageOperationLockRegistry' shared")" StorageOperationLockRegistry
next_session; expect_nudge "呼び出しの形 (末尾の括弧) で Swift を対象" "$(run_bash 'grep -rn "retrying(" --include=*.swift .')" retrying
next_session; expect_nudge "\\b つきの camelCase" "$(run_bash 'grep -rnw "\bdiagnosticCode\b" .')" diagnosticCode
next_session; expect_nudge "git grep の Type.member" "$(run_bash 'git grep -n "DropboxClient.upload"')" DropboxClient.upload
next_session; expect_nudge "パイプの後ろの segment" "$(run_bash 'cd shared && grep -rn "struct HTTPRequestHead" Sources | head -5')" HTTPRequestHead
next_session; expect_nudge "-e で渡したパターン" "$(run_bash 'grep -rn -e "makeUploadSessionBodyRequest" Sources')" makeUploadSessionBodyRequest
next_session; expect_nudge "Grep ツールの camelCase" "$(run_grep 'func statsEvent' '')" statsEvent
next_session; expect_nudge "Grep ツールの小文字の語でも glob が swift" "$(run_grep 'retrying' '*.swift')" retrying

next_session; expect_silent "小文字の語で Swift を対象にしていない" "$(run_bash 'grep -n refresh config.yml')"
next_session; expect_silent "全部が大文字の語" "$(run_bash 'grep -rn "TODO" Sources')"
next_session; expect_silent "正規表現" "$(run_bash 'grep -rn "foo.*Bar" Sources')"
next_session; expect_silent "grep を含まないコマンド" "$(run_bash 'swift build')"
next_session; expect_silent "grep が語の一部 (grepper)" "$(run_bash 'grepper StorageOperationLockRegistry')"
next_session; expect_silent "heredoc の本文の中の grep" "$(run_bash "$(printf 'git commit -F - <<%sM%s\nfix: grep -rn StorageOperationLockRegistry を直す\nM\n' "'" "'")")"
next_session; expect_silent "Grep ツールの記号だけのパターン" "$(run_grep 'error:' '')"

# 出しすぎない: 同じ session の 2 回目は出さず、別の session なら出す
next_session
expect_nudge "1 回目は出る" "$(run_bash 'grep -rn "func fooBarBaz" .')" fooBarBaz
expect_silent "同じ session の 2 回目は出ない" "$(run_bash 'grep -rn "func fooBarBaz" .')"
next_session; expect_nudge "別の session なら出る" "$(run_bash 'grep -rn "func fooBarBaz" .')" fooBarBaz
# 状態ファイルが壊れていても (数字でない・桁が多すぎる) 出す側に倒れ、エラーで無出力にならない
next_session; printf 'abc\n' > "$TMP_ROOT/claude-dupscan-nudge-s$session"
expect_nudge "状態ファイルが数字でない" "$(run_bash 'grep -rn "func fooBarBaz" .')" fooBarBaz
next_session; printf '12345678901234567890\n' > "$TMP_ROOT/claude-dupscan-nudge-s$session"
expect_nudge "状態ファイルの桁が多すぎる" "$(run_bash 'grep -rn "func fooBarBaz" .')" fooBarBaz

# .dupscan.json の無い repo・git の外では出さない
new_repo 0
next_session; expect_silent ".dupscan.json の無い repo" "$(run_bash 'grep -rn "func executeMutation" Sources')"
REPO="$TMP_ROOT"
next_session; expect_silent "git の外" "$(run_bash 'grep -rn "func executeMutation" Sources')"

echo "[suggest-dupscan-for-symbol-grep] $ok passed / $fail failed"
[ "$fail" -eq 0 ]
