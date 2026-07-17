#!/usr/bin/env bash
# scripts/tmux_jump_last_touched.sh (prefix+u) の unit テスト。
#
# 選択ロジックは awk 1 本に集約されていて壊れても静かに誤ジャンプ/無反応になるだけなので、
# stub tmux (PATH 先頭) で list-windows の応答を注入し select-window の呼び出しを固定する。
# 実 tmux サーバには触れない。
#
# 固定する不変条件:
#   - @last-touched 最大の非 active window が選ばれる (現在 window は最大値でも除外)
#   - 未スタンプ window は候補外
#   - 候補なしは select-window を呼ばず exit 0 (非 0 だと run-shell が "returned 1" を
#     status line に出す — スクリプト内コメントの一次情報を挙動で pin)
#   - 同値タイブレークは列挙順で先勝ち (`>` を `>=` に変えると反転する)
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_jump_last_touched.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

[[ -x "$SCRIPT" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"; exit 1; }

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  list-windows)
    [ -n "${STUB_LW_EXIT:-}" ] && exit "$STUB_LW_EXIT"
    printf '%b' "$STUB_WINDOWS" ;;
  select-window) exit 0 ;;
esac
exit 0
EOS
chmod +x "$TMP_DIR/bin/tmux"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

# 行形式は list-windows -F '#{window_id} #{@last-touched} #{window_active}' の実出力に合わせる
# (未スタンプは空フィールド = 連続スペース)
reset_calls
STUB_WINDOWS='@0 100 0\n@1 300 0\n@2 200 1\n' run "$STUB_PATH" "$SCRIPT"
assert_called "select-window -t @1" "非 active の中でスタンプ最大の window へジャンプ"

reset_calls
STUB_WINDOWS='@0 100 0\n@1 999 1\n' run "$STUB_PATH" "$SCRIPT"
assert_called "select-window -t @0" "現在 window はスタンプ最大でも除外される"

reset_calls
STUB_WINDOWS='@0  0\n@1 150 0\n' run "$STUB_PATH" "$SCRIPT"
assert_called "select-window -t @1" "未スタンプ window は候補外"

reset_calls
STUB_WINDOWS='@0 100 1\n' run "$STUB_PATH" "$SCRIPT"
assert_not_called "select-window" "候補なし (唯一の window が active) → ジャンプしない"
[[ "$RC" -eq 0 ]] || { printf '✗ 候補なしで exit %s (0 のはず。run-shell の returned 1 表示回帰)\n' "$RC"; exit 1; }
printf '✓ 候補なしでも exit 0 (status line へのエラー表示なし)\n'

reset_calls
STUB_WINDOWS='@0  0\n@1  0\n' run "$STUB_PATH" "$SCRIPT"
assert_not_called "select-window" "全 window 未スタンプ → ジャンプしない"
[[ "$RC" -eq 0 ]] || { printf '✗ 全未スタンプで exit %s (0 のはず)\n' "$RC"; exit 1; }
printf '✓ 全未スタンプでも exit 0\n'

reset_calls
STUB_WINDOWS='@0 500 0\n@1 500 0\n' run "$STUB_PATH" "$SCRIPT"
assert_called "select-window -t @0" "同値タイブレークは列挙順で先勝ち (> の厳密比較)"

reset_calls
STUB_LW_EXIT=1 STUB_WINDOWS='' run "$STUB_PATH" "$SCRIPT"
assert_not_called "select-window" "list-windows 失敗 → ジャンプしない"
[[ "$RC" -eq 0 ]] || { printf '✗ list-windows 失敗で exit %s (pipefail 無しの現契約では 0)\n' "$RC"; exit 1; }
printf '✓ list-windows 失敗は exit 0 に握りつぶす (現契約の pin)\n'

printf '\nAll jump-last-touched tests passed successfully!\n'
