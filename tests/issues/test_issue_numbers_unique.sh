#!/usr/bin/env bash
# issue 番号 (NNN) が issues/ 全体で一意であることを検査する。
#
# なぜ: issues/README.md の命名規約は「issues/ 直下・pending/・done/ の全体で最大番号 + 1 を
# 採番する (番号は再利用しない)」と定めているが、これを守るのは採番する人の目視だけだった。
# 並行セッションが同じ番号を同時に取ると衝突し、**先に踏むのは番号を取る次の人**になる
# (最大番号 + 1 が既に使われている / `issues/127-*` の glob が 2 件返る)。
# 2026-08-28 に 127 と 133 が同時に衝突していたのを人手で見つけたのが起点。
#
# 参照はどれもファイル名込み (`[127](127-….md)` / audit-log は full path) なので、衝突の
# 実害は「番号が一意でない」ことに限られる。だから検出はこの 1 点に絞る。
#
# 変異検証のため、検査対象ディレクトリを第 1 引数で差し替えられる (既定 = repo の issues/)。
#   重複を作って red を見る:
#     d=$(mktemp -d); : > "$d/010-bug-a.md"; : > "$d/010-bug-b.md"
#     tests/issues/test_issue_numbers_unique.sh "$d"   # → 非 0 で落ちる
#   epic 側との衝突も red になること (深さの回帰):
#     d=$(mktemp -d); mkdir -p "$d/epic/g/next"; : > "$d/010-bug-a.md"; : > "$d/epic/g/next/010-bug-b.md"
#     tests/issues/test_issue_numbers_unique.sh "$d"   # → 非 0 で落ちる
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

issues_dir="${1:-issues}"

if [ ! -d "$issues_dir" ]; then
  printf '✗ 検査対象ディレクトリが無い: %s\n' "$issues_dir" >&2
  exit 1
fi

# ディレクトリを列挙せず、深さも切らずに掘る: 将来サブディレクトリが増えても黙って対象外に
# ならないようにする。🚨 -maxdepth を置かない: 以前 `-maxdepth 2` だったため、
# `issues/epic/<name>/NNN-*.md` (深さ 3) と `epic/<name>/next/` (深さ 4) が検査から漏れていた
# (2026-09-05 に発見。epic 側と直下で同じ番号を取っても緑のままだった)。
files=$(find "$issues_dir" -type f -name '[0-9][0-9][0-9]-*.md' -print | sort)

# 収集 0 件は成功にしない (tests/CLAUDE.md「0 件・skip・沈黙の扱い」)。
# find の失敗・ディレクトリ改名・パターンの空振りは、どれも「重複なし」と同じ空出力になる。
if [ -z "$files" ]; then
  printf '✗ %s 配下に NNN-*.md が 1 件も見つからない (find 失敗 or ディレクトリ改名を疑う)\n' "$issues_dir" >&2
  exit 1
fi

total=$(printf '%s\n' "$files" | wc -l | tr -d ' ')

# basename の先頭 3 桁だけを取り出して重複を数える。
numbers=$(printf '%s\n' "$files" | sed 's|.*/||; s|^\([0-9][0-9][0-9]\)-.*|\1|')
dups=$(printf '%s\n' "$numbers" | sort | uniq -d)

if [ -n "$dups" ]; then
  printf '✗ issue 番号が重複している (issues/README.md: 番号は再利用しない = 全体で一意):\n' >&2
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    printf '  番号 %s:\n' "$n" >&2
    # grep はパイプ終端に置かない (pipefail + grep -q の判定反転を避ける形に揃える)
    printf '%s\n' "$files" | sed -n "s|\(.*/${n}-.*\)|    \1|p" >&2
  done <<< "$dups"
  printf '\n  直し方: 参照の少ない側を空き番号へ改番する。参照は\n' >&2
  printf '    grep -rn "<番号>" . --exclude-dir=.git --exclude-dir=tmp\n' >&2
  printf '    git log --oneline --grep="issue <番号>"   # commit message は履歴なので直せない\n' >&2
  printf '  で数え、tracked ファイルと push 済み commit の両方から参照されている側は動かさない。\n' >&2
  exit 1
fi

printf '✓ issue 番号の一意性: %s 件を検査、重複なし (%s)\n' "$total" "$issues_dir"
