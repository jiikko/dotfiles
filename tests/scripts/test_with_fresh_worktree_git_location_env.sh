#!/usr/bin/env bash
# scripts/with_fresh_worktree.sh を直接起動したとき、継承した GIT_DIR / GIT_WORK_TREE が別の repo (B) を指していても、
# cwd の repo (A) の新品の checkout でコマンドを回すことを pin する。外さないと B の HEAD から worktree を作り、B の中身で動く。
# make test-fresh は Makefile の unexport で外れるので、ここでは make を通さずに起動する。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fails=0
fail() { echo "✗ $*" >&2; fails=$((fails + 1)); }
ok() { echo "✓ $*"; }

td=$(mktemp -d) || exit 1
trap 'rm -rf "$td"' EXIT
td=$(cd "$td" && pwd -P) || exit 1

# A は道具を自分の中から読む (with_fresh_worktree.sh は $root/scripts/lib/worktree_scratch.sh を読む) ので、道具ごと commit する
for r in A B; do
  git init -q "$td/$r" || exit 1
  git -C "$td/$r" config user.email t@t; git -C "$td/$r" config user.name t
  printf '%s\n' "$r" > "$td/$r/who"
done
mkdir -p "$td/A/scripts/lib" "$td/A/bin/lib"
cp "$ROOT_DIR/scripts/with_fresh_worktree.sh" "$td/A/scripts/"
cp "$ROOT_DIR/scripts/lib/worktree_scratch.sh" "$td/A/scripts/lib/"
cp "$ROOT_DIR/bin/lib/git_location_env.sh" "$td/A/bin/lib/"
git -C "$td/A" add -A && git -C "$td/A" commit -qm A || exit 1
git -C "$td/B" add -A && git -C "$td/B" commit -qm B || exit 1
b_head=$(git -C "$td/B" rev-parse HEAD)
b_index=$(cksum < "$td/B/.git/index")

# GIT_INDEX_FILE=.git/index は pre-commit の hook が付ける形
for spec in "GIT_DIR=$td/B/.git" "GIT_WORK_TREE=$td/B" "GIT_INDEX_FILE=.git/index" "GIT_INDEX_FILE=$td/B/.git/index"; do
  rc=0
  out=$(cd "$td/A" && env "$spec" TMPDIR="$td" ./scripts/with_fresh_worktree.sh cat who 2>"$td/err") || rc=$?
  if [ "$rc" -eq 0 ] && [ "$out" = A ]; then
    ok "$spec を継承しても cwd の repo の新品の checkout で動く"
  else
    fail "$spec を継承すると rc=$rc / 出力=$out (期待 rc=0 / A): $(cat "$td/err")"
  fi
done
[ "$(git -C "$td/B" rev-parse HEAD)" = "$b_head" ] || fail "B の HEAD が動いた"
[ "$(cksum < "$td/B/.git/index")" = "$b_index" ] || fail "B の index を書き換えた"
[ "$(git -C "$td/B" worktree list | wc -l | tr -d ' ')" = 1 ] || fail "B に worktree を残した: $(git -C "$td/B" worktree list)"

[ "$fails" -eq 0 ] || exit 1
