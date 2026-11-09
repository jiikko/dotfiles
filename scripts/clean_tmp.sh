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
#     ただし、エントリの中に作業ツリーごと閉じた使い捨ての repo (remote の無い `git init` の fixture) は印に数えない (disposable_gitdir)。
#     数えると、レビュー・計測で fixture を 1 つ作った作業 dir が古くなっても永久に残る
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
# shellcheck source=bin/lib/git_location_env.sh
. "$(dirname "$(readlink -f "$0")")/../bin/lib/git_location_env.sh" ||
  { echo "clean-tmp: bin/lib/git_location_env.sh を読めない" >&2; exit 2; }
git_location_env_unset || { echo "clean-tmp: 継承した git の場所の環境変数 (GIT_DIR など) を外せない" >&2; exit 2; }
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

# tmp の中で「どこ (持ち主) が、どこ (先。実体のパス) を使っているか」の組。disposable_gitdir が、別の場所から使われている repo を
# 使い捨てにしないために使う (borrowed)。持ち主と先は 3 種:
#   - git dir と、その objects の実体 (objects を symlink で別のエントリへ置いた形・git-new-workdir の形)
#   - git dir と、それが alternates で借りている objects (clone -s)。🚨 alternates のファイルを探して読まない: 名前の大文字小文字 (APFS)・
#     info / objects の symlink・引用符の行を字面で追うと抜ける (1・2 周目の敵対的レビューで 4 形を再現)。git 自身に答えさせる
#     (count-objects -v の alternate: の行。git が解いた後のパス)
#   - tmp の中の symlink と、その先 (.git ごとの symlink。find は symlink の下へ降りないので、HEAD の列挙では見つからない)
# 🚨 列挙は -print0 で読む: 改行で割ると、改行を含むパスの git dir が黙って列挙から外れ、貸している側を消した (3 周目に再現)
ref_owner=() ref_target=() refs_ok=1 refs_why=''
addref() { ref_owner+=("$1"); ref_target+=("$2"); }
if find "$tmp" \( -type f -o -type l \) -iname HEAD -print0 > "$work/heads" 2>/dev/null &&
	find "$tmp" -type l -print0 > "$work/links" 2>/dev/null; then
	while IFS= read -r -d '' h; do
		g=${h%/*}
		[ -d "$g/objects" ] && [ -d "$g/refs" ] || continue
		r=$(cd "$g/objects" 2>/dev/null && /bin/pwd -P) && addref "$g" "$r"
		if ! out=$(GIT_OPTIONAL_LOCKS=0 git --git-dir="$g" -c core.fsmonitor=false count-objects -v 2>/dev/null); then
			refs_ok=0 refs_why="借りている先を git が答えない repo がある: ${g#"$root"/}" # 壊れた・読めない config
			continue
		fi
		IFS=$'\n'
		set -f
		for l in $out; do
			case $l in
			'alternate: "'*) refs_ok=0 refs_why="借りている先を git が引用符で書いた repo がある: ${g#"$root"/}" ;; # 解かずに、借りられている先を知らない側に倒す
			'alternate: '*)
				# 指す先が無いものは、どの repo も借りていない (借りた側が既に壊れている)
				r=$(cd "${l#alternate: }" 2>/dev/null && /bin/pwd -P) && addref "$g" "$r"
				;;
			esac
		done
		set +f
		unset IFS
	done < "$work/heads"
	while IFS= read -r -d '' l; do
		t=$(readlink -f "$l" 2>/dev/null) && [ -e "$t" ] && addref "$l" "$t" # 先の無い symlink は何も使っていない
	done < "$work/links"
else
	refs_ok=0 refs_why="tmp の中の git の置き場と symlink を列挙できない"
fi
[ "$refs_ok" = 1 ] || echo "  使い捨ての git repo も残す (${refs_why}。どの repo が別の場所から使われているかを決められない。その repo を直すか消すと再開する)"

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
# 使い捨ての repo (disposable_gitdir / disposable_gitfile を満たすもの) は印に数えない。
find_git() {
	local c=$1 f d IFS
	f=$(find "$c" \( -iname .git -o \( \( -type f -o -type l \) -iname HEAD \) \) -print) || return 1
	# 🚨 here-string (<<<) で回さない: bash 3.2 は一時ファイルに書くので、ディスクが一杯だとリダイレクトが失敗し、
	# 印を見ないまま rc=0 (= 消す) を返した (ulimit -f 0 で再現)。変数を改行で割って回す
	IFS=$'\n'
	set -f
	for d in $f; do
		case ${d##*/} in
		[Hh][Ee][Aa][Dd])
			# 名前がちょうど .git の dir の HEAD は、その .git 自身の印として下の .git の枝で判定する (find はその .git も出す)。
			# 🚨 .git の中の他の git dir (submodule の modules/*) は飛ばさない: origin を持つ clone で、まだ push していない commit があり得る
			[ "${d%/*}" != "${d%/*/*}/.git" ] || continue
			[ -d "${d%/*}/objects" ] && [ -d "${d%/*}/refs" ] && { set +f; printf '%s\n' "${d%/*}"; return 0; }
			;;
		.git)
			disposable_gitdir "$d" "$c" && continue
			disposable_gitfile "$d" "$c" && continue
			set +f; printf '%s\n' "$d"; return 0
			;;
		*) set +f; printf '%s\n' "$d"; return 0 ;;
		esac
	done
	set +f
	return 0
}

# resolve_dir は dir $1 の実体のパスを出す (解決できなければ rc≠0)。inside は $2 が $1 の中 (自身は含まない) か
resolve_dir() { (cd "$1" 2>/dev/null && /bin/pwd -P); }
inside() { case $2 in "$1"/*) return 0 ;; esac; return 1; }

# disposable_gitdir は $1 (名前が .git の dir) が、エントリ $2 の中に作業ツリーごと閉じた使い捨ての repo (`git init` した fixture) かを返す。
# 1 つでも確かめられなければ使い捨てとしない (= 今までどおり残す)。🚨 外から使われうる形はどれも残す:
#   - 親に .git しか無い: 名前が .git の --separate-git-dir の置き場を、外の作業ツリーの .git ファイルが指している形と見分けられない
#   - [remote / [include(If) の節・worktree = の行: clone (まだ push していない commit があり得る)・外の設定・外の作業ツリー (core.worktree)
#   - commondir: 別の repo の worktree 側の git dir
#   - config.worktree (extensions.worktreeConfig): core.worktree がそこへ移る (sparse-checkout init でも有効になる)
#   - 親にある作業ファイルが index と食い違う (ls-files -d が出す): 親に兄弟がある --separate-git-dir の置き場で、作業ツリーは外にある
#     (`git init --separate-git-dir=X/.git W` は X/.git/config に core.worktree を書かない。2026-11-09 に実測)
#   - worktrees/* の作業ツリーが外 (登録済みの worktree の commit と未コミットの変更を失う)。指す先を解決できない (prune 待ち) も残す
#   - tmp の中の別の repo が alternates (clone --shared) でこの objects を借りている (借りた側の objects が壊れる)
#   - core.bare が true (名前が .git の bare。外から push される置き場で、index が無いので ls-files -d も空になる)
# 検出しない (中身から見えない): 外の dir がこの .git を path で直接使う形 (GIT_DIR・git-new-workdir の symlink。使っている間は
# objects・refs が書かれて中身が新しくなるので、DAYS 日使われなかったときだけ消える)・tmp の外の repo が alternates で借りる形
# (🚨 借りる側は読むだけなので、使われている間も消える。tmp の fixture を外から clone -s で借りて使い続ける使い方を前提にしない)・
# 外の作業ツリーの tracked のファイルが、たまたま全部親にも同名である --separate-git-dir の置き場。
# alternates の一覧は起動時に 1 回だけ取る (実行中に clone -s で借りた側は見ない。判定から rm までの書き込みを見ないのと同じ)
disposable_gitdir() {
	local g=$1 c=$2 p n ws w t d rc IFS
	[ "${g##*/}" = .git ] && [ -d "$g" ] && [ ! -L "$g" ] || return 1
	p=${g%/*}
	n=$(find "$p" -maxdepth 1 -mindepth 1 ! -name .git -print -quit 2>/dev/null) || return 1
	[ -n "$n" ] || return 1
	[ -f "$g/config" ] && [ ! -L "$g/config" ] && [ -r "$g/config" ] || return 1
	[ ! -e "$g/commondir" ] && [ ! -L "$g/commondir" ] || return 1
	# 🚨 config を grep で読まない: 節の見出しと同じ行の key・先頭の BOM など、git が読む書き方を字面では追い切れない。git 自身に読ませる
	# (--file は include を辿らず、そのファイルだけを読む。読むだけで何も実行しない)
	# 🚨 読めない config でも git config は警告だけ出して rc=1 (当たらない) を返す。stderr が出たら読めなかったとみなす
	n=$(git config --file "$g/config" --get-regexp '^(remote|include|includeif)\.|^core\.worktree$|^extensions\.worktreeconfig$|^branch\..*\.remote$' 2>&1 >/dev/null)
	rc=$?
	[ "$rc" -eq 1 ] && [ -z "$n" ] || return 1 # 0 = 当たった、それ以外 = 読めない
	n=$(git config --file "$g/config" --type=bool --get core.bare 2>/dev/null)
	rc=$?
	[ "$rc" -eq 1 ] || { [ "$rc" -eq 0 ] && [ "$n" = false ]; } || return 1 # 名前が .git の bare (外から push される置き場)
	n=$(find "$g" -maxdepth 3 -iname config.worktree -print -quit 2>/dev/null) || return 1
	[ -z "$n" ] || return 1
	borrowed "$c" && return 1
	# 🚨 fsmonitor は config に書いたコマンドを ls-files が起こす。fixture の config の中身を実行しないように切る
	n=$(GIT_OPTIONAL_LOCKS=0 git --git-dir="$g" --work-tree="$p" -c core.fsmonitor=false ls-files -d 2>/dev/null) || return 1
	[ -z "$n" ] || return 1
	[ -e "$g/worktrees" ] || [ -L "$g/worktrees" ] || return 0
	[ -d "$g/worktrees" ] && [ ! -L "$g/worktrees" ] || return 1
	ws=$(find "$g/worktrees" -maxdepth 1 -mindepth 1 -print) || return 1
	IFS=$'\n'
	for w in $ws; do
		[ -f "$w/gitdir" ] && [ ! -L "$w/gitdir" ] || return 1
		t=$(head -1 "$w/gitdir") && [ -n "$t" ] || return 1
		case $t in /*) ;; *) t="$w/$t" ;; esac # worktree.useRelativePaths は $w からの相対
		d=$(resolve_dir "${t%/*}") || return 1
		inside "$c" "$d" || return 1
	done
	return 0
}

# borrowed は、エントリ $1 の外の持ち主 (git dir・symlink) が、$1 の中を使っているか (組を決められなかったら使われているとみなす)。
# 🚨 照合は git dir ではなくエントリで行う: 貸す側の objects を symlink で同じエントリの別の場所へ置くと、git dir のパスと実体が食い違う
borrowed() {
	local i=0
	[ "$refs_ok" = 1 ] || return 0
	while [ "$i" -lt "${#ref_owner[@]}" ]; do
		if { [ "${ref_target[$i]}" = "$1" ] || inside "$1" "${ref_target[$i]}"; } && ! inside "$1" "${ref_owner[$i]}"; then
			return 0
		fi
		i=$((i + 1))
	done
	return 1
}

# disposable_gitfile は $1 (名前が .git のファイル) の gitdir: がエントリ $2 の中を指すかを返す。指す先の git dir は、それ自身が印として
# find_git で判定される (使い捨ての .git dir の中なら数えず、bare の形なら残す)
disposable_gitfile() {
	local f=$1 c=$2 t d
	[ -f "$f" ] && [ ! -L "$f" ] || return 1
	t=$(sed -n '1s/^gitdir: //p' "$f") && [ -n "$t" ] || return 1
	case $t in /*) ;; *) t="${f%/*}/$t" ;; esac
	d=$(resolve_dir "$t") || return 1
	inside "$c" "$d"
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
