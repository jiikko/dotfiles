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

# ---- wrapper の復元中ガード (tt_save_restore_in_progress) ----
# choke point 検査の回帰テスト: 復元中 (TTL 内 epoch) は保存を止め、
# 降り損ねの stale フラグ (TTL 超過) では保存を再開する (全経路凍結の防止)。
GUARD_OUT="$(
  HOME="$TMP_HOME" TT_SAVE_STATE_DIR="$TMP_HOME/state_guard" TT_SAVE_SOURCE_ONLY=1 \
  bash -c '
    SCRIPT="'"$SCRIPT"'"
    tmux() {
      case "$1 $2 $3" in
        "show -gqv @tt-restore-in-progress") printf "%s\n" "$_T_INPROGRESS" ;;
        *) : ;;
      esac
      return 0
    }
    source "$SCRIPT"
    _T_INPROGRESS="$(date +%s)"   # 直近 epoch → 復元中
    if tt_save_restore_in_progress; then printf "CASE:g_inprogress guard=block\n"; else printf "CASE:g_inprogress guard=pass\n"; fi
    _T_INPROGRESS="1"             # epoch=1 (1970) → TTL 超過の降り損ね → 保存再開
    if tt_save_restore_in_progress; then printf "CASE:g_stale guard=block\n"; else printf "CASE:g_stale guard=pass\n"; fi
    _T_INPROGRESS=""              # クリア済み → 保存可
    if tt_save_restore_in_progress; then printf "CASE:g_clear guard=block\n"; else printf "CASE:g_clear guard=pass\n"; fi
    _T_INPROGRESS="0"             # post-restore-all が降ろした後 → 保存可
    if tt_save_restore_in_progress; then printf "CASE:g_zero guard=block\n"; else printf "CASE:g_zero guard=pass\n"; fi
  ' 2>/dev/null
)"
OUT="$OUT
$GUARD_OUT"

# ---- Fix B2: pane_contents.tar.gz の退避 (退行時に last と一緒に戻す) ----
FIXB_RDIR="$TMP_HOME/rdir"; mkdir -p "$FIXB_RDIR"
FAKE_REALSAVE="$TMP_HOME/fake_realsave.sh"
cat > "$FAKE_REALSAVE" <<'FS'
#!/bin/sh
# 退行を模す fake save: .fake_n セッションの new.txt を作って last を差し替え、
# 共有 pane_contents.tar.gz を退行後の内容 (DEGRADED) で上書きする。
n=$(cat "$RDIR/.fake_n" 2>/dev/null || echo 0)
: > "$RDIR/new.txt"
i=1
while [ "$i" -le "$n" ]; do printf 'window\tn%s\tx\n' "$i" >> "$RDIR/new.txt"; i=$((i+1)); done
ln -sf new.txt "$RDIR/last"
printf 'DEGRADED' > "$RDIR/pane_contents.tar.gz"
FS
chmod +x "$FAKE_REALSAVE"

FIXB_OUT="$(
  HOME="$TMP_HOME" TT_SAVE_STATE_DIR="$TMP_HOME/state_fixb" TT_SAVE_SOURCE_ONLY=1 \
  TT_REAL_SAVE_SCRIPT="$FAKE_REALSAVE" RDIR="$FIXB_RDIR" \
  bash -c '
    export RDIR
    tmux() {
      case "$1 $2 $3" in
        "show -gqv @resurrect-dir") printf "%s\n" "$RDIR" ;;
        *) printf "\n" ;;   # restore-in-progress 等は空 = 非復元中
      esac
      return 0
    }
    source "'"$SCRIPT"'"
    make_prev() {   # 退行前: 7 セッション + ORIGINAL archive (7→2 は 6<=7 で退行、7→4 は非退行)
      : > "$RDIR/prev.txt"
      for ss in s1 s2 s3 s4 s5 s6 s7; do printf "window\t%s\tx\n" "$ss" >> "$RDIR/prev.txt"; done
      ln -sf prev.txt "$RDIR/last"
      printf "ORIGINAL" > "$RDIR/pane_contents.tar.gz"
    }
    bakcount() { ls "$RDIR"/.pane_contents.ttguard.* 2>/dev/null | wc -l | tr -d " "; }

    # 退行 (5→2): last も archive も退行前へ戻り、退避ファイルは残らない
    make_prev; printf 2 > "$RDIR/.fake_n"; ( tt_save_main quiet )
    printf "CASE:fixb_regress last=%s archive=%s bak=%s\n" "$(readlink "$RDIR/last")" "$(cat "$RDIR/pane_contents.tar.gz")" "$(bakcount)"

    # 非退行 (5→4): last も archive も新しいまま、退避ファイルは掃除される
    make_prev; printf 4 > "$RDIR/.fake_n"; ( tt_save_main quiet )
    printf "CASE:fixb_ok last=%s archive=%s bak=%s\n" "$(readlink "$RDIR/last")" "$(cat "$RDIR/pane_contents.tar.gz")" "$(bakcount)"
  ' 2>/dev/null
)"
OUT="$OUT
$FIXB_OUT"

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

printf '\n## wrapper の復元中ガード (choke point 検査)\n'
assert_eq_line g_inprogress "guard=block" "復元中 (直近 epoch) は wrapper が保存を止める"
assert_eq_line g_stale      "guard=pass"  "TTL 超過の降り損ねフラグでは保存を再開する (全経路凍結の防止)"
assert_eq_line g_clear      "guard=pass"  "フラグ未設定は保存可"
assert_eq_line g_zero       "guard=pass"  "post-restore-all がクリアした後 (0) は保存可"

printf '\n## Fix B2: pane_contents 退避 (退行時に last と一緒に戻す)\n'
assert_eq_line fixb_regress "last=prev.txt archive=ORIGINAL bak=0" "退行時: last も pane_contents も退行前へ戻し、退避ファイルを残さない"
assert_eq_line fixb_ok      "last=new.txt archive=DEGRADED bak=0"  "非退行時: last も archive も新しいまま、退避ファイルは掃除する"

printf '\nAll resurrect-save lock tests passed successfully!\n'
