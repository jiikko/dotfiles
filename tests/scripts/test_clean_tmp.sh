#!/usr/bin/env bash
# scripts/clean_tmp.sh (make clean-tmp) の判定を固定する。
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

# 10. 書き込みが失敗する状況 (ディスクが一杯) でも find_git は印を見落とさない (bash 3.2 の here-string は一時ファイルに書く)
mkdir -p "$WORK/r10" && "$REAL_GIT" clone -q --bare "$WORK/r1" "$WORK/r10/b.git" 2>/dev/null
sed -n '/^find_git() {/,/^}/p' "$SCRIPT" > "$WORK/find_git.sh"
out=$(/bin/bash -c "source '$WORK/find_git.sh'; ( trap '' XFSZ; ulimit -f 0; find_git '$WORK/r10' 2>/dev/null )")
if [ "${out##*/}" = b.git ]; then ok "10: 書き込みが失敗しても git の置き場を見つける"; else fail "10: ulimit -f 0 の下で find_git = [$out] (b.git を返すこと)"; fi

# 11. skill の dir (印 .skill-tmpdir。bin/skill-tmpdir が置く) は丸ごと判定せず、中の session の dir を 1 件ずつ同じ規則で判定する
newrepo r11
sk="$REPO/tmp/sk"
mkdir -p "$sk/oldsess/x" "$sk/newsess" "$sk/oldlive/x" "$sk/oldgit"
: > "$sk/.skill-tmpdir"
echo a > "$sk/oldsess/x/f"; echo a > "$sk/newsess/f"; echo a > "$sk/oldlive/x/f"
"$REAL_GIT" init -q "$sk/oldgit/repo" 2>/dev/null
find "$sk/oldsess" "$sk/oldgit" -exec touch -h -t $OLD {} +
touch -t $OLD "$sk/oldlive/x" "$sk/oldlive" # 中の f は今日のまま
run; rc=$?
[ $rc -eq 0 ] || fail "11: rc=$rc ($(cat "$WORK/out"))"
if [ ! -e "$sk/oldsess" ] && [ -e "$sk/newsess/f" ] && [ -f "$sk/.skill-tmpdir" ]; then ok "11: 古い session の dir だけを消し、新しいもの・skill の dir・印は残した"; else fail "11: session 単位の判定 ($(cat "$WORK/out"))"; fi
if [ -e "$sk/oldlive/x/f" ] && grep -q "残す (中に 30 日以内の更新がある: tmp/sk/oldlive" "$WORK/out"; then ok "11: 古い session の dir でも中に新しい更新があれば残した"; else fail "11: oldlive ($(cat "$WORK/out"))"; fi
if [ -d "$sk/oldgit/repo" ]; then ok "11: session の dir の中の git は残した"; else fail "11: oldgit を消した"; fi

# 11b. skill の dir ごと古くても、消すのは中の session の dir だけ (skill の dir と印は残す)
newrepo r11b
sk="$REPO/tmp/sk"
mkdir -p "$sk/s1" && : > "$sk/.skill-tmpdir" && echo a > "$sk/s1/f"
find "$sk" -exec touch -h -t $OLD {} +
run; rc=$?
if [ ! -e "$sk/s1" ] && [ -f "$sk/.skill-tmpdir" ] && grep -q "消した: tmp/sk/s1" "$WORK/out"; then ok "11b: 古い skill の dir は中身だけ消した"; else fail "11b: rc=$rc ($(cat "$WORK/out"))"; fi

# 11c. 印が symlink / skill の dir が symlink なら中へ降りない (普通のエントリとして扱う)
newrepo r11c
mkdir -p "$REPO/tmp/fake/oldsess" "$WORK/outside11/oldsess"
: > "$WORK/marker11" && ln -s "$WORK/marker11" "$REPO/tmp/fake/.skill-tmpdir" # 先は普通のファイル ([ -f ] は通る)
echo a > "$REPO/tmp/fake/oldsess/f"; echo a > "$REPO/tmp/fake/new"
touch -t $OLD "$REPO/tmp/fake/oldsess/f" "$REPO/tmp/fake/oldsess"
: > "$WORK/outside11/.skill-tmpdir"; echo a > "$WORK/outside11/oldsess/f"
find "$WORK/outside11" -exec touch -h -t $OLD {} +
ln -s "$WORK/outside11" "$REPO/tmp/linked"
touch -h -t $OLD "$REPO/tmp/linked"
run; rc=$?
if [ -e "$REPO/tmp/fake/oldsess/f" ]; then ok "11c: 印が symlink の dir には降りない"; else fail "11c: 印が symlink の dir の中を消した ($(cat "$WORK/out"))"; fi
if [ -e "$WORK/outside11/oldsess/f" ]; then ok "11c: symlink の skill の dir の先には降りない"; else fail "11c: symlink の先の session を消した ($(cat "$WORK/out"))"; fi
if [ ! -L "$REPO/tmp/linked" ]; then ok "11c: 古い symlink は skill の dir を指していても link だけ消す"; else fail "11c: 古い symlink を残した ($(cat "$WORK/out"))"; fi

# 11d. DRY_RUN は session の dir も一覧だけ
newrepo r11d
sk="$REPO/tmp/sk"
mkdir -p "$sk/s1" && : > "$sk/.skill-tmpdir" && echo a > "$sk/s1/f"
find "$sk/s1" -exec touch -h -t $OLD {} +
run_dry; rc=$?
if [ $rc -eq 0 ] && [ -e "$sk/s1/f" ] && grep -q "消す: tmp/sk/s1" "$WORK/out"; then ok "11d: DRY_RUN は session の dir も消さずに一覧に出す"; else fail "11d: rc=$rc ($(cat "$WORK/out"))"; fi

# 11e. 印があっても直下に git がある dir (clone・worktree の中で skill-tmpdir を呼んだ) には降りない
newrepo r11e
"$REAL_GIT" init -q "$REPO/tmp/foo" 2>/dev/null && mkdir -p "$REPO/tmp/foo/src" && echo x > "$REPO/tmp/foo/src/f" && : > "$REPO/tmp/foo/.skill-tmpdir"
"$REAL_GIT" -C "$REPO" worktree add -q --detach "$REPO/tmp/bar" >/dev/null 2>&1 && : > "$REPO/tmp/bar/.skill-tmpdir"
find "$REPO/tmp" -mindepth 1 -exec touch -h -t $OLD {} + 2>/dev/null
touch "$REPO/tmp/foo/.skill-tmpdir" "$REPO/tmp/bar/.skill-tmpdir" # 印は今日 (使われ続けている skill の dir)
run; rc=$?
if [ -e "$REPO/tmp/foo/src/f" ] && [ -e "$REPO/tmp/bar/.git" ]; then ok "11e: 直下に git がある印の dir の中身を消さない"; else fail "11e: rc=$rc git の作業ツリーの中を消した ($(cat "$WORK/out"))"; fi

# 11f. 作ったばかりの空の session の dir は消さない (古い skill の dir の中でも)
newrepo r11f
sk="$REPO/tmp/sk"
mkdir -p "$sk/newempty" && : > "$sk/.skill-tmpdir"
touch -t $OLD "$sk/.skill-tmpdir" "$sk"
run; rc=$?
if [ -d "$sk/newempty" ]; then ok "11f: 新しい空の session の dir を残した"; else fail "11f: rc=$rc 新しい空の dir を消した ($(cat "$WORK/out"))"; fi

# 11g. 判定から消すまでの間に skill の dir を symlink に差し替えられても、外は消さない (du の shim で最初の du の時に差し替える)
newrepo r11g
sk="$REPO/tmp/sk"
mkdir -p "$sk/s1" "$WORK/outside11g/s1" && : > "$sk/.skill-tmpdir" && echo a > "$sk/s1/f" && echo keep > "$WORK/outside11g/s1/precious"
: > "$WORK/outside11g/.skill-tmpdir"
find "$sk/s1" "$WORK/outside11g" -exec touch -h -t $OLD {} +
real_du=$(command -v du) || fail "11g: du が無い"
mkdir -p "$WORK/shim11g"
cat > "$WORK/shim11g/du" <<SHIM
#!/bin/sh
if [ ! -e "$WORK/shim11g/done" ]; then : > "$WORK/shim11g/done"; mv "$sk" "$WORK/moved11g" && ln -s "$WORK/outside11g" "$sk"; fi
exec "$real_du" "\$@"
SHIM
chmod +x "$WORK/shim11g/du"
(cd "$REPO" && PATH="$WORK/shim11g:$PATH" DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
if [ ! -e "$WORK/shim11g/done" ]; then fail "11g: shim が呼ばれていない (差し替えが起きていない)"
elif [ -e "$WORK/outside11g/s1/precious" ] && grep -q "残す (親の tmp/sk が skill の dir でなくなった)" "$WORK/out"; then ok "11g: 差し替えられた skill の dir の先を消さない"
else fail "11g: rc=$rc 外を消した / 理由が出ない ($(cat "$WORK/out"))"; fi

# 11h. 読めない skill の dir があっても、他のエントリの掃除は止めない (読めない dir は is_skill_dir の find で外れ、丸ごと判定に回る経路)
newrepo r11h
mkdir -p "$REPO/tmp/sk/s1" && : > "$REPO/tmp/sk/.skill-tmpdir"
echo a > "$REPO/tmp/old" && touch -t $OLD "$REPO/tmp/old"
chmod 300 "$REPO/tmp/sk"
run; rc=$?
chmod 755 "$REPO/tmp/sk"
if [ $rc -eq 0 ] && [ ! -e "$REPO/tmp/old" ]; then ok "11h: 読めない skill の dir があっても他は掃除する"; else fail "11h: rc=$rc ($(cat "$WORK/out"))"; fi

# 11i. 印の名前の大文字小文字が違っても (APFS では同じファイル) 印を消さない
newrepo r11i
sk="$REPO/tmp/sk"
mkdir -p "$sk/new" && : > "$sk/.Skill-Tmpdir"
touch -t $OLD "$sk/.Skill-Tmpdir"
run; rc=$?
if [ -e "$sk/.Skill-Tmpdir" ]; then ok "11i: 大文字小文字の違う印を消さない"; else fail "11i: rc=$rc 印を消した ($(cat "$WORK/out"))"; fi

# shim は実体を絶対パスで解決してから PATH の先頭に置く (自分自身へ解決し直さない)
REAL_RM=$(command -v rm) || fail "rm が無い"
REAL_DU=$(command -v du) || fail "du が無い"
# mkshim <dir> <名前> <実体> <初回だけ走らせる sh の文>
mkshim() {
	mkdir -p "$1"
	printf '#!/bin/sh\nif [ ! -e "%s/done" ]; then : > "%s/done"; %s; fi\nexec "%s" "$@"\n' "$1" "$1" "$4" "$3" > "$1/$2"
	chmod +x "$1/$2"
}

# 11j. 親の確認の後・rm の直前に skill の dir を symlink に差し替えても、外は消さない (rm は cd した実体の中で相対名を消す)
newrepo r11j
sk="$REPO/tmp/sk"
mkdir -p "$sk/s1" "$WORK/out11j/s1" && : > "$sk/.skill-tmpdir" && echo a > "$sk/s1/f" && echo keep > "$WORK/out11j/s1/precious"
find "$sk/s1" -exec touch -h -t $OLD {} +
mkshim "$WORK/shim11j" rm "$REAL_RM" "mv '$sk' '$WORK/moved11j' && ln -s '$WORK/out11j' '$sk'"
(cd "$REPO" && PATH="$WORK/shim11j:$PATH" DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
if [ ! -e "$WORK/shim11j/done" ]; then fail "11j: rm の shim が呼ばれていない"
elif [ -e "$WORK/out11j/s1/precious" ] && [ ! -e "$WORK/moved11j/s1" ]; then ok "11j: rm の直前の差し替えでも外を消さず、実体の方を消した"
else fail "11j: rc=$rc 外を消した / 実体が残った ($(cat "$WORK/out"))"; fi

# 11k. 判定の途中で祖先 (tmp 自身) を symlink に差し替えても、外の skill の dir の中は消さない
newrepo r11k
sk="$REPO/tmp/sk"
mkdir -p "$sk/s1" "$WORK/out11k/sk/s1" && : > "$sk/.skill-tmpdir" && : > "$WORK/out11k/sk/.skill-tmpdir"
echo a > "$sk/s1/f" && echo keep > "$WORK/out11k/sk/s1/precious"
find "$sk/s1" "$WORK/out11k/sk/s1" -exec touch -h -t $OLD {} +
mkshim "$WORK/shim11k" du "$REAL_DU" "mv '$REPO/tmp' '$WORK/moved11k' && ln -s '$WORK/out11k' '$REPO/tmp'"
(cd "$REPO" && PATH="$WORK/shim11k:$PATH" DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
if [ ! -e "$WORK/shim11k/done" ]; then fail "11k: du の shim が呼ばれていない"
elif [ -e "$WORK/out11k/sk/s1/precious" ] && grep -q "残す (親の tmp/sk へ入れない・実体の dir でなくなった)" "$WORK/out"; then ok "11k: 祖先の差し替えでも外を消さない"
else fail "11k: rc=$rc ($(cat "$WORK/out"))"; fi

# 11l. 一覧を取った後に更新されたエントリは消さない (最初の du の時に両方を今日にする。判定済みの 1 件は消え、もう 1 件は残る)
newrepo r11l
echo a > "$REPO/tmp/a" && echo b > "$REPO/tmp/b" && touch -t $OLD "$REPO/tmp/a" "$REPO/tmp/b"
mkshim "$WORK/shim11l" du "$REAL_DU" "touch '$REPO/tmp/a' '$REPO/tmp/b'"
(cd "$REPO" && PATH="$WORK/shim11l:$PATH" DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
left=0; [ -e "$REPO/tmp/a" ] && left=$((left + 1)); [ -e "$REPO/tmp/b" ] && left=$((left + 1))
if [ ! -e "$WORK/shim11l/done" ]; then fail "11l: du の shim が呼ばれていない"
elif [ $left -eq 1 ] && grep -q "残す (一覧の後に更新された" "$WORK/out"; then ok "11l: 一覧の後に更新されたエントリは消さない"
else fail "11l: rc=$rc 残り $left 件 ($(cat "$WORK/out"))"; fi

# 12. 実体の比較は getcwd を使う /bin/pwd -P で行う (builtin の pwd -P は $PWD の文字列を解決し直すだけで、cd の後に親を実体へ
# 戻されると外の cwd を「一致」と答える。cd と比較の間は外部の呼び出しが挟まらず shim で窓を作れないので、形を固定する)
# 数えるのは行ではなく出現 (grep -o)。下限の件数ではなく、削除の直前の比較の形そのものを固定する
code=$(grep -v '^[[:space:]]*#' "$SCRIPT")
total=$(printf '%s\n' "$code" | grep -o 'pwd' | wc -l | tr -d ' ')
bin=$(printf '%s\n' "$code" | grep -o '/bin/pwd -P' | wc -l | tr -d ' ')
cmp_form=$(printf '%s\n' "$code" | grep -cF '[ "$(/bin/pwd -P)" = "$dirp" ] || exit 3; rm -rf -- "./${c##*/}"')
if [ "$total" -eq "$bin" ] && [ "$cmp_form" -eq 1 ]; then ok "12: pwd は全部 /bin/pwd -P ($bin 箇所) で、削除の直前の比較の形がある"
else fail "12: pwd の出現 $total / /bin/pwd -P $bin / 削除の直前の比較の形 $cmp_form 件"; fi

# 13. 継承した GIT_DIR / GIT_WORK_TREE に引きずられて別の場所の tmp を消さない (4 周目)
newrepo r13
mkdir -p "$WORK/other13/tmp" && echo a > "$WORK/other13/tmp/y" && touch -t $OLD "$WORK/other13/tmp/y"
echo a > "$REPO/tmp/x" && touch -t $OLD "$REPO/tmp/x"
(cd "$WORK/other13" && GIT_CEILING_DIRECTORIES="$WORK" GIT_DIR="$REPO/.git" DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
if [ -e "$WORK/other13/tmp/y" ] && [ -e "$REPO/tmp/x" ]; then ok "13: 継承した GIT_DIR では何も消さない (cwd は repo の外)"; else fail "13: rc=$rc GIT_DIR に引きずられて消した ($(cat "$WORK/out"))"; fi
(cd "$REPO" && GIT_WORK_TREE="$WORK/other13" DAYS=30 "$SCRIPT") > "$WORK/out" 2>&1; rc=$?
if [ -e "$WORK/other13/tmp/y" ]; then ok "13: 継承した GIT_WORK_TREE の先の tmp を消さない"; else fail "13: rc=$rc GIT_WORK_TREE の先を消した ($(cat "$WORK/out"))"; fi

if [ "$fails" -ne 0 ]; then
	echo "✗ clean_tmp: $fails 件失敗"
	exit 1
fi
echo "✓ clean_tmp: 全部通った"
