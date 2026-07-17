#!/usr/bin/env bash
# scripts/tmux_{kill,restore,reload}_confirm.sh の unit テスト。
#
# 確認 popup 群は「壊れても平常時は誰も気づかず、気づくのは誤爆事故の瞬間」という
# 回帰ガード価値が最も高い部類なのにテストが無かった (2026-07-17 の C-t C-r 復元暴発を機に
# ガード群が追加された経緯)。ここで判定ロジックと fail-safe 構造を stub で固定する:
#   - PATH 先頭の stub tmux / stub gum が全外部呼び出しを傍受・記録する。実 tmux サーバには
#     一切触れない (bare tmux が継承 $TMUX 経由で実サーバへ届く事故の予防に unset TMUX も行う)
#   - tty 必須の popup 開閉体感・gum の TUI 描画は対象外 (test_fork_scratch.sh と同方針)
#
# 固定する不変条件:
#   - fail-safe: gum の拒否/未導入 (exit 127) で破壊的コマンドが 1 つも飛ばないこと
#   - kill 対象の固定: 冒頭で解決した pane/window id がそのまま kill の -t に渡ること
#     (「確認した相手と kill する相手の一致」)
#   - reload の uptime ゲート: 発火窓の内外で popup / 即 source-file が正しく分岐すること
#     (@continuum-restore-max-delay 未設定時の fallback 60 と、境界 = 窓内扱い も pin)
#   - restore の degrade: @resurrect-restore-script-path 空/不在で復元経路に入らないこと
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
KILL="$ROOT_DIR/scripts/tmux_kill_confirm.sh"
RESTORE="$ROOT_DIR/scripts/tmux_restore_confirm.sh"
RELOAD="$ROOT_DIR/scripts/tmux_reload_confirm.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

for s in "$KILL" "$RESTORE" "$RELOAD"; do
  [[ -x "$s" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$s"; exit 1; }
done

CALLS="$TMP_DIR/calls.log"
: > "$CALLS"

# stub tmux: 呼び出しを記録し、confirm スクリプト群が使う照会にだけ環境変数で応える
mkdir -p "$TMP_DIR/bin" "$TMP_DIR/bin_nogum"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$*" in
  "display-message -p #{start_time}")            printf '%s\n' "${STUB_START_TIME:-0}" ;;
  "show -gqv @continuum-restore-max-delay")      printf '%s\n' "${STUB_MAX_DELAY:-}" ;;
  "show -gqv @resurrect-restore-script-path")    printf '%s\n' "${STUB_RESTORE_PATH:-}" ;;
  "display-message -p #{window_id}")             printf '@7\n' ;;
  "display-message -p -t @7 #{window_name}")     printf 'work\n' ;;
  "display-message -p -t @7 #{window_panes}")    printf '3\n' ;;
  "display-message -p #{pane_id}")               printf '%%5\n' ;;
  "display-message -p -t %5 #{pane_current_command}") printf 'zsh\n' ;;
esac
exit 0
EOS
chmod +x "$TMP_DIR/bin/tmux"
cp "$TMP_DIR/bin/tmux" "$TMP_DIR/bin_nogum/tmux"

# stub gum: STUB_GUM_EXIT で承認 (0) / 拒否 (1) を切替。bin_nogum には置かない
# (= 未導入マシンの exit 127 経路の再現)
cat > "$TMP_DIR/bin/gum" <<'EOS'
#!/bin/sh
echo "gum $*" >> "$CALLS"
exit "${STUB_GUM_EXIT:-1}"
EOS
chmod +x "$TMP_DIR/bin/gum"

# stub date: STUB_NOW 指定時のみ固定 epoch を返す (境界ケースを実時計とのレースなしで
# 決定論化する)。未指定なら実 date へ委譲
cat > "$TMP_DIR/bin/date" <<'EOS'
#!/bin/sh
[ -n "${STUB_NOW:-}" ] && { printf '%s\n' "$STUB_NOW"; exit 0; }
exec /bin/date "$@"
EOS
chmod +x "$TMP_DIR/bin/date"
cp "$TMP_DIR/bin/date" "$TMP_DIR/bin_nogum/date"

export CALLS
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
NOGUM_PATH="$TMP_DIR/bin_nogum:/usr/bin:/bin"

RUN_OUT="$TMP_DIR/out.log"; RUN_ERR="$TMP_DIR/err.log"
# shellcheck source=tests/tmux/lib/stub_assert_helper.sh
. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

printf '## kill_confirm: 対象の固定と fail-safe\n'
reset_calls
STUB_GUM_EXIT=0 run "$STUB_PATH" "$KILL" window
assert_called "tmux kill-window -t @7" "window 承認 → 冒頭で固定した window_id へ kill-window"
reset_calls
STUB_GUM_EXIT=1 run "$STUB_PATH" "$KILL" window
assert_not_called "kill-window" "window 拒否 → kill されない"
reset_calls
run "$NOGUM_PATH" "$KILL" window
[[ "$RC" -ne 0 ]] || { printf '✗ gum 未導入なのに exit 0\n'; exit 1; }
assert_not_called "kill-window" "gum 未導入 (exit 127) → kill されない (fail-safe の && 短絡)"
reset_calls
STUB_GUM_EXIT=0 run "$STUB_PATH" "$KILL" pane
assert_called "tmux kill-pane -t %5" "pane 承認 → 固定した pane_id へ kill-pane"
reset_calls
STUB_GUM_EXIT=1 run "$STUB_PATH" "$KILL" pane
assert_not_called "kill-pane" "pane 拒否 → kill されない"
reset_calls
STUB_GUM_EXIT=0 run "$STUB_PATH" "$KILL" others
assert_called "tmux kill-pane -a -t %5" "others 承認 → 自分以外 (-a) を kill"
reset_calls
run "$STUB_PATH" "$KILL" bogus
if [[ "$RC" -ne 1 ]] || ! grep -q 'usage:' "$TMP_DIR/err.log"; then
  printf '✗ 不正 scope が usage + exit 1 にならない (RC=%s)\n' "$RC"; exit 1
fi
printf '✓ 不正 scope → usage + exit 1\n'

printf '\n## restore_confirm: degrade と復元経路\n'
MARKER="$TMP_DIR/marker_hit"
cat > "$TMP_DIR/fake_restore.sh" <<EOS
#!/bin/sh
touch "$MARKER"
EOS
chmod +x "$TMP_DIR/fake_restore.sh"
reset_calls
STUB_RESTORE_PATH="" run "$STUB_PATH" "$RESTORE"
[[ "$RC" -eq 1 ]] || { printf '✗ restore-script-path 空で exit 1 にならない (RC=%s)\n' "$RC"; exit 1; }
grep -q '未ロード' "$TMP_DIR/err.log" || { printf '✗ degrade メッセージが stderr に出ない\n'; exit 1; }
assert_not_called "gum" "path 空 → degrade (gum まで到達しない)"
reset_calls
STUB_RESTORE_PATH="$TMP_DIR/no_such_file.sh" run "$STUB_PATH" "$RESTORE"
[[ "$RC" -eq 1 && ! -f "$MARKER" ]] || { printf '✗ path 不在で degrade しない\n'; exit 1; }
printf '✓ path 不在 → degrade\n'
reset_calls
STUB_RESTORE_PATH="$TMP_DIR/fake_restore.sh" STUB_GUM_EXIT=1 run "$STUB_PATH" "$RESTORE"
[[ ! -f "$MARKER" ]] || { printf '✗ 拒否したのに復元が走った\n'; exit 1; }
printf '✓ 拒否 → 復元は実行されない\n'
reset_calls
STUB_RESTORE_PATH="$TMP_DIR/fake_restore.sh" STUB_GUM_EXIT=0 run "$STUB_PATH" "$RESTORE"
[[ -f "$MARKER" ]] || { printf '✗ 承認したのに復元が実行されない\n'; exit 1; }
printf '✓ 承認 → 解決済みパスの復元スクリプトを exec\n'

printf '\n## reload_confirm: uptime ゲートの分岐\n'
NOW="$(date +%s)"
reset_calls
STUB_START_TIME=$((NOW - 300)) STUB_MAX_DELAY=60 run "$STUB_PATH" "$RELOAD" "/dev/ttys_test"
assert_called "source-file" "窓外 (uptime 300 > 60) → 即リロード"
assert_not_called "display-popup" "窓外 → popup を出さない"
assert_not_called "gum" "窓外 → gum 確認なし (通常運用の摩擦ゼロ)"
reset_calls
STUB_START_TIME=$((NOW - 10)) STUB_MAX_DELAY=60 run "$STUB_PATH" "$RELOAD" "/dev/ttys_test"
assert_called "display-popup" "窓内 (uptime 10 <= 60) → 確認 popup を開く"
assert_called "-c /dev/ttys_test" "窓内 → popup は渡された client へ表示"
assert_not_called "source-file" "窓内 → 確認前にリロードしない"
reset_calls
STUB_START_TIME=$((NOW - 10)) STUB_MAX_DELAY=60 run "$STUB_PATH" "$RELOAD" ""
assert_called "display-popup" "client 空でも popup は開く (-c なしフォールバック)"
assert_not_called "-c " "client 空 → -c を付けない"
reset_calls
STUB_START_TIME=$((NOW - 30)) STUB_MAX_DELAY="" run "$STUB_PATH" "$RELOAD" ""
assert_called "display-popup" "max-delay 未設定 + uptime 30 → fallback 60 で窓内扱い"
reset_calls
STUB_START_TIME=$((NOW - 300)) STUB_MAX_DELAY="" run "$STUB_PATH" "$RELOAD" ""
assert_called "source-file" "max-delay 未設定 + uptime 300 → fallback 60 で窓外扱い"
reset_calls
STUB_NOW=1000 STUB_START_TIME=940 STUB_MAX_DELAY=60 run "$STUB_PATH" "$RELOAD" ""
assert_called "display-popup" "境界 (uptime == max-delay) は窓内扱い (安全側。stub date で決定論化)"
reset_calls
STUB_GUM_EXIT=0 STUB_START_TIME=$((NOW - 10)) run "$STUB_PATH" "$RELOAD" --confirm
assert_called "gum confirm" "--confirm (popup 内) → gum で確認"
assert_called "source-file" "--confirm 承認 → リロード実行"
reset_calls
STUB_GUM_EXIT=1 STUB_START_TIME=$((NOW - 10)) run "$STUB_PATH" "$RELOAD" --confirm
assert_not_called "source-file" "--confirm 拒否 → リロードしない"

printf '\n## _tmux.conf の配線 (bind がガード経由になっていること)\n'
CONF="$ROOT_DIR/_tmux.conf"
assert_bind() {  # $1=キー $2=必須文字列 $3=説明
  grep -E "^bind(-key)? +$1 " "$CONF" | grep -qF "$2" \
    || { printf '✗ %s\n  (bind %s が %s を参照していない)\n' "$3" "$1" "$2"; exit 1; }
  printf '✓ %s\n' "$3"
}
assert_bind "R"    "tmux_reload_confirm.sh"  "bind R → uptime ゲート経由"
assert_bind "R"    "#{q:client_name}"        "bind R → client を #{q:} で受け渡し"
assert_bind '&'    "tmux_kill_confirm.sh window" "bind & → window kill 確認経由"
assert_bind "C-r"  "tmux_restore_confirm.sh" "bind C-r → 手動復元の確認経由"
assert_bind "x"    "tmux_kill_confirm.sh pane"   "bind x → pane kill 確認経由 (既存の回帰ガード)"
assert_bind "q"    "tmux_kill_confirm.sh others" "bind q → 他全 pane kill 確認経由 (既存の回帰ガード)"

printf '\nAll confirm-scripts tests passed successfully!\n'
