#!/usr/bin/env bash
# 直列 runner (Makefile の run_tests) が、stdin を読むテストに残りの一覧を食べさせないことを固定する。
#
# なぜ: run_tests は一覧を while に stdin で流す。テストを stdin のまま起動すると、stdin を読むテストが
# 残りの一覧を食べ、後ろのテストが黙って走らない (実測 2026-10-07: make test-dir DIR=tests で 170 本中 129 本が
# 走らず、名前の途中で切れた 1 行の失敗だけが出た)。make test の直列の枠 (SERIAL_TEST_DIRS) と
# test-changed が使う make test-dir がこの runner を通る。
#
# 実際に runner を動かす: stdin を読み切るテストの後ろに置いたテストが走ったかを、印のファイルで見る。
set -uo pipefail
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/runner-stdin.XXXXXX") || { echo "✗ mktemp -d 失敗"; exit 1; }
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/suite"
marker="$work/b-ran"
# 一覧の順 (sort) で a が先、b が後になる名前にする
printf '#!/bin/sh\ncat >/dev/null\nexit 0\n' > "$work/suite/test_a_reads_stdin.sh"
printf '#!/bin/sh\n: > "%s"\nexit 0\n' "$marker" > "$work/suite/test_b_after.sh"
chmod +x "$work/suite/"test_*.sh

out="$work/out.log"
env -u MAKEFLAGS -u MAKELEVEL -u MFLAGS make -C "$ROOT_DIR" --no-print-directory test-dir DIR="$work/suite" > "$out" 2>&1
rc=$?

fails=0
# 前提: runner が a を実際に走らせた (走っていなければ、下の判定は何も検査しない)
if grep -qF "[run] $work/suite/test_a_reads_stdin.sh" "$out"; then
  printf '✓ 前提: stdin を読むテストが runner で走った\n'
else
  printf '✗ 前提が崩れた: stdin を読むテストが走っていない\n'; cat "$out"; fails=1
fi
if [ -f "$marker" ]; then
  printf '✓ stdin を読むテストの後ろのテストも走る\n'
else
  printf '✗ stdin を読むテストの後ろのテストが走っていない (一覧を食べられた)\n'; cat "$out"; fails=1
fi
if [ "$rc" -eq 0 ]; then
  printf '✓ runner は rc=0\n'
else
  printf '✗ runner が rc=%s\n' "$rc"; cat "$out"; fails=1
fi
[ "$fails" -eq 0 ] || exit 1
printf '=== runner の stdin 隔離: すべて成功 ===\n'
