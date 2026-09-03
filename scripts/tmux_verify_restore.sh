#!/usr/bin/env bash
# 復元の「完全性」を検証する。保存ファイルに載っているセッションが実際に全部復元されたかを
# 突き合わせ、欠落があれば観測ログと (人向けに) toast へ出す。
#
# なぜ必要か (2026-07-30 の一次事実):
#   手動復元が 29 セッション中 22 で途中死したのに、誰も気づかなかった。復元プロセスは rc=0 で
#   完走扱い、観測ログも restore-start / restore-end の正常ペアだった。つまり
#   「復元プロセスが完走した」と「保存内容が全部復元された」を区別する仕組みが無かった。
#   restore-end rc=0 は「main が最後まで走った」しか意味しない。ここを埋めるのが本スクリプト。
#
# 出力: 観測ログへ 1 行
#   restore-verify saved=N live=M missing=K [missing_names=a,b,c] epoch=...
# 欠落があれば toast でも知らせる (フォーカスを奪わない)。
# 終了コード: 欠落 0 なら 0、欠落があれば 1 (呼び出し側は無視してよい。記録が主目的)。
#
# 呼び出し元: scripts/tmux_restore_runner.sh (手動復元の完了直後) と
#   _tmux.conf の @resurrect-hook-post-restore-all (自動復元も同じ検証を通すため)。
# default socket ゲートを通す (隔離テストサーバの復元を本番ログに混ぜない)。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"
TT_TOAST="${TT_TOAST:-$SCRIPT_DIR/../bin/tmux-toast}"
# 報告する欠落セッション名の上限 (ログ 1 行が長くなりすぎないように)
TT_VERIFY_NAME_LIMIT="${TT_VERIFY_NAME_LIMIT:-8}"

tt_on_default_server || exit 0


rdir="$(tt_resurrect_dir)"
last_target="$(readlink "$rdir/last" 2>/dev/null || true)"
if [ -z "$last_target" ] || [ ! -f "$rdir/$last_target" ]; then
  tt_trigger_log "restore-verify skipped=no-last epoch=$(date +%s)"
  exit 0
fi

# 保存ファイルの pane 行 (field2 = セッション名) から保存済みセッション集合を作る
saved_names="$(awk -F'\t' '$1=="pane"{print $2}' "$rdir/$last_target" 2>/dev/null | sort -u)"
saved="$(printf '%s\n' "$saved_names" | grep -c . || true)"

# 実セッション (hold は復元対象ではないので除外)
live_names="$(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep -v "^${TT_HOLD_PREFIX}" | sort -u || true)"
live="$(printf '%s\n' "$live_names" | grep -c . || true)"

# 保存にあって実在しないもの = 未復元
missing_names="$(comm -23 <(printf '%s\n' "$saved_names") <(printf '%s\n' "$live_names") 2>/dev/null || true)"
missing="$(printf '%s\n' "$missing_names" | grep -c . || true)"

if [ "${missing:-0}" -eq 0 ]; then
  tt_trigger_log "restore-verify saved=$saved live=$live missing=0 epoch=$(date +%s)"
  exit 0
fi

names="$(printf '%s\n' "$missing_names" | head -"$TT_VERIFY_NAME_LIMIT" | paste -sd, - 2>/dev/null)"
[ "$missing" -gt "$TT_VERIFY_NAME_LIMIT" ] && names="$names,…"
tt_trigger_log "restore-verify saved=$saved live=$live missing=$missing missing_names=$names epoch=$(date +%s)"

# 人にも見せる (これが無いと「完走したのに部分復元」に気づけない = 今日の 22/29)
[ -x "$TT_TOAST" ] && "$TT_TOAST" -d 8 -b 52 \
  "🚨 復元が不完全: 保存 $saved 中 $missing セッション未復元 ($names)" 2>/dev/null || true

exit 1
