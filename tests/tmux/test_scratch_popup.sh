#!/usr/bin/env bash
# scripts/tmux_scratch_popup.sh (開閉トグル) の unit テスト。
# test_fork_scratch.sh の検査 B は静的 grep (bind 参照 / -A 不使用) のみで、
# 分岐の実挙動 (どの session 名で閉じるか / client 引数の形状 / 終了コード) は未カバーだった。
# stub tmux (PATH 先頭) で全呼び出しを傍受し、実サーバには触れない。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRATCH="$ROOT_DIR/scripts/tmux_scratch_popup.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  detach-client) exit "${STUB_DETACH_EXIT:-0}" ;;
esac
exit 0
EOS
chmod +x "$TMP_DIR/bin/tmux"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

printf '## scratch_popup: 開閉トグル\n'
reset_calls
run "$STUB_PATH" sh "$SCRATCH" myclient scratch
assert_called "detach-client -t myclient" "session=scratch → detach で閉じる (client 指定付き)"
assert_not_called "display-popup" "閉じ経路では popup を開かない"
[[ "$RC" -eq 0 ]] || { printf '✗ 閉じ経路の exit が %s (0 のはず)\n' "$RC"; exit 1; }
printf '✓ 閉じ経路は exit 0\n'
reset_calls
STUB_DETACH_EXIT=1 run "$STUB_PATH" sh "$SCRATCH" myclient scratch
[[ "$RC" -eq 0 ]] || { printf '✗ detach 失敗でも exit 0 のはず (RC=%s)\n' "$RC"; exit 1; }
printf '✓ detach-client 失敗でも exit 0 (強制 0 の契約)\n'
reset_calls
run "$STUB_PATH" sh "$SCRATCH" "" scratch
assert_called "detach-client" "client 空でも閉じ経路は動く"
grep -qF -- "-t" "$CALLS" && { printf '✗ client 空で -t トークンが出た\n'; cat "$CALLS"; exit 1; }
printf '✓ client 空 → -t トークン自体が消える (word-splitting 契約)\n'
reset_calls
run "$STUB_PATH" sh "$SCRATCH" myclient work
assert_called "display-popup" "scratch 以外 → popup を開く"
assert_called "-c myclient" "開き経路は client へ popup を表示"
assert_not_called "detach-client" "開き経路で detach しない"

printf '\nAll scratch popup tests passed successfully!\n'
