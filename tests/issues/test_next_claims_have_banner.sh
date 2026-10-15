#!/usr/bin/env bash
# next/ に claim の目印がある issue は、本文冒頭 (タイトル直下・最初の `## ` 見出しより前) に
# 担当者バナー (`**担当中` か `**着手中` で始まる強調) を持つことを検査する。
# 逆向きも見る: 目印の無い issue (done/ pending/ waiting/ と直下) の冒頭にバナーが残っていたら落とす。
# バナーは next にいる間だけの表示で (glogx の viewer は next から出すときに外す)、残ると done/ の issue が「誰かが作業中」に読める。issue 763。
#
# なぜ: `next/` の目印は next/ を見る入口にしか届かない。issue ファイルを直接開く / 別経路から照会する
# 相手には claim が存在しないのと同じで、2026-09-11 に照会の往復と推測による誤帰属を生んだ (retro 361)。
# claim ルール (_claude/issue-rules.d/claim-issue-in-next-and-push.md) は本文にも書けと要求しているが、
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

# 判定の規則は scripts/lib/claim_banner.awk (issue_done.sh の除去と共用。行頭に固定するのは散文・H1・HTML コメントの中の語を拾わないため)
BANNER_AWK="$ROOT_DIR/scripts/lib/claim_banner.awk"
[ -f "$BANNER_AWK" ] || { printf '✗ バナーの判定規則が無い: %s\n' "$BANNER_AWK" >&2; exit 1; }
# has_banner <file>: 冒頭にバナーがあれば 0
has_banner() { LC_ALL=C awk -v mode=find -f "$BANNER_AWK" "$1"; } # LC_ALL=C の理由は rev_stale の注記

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
# 起点の末尾に /: 起点が symlink のディレクトリでも辿る (BSD find は辿らず、0 件の緑になる)
find "$issues_dir/" \( -type l -o -type f \) -print0 > "$list" || { printf '✗ find が失敗した\n' >&2; exit 1; }

bad=0
checked=0
while IFS= read -r -d '' c; do
  # 目印になりうるのはパスに next を (大文字小文字を問わず) 含むものだけ。先に字句で落として、
  # 残りだけ tr で小文字にする (全 621 本で tr を起こしていて約 1.9 秒かかっていた。2026-10-02 実測)
  case "${c#"$issues_dir"/}" in *[Nn][Ee][Xx][Tt]*) ;; *) continue ;; esac
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

# --- 逆向き: 目印の無い issue の冒頭にバナーが残っていないか -------------------------------------
# 目印の置き場は group root (issues_dir か epic/<name>) の next/<同名>。issue の実体は group root の直下か、その状態ディレクトリ
# (done / pending / waiting) に居る。next/ の中の実ファイル (旧運用の claim) はそれ自体が claim なので見ない
# claim_of <rel>: issue の実体 (issues_dir からの相対) に対応する目印のパス (issues_dir からの相対)。対象外なら空。
# ディレクトリ名は大文字小文字を問わずに読む (順方向と同じく `Done/` `Epic/` も置き場として扱う)。目印は小文字の next/ だけを見る
claim_of() {
  local rel="$1" base="${1##*/}" root="" low
  low=$(printf '%s' "$rel" | tr '[:upper:]' '[:lower:]')
  IFS=/ read -r -a seg <<< "$low"
  IFS=/ read -r -a orig <<< "$rel"
  case "${#seg[@]}" in
    1) ;;
    2) case "${seg[0]}" in done | pending | waiting) ;; *) return 0 ;; esac ;;
    3) [ "${seg[0]}" = epic ] || return 0; root="${orig[0]}/${orig[1]}/" ;;
    4) [ "${seg[0]}" = epic ] || return 0
       case "${seg[2]}" in done | pending | waiting) ;; *) return 0 ;; esac
       root="${orig[0]}/${orig[1]}/" ;;
    *) return 0 ;;
  esac
  printf '%snext/%s' "$root" "$base"
}
# rev_stale <issues_dir>: 目印が無いのにバナーがある issue を 1 行ずつ出す。awk が 1 本でも失敗したら rc=1
# 🚨 awk は LC_ALL=C で読む。UTF-8 のロケールでは不正なバイトを含むファイルで awk が止まり、同じ xargs の束の残りを読まないまま
# 「0 件」に見える (敵対レビュー 2026-10-15 で再現)。バナーの判定はバイト列の一致なので C ロケールで変わらない
rev_stale() {
  local dir="$1" f rel claim found
  found=$(mktemp)
  # 起点の末尾に / : 起点が symlink のディレクトリでも辿る (BSD find は辿らず、0 件の緑になる)
  find "$dir/" -type f -iname '[0-9][0-9][0-9]*-*.md' -print0 | xargs -0 env LC_ALL=C awk -v mode=list -f "$BANNER_AWK" > "$found" ||
    { rm -f "$found"; return 1; }
  while IFS= read -r f; do
    rel="${f#"$dir"/}"
    rel="${rel#/}" # find "$dir/" は "$dir//x" の形で出すことがある
    claim="$(claim_of "$rel")"
    [ -n "$claim" ] || continue
    [ -L "$dir/$claim" ] || printf '%s\n' "$f"
  done < "$found"
  rm -f "$found"
}
# canary: 置き場ごとに 1 件ずつ (ディレクトリ名の大文字小文字は macOS のファイルシステムが区別しないので canary に置けない)。目印の無いバナーは拾い、目印のあるバナー・next の実ファイル・番号の無い md は拾わない
# (本走査と同じ rev_stale を通す。置き場の扱いを 1 つ外すと期待の集合と食い違う)
rv="$canary/rev"
mkdir -p "$rv/done" "$rv/pending" "$rv/waiting" "$rv/next" "$rv/epic/900/done" "$rv/epic/900/pending" "$rv/epic/900/next"
for f in 001-root.md done/002-done.md pending/003-pending.md waiting/004-waiting.md 005-upper.MD 1006-wide.md \
         epic/900/007-epic.md epic/900/done/008-epic-done.md epic/900/pending/009-epic-pending.md \
         010-claimed.md epic/900/011-epic-claimed.md next/012-old-style.md README.md; do
  printf '# t\n\n> 🚨 **担当中: x**（2026-09-19〜）\n\n## 概要\n' > "$rv/$f"
done
printf '# t\n\xff\n' > "$rv/done/013-bad-bytes.md" # 不正なバイト: UTF-8 のロケールの awk はここで止まる (LC_ALL=C で読む理由)
ln -s ../010-claimed.md "$rv/next/010-claimed.md"
ln -s ../011-epic-claimed.md "$rv/epic/900/next/011-epic-claimed.md"
want="$(printf '%s\n' 001-root.md done/002-done.md pending/003-pending.md waiting/004-waiting.md 005-upper.MD 1006-wide.md \
  epic/900/007-epic.md epic/900/done/008-epic-done.md epic/900/pending/009-epic-pending.md | sed "s|^|$rv/|" | sort)"
got="$(rev_stale "$rv" | sort)" || got="(走査が失敗)"
if [ "$got" != "$want" ]; then
  printf '✗ 逆向きの canary が期待と違う (目印の無いバナーの拾い方が壊れている)\n' >&2
  diff <(printf '%s\n' "$want") <(printf '%s\n' "$got") >&2 || true
  exit 1
fi
stale_out="$(rev_stale "$issues_dir")" || { printf '✗ 逆向きの走査が失敗した\n' >&2; exit 1; }
stale=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  printf '✗ claim の目印が無いのに本文冒頭に担当者バナーが残っている: %s\n' "$f" >&2
  # shellcheck disable=SC2016 # 案内文のバッククォートはリテラル
  printf '  → 作業中なら next/ の目印 (`ln -s ../<同名>`) を git add して同じ push に含める。完了したなら scripts/issue_done.sh で送る (バナーも外す)。\n    claim をやめたならバナーを外す (`LC_ALL=C awk -v mode=strip -f scripts/lib/claim_banner.awk` の出力で置き換える)\n' >&2
  stale=$((stale + 1))
done <<< "$stale_out"

if [ "$bad" -gt 0 ] || [ "$stale" -gt 0 ]; then
  printf '✗ claim のバナー: %d 件を検査、%d 件にバナーが無い / 目印の無いバナー %d 件 (%s)\n' "$checked" "$bad" "$stale" "$issues_dir" >&2
  exit 1
fi
printf '✓ claim のバナー: %d 件を検査、すべてバナーあり。目印の無いバナーも 0 件 (%s。canary で判定と置き場の扱いを確認)\n' "$checked" "$issues_dir"
