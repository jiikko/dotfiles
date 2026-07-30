#!/usr/bin/env bash
# scripts/tmux_log_kill_command.sh (kill-server/kill-session alias shim) の unit テスト。
#
# この shim は kill コマンドの実行経路上に同期で挟まるため、
#   (1) どんな入力でも exit 0 (非 0 だと run-shell エラーが pane に積まれ、kill も阻害しうる)
#   (2) 発火条件 (kill-server は常に保存 / kill-session は残り 1 以下のみ保存)
#   (3) default socket 以外では完全 no-op (共有ログを汚さない)
# の 3 点を stub tmux + 隔離ログで pin する。実 tmux サーバ・実 ~/.cache には触れない。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_log_kill_command.sh"
TMP_DIR="$(mktemp -d)"
HELPER_PIDS=()
cleanup() {
  for p in "${HELPER_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin"
DEFAULT_SOCK="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"

# subcommand を見て応答を変える stub tmux (display-message は socket_path / pid、
# list-sessions はセッション一覧、show は保存スクリプトパス)
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  display-message)
    case "$*" in
      *socket_path*) printf '%s\n' "${STUB_SOCKET_PATH:-}" ;;
      *) printf '%s\n' "99999" ;;
    esac ;;
  list-sessions) printf '%b' "${STUB_SESSIONS:-}" ;;
  show) printf '%s\n' "${STUB_SAVE_SCRIPT:-}" ;;
esac
EOS
chmod +x "$TMP_DIR/bin/tmux"

# 呼び出しを記録するだけの stub 保存スクリプト
cat > "$TMP_DIR/bin/fake_save.sh" <<'EOS'
#!/bin/sh
echo "save $*" >> "$CALLS"
exit "${STUB_SAVE_RC:-0}"
EOS
chmod +x "$TMP_DIR/bin/fake_save.sh"

STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
LOG="$TMP_DIR/trigger.log"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

# --- (a) default socket で kill-server → 保存が走り kill-cmd 行が書かれる -------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" TT_KILL_SAVE_WAIT_SECONDS=2 \
  STUB_SOCKET_PATH="$DEFAULT_SOCK" STUB_SESSIONS='a: 1 windows\nb: 2 windows\n' \
  STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-server
[[ "$RC" -eq 0 ]] || { printf '✗ kill-server 正常系で exit %s (0 のはず)\n' "$RC"; exit 1; }
assert_called "save quiet" "kill-server で直前保存が quiet 起動される"
grep -qE '	kill-cmd cmd=kill-server pid=[0-9]+ sessions=2 save=ok epoch=[0-9]+ issuer=' "$LOG" \
  || { printf '✗ kill-cmd 行の書式が想定と違う:\n'; cat "$LOG"; exit 1; }
printf '✓ kill-cmd 行 (cmd/pid/sessions/save/epoch/issuer) がタブ区切りで追記される\n'

# --- (a2) wrapper がガードで拒否したら save=ok と書かない (ログが嘘をつかない) --------
# レビューで実証された欠陥: kill -0 ポーリングだけで exit code を捨てていたため、復元中ガード等で
# 何も保存されていないのに save=ok と記録していた ("損失窓ゼロ" の主張がログ上見えなくなる)。
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" TT_KILL_SAVE_WAIT_SECONDS=2 \
  STUB_SOCKET_PATH="$DEFAULT_SOCK" STUB_SESSIONS='a: 1 windows\nb: 1 windows\n' \
  STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" STUB_SAVE_RC=1 \
  run "$STUB_PATH" "$SCRIPT" kill-server
[[ "$RC" -eq 0 ]] || { printf '✗ 保存拒否時に exit %s (kill を阻害してはいけない)\n' "$RC"; exit 1; }
grep -q 'save=rejected-rc1' "$LOG" \
  || { printf '✗ 保存が拒否されたのに rejected-rc1 が記録されない:\n'; cat "$LOG"; exit 1; }
grep -q 'save=ok' "$LOG" && { printf '✗ 保存されていないのに save=ok と記録した:\n'; cat "$LOG"; exit 1; }
printf '✓ wrapper が拒否した保存は save=rejected-rc<N> として記録される (save=ok と書かない)\n'

# --- (a3) hold セッションのみなら保存を発火させない (tt bootstrap を待たせない) -------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='__tt_hold_1234: 1 windows\n' STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-session
assert_not_called "save quiet" "hold のみの状態では保存を発火しない (lock 待ちで tt を遅らせない)"
grep -q 'save=skipped-hold-only' "$LOG" \
  || { printf '✗ skipped-hold-only が記録されない:\n'; cat "$LOG"; exit 1; }
printf '✓ hold のみの kill-session は保存せず skipped-hold-only を記録\n'

# --- (b) 残り 3 セッションへの kill-session → 保存しない (サーバは存続するため) --------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='a: 1 windows\nb: 2 windows\nc: 1 windows\n' \
  STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-session
[[ "$RC" -eq 0 ]] || { printf '✗ kill-session で exit %s\n' "$RC"; exit 1; }
assert_not_called "save quiet" "複数セッション時の kill-session は保存しない"
grep -qE 'kill-cmd cmd=kill-session pid=[0-9]+ sessions=3 save=skipped' "$LOG" \
  || { printf '✗ kill-session の記録が無い/書式相違:\n'; cat "$LOG"; exit 1; }
printf '✓ kill-session (残 3) は記録のみで保存 skip\n'

# --- (c) 最後の 1 セッションへの kill-session → exit-empty でサーバごと死ぬので保存 ----
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" TT_KILL_SAVE_WAIT_SECONDS=2 STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='solo: 1 windows\n' \
  STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-session
assert_called "save quiet" "最後の 1 セッションへの kill-session は直前保存する"

# --- (d) default socket 以外 → 完全 no-op (ログも保存も無し) ---------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_SOCKET_PATH="/nowhere/tmux-501/testsock" \
  STUB_SESSIONS='a: 1 windows\n' STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-server
[[ "$RC" -eq 0 ]] || { printf '✗ 非 default socket で exit %s (0 のはず)\n' "$RC"; exit 1; }
assert_not_called "save quiet" "非 default socket では保存しない"
[[ ! -s "$LOG" ]] || { printf '✗ 非 default socket でログが書かれた:\n'; cat "$LOG"; exit 1; }
printf '✓ 非 default socket (テストサーバ) では完全 no-op\n'

# --- (d2) 発行元の同定がフルパス起動でも効く -------------------------------------------
# `awk '$2 == "tmux"'` の厳密一致だとフルパス起動 (/opt/homebrew/bin/tmux kill-server。
# スクリプト経由では普通) を取り逃し、装置の主目的が not-found になる (2026-07-30 検出)。
# 実プロセスを立てて find_issuers の basename 判定を検証する。
# ps の command 第 1 語がフルパスの ".../tmux"、かつ argv に kill-server を含む実プロセスを作る。
# exec -a で argv[0] を差し替え、本体は sh -c で待たせる。
# ⚠️ スクリプト部を `'sleep 300'` 単体にしないこと: sh が sleep へ exec 最適化して argv[0] が
#    置き換わり、フルパスの偽装が消える (実測)。`; :` を付けて複合コマンドにすると sh が残る。
#    kill-server は $0 の位置に置く (sleep の引数にすると即死する)。
#    EXIT trap を落としてから exec する理由は test_periodic_save.sh 冒頭の注記と同じ。
reset_calls; : > "$LOG"
FAKE_TMUX_PATH="$TMP_DIR/fakebin/tmux"
mkdir -p "$TMP_DIR/fakebin"
# ⚠️ 補助プロセスの stdout/stderr は必ず /dev/null へ落とすこと。テストの stdout を継承させると
#    テスト本体が終わってもパイプが閉じず、呼び出し側 (make test / CI) が EOF を待ってハングする
#    (2026-07-30 に実際に踏んだ。テストは pass していたのに終わらなく見えた)。
( trap - EXIT; exec -a "$FAKE_TMUX_PATH" /bin/sh -c 'sleep 300; :' kill-server ) >/dev/null 2>&1 &
FULLPATH_PID=$!
HELPER_PIDS+=("$FULLPATH_PID")
sleep 0.3
# ⚠️ `ps | grep -q` のパイプにしないこと: grep -q が一致で即 exit して ps に SIGPIPE(141) を
#    返し、set -o pipefail がそれを拾って「一致したのに失敗」になる (guards.sh の
#    tt_only_hold_sessions が同じ罠を文書化している)。ps の出力を変数に取って here-string で渡す。
PS_SNAPSHOT="$(ps -axo pid=,command=)"
grep -q "^ *$FULLPATH_PID $FAKE_TMUX_PATH " <<<"$PS_SNAPSHOT" \
  || { printf '✗ テスト前提が崩れている (フルパス argv[0] のプロセスを作れていない):\n'; grep "$FULLPATH_PID" <<<"$PS_SNAPSHOT" | head -2; exit 1; }
TT_TRIGGER_LOG="$LOG" TT_KILL_SAVE_WAIT_SECONDS=2 STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='a: 1 windows\nb: 1 windows\n' STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-server
# 子プロセス (sh 配下の sleep) も落とす。親だけ殺すと sleep が ppid=1 の孤児として 5 分残り、
# テスト実行ごとに 1 個ずつ蓄積する (レビュー指摘 2026-07-30)
for child in $(pgrep -P "$FULLPATH_PID" 2>/dev/null); do kill "$child" 2>/dev/null || true; done
kill "$FULLPATH_PID" 2>/dev/null; wait "$FULLPATH_PID" 2>/dev/null || true
if grep -q "issuer=not-found" "$LOG"; then
  printf '✗ フルパス起動の発行元を取り逃した (issuer=not-found):\n'; cat "$LOG"; exit 1
fi
grep -q "issuer=${FULLPATH_PID}:" "$LOG" \
  || { printf '✗ フルパス起動の pid %s が issuer に出ない:\n' "$FULLPATH_PID"; cat "$LOG"; exit 1; }
printf '✓ フルパス起動 (/path/to/tmux) の発行元も basename 判定で捕まえる\n'

# --- (d3) 略記 subcommand の発行元も捕まえる --------------------------------------------
# tmux は曖昧でない前方一致を受理するため発行元の argv は `kill-sessio` 等になりうる。
# 正式名で完全一致すると取り逃す (本番 e2e で issuer=not-found を実測 2026-07-30)。
reset_calls; : > "$LOG"
( trap - EXIT; exec -a "$FAKE_TMUX_PATH" /bin/sh -c 'sleep 300; :' kill-sessio ) >/dev/null 2>&1 &
ABBREV_PID=$!
HELPER_PIDS+=("$ABBREV_PID")
sleep 0.3
TT_TRIGGER_LOG="$LOG" STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='a: 1 windows\nb: 1 windows\n' STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-session
for child in $(pgrep -P "$ABBREV_PID" 2>/dev/null); do kill "$child" 2>/dev/null || true; done
kill "$ABBREV_PID" 2>/dev/null; wait "$ABBREV_PID" 2>/dev/null || true
grep -q "issuer=${ABBREV_PID}:" "$LOG" \
  || { printf '✗ 略記 subcommand (kill-sessio) の発行元を取り逃した:\n'; cat "$LOG"; exit 1; }
printf '✓ 略記 subcommand (kill-sessio) の発行元も捕まえる\n'

# --- (e) 保存スクリプト未解決でも kill を阻害しない ------------------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='a: 1 windows\n' STUB_SAVE_SCRIPT="" \
  run "$STUB_PATH" "$SCRIPT" kill-server
[[ "$RC" -eq 0 ]] || { printf '✗ 保存スクリプト不在で exit %s (0 のはず)\n' "$RC"; exit 1; }
grep -q 'save=no-save-script' "$LOG" \
  || { printf '✗ save=no-save-script が記録されない:\n'; cat "$LOG"; exit 1; }
printf '✓ 保存スクリプト不在でも exit 0 + save=no-save-script を記録\n'

printf '\nAll log-kill-command tests passed successfully!\n'
