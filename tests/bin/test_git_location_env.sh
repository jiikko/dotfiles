#!/usr/bin/env bash
# bin/lib/git_location_env.sh の git_location_env_unset が、git の挙げる「repo の場所を決める環境変数」を全部外し、
# 外せなかったときは rc≠0 を返すことを pin する。
# 正解役は本物の git (`git rev-parse --local-env-vars`) で、helper の字面は読まない。GIT_NAMESPACE は一覧に出ないが helper が足す。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LIB="$ROOT_DIR/bin/lib/git_location_env.sh"
fails=0
fail() { echo "✗ $*" >&2; fails=$((fails + 1)); }
ok() { echo "✓ $*"; }

vars=$(git rev-parse --local-env-vars) || { echo "✗ git rev-parse --local-env-vars が失敗した (判定できない)" >&2; exit 1; }
vars+=$'\nGIT_NAMESPACE'
n=$(printf '%s\n' "$vars" | grep -c .)
# 空振りを緑にしない。git 2.55 で 15 個 + GIT_NAMESPACE
[ "$n" -ge 10 ] || { printf '✗ git が挙げた変数が %d 個しかない (取り出しが壊れている)\n' "$n" >&2; exit 1; }

# 1. 全部に値を入れて呼ぶと、1 つも残らない (値は存在しない場所。git がそれを見てもどこにも書かない)
setenv=()
while IFS= read -r v; do [ -n "$v" ] && setenv+=("$v=/nonexistent/git-location-env/$v"); done <<<"$vars"
left=$(env "${setenv[@]}" bash -c '. "$1" && git_location_env_unset || { echo "rc=$?"; exit 0; }; env | sed -n "s/^\(GIT_[A-Z_]*\)=.*/\1/p"' _ "$LIB")
remain=""
while IFS= read -r v; do
  [ -n "$v" ] || continue
  grep -qxF "$v" <<<"$left" && remain="$remain $v"
done <<<"$vars"
case "$left" in rc=*) fail "値の入った環境で呼ぶと失敗した ($left)" ;; esac
if [ -z "$remain" ]; then ok "git の挙げる $n 個の変数が全部外れる"; else fail "外れずに残った変数:$remain"; fi

# 2. 外せなかったら rc≠0。IFS を変えた後に呼ぶと一覧が 1 個の名前になり何も外さないが、bash の unset はその名前でも rc=0 を返す
#    ので、最後の「GIT_DIR / GIT_WORK_TREE が外れたか」の確認だけがこれを拾う
rc=0
env GIT_DIR=/nonexistent/x bash -c '. "$1"; IFS=" "; git_location_env_unset' _ "$LIB" 2>/dev/null || rc=$?
if [ "$rc" -ne 0 ]; then ok "外せなかったときは rc≠0 ($rc)"; else fail "IFS を変えた後に呼んで GIT_DIR が残ったのに rc=0"; fi

# 2b. 最後の確認が見ない変数が外せないとき (readonly) も rc≠0。unset の rc を握り潰さない
rc=0
env GIT_INDEX_FILE=/nonexistent/index bash -c '. "$1"; readonly GIT_INDEX_FILE; git_location_env_unset' _ "$LIB" 2>/dev/null || rc=$?
if [ "$rc" -ne 0 ]; then ok "readonly で外せない変数があれば rc≠0 ($rc)"; else fail "readonly の GIT_INDEX_FILE が残ったのに rc=0"; fi

# 3. sh (/bin/sh) からも使える
rc=0
env GIT_DIR=/nonexistent/x /bin/sh -c '. "$1" && git_location_env_unset && [ -z "${GIT_DIR+x}" ]' _ "$LIB" || rc=$?
if [ "$rc" -eq 0 ]; then ok "/bin/sh からも外せる"; else fail "/bin/sh から呼んで rc=$rc"; fi

# 4. 道具の中に git の場所の変数を手で並べた unset を書かない (git_location_env_unset を使う)。手で並べた一覧は道具ごとに
#    違う部分集合になり、GIT_INDEX_FILE などが漏れる。tests/ は対象外 (各テストの前処理)。
#    判定は字面: コメント行以外で、`unset` の後ろの同じ行に git の場所の変数の名前 (引用符つきも) がある行。前の構文 (if / { / && / `) は見ない。
#    誤検出として受けるもの: 文字列・ヒアドキュメントの中の同じ字面 (今の repo には無い)、設定の隔離のための `unset GIT_CONFIG`。
#    検出しない形: 変数に入れた名前・`unset \` の続き行に書いた名前・env -u (1 コマンドだけに効く)・zsh の `unset -m`・`export -n`・
#    走査の外 (zshlib・_claude/skills・setup.sh など)・Makefile の unexport・Go の gitLocationEnv
loc_vars=$(printf '%s\n' "$vars" | paste -sd'|' -)
hand_unset() { # 引数のファイルを走査し、当たった行を出す。rc: 0 = 当たった / 1 = 当たらない / 2 = 読めなかった
  local out rc=0
  out=$(grep -HnE "(^|[^A-Za-z0-9_])unset[[:space:]]([^#]*[^A-Za-z0-9_])?($loc_vars)([^A-Za-z0-9_]|\$)" "$@") || rc=$?
  [ "$rc" -le 1 ] || return 2
  [ "$rc" -eq 0 ] || return 1
  out=$(printf '%s\n' "$out" | grep -vE '^[^:]+:[0-9]+:[[:space:]]*#') || return 1
  printf '%s\n' "$out"
}
canary_dir=$(mktemp -d) || exit 1
cat > "$canary_dir/c.sh" <<'CANARY'
#!/bin/bash
  unset GIT_DIR GIT_WORK_TREE
true && unset GIT_INDEX_FILE GIT_COMMON_DIR
( unset GIT_PREFIX )
if x; then unset GIT_DIR; fi
{ unset "GIT_OBJECT_DIRECTORY"; }
y=`unset GIT_DIR`
# unset GIT_DIR はコメント
unset CDPATH MY_GIT_DIR GIT_NAMESPACE_X
CANARY
hits=$(hand_unset "$canary_dir/c.sh" | cut -d: -f2 | paste -sd, -)
if [ "$hits" = "2,3,4,5,6,7" ]; then ok "手書きの unset の検出が既知の 6 形に当たり、コメントと別名には当たらない"; else fail "手書きの unset の検出が壊れている (当たった行: ${hits:-なし}。期待 2,3,4,5,6,7)"; fi
rc=0; hand_unset "$canary_dir/no-such-file" >/dev/null 2>&1 || rc=$?
if [ "$rc" = 2 ]; then ok "読めないファイルは rc=2 (当たらないと区別する)"; else fail "読めないファイルで rc=$rc (期待 2)"; fi
rm -rf "$canary_dir"
files=()
for d in bin scripts githooks _claude/hooks; do
  n=0
  while IFS= read -r f; do files+=("$f"); n=$((n + 1)); done < <(cd "$ROOT_DIR" && git ls-files -- "$d" | grep -vxF bin/lib/git_location_env.sh)
  [ "$n" -ge 1 ] || fail "走査の対象 $d が 0 件 (列挙が壊れている)"
done
rc=0; found=$(cd "$ROOT_DIR" && hand_unset "${files[@]}") || rc=$?
case "$rc" in
  0) fail "git の場所の変数の手書きの unset がある (git_location_env_unset を使う): $found" ;;
  1) ok "手書きの unset は無い (${#files[@]} 件を走査)" ;;
  *) fail "走査の対象を読めなかった (grep rc=2。消えたファイルや dir を指す symlink が無いか見る)" ;;
esac

[ "$fails" -eq 0 ] || exit 1
