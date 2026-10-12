#!/bin/bash
# ./tmp のスクラッチを掃除する本体 (`make clean-tmp` が呼ぶ。使い方と「消す前に確かめること」は Makefile の clean-tmp の注記)。
#
#   DAYS=30 DRY_RUN=1 scripts/clean_tmp.sh   # 消さずに一覧と理由だけ出す
#
# 判定 (issue 729):
#   - 対象は <repo の root>/tmp の直下のエントリのうち、自身の mtime が DAYS 日より古いもの
#   - 🚨 フォルダは**中に DAYS 日以内に更新された子孫 (ファイル・フォルダ) が 1 つでもあれば残す**。フォルダの mtime は直下の出入りでしか
#     変わらず、中のファイルを書き換えても古いままなので、自身の mtime だけでは使っている中身ごと消す
#   - 🚨 中を調べる find が失敗したら (読めない子孫など) **判定不能として残す**。出力が空でも「新しいものは無い」と読まない
#   - 中のどこかに .git (worktree の印のファイル・clone のフォルダ) があるエントリは残す。🚨 worktree をパスで照合しない:
#     git の登録のパスと実体の照合は、大文字小文字 (APFS)・後から symlink に差し替えた経路・動かしただけの worktree で
#     2 周続けて外れ、作業ごと消せた (issue 729 の敵対レビュー)。中身の印は場所の書き方に依らない (印の種類は find_git)
#     🚨 検出しない: 名前に改行を含む git dir・印がまったく無い作業ツリー (外に置いた GIT_DIR で作った・worktree の .git ファイルが消えた)。中身だけでは見分けられない
#   - 残したものは理由を出す (黙って外さない)。symlink は辿らず、link 自身を消す
#   - 未確認 (直していない): tmp の下にマウントポイントがあると rm -rf がその先へ入りうる (旧実装から同じ。issue 729 に記録)
#   - 1 件ずつ「調べる → すぐ消す」(全部を調べてから消すと、調べた後の書き込みを消す窓が件数ぶん広がる)
#
# 🚨 消す場所は <repo の root>/tmp (の実体がその root の直下にあるもの) に限る。cwd が repo の外・tmp が symlink なら何もせずに止める
# (テストは一時ディレクトリに git の repo を作って、その中で走らせる)。
set -u
unset CDPATH
days=${DAYS:-30}
case $days in '' | *[!0-9]*) echo "clean-tmp: DAYS は 0 以上の整数 ($days)" >&2; exit 2 ;; esac
# 🚨 DRY_RUN は 1 (一覧だけ) か 空・0 (消す) だけを受ける。知らない値 (true / yes) を「消す」に読むと、確かめるつもりの実行で消す
case ${DRY_RUN:-} in
'' | 0) dry=0 ;;
1) dry=1 ;;
*) echo "clean-tmp: DRY_RUN は 1 (一覧だけ) か 0 (消す) ($DRY_RUN)。何もしない" >&2; exit 2 ;;
esac

root=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "clean-tmp: git の repo の中で走らせる" >&2; exit 2; }
root=$(cd "$root" && pwd -P) || exit 2
tmp="$root/tmp"
[ -e "$tmp" ] || { echo "tmp/ が無い (掃除するものなし)"; exit 0; }
if [ -L "$tmp" ] || [ ! -d "$tmp" ] || [ "$(cd "$tmp" && pwd -P)" != "$tmp" ]; then
	echo "clean-tmp: $tmp が repo の直下の実体のフォルダでないので何もしない" >&2
	exit 2
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/clean-tmp.XXXXXX") || exit 2
trap 'rm -rf "$work"' EXIT

if ! find "$tmp" -maxdepth 1 -mindepth 1 -mtime +"$days" -print0 > "$work/cands"; then
	echo "clean-tmp: tmp の一覧を読めない。何もしない" >&2
	exit 2
fi

# find_git は $1 の中の git の印を 1 つ出す (無ければ空)。印は 2 種: .git (worktree の印のファイル・clone のフォルダ。大文字小文字を問わない:
# APFS では .GIT でも git が読む) と、git dir の形 (HEAD (ファイルか symlink。大文字小文字を問わない) と objects/・refs/ が並ぶフォルダ。bare や --separate-git-dir の置き場は
# .git を持たないので、これが無いと worktree を残して commit の置き場の方を消した。issue 729 の敵対レビュー 3 周目)。find の失敗は rc で返す
find_git() {
	local f d IFS
	f=$(find "$1" \( -iname .git -o \( \( -type f -o -type l \) -iname HEAD \) \) -print) || return 1
	# 🚨 here-string (<<<) で回さない: bash 3.2 は一時ファイルに書くので、ディスクが一杯だとリダイレクトが失敗し、
	# 印を見ないまま rc=0 (= 消す) を返した (issue 729 の敵対レビュー 4 周目、ulimit -f 0 で再現)。変数を改行で割って回す
	IFS=$'\n'
	set -f
	for d in $f; do
		case ${d##*/} in
		[Hh][Ee][Aa][Dd]) [ -d "${d%/*}/objects" ] && [ -d "${d%/*}/refs" ] && { set +f; printf '%s\n' "${d%/*}"; return 0; } ;;
		*) set +f; printf '%s\n' "$d"; return 0 ;;
		esac
	done
	set +f
	return 0
}

n=0 kb=0 kept=0
while IFS= read -r -d '' c; do
	rel=${c#"$root"/}
	if [ -d "$c" ] && [ ! -L "$c" ]; then
		if ! repo=$(find_git "$c" 2>"$work/err"); then
			echo "  残す (判定不能: 中を読めない。$(head -1 "$work/err")): $rel"
			kept=$((kept + 1))
			continue
		fi
		if [ -n "$repo" ]; then
			echo "  残す (中に git の作業ツリーか置き場がある: ${repo#"$root"/}): $rel"
			kept=$((kept + 1))
			continue
		fi
		if ! newer=$(find "$c" -mindepth 1 ! -mtime +"$days" -print -quit 2>"$work/err"); then
			echo "  残す (判定不能: 中を読めない。$(head -1 "$work/err")): $rel"
			kept=$((kept + 1))
			continue
		fi
		if [ -n "$newer" ]; then
			echo "  残す (中に ${days} 日以内の更新がある: ${newer#"$root"/}): $rel"
			kept=$((kept + 1))
			continue
		fi
	fi
	size=$(du -sk "$c" 2>/dev/null | awk '{print $1}')
	kb=$((kb + ${size:-0}))
	n=$((n + 1))
	if [ "$dry" = 1 ]; then
		echo "  消す: $rel"
	else
		rm -rf -- "$c" || { echo "clean-tmp: 消せなかった: $rel" >&2; exit 1; }
		echo "  消した: $rel"
	fi
done < "$work/cands"

if [ "$n" -eq 0 ] && [ "$kept" -eq 0 ]; then
	echo "✓ ${days} 日より古い tmp のエントリは 0 件"
elif [ "$dry" = 1 ]; then
	echo "(DRY_RUN) ${n} 件 / $((kb / 1024))MB が対象 (残すもの ${kept} 件)。消すには DRY_RUN を外す"
else
	echo "✓ ${n} 件 / $((kb / 1024))MB を削除した (残したもの ${kept} 件。残り $(find "$tmp" -maxdepth 1 -mindepth 1 | wc -l | tr -d ' ') エントリ)"
fi
