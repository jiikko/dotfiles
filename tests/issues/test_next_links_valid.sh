#!/usr/bin/env bash
# issues/ 配下の symlink は「next/ の目印」だけで、しかも全部が有効であることを検査する。
#
# なぜ: 「次にやる」の claim は issue ファイルを next/ へ rename せず、`next/<base> -> ../<base>`
# の symlink を目印として置く運用になった (issue 263。rename すると本文の相対リンクが切れる)。
# 目印は glogx が読むとき採用条件で弾かれても警告が出るだけなので、壊れた目印 (issue を done/ へ
# 動かして symlink を消し忘れた = dangling、別の物を指す、next/ 以外に置かれた symlink) は
# 誰かが viewer を開くまで気づかれず、CI には見えなかった。ここで push 前後に止める。
#
# 採用条件は glogx 側 (src/glogx/issues/nextlink.go + parse.go の照合) と同じ:
#   next/ 直下で、その next/ の親が issue ディレクトリ自身か epic/<name> /
#   Readlink がちょうど ../<同名> / 指す先が通常ファイル /
#   直下の実エントリ名と**大文字小文字まで一致** (APFS は case-insensitive なので readlink と -f だけでは
#   通ってしまい、glogx の完全一致照合と割れる。敵対レビュー 2 周目で実測) /
#   README.md 等の meta ファイルは目印にしない
# glogx 側の検査を緩めてもここが残るよう、判定はこのスクリプトが独立に持つ (値は転記だが、
# 変えるなら両方を同じ commit で変える)。
# 🚨 glogx より**厳しい**向きにずれている (意図的。CI で止めたいものだから): glogx が「目印ではない」
# として無言で無視するもの (meta ファイル / .md 以外 / glogx が読まない場所 (done/next/ 等) の symlink)
# を、ここでは不正にする (dangling が viewer から永久に見えない形を残さない)。
# 「../<同名> / 通常ファイル / 直下エントリ名との大文字小文字一致」は glogx と同じ。ディレクトリ名
# (next / epic) の比較は glogx と同じく大文字小文字を無視する
#
# 変異検証のため、検査対象ディレクトリを第 1 引数で差し替えられる (既定 = repo の issues/):
#   d=$(mktemp -d); mkdir -p "$d/next"; : > "$d/010-bug-a.md"
#   ln -s ../010-bug-a.md "$d/next/010-bug-a.md"        # 正当 → 通る
#   ln -s ../010-bug-a.md "$d/next/011-bug-b.md"        # 別名 → 落ちる
#   ln -s /etc/passwd "$d/010-bug-c.md"                 # next/ 以外の symlink → 落ちる
#   tests/issues/test_next_links_valid.sh "$d"
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

issues_dir="${1:-issues}"
while [ "${issues_dir%/}" != "$issues_dir" ]; do issues_dir="${issues_dir%/}"; done # 末尾 / は全部落とす (残ると prefix 除去が外れて全件「読まない場所」になる)
if [ ! -d "$issues_dir" ]; then
  printf '✗ 検査対象ディレクトリが無い: %s\n' "$issues_dir" >&2
  exit 1
fi

# find の失敗は「symlink 0 件 = 合格」と同じ空出力になるので、rc を別に見る
links=$(find "$issues_dir" -type l -print | sort) || { printf '✗ find が失敗した\n' >&2; exit 1; }

bad=0
checked=0
while IFS= read -r link; do
  [ -n "$link" ] || continue
  checked=$((checked + 1))
  base=$(basename "$link")
  parent_of_next=$(dirname "$(dirname "$link")")
  lower() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }
  # ディレクトリ名の比較は大文字小文字を無視する (glogx は EqualFold で next / epic を読む)
  if [ "$(lower "$(basename "$(dirname "$link")")")" != "next" ]; then
    printf '✗ next/ 以外に symlink がある (issue ファイルは通常ファイルであること): %s\n' "$link" >&2
    bad=$((bad + 1)); continue
  fi
  # next/ の親は issue ディレクトリ自身か epic/<name> だけ (done/next/ 等は glogx が読まない場所)
  rel_parent="${parent_of_next#"$issues_dir"}"; rel_parent="${rel_parent#/}"
  rel_parent=$(lower "$rel_parent")
  case "$rel_parent" in
    ''|epic/*) ;;
    *) printf '✗ glogx が目印として読まない場所にある: %s\n' "$link" >&2; bad=$((bad + 1)); continue ;;
  esac
  case "$rel_parent" in
    epic/*/*) printf '✗ glogx が目印として読まない場所にある: %s\n' "$link" >&2; bad=$((bad + 1)); continue ;;
  esac
  case "$(lower "$base")" in
    readme.md|index.md|template.md)
      printf '✗ meta ファイルは目印にしない: %s\n' "$link" >&2; bad=$((bad + 1)); continue ;;
    *.md) ;;
    *) printf '✗ .md 以外の symlink がある (glogx は目印として読まない): %s\n' "$link" >&2; bad=$((bad + 1)); continue ;;
  esac
  target=$(readlink "$link")
  if [ "$target" != "../$base" ]; then
    printf '✗ 目印の指す先が ../<同名> でない: %s -> %s\n' "$link" "$target" >&2
    bad=$((bad + 1)); continue
  fi
  real="$parent_of_next/$base"
  if [ -L "$real" ] || [ ! -f "$real" ]; then
    printf '✗ 目印の指す先が通常ファイルとして存在しない (done/ へ動かしたなら symlink も消す): %s -> %s\n' "$link" "$real" >&2
    bad=$((bad + 1)); continue
  fi
  # 大文字小文字まで一致する実エントリがあること。find -name はパターン扱いで base の [ や * が
  # 効いてしまうので、列挙をリテラル (grep -Fx) で突き合わせる
  if ! grep -Fxq "$parent_of_next/$base" <<< "$(find "$parent_of_next" -maxdepth 1 -type f -print)"; then
    printf '✗ 直下のエントリ名と大文字小文字が一致しない (glogx は照合に落とす): %s\n' "$link" >&2
    bad=$((bad + 1)); continue
  fi
done <<< "$links"

if [ "$bad" -gt 0 ]; then
  printf '✗ next の目印: %d 件を検査、%d 件が不正 (%s)\n' "$checked" "$bad" "$issues_dir" >&2
  exit 1
fi
printf '✓ next の目印: symlink %d 件を検査、すべて有効 (%s)\n' "$checked" "$issues_dir"
