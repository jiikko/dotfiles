#!/usr/bin/env bash
# src/*/.golangci.yml が全部 `run.allow-parallel-runners: true` を持つことを pin する。
#
# なぜ: root の Makefile (run_go_projects) は 7 プロジェクトの lint を並列に回す。golangci-lint は
# 既定で os.TempDir() のグローバル file lock を取るので、**1 プロジェクトでも設定が抜けると、
# そのプロジェクトだけが他の起動タイミング次第で "parallel golangci-lint is running" で落ちる**
# (flaky に見え、抜けたプロジェクトが原因だと気づきにくい)。新しい src/ を切ったときの漏れを
# ここで止める。issue 258
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

fail=0; n=0
for mod in src/*/go.mod; do
  [ -f "$mod" ] || continue
  dir=${mod%/go.mod}; yml="$dir/.golangci.yml"; n=$((n + 1))
  if [ ! -f "$yml" ]; then
    printf '  ✗ %s: .golangci.yml が無い (lint は既定設定で走り、lock が既定 = 並列で落ちる)\n' "$dir"; fail=1; continue
  fi
  # run: 節の下の allow-parallel-runners: true。インデントは 2 固定 (repo 内の書き方)。
  # 🚨 `grep -q` を `|` の右に置かない (test-pipefail-grep-q)。ファイルを直接渡す
  if grep -qE '^run:' "$yml" && grep -qE '^  allow-parallel-runners: true$' "$yml"; then
    printf '  ✓ %s\n' "$dir"
  else
    printf '  ✗ %s: run.allow-parallel-runners: true が無い\n' "$dir"; fail=1
  fi
done
# 発見 0 件は失敗 (src/ の配置が変わって対象を見失っても緑にしない)
[ "$n" -gt 0 ] || { printf '✗ src/*/go.mod が 1 つも見つからない (発見が壊れている)\n'; exit 1; }
[ "$fail" -eq 0 ] || { printf '✗ allow-parallel-runners の漏れあり (%d プロジェクト検査)\n' "$n"; exit 1; }
printf '✓ %d プロジェクトすべてに run.allow-parallel-runners: true がある\n' "$n"
