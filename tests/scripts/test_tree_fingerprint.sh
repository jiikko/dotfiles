#!/usr/bin/env bash
# scripts/tree_fingerprint.sh (make test が前後で作業ツリーの指紋を比べる) の unit テスト。
#
# なぜ: 検査が走っている間の編集を見逃すと、どの時点の木も検査していない結果を緑として読む。見逃しの退行は無音なので、
# 変わったと見分けるべき変更 (untracked の追加・中身・commit) と、見ない変更 (tmp/・ignore) を 1 つずつ固定する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FP="$ROOT_DIR/scripts/tree_fingerprint.sh"
# shellcheck source=bin/lib/git_location_env.sh
. "$ROOT_DIR/bin/lib/git_location_env.sh"
git_location_env_unset || { echo "✗ git の場所の環境変数を外せない"; exit 1; }
# 利用者の global の設定 (core.excludesFile が tmp/ を ignore する等) を切り離す: 効いていると、tmp/ を除く指定が無くても同じ結果になる
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1

ok=0; fail=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cd "$WORK" || exit 1
git init -q -b master
printf 'build/\n' > .gitignore
echo base > tracked.txt
git add .gitignore tracked.txt
git -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m base

expect() { # $1=期待の rc $2=説明 $3=変更を加えるコマンド
  local before rc
  before=$("$FP" print)
  eval "$3"
  rc=0; "$FP" check "$before" 2>/dev/null || rc=$?
  if [ "$rc" = "$1" ]; then echo "✓ $2"; ok=$((ok+1)); else echo "✗ $2: rc=$rc (want $1)"; fail=$((fail+1)); fi
}
expect 0 "何も変えなければ同じ" ':'
expect 1 "untracked のファイルが増えたら変わった" 'echo x > new.txt'
expect 1 "変更済みのファイルの中身だけを変えても変わった (一覧は同じ)" 'echo y > new.txt'
expect 1 "追跡しているファイルを変えたら変わった" 'echo z >> tracked.txt'
expect 1 "中身を変えない commit でも変わった (HEAD)" 'git -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q --allow-empty -m e'
expect 1 "追跡しているファイルを消したら変わった" 'rm tracked.txt'
expect 1 "rename したら変わった" 'git add -A && git -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m c && git mv new.txt renamed.txt'
expect 0 "tmp/ の下の変更は見ない (CI の checkout では ignore されない出力の置き場)" 'mkdir -p tmp && echo a > tmp/log'
expect 0 "ignore されたファイルの変更は見ない" 'mkdir -p build && echo b > build/out'

# 属性だけが変わる形は、一覧 (git status) が同じままになる状態から試す (一覧が変われば、属性を見なくても変わったと出る)
fresh() { # 使い捨ての repo に入る。$1 の名前で作る
  mkdir -p "$WORK/$1" && cd "$WORK/$1" && git init -q -b master || exit 1
}
c() { git -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q "$@"; }

# 一覧の最後が消えたファイル (untracked を残さない) でも指紋を取れる。取れないと「比べられない」に化け、編集を見逃す
fresh only-deleted; echo a > a.txt; echo b > b.txt; git add a.txt b.txt; c -m base; rm b.txt
rc=0; "$FP" print >/dev/null 2>&1 || rc=$?
if [ "$rc" = 0 ]; then echo "✓ 一覧の最後が消えたファイルでも指紋を取れる"; ok=$((ok+1)); else echo "✗ 一覧の最後が消えたファイルで指紋を取れない (rc=$rc)"; fail=$((fail+1)); fi

# 既に変更済み ( M) のファイルの mode だけを変える: 一覧は同じで、属性の行だけが変わる
fresh mode-only; echo a > x.sh; git add x.sh; c -m base; echo b >> x.sh
expect 1 "変更済みのファイルの mode だけを変えても変わった (一覧は同じ)" 'chmod +x x.sh'

# commit 済みの symlink を張り替えて変更済みにしてから、指す先だけをもう一度変える: 一覧は同じで、指す先の行だけが変わる
fresh symlink-only; ln -s one link; git add link; c -m base; ln -sfn two link
expect 1 "symlink の指す先を変えたら変わった (一覧は同じ)" 'ln -sfn three link'

# 読めないファイルがあっても、その後ろの変更済みファイルの中身の変化を見分ける (読めないパスで hash-object が止まらない)
# 読めないファイル (a.txt) は一覧で z.txt より前に来るよう、どちらも追跡済みの変更にする (untracked は一覧の最後に来て、止まっても前は hash 済み)
fresh unreadable; echo a > a.txt; echo z > z.txt; git add a.txt z.txt; c -m base; echo b >> a.txt; echo b > z.txt; chmod 000 a.txt
expect 1 "読めないファイルの後ろの、変更済みファイルの中身だけの変化も見分ける" 'echo c > z.txt'
chmod 644 a.txt

# サブディレクトリから打っても、repo の root で中身を比べる
fresh subdir; mkdir sub; echo a > sub/f.txt; git add sub; c -m base; echo b > sub/f.txt; cd sub || exit 1
expect 1 "サブディレクトリから打っても、変更済みのファイルの中身の変化を見分ける" 'echo c > f.txt'
cd "$WORK" || exit 1

rc=0; "$FP" check "" 2>/dev/null || rc=$?
if [ "$rc" = 2 ]; then echo "✓ 前の指紋が無ければ比べられない (rc=2。変わっていないとは言わない)"; ok=$((ok+1)); else echo "✗ 前の指紋が空で rc=$rc (want 2)"; fail=$((fail+1)); fi

# make test が待ちの後に test-guarded を通り、その中で前後を比べていること
test_rule=$(sed -n '/^test:/,/^$/p' "$ROOT_DIR/Makefile")
guard_rule=$(sed -n '/^test-guarded:/,/^$/p' "$ROOT_DIR/Makefile")
if [[ "$test_rule" == *test-guarded* && "$guard_rule" == *"tree_fingerprint.sh check"* ]]; then
  echo "✓ make test は test-guarded を通り、前後の指紋を比べる"; ok=$((ok+1))
else
  echo "✗ make test が test-guarded / tree_fingerprint.sh check を通らない"; fail=$((fail+1))
fi

# test-guarded の rc: 本体の赤を潰さない・変わったら赤・比べられなくても赤 (本体を差し替えた Makefile で実際に走らせる)
guarded_rc() { # $1=本体の rc $2=本体の間にすること → test-guarded の rc
  { printf 'test-all:\n\t@%s; exit %s\n\n' "$2" "$1"
    sed -n '/^test-guarded:/,/^$/p' "$ROOT_DIR/Makefile" | sed "s#\./scripts/tree_fingerprint\.sh#$FP#g"
  } > "$WORK/Makefile" # $(MAKE) は -f を引き継がないので、本体の差し替えは既定の名前で置く
  local rc=0
  ( cd "$WORK" && make -s test-guarded >/dev/null 2>&1 ) || rc=$?
  echo "$rc"
}
for c in "0|:|0|本体が緑で何も変えなければ緑" "1|:|2|本体の赤を潰さない" "0|echo q > guard-new.txt|2|走っている間に変わったら赤"; do
  IFS='|' read -r body act want desc <<< "$c"
  got=$(guarded_rc "$body" "$act")
  if { [ "$want" = 0 ] && [ "$got" = 0 ]; } || { [ "$want" != 0 ] && [ "$got" != 0 ]; }; then echo "✓ test-guarded: $desc"; ok=$((ok+1)); else echo "✗ test-guarded: $desc (rc=$got)"; fail=$((fail+1)); fi
done

echo "---"
echo "ok=$ok fail=$fail"
[ "$fail" -eq 0 ]
