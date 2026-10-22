#!/bin/bash
# 新品チェックアウト (= git に追跡されているものだけがある状態) で任意のコマンドを回す。
#
# 用途: 「手元には在るが git に載っていないもの」への依存を push 前に炙り出す。
# ignore されているもの (tmp/) だけでなく、空ディレクトリ (issues/next/) や untracked も
# 同じ形で壊れるので、判定は「ignore か」ではなく「git に載っているか」で行う必要がある。
# git worktree はまさにその状態を作るので、環境差の再現に使う。
#
# 使い方: scripts/with_fresh_worktree.sh <command> [args...]
#   worktree の中へ cd してから <command> を実行する。入口は Makefile の test-fresh。
#
# worktree の作成と後始末 (stale sweep / 残骸の再試行 / 子プロセスの停止) は
# scripts/lib/worktree_scratch.sh が持つ (bin/mutate-verify と共通)。
set -euo pipefail

# 新品にするのは cwd の repo。継承した GIT_DIR などが別の repo を指すと、その repo の HEAD から worktree を作る
# (make test-fresh は Makefile の unexport で外れているが、直接起動したときは外れていない)
# shellcheck source=bin/lib/git_location_env.sh
. "$(dirname "$(readlink -f "$0")")/../bin/lib/git_location_env.sh" ||
  { echo "with_fresh_worktree: bin/lib/git_location_env.sh を読めない" >&2; exit 2; }
git_location_env_unset || { echo "with_fresh_worktree: 継承した git の場所の環境変数 (GIT_DIR など) を外せない" >&2; exit 2; }

root="$(git rev-parse --show-toplevel)"
# shellcheck source=scripts/lib/worktree_scratch.sh
. "$root/scripts/lib/worktree_scratch.sh"

wts_init "$root" "dotfiles-fresh"
wts_create HEAD

cd "$WTS_PATH" || exit 1
# 親 make の jobserver を持ち込まない (再帰 make の警告と取り合いを避ける)
unset MAKEFLAGS MAKELEVEL
"$@"
