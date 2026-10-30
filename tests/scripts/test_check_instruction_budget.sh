#!/usr/bin/env bash
# scripts/check_instruction_budget.sh (常時読み込みの指示ファイルの合計の上限) の契約を fixture の木で固定する。
#
# 守るもの: 上限の内なら通す / global 層が上限を超えたら落とす / frontmatter に paths: がある rule は数えない /
# 本文にある paths: は条件ロードではないので数える / 上限ちょうどは通す / サブディレクトリの rule も数える / 字数をバイトでなく文字で数える / repo 層を足して上限を超えたら落とす /
# 数える対象が空 (rule 0 本・_claude/CLAUDE.md 無し) なら緑にしない / Makefile の test-lint から配線されている。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_instruction_budget.sh"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

grep -qE '^test-instruction-budget:' "$ROOT_DIR/Makefile" && grep -qE '^\t@scripts/check_instruction_budget\.sh$' "$ROOT_DIR/Makefile" \
  && grep -qE 'run_make_targets_parallel\.sh .*test-instruction-budget' "$ROOT_DIR/Makefile" \
  || { printf '✗ Makefile の test-lint が check_instruction_budget.sh を回していない (配線が外れている)\n'; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-check-instruction-budget.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# $1 個の ASCII 字 (末尾の改行を含めて $1 字)
ascii() { awk -v n="$1" 'BEGIN { s = ""; for (i = 1; i < n; i++) s = s "a"; print s }'; }

# 新しい fixture の木を作る。global 層に CLAUDE.md 1,000 字と rule 1 本 1,000 字を置く
new_tree() {
  T="$WORK/$1"
  mkdir -p "$T/_claude/rules" "$T/.claude/rules"
  ascii 1000 > "$T/_claude/CLAUDE.md"
  ascii 1000 > "$T/_claude/rules/base.md"
}
run() {
  RC=0
  OUT="$(INSTRUCTION_BUDGET_ROOT="$T" "$CHECK" 2>&1)" || RC=$?
}

new_tree under
ascii 100000 > "$T/_claude/rules/big.md"
run
if [ "$RC" -eq 0 ] && grep -q 'global 層 102000 字 (_claude/CLAUDE.md + rule 2 本' <<<"$OUT"; then ok '上限の内 (global 102,000 字) は通し、数えた字数と本数を出す'
else bad "上限の内が通らない / 字数が違う (rc=$RC): $OUT"; fi

new_tree over
ascii 110000 > "$T/_claude/rules/big.md"
run
if [ "$RC" -eq 1 ] && grep -q 'global 層が 1000 字超えている' <<<"$OUT"; then ok 'global 層が上限 (111,000) を超えたら落とす'
else bad "global 層の上限超えが落ちない (rc=$RC): $OUT"; fi

new_tree edge
ascii 109000 > "$T/_claude/rules/big.md"
run
rc_at=$RC
printf 'a' >> "$T/_claude/rules/big.md"
run
if [ "$rc_at" -eq 0 ] && [ "$RC" -eq 1 ]; then ok '上限ちょうど (111,000) は通し、1 字超えたら落とす'
else bad "上限の境界が違う (ちょうど rc=$rc_at / 1 字超え rc=$RC): $OUT"; fi

new_tree subdir
mkdir -p "$T/_claude/rules/sub"
ascii 120000 > "$T/_claude/rules/sub/deep.md"
run
if [ "$RC" -eq 1 ]; then ok 'rules のサブディレクトリに置いた rule も数える'
else bad "サブディレクトリの rule を数えていない (rc=$RC): $OUT"; fi

new_tree paths
{ printf -- '---\npaths:\n  - "**/*.swift"\n---\n'; ascii 200000; } > "$T/_claude/rules/cond.md"
run
if [ "$RC" -eq 0 ]; then ok 'frontmatter に paths: がある rule は数えない'
else bad "paths: 付きの rule を数えている (rc=$RC): $OUT"; fi

new_tree paths-in-body
{ printf -- '---\nname: x\n---\npaths:\n'; ascii 200000; } > "$T/_claude/rules/body.md"
run
if [ "$RC" -eq 1 ]; then ok 'frontmatter の外にある paths: は条件ロードと見なさない'
else bad "本文の paths: で数えるのをやめている (rc=$RC): $OUT"; fi

new_tree multibyte
awk 'BEGIN { s = ""; for (i = 1; i < 50000; i++) s = s "字"; print s }' > "$T/_claude/rules/ja.md"
run
if [ "$RC" -eq 0 ] && grep -q 'global 層 52000 字' <<<"$OUT"; then ok '全角を 1 字と数える (50,000 字 = 150,000 バイトでも通す)'
else bad "字数をバイトで数えている (rc=$RC): $OUT"; fi

new_tree session
ascii 100000 > "$T/_claude/rules/big.md"
ascii 1000 > "$T/CLAUDE.md"
ascii 50000 > "$T/.claude/rules/repo.md"
run
if [ "$RC" -eq 1 ] && grep -q 'dotfiles のセッションが上限を 3000 字超えている' <<<"$OUT" && ! grep -q 'global 層が' <<<"$OUT"; then
  ok 'global 層が上限の内でも、repo 層を足したセッションが 150,000 を超えたら落とす'
else bad "repo 層を足した上限超えが落ちない (rc=$RC): $OUT"; fi

new_tree no-rules
rm "$T/_claude/rules/base.md"
run
if [ "$RC" -eq 1 ] && grep -q '1 本も無い' <<<"$OUT"; then ok '常時読み込みの rule が 0 本なら緑にしない'
else bad "rule 0 本で緑になる (rc=$RC): $OUT"; fi

new_tree no-claude-md
rm "$T/_claude/CLAUDE.md"
run
if [ "$RC" -eq 1 ] && grep -q 'CLAUDE.md が無い' <<<"$OUT"; then ok '_claude/CLAUDE.md が無ければ緑にしない'
else bad "_claude/CLAUDE.md が無くても緑になる (rc=$RC): $OUT"; fi

if [ "$fails" -ne 0 ]; then
  printf '✗ check_instruction_budget の契約が %d 件崩れている\n' "$fails"
  exit 1
fi
printf '✓ check_instruction_budget の契約がすべて成り立つ\n'
