#!/usr/bin/env bash
# `scripts/pull_main_checkout.sh` が、本体の checkout の pull を決まった lock で 1 つずつにすることを確かめる (issue 544)。
# 一時的な origin と clone の上だけで動かす (本物の ~/dotfiles は触らない)。lockman は repo の bin/lockman を使う。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tests/lib/wait_until.sh
. "$ROOT_DIR/tests/lib/wait_until.sh"
cd "$ROOT_DIR" || exit 1
TMP_DIR="$(mktemp -d)"
holder=""
trap '[ -n "$holder" ] && kill "$holder" 2>/dev/null; rm -rf "$TMP_DIR"' EXIT

fail=0
note() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fail=1; }

export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
unset GIT_DIR GIT_WORK_TREE
git init -q --bare "$TMP_DIR/origin.git"
git clone -q "$TMP_DIR/origin.git" "$TMP_DIR/main" 2>/dev/null
git -C "$TMP_DIR/main" commit -q --allow-empty -m first
git -C "$TMP_DIR/main" push -q origin HEAD 2>/dev/null
git clone -q "$TMP_DIR/origin.git" "$TMP_DIR/other"
git -C "$TMP_DIR/other" commit -q --allow-empty -m second
git -C "$TMP_DIR/other" push -q origin HEAD 2>/dev/null
want=$(git -C "$TMP_DIR/other" rev-parse HEAD)

LM="$ROOT_DIR/bin/lockman"
# lockman は初回に Go でビルドする。Go の無い runner (CI の rest) では始められないので skip にする
# (exit 77 = runner が [skip] と数える)。Go があるのに lockman が動かないときは下の検査で赤にする
if ! "$LM" --help >/dev/null 2>&1 && ! command -v go >/dev/null 2>&1; then
  echo "SKIP: go が無く lockman をビルドできない"; exit 77
fi
run() { DOTFILES_DIR="$TMP_DIR/main" PULL_MAIN_LOCKMAN="$LM" PULL_MAIN_WAIT="${WAIT:-10s}" "$ROOT_DIR/scripts/pull_main_checkout.sh" > "$TMP_DIR/out" 2> "$TMP_DIR/err"; }

# 1. ほかが lock を持っている間は pull しない (rc 121・理由を出す・HEAD はそのまま)
dir="$(git -C "$TMP_DIR/main" rev-parse --path-format=absolute --git-common-dir)/dotfiles-locks/pull-main"
mkdir -p "$dir"
"$LM" with "$dir" --label test-holder -- bash -c "touch '$TMP_DIR/held'; while [ ! -f '$TMP_DIR/release' ]; do sleep 0.1; done" &
holder=$!
TT_WAIT_TICKS=200 TT_WAIT_TICK=0.05 tt_wait_until test -f "$TMP_DIR/held" || :
[ -f "$TMP_DIR/held" ] || { bad "lock を持つ側が始まらない (10 秒待った)"; exit 1; }
rc=0; WAIT=1s run || rc=$?
if [ "$rc" -eq 121 ] && grep -q 'ほかの pull が lock を持ったまま' "$TMP_DIR/err" && [ "$(git -C "$TMP_DIR/main" rev-parse HEAD)" != "$want" ]; then
  note "ほかが lock を持っている間は pull せずに rc 121 と理由を出す"
else
  bad "lock が持たれているのに pull した / rc か理由が違う (rc=$rc): $(cat "$TMP_DIR/err")"
fi
touch "$TMP_DIR/release"; wait "$holder" || true; holder=""

# 2. lock が空いていれば pull する
rc=0; run || rc=$?
if [ "$rc" -eq 0 ] && [ "$(git -C "$TMP_DIR/main" rev-parse HEAD)" = "$want" ]; then
  note "lock が空いていれば pull --rebase する"
else
  bad "空いているのに pull しない (rc=$rc): $(cat "$TMP_DIR/err")"
fi

# 3. git の checkout でない所を指したら rc 2 で断る
rc=0; DOTFILES_DIR="$TMP_DIR/nowhere" PULL_MAIN_LOCKMAN="$LM" "$ROOT_DIR/scripts/pull_main_checkout.sh" > /dev/null 2> "$TMP_DIR/err" || rc=$?
if [ "$rc" -eq 2 ] && grep -q 'git の checkout ではない' "$TMP_DIR/err"; then
  note "git の checkout でない所は rc 2 で断る"
else
  bad "checkout でない所を断らない (rc=$rc)"
fi

exit $fail
