#!/usr/bin/env bash
# 本物の git を起こすテストのパッケージが、TestMain で subproc.UnsetGitLocationEnv を呼ぶことを pin する。
#
# なぜ: hook の中など GIT_DIR / GIT_WORK_TREE を継承した環境で go test を走らせると、`git -C <一時dir> init / add / commit` が
# 一時 dir ではなく継承した repo を再 init し、そこへ commit する (git 2.55.0 で再現)。
# 脅威モデル: うっかりの漏れ (fixture の repo を作るテストを足したパッケージで外し忘れる) を止める。
# 呼んだと認める形 (直接 / internal/testenv の Main 経由) は tests/lib/go_testmain_guard.sh の冒頭。
# 拾う形: _test.go に `exec.Command("git"` / `exec.CommandContext(ctx, "git"` を書いたディレクトリ。
# 検出しない形: 呼んでいるが m.Run の後 (効かない)・TestMain 以外の関数の中・コメントの中・testdata の下・`//go:build ignore` などで外れる _test.go の中・
# git をヘルパー (コマンド名を変数で渡す・別パッケージの関数) 経由でだけ起こすパッケージ・テストから呼ぶ production の関数が起こす git だけのパッケージ
# 偽陽性で受けるもの: 文字列リテラルの中の `exec.Command("git"` (git を起こさないパッケージに外す呼び出しを求める。安全側なので直さない)
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1
# shellcheck source=tests/lib/go_testmain_guard.sh
. "$ROOT_DIR/tests/lib/go_testmain_guard.sh"

pkgs=0; bad=0
while IFS= read -r dir; do
  shopt -s nullglob
  tests=("$dir"/*_test.go)
  shopt -u nullglob
  # 読めないファイルは判定不能として落とす (grep の rc=2 を「無い」に丸めない)
  grep -lqE 'exec\.Command(Context)?\((ctx, )?"git"' "${tests[@]}"; rc=$?
  [ "$rc" -le 1 ] || { printf '✗ %s の _test.go を読めない (判定できない)\n' "$dir" >&2; exit 1; }
  [ "$rc" -eq 0 ] || continue
  pkgs=$((pkgs + 1))
  go_testmain_guard_present "$dir" '(subproc\.)?UnsetGitLocationEnv\(\)'; rc=$?
  [ "$rc" -le 1 ] || { printf '✗ %s の _test.go を読めない (判定できない)\n' "$dir" >&2; exit 1; }
  if [ "$rc" -ne 0 ]; then
    printf '✗ %s: テストが本物の git を起こすのに、subproc.UnsetGitLocationEnv を呼ばない (TestMain の m.Run の前で呼ぶ)\n' "$dir" >&2
    bad=$((bad + 1))
  fi
done < <(find src -name '*_test.go' ! -path '*/testdata/*' -exec dirname {} \; | sort -u)

# 空振り (走査の根や正規表現が壊れて 0 件) を緑にしない。2026-10-12 に 9 パッケージ
if [ "$pkgs" -lt 8 ]; then
  printf '✗ 本物の git を起こすテストのパッケージが %d 個しか見つからない (走査が壊れている)\n' "$pkgs" >&2
  exit 1
fi
[ "$bad" -eq 0 ] || exit 1
printf '✓ 本物の git を起こすテストのパッケージ %d 個がすべて継承した GIT_DIR などを外す\n' "$pkgs"
