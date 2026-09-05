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
# tmux が非 ASCII を `_` に潰さないようロケールを UTF-8 に固定する (非 UTF-8 環境で
# このテストの ✓ / ⚙ 等が化ける。実測 2026-09-05)。
# shellcheck source=tests/lib/utf8_locale.sh
. "$ROOT_DIR/tests/lib/utf8_locale.sh"

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
TMUX_OUTER_SOCK=${TMUX:-}; TMUX_OUTER_SOCK=${TMUX_OUTER_SOCK%%,*}   # 本番ソケット (tmux 外 = CI では空。set -u なので :- が要る)
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
# alert-bell はどのウィンドウで鳴ったかを window_id 付きで記録する。
# 🚨 「hook を呼んで 1 秒待って MARK が空なら合格」にしない (否定 assert が負荷で偽の緑になる —
# run-shell が 1 秒に間に合わないほど合格しやすくなる)。代わりに **対照ベル**を使う: hook の後に
# 別の (隠れた) ウィンドウで自分がベルを鳴らし、その行が MARK に現れるまで待つ。tmux の hook は
# イベント順に走るので、対照の行が見えた時点で hook 由来のベルが在ればその手前に必ず出ている。
tmux -L "$SOCKET" set-hook -g alert-bell "run-shell 'echo fired #{window_id} >> $MARK'"
VIS_PANE=$(fresh_pane)
CTL_PANE=$(fresh_pane)   # 対照ベル用 (current にしないので隠れたまま = 鳴れば alert-bell が走る)
VIS_WIN=$(tmux -L "$SOCKET" display -p -t "$VIS_PANE" '#{window_id}')
CTL_WIN=$(tmux -L "$SOCKET" display -p -t "$CTL_PANE" '#{window_id}')
if python3 - "$SOCKET" "$VIS_PANE" "$CTL_PANE" "$CTL_WIN" "$TMUX_SOCK" "$HOOK" "$MARK" <<'PYEOF'
import os, pty, subprocess, sys, time
socket, pane, ctl_pane, ctl_win, sock_path, hook, mark = sys.argv[1:8]
def tmux(*a):
    return subprocess.run(['tmux', '-L', socket, *a], capture_output=True, text=True).stdout.strip()
def wait_until(pred, limit=15):
    deadline = time.monotonic() + limit
    while time.monotonic() < deadline:
        if pred():
            return True
        time.sleep(0.05)
    return pred()
# 対象ウィンドウを current にしてから pty で attach する (= 人が見ている状態)
subprocess.run(['tmux', '-L', socket, 'select-window', '-t', pane], check=True)
# 🚨 pty の子には TERM を必ず与える。CI runner は TERM 未設定で、その pty で `tmux attach` は
# 何も出さずに即終了する (手元で `env -u TERM` で再現。run 33937946213 の「15 秒待っても
# window_active_clients=0」の正体)。attach が死んでいるのに 15 秒待つのは無駄なので、
# 子の生死も見て早期終了なら理由付きで判定不能にする
child_env = dict(os.environ)
child_env.setdefault('TERM', 'xterm-256color')
pid, fd = pty.fork()
if pid == 0:
    os.execvpe('tmux', ['tmux', '-L', socket, 'attach'], child_env)
rc = 2
child_dead = False
def child_alive():
    # waitpid で回収した後にもう一度呼ぶと ChildProcessError になるので、死を覚えておく
    global child_dead
    if child_dead:
        return False
    try:
        child_dead = os.waitpid(pid, os.WNOHANG)[0] != 0
    except ChildProcessError:
        child_dead = True
    return not child_dead
try:
    # attach が登録されるまで待つ。固定 sleep にしない (avoid-wall-clock-assertions.md):
    # CI runner では 1.5 秒で登録されず「判定不能」になった (run 33936632229)。上限まで 0 の
    # ままなら「遅い」ではなく「この環境では attach が見えない」の証拠になる
    if not wait_until(lambda: (not child_alive()) or tmux('display', '-p', '-t', pane, '#{window_active_clients}') != '0'):
        print('  (ハーネス異常: client を attach して 15 秒待っても window_active_clients=0)')
    elif not child_alive():
        print('  (ハーネス異常: tmux attach が即終了した。TERM=%r' % child_env.get('TERM') + ')')
    else:
        env = dict(os.environ, TMUX=f'{sock_path},0,0', TMUX_PANE=pane)
        subprocess.run([hook, 'idle'], input='{"hook_event_name":"Stop","background_tasks":[]}',
                       text=True, env=env)
        # 対照ベル: 隠れたウィンドウの pane_tty へ直接 \a を書き、その alert-bell 行を待つ
        with open(tmux('display', '-p', '-t', ctl_pane, '#{pane_tty}'), 'w') as tty:
            tty.write('\a')
        def ctl_seen():
            try:
                return any(line.strip() == f'fired {ctl_win}' for line in open(mark))
            except FileNotFoundError:
                return False
        if wait_until(ctl_seen):
            rc = 0
        else:
            print('  (ハーネス異常: 対照ベルの alert-bell が 15 秒待っても記録されない)')
finally:
    try:
        os.kill(pid, 15)
    except ProcessLookupError:
        pass
sys.exit(rc)
PYEOF
then
  if grep -q "fired $VIS_WIN" "$MARK"; then bad '見えているウィンドウでベルを鳴らしている (800ms トーストが被さる)'
  else ok "見えているウィンドウでは鳴らさない (対照ベル $CTL_WIN は記録された)"; fi
  expect_state_pane "$VIS_PANE" '✓ idle' '見えていても状態表示は更新する'
else
  bad '見えているウィンドウの検査ができなかった (判定不能)'
fi
rm -f "$MARK"

[ "$fail" -eq 0 ] || { printf '✗ 失敗あり\n'; exit 1; }
printf '✓ すべて成功\n'
