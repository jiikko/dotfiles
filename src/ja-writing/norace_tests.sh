#!/usr/bin/env bash
# `//go:build !race` のファイルにあるテストを集め、race なしで 1 本ずつ回す (`make test` が `go test -race ./...` の後に呼ぶ)。
#
#   ./norace_tests.sh          集めて回す
#   ./norace_tests.sh --help   この説明を出す
#
# 集め方: tag のファイルを持つ package ごとに `go test -list` を race なしと `-race` で打ち、race なしにだけ出るテストを回す。
# テストを tag のファイルに足せば、一覧を手で直さなくても回る。
# 集めた名前は、tag のファイルの `func Test…(` の行から読んだ名前と package ごとに突き合わせる。集め方が壊れて本数が減っても
# 緑にしないためで、2 つは別の読み方 (go の build の判定と、ファイルの字面) なので、Test の関数については片方の誤りがもう片方で落ちる。
# どちらの読み方も見ないものが 2 つあり、突き合わせの前に止める:
#   - tag のファイルの Fuzz・Example の関数 (回すのは Test だけ。黙って外さず、tag の無いファイルへ移すかこのスクリプトを広げる)
#   - `race` を含むのに `!race` の字面に当たらない tag (`!(race && cgo)` など。gofmt はこの形を直さず、package ごと外れる)
# 回したテストは、`--- PASS: <名前> (` の行が出たことも見る (rc=0 でも 0 件実行のことがあるため)。
#
# 結果の読み方: 終わりに「race なしのテスト N 本が走って通った」が出れば全部通っている。
# rc: 0 = 全部走って通った / 1 = tag のファイルかテストが 0 本・読めない tag の書き方・tag のファイルに Fuzz か Example・
#     集めた名前が字面と合わない・`go test -list` の失敗・テストが走っていないか落ちた / 2 = 引数の誤り
set -euo pipefail
unset CDPATH

case "${1:-}" in
  "") ;;
  -h | --help)
    sed -n '2,/^set -euo pipefail$/p' "$0" | sed -e '$d' -e 's/^# \{0,1\}//'
    exit 0
    ;;
  *)
    echo "norace_tests: 引数は取らない (--help で説明を出す)" >&2
    exit 2
    ;;
esac

cd "$(dirname "$(readlink -f "$0")")"

fail() {
  echo "norace_tests: $*" >&2
  exit 1
}

# 改行区切りの並び $1 に、行 $2 があるか。grep に並びを渡さない (空の並びを -F で渡すと全部の行に当たる)。
has_line() {
  case $'\n'"$1"$'\n' in
    *$'\n'"$2"$'\n'*) return 0 ;;
  esac
  return 1
}

# `go test -list` の出力から、名前の頭が $1 (正規表現) のものだけを残す (終わりの `ok <package>` の行を落とす)。残りの引数は go test に渡す。
list_tests() {
  local head=$1 out
  shift
  out=$(go test "$@" -list "^${head}" "$pkg") || fail "go test $* -list が落ちた (${pkg})"
  grep -E "^${head}" <<<"$out" | LC_ALL=C sort || true
}

# race なしの一覧にだけ出る名前 ($1 と $2 は list_tests の出力)。
only_plain() {
  local name out=""
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    has_line "$2" "$name" || out="${out}${name}"$'\n'
  done <<<"$1"
  printf '%s' "$out"
}

tagged=$(grep -rlE --include='*_test.go' '^//go:build .*!race' . | LC_ALL=C sort) || true
[ -n "$tagged" ] || fail "\`//go:build !race\` のテストのファイルが 0 本 (tag の書き方か、このスクリプトの探し方が変わっていないか見る)"

# race を肯定で使う tag (`//go:build race`) のファイルは今は無いので、それも止まる。足すなら、ここで除く条件を決める。
racetag=$(grep -rlE --include='*_test.go' '^(//go:build|// \+build) (.*[^A-Za-z0-9_])?race([^A-Za-z0-9_]|$)' . | LC_ALL=C sort) || true
unread=""
while IFS= read -r f; do
  [ -n "$f" ] || continue
  has_line "$tagged" "$f" || unread="${unread}${f}"$'\n'
done <<<"$racetag"
[ -z "$unread" ] || fail "読めない tag の書き方 (race を含むが \`//go:build … !race\` の形でない。このスクリプトが集められない): ${unread}"

pkgs=$(while IFS= read -r f; do dirname "$f"; done <<<"$tagged" | LC_ALL=C sort -u)

runs=""
total=0
while IFS= read -r pkg; do
  want=""
  while IFS= read -r f; do
    [ "$(dirname "$f")" = "$pkg" ] || continue
    names=$(sed -n 's/^func \(Test[A-Za-z0-9_]*\)(.*/\1/p' "$f")
    [ -z "$names" ] || want="${want}${names}"$'\n'
  done <<<"$tagged"
  want=$(printf '%s' "$want" | grep -vx 'TestMain' | LC_ALL=C sort || true)

  # Fuzz と Example は回さないので、tag のファイルにあれば止める。字面でなく go の読み方 (-list の差) で拾う
  # (`func /* x */ FuzzA(` のように func と名前の間にコメントを挟むと、字面の grep をすり抜けて gofmt も直さない)
  # list_tests の結果は変数に受けてから渡す (コマンド置換の引数の中で fail しても、外の rc に伝わらない)
  plain=$(list_tests '(Fuzz|Example)')
  race=$(list_tests '(Fuzz|Example)' -race)
  other=$(only_plain "$plain" "$race")
  [ -z "$other" ] || fail "tag のファイルに、回さない形のテスト (Fuzz・Example) がある (${pkg})。tag の無いファイルへ移すか、このスクリプトを広げる: ${other}"

  plain=$(list_tests Test)
  race=$(list_tests Test -race)
  got=$(only_plain "$plain" "$race")

  if [ "$got" != "$want" ]; then
    echo "--- tag のファイルの字面から読んだテスト (${pkg})" >&2
    echo "$want" >&2
    echo "--- go test -list で race なしにだけ出たテスト (${pkg})" >&2
    echo "$got" >&2
    fail "集めたテストが tag のファイルの字面と合わない (${pkg})"
  fi

  while IFS= read -r name; do
    [ -n "$name" ] || continue
    runs="${runs}${pkg}:${name}"$'\n'
    total=$((total + 1))
  done <<<"$got"
done <<<"$pkgs"

[ "$total" -gt 0 ] || fail "\`//go:build !race\` のファイルにテストが 0 本"
echo "norace_tests: race なしで回すテストは ${total} 本"

while IFS= read -r run; do
  [ -n "$run" ] || continue
  pkg=${run%%:*}
  name=${run##*:}
  rc=0
  out=$(go test -count=1 -v -run "^${name}\$" "$pkg" 2>&1) || rc=$?
  tail -n 3 <<<"$out"
  passed=$(grep -c -- "^--- PASS: ${name} (" <<<"$out" || true)
  if [ "$rc" -ne 0 ] || [ "$passed" -ne 1 ]; then
    fail "${name} (${pkg}) が走っていないか落ちた (rc=${rc})"
  fi
done <<<"$runs"

echo "norace_tests: race なしのテスト ${total} 本が走って通った"
