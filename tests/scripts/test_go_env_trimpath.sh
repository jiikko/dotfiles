#!/usr/bin/env bash
# test_go_env_trimpath.sh — scripts/go_env_trimpath.sh が go env の GOFLAGS に -trimpath を足す規則の検査。
# GOENV を一時ファイルに向けて実ユーザーの go env を書かない。
set -uo pipefail
unset CDPATH
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
SCRIPT="$ROOT/scripts/go_env_trimpath.sh"
if ! command -v go >/dev/null 2>&1; then
	echo "[test_go_env_trimpath] skipped: go が無い" >&2
	exit 77
fi
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
fail=0
ran=0

# case <名前> <書いておく GOFLAGS (空ならファイルを作らない)> <期待する GOFLAGS> [<期待する rc>]
case_() {
	local name=$1 seed=$2 want=$3 want_rc=${4:-0} env="$TMP/env.$RANDOM$RANDOM" got rc
	if [ -n "$seed" ]; then
		GOENV=$env GOFLAGS='' go env -w GOFLAGS="$seed" || { echo "✗ $name: 前提の go env -w に失敗"; fail=1; return; }
	fi
	# 親の GOFLAGS が立っていても、それをファイルへ写さないこと
	GOENV=$env GOFLAGS=-x "$SCRIPT" >/dev/null 2>&1
	rc=$?
	got=$(GOENV=$env GOFLAGS='' go env GOFLAGS) || { echo "✗ $name: 読み戻しに失敗"; fail=1; return; }
	ran=$((ran + 1))
	if [ "$rc" != "$want_rc" ] || [ "$got" != "$want" ]; then
		echo "✗ $name: rc=$rc (want $want_rc) GOFLAGS='$got' (want '$want')"
		fail=1
		return
	fi
	# 2 回目は変わらない (冪等)
	GOENV=$env "$SCRIPT" >/dev/null 2>&1
	rc=$?
	got=$(GOENV=$env GOFLAGS='' go env GOFLAGS) || { echo "✗ $name: 2 回目の読み戻しに失敗"; fail=1; return; }
	if [ "$rc" != "$want_rc" ] || [ "$got" != "$want" ]; then
		echo "✗ $name: 2 回目: rc=$rc (want $want_rc) GOFLAGS='$got' (want '$want')"
		fail=1
	fi
}

case_ "空なら足す" "" "-trimpath"
case_ "既存の値を残して後ろに足す" "-mod=mod" "-mod=mod -trimpath"
case_ "既にあれば触らない" "-trimpath" "-trimpath"
case_ "明示の false を上書きしない" "-trimpath=false" "-trimpath=false"
case_ "後ろの false も上書きしない" "-trimpath -trimpath=false" "-trimpath -trimpath=false"
case_ "-- の形も既にあるとみなす" "--trimpath=true" "--trimpath=true"
case_ "引用符の中の別のフラグの引数は数えない" "'-gcflags=all=-N -trimpath /old=>/new'" "'-gcflags=all=-N -trimpath /old=>/new' -trimpath"

if [ "$ran" -lt 7 ]; then
	echo "✗ 走ったケースが $ran 件 (下限 7)"
	fail=1
fi
[ "$fail" = 0 ] && echo "OK: go_env_trimpath の規則 ($ran 件)"
exit "$fail"
