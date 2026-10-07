#!/bin/bash
#
# bin/mutate-verify-list (issue 580) の self-test。
#
# 判定は全部 mutate-verify が持つので、ここで確かめるのは並べ役の仕事だけ:
#   - LIST の引数が引用を崩さずに mutate-verify へ届く (defaults が前、mutant が後、--name)
#   - 表の rc と終了コード (1 本でも rc≠0 なら 1)
#   - LIST の書き損じでは 1 本も当てない (途中まで当ててから止まると、どこまで当てたかが分からない)
#   - 本物の mutate-verify と繋いで red / green が表に出る
set -uo pipefail
unset CDPATH  # export された CDPATH の下では、相対パスの cd が解決先を stdout に出し $(cd … && pwd) を壊す

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
MVL="$ROOT_DIR/bin/mutate-verify-list"
[ -x "$MVL" ] || { echo "NG: bin/mutate-verify-list が無い"; exit 1; }

# 🚨 道具を bash 3.2 (/bin/bash = shebang) と PATH の bash (開発機・CI は brew の 5 系) の両方で走らせる。
#    set -e の効く文脈が版で違い、3.2 だけで測っていたときに 5 系の素通りを見落とした (2 周目の敵対レビュー)
if [ -z "${MVL_BASH:-}" ]; then
  seen="" rc_all=0
  for b in /bin/bash "$(command -v bash)"; do
    b="$(cd "$(dirname "$b")" && pwd -P)/$(basename "$b")"
    case " $seen " in *" $b "*) continue ;; esac
    seen="$seen $b"
    # shellcheck disable=SC2016 # $BASH_VERSION は走らせる側の bash に展開させる
    echo "[test-mutate-verify-list] 道具を $b ($("$b" -c 'echo $BASH_VERSION')) で走らせる"
    MVL_BASH="$b" bash "$0" || rc_all=1
  done
  exit "$rc_all"
fi

work="$(mktemp -d)"
fails=0
cleanup() { rm -rf "$work"; }
trap cleanup EXIT
fail() { echo "NG: $*"; fails=$((fails + 1)); }
export MUTATE_VERIFY_LOG_ROOT="$work/logs"
mkdir -p "$MUTATE_VERIFY_LOG_ROOT"

# --- stub: 受け取った引数を 1 行 1 引数で記録し、--name ごとに決めた rc で終わる ------------------
stub="$work/stub-mv"
cat > "$stub" <<'S'
#!/bin/bash
n=""; prev=""
for a in "$@"; do [ "$prev" = "--name" ] && n="$a"; prev="$a"; done
printf '%s\n' "$@" > "$STUB_DIR/args.$n"
echo "$n" >> "$STUB_DIR/calls"
case "$n" in green*) exit 6 ;; tree*) exit 9 ;; *) exit 0 ;; esac
S
chmod +x "$stub"
run_stub() { # $1=LIST の中身。rc を返し、出力は $work/out.log
  local sd="$work/stub.$RANDOM$RANDOM"; mkdir -p "$sd"; STUB="$sd"
  printf '%s\n' "$1" > "$sd/list.sh"
  STUB_DIR="$sd" MUTATE_VERIFY_BIN="$stub" "$MVL_BASH" "$MVL" --log-dir "$sd/logs" "$sd/list.sh" > "$work/out.log" 2>&1
}

# 1. 引数の順序と引用の保存 (LIST の単一引用符の中の `\$x` は、bash が解くとおり `\$x` のまま届く)
# shellcheck disable=SC2016 # LIST の中の $ は LIST が解く (ここでは展開させない)
run_stub '
defaults --syntax "bash -n x" --baseline-expect "^ran 2"
mutant red-1 --file a.sh --apply '\''perl -pi -e "s/\$x/a b/" "$MUTATE_FILE"'\'' --expect "FAIL: a|b"
defaults --baseline-expect "^other"
mutant red-2 --file b.sh --apply "sed -i \"\" s/x/y/ b.sh" --expect "FAIL: 2"'
rc=$?
[ "$rc" = 0 ] || fail "全部 rc=0 なのに rc=$rc: $(cat "$work/out.log")"
# shellcheck disable=SC2016 # 期待値の $x / $MUTATE_FILE は文字どおり届くことを見る
want1='--name
red-1
--syntax
bash -n x
--baseline-expect
^ran 2
--file
a.sh
--apply
perl -pi -e "s/\$x/a b/" "$MUTATE_FILE"
--expect
FAIL: a|b'
[ "$(cat "$STUB/args.red-1" 2>/dev/null)" = "$want1" ] || fail "red-1 の引数が崩れた: $(cat "$STUB/args.red-1" 2>/dev/null)"
# defaults を書き直したら、以降の mutant は新しい defaults だけを前置する (古い --syntax を持ち越さない)
want2='--name
red-2
--baseline-expect
^other
--file
b.sh
--apply
sed -i "" s/x/y/ b.sh
--expect
FAIL: 2'
[ "$(cat "$STUB/args.red-2" 2>/dev/null)" = "$want2" ] || fail "red-2 の引数が崩れた: $(cat "$STUB/args.red-2" 2>/dev/null)"
grep -q $'^red-1\t0\t想定どおり red$' "$work/out.log" || fail "表に red-1 の行が無い: $(cat "$work/out.log")"

# 2. 1 本でも rc≠0 なら終了コード 1、表にその rc と意味が出る
run_stub '
mutant red-a --file a.sh --apply x --verify v --expect e --baseline-expect b
mutant green-b --file a.sh --apply x --verify v --expect e --baseline-expect b'
rc=$?
[ "$rc" = 1 ] || fail "rc=6 が混ざっているのに rc=$rc"
grep -q $'^green-b\t6\t🚨 緑のまま通った' "$work/out.log" || fail "表に green-b の rc=6 が出ない: $(cat "$work/out.log")"
[ "$(cat "$STUB/calls")" = $'red-a\ngreen-b' ] || fail "1 本ずつ LIST の順に当てていない: $(cat "$STUB/calls")"

# 3. LIST の書き損じでは 1 本も当てない (rc=2)
for bad in \
  'mutant ok-1 --file a
mutant "bad name" --file a' \
  'mutant dup --file a
mutant dup --file b' \
  'mutant ok-2 --file a
no_such_function x' \
  'defaults --file a' \
  'mutant ok-3 --file a
no_such_function x
mutant ok-4 --file a' \
  'mutant ok-5 --file a
false
mutant ok-6 --file a' \
  'mutant ok-7 --file a
exit 0
mutant ok-8 --file a' \
  'mutant ok-9 --file a --name other' \
  'mutant ok-10 --file a
__mvl_records=/dev/null
mutant ok-11 --file a' \
  'f() { false; echo after; }
mutant ok-12 --file a
f
mutant ok-13 --file a'; do
  run_stub "$bad"
  rc=$?
  [ "$rc" = 2 ] || fail "書き損じの LIST で rc=$rc (want 2): $bad"
  [ ! -e "$STUB/calls" ] || fail "書き損じの LIST で mutate-verify を起動した ($(tr '\n' ' ' < "$STUB/calls")): $bad"
done

# 4. LIST の set -e / cd は変異の実行に効かない (1 本目が rc≠0 でも 2 本目を当てて表を出す)
run_stub '
set -euo pipefail
cd /  # cd-rc: allow LIST の本文 (テストの入力)。LIST の cd が変異の実行に効かないことを確かめるために置く
mutant green-1 --file a
mutant red-2 --file a'
rc=$?
[ "$rc" = 1 ] || fail "LIST の set -e で rc=$rc (want 1): $(cat "$work/out.log")"
grep -q $'^red-2\t0\t' "$work/out.log" || fail "LIST の set -e で 2 本目が当たらない: $(cat "$work/out.log")"

# 5. rc=9 が出たら残りは当てずに止め、表に「未実行」と出す
run_stub '
mutant tree-1 --file a
mutant red-2 --file a'
rc=$?
[ "$rc" = 1 ] || fail "rc=9 なのに rc=$rc"
[ "$(cat "$STUB/calls")" = "tree-1" ] || fail "rc=9 の後も当てた: $(tr '\n' ' ' < "$STUB/calls")"
grep -q $'^red-2\t-\t未実行' "$work/out.log" || fail "rc=9 の後の未実行が表に出ない: $(cat "$work/out.log")"

# 6. 引数 0 個の mutant は空文字列の引数を渡さない (--name だけ)
run_stub 'mutant bare'
[ "$(cat "$STUB/args.bare" 2>/dev/null)" = $'--name\nbare' ] || fail "引数 0 個の mutant に余分な引数: $(cat "$STUB/args.bare" 2>/dev/null | od -c | head -3)"

# 7. ログの置き場が repo の中なら 1 本も当てない
inrepo="$work/inrepo"; mkdir -p "$inrepo"; git -C "$inrepo" init -q .
printf '%s\n' 'mutant red-1 --file a' > "$work/in.list"
( cd "$inrepo" && STUB_DIR="$work" MUTATE_VERIFY_BIN="$stub" "$MVL_BASH" "$MVL" --log-dir "$inrepo/logs" "$work/in.list" ) > "$work/out.log" 2>&1
rc=$?
[ "$rc" = 2 ] || fail "repo の中のログの置き場で rc=$rc (want 2): $(cat "$work/out.log")"
[ ! -e "$work/args.red-1" ] || fail "repo の中のログの置き場で mutate-verify を起動した"
# 大文字小文字を変えて書いても repo の中と判定する (APFS は区別しない。字面で比べると素通りする)
up="$(dirname "$inrepo")/$(basename "$inrepo" | tr '[:lower:]' '[:upper:]')"
if [ -d "$up" ]; then
  ( cd "$inrepo" && STUB_DIR="$work" MUTATE_VERIFY_BIN="$stub" "$MVL_BASH" "$MVL" --log-dir "$up/logs2" "$work/in.list" ) > "$work/out.log" 2>&1
  rc=$?
  [ "$rc" = 2 ] || fail "大文字で書いた repo の中の置き場で rc=$rc (want 2): $(cat "$work/out.log")"
else
  echo "[test-mutate-verify-list] SKIP: 大文字小文字を区別するファイルシステム (この迂回は起きない)"
fi
# ignore された置き場 (tmp/ 等) は git status に出ないので通す
printf 'ign/\n' > "$inrepo/.gitignore"
( cd "$inrepo" && STUB_DIR="$work" MUTATE_VERIFY_BIN="$stub" "$MVL_BASH" "$MVL" --log-dir "$inrepo/ign/logs" "$work/in.list" ) > "$work/out.log" 2>&1
rc=$?
[ "$rc" = 0 ] || fail "ignore された置き場を拒否した (rc=$rc): $(cat "$work/out.log")"

# 8. 別の dir へ symlink した mutate-verify-list も、兄弟の mutate-verify を実体の隣で見つける (issue 585)
#    見つからなければ「実行できない」で止まり、見つかれば一覧の中身の検査 (mutant 0 本) まで進む
mkdir -p "$work/linkbin"; ln -s "$MVL" "$work/linkbin/mutate-verify-list"; : > "$work/empty.list"
( cd "$work" && env -u MUTATE_VERIFY_BIN "$MVL_BASH" "$work/linkbin/mutate-verify-list" --log-dir "$work/link-logs" "$work/empty.list" ) > "$work/out.log" 2>&1
grep -q 'mutant が 1 つも無い' "$work/out.log" || fail "symlink 経由で兄弟の mutate-verify を見失った: $(cat "$work/out.log")"

# --- 本物の mutate-verify と繋ぐ: red と green が表に出る -----------------------------------
# helper を repo へコピーしない (dotfiles の外の repo と同じ形で繋ぐ。issue 585)
repo="$work/repo"
mkdir -p "$repo"
cat > "$repo/guard.sh" <<'G'
#!/bin/bash
# note: この行は検査に効かない
if [ "$1" = "bad" ]; then echo "rejected"; exit 1; fi
echo "accepted"
G
cat > "$repo/verify.sh" <<'V'
#!/bin/bash
ng=0
[ "$(bash guard.sh bad)" = "rejected" ] || { echo "FAIL: reject-bad"; ng=1; }
echo "ran 1 checks"
exit "$ng"
V
git -C "$repo" init -q . && git -C "$repo" config user.email t@t && git -C "$repo" config user.name t &&
  git -C "$repo" add -A && git -C "$repo" commit -qm init
cat > "$work/real.list" <<'L'
defaults --file guard.sh --verify 'bash verify.sh' --baseline-expect '^ran 1 checks'
mutant guard-off --apply 'perl -pi -e "s/= \"bad\"/= \"never\"/" "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
mutant comment-only --apply 'perl -pi -e "s/この行は/この行も/" "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
L
( cd "$repo" && "$MVL_BASH" "$MVL" --log-dir "$work/real-logs" "$work/real.list" ) > "$work/real.out" 2>&1
rc=$?
[ "$rc" = 1 ] || fail "本物: green を含むのに rc=$rc: $(cat "$work/real.out")"
grep -q $'^guard-off\t0\t' "$work/real.out" || fail "本物: guard-off が rc=0 でない: $(cat "$work/real.out") / $(cat "$work/real-logs/guard-off.log")"
grep -q $'^comment-only\t6\t' "$work/real.out" || fail "本物: comment-only が rc=6 でない: $(cat "$work/real.out") / $(cat "$work/real-logs/comment-only.log")"
[ -s "$work/real-logs/guard-off.log" ] || fail "本物: 変異ごとのログが残っていない"

if [ "$fails" -gt 0 ]; then
  echo "[test-mutate-verify-list] $fails 件失敗"
  exit 1
fi
echo "[test-mutate-verify-list] すべて成功"
