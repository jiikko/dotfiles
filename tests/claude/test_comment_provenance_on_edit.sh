#!/usr/bin/env bash
# _claude/hooks/comment-provenance-on-edit.sh (PostToolUse(Write / Edit): 書いたファイルのコメントの経緯の語を知らせる) の unit テスト。
#
# なぜ: 知らせが出なくなる退行は無音 (書き込みはそのまま通り、make test まで気づかない)。他の repo の同名スクリプトを
# 実行してしまう退行も、出力からは見えない。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK_SRC="$ROOT_DIR/_claude/hooks/comment-provenance-on-edit.sh"
CHECK_SRC="$ROOT_DIR/scripts/check_comment_provenance.sh"
# 本番の配線と同じ上限 (timeout に殺されると無出力 = 知らせが消える)
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

# 配線: settings.json の PostToolUse の Write|Edit から呼ばれていること
jq -e '.hooks.PostToolUse[] | select(.matcher == "Write|Edit") | .hooks[] | select(.command == "~/dotfiles/_claude/hooks/comment-provenance-on-edit.sh")' \
  "$ROOT_DIR/_claude/settings.json" >/dev/null || { echo "✗ settings.json の PostToolUse(Write|Edit) に hook が無い (配線が外れている)"; exit 1; }

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT

# hook は自分の置き場所の repo を「dotfiles」とみなすので、hook と検査を写した fixture の repo を dotfiles の代わりにする
D="$TMP_ROOT/dot"
mkdir -p "$D/_claude/hooks" "$D/scripts" "$D/src/x"
cp "$HOOK_SRC" "$D/_claude/hooks/"; cp "$CHECK_SRC" "$D/scripts/"
printf '# 一覧\n' > "$D/scripts/comment_provenance_allowlist.txt"
printf 'package x\n// issue 802 で直した\n' > "$D/src/x/old.go"
( cd "$D" && git init -q . && git add -A && git -c user.name=t -c user.email=t@t commit -qm init &&
  CHECK_COMMENT_PROVENANCE_MIN_FILES=1 scripts/check_comment_provenance.sh --update --allow-increase >/dev/null &&
  git add -A && git -c user.name=t -c user.email=t@t commit -qm allow ) >/dev/null 2>&1 \
  || { echo "✗ fixture の repo を作れない"; exit 1; }
HOOK="$D/_claude/hooks/comment-provenance-on-edit.sh"
git -C "$D" worktree add -q --detach "$TMP_ROOT/wt" >/dev/null 2>&1 || { echo "✗ fixture の worktree を作れない"; exit 1; }

# 別の repo: 同名の検査を置き、実行されたら印を残す
E="$TMP_ROOT/other"
mkdir -p "$E/scripts"
printf '#!/bin/sh\ntouch "%s/ran"\necho "✗ fake"\nexit 1\n' "$TMP_ROOT" > "$E/scripts/check_comment_provenance.sh"
chmod +x "$E/scripts/check_comment_provenance.sh"
( cd "$E" && git init -q . ) >/dev/null 2>&1

# 🚨 rc と stderr を捨てない: 知らせだけの hook は、どの入力でも rc=0・stderr は空でなければならない
run() { # $1=file_path → stdout
  local rc
  jq -n --arg f "$1" '{tool_name: "Write", session_id: "s", cwd: "/", tool_input: {file_path: $f}}' |
    "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  rc=$?
  if [ "$rc" -ne 0 ] || [ -s "$TMP_ROOT/err" ]; then
    printf 'HOOK-FAILED rc=%s stderr=%s\n' "$rc" "$(head -c 200 "$TMP_ROOT/err")"
    return 0
  fi
  cat "$TMP_ROOT/out"
}
expect_notice() { # $1=説明 $2=stdout $3=知らせに含むべき行
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if ! printf '%s' "$2" | jq -e --arg l "$3" '.hookSpecificOutput.hookEventName == "PostToolUse" and (.hookSpecificOutput.additionalContext | contains($l))' >/dev/null 2>&1; then
    echo "✗ 知らせが出ない / 行が無い / JSON が壊れている: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_silent() { # $1=説明 $2=stdout
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if [ -n "$2" ]; then echo "✗ 黙るはずが出した: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

printf 'package x\n\n// retro 1007 の形\nfunc f() {}\n' > "$D/src/x/new.go"
expect_notice 'add する前の新しいファイルの番号を知らせる' "$(run "$D/src/x/new.go")" 'src/x/new.go:3:// retro 1007 の形'
printf '#!/bin/sh\n# 以前は別の形\n' > "$D/top.sh"
expect_notice 'repo root の直下のファイルも知らせる' "$(run "$D/top.sh")" 'top.sh:2:# 以前は別の形'
printf 'package x\n// 敵対的レビューで見つけた\n' > "$TMP_ROOT/wt/src/x/w.go"
expect_notice 'worktree の中のファイルも知らせる' "$(run "$TMP_ROOT/wt/src/x/w.go")" 'src/x/w.go:2:'
expect_silent '一覧の件数までの既存の違反は黙る' "$(run "$D/src/x/old.go")"
printf 'package x\n// 空のときは 0 を返す (呼び出し側が分岐しない)\n' > "$D/src/x/clean.go"
expect_silent '違反の無いファイルは黙る' "$(run "$D/src/x/clean.go")"
printf '# issue 802\n' > "$D/note.md"
expect_silent '検査の対象外 (Markdown) は黙る' "$(run "$D/note.md")"
expect_silent '無いファイルは黙る' "$(run "$D/src/x/nope.go")"
expect_silent '相対パスは黙る' "$(run "src/x/new.go")"
mkdir -p "$TMP_ROOT/home"; ln -s "$D/src/x/new.go" "$TMP_ROOT/home/link.go"
expect_notice 'repo の外の symlink 越しに書いたファイルも、実体で知らせる' "$(run "$TMP_ROOT/home/link.go")" 'src/x/new.go:3:'
printf 'tmp/\n' > "$D/.gitignore"; mkdir -p "$D/tmp"; printf '#!/bin/sh\n# issue 802\n' > "$D/tmp/s.sh"
expect_silent 'gitignore されたファイル (CI は見ない) は黙る' "$(run "$D/tmp/s.sh")"
printf '#!/bin/sh\n# 普通のコメント\n' > "$D/src/x/ta"$'\t'"b.sh"
expect_silent 'path にタブがある (判定不能) ときは、経緯の語の知らせを出さない' "$(run "$D/src/x/ta"$'\t'"b.sh")"
printf 'package x\n// issue 802\n' > "$E/a.go"
expect_silent '別の repo は黙る' "$(run "$E/a.go")"
if [ -e "$TMP_ROOT/ran" ]; then echo "✗ 別の repo の同名スクリプトを実行した"; fail=$((fail+1)); else echo "✓ 別の repo の同名スクリプトを実行しない"; ok=$((ok+1)); fi

[ "$fail" -eq 0 ] || { echo "✗ $fail 件失敗 ($ok 件成功)"; exit 1; }
echo "✓ comment-provenance-on-edit: $ok 件"
