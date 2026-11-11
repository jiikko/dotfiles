#!/usr/bin/env bash
# bin/serial-run の入口がビルドできないとき: 既定は 125 で落ち、SERIAL_RUN_FAIL_OPEN=1 (PATH shim が付ける) なら
# -- の後のコマンドを順番を待たずに起こすことを固定する。ビルドの失敗は go_autobuild を偽物に差し替えたコピーで作る
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

td=$(mktemp -d) || exit 1
trap 'rm -rf "$td"' EXIT
mkdir -p "$td/bin/lib"
cp "$ROOT_DIR/bin/serial-run" "$td/bin/serial-run"
printf 'go_autobuild_exec() { return 1 }\n' > "$td/bin/lib/go_autobuild.zsh"

PASS=0
FAIL=0
check() {  # $1=説明 $2=期待する rc $3=期待する stdout 残り=serial-run の前に置く env の足し と引数 (-- 区切り)
  local desc="$1" want_rc="$2" want_out="$3"
  shift 3
  local -a extra=()
  while [ "$1" != :: ]; do extra+=("$1"); shift; done
  shift
  local out rc
  out=$(env -u SERIAL_RUN_FAIL_OPEN -u SERIAL_RUN_HELD ${extra[@]+"${extra[@]}"} "$td/bin/serial-run" "$@" 2>"$td/err")
  rc=$?
  if [ "$rc" = "$want_rc" ] && [ "$out" = "$want_out" ]; then
    echo "✓ $desc"
    PASS=$((PASS + 1))
  else
    echo "✗ $desc: rc=$rc out=[$out] (want rc=$want_rc out=[$want_out]) stderr: $(cat "$td/err")"
    FAIL=$((FAIL + 1))
  fi
}

# shellcheck disable=SC2016 # $ は子の sh が展開する
child='echo "[${SERIAL_RUN_FAIL_OPEN-unset}]"; exit 4'
check "ビルドできなければ 125 (既定)" 125 "" :: -- /bin/sh -c "$child"
check "SERIAL_RUN_FAIL_OPEN なら -- の後を起こし、その rc を返す (子へは印を渡さない)" 4 "[unset]" \
  SERIAL_RUN_FAIL_OPEN=1 :: --if-heavy --max-wait 0 -- /bin/sh -c "$child"
check "SERIAL_RUN_FAIL_OPEN でも -- の後が空なら 125" 125 "" SERIAL_RUN_FAIL_OPEN=1 :: --if-heavy --
check "SERIAL_RUN_FAIL_OPEN でも -- が無ければ 125" 125 "" SERIAL_RUN_FAIL_OPEN=1 :: status

if [ "$FAIL" -gt 0 ]; then
  echo "✗ bin/serial-run の fail-open: $FAIL 件失敗 ($PASS 件通過)"
  exit 1
fi
echo "✓ bin/serial-run の fail-open: すべて通過 ($PASS 件)"
