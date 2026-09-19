#!/usr/bin/env bash
# _claude/hooks/issue-rules-inject.sh (issues/ を持つ repo にだけ issue 運用規約を注入する
# SessionStart hook) の unit テスト。
#
# なぜ: この hook の壊れ方は 2 つで、どちらも静かに起きる。
#   ① issues/ がある repo で注入しない → 規約なしで issue が書かれる (各 README から規約を抜いたため)
#   ② issues/ が無い repo (仕事の repo) で注入する → 無関係な規約が毎セッション読まれる
# 同じ repo で issues/ の有無だけを変えた A-B で両方を固定する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/issue-rules-inject.sh"
RULES="$ROOT_DIR/_claude/issue-rules.md"
fails=0

if ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: jq が無い環境"
  exit 77
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/issue-rules-inject.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
repo="$WORK/repo"
mkdir -p "$repo/src"
git -C "$repo" init -q .

ctx() { # env ... を前置できる。cwd はサブディレクトリ (repo root 以外から起動しても解決できること)
  local out
  out=$(printf '{"cwd":"%s/src"}' "$repo" | "$@" "$HOOK")
  [ -n "$out" ] || return 0
  printf '%s' "$out" | jq -r '.hookSpecificOutput.additionalContext'
}

ng() { echo "NG: $1"; fails=$((fails + 1)); }

# ② issues/ が無い repo では何も出さない
got=$(ctx env)
[ -z "$got" ] || ng "issues/ が無い repo で注入した"

# ① issues/ がある repo では規約の本文を丸ごと注入する (見出しだけでなく本文全体を比較)
mkdir -p "$repo/issues"
got=$(ctx env)
want=$(cat "$RULES")
case "$got" in
  *"$want"*) ;;
  *) ng "issues/ がある repo で規約全文が注入されていない" ;;
esac

# 規約ファイルを読めないときは黙らずに警告する
got=$(ctx env ISSUE_RULES_FILE="$WORK/missing.md")
case "$got" in
  *"読めなかった"*) ;;
  *) ng "規約ファイルを読めないのに警告が出ない: [$got]" ;;
esac

# 入れ子 1 段 (<root>/macOS/issues/ のような形) でも注入する
rm -rf "$repo/issues"; mkdir -p "$repo/macOS/issues"
got=$(ctx env)
[ -n "$got" ] || ng "入れ子の <root>/*/issues で注入しない"

if [ "$fails" -eq 0 ]; then echo "OK: issue-rules-inject (4 ケース)"; else exit 1; fi
