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

[ "$fails" -eq 0 ] || exit 1
