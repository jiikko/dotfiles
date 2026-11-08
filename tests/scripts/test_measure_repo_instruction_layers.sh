#!/usr/bin/env bash
# scripts/measure_repo_instruction_layers.py (他の repo のセッションの指示ファイルの字数) の数え方を、ローカルの git repo の fixture (--local) で固定する。
#
# 守るもの: umbrella の root から起動した場所までの各ディレクトリの CLAUDE.md と .claude/rules を足す / submodule には umbrella の層 (途中のディレクトリを含む) を足す /
# frontmatter に paths: がある rule は数えない / 1 段下はビルドの入口を持つディレクトリだけを起動する場所と見なす (bin/ は数えない) /
# 隠しディレクトリは起動する場所にしない / 全角を 1 字と数える / --extra の repo も数える / 最大の行を出す /
# symlink の rule は測れないとして rc=2 / 既定ブランチの先端 (HEAD) を読み、作業ツリーの未 commit の変更は数えない。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MEASURE="$ROOT_DIR/scripts/measure_repo_instruction_layers.py"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-measure-repo-layers.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
export XDG_CACHE_HOME="$WORK/cache"  # 本物の cache を汚さない

# $1 個の ASCII 字 (末尾の改行を含めて $1 字)
ascii() { awk -v n="$1" 'BEGIN { s = ""; for (i = 1; i < n; i++) s = s "a"; print s }'; }
# repo を作って commit する。$1 = owner/repo、残り = "path=字数" か "path=paths:字数" (frontmatter に paths: を付ける)
mkrepo() {
  local d="$WORK/src/$1" spec path n
  shift
  mkdir -p "$d"
  git -C "$d" init -q
  for spec in "$@"; do
    path=${spec%%=*}
    n=${spec#*=}
    mkdir -p "$d/$(dirname "$path")"
    case "$n" in
      paths:*) { printf -- '---\npaths:\n  - "**/*.swift"\n---\n'; ascii "${n#paths:}"; } > "$d/$path" ;;
      ja:*) awk -v n="${n#ja:}" 'BEGIN { s = ""; for (i = 1; i < n; i++) s = s "字"; print s }' > "$d/$path" ;;
      *) ascii "$n" > "$d/$path" ;;
    esac
  done
  git -C "$d" add -A
  git -C "$d" -c user.name=t -c user.email=t@example.com commit -q -m fixture
}
run() {
  RC=0
  OUT="$("$MEASURE" --local "$WORK/src" "$@" 2>&1)" || RC=$?
}

mkrepo o/umb CLAUDE.md=100 .claude/rules/a.md=200 .claude/rules/cond.md=paths:1000 apps/CLAUDE.md=10
printf '[submodule "apps/app"]\n\tpath = apps/app\n\turl = git@github.com:o/app.git\n[submodule "lib/x"]\n\tpath = lib/x\n\turl = https://github.com/o/x.git\n' \
  > "$WORK/src/o/umb/.gitmodules"
git -C "$WORK/src/o/umb" add .gitmodules && git -C "$WORK/src/o/umb" -c user.name=t -c user.email=t@example.com commit -q -m gitmodules
mkrepo o/app CLAUDE.md=300 .claude/rules/r.md=ja:51 macOS/CLAUDE.md=40 macOS/Makefile=1 bin/CLAUDE.md=5000 .claude/CLAUDE.md=5000
mkrepo o/x CLAUDE.md=7
mkrepo o/ext CLAUDE.md=20
ascii 9000 > "$WORK/src/o/app/macOS/CLAUDE.md"  # commit しない変更は数えない

run --umbrella o/umb --extra o/ext
line() { awk -F '\t' -v l="$1" '$2 == l { print $1 }' <<<"$OUT"; }
if [ "$RC" -eq 0 ] && [ "$(line 'o/umb:.')" = 300 ]; then ok 'umbrella の root は CLAUDE.md と paths: の無い rule だけを足す (300)'
else bad "umbrella の root の値が違う (rc=$RC): $OUT"; fi
if [ "$(line 'o/app:. (o/umb の apps/app)')" = 661 ]; then ok 'submodule には umbrella の root と途中の apps/ の層を足し、全角を 1 字と数える (300 + 10 + 300 + 51)'
else bad "submodule の root の値が違う: $OUT"; fi
if [ "$(line 'o/app:macOS (o/umb の apps/app)')" = 701 ]; then ok 'ビルドの入口を持つ 1 段下 (macOS/) は起動する場所で、上の層を全部足す。未 commit の変更は数えない (701)'
else bad "macOS/ のセッションの値が違う: $OUT"; fi
if ! grep -q 'o/app:bin' <<<"$OUT" && ! grep -q 'o/app:.claude' <<<"$OUT"; then ok 'ビルドの入口の無い bin/ と隠しディレクトリは起動する場所にしない'
else bad "bin/ か .claude/ を起動する場所として数えている: $OUT"; fi
if [ "$(line 'o/x:. (o/umb の lib/x)')" = 307 ] && [ "$(line 'o/ext:.')" = 20 ]; then ok 'https の url の submodule と --extra の repo も数える'
else bad "lib/x か --extra の値が違う: $OUT"; fi
if grep -qx $'max\t701\to/app:macOS (o/umb の apps/app)' <<<"$OUT"; then ok '最後の行に最大のセッションを出す'
else bad "max の行が違う: $OUT"; fi

mkrepo o/sym CLAUDE.md=10
mkdir -p "$WORK/src/o/sym/.claude/rules"
ln -s ../../CLAUDE.md "$WORK/src/o/sym/.claude/rules/link.md"
git -C "$WORK/src/o/sym" add -A && git -C "$WORK/src/o/sym" -c user.name=t -c user.email=t@example.com commit -q -m link
run --umbrella o/umb --extra o/sym
if [ "$RC" -eq 2 ] && grep -q 'symlink' <<<"$OUT" && ! grep -q '^max' <<<"$OUT"; then ok 'symlink の rule は測れないとして rc=2 で止め、max を出さない'
else bad "symlink の rule で止まらない (rc=$RC): $OUT"; fi

run --umbrella o/none
if [ "$RC" -eq 2 ] && ! grep -q '^max' <<<"$OUT"; then ok '読めない repo は rc=2 で止め、max を出さない'
else bad "読めない repo で止まらない (rc=$RC): $OUT"; fi

if [ "$fails" -ne 0 ]; then
  printf '✗ measure_repo_instruction_layers の契約が %d 件崩れている\n' "$fails"
  exit 1
fi
printf '✓ measure_repo_instruction_layers の契約がすべて成り立つ\n'
