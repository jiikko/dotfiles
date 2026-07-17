#!/usr/bin/env bash
# scripts/tmux_scratch_popup.sh (開閉トグル) と scripts/tmux_launcher_run.sh (実行エンジン) の
# unit テスト。test_fork_scratch.sh の検査 B は静的 grep (bind 参照 / -A 不使用) のみで、
# 分岐の実挙動 (どの session 名で閉じるか / client 引数の形状 / 終了コード) は未カバーだった。
# stub tmux (PATH 先頭) で全呼び出しを傍受し、実サーバには触れない。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRATCH="$ROOT_DIR/scripts/tmux_scratch_popup.sh"
LAUNCHER="$ROOT_DIR/scripts/tmux_launcher_run.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
STATE="$TMP_DIR/state"; export STATE
mkdir -p "$TMP_DIR/bin" "$STATE"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  detach-client) exit "${STUB_DETACH_EXIT:-0}" ;;
  has-session)
    n=$(cat "$STATE/has_n" 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > "$STATE/has_n"
    # STUB_HAS_EXITS: has-session の n 回目の exit code を空白区切りで指定 (省略時 0)
    code=$(printf '%s' "${STUB_HAS_EXITS:-0}" | cut -d' ' -f"$n")
    exit "${code:-0}" ;;
  new-session)   exit "${STUB_NEWSESS_EXIT:-0}" ;;
esac
exit 0
EOS
chmod +x "$TMP_DIR/bin/tmux"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
reset_state() { reset_calls; rm -f "$STATE/has_n"; }

printf '## scratch_popup: 開閉トグル\n'
reset_state
run "$STUB_PATH" sh "$SCRATCH" myclient scratch
assert_called "detach-client -t myclient" "session=scratch → detach で閉じる (client 指定付き)"
assert_not_called "display-popup" "閉じ経路では popup を開かない"
[[ "$RC" -eq 0 ]] || { printf '✗ 閉じ経路の exit が %s (0 のはず)\n' "$RC"; exit 1; }
printf '✓ 閉じ経路は exit 0\n'
reset_state
run "$STUB_PATH" sh "$SCRATCH" myclient launcher
assert_called "detach-client" "session=launcher も閉じ対象 (C-t t 共通閉じキーのスタック防止)"
reset_state
STUB_DETACH_EXIT=1 run "$STUB_PATH" sh "$SCRATCH" myclient scratch
[[ "$RC" -eq 0 ]] || { printf '✗ detach 失敗でも exit 0 のはず (RC=%s)\n' "$RC"; exit 1; }
printf '✓ detach-client 失敗でも exit 0 (強制 0 の契約)\n'
reset_state
run "$STUB_PATH" sh "$SCRATCH" "" scratch
assert_called "detach-client" "client 空でも閉じ経路は動く"
grep -qF -- "-t" "$CALLS" && { printf '✗ client 空で -t トークンが出た\n'; cat "$CALLS"; exit 1; }
printf '✓ client 空 → -t トークン自体が消える (word-splitting 契約)\n'
reset_state
run "$STUB_PATH" sh "$SCRATCH" myclient work
assert_called "display-popup" "scratch/launcher 以外 → popup を開く"
assert_called "-c myclient" "開き経路は client へ popup を表示"
assert_not_called "detach-client" "開き経路で detach しない"

printf '\n## launcher_run: セッション管理と popup\n'
reset_state
run "$STUB_PATH" sh "$LAUNCHER" myclient work /somewhere mywin
[[ "$RC" -ne 0 ]] || { printf '✗ 引数不足 (cmd 欠落) で exit 0 になった\n'; exit 1; }
[[ ! -s "$CALLS" ]] || { printf '✗ 引数不足なのに tmux が呼ばれた\n'; cat "$CALLS"; exit 1; }
printf '✓ 必須引数の欠落は tmux を一切呼ばず非 0 (set -u)\n'
reset_state
run "$STUB_PATH" sh "$LAUNCHER" myclient work /somewhere mywin "make build"
assert_called "new-window -t launcher -n mywin -c /somewhere make build; exec " "コマンドは launcher セッションの新 window で実行 (終了後 shell へ降りる)"
assert_called "display-popup" "呼び出し元が launcher 外 → popup を開く"
assert_called "attach -t launcher" "popup は launcher セッションへ attach"
assert_not_called "new-session" "セッション既存 (has-session 成功) なら作成しない"
reset_state
run "$STUB_PATH" sh "$LAUNCHER" myclient launcher /somewhere mywin "make build"
assert_called "new-window -t launcher" "launcher 内からの実行でも window は作る"
assert_not_called "display-popup" "既に launcher popup 内なら popup を開かない"
[[ "$RC" -eq 0 ]] || { printf '✗ launcher 内経路の exit が %s (0 のはず)\n' "$RC"; exit 1; }
printf '✓ launcher 内経路は exit 0\n'
reset_state
STUB_HAS_EXITS="1 0" run "$STUB_PATH" sh "$LAUNCHER" myclient work /x w "cmd"
assert_called "new-session -d -s launcher" "セッション未存在 → new-session -d で作成 (-A なし)"
assert_called "new-window" "作成後に window 起動へ進む"
reset_state
STUB_HAS_EXITS="1 0" STUB_NEWSESS_EXIT=1 run "$STUB_PATH" sh "$LAUNCHER" myclient work /x w "cmd"
assert_called "new-window" "並行レース (new-session 重複失敗) は再確認して続行"
reset_state
STUB_HAS_EXITS="1 1" STUB_NEWSESS_EXIT=1 run "$STUB_PATH" sh "$LAUNCHER" myclient work /x w "cmd"
[[ "$RC" -ne 0 ]] || { printf '✗ セッション確保が全滅しても続行した\n'; exit 1; }
assert_not_called "new-window" "セッション確保全滅 → window を作らず停止 (set -e の契約)"

printf '\nAll scratch/launcher popup tests passed successfully!\n'
