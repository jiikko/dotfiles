#!/usr/bin/env bash
# bin/gopls shim は隔離 HOME / PATH の偽実体だけを起動する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SHIM="$ROOT_DIR/bin/gopls"
TMP_ROOT="$ROOT_DIR/tmp"
# shellcheck disable=SC1091
source "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR"
mkdir -p "$TMP_ROOT"
TMP_DIR="$(mktemp -d "$TMP_ROOT/gopls-test.XXXXXX")"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

fail=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fail=1; }

HOME_DIR="$TMP_DIR/home"
MASON_BIN="$HOME_DIR/.local/share/nvim/mason/bin"
PATH_BIN="$TMP_DIR/path-bin"
ALIAS_BIN="$TMP_DIR/alias-bin"
HARDLINK_BIN_A="$TMP_DIR/hardlink-bin-a"
HARDLINK_BIN_B="$TMP_DIR/hardlink-bin-b"
REAL_BIN="$TMP_DIR/real-bin"
CALL_LOG="$TMP_DIR/calls.log"
mkdir -p "$MASON_BIN" "$PATH_BIN" "$ALIAS_BIN" "$HARDLINK_BIN_A" "$HARDLINK_BIN_B" "$REAL_BIN"
export CALL_LOG

# 偽の実体は自分の名前 (label) を焼き込んで記録する。呼び出し側の環境から名前を取ると、どの実体が呼ばれても
# 同じ名前が記録され、優先順位の退行 (mason より PATH を先に引く) が見えなくなる。
make_fake() {
  local path="$1" label="$2"
  cat > "$path" <<SH
#!/bin/sh
printf '%s\n' '$label' >> "\$CALL_LOG"
for arg do printf '<%s>\n' "\$arg" >> "\$CALL_LOG"; done
stdin=\$(cat)
printf '<stdin:%s>\n' "\$stdin" >> "\$CALL_LOG"
printf 'GOPLS_STDOUT\n'
printf 'GOPLS_STDERR\n' >&2
exit 23
SH
  chmod +x "$path"
}

run_isolated() {
  /usr/bin/env -i HOME="$HOME_DIR" CALL_LOG="$CALL_LOG" \
    PATH="$ROOT_DIR/bin:$PATH_BIN:$ALIAS_BIN:/usr/bin:/bin" "$SHIM" "$@"
}

check_transport() {
  local label="$1"
  shift
  local stderr_file="$TMP_DIR/stderr"
  local stdout stderr rc expected_log
  : > "$CALL_LOG"
  set +e
  stdout=$(printf '%s' 'stdin payload' | run_isolated "$@" 2>"$stderr_file")
  rc=$?
  set -e
  stderr=$(cat "$stderr_file")
  expected_log="$label"
  for arg do expected_log+=$'\n<'"$arg"'>'; done
  expected_log+=$'\n<stdin:stdin payload>'
  if [ "$rc" -eq 23 ] && [ "$stdout" = 'GOPLS_STDOUT' ] && [ "$stderr" = 'GOPLS_STDERR' ] \
    && [ "$(cat "$CALL_LOG")" = "$expected_log" ]; then
    ok "$label 経路で stdin / stdout / stderr / rc=23 / 引数境界を透過する"
  else
    ng "$label 経路の透過結果が違う (rc=$rc stdout=$stdout stderr=$stderr log=$(cat "$CALL_LOG"))"
  fi
}

make_fake "$MASON_BIN/gopls" mason
make_fake "$PATH_BIN/gopls" path
check_transport mason 'two words' '--flag=value'

rm -f "$MASON_BIN/gopls"
check_transport path 'path argument'

rm -f "$PATH_BIN/gopls"
: > "$CALL_LOG"
set +e
missing_output=$(/usr/bin/env -i HOME="$HOME_DIR" CALL_LOG="$CALL_LOG" \
  PATH="$ROOT_DIR/bin:/usr/bin:/bin" "$SHIM" 2>&1)
missing_rc=$?
set -e
if [ "$missing_rc" -ne 0 ] && [ "$missing_rc" -eq 127 ] && [[ "$missing_output" == *"Mason"* ]] && [ ! -s "$CALL_LOG" ]; then
  ok "実体が無いと案内を出して rc=127"
else
  ng "実体なしの結果が違う (rc=$missing_rc): $missing_output"
fi

ln -s "$SHIM" "$ALIAS_BIN/gopls"
: > "$CALL_LOG"
set +e
self_output=$(/usr/bin/env -i HOME="$HOME_DIR" CALL_LOG="$CALL_LOG" \
  PATH="$ROOT_DIR/bin:$ALIAS_BIN:/usr/bin:/bin" "$SHIM" 2>&1)
self_rc=$?
set -e
if [ "$self_rc" -eq 70 ] && [[ "$self_output" == *"shim 自身"* ]] && [ ! -s "$CALL_LOG" ]; then
  ok "別 PATH entry から shim 自身へ解決した場合は rc=70"
else
  ng "自己参照を拒否できなかった (rc=$self_rc): $self_output"
fi

ln -s "$SHIM" "$MASON_BIN/gopls"
: > "$CALL_LOG"
set +e
mason_self_output=$(/usr/bin/env -i HOME="$HOME_DIR" CALL_LOG="$CALL_LOG" \
  PATH="$ROOT_DIR/bin:$PATH_BIN:/usr/bin:/bin" "$SHIM" 2>&1)
mason_self_rc=$?
set -e
if [ "$mason_self_rc" -eq 70 ] && [[ "$mason_self_output" == *"shim 自身"* ]] && [ ! -s "$CALL_LOG" ]; then
  ok "Mason path が shim 自身を指す場合も rc=70"
else
  ng "Mason の自己参照を拒否できなかった (rc=$mason_self_rc): $mason_self_output"
fi

# A と B は同じ shim inode の hard link。A から B を引いた後も B から A に戻らず rc=70 で終わる。
rm -f "$MASON_BIN/gopls"
ln "$SHIM" "$HARDLINK_BIN_A/gopls"
ln "$HARDLINK_BIN_A/gopls" "$HARDLINK_BIN_B/gopls"
make_fake "$REAL_BIN/gopls" path-real
: > "$CALL_LOG"
set +e
hardlink_output=$(/usr/bin/env -i HOME="$HOME_DIR" CALL_LOG="$CALL_LOG" \
  PATH="$HARDLINK_BIN_A:$HARDLINK_BIN_B:$REAL_BIN:/usr/bin:/bin" \
  "$RUNTIMEOUT" 5 "$HARDLINK_BIN_A/gopls" 2>&1)
hardlink_rc=$?
set -e
if [ "$hardlink_rc" -eq 70 ] && [[ "$hardlink_output" == *"shim 自身"* ]] && [ ! -s "$CALL_LOG" ]; then
  ok "別 PATH entry の hard link も上限内に自己参照として rc=70"
else
  ng "hard link の自己参照を拒否できなかった (rc=$hardlink_rc): $hardlink_output"
fi

[ "$fail" -eq 0 ] || exit 1
printf '=== bin/gopls tests: all passed ===\n'
