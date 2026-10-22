#!/usr/bin/env bash
# scripts/golangci_config.rb (golangci-lint の設定の合成) の規則を、期待するバイト列で固定する。
# 見るもの: 鍵の並び (ベースの順 → 差分だけの鍵を差分の順)・一覧 (ベースの後に差分の新しい要素)・map の再帰の合わせ方・
# 同じ入力から同じバイト列 (2 回 / 入っている ruby の版どうし)・差分なしの合成・壊れた入力と「差分が共通の塊を外す・書き換える」形で rc=2。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1
gen=scripts/golangci_config.rb
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0
ng() { printf '  ✗ %s\n' "$1"; fail=1; }

cat >"$work/base.yml" <<'Y'
version: "2"
run:
  allow-parallel-runners: true
linters:
  enable:
    - b
    - a
  settings:
    x:
      p: 1
      q: 1
formatters:
  enable:
    - gofmt
Y
cat >"$work/local.yml" <<'Y'
issues:
  max: 3
linters:
  enable:
    - c
    - a
  settings:
    y: 2
    x:
      q: 1
      r: 1
Y
want='{
  "version": "2",
  "run": {
    "allow-parallel-runners": true
  },
  "linters": {
    "enable": [
      "b",
      "a",
      "c"
    ],
    "settings": {
      "x": {
        "p": 1,
        "q": 1,
        "r": 1
      },
      "y": 2
    }
  },
  "formatters": {
    "enable": [
      "gofmt"
    ]
  },
  "issues": {
    "max": 3
  }
}'
got=$(ruby "$gen" "$work/base.yml" "$work/local.yml"); rc=$?
[ "$rc" -eq 0 ] || ng "合成が rc=$rc"
[ "$got" = "$want" ] || ng "合成の結果が期待と違う:
$got"
again=$(ruby "$gen" "$work/base.yml" "$work/local.yml")
[ "$again" = "$got" ] || ng "同じ入力で 2 回目の出力が違う"
# 入っている ruby の版どうしでも同じバイト列 (macOS の /usr/bin/ruby は 2.6。PATH の ruby と別の版のことがある)
sysruby=/usr/bin/ruby
if [ -x "$sysruby" ]; then
  other=$("$sysruby" "$gen" "$work/base.yml" "$work/local.yml")
  [ "$other" = "$got" ] || ng "$sysruby ($("$sysruby" -e 'print RUBY_VERSION')) と ruby ($(ruby -e 'print RUBY_VERSION')) で出力が違う"
fi

only=$(ruby "$gen" "$work/base.yml") || ng "差分なしの合成が落ちた"
printf '%s' "$only" | ruby -rjson -ryaml -e 'exit(JSON.parse($stdin.read) == YAML.load_file(ARGV[0]) ? 0 : 1)' "$work/base.yml" ||
  ng "差分なしの合成がベースと同じ値にならない"

printf 'version: "3"\n' >"$work/v3.yml"
ruby "$gen" "$work/base.yml" "$work/v3.yml" >/dev/null 2>&1; rc=$?
[ "$rc" -eq 2 ] || ng "差分の version がベースと違うのに rc=$rc (2 で落とす)"
ruby "$gen" "$work/nothing.yml" >/dev/null 2>&1; rc=$?
[ "$rc" -eq 2 ] || ng "ベースが無いのに rc=$rc"
printf -- '- a\n' >"$work/list.yml"
ruby "$gen" "$work/list.yml" >/dev/null 2>&1; rc=$?
[ "$rc" -eq 2 ] || ng "最上位が map でないのに rc=$rc"
printf 'a: [\n' >"$work/broken.yml"
ruby "$gen" "$work/base.yml" "$work/broken.yml" >/dev/null 2>&1; rc=$?
[ "$rc" -eq 2 ] || ng "YAML として読めないのに rc=$rc"

# 差分が共通の塊を黙って外す・書き換える形は全部 rc=2 (golangci-lint はどれもエラーにせず、共通の linter が消えて緑になる)
reject() { # $1=説明 $2=差分の中身
  printf '%s\n' "$2" >"$work/bad.yml"
  ruby "$gen" "$work/base.yml" "$work/bad.yml" >/dev/null 2>&1; rc=$?
  [ "$rc" -eq 2 ] || ng "$1 を通した (rc=$rc)"
}
reject '値の書き換え' 'linters: {settings: {x: {q: 9}}}'
reject '空の linters' 'linters:'
reject '一覧を文字列で上書き' 'linters: {enable: depguard}'
reject 'map を一覧で上書き' 'run: [x]'
reject 'run の値の書き換え' 'run: {allow-parallel-runners: false}'
reject 'linters.disable' 'linters: {disable: [a]}'
reject 'linters.default' 'linters: {default: none}'
reject 'formatters.disable' 'formatters: {disable: [gofmt]}'
reject '同じ鍵が 2 度' "$(printf 'linters:\n  enable: [c]\nlinters:\n  settings: {y: 1}')"
reject '入れ子の同じ鍵' "$(printf 'issues:\n  max: 1\n  max: 2')"
reject '2 つ目の文書' "$(printf 'issues: {max: 1}\n---\nlinters: {enable: [c]}')"

[ "$fail" -eq 0 ] || { printf '✗ golangci_config.rb の合成の規則が崩れている\n'; exit 1; }
printf '✓ golangci_config.rb: 鍵の並び・一覧の足し方・同じバイト列 (ruby %s と /usr/bin/ruby)・壊れた入力と共通の塊を外す差分の rc=2\n' "$(ruby -e 'print RUBY_VERSION')"
