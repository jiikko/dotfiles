#!/usr/bin/env bash
# scripts/tmux_fzf_jump.sh / scripts/tmux_fzf_pane_move.sh の機能テスト (stub 方式)。
#
# 共有ロジック (候補構築・除外・選択キー) は tests/tmux/test_window_picker.sh が
# 関数単位で厚くテスト済み。ここで固定するのは「実スクリプトの末端」= picker の選択結果を
# 受けて実際に発行する tmux コマンド:
#   - jump      : switch-client -t <window_id> / fzf キャンセルで何も切り替えない
#   - pane_move : get = join-pane -s <選択> -t <自 pane> / give = 逆向き /
#                 引数なしは usage で非 0 / キャンセルで join しない
# (従来は test_fork_scratch.sh の静的 grep のみで、join-pane の向きが逆転しても
#  通ってしまった。docs/feedback-nvim-tmux-2026-07-29.md の未対応候補の解消)
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
FZF_IN="$TMP_DIR/fzf_in.log"; export FZF_IN
mkdir -p "$TMP_DIR/bin"

# stub tmux: display は format で分岐 (picker の現在地解決と pane_move の自 pane 解決が
# 同じ display サブコマンドで来るため)。join-pane / switch-client は記録のみ。
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  display)
    case "$*" in
      *pane_id*)      printf '%s\n' "${STUB_ME_PANE:-%9}" ;;
      *session_name*) printf '%s\n' "${STUB_CURRENT:-main:1}" ;;
    esac ;;
  list-windows) printf '%b' "${STUB_WINDOWS:-}" ;;
  join-pane|switch-client) : ;;
  *) exit 1 ;;  # 想定外のサブコマンド = 契約違反として失敗させる
esac
EOS
cat > "$TMP_DIR/bin/fzf" <<'EOS'
#!/bin/sh
cat > "$FZF_IN"
echo "fzf $*" >> "$CALLS"
case "${STUB_FZF_MODE:-first}" in
  first)  head -1 "$FZF_IN" ;;
  cancel) exit 130 ;;
esac
EOS
cat > "$TMP_DIR/bin/date" <<'EOS'
#!/bin/sh
[ -n "${STUB_NOW:-}" ] && { printf '%s\n' "$STUB_NOW"; exit 0; }
exec /bin/date "$@"
EOS
chmod +x "$TMP_DIR/bin/tmux" "$TMP_DIR/bin/fzf" "$TMP_DIR/bin/date"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

export STUB_NOW=1000000
# 形式: window_activity \t window_id \t session:index \t 表示名 (先頭候補 = @10)。
# 現在地 main:1 (@12) を必ず含める: 含めないと pane_move の exclude-current assert が
# 「元データに無いから出ない」だけの空振りになる (セルフレビューで検出した罠)。
# activity は最低にして先頭候補 (=stub fzf の選択) が @10 のままになるようにする
ROWS='999990\t@10\tmain:2\tvim\n999940\t@11\tsub:1\tserver\n999900\t@12\tmain:1\tzsh\n'
export STUB_WINDOWS="$ROWS" STUB_CURRENT="main:1" STUB_ME_PANE="%9"

printf '## jump (bind f)\n'
reset_calls; rm -f "$FZF_IN"
run "$STUB_PATH" "$ROOT_DIR/scripts/tmux_fzf_jump.sh"
[ "$RC" -eq 0 ] || { printf '✗ jump: 正常系で rc=%s\n' "$RC"; exit 1; }
assert_called "tmux switch-client -t @10" "jump: 選択した window_id へ switch-client する"
# 陽性対照: jump は現在地を候補に含める (これが通ることで下の pane_move 側の
# 「main:1 が無い」assert が exclude-current の効果を見ていると言える)
grep -q 'main:1' "$FZF_IN" || { printf '✗ jump: 現在 window が候補に無い (陽性対照の破れ)\n'; exit 1; }
printf '✓ jump: 現在 window も候補に含まれる (exclude なしの陽性対照)\n'

reset_calls; rm -f "$FZF_IN"
STUB_FZF_MODE=cancel run "$STUB_PATH" "$ROOT_DIR/scripts/tmux_fzf_jump.sh"
[ "$RC" -eq 0 ] || { printf '✗ jump: キャンセルで rc=%s (popup にエラーを残さない契約)\n' "$RC"; exit 1; }
assert_not_called "switch-client" "jump: fzf キャンセルでは switch-client しない"

printf '\n## pane_move (bind g/G)\n'
reset_calls; rm -f "$FZF_IN"
run "$STUB_PATH" "$ROOT_DIR/scripts/tmux_fzf_pane_move.sh"
[ "$RC" -ne 0 ] || { printf '✗ pane_move: 引数なしが成功した (usage で非 0 の契約)\n'; exit 1; }
assert_not_called "join-pane" "pane_move: 引数なしは join-pane に到達しない"

reset_calls; rm -f "$FZF_IN"
run "$STUB_PATH" "$ROOT_DIR/scripts/tmux_fzf_pane_move.sh" get
[ "$RC" -eq 0 ] || { printf '✗ pane_move get: rc=%s\n' "$RC"; exit 1; }
assert_called "tmux join-pane -s @10 -t %9" "get: 選択 window の pane を自 pane へ合流 (-s 選択 / -t 自分)"

reset_calls; rm -f "$FZF_IN"
run "$STUB_PATH" "$ROOT_DIR/scripts/tmux_fzf_pane_move.sh" give
[ "$RC" -eq 0 ] || { printf '✗ pane_move give: rc=%s\n' "$RC"; exit 1; }
assert_called "tmux join-pane -s %9 -t @10" "give: 自 pane を選択 window へ送る (-s 自分 / -t 選択)"

# exclude-current が末端から picker へ渡っていること (自 window は join 不可)。
# 候補構築の詳細は test_window_picker.sh 側の責務なので、ここは「現在地が候補に無い」だけ見る
grep -q 'main:1' "$FZF_IN" && { printf '✗ pane_move: 現在 window が候補に混入 (exclude-current 欠落)\n'; exit 1; }
printf '✓ pane_move: 現在 window は候補から除外されている (exclude-current)\n'

reset_calls; rm -f "$FZF_IN"
STUB_FZF_MODE=cancel run "$STUB_PATH" "$ROOT_DIR/scripts/tmux_fzf_pane_move.sh" give
[ "$RC" -eq 0 ] || { printf '✗ pane_move: キャンセルで rc=%s\n' "$RC"; exit 1; }
assert_not_called "join-pane" "pane_move: fzf キャンセルでは join-pane しない"

printf '\nAll fzf jump/pane-move tests passed successfully!\n'
