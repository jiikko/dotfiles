#!/usr/bin/env zsh
#
# scripts/tmux_reap_orphan_servers.sh の回帰テスト。
# 不変条件を 2 つ検査する:
#   A) socket ファイルが消えた孤児 tmux サーバ（プロセスだけ生存）を reap が kill する
#   B) 生存 socket を持つサーバ（実サーバ・接続中 client 相当）を reap が絶対に kill しない
#
# 隔離方針: 孤児役・生存役とも専用 TMUX_TMPDIR + 名前付き socket (-L) で起こす。reap は
# ユーザーの全 `^tmux` プロセスを走査するが、判定キーは「socket ファイルの実在」なので、
# このテストが起こした生存役と、(もし在れば)実本番サーバは生存 socket を持つため保護される。
# 孤児役だけが socket 消滅状態になり reap 対象になる。実サーバには一切触れない。

set -euo pipefail
unset CDPATH

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
REAP="$ROOT_DIR/scripts/tmux_reap_orphan_servers.sh"

UID_NUM=$(id -u)
# unix socket のパス長上限 (macOS は sun_path 104 byte) を超えないよう、TMUX_TMPDIR は
# /var/folders 配下の長い mktemp ではなく /tmp 直下の短い temp dir にし、socket 名も短くする。
ORPHAN_DIR=$(mktemp -d /tmp/reapo.XXXXXX)
LIVE_DIR=$(mktemp -d /tmp/reapl.XXXXXX)
ORPHAN_SOCK="ro$$"
LIVE_SOCK="rl$$"
orphan_pid=""
live_pid=""
# reap は実プロセステーブルを pgrep で走査する設計のため、このテストの reap 実行は「自分が作った
# 孤児」だけでなく、実環境に偶々存在する他の dead-socket 孤児も回収しうる（reap は生存 socket を
# 持つプロセスには絶対触れないので副作用は常に良性=ゴミ掃除）。ただし reap のログ書き込みは
# 実 ~/.cache を汚さないよう temp HOME に隔離する。
export HOME="$ORPHAN_DIR/home"
mkdir -p "$HOME"

fail() { print -u2 "[test-reap:zsh] FAIL: $1"; exit 1; }
ok()   { print "[test-reap:zsh] ok: $1"; }

cleanup() {
  # 生存役は明示的に PID で確実に落とす（socket 経由でなく PID 指定なので取りこぼさない）。
  [[ -n "$live_pid" ]]   && kill -KILL "$live_pid"   2>/dev/null || true
  [[ -n "$orphan_pid" ]] && kill -KILL "$orphan_pid" 2>/dev/null || true
  env TMUX_TMPDIR="$LIVE_DIR"   "$TMUX_BIN_PATH" -L "$LIVE_SOCK"   kill-server >/dev/null 2>&1 || true
  env TMUX_TMPDIR="$ORPHAN_DIR" "$TMUX_BIN_PATH" -L "$ORPHAN_SOCK" kill-server >/dev/null 2>&1 || true
  rm -rf "$ORPHAN_DIR" "$LIVE_DIR"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

command -v "$TMUX_BIN_PATH" >/dev/null 2>&1 || { print -u2 "tmux not found (set \$TMUX_BIN)"; exit 1; }
[[ -x "$REAP" ]] || fail "reap script not found/executable: $REAP"
command -v lsof >/dev/null 2>&1 || { print -u2 "[test-reap:zsh] skipped: lsof not available"; exit 0; }

# ---- 生存役・孤児役のサーバを起こす ----
start_log="$ORPHAN_DIR/start.log"
if ! env TMUX_TMPDIR="$LIVE_DIR" "$TMUX_BIN_PATH" -L "$LIVE_SOCK" \
     new-session -d -s live "tail -f /dev/null" >"$start_log" 2>&1; then
  if grep -qiE "operation not permitted|permission denied" "$start_log"; then
    print -u2 "[test-reap:zsh] skipped: tmux cannot create sockets in this environment"
    exit 0
  fi
  cat "$start_log" >&2; fail "failed to start live server"
fi
env TMUX_TMPDIR="$ORPHAN_DIR" "$TMUX_BIN_PATH" -L "$ORPHAN_SOCK" \
  new-session -d -s orphan "tail -f /dev/null" >>"$start_log" 2>&1 || { cat "$start_log" >&2; fail "failed to start orphan server"; }

live_pid=$(env TMUX_TMPDIR="$LIVE_DIR"   "$TMUX_BIN_PATH" -L "$LIVE_SOCK"   display-message -p '#{pid}')
orphan_pid=$(env TMUX_TMPDIR="$ORPHAN_DIR" "$TMUX_BIN_PATH" -L "$ORPHAN_SOCK" display-message -p '#{pid}')
[[ -n "$live_pid" && -n "$orphan_pid" ]] || fail "server PID を取得できなかった (live=$live_pid orphan=$orphan_pid)"
kill -0 "$live_pid" 2>/dev/null   || fail "live server がすぐ死んだ (pid=$live_pid)"
kill -0 "$orphan_pid" 2>/dev/null || fail "orphan server がすぐ死んだ (pid=$orphan_pid)"

# ---- 孤児化: socket ファイルだけ消す（プロセスは残す） ----
orphan_socket_path="$ORPHAN_DIR/tmux-$UID_NUM/$ORPHAN_SOCK"
[[ -S "$orphan_socket_path" ]] || fail "orphan socket が想定パスに無い: $orphan_socket_path"
rm -f "$orphan_socket_path"
[[ -S "$orphan_socket_path" ]] && fail "orphan socket を消せていない"
kill -0 "$orphan_pid" 2>/dev/null || fail "socket を消したら orphan プロセスまで消えた (想定外)"
ok "準備: orphan は socket 消滅・プロセス生存 / live は socket 生存"

# ---- reap 実行 ----
"$REAP" >/dev/null 2>&1 || true

# A) 孤児が kill されること（TERM は非同期なので最大 ~3s 待つ）
i=0
while kill -0 "$orphan_pid" 2>/dev/null && [ "$i" -lt 30 ]; do sleep 0.1; i=$((i+1)); done
kill -0 "$orphan_pid" 2>/dev/null && fail "A: 孤児サーバ (pid=$orphan_pid) が reap 後も生存している"
ok "A: socket 消滅の孤児サーバを reap が回収した"

# B) 生存 socket のサーバは保護されること
kill -0 "$live_pid" 2>/dev/null || fail "B: 生存 socket のサーバ (pid=$live_pid) を reap が誤って kill した"
ok "B: 生存 socket のサーバは保護された"

print "[test-reap:zsh] done"
