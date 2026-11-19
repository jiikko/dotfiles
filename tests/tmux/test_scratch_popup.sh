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
  display-message) [ -n "${STUB_SOCKET_PATH:-}" ] && printf '%s\n' "$STUB_SOCKET_PATH"; exit 0 ;;
  list-clients) [ -n "${STUB_CLIENTS:-}" ] && printf '%s\n' "$STUB_CLIENTS"; exit 0 ;;
  attach)
    echo "attach TMUX=${TMUX:-} TMUX_TMPDIR=${TMUX_TMPDIR:-}" >> "$CALLS"
    [ -n "${STUB_ATTACH_SLEEP:-}" ] && exec sleep "$STUB_ATTACH_SLEEP"  # sleep-ok: stub: HUP が来るまで attach が続いている形を演じる
    exit "${STUB_ATTACH_EXIT:-0}" ;;
esac
exit 0
EOS
chmod +x "$TMP_DIR/bin/tmux"
# 書き手が中の shell の pty を引く ps。テストを走らせる環境に tty が無いと本物は ?? を返し、tty= の形を検査できない
cat > "$TMP_DIR/bin/ps" <<'EOS'
#!/bin/sh
case "$*" in
  "-o tty= -p "*) [ -n "${STUB_PS_TTY:-}" ] && { printf '%s\n' "$STUB_PS_TTY"; exit 0; } ;;
esac
exec /bin/ps "$@"
EOS
chmod +x "$TMP_DIR/bin/ps"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

# 開閉の観測ログの書き手。本物 (scripts/tmux_scratch_popup_log.sh) は下でまとめて試す。
# ここでは呼ばれた引数と、そのときの TMUX (書き手のゲートが見るサーバ) を残すだけ
FAKE_LOG_OUT="$TMP_DIR/fakelog.out"; : > "$FAKE_LOG_OUT"; export FAKE_LOG_OUT
cat > "$TMP_DIR/fakelog" <<'EOS'
#!/bin/sh
echo "log-begin $1" >> "$CALLS"
[ -n "${FAKE_LOG_DELAY:-}" ] && sleep "$FAKE_LOG_DELAY"  # sleep-ok: stub: 書き終わる前に popup の片付けの signal が届く窓・開閉が待たないことを順序で見る窓を作る
echo "$* TMUX=${TMUX:-}" >> "$FAKE_LOG_OUT"
echo "log-end $1" >> "$CALLS"
EOS
chmod +x "$TMP_DIR/fakelog"
export TT_SCRATCH_POPUP_LOG="$TMP_DIR/fakelog"
# 本物の書き手が万一動いても本番の観測ログ (~/.cache/tt-restore-trigger.log) に書かない
export TT_TRIGGER_LOG="$TMP_DIR/trigger.log"
. "$ROOT_DIR/tests/lib/wait_until.sh"

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

printf '\n## scratch_popup: 開閉を観測ログに残す (issue 1098)\n'
reset_calls
run "$STUB_PATH" sh "$SCRATCH" myclient work C-t
# shellcheck disable=SC2016 # 最後の値は popup のコマンドの字面 ($TT_SP_SELF は中の shell が展開する)
for want in "-e TT_SP_KEY=C-t" "-e TT_SP_CLIENT=myclient" "-e TT_SP_SELF=$SCRATCH" 'exec sh "$TT_SP_SELF" --inner'; do
  assert_called "$want" "開き経路は popup の中の shell へ $want を渡す"
done
assert_not_called "TT_SP_SESSION" "セッション名は display-popup の argv に載せない (; で終わる名前で popup が切れる)"
reset_calls; : > "$FAKE_LOG_OUT"
FAKE_LOG_DELAY=0.5 run "$STUB_PATH" sh "$SCRATCH" /dev/ttys102 scratch t
echo "script-returned" >> "$CALLS"
has_close() { grep -qF 'close-key /dev/ttys102 key=t' "$FAKE_LOG_OUT" && grep -qx 'log-end close-key' "$CALLS"; }
tt_wait_until has_close || { printf '✗ C-t t で閉じた行が無い\n'; cat "$FAKE_LOG_OUT"; exit 1; }
printf '✓ popup の中で閉じると close-key を押したクライアントと押したキーつきで書く\n'
before() {  # $1 の行が $2 の行より前にある
  awk -v a="$1" -v b="$2" 'index($0, a) == 1 && !ia { ia = NR } index($0, b) == 1 && !ib { ib = NR } END { exit !(ia && ib && ia < ib) }' "$CALLS"
}
for first in "tmux detach-client" "script-returned"; do
  before "$first" "log-end close-key" || { printf '✗ 閉じる経路が書き手を待った (%s が書き終わりより後)\n' "$first"; cat "$CALLS"; exit 1; }
done
printf '✓ 閉じる経路は書き手を待たずに detach して終わる\n'

# popup が中の shell に渡す環境
INNER_ENV=("TMUX=/outer/sock,1,0" TMUX_TMPDIR=/outer/tmpdir TT_SP_CLIENT=/dev/ttys001 TT_SP_KEY=C-t)
reset_calls; : > "$FAKE_LOG_OUT"
PATH="$STUB_PATH" FAKE_LOG_DELAY=0.5 env "${INNER_ENV[@]}" sh "$SCRATCH" --inner >/dev/null 2>&1 && RC=0 || RC=$?
echo "script-returned" >> "$CALLS"
has_start() { grep -qE '^start /dev/ttys001 key=C-t shell=[0-9]+ TMUX=/outer/sock,1,0$' "$FAKE_LOG_OUT"; }
tt_wait_until has_start || { printf '✗ start の行が無いか、外側のサーバの TMUX で書いていない\n'; cat "$FAKE_LOG_OUT"; exit 1; }
printf '✓ 中の shell は start を外側のクライアント・キー・中の shell の pid つきで、外側のサーバの TMUX で書く\n'
has_end() { grep -qE "^end /dev/ttys001 $1 shell=[0-9]+ TMUX=/outer/sock,1,0\$" "$FAKE_LOG_OUT"; }
end_done() { has_end rc=0 && grep -qx 'log-end start' "$CALLS" && grep -qx 'log-end end' "$CALLS"; }
tt_wait_until end_done || { printf '✗ attach が終わった end の行が無い\n'; cat "$FAKE_LOG_OUT"; exit 1; }
printf '✓ attach が終わると end rc=0 を外側のサーバの TMUX で書く\n'
assert_called "attach TMUX= TMUX_TMPDIR=" "中の attach は TMUX / TMUX_TMPDIR を外して打つ (入れ子のガードと孤児サーバの予防)"
before "attach TMUX=" "log-end start" || { printf '✗ 中の shell が start の書き手を待ってから attach した\n'; cat "$CALLS"; exit 1; }
before "script-returned" "log-end end" || { printf '✗ 中の shell が end の書き手を待ってから終わった (popup が閉じるのが遅れる)\n'; cat "$CALLS"; exit 1; }
printf '✓ 中の shell は書き手を待たずに attach し、待たずに終わる\n'

reset_calls; : > "$FAKE_LOG_OUT"
PATH="$STUB_PATH" STUB_ATTACH_EXIT=1 env "${INNER_ENV[@]}" sh "$SCRATCH" --inner >/dev/null 2>&1 && RC=0 || RC=$?
tt_wait_until has_end rc=1 || { printf '✗ attach の失敗が end の rc に出ない\n'; cat "$FAKE_LOG_OUT"; exit 1; }
[[ "$RC" -eq 1 ]] || { printf '✗ attach の失敗で shell の rc が %s (1 のはず)\n' "$RC"; exit 1; }
printf '✓ attach が作り直しても失敗すると end rc=1 を書き、rc をそのまま返す\n'

# 外側のクライアントが消えると、tmux は popup の job (中の sh) に TERM を送り、pty が閉じて前面のプロセスグループ
# (sh・attach・sh が背景で起こした書き手) に HUP が届く (隔離サーバで実測した順)。同じ形を、自分のプロセスグループで
# 起こした sh へ作る。書き手が書き終わる前に、グループへもう一度 HUP と TERM を送り、書き手が生き残ることも見る
# (関数を挟まず env から sh を exec させる。間に bash の subshell が入ると、HUP でそちらが先に死に、wait が書き終わる前に返る)
reset_calls; : > "$FAKE_LOG_OUT"
set -m
PATH="$STUB_PATH" STUB_ATTACH_SLEEP=30 FAKE_LOG_DELAY=1 env "${INNER_ENV[@]}" sh "$SCRATCH" --inner >/dev/null 2>&1 &
inner_pid=$!
set +m
attached() { grep -qF 'attach TMUX=' "$CALLS"; }
tt_wait_until attached || { printf '✗ attach まで進まない\n'; exit 1; }
kill -TERM "$inner_pid"
kill -HUP -- "-$inner_pid"
wait "$inner_pid" && RC=0 || RC=$?
kill -HUP -- "-$inner_pid" 2>/dev/null || true
kill -TERM -- "-$inner_pid" 2>/dev/null || true
end_lines() { grep -c '^end ' "$FAKE_LOG_OUT" || true; }
has_signal_end() { grep -qE '^end /dev/ttys001 signal=(HUP|TERM) shell=[0-9]+ TMUX=/outer/sock,1,0$' "$FAKE_LOG_OUT"; }
tt_wait_until has_signal_end || { printf '✗ 外側が消えて終わったときの end の行が無い (書き手が片付けの signal で止まった)\n'; cat "$FAKE_LOG_OUT"; exit 1; }
[[ "$RC" -eq 129 || "$RC" -eq 143 ]] || { printf '✗ signal で終わった shell の rc が %s (129 か 143 のはず)\n' "$RC"; exit 1; }
sleep 1.5  # sleep-ok: negative: 2 本目の end が遅れて書かれないこと (書き手は 1 秒かけて書く)
[ "$(end_lines)" -eq 1 ] || { printf '✗ end の行が 1 本でない\n'; cat "$FAKE_LOG_OUT"; exit 1; }
printf '✓ 外側が消えて TERM と HUP が来ると、end を 1 行だけ書き、書き手は片付けの signal を受けても書き終える\n'

start_inner() {  # 自分のプロセスグループで中の sh を起こし、attach (stub) まで進むのを待つ
  reset_calls; : > "$FAKE_LOG_OUT"
  set -m
  PATH="$STUB_PATH" STUB_ATTACH_SLEEP=30 env "${INNER_ENV[@]}" sh "$SCRATCH" --inner >/dev/null 2>&1 &
  inner_pid=$!
  set +m
  tt_wait_until attached || { printf '✗ attach まで進まない\n'; exit 1; }
}
# pty が閉じる HUP だけが来た形 (tmux が TERM を送らない場合)
start_inner
kill -HUP -- "-$inner_pid"
wait "$inner_pid" && RC=0 || RC=$?
tt_wait_until has_end signal=HUP || { printf '✗ HUP だけが来たとき signal=HUP の end が無い\n'; cat "$FAKE_LOG_OUT"; exit 1; }
[[ "$RC" -eq 129 ]] || { printf '✗ HUP だけで終わった shell の rc が %s (129 のはず)\n' "$RC"; exit 1; }
printf '✓ HUP だけが来ても signal=HUP の end を書いて 129 で終わる\n'
# sh に TERM だけが来て、attach は別の理由で終わった形
start_inner
# sleep-ok: other: 待ちではない。attach の stub が exec したプロセスを名前で引く (exec の前に当たらないよう条件で待つ)
attach_exec() { attach_pid="$(pgrep -P "$inner_pid" sleep || true)"; [ -n "$attach_pid" ]; }
tt_wait_until attach_exec || { printf '✗ attach (stub) の pid が取れない\n'; exit 1; }
kill -TERM "$inner_pid"
kill -TERM "$attach_pid"
wait "$inner_pid" && RC=0 || RC=$?
tt_wait_until has_end signal=TERM || { printf '✗ TERM だけが来たとき signal=TERM の end が無い\n'; cat "$FAKE_LOG_OUT"; exit 1; }
[[ "$RC" -eq 143 ]] || { printf '✗ TERM で終わった shell の rc が %s (143 のはず)\n' "$RC"; exit 1; }
printf '✓ TERM が来ると signal=TERM の end を書いて 143 で終わる (書き手の起動で入れた無視が残っていない)\n'

printf '\n## scratch_popup: 本番の書き手の置き場\n'
: > "$TT_TRIGGER_LOG"; reset_calls
PATH="$STUB_PATH" STUB_SOCKET_PATH="$(realpath /tmp)/tmux-$(id -u)/default" env -u TT_SCRATCH_POPUP_LOG sh "$SCRATCH" /dev/ttys102 scratch t >/dev/null 2>&1
has_real_close() { grep -qF 'scratch-popup-close-key client=/dev/ttys102 key=t' "$TT_TRIGGER_LOG"; }
tt_wait_until has_real_close || { printf '✗ 差し込み口を外すと書き手に届かない (本番の既定のパス)\n'; cat "$TT_TRIGGER_LOG"; exit 1; }
printf '✓ 差し込み口を外すと、隣の tmux_scratch_popup_log.sh が観測ログに書く\n'

printf '\n## scratch_popup_log: 観測ログの行\n'
LOGGER="$ROOT_DIR/scripts/tmux_scratch_popup_log.sh"
DEFAULT_SOCK="$(realpath /tmp)/tmux-$(id -u)/default"
TAB="$(printf '\t')"
: > "$TT_TRIGGER_LOG"
PATH="$STUB_PATH" STUB_PS_TTY=ttys102 STUB_SOCKET_PATH="$DEFAULT_SOCK" STUB_CLIENTS="/dev/ttys009${TAB}session=other size=1x1 term=x
/dev/ttys001${TAB}session=work size=200x50 term=xterm-256color" \
  "$LOGGER" start /dev/ttys001 key=C-t "shell=$$"
want="scratch-popup-start client=/dev/ttys001 session=work size=200x50 term=xterm-256color tty=/dev/ttys102 key=C-t shell=$$"
grep -qF "$want" "$TT_TRIGGER_LOG" || { printf '✗ start の行が違う\n  want: %s\n' "$want"; cat "$TT_TRIGGER_LOG"; exit 1; }
printf '✓ 本番のサーバでは外側のクライアントのセッション・大きさ・端末・中の shell の tty (/dev/ つき) を 1 行に書く\n'
if grep -qE '(^|[[:space:]])(pid|epoch)=' "$TT_TRIGGER_LOG"; then printf '✗ watchdog の死因分類が読むキー (pid= / epoch=) を書いた\n'; cat "$TT_TRIGGER_LOG"; exit 1; fi
printf '✓ pid= / epoch= を書かない (watchdog の死因分類に拾われない)\n'
: > "$TT_TRIGGER_LOG"
PATH="$STUB_PATH" STUB_SOCKET_PATH="$DEFAULT_SOCK" "$LOGGER" end /dev/ttys001 signal=HUP
grep -qF 'scratch-popup-end client=/dev/ttys001 client-gone signal=HUP' "$TT_TRIGGER_LOG" || { printf '✗ 外側が見つからないときの行が違う\n'; cat "$TT_TRIGGER_LOG"; exit 1; }
printf '✓ 外側のクライアントが見つからなければ client-gone と書く\n'
: > "$TT_TRIGGER_LOG"
PATH="$STUB_PATH" STUB_SOCKET_PATH="$DEFAULT_SOCK" "$LOGGER" close-key /dev/ttys102 key=t
grep -qxF 'scratch-popup-close-key client=/dev/ttys102 key=t' <<< "$(cut -f2 "$TT_TRIGGER_LOG")" || { printf '✗ close-key の行が違う (detach で消える中のクライアントを引かない)\n'; cat "$TT_TRIGGER_LOG"; exit 1; }
printf '✓ close-key は中のクライアントを引かず、client-gone も書かない\n'
: > "$TT_TRIGGER_LOG"
PATH="$STUB_PATH" STUB_SOCKET_PATH="/private/tmp/isolated/tmux-$(id -u)/test" "$LOGGER" start /dev/ttys001 key=t
[ ! -s "$TT_TRIGGER_LOG" ] || { printf '✗ -L の隔離サーバの popup を本番のログに書いた\n'; cat "$TT_TRIGGER_LOG"; exit 1; }
printf '✓ 本番でないサーバ (-L の隔離サーバ) の popup は書かない\n'
: > "$TT_TRIGGER_LOG"
PATH="$STUB_PATH" TMUX="/private/tmp/isolated/tmux-$(id -u)/test,123,0" "$LOGGER" end /dev/ttys001 signal=HUP
[ ! -s "$TT_TRIGGER_LOG" ] || { printf '✗ 届かなくなった隔離サーバの popup の end を本番のログに書いた\n'; cat "$TT_TRIGGER_LOG"; exit 1; }
printf '✓ 隔離サーバが消えてサーバに届かなくても、TMUX の socket が本番でなければ書かない\n'
: > "$TT_TRIGGER_LOG"
PATH="$STUB_PATH" TMUX="$DEFAULT_SOCK,123,0" "$LOGGER" end /dev/ttys001 signal=HUP
grep -qF 'scratch-popup-end client=/dev/ttys001 client-gone signal=HUP' "$TT_TRIGGER_LOG" || { printf '✗ 本番の socket の TMUX で書かなかった\n'; cat "$TT_TRIGGER_LOG"; exit 1; }
printf '✓ TMUX の socket が本番なら書く\n'

printf '\nAll scratch popup tests passed successfully!\n'
