#!/usr/bin/env bash
# bin/skill-tmpdir が <本体 checkout>/tmp/<名前>/<session-id>/ を返すことを固定する (issue 774)。
#
# 守るもの: worktree の中から呼んでも本体 checkout の tmp/ を返す / session-id が無い・不正なら nosession- に倒す /
# 名前に / や .. を通さない (tmp/ の外へ出さない) / repo の外では cwd を基準にする。
set -euo pipefail
unset CDPATH
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SUT="$ROOT_DIR/bin/skill-tmpdir"
fails=0
passes=0
ok()   { printf '✓ %s\n' "$1"; passes=$((passes + 1)); }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }
# expect <実際> <期待> <説明>
expect() { if [ "$1" = "$2" ]; then ok "$3"; else fail "$3: $1"; fi; }

WORK="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/test-skill-tmpdir.XXXXXX")" && pwd -P)"
trap 'rm -rf "$WORK"' EXIT

REPO="$WORK/repo"
git init -q "$REPO"
git -C "$REPO" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m init
git -C "$REPO" worktree add -q --detach "$WORK/wt" HEAD

# 1. 本体 checkout から
got=$(cd "$REPO/." && CLAUDE_CODE_SESSION_ID=abc-123 "$SUT" codex-drive)
expect "$got" "$REPO/tmp/codex-drive/abc-123" "本体 checkout の tmp/<名前>/<sid>/ を返す"
if [ -d "$got" ]; then ok "dir を作る"; else fail "dir を作っていない: $got"; fi

if [ -f "$REPO/tmp/codex-drive/.skill-tmpdir" ] && [ ! -L "$REPO/tmp/codex-drive/.skill-tmpdir" ]; then ok "make clean-tmp 用の印を置く"; else fail "印が無い"; fi

mkdir -p "$REPO/tmp/dang"
ln -s "$WORK/nowhere-marker" "$REPO/tmp/dang/.skill-tmpdir"
(cd "$REPO" && CLAUDE_CODE_SESSION_ID=d1 "$SUT" dang) >/dev/null
if [ ! -e "$WORK/nowhere-marker" ]; then ok "先の無い symlink の印を辿って外に作らない"; else fail "印の symlink の先に作った"; fi

# 1b. 既にある古い session の dir を返すときは今の mtime にする (再開した session の dir を make clean-tmp に消させない)
mkdir -p "$REPO/tmp/codex-review/old-sid" && touch -t 202501010000 "$REPO/tmp/codex-review/old-sid"
got=$(cd "$REPO" && CLAUDE_CODE_SESSION_ID=old-sid "$SUT" codex-review)
if [ -n "$(find "$got" -prune -mtime -1 -print)" ]; then ok "既にある古い session の dir を今の mtime にする"; else fail "古い mtime のまま: $got"; fi

# 2. worktree の中から呼んでも本体 checkout を返す
got=$(cd "$WORK/wt" && CLAUDE_CODE_SESSION_ID=abc-123 "$SUT" codex-drive)
expect "$got" "$REPO/tmp/codex-drive/abc-123" "worktree からでも本体 checkout の tmp/ を返す"
if [ ! -e "$WORK/wt/tmp" ]; then ok "worktree 側に tmp/ を作らない"; else fail "worktree に tmp/ ができた"; fi

# 2b. submodule の worktree / --separate-git-dir の worktree (common-dir の末尾が .git でない)
SUBSRC="$WORK/subsrc"
git init -q "$SUBSRC"
git -C "$SUBSRC" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m init
git -C "$REPO" -c protocol.file.allow=always submodule add -q "$SUBSRC" sm >/dev/null 2>&1
git -C "$REPO/sm" worktree add -q --detach "$WORK/sm-wt" HEAD
got=$(cd "$WORK/sm-wt" && CLAUDE_CODE_SESSION_ID=s2 "$SUT" x)
expect "$got" "$REPO/sm/tmp/x/s2" "submodule の worktree からは submodule の本体 checkout を返す"
git init -q --separate-git-dir "$WORK/sep.gitdir" "$WORK/sep"
git -C "$WORK/sep" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m init
git -C "$WORK/sep" worktree add -q --detach "$WORK/sep-wt" HEAD
# 本体の場所を git が持たないので、worktree 自身に倒して警告する (黙って倒さない)
got=$(cd "$WORK/sep-wt" && CLAUDE_CODE_SESSION_ID=s3 "$SUT" x 2>"$WORK/sep.err")
expect "$got" "$WORK/sep-wt/tmp/x/s3" "separate-git-dir の worktree は cwd の checkout に倒す"
if grep -q "本体 checkout を特定できない" "$WORK/sep.err"; then ok "その時は警告を出す"; else fail "警告が出ない"; fi

got=$(cd "$WORK/sep" && CLAUDE_CODE_SESSION_ID=s3 "$SUT" x 2>"$WORK/sep-main.err")
expect "$got" "$WORK/sep/tmp/x/s3" "separate-git-dir の本体 checkout ではその checkout を返す"
if [ ! -s "$WORK/sep-main.err" ]; then ok "本体にいるときは警告しない"; else fail "本体で警告した: $(cat "$WORK/sep-main.err")"; fi

# 2c. 継承した GIT_DIR は無視して cwd で決める
mkdir "$WORK/other"
git init -q "$WORK/other"
got=$(cd "$WORK/other" && GIT_DIR="$REPO/.git" CLAUDE_CODE_SESSION_ID=s4 "$SUT" x)
expect "$got" "$WORK/other/tmp/x/s4" "継承した GIT_DIR に引きずられない"

# 3. session-id が無い・不正
for bad in "" "../x" "a/b" ".hidden"; do
	got=$(cd "$REPO" && CLAUDE_CODE_SESSION_ID="$bad" "$SUT" codex-drive)
	case $got in
	"$REPO/tmp/codex-drive/nosession-"*) ok "session-id '$bad' は nosession- に倒す" ;;
	*) fail "session-id '$bad': $got" ;;
	esac
done

# 4. 名前の検査 (tmp/ の外へ出さない)
for bad in "" ".." "../etc" "a/b" ".x"; do
	if (cd "$REPO" && "$SUT" "$bad") >/dev/null 2>&1; then fail "名前 '$bad' を通した"; else ok "名前 '$bad' を拒否する"; fi
done

# 5. repo の外では cwd
mkdir "$WORK/plain"
got=$(cd "$WORK/plain" && CLAUDE_CODE_SESSION_ID=s1 GIT_CEILING_DIRECTORIES="$WORK" "$SUT" perf-analysis)
expect "$got" "$WORK/plain/tmp/perf-analysis/s1" "repo の外では cwd の tmp/ を返す"

printf '%d passed, %d failed\n' "$passes" "$fails"
[ "$fails" -eq 0 ]
