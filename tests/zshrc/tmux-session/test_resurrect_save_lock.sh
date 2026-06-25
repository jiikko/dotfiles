#!/usr/bin/env bash
# scripts/tmux_resurrect_save.sh の lock stale 判定 (tt_save_owner_is_stale) の unit テスト。
#
# 実際の保存 (upstream save.sh) や lock 競合は実環境でしか確認できないため、ここでは
# 「取り残し lock を正しく取り残しと判定し、進行中の正当な lock は奪わない」という
# 直列化の不変条件だけを検証する。owner は「PID + 起動時刻」で同定する (tt_save_owner_is_stale):
#   - owner PID 生存 + 起動時刻一致 + mtime 新しい  → 進行中 (待つ)
#   - owner PID 生存 + 起動時刻一致 + mtime 過去     → 進行中 (待つ) … 長時間保存を奪わない (codex P1)
#   - owner PID 生存 + 起動時刻不一致 (PID 再利用)   → 取り残し (解除可) … 永久残置を防ぐ
#   - owner PID 死亡                                → 取り残し (解除可)
#   - PID 不明 + mtime 新しい                       → 進行中 (待つ)
#   - PID 不明 + mtime soft TTL 超過                → 取り残し (解除可)
#   - lock dir 不在                                 → 取り残しでない
#
# スクリプトは TT_SAVE_SOURCE_ONLY=1 で source すると本体 (tt_save_main) を実行しないので、
# 関数を直接呼んで検証する (debounced-save / tt の unit テストと同方式)。
#
# mtime のバックデートは固定の過去日付 (touch -t) で行い、date 演算の BSD/GNU 差を避ける
# (CI は ubuntu-slim)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_resurrect_save.sh"
TMP_HOME="$(mktemp -d)"
cleanup() { rm -rf "$TMP_HOME"; }
trap cleanup EXIT

if [[ ! -x "$SCRIPT" ]]; then
  printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"
  exit 1
fi
printf '✓ %s exists (executable)\n' "${SCRIPT#$ROOT_DIR/}"

# 全ケースを 1 つの bash サブシェルで走らせ、"CASE:<id> ..." を 1 行ずつ出力する。
OUT="$(
  HOME="$TMP_HOME" \
  TT_SAVE_STATE_DIR="$TMP_HOME/state" \
  TT_SAVE_SOURCE_ONLY=1 \
  bash -c '
    SCRIPT="'"$SCRIPT"'"
    source "$SCRIPT"   # 本体は実行されず関数定義だけ読み込まれる

    LOCK="$TT_SAVE_LOCK_DIR"
    ANCIENT="200001010000"   # 2000-01-01: soft TTL を確実に超える過去日付

    # 生存 PID: 自分自身 (このサブシェル) を使う。kill -0 が必ず真。
    ALIVE=$$
    ALIVE_START="$(tt_save_proc_starttime "$ALIVE")"   # 記録と一致させる正しい起動時刻
    # ps -o lstart= が使えるか（PID 再利用検出はこれに依存。使えない環境は fail-safe に縮退）。
    if [ -n "$ALIVE_START" ]; then printf "CASE:lstart val=yes\n"; else printf "CASE:lstart val=no\n"; fi
    # 死亡 PID: 起動直後に終了させた子の PID。テストは <1s で終わるため再利用はまず起きない。
    sleep 100 & DEAD=$!; kill "$DEAD" 2>/dev/null; wait "$DEAD" 2>/dev/null || true

    reset_lock() { rm -rf "$LOCK"; mkdir -p "$LOCK"; }   # mtime=現在 (新しい)
    # ループと同様に owner 行を pid から一度読んで判定関数へ渡す。
    is_stale() { if tt_save_owner_is_stale "$(cat "$LOCK/pid" 2>/dev/null || true)"; then printf yes; else printf no; fi; }

    # lock dir 不在
    rm -rf "$LOCK"
    printf "CASE:no_lock stale=%s\n" "$(is_stale)"

    # owner 生存 + 起動時刻一致 + mtime 新しい → 進行中 (待つ)
    reset_lock; printf "%s %s\n" "$ALIVE" "$ALIVE_START" > "$LOCK/pid"
    printf "CASE:alive_fresh stale=%s\n" "$(is_stale)"

    # owner 生存 + 起動時刻一致 + mtime 過去 → 進行中 (待つ)。長時間保存を奪わない (codex P1 回帰防止)
    reset_lock; printf "%s %s\n" "$ALIVE" "$ALIVE_START" > "$LOCK/pid"; touch -t "$ANCIENT" "$LOCK"
    printf "CASE:alive_long stale=%s\n" "$(is_stale)"

    # owner 生存 + 起動時刻不一致 (PID 再利用 / 再起動跨ぎ) → 取り残し
    reset_lock; printf "%s %s\n" "$ALIVE" "STALE_FROM_PREVIOUS_BOOT" > "$LOCK/pid"
    printf "CASE:alive_reused stale=%s\n" "$(is_stale)"

    # owner 死亡 → 取り残し
    reset_lock; printf "%s %s\n" "$DEAD" "$ALIVE_START" > "$LOCK/pid"
    printf "CASE:dead stale=%s\n" "$(is_stale)"

    # 旧形式 (PID のみ・起動時刻なし) + 生存 + fresh mtime → 進行中 (hard TTL 内, 奪わない)
    reset_lock; printf "%s\n" "$ALIVE" > "$LOCK/pid"
    printf "CASE:legacy_fresh stale=%s\n" "$(is_stale)"

    # 旧形式 + 生存 + hard TTL 超過 mtime → 取り残し (起動時刻で同定できない時の PID 再利用回復)
    reset_lock; printf "%s\n" "$ALIVE" > "$LOCK/pid"; touch -t "$ANCIENT" "$LOCK"
    printf "CASE:legacy_ancient stale=%s\n" "$(is_stale)"

    # PID 不明 (pid ファイル無し) + mtime 新しい → 進行中 (待つ)
    reset_lock
    printf "CASE:nopid_fresh stale=%s\n" "$(is_stale)"

    # PID 不明 + mtime soft TTL 超過 → 取り残し
    reset_lock; touch -t "$ANCIENT" "$LOCK"
    printf "CASE:nopid_ancient stale=%s\n" "$(is_stale)"

    ##########################################################################
    # conditional release: owner 一致時のみ解除（別プロセスの新 lock を消さない）
    ##########################################################################
    reset_lock; printf "%s %s\n" "$ALIVE" "$ALIVE_START" > "$LOCK/pid"
    tt_save_release_lock_if_owner "$ALIVE $ALIVE_START"   # 一致 → 解除される
    if [ -d "$LOCK" ]; then printf "CASE:rel_match removed=no\n"; else printf "CASE:rel_match removed=yes\n"; fi

    reset_lock; printf "%s %s\n" "$ALIVE" "$ALIVE_START" > "$LOCK/pid"
    tt_save_release_lock_if_owner "99999 OTHER_OWNER"     # 不一致 → 解除されない
    if [ -d "$LOCK" ]; then printf "CASE:rel_mismatch removed=no\n"; else printf "CASE:rel_mismatch removed=yes\n"; fi
  ' 2>/dev/null
)"

# ---- 検証 -------------------------------------------------------------------
case_line() { printf '%s\n' "$OUT" | grep "CASE:$1 " || true; }

assert_eq_line() {
  local id="$1" expect="$2" msg="$3" line
  line="$(case_line "$id")"
  if [[ "$line" != "CASE:$id $expect" ]]; then
    printf '✗ %s\n  expected: CASE:%s %s\n  actual:   %s\n' "$msg" "$id" "$expect" "$line"
    exit 1
  fi
  printf '✓ %s\n' "$msg"
}

printf '\n## lock stale 判定 (保存直列化の不変条件)\n'
assert_eq_line no_lock       "stale=no"  "lock dir が無ければ取り残しでない"
assert_eq_line alive_fresh   "stale=no"  "owner 生存 + 新しい mtime は進行中 (奪わない)"
# 起動時刻 (ps -o lstart=) が取れるかで、長時間保存の保護と PID 再利用検出の挙動が変わる:
#   取れる   → 起動時刻で厳密同定 (一致=奪わない / 不一致=取り残し)
#   取れない → 起動時刻で同定できず mtime hard TTL に縮退 (新しい=待つ / 過去=取り残し)
if [[ "$(case_line lstart)" == "CASE:lstart val=yes" ]]; then
  assert_eq_line alive_long   "stale=no"  "起動時刻一致なら mtime 過去でも奪わない (長時間保存を保護)"
  assert_eq_line alive_reused "stale=yes" "起動時刻不一致 (PID 再利用) は取り残し"
else
  assert_eq_line alive_long   "stale=yes" "ps -o lstart= 非対応: 起動時刻同定不可で hard TTL 超過 → 取り残し"
  assert_eq_line alive_reused "stale=no"  "ps -o lstart= 非対応: 再利用を識別できず生存 owner を尊重 (fail-safe)"
fi
assert_eq_line dead          "stale=yes" "owner 死亡なら取り残し"
assert_eq_line legacy_fresh   "stale=no"  "旧形式(起動時刻なし)+生存+新しい mtime は進行中 (hard TTL 内)"
assert_eq_line legacy_ancient "stale=yes" "旧形式(起動時刻なし)+生存でも hard TTL 超過なら取り残し (再利用回復)"
assert_eq_line nopid_fresh   "stale=no"  "PID 不明 + 新しい mtime は進行中 (待つ)"
assert_eq_line nopid_ancient "stale=yes" "PID 不明 + soft TTL 超過なら取り残し"

printf '\n## conditional release (owner 一致時のみ解除)\n'
assert_eq_line rel_match    "removed=yes" "owner 一致なら lock を解除する"
assert_eq_line rel_mismatch "removed=no"  "owner 不一致なら lock を解除しない (新 owner の lock を消さない)"

printf '\nAll resurrect-save lock tests passed successfully!\n'
