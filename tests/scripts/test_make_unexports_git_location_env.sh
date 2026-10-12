#!/usr/bin/env bash
# make の recipe が、継承した git の repo の場所 (GIT_DIR など) を持たないことを pin する (issue 744)。
#
# なぜ: hook から起動された make test は GIT_DIR などを持ったまま走り、テストの `git -C <一時 dir>` がそれに負けて
# 継承した repo の config とブランチを書き換えた (2026-10-12 に scratch の repo で再現)。Makefile の unexport で外している。
# 正解役は本物の git (`git rev-parse --local-env-vars`) で、Makefile の字面は読まない。本物の make を起こし、
# recipe の環境にその変数が 1 つも残らないことを見る (unexport の書き損じ・一覧の漏れ・git の版上げで増えた変数を落とす)。
# GIT_NAMESPACE は --local-env-vars に出ないが Makefile と src/subproc/gitenv.go が外しているので、ここで足して固定する。
# 検出しない形: make を通さない直接実行 (tests/CLAUDE.md で禁止)・$(shell ...) の環境 (make 3.81 では unexport が効かない)・
# make のコマンドライン / MAKEFLAGS で渡した GIT_DIR=… (環境の継承ではない。src/*/Makefile の sub-make へ届くが、go test は TestMain で外す)
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

vars=$(git rev-parse --local-env-vars) || { echo "✗ git rev-parse --local-env-vars が失敗した (判定できない)" >&2; exit 1; }
vars+=$'\nGIT_NAMESPACE'
n=$(printf '%s\n' "$vars" | grep -c .)
# 空振りを緑にしない。git 2.55 で 15 個 + GIT_NAMESPACE
[ "$n" -ge 10 ] || { printf '✗ git が挙げた変数が %d 個しかない (取り出しが壊れている)\n' "$n" >&2; exit 1; }

td=$(mktemp -d) || exit 1
trap 'rm -rf "$td"' EXIT
# 値は存在しない場所にする。make の $(shell ...) (読むだけ) がこれを見ても、どこにも書かない
setenv=()
while IFS= read -r v; do
  case "$v" in
    GIT_CONFIG_COUNT) setenv+=("$v=0") ;;
    GIT_CONFIG_PARAMETERS) setenv+=("$v='probe.x'='1'") ;;
    *) setenv+=("$v=$td/nowhere") ;;
  esac
done <<<"$vars"

# 対照: unexport していない変数が recipe に届くことを、同じ取り方で確かめる (env の取り出しが壊れて 0 件 = 緑を防ぐ)
# 本物の Makefile を include した probe で recipe の環境を出す (make 3.81 には --eval が無い)
printf 'include Makefile\nprobe-744:\n\t@env\n' > "$td/probe.mk"
out=$(env "${setenv[@]}" PROBE_744_CANARY=1 make -C "$ROOT_DIR" -s --no-print-directory \
  -f "$td/probe.mk" probe-744 2>"$td/err"); rc=$?
[ "$rc" -eq 0 ] || { echo "✗ make の probe が rc=$rc で落ちた (判定できない):" >&2; cat "$td/err" >&2; exit 1; }
grep -qx 'PROBE_744_CANARY=1' <<<"$out" || { echo "✗ 対照の変数が recipe に届かない (env の取り出しが壊れている)" >&2; exit 1; }

bad=0
while IFS= read -r v; do
  if grep -q "^$v=" <<<"$out"; then
    printf '✗ %s が make の recipe に残る (Makefile の unexport に足す)\n' "$v" >&2
    bad=$((bad + 1))
  fi
done <<<"$vars"
[ "$bad" -eq 0 ] || exit 1
printf '✓ git が挙げる repo の場所の変数 %d 個が、どれも make の recipe に届かない\n' "$n"
