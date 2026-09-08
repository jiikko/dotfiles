#!/usr/bin/env bash
# scripts/tmux_restore_runner.sh (手動復元の detach 実行体) の unit テスト。
#
# runner の存在理由は「復元の途中死を silent にしない」(2026-07-30 の 22/29 部分復元が
# 完全に無記録だった)。よって pin するのは:
#   (1) 正常完了 (post-restore-all 到達 = @tt-restore-complete=1) → restore-end を記録
#   (2) 途中死 (complete 未設定) → @tt-restore-in-progress を掃除し restore-aborted を記録
#   (3) restore.sh 未解決 → restore-aborted reason=no-restore-script
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_restore_runner.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin"

cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$*" in
  *@resurrect-restore-script-path*) printf '%s\n' "${STUB_RESTORE:-}" ;;
  *"show -gqv @tt-restore-complete"*) printf '%s\n' "${STUB_COMPLETE:-}" ;;
  *"show -gqv @resurrect-capture-pane-contents"*) printf '%s\n' "${STUB_CAPTURE:-}" ;;
  *"show -gqv @resurrect-dir"*) printf '%s\n' "${STUB_RESURRECT_DIR:-}" ;;
esac
EOS
chmod +x "$TMP_DIR/bin/tmux"

cat > "$TMP_DIR/bin/fake_restore.sh" <<'EOS'
#!/bin/sh
echo "restore ran" >> "$CALLS"
EOS
chmod +x "$TMP_DIR/bin/fake_restore.sh"

STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
LOG="$TMP_DIR/trigger.log"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
# owner は production と同じ書式で作る (書式をテスト側へ写すと、書式変更に追従できない fixture になる)
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$ROOT_DIR/scripts/lib/tmux_resurrect_guards.sh"

LIVE_PIDS=()
spawn_live() { ( trap - EXIT; exec sleep 300 ) & LIVE_PIDS+=("$!"); REPLY_PID="$!"; }
cleanup_all() { local p; for p in ${LIVE_PIDS+"${LIVE_PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done; rm -rf "$TMP_DIR"; }
trap cleanup_all EXIT

# --- (1) 正常完了 -----------------------------------------------------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
  run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 正常系で exit %s\n' "$RC"; exit 1; }
assert_called "restore ran" "restore.sh が実行される"
grep -qE '	restore-manual-begin epoch=[0-9]+' "$LOG" || { printf '✗ restore-manual-begin が無い:\n'; cat "$LOG"; exit 1; }
grep -qE '	restore-end rc=0 epoch=[0-9]+' "$LOG" || { printf '✗ restore-end が無い:\n'; cat "$LOG"; exit 1; }
assert_not_called "set-option -g @tt-restore-in-progress 0" "正常完了ではフラグ掃除しない (post hook の責務)"
printf '✓ 正常完了: begin/end が記録される\n'

# --- (2) 途中死 (complete 未設定のまま restore が返った) --------------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE="" \
  run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 途中死系で exit %s\n' "$RC"; exit 1; }
assert_called "set-option -g @tt-restore-in-progress 0" "途中死で in-progress フラグを掃除する"
grep -qE '	restore-aborted reason=rc-0 epoch=[0-9]+' "$LOG" \
  || { printf '✗ restore-aborted (reason=rc-N) が無い:\n'; cat "$LOG"; exit 1; }
printf '✓ 途中死: フラグ掃除 + restore-aborted を記録\n'

# --- (3) restore.sh 未解決 ---------------------------------------------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="" run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 未解決系で exit %s\n' "$RC"; exit 1; }
grep -q 'restore-aborted reason=no-restore-script' "$LOG" \
  || { printf '✗ no-restore-script が記録されない:\n'; cat "$LOG"; exit 1; }
assert_not_called "restore ran" "未解決時は何も実行しない"
printf '✓ restore.sh 未解決: 記録のみで無害終了\n'

# --- (4) 先任が実行中なら復元しない (tt_lock_acquire rc=1) -------------------------------
# 🚨 この rc 分岐は issue 078 の統合で新設したもので、**どのテストからも踏まれていなかった**
#   (敵対レビューの指摘)。`|| tt_lock_rc=$?` を `&& ...` に変える 1 文字のタイポで lock を
#   取らないまま restore.sh を走らせる = 二重復元になるが、旧 3 ケースは全部 green のまま通る。
reset_calls; : > "$LOG"
STATE="$TMP_DIR/rstate"; rm -rf "$STATE"; mkdir -p "$STATE/lock"
spawn_live; tt_lock_write_owner "$STATE/lock" "$REPLY_PID"
TT_TRIGGER_LOG="$LOG" TT_RESTORE_STATE_DIR="$STATE" \
  STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
  run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 先任生存時に exit %s\n' "$RC"; exit 1; }
assert_not_called "restore ran" "先任が実行中なら restore.sh を走らせない"
grep -q 'restore-skipped reason=already-running' "$LOG" \
  || { printf '✗ restore-skipped reason=already-running が無い:\n'; cat "$LOG"; exit 1; }
printf '✓ 先任が実行中: 復元せず skip を記録\n'

# --- (5) lock を取れないなら復元しない (tt_lock_acquire rc=2) ----------------------------
if [ "$(id -u)" = 0 ]; then
  printf '🚨 root では書き込み不可ディレクトリを作れないため lock 取得失敗のテストを skip した\n'
else
  reset_calls; : > "$LOG"
  RO_STATE="$TMP_DIR/rstate_ro"; rm -rf "$RO_STATE"; mkdir -p "$RO_STATE"; chmod 500 "$RO_STATE"
  TT_TRIGGER_LOG="$LOG" TT_RESTORE_STATE_DIR="$RO_STATE" \
    STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
    run "$STUB_PATH" "$SCRIPT"
  chmod 700 "$RO_STATE"
  [[ "$RC" -eq 0 ]] || { printf '✗ lock 取得失敗時に exit %s\n' "$RC"; exit 1; }
  assert_not_called "restore ran" "lock を取れないなら restore.sh を走らせない"
  grep -q 'restore-aborted reason=lock-failed' "$LOG" \
    || { printf '✗ restore-aborted reason=lock-failed が無い:\n'; cat "$LOG"; exit 1; }
  printf '✓ lock を取れない: 復元せず理由を記録\n'
fi

# --- (6) archive の完全性: capture-contents が on のときだけ記録する -----------------------
#
# 🚨 production (`tmux_restore_runner.sh`) は「壊れていても復元は続行するが記録は残す」契約。
# upstream は archive 展開の失敗を検証せず rc=0 で完走するので、この記録が無いと
# 「window は全部戻ったのに全 pane の scrollback が空」が完全に silent になる (実証 2026-07-30)。
# **それまでこの経路にテストが 1 本も無かった** (issue 316: 判定式を guards.sh へ寄せる前提として
# 「述語を false へ倒す変異で 2 スクリプトとも red」を確認しようとして発覚した)。
RDIR="$TMP_DIR/resurrect"; mkdir -p "$RDIR"
printf 'not a gzip' > "$RDIR/pane_contents.tar.gz"   # gzip -t が落ちる中身

# 🚨 **不在を assert するケースでは「そこへ到達したこと」を先に固定する**。archive チェックの
# 手前には早期 return が 4 本ある (既定サーバでない / 先任が実行中 / lock 失敗 / restore.sh 未解決)。
# 到達を見ないと、どれかで抜けた run が「記録しなかった = 正しい」に化ける。
assert_reached_archive_check() { # assert_reached_archive_check <ラベル>
  grep -qE '	restore-manual-begin epoch=[0-9]+' "$LOG" \
    || { printf '✗ %s: archive チェックへ到達していない (手前の早期 return で抜けた):\n' "$1"; cat "$LOG"; exit 1; }
}

reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
  STUB_CAPTURE=on STUB_RESURRECT_DIR="$RDIR" \
  run "$STUB_PATH" "$SCRIPT"
assert_reached_archive_check "壊れた archive (on)"
# 🚨 パスは固定文字列で照合する ($RDIR は mktemp -d 由来で `.` を含み、ERE だと任意 1 文字に化ける)
grep -qF "	restore-archive-broken path=$RDIR/pane_contents.tar.gz epoch=" "$LOG" \
  || { printf '✗ 壊れた archive を記録していない (capture-contents=on):\n'; cat "$LOG"; exit 1; }
grep -qE '	restore-archive-broken .* epoch=[0-9]+' "$LOG" \
  || { printf '✗ epoch が数字で入っていない:\n'; cat "$LOG"; exit 1; }
printf '✓ archive 破損: capture-contents=on なら restore-archive-broken を記録\n'

# 🚨 **健全な archive では鳴らない (positive control の裏)**。これが無いと
# `&& ! gzip -t "$tt_archive"` を丸ごと外す変異が緑で通り、**毎回「壊れている」と記録する**
# 狼少年になる (本物の破損がログに埋もれる)。
reset_calls; : > "$LOG"
printf 'ok' | gzip > "$RDIR/pane_contents.tar.gz"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
  STUB_CAPTURE=on STUB_RESURRECT_DIR="$RDIR" \
  run "$STUB_PATH" "$SCRIPT"
assert_reached_archive_check "健全な archive (on)"
grep -q 'restore-archive-broken' "$LOG" \
  && { printf '✗ 健全な archive なのに破損を記録した:\n'; cat "$LOG"; exit 1; }
printf '✓ archive 健全: capture-contents=on でも記録しない\n'

# 🚨 **archive が「無い」だけでは鳴らない**。これが無いと `[ -f "$tt_archive" ]` を外す変異が
# 緑で通り、capture を on にした直後 (まだ保存していない) を「壊れている」と記録する。
reset_calls; : > "$LOG"
rm -f "$RDIR/pane_contents.tar.gz"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
  STUB_CAPTURE=on STUB_RESURRECT_DIR="$RDIR" \
  run "$STUB_PATH" "$SCRIPT"
assert_reached_archive_check "archive なし (on)"
grep -q 'restore-archive-broken' "$LOG" \
  && { printf '✗ archive が無いだけなのに破損を記録した:\n'; cat "$LOG"; exit 1; }
printf '✓ archive なし: 破損としては記録しない\n'

# 🚨 **述語が効いていることの固定 (negative control)**。off のときも記録すると、
# 「pane 内容を保存していないのに『復元できない』と毎回言う」ノイズになる。
# これが無いと `tt_capture_contents_on` を常に true へ倒す変異が緑で通る。
reset_calls; : > "$LOG"
printf 'not a gzip' > "$RDIR/pane_contents.tar.gz"   # 壊れた中身に戻す
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
  STUB_CAPTURE=off STUB_RESURRECT_DIR="$RDIR" \
  run "$STUB_PATH" "$SCRIPT"
assert_reached_archive_check "壊れた archive (off)"
grep -q 'restore-archive-broken' "$LOG" \
  && { printf '✗ capture-contents=off なのに archive 破損を記録した:\n'; cat "$LOG"; exit 1; }
printf '✓ archive 破損: capture-contents=off なら記録しない\n'

printf '\nAll restore-runner tests passed successfully!\n'
