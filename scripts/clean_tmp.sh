#!/bin/bash
# ./tmp のスクラッチを掃除する本体 (`make clean-tmp` が呼ぶ。使い方と「消す前に確かめること」は Makefile の clean-tmp の注記)。
#
#   DAYS=30 DRY_RUN=1 scripts/clean_tmp.sh   # 消さずに一覧と理由だけ出す
#
# 判定:
#   - 対象は <repo の root>/tmp の直下のエントリのうち、自身の mtime が DAYS 日より古いもの
#   - 🚨 フォルダは**中に DAYS 日以内に更新された子孫 (ファイル・フォルダ) が 1 つでもあれば残す**。フォルダの mtime は直下の出入りでしか
#     変わらず、中のファイルを書き換えても古いままなので、自身の mtime だけでは使っている中身ごと消す
#   - 🚨 中を調べる find が失敗したら (読めない子孫など) **判定不能として残す**。出力が空でも「新しいものは無い」と読まない
#   - 中のどこかに .git (worktree の印のファイル・clone のフォルダ) があるエントリは残す。🚨 worktree をパスで照合しない:
#     git の登録のパスと実体の照合は、大文字小文字 (APFS)・後から symlink に差し替えた経路・動かしただけの worktree で
#     2 周続けて外れ、作業ごと消せた。中身の印は場所の書き方に依らない (印の種類は find_git)
#     🚨 検出しない: 名前に改行を含む git dir・印がまったく無い作業ツリー (外に置いた GIT_DIR で作った・worktree の .git ファイルが消えた)。中身だけでは見分けられない
#   - 残したものは理由を出す (黙って外さない)。symlink は辿らず、link 自身を消す
#   - 未確認 (直していない): tmp の下にマウントポイントがあると rm -rf がその先へ入りうる (これまでと同じ)
#   - skill の dir (`bin/skill-tmpdir` が tmp/<skill名>/ に印 .skill-tmpdir を置く) は丸ごと判定しない。使われ続けて中に
#     新しい session の dir がある限り、上の規則では古い session ごと残り続けるので、中の session ごとの dir を 1 件ずつ同じ規則で判定する。
#     skill の dir と印は残す。印や skill の dir が symlink なら降りない (普通のエントリとして扱う)。🚨 skill の dir の直下に git の印
#     (.git / HEAD) があるなら降りない: 子だけを見ると、兄弟の .git (worktree の印) や作業ツリーの中身を消す
#   - 判定の始めにエントリ自身がまだ DAYS 日より古いかを、消す前に skill の dir の子なら親がまだ skill の dir かを取り直す。一覧を取ってから
#     判定するまでの間に、直下のエントリ全部の判定が挟まる (判定の始めから rm までの間の書き込みは見ない)
#   - 🚨 rm はパスを解決し直す。消すときはサブシェルで親へ cd し、/bin/pwd -P が想定の実体と一致してから相対名 (./名前) で消す。確認の後に
#     親や祖先 (tmp 自身を含む) を symlink に差し替えられても、cwd は実体に固定されているので外へ届かない。
#     🚨 builtin の pwd -P は cwd を見ず $PWD の文字列を解決し直すだけで、cd の後に親を実体へ戻されると外の cwd を「一致」と答える
#     (3 周目)。getcwd を使う /bin/pwd -P で比べ、基準の root / tmp も同じ /bin/pwd -P で取る (綴りを揃える)
#   - 1 件ずつ「調べる → すぐ消す」(全部を調べてから消すと、調べた後の書き込みを消す窓が件数ぶん広がる)
#
# 🚨 消す場所は <repo の root>/tmp (の実体がその root の直下にあるもの) に限る。cwd が repo の外・tmp が symlink なら何もせずに止める
# (テストは一時ディレクトリに git の repo を作って、その中で走らせる)。
set -u
unset CDPATH
# 🚨 継承した GIT_DIR / GIT_WORK_TREE は cwd と別の repo を root にする (hook・rebase --exec から呼ばれた経路)。cwd だけで決める
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR
days=${DAYS:-30}
case $days in '' | *[!0-9]*) echo "clean-tmp: DAYS は 0 以上の整数 ($days)" >&2; exit 2 ;; esac
# 🚨 DRY_RUN は 1 (一覧だけ) か 空・0 (消す) だけを受ける。知らない値 (true / yes) を「消す」に読むと、確かめるつもりの実行で消す
case ${DRY_RUN:-} in
'' | 0) dry=0 ;;
1) dry=1 ;;
*) echo "clean-tmp: DRY_RUN は 1 (一覧だけ) か 0 (消す) ($DRY_RUN)。何もしない" >&2; exit 2 ;;
esac

root=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "clean-tmp: git の repo の中で走らせる" >&2; exit 2; }
root=$(cd "$root" && /bin/pwd -P) || exit 2
tmp="$root/tmp"
[ -e "$tmp" ] || { echo "tmp/ が無い (掃除するものなし)"; exit 0; }
if [ -L "$tmp" ] || [ ! -d "$tmp" ] || [ "$(cd "$tmp" && /bin/pwd -P)" != "$tmp" ]; then
	echo "clean-tmp: $tmp が repo の直下の実体のフォルダでないので何もしない" >&2
	exit 2
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/clean-tmp.XXXXXX") || exit 2
trap 'rm -rf "$work"' EXIT

# is_skill_dir は $1 が skill-tmpdir の作った skill の dir か (実体のフォルダで、印が実体のファイル)
# 直下に git の印があるものは skill の dir にしない (find が失敗しても skill の dir にしない = 丸ごと判定に倒す)
is_skill_dir() {
	local g
	[ -d "$1" ] && [ ! -L "$1" ] && [ -f "$1/.skill-tmpdir" ] && [ ! -L "$1/.skill-tmpdir" ] || return 1
	g=$(find "$1" -maxdepth 1 -mindepth 1 \( -iname .git -o -iname HEAD \) -print -quit 2>/dev/null) || return 1
	[ -z "$g" ]
}

if ! find "$tmp" -maxdepth 1 -mindepth 1 -mtime +"$days" -print0 > "$work/cands"; then
	echo "clean-tmp: tmp の一覧を読めない。何もしない" >&2
	exit 2
fi
if ! find "$tmp" -maxdepth 1 -mindepth 1 -type d -print0 > "$work/tops"; then
	echo "clean-tmp: tmp の一覧を読めない。何もしない" >&2
	exit 2
fi
n=0 kb=0 kept=0
# skill の dir はここで 1 回だけ決める (下の 2 つのループで判定がずれると、同じものを丸ごとと子の両方で数える)
: > "$work/skillcands"
: > "$work/skilldirs"
while IFS= read -r -d '' s; do
	is_skill_dir "$s" || continue
	printf '%s\0' "$s" >> "$work/skilldirs"
	# 読めない dir は上の is_skill_dir の find で先に外れる (丸ごと判定に回る)。ここで失敗するのはその後に読めなくなった競合だけ
	if ! find "$s" -maxdepth 1 -mindepth 1 ! -iname .skill-tmpdir -mtime +"$days" -print0 > "$work/one" 2>"$work/err"; then
		echo "  残す (判定不能: 中を読めない。$(head -1 "$work/err")): ${s#"$root"/}"
		kept=$((kept + 1))
		continue
	fi
	cat "$work/one" >> "$work/skillcands"
done < "$work/tops"
in_skilldirs() {
	local s
	while IFS= read -r -d '' s; do [ "$s" = "$1" ] && return 0; done < "$work/skilldirs"
	return 1
}

# find_git は $1 の中の git の印を 1 つ出す (無ければ空)。印は 2 種: .git (worktree の印のファイル・clone のフォルダ。大文字小文字を問わない:
# APFS では .GIT でも git が読む) と、git dir の形 (HEAD (ファイルか symlink。大文字小文字を問わない) と objects/・refs/ が並ぶフォルダ。bare や --separate-git-dir の置き場は
# .git を持たないので、これが無いと worktree を残して commit の置き場の方を消した)。find の失敗は rc で返す
find_git() {
	local f d IFS
	f=$(find "$1" \( -iname .git -o \( \( -type f -o -type l \) -iname HEAD \) \) -print) || return 1
	# 🚨 here-string (<<<) で回さない: bash 3.2 は一時ファイルに書くので、ディスクが一杯だとリダイレクトが失敗し、
	# 印を見ないまま rc=0 (= 消す) を返した (ulimit -f 0 で再現)。変数を改行で割って回す
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

# judge は 1 件を判定し、消してよければすぐ消す (直下のエントリと skill の dir の中の session の dir で同じ規則)
judge() {
	local c=$1 parent=${2:-} rel repo newer size
	rel=${c#"$root"/}
	if [ -z "$(find "$c" -prune -mtime +"$days" -print 2>/dev/null)" ]; then
		echo "  残す (一覧の後に更新された・読めない): $rel"
		kept=$((kept + 1))
		return 0
	fi
	if [ -d "$c" ] && [ ! -L "$c" ]; then
		if ! repo=$(find_git "$c" 2>"$work/err"); then
			echo "  残す (判定不能: 中を読めない。$(head -1 "$work/err")): $rel"
			kept=$((kept + 1))
			return 0
		fi
		if [ -n "$repo" ]; then
			echo "  残す (中に git の作業ツリーか置き場がある: ${repo#"$root"/}): $rel"
			kept=$((kept + 1))
			return 0
		fi
		if ! newer=$(find "$c" -mindepth 1 ! -mtime +"$days" -print -quit 2>"$work/err"); then
			echo "  残す (判定不能: 中を読めない。$(head -1 "$work/err")): $rel"
			kept=$((kept + 1))
			return 0
		fi
		if [ -n "$newer" ]; then
			echo "  残す (中に ${days} 日以内の更新がある: ${newer#"$root"/}): $rel"
			kept=$((kept + 1))
			return 0
		fi
	fi
	size=$(du -sk "$c" 2>/dev/null | awk '{print $1}')
	kb=$((kb + ${size:-0}))
	n=$((n + 1))
	if [ -n "$parent" ] && ! is_skill_dir "$parent"; then
		echo "  残す (親の ${parent#"$root"/} が skill の dir でなくなった): $rel"
		kept=$((kept + 1))
		n=$((n - 1)) kb=$((kb - ${size:-0}))
		return 0
	fi
	if [ "$dry" = 1 ]; then
		echo "  消す: $rel"
		return 0
	fi
	# 親へ cd して実体を確かめてから相対名で消す (rc 3 = 親が実体でなくなった)
	local dirp=${c%/*} rc=0
	(cd "$dirp" 2>/dev/null && [ "$(/bin/pwd -P)" = "$dirp" ] || exit 3; rm -rf -- "./${c##*/}") || rc=$?
	if [ "$rc" -eq 3 ]; then
		echo "  残す (親の ${dirp#"$root"/} へ入れない・実体の dir でなくなった): $rel"
		kept=$((kept + 1))
		n=$((n - 1)) kb=$((kb - ${size:-0}))
		return 0
	fi
	[ "$rc" -eq 0 ] || { echo "clean-tmp: 消せなかった: $rel" >&2; exit 1; }
	echo "  消した: $rel"
}
while IFS= read -r -d '' c; do
	in_skilldirs "$c" && continue # 中の session の dir を下で判定する
	judge "$c"
done < "$work/cands"
while IFS= read -r -d '' c; do
	judge "$c" "${c%/*}"
done < "$work/skillcands"

if [ "$n" -eq 0 ] && [ "$kept" -eq 0 ]; then
	echo "✓ ${days} 日より古い tmp のエントリは 0 件"
elif [ "$dry" = 1 ]; then
	echo "(DRY_RUN) ${n} 件 / $((kb / 1024))MB が対象 (残すもの ${kept} 件)。消すには DRY_RUN を外す"
else
	echo "✓ ${n} 件 / $((kb / 1024))MB を削除した (残したもの ${kept} 件。残り $(find "$tmp" -maxdepth 1 -mindepth 1 | wc -l | tr -d ' ') エントリ)"
fi
