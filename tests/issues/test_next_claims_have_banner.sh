#!/usr/bin/env bash
# next/ に claim の目印がある issue は、本文冒頭 (タイトル直下・最初の `## ` 見出しより前) に
# 担当者バナー (`**担当中` か `**着手中` で始まる強調) を持つことを検査する。
#
# なぜ: `next/` の目印は next/ を見る入口にしか届かない。issue ファイルを直接開く / 別経路から照会する
# 相手には claim が存在しないのと同じで、2026-09-11 に照会の往復と推測による誤帰属を生んだ (retro 361)。
# claim ルール (_claude/rules/claim-issue-in-next-and-push.md) は本文にも書けと要求しているが、
# next/ への push は hook が促す一方バナーには促す仕掛けが無く、push した時点で claim が閉じたように感じて落ちる。
# glogx の `n` も symlink だけを作るので hook では捕まらない。ここ (CI) で止める。issue 403。
#
# 対象: issues/next/ と issues/epic/<name>/next/ の直下にある .md (symlink の目印と、旧運用の実ファイル)。
# 目印そのものの形の正しさは test_next_links_valid.sh が見る (ここでは解決できたものだけを読む)。
# 🚨 バナーの書式を変えるなら claim ルールの記述とこのパターンを同じ commit で変える。
#
# 検査対象ディレクトリは第 1 引数で差し替えられる (既定 = repo の issues/。変異検証用)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

issues_dir="${1:-issues}"
while [ "${issues_dir%/}" != "$issues_dir" ]; do issues_dir="${issues_dir%/}"; done # 末尾 / は全部落とす (残ると prefix 除去が外れ、段数が合わず 0 件の緑になる。敵対レビュー 2 周目)
if [ ! -d "$issues_dir" ]; then
  printf '✗ 検査対象ディレクトリが無い: %s\n' "$issues_dir" >&2
  exit 1
fi

# has_banner <file>: 最初の `## ` 見出しより前に、行頭の `**担当中` / `**着手中` (前に `> ` と `🚨 ` を許す) が
# あれば 0。行頭に固定するのは散文・H1・HTML コメントの中の語を拾わないため。コードフェンスの中は読まない
has_banner() {
  awk '/^ {0,3}(```|~~~)/{fence=!fence; next} fence{next} /^## /{exit}
       /^(> )?(🚨 )?\*\*(担当中|着手中)/{found=1; exit} END{exit !found}' "$1"
}

# 🚨 canary: 本走査と同じ関数に既知の入力を通す (抽出が壊れて全件「バナーなし」/「あり」になるのを検出)
canary=$(mktemp -d)
list=""
trap 'rm -rf "$canary" ${list:+"$list"}' EXIT
printf '# t\n\n> 🚨 **担当中: x**（2026-09-19〜）\n\n## 概要\n' > "$canary/yes.md"
printf '# t\n\n## 概要\n\n**担当中: x**\n' > "$canary/late.md"   # 見出しより後 = 冒頭ではない
printf '# t\n\n本文\n' > "$canary/no.md"
# shellcheck disable=SC2016 # バッククォートは markdown のフェンス (展開させない)
printf '# t\n\n```\n**担当中: x**\n```\n過去は**担当中**だった\n' > "$canary/fence.md"   # フェンス内・散文中は数えない
if ! has_banner "$canary/yes.md" || has_banner "$canary/late.md" || has_banner "$canary/no.md" ||
  has_banner "$canary/fence.md"; then
  printf '✗ バナー判定の canary が期待と違う (awk の判定が壊れている)\n' >&2
  exit 1
fi

# 🚨 ディレクトリ名 (next / epic) と拡張子は大文字小文字を無視して拾う。test_next_links_valid.sh と glogx は
# `NEXT/` `Epic/` `.MD` も有効な目印と読むので、find -path の完全一致では有効な claim を黙って飛ばす
# (敵対レビュー 2026-09-19 で実測)。名前に改行を含んでも割れないよう -print0 で読む。
# find の失敗は「claim 0 件 = 合格」と同じ空出力になるので rc を別に見る
list=$(mktemp)
find "$issues_dir" \( -type l -o -type f \) -print0 > "$list" || { printf '✗ find が失敗した\n' >&2; exit 1; }

bad=0
checked=0
while IFS= read -r -d '' c; do
  rel=$(printf '%s' "${c#"$issues_dir"/}" | tr '[:upper:]' '[:lower:]')
  # 置き場は next/<f> か epic/<name>/next/<f> だけ (段数で判定する。case の * は / もまたぐ)。
  # それ以外の深さの目印は test_next_links_valid.sh が落とす
  IFS=/ read -r -a seg <<< "$rel"
  case "${#seg[@]}" in
    2) [ "${seg[0]}" = next ] || continue ;;
    4) [ "${seg[0]}" = epic ] && [ "${seg[2]}" = next ] || continue ;;
    *) continue ;;
  esac
  case "${rel##*/}" in readme.md | index.md | template.md) continue ;; *.md) ;; *) continue ;; esac
  [ -f "$c" ] || continue # dangling は test_next_links_valid.sh の担当
  checked=$((checked + 1))
  if ! has_banner "$c"; then
    printf '✗ claim されているのに本文冒頭に担当者バナーが無い: %s\n' "$c" >&2
    # 案内文のバッククォートは書式をそのまま見せるためのリテラル (展開させない)
    # shellcheck disable=SC2016
    printf '  → タイトル直下に `> 🚨 **担当中: <セッション名>**（YYYY-MM-DD〜）` を書き、claim と同じ commit で push する\n' >&2
    bad=$((bad + 1))
  fi
done < "$list"

if [ "$bad" -gt 0 ]; then
  printf '✗ claim のバナー: %d 件を検査、%d 件にバナーが無い (%s)\n' "$checked" "$bad" "$issues_dir" >&2
  exit 1
fi
printf '✓ claim のバナー: %d 件を検査、すべてバナーあり (%s。canary 4 件で判定を確認)\n' "$checked" "$issues_dir"
