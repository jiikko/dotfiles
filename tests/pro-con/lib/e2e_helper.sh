# shellcheck shell=bash
# tests/pro-con/ の e2e テストの共通部品。`pro-con e2e start` が立てた置き場ごとの隔離 tmux サーバだけを操作する。
#
# 🚨 tmux は必ず `-S <置き場の e2e-tmux-socket に控えられたパス>` で呼び、TMUX / TMUX_PANE を落とす
#   ($TMUX が生きていると本番サーバへ繋がる。~/.claude/rules/tmux-probe-requires-socket-isolation.md)。
#   控えのパスが pro-con-e2e-* で終わらなければ撃たずに落とす。
# 🚨 kill -9 するのは、この置き場の dispatcher / 画面だと ps の command line で確かめたプロセスだけ。
# 待ちは sleep でなく tt_wait_until (tests/lib/wait_until.sh) の条件のポーリング。

# shellcheck source=tests/lib/wait_until.sh
. "$ROOT_DIR/tests/lib/wait_until.sh"

PC="$ROOT_DIR/bin/pro-con"

e2e_skip_unless_tools() {
  local c
  for c in tmux go jq; do
    command -v "$c" >/dev/null 2>&1 || { echo "[skip] $c が無い"; exit 77; }
  done
}

# e2e_setup: 置き場を作り、終了時の後始末を仕掛ける。$work / $root / $state を決める
e2e_setup() {
  work="$(mktemp -d)" || exit 1
  root="$work/e2e"
  state="$root/state"
  sock=""
  trap e2e_teardown EXIT
}

# 後始末: 画面・dispatcher・偽の PG を `pro-con e2e stop` で止める (控えた socket も消す)。
# 通常の出口では各テストが e2e_assert_clean で確かめた後なので、ここは失敗時の片付け
e2e_teardown() {
  [ -n "${root:-}" ] && [ -d "$root" ] && "$PC" e2e stop "$root" > /dev/null 2>&1
  rm -rf "$work"
}

tmx() {
  case "$sock" in
    */pro-con-e2e-*) ;;
    *) echo "✗ 隔離サーバの socket が控えられていない / 名前が想定外: [$sock]"; exit 1 ;;
  esac
  env -u TMUX -u TMUX_PANE tmux -S "$sock" "$@"
}

e2e_start() {
  if ! "$PC" e2e start "$root" > "$work/start.out" 2>&1; then
    echo "✗ e2e の画面を起動できない"; cat "$work/start.out"; exit 1
  fi
  sock="$(cat "$root/e2e-tmux-socket")"
}

# 画面 <window> に文 <want> が出ているか
e2e_screen_has() { tmx capture-pane -p -t "0:$1" 2>/dev/null | grep -qF -- "$2"; }

# 2 つ目の画面を window 1 に開き、起動を待つ
e2e_open_second_screen() {
  tmx new-window -d -t 0: "$PC --e2e '$root'"
  if ! TT_WAIT_TICKS=200 tt_wait_until e2e_screen_has 1 producer-consumer; then
    echo "✗ 2 つ目の画面が起動しない"; tmx capture-pane -p -t 0:1 2>&1 | tail -20; exit 1
  fi
}

# 画面 <window> で Q → quit
e2e_quit() {
  tmx send-keys -t "0:$1" Q
  tmx send-keys -t "0:$1" -l quit
  tmx send-keys -t "0:$1" Enter
}

e2e_window_count() { tmx list-windows -t 0 -F x 2>/dev/null | grep -c x; }
e2e_windows_are() { [ "$(e2e_window_count)" = "$1" ]; }
e2e_server_gone() { ! tmx has-session 2>/dev/null; }

# この置き場の dispatcher の pid (ロックの記録 + command line で本人確認。違えば空)
e2e_dispatcher_pid() {
  local pid
  pid="$(cat "$state/dispatcher.lock" 2>/dev/null)" || return 0
  case "$pid" in ''|*[!0-9]*) return 0 ;; esac
  ps -p "$pid" -o command= 2>/dev/null | grep -F -- "--e2e $root" | grep -qF dispatcher && echo "$pid"
  return 0
}
e2e_dispatcher_alive() { [ -n "$(e2e_dispatcher_pid)" ]; }
e2e_dispatcher_gone() { ! e2e_dispatcher_alive; }

# 画面が 1 つも無い状態が続いて dispatcher が自分で抜けた (最後の画面の quit が止めたのではない) なら、dispatcher.log にこの行が出る。
# 🚨 最後の画面が止めたかを stop-result=ok だけで見ない: 止めなくても 1 分後にこの経路が ok を書くので、待ちの上限次第で素通りする
# (変異「最後の画面でも止めない」が緑のまま通った。2026-09-25)
E2E_ALONE_EXIT="開いている画面が"
e2e_exited_alone() { grep -qF "$E2E_ALONE_EXIT" "$state/dispatcher.log" 2>/dev/null; }

e2e_stop_result() { cat "$state/stop-result" 2>/dev/null; }
e2e_stopped_ok() { [ "$(e2e_stop_result)" = ok ]; }

# 期待が外れたら、見るべきものを出して落とす
e2e_fail() {
  echo "✗ $1"
  echo "--- stop-result: [$(e2e_stop_result)] / dispatcher pid: [$(e2e_dispatcher_pid)] / windows: [$(e2e_window_count 2>/dev/null)]"
  ps -A -o pid=,command= | grep -F -- "--e2e $root" | grep -v grep
  exit 1
}

# 閉じた後に残骸が無いこと: この置き場のプロセス / 隔離サーバ
e2e_no_procs() { ! ps -A -o command= | grep -F -- "--e2e $root" | grep -qv grep; }
e2e_assert_clean() {
  TT_WAIT_TICKS=200 tt_wait_until e2e_no_procs || e2e_fail "閉じた後もこの置き場のプロセスが残っている"
  e2e_server_gone || e2e_fail "閉じた後も隔離サーバが残っている"
  # 画面を quit で閉じた形では socket の控えが残るので、e2e stop に消させてから socket の不在を見る
  "$PC" e2e stop "$root" > "$work/stop.out" 2>&1 || { cat "$work/stop.out"; e2e_fail "e2e stop が失敗した"; }
  [ ! -e "$sock" ] || e2e_fail "隔離サーバの socket が残っている: $sock"
}
