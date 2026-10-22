#!/usr/bin/env bash
# src/*/ の各モジュールの、合成した golangci-lint の設定 (src/.golangci.yml + .golangci.local.yml を scripts/golangci_config.rb が
# 合わせたもの) が `run.allow-parallel-runners: true` と `run.relative-path-mode: gomod` を持つことを pin する。
#
# なぜ: root の Makefile (run_go_projects) は全プロジェクトの lint を並列に回す。golangci-lint は
# 既定で os.TempDir() のグローバル file lock を取るので、**1 プロジェクトでも設定が抜けると、
# そのプロジェクトだけが他の起動タイミング次第で "parallel golangci-lint is running" で落ちる**
# (flaky に見え、抜けたプロジェクトが原因だと気づきにくい)。relative-path-mode は、合成した設定を一時ファイルで渡すと
# 除外のパスの基準がずれて黙って効かなくなるのを止める。どちらも差分の側が上書きできるので、ベースではなく合成した結果を見る。
# モジュールに .golangci.yml が残っていないことも見る (残ると素の golangci-lint がその写しを読み、合成の外に出る)。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

fail=0; n=0
for mod in src/*/go.mod; do
  [ -f "$mod" ] || continue
  dir=${mod%/go.mod}; n=$((n + 1))
  if [ -e "$dir/.golangci.yml" ]; then
    printf '  ✗ %s: .golangci.yml がある (モジュールの設定は差分だけを .golangci.local.yml に書く)\n' "$dir"; fail=1; continue
  fi
  locals=()
  [ -e "$dir/.golangci.local.yml" ] && locals=("$dir/.golangci.local.yml")
  if ! cfg=$(ruby scripts/golangci_config.rb src/.golangci.yml ${locals[@]+"${locals[@]}"}); then
    printf '  ✗ %s: 設定を合成できない\n' "$dir"; fail=1; continue
  fi
  got=$(printf '%s' "$cfg" | ruby -rjson -e 'r = JSON.parse($stdin.read)["run"] || {}; puts "#{r["allow-parallel-runners"].inspect} #{r["relative-path-mode"].inspect}"')
  if [ "$got" = 'true "gomod"' ]; then
    printf '  ✓ %s\n' "$dir"
  else
    printf '  ✗ %s: 合成した設定の run が allow-parallel-runners: true / relative-path-mode: gomod でない (%s)\n' "$dir" "$got"; fail=1
  fi
done
[ "$n" -gt 0 ] || { printf '✗ src/*/go.mod が 1 つも見つからない (発見が壊れている)\n'; exit 1; }
[ "$fail" -eq 0 ] || { printf '✗ 合成した設定の run の漏れあり (%d プロジェクト検査)\n' "$n"; exit 1; }
printf '✓ %d プロジェクトすべての合成した設定に run.allow-parallel-runners: true と relative-path-mode: gomod がある\n' "$n"
