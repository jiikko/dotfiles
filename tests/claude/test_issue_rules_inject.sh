#!/usr/bin/env bash
# _claude/hooks/issue-rules-inject.sh (issues/ を持つ repo にだけ issue 運用規約を注入する
# SessionStart hook) の unit テスト。
#
# なぜ: この hook の壊れ方は 2 つで、どちらも静かに起きる。
#   ① issues/ がある repo で注入しない → 規約なしで issue が書かれる (各 README から規約を抜いたため)
#   ② issues/ が無い repo (仕事の repo) で注入する → 無関係な規約が毎セッション読まれる
#   ③ issue-rules.d/ の rule が届かない → hook 1 本の注入は約 10k 字で末尾が黙って落ちる (issue 414) ので、
#     1 ファイル 1 呼び出しで settings.json に並べる。並べ漏れ・上限超えを固定する
# 同じ repo で issues/ の有無だけを変えた A-B で①②を固定する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/issue-rules-inject.sh"
RULES="$ROOT_DIR/_claude/issue-rules.md"
RULES_D="$ROOT_DIR/_claude/issue-rules.d"
SETTINGS="$ROOT_DIR/_claude/settings.json"
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

# ③ issue-rules.d/ の各ファイルを引数で渡すと全文を注入し、上限の警告は出ない
nd=0
for f in "$RULES_D"/*.md; do
  [ -f "$f" ] || continue
  nd=$((nd + 1))
  rel="issue-rules.d/$(basename "$f")"
  got=$(printf '{"cwd":"%s/src"}' "$repo" | "$HOOK" "$rel" | jq -r '.hookSpecificOutput.additionalContext')
  want=$(cat "$f")
  case "$got" in *"$want"*) ;; *) ng "$rel の全文が注入されていない" ;; esac
  case "$got" in "🚨 この規約は"*) ng "$rel が注入の上限を超えている (分けるか縮める)" ;; esac
done
[ "$nd" -ge 1 ] || ng "issue-rules.d/ に rule が 1 本も無い (移動で消えていないか)"

# ③ settings.json の SessionStart に、本体 + issue-rules.d/ の各ファイルがちょうど 1 本ずつ並ぶ
wired=$(jq -r '[.hooks.SessionStart[].hooks[].command | select(test("issue-rules-inject\\.sh"))
               | sub(".*issue-rules-inject\\.sh ?"; "")] | sort | .[]' "$SETTINGS")
expect=$( { echo ""; for f in "$RULES_D"/*.md; do [ -f "$f" ] && echo "issue-rules.d/$(basename "$f")"; done; } | sort)
[ "$wired" = "$expect" ] || ng "settings.json の配線と issue-rules.d/ が食い違う: wired=[$wired] expect=[$expect]"

# 上限を超えたら、警告を先頭に置く (打ち切られるのは末尾)
got=$(ctx env ISSUE_RULES_INJECT_MAX=10)
case "$got" in
  "🚨 この規約は"*"上限"*) ;;
  *) ng "上限を超えたのに先頭に警告が出ない: [$(printf '%s' "$got" | head -1)]" ;;
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

if [ "$fails" -eq 0 ]; then echo "OK: issue-rules-inject (issue-rules.d $nd 本を含む)"; else exit 1; fi
