#!/usr/bin/env bash
# 製品の package に、どの `main` からも届かない関数が無いことを確かめる (`make lint` が呼ぶ)。
#
#   ./deadcode_check.sh          確かめる
#   ./deadcode_check.sh --help   この説明を出す
#
# 道具は golang.org/x/tools/cmd/deadcode。テストを含めず、module の全部の `main` (`.` と `oracle/cmd/*`) から辿る。
# repo の unused の検査 (scripts/check_unused_excluding_tests.sh) は export した名前を報告しないので、テストからだけ呼ぶ
# export の入口と、その入口からだけ呼ばれる下請けは、そちらでは見つからない。
#
# 出たときの直し方: 同じフォルダのテストからだけ呼ぶ関数は、そのフォルダの export_test.go へ移す。
# 別の package のテストから呼ぶなどで移せない関数だけ、下の ALLOW に理由つきで足す。
#
# 見ないもの:
#   - `oracle/` (本家の記録を取るテストの道具。製品のバイナリに入らない)
#   - 型・変数・定数 (deadcode は関数だけを報告する)
#   - reflect で呼ぶ関数 (deadcode が辿れない。使っていれば誤って出るので ALLOW へ)
#   - darwin 以外の build のファイル (lint を回す OS の build だけを読む)
#   - テストの道具の main (`oracle/cmd/*`。`make golden` など) だけが呼ぶ製品の関数 (起点に含めるので、届くと数える)
#   - interface のメソッドと同じ名前のメソッド (deadcode は interface の呼び出しを多めに届くと見積もるので、出ないことがある。未実測)
#   - 中に go.mod を置いたフォルダ (`./...` から外れる。今は third_party/regexp2。製品の package を別の module にすると検査の外へ出る)
#
# 道具は DEADCODE_VERSION で固定し、scripts/golangci_lint.sh と同じ置き場に一度だけビルドする
# (`go run pkg@バージョン` は起動のたびに module の解決をやり直す。置き場の鍵の考え方は golangci_lint.sh の冒頭)。
# go.mod の `tool` に足さない: x/tools とその依存が ja-writing の go.sum に入り、製品の依存と見分けにくくなる。
#
# 結果の読み方: 終わりに「製品の package に届かない関数なし」が出れば通っている。
# rc: 0 = 届かない関数なし / 1 = 届かない関数がある・ALLOW の行がもう報告されない・deadcode が落ちた・出力を読めない・
#     `oracle/` の行が 0 件 (道具が module を読めていない) / 2 = 引数の誤り
set -euo pipefail
unset CDPATH

DEADCODE_VERSION=v0.52.0

# "<ファイル>:<関数>|<理由>"。ここの行が報告されなくなったら落とす (製品から呼ばれるようになった・消した・移した)。
ALLOW=$(
  cat <<'ALLOWEOF'
internal/pystr/pystr.go:IsAlpha|str の分類 (isspace・isalpha・isalnum・isdecimal・isdigit・isnumeric) を 1 組で写し、pystr.json の記録と比べる (README「標準ライブラリを写す package」の表)。製品が今使うのは IsSpace だけ
internal/pystr/pystr.go:IsAlnum|IsAlpha と同じ
internal/pystr/pystr.go:IsDecimal|IsAlpha と同じ
internal/pystr/pystr.go:IsDigit|IsAlpha と同じ
internal/pystr/pystr.go:IsNumeric|IsAlpha と同じ
internal/pyre/pattern.go:Match.GroupDict|re の Match の groupdict。pyre が写す範囲として README の表にあり、pyre.json の記録と比べる
internal/pyre/pattern.go:Pattern.Pattern|lint と visibility のテスト (別の package) が、組んだパターンの文字列を記録と比べるのに呼ぶ。export_test.go は自分のフォルダのテストにしか入らない
internal/pyre/lazy.go:Lazy.Flags|lint と diff のテスト (別の package) が、パターンのフラグを記録と比べるのに呼ぶ。export_test.go は自分のフォルダのテストにしか入らない
internal/pyjson/round.go:RoundInt|round(x)。pyjson が写す範囲として README の表にあり、json.json の記録 (503 件) と比べる
internal/difflib/difflib.go:SequenceMatcher.FindLongestMatch|difflib の find_longest_match。写すメソッドの 1 つとして README にあり、difflib.json の記録と比べる
ALLOWEOF
)

case "${1:-}" in
  "") ;;
  -h | --help)
    sed -n '2,/^set -euo pipefail$/p' "$0" | sed -e '$d' -e 's/^# \{0,1\}//'
    exit 0
    ;;
  *)
    echo "deadcode_check: 引数は取らない (--help で説明を出す)" >&2
    exit 2
    ;;
esac

cd "$(dirname "$(readlink -f "$0")")"

fail() {
  echo "deadcode_check: $*" >&2
  exit 1
}

# 改行区切りの並び $1 に、行 $2 があるか。grep に並びを渡さない (空の並びを -F で渡すと全部の行に当たる)。
has_line() {
  case $'\n'"$1"$'\n' in
    *$'\n'"$2"$'\n'*) return 0 ;;
  esac
  return 1
}

envs=$(go env GOVERSION GOOS GOARCH CGO_ENABLED)
{
  read -r gover
  read -r goos
  read -r goarch
  read -r cgo
} <<<"$envs"
[ -n "$gover" ] || fail "go env から GOVERSION を読めない"
root=${DOTFILES_GO_TOOLS_CACHE:-${XDG_CACHE_HOME:-$HOME/Library/Caches}/dotfiles-go-tools}
dir="$root/deadcode@$DEADCODE_VERSION-$gover-$goos-$goarch-$cgo"
bin="$dir/deadcode"
if [ ! -x "$bin" ] || [ ! -s "$bin" ]; then
  mkdir -p "$dir"
  tmp=$(mktemp -d "$dir/.install.XXXXXX")
  trap 'rm -rf "$tmp"' EXIT
  # 並列の lint に書きかけのバイナリを起動させないよう、一時のフォルダに作ってから rename で置く
  GOBIN="$tmp" go install "golang.org/x/tools/cmd/deadcode@$DEADCODE_VERSION"
  mv -f "$tmp/deadcode" "$bin"
  rm -rf "$tmp"
  trap - EXIT
fi

rc=0
out=$("$bin" ./...) || rc=$?
[ "$rc" -eq 0 ] || fail "deadcode が落ちた (rc=${rc})"

keys=""
tool=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  # `diff/tone.go:658:6: unreachable func: Tone` → `diff/tone.go:Tone`
  file=${line%%:*}
  name=${line##*: unreachable func: }
  if [ "$name" = "$line" ] || [ -z "$file" ] || [ "$file" = "$line" ]; then
    fail "解釈できない deadcode の出力: ${line}"
  fi
  case "$file" in
    oracle/*)
      tool=$((tool + 1))
      continue
      ;;
  esac
  keys="${keys}${file}:${name}"$'\n'
done <<<"$out"

# oracle の道具には、テストからだけ呼ぶ関数がいつもある。0 件なら、deadcode が module を読めていない
[ "$tool" -gt 0 ] || fail "\`oracle/\` の行が 0 件 (deadcode が module を読めていない。oracle の届かない関数を全部片付けたなら、この確かめを別の形にする)"

allowkeys=""
while IFS= read -r entry; do
  [ -n "$entry" ] || continue
  allowkeys="${allowkeys}${entry%%|*}"$'\n'
done <<<"$ALLOW"

fails=0
while IFS= read -r key; do
  [ -n "$key" ] || continue
  if ! has_line "$allowkeys" "$key"; then
    echo "deadcode_check: どの main からも届かない: ${key}" >&2
    fails=$((fails + 1))
  fi
done <<<"$keys"

allowed=0
while IFS= read -r entry; do
  [ -n "$entry" ] || continue
  allowed=$((allowed + 1))
  if ! has_line "$keys" "${entry%%|*}"; then
    echo "deadcode_check: ALLOW の行がもう報告されない (製品から呼ばれるようになった・消した・移した。行を外す): ${entry%%|*}" >&2
    fails=$((fails + 1))
  fi
done <<<"$ALLOW"

if [ "$fails" -gt 0 ]; then
  echo "deadcode_check: ${fails} 件。同じフォルダのテストからだけ呼ぶ関数は export_test.go へ移す (--help に直し方)" >&2
  exit 1
fi
echo "deadcode_check: 製品の package に届かない関数なし (deadcode ${DEADCODE_VERSION}。理由つきで残した ${allowed} 件と oracle/ の ${tool} 件を除く)"
