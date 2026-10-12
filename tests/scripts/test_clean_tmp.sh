#!/usr/bin/env bash
# scripts/clean_tmp.sh (make clean-tmp) の判定を固定する (issue 729)。
# 🚨 本物の rm -rf を呼ぶので、一時ディレクトリに作った git の repo の中でだけ走らせる。clean_tmp.sh は <repo の root>/tmp の外を
# 実行前に拒否する (その拒否もここで確かめる)。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/clean_tmp.sh"
REAL_GIT="$(command -v git)" || { echo "✗ git が無い"; exit 1; }
WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-clean-tmp.XXXXXX")" || { echo "✗ 一時ディレクトリを作れない"; exit 1; }
cleanup() {
	chmod -R u+rwx "$WORK" 2>/dev/null || true
	rm -rf "$WORK"
}
trap cleanup EXIT

fails=0
fail() { echo "✗ $*"; fails=$((fails + 1)); }
ok() { echo "✓ $*"; }
OLD=202601010000 # 30 日より十分古い

# newrepo は一時ディレクトリに repo と tmp/ を作り、そのパスを REPO に入れる (サブシェルで受けない: 後始末の登録が親に残るように)
newrepo() {
	REPO="$WORK/$1"
	mkdir -p "$REPO/tmp"
	"$REAL_GIT" -C "$REPO" init -q
	"$REAL_GIT" -C "$REPO" -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
}
run() { (cd "$REPO" && DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; }

# 1. 古いファイル・中身も古いフォルダは消す。新しいファイルと、古いフォルダの中に新しい更新があるものは残す
newrepo r1
mkdir -p "$REPO/tmp/oldfull/sub" "$REPO/tmp/oldbutlive/sub"
echo a > "$REPO/tmp/oldfile"
echo a > "$REPO/tmp/newfile"
echo a > "$REPO/tmp/oldfull/sub/f"
echo a > "$REPO/tmp/oldbutlive/sub/f"
touch -t $OLD "$REPO/tmp/oldfile" "$REPO/tmp/oldfull/sub/f" "$REPO/tmp/oldfull/sub" "$REPO/tmp/oldfull"
touch -t $OLD "$REPO/tmp/oldbutlive/sub" "$REPO/tmp/oldbutlive" # 中の f は今日のまま
run; rc=$?
[ $rc -eq 0 ] || fail "1: rc=$rc ($(cat "$WORK/out"))"
[ ! -e "$REPO/tmp/oldfile" ] && [ ! -e "$REPO/tmp/oldfull" ] && ok "1: 古いファイル・中身も古いフォルダを消した" || fail "1: 古いものが残った"
[ -e "$REPO/tmp/newfile" ] || fail "1: 新しいファイルを消した"
if [ -e "$REPO/tmp/oldbutlive/sub/f" ] && grep -q "残す (中に 30 日以内の更新がある: tmp/oldbutlive/sub" "$WORK/out"; then
	ok "1: 古いフォルダでも中に新しい更新があれば残し、理由を出した"
else
	fail "1: 中に新しい更新があるフォルダを消した / 理由が出ない ($(cat "$WORK/out"))"
fi

# 2. 中を読めない (権限の無い子孫) は判定不能として残す
newrepo r2
mkdir -p "$REPO/tmp/locked/inner"
echo a > "$REPO/tmp/locked/inner/f"
touch -t $OLD "$REPO/tmp/locked/inner/f" "$REPO/tmp/locked/inner" "$REPO/tmp/locked"
chmod 000 "$REPO/tmp/locked/inner"
touch -t $OLD "$REPO/tmp/locked"
run; rc=$?
chmod 755 "$REPO/tmp/locked/inner"
if [ -e "$REPO/tmp/locked/inner/f" ] && grep -q "残す (判定不能" "$WORK/out"; then ok "2: 中を読めないフォルダは消さない"; else fail "2: rc=$rc 判定不能を消した ($(cat "$WORK/out"))"; fi

# 3. 中に git の作業ツリー (worktree・clone) があるエントリは、mtime が古くても、場所の書き方に依らず残す
wt_case() { # $1 = 名前、$2 = 作った後に何をするか。tmp/a/wt に worktree を作り、未コミットのファイルを置き、全部を古くする
	newrepo "$1"
	mkdir -p "$REPO/tmp/a"
	"$REAL_GIT" -C "$REPO" worktree add -q --detach "$REPO/tmp/a/wt" >/dev/null 2>&1 || fail "3 ($1): worktree を作れない"
	echo wip > "$REPO/tmp/a/wt/uncommitted"
	eval "$2"
	find "$REPO/tmp" -mindepth 1 -exec touch -h -t $OLD {} + 2>/dev/null
	run; rc=$?
}
wt_case r3a ":"
if [ -e "$REPO/tmp/a/wt/uncommitted" ] && grep -q "残す (中に git の作業ツリーか置き場がある" "$WORK/out"; then ok "3: 入れ子の worktree を残した"; else fail "3: rc=$rc 入れ子の worktree を消した ($(cat "$WORK/out"))"; fi
wt_case r3b 'mv "$REPO/tmp/a" "$REPO/tmp/b"'
if [ -e "$REPO/tmp/b/wt/uncommitted" ]; then ok "3: 動かしただけの worktree を残した"; else fail "3: rc=$rc 動かした worktree を消した ($(cat "$WORK/out"))"; fi
wt_case r3c 'mv "$REPO/tmp/a" "$REPO/tmp/b" && ln -s b "$REPO/tmp/a"'
if [ -e "$REPO/tmp/b/wt/uncommitted" ]; then ok "3: 経路を symlink に差し替えた worktree の実体を残した"; else fail "3: rc=$rc 実体を消した ($(cat "$WORK/out"))"; fi
newrepo r3d
"$REAL_GIT" -C "$REPO/tmp" init -q clone 2>/dev/null; echo x > "$REPO/tmp/clone/f"
find "$REPO/tmp" -mindepth 1 -exec touch -h -t $OLD {} + 2>/dev/null
run; rc=$?
if [ -e "$REPO/tmp/clone/f" ]; then ok "3: tmp の中の clone を残した"; else fail "3: rc=$rc clone を消した"; fi

# 3b. git の置き場 (bare・--separate-git-dir) は .git を持たなくても残す。.GIT (大文字) の印も残す
newrepo r3e
"$REAL_GIT" clone -q --bare "$REPO" "$REPO/tmp/bare.git" 2>/dev/null
"$REAL_GIT" init -q --separate-git-dir="$REPO/tmp/store" "$WORK/sepwt" 2>/dev/null
"$REAL_GIT" -c core.preferSymlinkRefs=true init -q --bare "$REPO/tmp/symhead.git" 2>/dev/null # HEAD が symlink
mkdir -p "$REPO/tmp/up" && "$REAL_GIT" init -q "$REPO/tmp/up" && mv "$REPO/tmp/up/.git" "$REPO/tmp/up/.GIT"
"$REAL_GIT" -C "$REPO" worktree add -q --detach "$REPO/tmp/wtup" >/dev/null 2>&1 && mv "$REPO/tmp/wtup/.git" "$REPO/tmp/wtup/.GIT" # 印のファイルの大文字 (形では拾えない)
mkdir -p "$REPO/tmp/plain/HEAD" "$REPO/tmp/plain2" && echo x > "$REPO/tmp/plain2/HEAD" # 形の揃わない HEAD は印にしない
find "$REPO/tmp" -mindepth 1 -exec touch -h -t $OLD {} + 2>/dev/null
run; rc=$?
for d in bare.git store up wtup symhead.git; do
	if [ -d "$REPO/tmp/$d" ]; then ok "3b: $d を残した"; else fail "3b: rc=$rc $d を消した ($(cat "$WORK/out"))"; fi
done
if [ ! -e "$REPO/tmp/plain" ] && [ ! -e "$REPO/tmp/plain2" ]; then ok "3b: 形の揃わない HEAD だけのフォルダは消した"; else fail "3b: 印でないものを残した"; fi

# 5. tmp が symlink / repo の外 / tmp を読めない なら何もしない
newrepo r5
mkdir -p "$WORK/elsewhere" && echo a > "$WORK/elsewhere/victim" && touch -t $OLD "$WORK/elsewhere/victim"
rmdir "$REPO/tmp" && ln -s "$WORK/elsewhere" "$REPO/tmp"
run; rc=$?
if [ $rc -ne 0 ] && [ -e "$WORK/elsewhere/victim" ]; then ok "5: tmp が symlink なら止まる"; else fail "5: symlink の tmp の先を消した (rc=$rc)"; fi
mkdir -p "$WORK/norepo/tmp" && echo a > "$WORK/norepo/tmp/old" && touch -t $OLD "$WORK/norepo/tmp/old"
(cd "$WORK/norepo" && GIT_CEILING_DIRECTORIES="$WORK" DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
if [ $rc -ne 0 ] && [ -e "$WORK/norepo/tmp/old" ]; then ok "5: repo の外では止まる"; else fail "5: repo の外で消した (rc=$rc)"; fi
newrepo r5b
echo a > "$REPO/tmp/old" && touch -t $OLD "$REPO/tmp/old"
chmod 000 "$REPO/tmp"
run; rc=$?
chmod 755 "$REPO/tmp"
if [ $rc -ne 0 ] && [ -e "$REPO/tmp/old" ]; then ok "5: tmp の一覧を読めないと止まる"; else fail "5: 読めない tmp で rc=$rc"; fi

# 6. 古い symlink のエントリは link 自身だけを消す (先は辿らない)
newrepo r6
mkdir -p "$WORK/target6" && echo a > "$WORK/target6/keep" && touch -t $OLD "$WORK/target6/keep" "$WORK/target6"
ln -s "$WORK/target6" "$REPO/tmp/link"
touch -h -t $OLD "$REPO/tmp/link"
run; rc=$?
if [ ! -L "$REPO/tmp/link" ] && [ -e "$WORK/target6/keep" ]; then ok "6: symlink は link だけを消した"; else fail "6: rc=$rc symlink の扱い ($(cat "$WORK/out"))"; fi

# 7. DRY_RUN は消さない・0 件は 0 件と出す
newrepo r7
echo a > "$REPO/tmp/old" && touch -t $OLD "$REPO/tmp/old"
run_dry() { (cd "$REPO" && DAYS=30 DRY_RUN=1 "$SCRIPT") > "$WORK/out" 2>&1; }
run_dry; rc=$?
if [ $rc -eq 0 ] && [ -e "$REPO/tmp/old" ] && grep -q "消す: tmp/old" "$WORK/out"; then ok "7: DRY_RUN は一覧だけ"; else fail "7: DRY_RUN rc=$rc ($(cat "$WORK/out"))"; fi
rm -f "$REPO/tmp/old"
run; rc=$?
if [ $rc -eq 0 ] && grep -q "古い tmp のエントリは 0 件" "$WORK/out"; then ok "7: 0 件"; else fail "7: 0 件で rc=$rc ($(cat "$WORK/out"))"; fi

# 8. DRY_RUN に知らない値 (true) を渡したら消さずに止まる
newrepo r8
echo a > "$REPO/tmp/old" && touch -t $OLD "$REPO/tmp/old"
(cd "$REPO" && DAYS=30 DRY_RUN=true "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
if [ $rc -ne 0 ] && [ -e "$REPO/tmp/old" ]; then ok "8: DRY_RUN=true は消さずに止まる"; else fail "8: DRY_RUN=true で rc=$rc 消した"; fi

# 9. worktree のパスの大文字小文字が repo と違っても (APFS では同じ場所) 残す
mkdir -p "$WORK/CaseRepo/tmp"
"$REAL_GIT" -C "$WORK/CaseRepo" init -q && "$REAL_GIT" -C "$WORK/CaseRepo" -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
if [ -d "$WORK/caserepo" ]; then # 大文字小文字を区別しない FS だけ
	"$REAL_GIT" -C "$WORK/caserepo" worktree add -q --detach "$WORK/CASEREPO/tmp/wt" >/dev/null 2>&1 || fail "9: worktree を作れない"
	find "$WORK/CaseRepo/tmp/wt" -exec touch -h -t $OLD {} + 2>/dev/null
	(cd "$WORK/caserepo" && DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
	if [ -e "$WORK/CaseRepo/tmp/wt/.git" ]; then ok "9: 大文字小文字の違う登録でも worktree を残した"; else fail "9: rc=$rc worktree を消した ($(cat "$WORK/out"))"; fi
else
	echo "skip 9: 大文字小文字を区別する FS"
fi

# 10. 書き込みが失敗する状況 (ディスクが一杯) でも find_git は印を見落とさない (bash 3.2 の here-string は一時ファイルに書く。issue 729 の 4 周目)
mkdir -p "$WORK/r10" && "$REAL_GIT" clone -q --bare "$WORK/r1" "$WORK/r10/b.git" 2>/dev/null
sed -n '/^find_git() {/,/^}/p' "$SCRIPT" > "$WORK/find_git.sh"
out=$(/bin/bash -c "source '$WORK/find_git.sh'; ( trap '' XFSZ; ulimit -f 0; find_git '$WORK/r10' 2>/dev/null )")
if [ "${out##*/}" = b.git ]; then ok "10: 書き込みが失敗しても git の置き場を見つける"; else fail "10: ulimit -f 0 の下で find_git = [$out] (b.git を返すこと)"; fi

if [ "$fails" -ne 0 ]; then
	echo "✗ clean_tmp: $fails 件失敗"
	exit 1
fi
echo "✓ clean_tmp: 全部通った"
