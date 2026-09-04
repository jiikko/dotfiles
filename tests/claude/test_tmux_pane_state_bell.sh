#!/usr/bin/env bash
# _claude/hooks/tmux-pane-state.sh のベル発火条件を pin する unit テスト。
#
# なぜ: Claude Code 内蔵のベルは Stop / Notification で無条件に鳴り、bg タスクが
# 走っている最中も「手が空いた」と同じ印を tmux に付ける。内蔵ベルを切って
# (settings.json: preferredNotifChannel=notifications_disabled) このフックだけが
# 鳴らす形にしているので、①内蔵ベルの再有効化 ②bg 判定の退行 のどちらでも
# 「実行中なのにベル」が戻る。両方をここで検査する。
#
# 🚨 tmux は隔離ソケット (-L) で起こす。素の tmux は $TMUX が優先され本番サーバを向く。
# 規範: _claude/rules/tmux-probe-requires-socket-isolation.md
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/tmux-pane-state.sh"
SETTINGS="$ROOT_DIR/_claude/settings.json"

[ -x "$HOOK" ] || { printf '✗ フックが無い / 実行権限が無い: %s\n' "$HOOK"; exit 1; }
command -v tmux >/dev/null 2>&1 || { printf '✗ tmux が無く検査できない (判定不能)\n'; exit 1; }
command -v jq  >/dev/null 2>&1 || { printf '✗ jq が無いと bg 判定が常に 0 に倒れ検査にならない (判定不能)\n'; exit 1; }

fail=0
ok()   { printf '  ✓ %s\n' "$1"; }
bad()  { printf '  ✗ %s\n' "$1"; fail=1; }

# --- 配線: 内蔵ベルが切れていること (これが on だと本フックで絞った意味が消える) ---
printf 'Test 1: settings.json の配線\n'
if grep -q '"preferredNotifChannel"[[:space:]]*:[[:space:]]*"notifications_disabled"' "$SETTINGS"; then
  ok 'Claude Code 内蔵ベルが notifications_disabled'
else
  bad '内蔵ベルが無効化されていない (Stop / Notification で無条件に鳴る)'
fi
if grep -q 'tmux-pane-state.sh idle' "$SETTINGS" && grep -q 'tmux-pane-state.sh input' "$SETTINGS"; then
  ok 'Stop / Notification にフックが配線されている'
else
  bad 'フックが settings.json に配線されていない (ベルが誰も鳴らさない)'
fi

# --- 隔離 tmux サーバ ---
SOCKET="pane-state-bell-$$"
unset TMUX TMUX_PANE
cleanup() { tmux -L "$SOCKET" kill-server 2>/dev/null || :; }
trap cleanup EXIT
tmux -L "$SOCKET" -f /dev/null new-session -d -x 80 -y 24 'sleep 300'
# 🚨 本番サーバが見えていないことを実証してから使う (成功メッセージは隔離の証拠にならない)
isolated_sessions=$(tmux -L "$SOCKET" list-sessions -F '#{session_name}')
if grep -qv '^0$' <<< "$isolated_sessions"; then
  printf '✗ 隔離できていない (本番セッションが見える)\n'; exit 1
fi
TMUX_SOCK=$(tmux -L "$SOCKET" display -p '#{socket_path}')

# 🚨 window_bell_flag はクライアントが見るまで落ちない (monitor-bell の off/on でも
# 消えない — 実測)。ケース間で状態を持ち越すと前のケースのベルを次の期待値と
# 取り違えるので、ケースごとに新しいウィンドウ = 新しいペインを作る。
fresh_pane() {
  tmux -L "$SOCKET" new-window -d -P -F '#{pane_id}' 'sleep 300'
}

# フックは素の `tmux` を呼ぶので、$TMUX で隔離ソケットへ向ける
run_hook() { # run_hook <pane> <arg> <stdin-json>
  printf '%s' "$3" | TMUX="$TMUX_SOCK,0,0" TMUX_PANE="$1" "$HOOK" "$2"
}
bell_flag() { tmux -L "$SOCKET" display -p -t "$1" '#{window_bell_flag}'; }
state()     { tmux -L "$SOCKET" display -p -t "$1" '#{@claude_state}'; }

PANE=''  # 直前のケースのペイン (状態の確認用)
expect_bell() { # expect_bell <arg> <json> <期待 0|1> <label>
  PANE=$(fresh_pane)
  [ "$(bell_flag "$PANE")" = 0 ] || { bad "$4: 新ペインの bell が既に立っている (ハーネス異常)"; return; }
  run_hook "$PANE" "$1" "$2"
  local got; got=$(bell_flag "$PANE")
  if [ "$got" = "$3" ]; then ok "$4 (bell=$got)"; else bad "$4: 期待 bell=$3 / 実際 bell=$got"; fi
}
expect_state() { # expect_state <期待> <label>
  local got; got=$(state "$PANE")
  if [ "$got" = "$1" ]; then ok "$2"; else bad "$2: 期待 '$1' / 実際 '$got'"; fi
}

printf 'Test 2: Stop (bg タスクなし) → 鳴る\n'
expect_bell idle '{"hook_event_name":"Stop","background_tasks":[]}' 1 '応答完了でベル'
expect_state '✓ idle' '状態は ✓ idle' 

printf 'Test 3: Stop (bg タスク実行中) → 鳴らない\n'
expect_bell idle '{"hook_event_name":"Stop","background_tasks":[{"id":"a","status":"running"}]}' 0 'bg 実行中はベルなし'
expect_state '⚙ working (bg:1)' '状態は ⚙ working (bg:1)' 

printf 'Test 4: Stop (bg タスクは完了済み) → 鳴る\n'
expect_bell idle '{"hook_event_name":"Stop","background_tasks":[{"id":"a","status":"completed"}]}' 1 '完了済み bg のみなら応答完了扱い'

printf 'Test 5: Notification (入力待ち) → 鳴る\n'
expect_bell input '{"hook_event_name":"Notification","notification_type":"idle_prompt"}' 1 '入力待ちでベル'

printf 'Test 6: Notification (入力不要な種別) → 鳴らない\n'
expect_bell input '{"hook_event_name":"Notification","notification_type":"auth_success"}' 0 'auth_success はベルなし'
expect_bell input '{"hook_event_name":"Notification","notification_type":"agent_completed"}' 0 'agent_completed はベルなし'

printf 'Test 7: 作業中 (working / start) → 鳴らない\n'
expect_bell working '{}' 0 'working はベルなし'
expect_bell start   '{}' 0 'start はベルなし'

[ "$fail" -eq 0 ] || { printf '✗ 失敗あり\n'; exit 1; }
printf '✓ すべて成功\n'
