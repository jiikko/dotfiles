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
  *"--symbolic-full-name"*) [ "${STUB_NO_UPSTREAM:-0}" = 1 ] && exit 1; echo origin/master ;;
  *"rev-list --left-right --count"*) printf '0\t2\n' ;;
  *"rev-list --count"*) echo "${STUB_AHEAD:-20}" ;;
  *"log "*) echo "abc1234 fake commit" ;;
esac
exit 0
EOS

# stub fzf: 引数を記録し、STUB_FZF_KEY があれば --expect のキーとして 1 行目に出す
# (changes→log の ctrl-l ラウンドトリップ検証用)。TUI は開かない。
cat > "$TMP_DIR/bin/fzf" <<'EOS'
#!/bin/sh
echo "fzf $*" >> "$CALLS"
cat > /dev/null
printf '%s\n' "${STUB_FZF_KEY:-}"
exit 0
EOS

# stub gum: input はテスト制御のメッセージを返す (STUB_GUM_MSG)
cat > "$TMP_DIR/bin/gum" <<'EOS'
#!/bin/sh
echo "gum $*" >> "$CALLS"
[ "$1" = confirm ] && exit "${STUB_CONFIRM_RC:-0}"
printf '%s\n' "${STUB_GUM_MSG:-}"
EOS

# stub sleep: テストを待たせない
cat > "$TMP_DIR/bin/sleep" <<'EOS'
#!/bin/sh
exit 0
EOS

# stub glog: 実 TUI を開くとテストがハングするため、呼び出し記録だけして即終了する。
# 1 回目は STUB_GLOG_RC (既定 0)、2 回目以降は STUB_GLOG_RC2 (既定 0) を返す。CALLS 内の
# 既存 glog 行数で回数を判定するため reset_calls で自然にリセットされる。ラウンドトリップ
# (changes→log) テストで無限ループにならないよう 2 回目を閉じる (rc 0) にできる。
cat > "$TMP_DIR/bin/glog" <<'EOS'
#!/bin/sh
n=$(grep -c '^glog ' "$CALLS" 2>/dev/null || echo 0)
echo "glog $*" >> "$CALLS"
[ "$n" -ge 1 ] && exit "${STUB_GLOG_RC2:-0}"
exit "${STUB_GLOG_RC:-0}"
EOS
chmod +x "$TMP_DIR/bin/"*
cp "$TMP_DIR/bin/git" "$TMP_DIR/bin/fzf" "$TMP_DIR/bin/sleep" "$TMP_DIR/bin_nogum/"
chmod +x "$TMP_DIR/bin_nogum/"*

source "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
STUB="$TMP_DIR/bin:/usr/bin:/bin"
STUB_NOGUM="$TMP_DIR/bin_nogum:/usr/bin:/bin"
export STUB_GLOG_RC=0

echo "## main: fzf の配線"
reset_calls
STUB_GLOG_RC=20 run "$STUB" "$SCRIPT"
[[ "$RC" == 0 ]] || { echo "✗ main が rc=$RC"; cat "$CALLS"; exit 1; }
glog_line=$(grep -n '^glog' "$CALLS" | head -n1 | cut -d: -f1)
status_line=$(grep -n 'status --short' "$CALLS" | head -n1 | cut -d: -f1)
[[ -n "$glog_line" && -n "$status_line" && "$glog_line" -lt "$status_line" ]] || {
  echo "✗ popup の最初の画面で glog が呼ばれていない"; cat "$CALLS"; exit 1;
}
echo "✓ popup は最初に glog (log 画面) を開く"
assert_called "git rev-parse --is-inside-work-tree" "repo 判定を行う"
assert_called "--ansi" "fzf はインクリメンタル UI (--ansi) で起動される"
assert_called "--expect=ctrl-l" "fzf は C-l を log 遷移キーとして捕捉する"
assert_called "toggle {}" "Tab/Enter の stage toggle が配線されている"
assert_called "git add -A" "C-a の全 add が配線されている"   # bind 文字列内に含まれる
assert_called "commit" "C-o の commit が配線されている"
assert_called "ctrl-b:execute" "C-b の push が配線されている (C-p は fzf のカーソル移動と衝突するため不可)"
assert_called "preview {}" "diff preview が配線されている"

echo ""
echo "## main: changes(fzf) で C-l を押すと log(glog) に戻る"
reset_calls
# 1 回目 glog=20 で changes へ → fzf が ctrl-l を返す → log へ戻り 2 回目 glog=0 で閉じる
STUB_GLOG_RC=20 STUB_GLOG_RC2=0 STUB_FZF_KEY=ctrl-l run "$STUB" "$SCRIPT"
[[ "$RC" == 0 ]] || { echo "✗ ラウンドトリップで rc=$RC"; cat "$CALLS"; exit 1; }
glog_count=$(grep -c '^glog ' "$CALLS" || true)
[[ "$glog_count" -ge 2 ]] || { echo "✗ C-l で log に戻っていない (glog 呼び出し $glog_count 回)"; cat "$CALLS"; exit 1; }
echo "✓ changes の C-l で log(glog) に戻る (glog を再度開く)"
assert_called "fzf" "changes 画面では fzf を起動する"

echo ""
echo "## main: working tree が clean ならサマリ画面 (fzf を起動しない)"
reset_calls
STUB_CLEAN=1 STUB_GLOG_RC=20 run "$STUB" "$SCRIPT" < /dev/null
[[ "$RC" == 0 ]] || { echo "✗ clean 時に rc=$RC"; cat "$CALLS"; exit 1; }
assert_not_called "fzf" "clean 時は fzf を起動しない"
assert_not_called "git --no-pager log -5" "log 表示は静的 git log ではなく glog に委譲する"
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
echo "## push サブコマンド"
reset_calls
run "$STUB" "$SCRIPT" push < /dev/null
[[ "$RC" == 0 ]] || { echo "✗ push が rc=$RC"; cat "$CALLS"; exit 1; }
assert_called "git push" "push サブコマンドは git push を実行する"

reset_calls
STUB_AHEAD=0 run "$STUB" "$SCRIPT" push < /dev/null
assert_not_called "git push" "未 push コミットが無ければ push せずメッセージを出す"

reset_calls
STUB_NO_UPSTREAM=1 run "$STUB" "$SCRIPT" push < /dev/null
assert_not_called "git push" "upstream が無ければ push せずメッセージを出す"

reset_calls
run "$STUB" "$SCRIPT" push < /dev/null
assert_called "gum confirm --default=false" "push 前に gum confirm (デフォルト No) を挟む"

reset_calls
STUB_CONFIRM_RC=1 run "$STUB" "$SCRIPT" push < /dev/null
assert_not_called "git push" "confirm 拒否なら push しない"

reset_calls
run "$STUB_NOGUM" "$SCRIPT" push <<< "n"
assert_not_called "git push" "gum 無し: read fallback で n なら push しない"

reset_calls
run "$STUB_NOGUM" "$SCRIPT" push <<< "y"
assert_called "git push" "gum 無し: read fallback で y なら push する"

echo ""
echo "## commit: gum 未導入の degrade (read fallback)"
reset_calls
STUB_HAS_STAGED=1 run "$STUB_NOGUM" "$SCRIPT" commit <<< "chore: fallback"
assert_called "git commit -m chore: fallback" "gum 無しでは read でメッセージを受ける"

echo ""
echo "[test-git-popup] all assertions passed"
