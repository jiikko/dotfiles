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
TMUX_OUTER_SOCK=${TMUX%%,*}   # 本番ソケット (tmux 外で走らせたなら空)
unset TMUX TMUX_PANE
cleanup() { tmux -L "$SOCKET" kill-server 2>/dev/null || :; }
trap cleanup EXIT
tmux -L "$SOCKET" -f /dev/null new-session -d -x 80 -y 24 'sleep 300'
TMUX_SOCK=$(tmux -L "$SOCKET" display -p '#{socket_path}')

# 🚨 隔離の実証を「-L のサーバに本番セッションが見えないか」で書かないこと (-L は定義上
# その専用ソケットしか見ないので必ず通る = 何も守らない)。実際に壊れうるのは
# **フックを呼ぶときの $TMUX 差し替え**で、抜けるとフックの素の `tmux` が本番を向く。
if [ -n "$TMUX_OUTER_SOCK" ] && [ "$TMUX_SOCK" = "$TMUX_OUTER_SOCK" ]; then
  printf '✗ 隔離できていない (フックへ渡すソケットが本番と同じ: %s)\n' "$TMUX_SOCK"; exit 1
fi

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
expect_state_pane() { # expect_state_pane <pane> <期待> <label>
  local got; got=$(state "$1")
  if [ "$got" = "$2" ]; then ok "$3"; else bad "$3: 期待 '$2' / 実際 '$got'"; fi
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

printf 'Test 3b: Stop (bg タスクが pending / paused) → 鳴らない\n'
# 🚨 status は 6 値あり、Claude Code は background_tasks を
# "In-flight background work (running/pending + backgrounded)" と定義している
# (バイナリの enum と説明文を実測 2026-09-05)。"running" だけの allowlist だと
# ここが素通りする = 直したはずの「実行中なのにベル」がそのまま残る
expect_bell idle '{"hook_event_name":"Stop","background_tasks":[{"id":"a","status":"pending"}]}' 0 'pending はベルなし'
expect_state '⚙ working (bg:1)' '状態は ⚙ working (bg:1)'
expect_bell idle '{"hook_event_name":"Stop","background_tasks":[{"id":"a","status":"paused"}]}' 0 'paused はベルなし'

printf 'Test 4: Stop (bg タスクは終端状態) → 鳴る\n'
expect_bell idle '{"hook_event_name":"Stop","background_tasks":[{"id":"a","status":"completed"}]}' 1 'completed のみなら応答完了扱い'
expect_bell idle '{"hook_event_name":"Stop","background_tasks":[{"id":"a","status":"failed"},{"id":"b","status":"killed"}]}' 1 'failed / killed のみなら応答完了扱い'

printf 'Test 5: Notification (入力待ち) → 鳴る\n'
expect_bell input '{"hook_event_name":"Notification","notification_type":"idle_prompt"}' 1 '入力待ちでベル'

printf 'Test 6: Notification (入力不要な種別) → 鳴らない\n'
expect_bell input '{"hook_event_name":"Notification","notification_type":"auth_success"}' 0 'auth_success はベルなし'
expect_bell input '{"hook_event_name":"Notification","notification_type":"agent_completed"}' 0 'agent_completed はベルなし'

printf 'Test 7: 作業中 (working / start) → 鳴らない\n'
expect_bell working '{}' 0 'working はベルなし'
expect_bell start   '{}' 0 'start はベルなし'

printf 'Test 8: 見えているウィンドウ → 鳴らさない (alert-bell のトーストを出さない)\n'
# 🚨 クライアントを attach しないと構造的に観測できないケース。window_bell_flag は
# 可視ウィンドウでは tmux が即座に落とすので「flag=0」を見ても合格に見えるが、
# _tmux.conf の alert-bell は落ちる前に発火して 800ms のトースト (-N なのでキー入力でも
# 消えない) を出す。判定は「flag が立ったか」ではなく「alert-bell が発火したか」で行う。
MARK=$(mktemp -t pane-state-bell)
tmux -L "$SOCKET" set-hook -g alert-bell "run-shell 'echo fired >> $MARK'"
VIS_PANE=$(fresh_pane)
if python3 - "$SOCKET" "$VIS_PANE" "$TMUX_SOCK" "$HOOK" <<'PYEOF'
import os, pty, subprocess, sys, time
socket, pane, sock_path, hook = sys.argv[1:5]
# 対象ウィンドウを current にしてから pty で attach する (= 人が見ている状態)
subprocess.run(['tmux', '-L', socket, 'select-window', '-t', pane], check=True)
pid, fd = pty.fork()
if pid == 0:
    os.execvp('tmux', ['tmux', '-L', socket, 'attach'])
rc = 2
try:
    time.sleep(1.5)
    seen = subprocess.run(['tmux', '-L', socket, 'display', '-p', '-t', pane,
                           '#{window_active_clients}'], capture_output=True, text=True).stdout.strip()
    if seen == '0':
        print('  (ハーネス異常: client を attach したのに window_active_clients=0)')
    else:
        env = dict(os.environ, TMUX=f'{sock_path},0,0', TMUX_PANE=pane)
        subprocess.run([hook, 'idle'], input='{"hook_event_name":"Stop","background_tasks":[]}',
                       text=True, env=env)
        time.sleep(1)
        rc = 0
finally:
    os.kill(pid, 15)
sys.exit(rc)
PYEOF
then
  if [ -s "$MARK" ]; then bad '見えているウィンドウでベルを鳴らしている (800ms トーストが被さる)'
  else ok '見えているウィンドウでは鳴らさない'; fi
  expect_state_pane "$VIS_PANE" '✓ idle' '見えていても状態表示は更新する'
else
  bad '見えているウィンドウの検査ができなかった (判定不能)'
fi
rm -f "$MARK"

[ "$fail" -eq 0 ] || { printf '✗ 失敗あり\n'; exit 1; }
printf '✓ すべて成功\n'
