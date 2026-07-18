#!/usr/bin/env bash
# scripts/tmux_git_popup.sh の unit テスト (PATH stub 方式。実 git / fzf / gum / 実 repo に
# 一切触れない)。固定する不変条件:
#   - toggle の判定: worktree 側に変更があれば add、staged のみなら restore --staged
#     (「確認した行」と「操作するパス」の一致: rename は新パス側・quote 付きパスは剥ぐ)
#   - commit の fail-safe: staged が無ければ commit しない・メッセージ空なら commit しない
#   - repo 外では fzf を起動せず非 0 で終わる
#   - fzf の配線: toggle/commit/全add/diff の bind とインクリメンタル UI (--ansi/preview) が
#     揃っていること (壊れても平常時に気づきにくい配線の回帰ガード)
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_git_popup.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

[[ -x "$SCRIPT" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"; exit 1; }

CALLS="$TMP_DIR/calls.log"
export CALLS
: > "$CALLS"

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/bin_nogum"

# stub git: 呼び出しを記録し、テストごとの応答は環境変数で制御する
#   STUB_IN_REPO=0    → rev-parse --is-inside-work-tree が失敗 (repo 外)
#   STUB_HAS_STAGED=1 → diff --cached --quiet が非 0 (staged あり)
cat > "$TMP_DIR/bin/git" <<'EOS'
#!/bin/sh
echo "git $*" >> "$CALLS"
case "$*" in
  *"--is-inside-work-tree"*) [ "${STUB_IN_REPO:-1}" = 1 ] && exit 0 || exit 1 ;;
  *"rev-parse --abbrev-ref HEAD"*) echo master ;;
  *"diff --cached --quiet"*) [ "${STUB_HAS_STAGED:-0}" = 1 ] && exit 1 || exit 0 ;;
  *"status --short"*) [ "${STUB_CLEAN:-0}" = 1 ] || printf '?? new.txt\n M mod.txt\n' ;;
  *"--symbolic-full-name"*) echo origin/master ;;
  *"rev-list --left-right --count"*) printf '0\t2\n' ;;
  *"rev-list --count"*) echo 20 ;;
  *"log "*) echo "abc1234 fake commit" ;;
esac
exit 0
EOS

# stub fzf: 引数を記録するだけ (TUI は開かない)
cat > "$TMP_DIR/bin/fzf" <<'EOS'
#!/bin/sh
echo "fzf $*" >> "$CALLS"
cat > /dev/null
exit 0
EOS

# stub gum: input はテスト制御のメッセージを返す (STUB_GUM_MSG)
cat > "$TMP_DIR/bin/gum" <<'EOS'
#!/bin/sh
echo "gum $*" >> "$CALLS"
printf '%s\n' "${STUB_GUM_MSG:-}"
EOS

# stub sleep: テストを待たせない
cat > "$TMP_DIR/bin/sleep" <<'EOS'
#!/bin/sh
exit 0
EOS
chmod +x "$TMP_DIR/bin/"*
cp "$TMP_DIR/bin/git" "$TMP_DIR/bin/fzf" "$TMP_DIR/bin/sleep" "$TMP_DIR/bin_nogum/"
chmod +x "$TMP_DIR/bin_nogum/"*

source "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
STUB="$TMP_DIR/bin:/usr/bin:/bin"
STUB_NOGUM="$TMP_DIR/bin_nogum:/usr/bin:/bin"

echo "## main: fzf の配線"
reset_calls
run "$STUB" "$SCRIPT"
[[ "$RC" == 0 ]] || { echo "✗ main が rc=$RC"; cat "$CALLS"; exit 1; }
assert_called "git rev-parse --is-inside-work-tree" "repo 判定を行う"
assert_called "--ansi" "fzf はインクリメンタル UI (--ansi) で起動される"
assert_called "toggle {}" "Tab/Enter の stage toggle が配線されている"
assert_called "git add -A" "C-a の全 add が配線されている"   # bind 文字列内に含まれる
assert_called "commit" "C-o の commit が配線されている"
assert_called "preview {}" "diff preview が配線されている"

echo ""
echo "## main: working tree が clean ならサマリ画面 (fzf を起動しない)"
reset_calls
STUB_CLEAN=1 run "$STUB" "$SCRIPT" < /dev/null
[[ "$RC" == 0 ]] || { echo "✗ clean 時に rc=$RC"; cat "$CALLS"; exit 1; }
assert_not_called "fzf" "clean 時は fzf を起動しない"
assert_called "git --no-pager log -5" "clean 時は直近コミットのサマリを出す (pager 起動で画面が消えないよう --no-pager 必須)"
assert_called "rev-list --left-right --count" "upstream との ahead/behind を判定する"
assert_called "rev-list --count --max-count=20" "未 push ありならドットグラフ用に直近 commit 数を取る"
assert_not_called "gum" "clean 画面は素の ANSI で描く (gum 非依存)"

echo ""
echo "## main: repo 外では起動しない"
reset_calls
STUB_IN_REPO=0 run "$STUB" "$SCRIPT"
[[ "$RC" != 0 ]] || { echo "✗ repo 外で rc=0"; exit 1; }
echo "✓ repo 外は非 0 で終了"
assert_not_called "fzf" "repo 外では fzf を起動しない"

echo ""
echo "## toggle: stage/unstage の判定とパス解決"
reset_calls
run "$STUB" "$SCRIPT" toggle "?? new.txt"
assert_called "git add -- new.txt" "untracked → add"

reset_calls
run "$STUB" "$SCRIPT" toggle " M mod.txt"
assert_called "git add -- mod.txt" "worktree 変更 → add"

reset_calls
run "$STUB" "$SCRIPT" toggle "MM both.txt"
assert_called "git add -- both.txt" "staged+worktree 混在 → 残りを add"

reset_calls
run "$STUB" "$SCRIPT" toggle "M  staged.txt"
assert_called "git restore --staged -- staged.txt" "staged のみ → unstage"

reset_calls
run "$STUB" "$SCRIPT" toggle "R  old.txt -> new-name.txt"
assert_called "git restore --staged -- new-name.txt" "rename は新パス側を操作"

reset_calls
run "$STUB" "$SCRIPT" toggle '?? "a b.txt"'
assert_called 'git add -- a b.txt' "quote 付きパスは剥いで操作"

reset_calls
run "$STUB" "$SCRIPT" toggle ""
assert_not_called "git add" "空行 (リスト空) では何もしない"

echo ""
echo "## commit: fail-safe"
reset_calls
STUB_HAS_STAGED=0 run "$STUB" "$SCRIPT" commit
assert_not_called "git commit" "staged が無ければ commit しない"

reset_calls
STUB_HAS_STAGED=1 STUB_GUM_MSG="fix: テスト" run "$STUB" "$SCRIPT" commit
assert_called "git commit -m fix: テスト" "gum のメッセージで commit する"

reset_calls
STUB_HAS_STAGED=1 STUB_GUM_MSG="" run "$STUB" "$SCRIPT" commit
assert_not_called "git commit" "メッセージ空なら commit しない"

echo ""
echo "## commit: gum 未導入の degrade (read fallback)"
reset_calls
STUB_HAS_STAGED=1 run "$STUB_NOGUM" "$SCRIPT" commit <<< "chore: fallback"
assert_called "git commit -m chore: fallback" "gum 無しでは read でメッセージを受ける"

echo ""
echo "[test-git-popup] all assertions passed"
