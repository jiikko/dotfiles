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
# .fake_fail があれば「archive 生成中の死」を模す: archive を truncate 相当の
# 中途半端な内容にしてから rc≠0 で死ぬ (last は前進済みでも未でもよいので触らない)。
if [ -f "$RDIR/.fake_fail" ]; then
  printf 'TRUNCATED' > "$RDIR/pane_contents.tar.gz"
  exit 1
fi
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

    # 全喪失 (2→0): prev がしきい値未満 (セッション 4 未満 / window 8 未満) でも、
    # window 0 件の保存は無条件で退行扱いにして last も archive も戻す
    # (サーバ終了レースの空保存で last が空になる実観測 2026-07-04 の回帰防止)
    make_prev_small() {
      : > "$RDIR/prev.txt"
      for ss in s1 s2; do printf "window\t%s\tx\n" "$ss" >> "$RDIR/prev.txt"; done
      ln -sf prev.txt "$RDIR/last"
      printf "ORIGINAL" > "$RDIR/pane_contents.tar.gz"
    }
    make_prev_small; printf 0 > "$RDIR/.fake_n"; ( tt_save_main quiet )
    printf "CASE:fixb_zero_small last=%s archive=%s bak=%s\n" "$(readlink "$RDIR/last")" "$(cat "$RDIR/pane_contents.tar.gz")" "$(bakcount)"

    # save 失敗 (rc≠0): archive 生成中の死で truncate された共有 archive をバックアップから
    # 書き戻す (旧実装は rc≠0 だと Fix B 復元ブロックを丸ごと skip して無傷の退避を捨てて
    # いた)。rc は上流の失敗をそのまま呼び出し側へ返す。
    make_prev; : > "$RDIR/.fake_fail"; ( tt_save_main quiet ); frc=$?
    rm -f "$RDIR/.fake_fail"
    printf "CASE:fixb_fail rc=%s archive=%s bak=%s\n" "$frc" "$(cat "$RDIR/pane_contents.tar.gz")" "$(bakcount)"
  ' 2>/dev/null
)"
OUT="$OUT
$FIXB_OUT"

# ---- Fix B (window 軸): 1 セッション × 多 window の壊滅退行も検知する ----
FIXW_RDIR="$TMP_HOME/rdir_w"; mkdir -p "$FIXW_RDIR"
FAKE_REALSAVE_W="$TMP_HOME/fake_realsave_w.sh"
cat > "$FAKE_REALSAVE_W" <<'FS'
#!/bin/sh
# 単一セッション s1 に .fake_w 個の window を持つ保存を作り last を差し替える
w=$(cat "$RDIR/.fake_w" 2>/dev/null || echo 0)
: > "$RDIR/new.txt"
i=1
while [ "$i" -le "$w" ]; do printf 'window\ts1\tw%s\n' "$i" >> "$RDIR/new.txt"; i=$((i+1)); done
ln -sf new.txt "$RDIR/last"
FS
chmod +x "$FAKE_REALSAVE_W"

FIXW_OUT="$(
  HOME="$TMP_HOME" TT_SAVE_STATE_DIR="$TMP_HOME/state_fixw" TT_SAVE_SOURCE_ONLY=1 \
  TT_REAL_SAVE_SCRIPT="$FAKE_REALSAVE_W" RDIR="$FIXW_RDIR" \
  bash -c '
    export RDIR
    tmux() {
      case "$1 $2 $3" in
        "show -gqv @resurrect-dir") printf "%s\n" "$RDIR" ;;
        *) printf "\n" ;;
      esac
      return 0
    }
    source "'"$SCRIPT"'"
    make_prev_w() {   # 退行前: 1 セッション × 12 window (セッション数では退行を検知できない形)
      : > "$RDIR/prev.txt"
      i=1
      while [ "$i" -le 12 ]; do printf "window\ts1\tw%s\n" "$i" >> "$RDIR/prev.txt"; i=$((i+1)); done
      ln -sf prev.txt "$RDIR/last"
    }

    # window 壊滅 (12→2): セッション数は 1→1 で不変でも last を退行前へ戻す
    make_prev_w; printf 2 > "$RDIR/.fake_w"; ( tt_save_main quiet )
    printf "CASE:fixw_regress last=%s\n" "$(readlink "$RDIR/last")"

    # 通常の window 減 (12→8): 1/3 以下ではないので新保存を採用する
    make_prev_w; printf 8 > "$RDIR/.fake_w"; ( tt_save_main quiet )
    printf "CASE:fixw_ok last=%s\n" "$(readlink "$RDIR/last")"

    make_prev_w6() {   # しきい値未満の prev: 1 セッション × 6 window
      : > "$RDIR/prev.txt"
      i=1
      while [ "$i" -le 6 ]; do printf "window\ts1\tw%s\n" "$i" >> "$RDIR/prev.txt"; i=$((i+1)); done
      ln -sf prev.txt "$RDIR/last"
    }

    # 全喪失 (6→0): prev がしきい値未満でも window 0 件は無条件に退行扱い
    make_prev_w6; printf 0 > "$RDIR/.fake_w"; ( tt_save_main quiet )
    printf "CASE:fixw_zero_small last=%s\n" "$(readlink "$RDIR/last")"

    # 部分喪失 (6→2): prev がしきい値未満 かつ 全喪失でもない → 正当な kill と
    # 区別できないので新保存を採用する (保守的しきい値の現状仕様を pin する)
    make_prev_w6; printf 2 > "$RDIR/.fake_w"; ( tt_save_main quiet )
    printf "CASE:fixw_partial_small last=%s\n" "$(readlink "$RDIR/last")"

    # 退避コピーの残置 GC: 死亡 pid の .pane_contents.ttguard.* は保存時に掃除され、
    # 生存 pid のものは残す (誤って進行中の退避を消さない)
    sleep 100 & GCDEAD=$!; kill "$GCDEAD" 2>/dev/null; wait "$GCDEAD" 2>/dev/null || true
    printf x > "$RDIR/.pane_contents.ttguard.$GCDEAD.tar.gz"
    printf x > "$RDIR/.pane_contents.ttguard.$$.tar.gz"
    make_prev_w6; printf 6 > "$RDIR/.fake_w"; ( tt_save_main quiet )
    dead_left=no; [ -f "$RDIR/.pane_contents.ttguard.$GCDEAD.tar.gz" ] && dead_left=yes
    live_left=no; [ -f "$RDIR/.pane_contents.ttguard.$$.tar.gz" ] && live_left=yes
    printf "CASE:fixw_gc dead=%s live=%s\n" "$dead_left" "$live_left"
  ' 2>/dev/null
)"
OUT="$OUT
$FIXW_OUT"

# ---- 同一秒再保存ガード (tt_save_avoid_same_second_target) ----
# upstream save.sh は秒精度ファイル名 + 「差分なしなら rm」のため、直前保存と同一秒の
# 再保存は last の実体を truncate → rm して dangling にする。wrapper は last の実体名が
# 現在秒と一致するときだけ次の秒まで待つ。date を固定スタブにして決定的に検証する。
SS_RDIR="$TMP_HOME/rdir_ss"; mkdir -p "$SS_RDIR"
SS_OUT="$(
  HOME="$TMP_HOME" TT_SAVE_STATE_DIR="$TMP_HOME/state_ss" TT_SAVE_SOURCE_ONLY=1 \
  RDIR="$SS_RDIR" \
  bash -c '
    export RDIR
    tmux() {
      case "$1 $2 $3" in
        "show -gqv @resurrect-dir") printf "%s\n" "$RDIR" ;;
        *) printf "\n" ;;
      esac
      return 0
    }
    source "'"$SCRIPT"'"
    date() { printf "20990101T000000\n"; }        # 「現在秒」を固定 (レース排除)
    slept=0; sleep() { slept=$((slept+1)); }      # 待ちをカウントに置換

    ln -sf "tmux_resurrect_20990101T000000.txt" "$RDIR/last"   # 現在秒と同名 → 待つ
    slept=0; tt_save_avoid_same_second_target
    printf "CASE:ss_same slept=%s\n" "$slept"

    ln -sf "tmux_resurrect_20000101T000000.txt" "$RDIR/last"   # 過去秒 → 待たない
    slept=0; tt_save_avoid_same_second_target
    printf "CASE:ss_old slept=%s\n" "$slept"

    rm -f "$RDIR/last"                                          # last 無し → 待たない
    slept=0; tt_save_avoid_same_second_target
    printf "CASE:ss_nolast slept=%s\n" "$slept"
  ' 2>/dev/null
)"
OUT="$OUT
$SS_OUT"

# ---- wrapper choke-point 統合ガード + lock 取得ループ ----
# 7e8b811 で tt_save_main に足した「hold のみ (bootstrap) / 第 2 サーバは弾く」統合と、
# bounded-wait lock の「生存 owner は奪わず保存せず非 0 / dead owner は横取りして保存」を
# tt_save_main ごと検証する (ガード関数単体は debounce 側テストが担うが、wrapper が
# 実際に呼ぶ配線はここでしか守れない)。
WRAP_RDIR="$TMP_HOME/rdir_wrap"; mkdir -p "$WRAP_RDIR"
WRAP_TRACE="$TMP_HOME/wrap_trace.log"
FAKE_REALSAVE_MARK="$TMP_HOME/fake_realsave_mark.sh"
cat > "$FAKE_REALSAVE_MARK" <<'FS'
#!/bin/sh
echo ran >> "$RDIR/save_runs"
FS
chmod +x "$FAKE_REALSAVE_MARK"

WRAP_OUT="$(
  HOME="$TMP_HOME" TT_SAVE_STATE_DIR="$TMP_HOME/state_wrap" TT_SAVE_SOURCE_ONLY=1 \
  TT_REAL_SAVE_SCRIPT="$FAKE_REALSAVE_MARK" RDIR="$WRAP_RDIR" \
  WRAP_TRACE="$WRAP_TRACE" \
  bash -c '
    export RDIR
    # CI でのみ稀に w_hold_with_real が rc=1 runs=0 で落ちる flake (2026-07-11, run 29129167971)
    # の観測用: どのガードで弾かれたかを xtrace で常時採取し、assert 失敗時だけダンプする
    # (assert_eq_line 参照)。原因特定までは外さないこと。
    exec 9>"$WRAP_TRACE"
    export BASH_XTRACEFD=9
    export PS4="+ \${FUNCNAME[0]:-main}:\${LINENO}: "
    set -x
    # スタブ状態: _T_SESSIONS (list-sessions の行群) / _T_SOCKET (socket_path)
    tmux() {
      case "$1 $2 $3" in
        "show -gqv @resurrect-dir") printf "%s\n" "$RDIR" ;;
        "list-sessions -F"*)        printf "%s\n" $_T_SESSIONS ;;
        "display-message -p #{socket_path}") printf "%s\n" "$_T_SOCKET" ;;
        *) printf "\n" ;;
      esac
      return 0
    }
    source "'"$SCRIPT"'"
    runs()  { wc -l < "$RDIR/save_runs" 2>/dev/null | tr -d " "; }
    reset() { : > "$RDIR/save_runs"; rm -rf "$TT_SAVE_LOCK_DIR"; }

    # hold のみ (bootstrap) → choke point で弾く (continuum 周期保存が hold のみの瞬間に
    # 発火して貧弱状態を保存する非対称の回帰防止)
    reset; _T_SESSIONS="__tt_hold_9"; _T_SOCKET=""
    ( tt_save_main quiet ); rc=$?
    printf "CASE:w_hold_only rc=%s runs=%s\n" "$rc" "$(runs)"

    # hold 残置でも実セッションがあれば保存する (永久抑止しない)
    reset; _T_SESSIONS="proj __tt_hold_9"
    ( tt_save_main quiet ); rc=$?
    printf "CASE:w_hold_with_real rc=%s runs=%s\n" "$rc" "$(runs)"

    # 第 2 サーバ (default 以外の socket) → 弾く / default socket → 保存する
    reset; _T_SESSIONS="proj"
    _T_SOCKET="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/exp"
    ( tt_save_main quiet ); rc=$?
    printf "CASE:w_second_server rc=%s runs=%s\n" "$rc" "$(runs)"
    reset
    _T_SOCKET="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"
    ( tt_save_main quiet ); rc=$?
    printf "CASE:w_default_server rc=%s runs=%s\n" "$rc" "$(runs)"

    # REAL_SAVE が存在しない / 実行不可なら即 非 0 で「保存せず」を返す (冒頭ガード)
    reset; _T_SOCKET=""
    ( REAL_SAVE="$RDIR/missing_save.sh"; tt_save_main quiet ); rc=$?
    printf "CASE:w_no_realsave rc=%s runs=%s\n" "$rc" "$(runs)"

    # 生存 owner の lock は奪わない: bounded-wait timeout (=0 で即時化) で保存せず非 0、
    # lock は残る (呼び出し側 debounce が timestamp を進めない契約の前提)
    reset; _T_SESSIONS="proj"; _T_SOCKET=""
    TT_SAVE_LOCK_WAIT_SECONDS=0
    mkdir -p "$TT_SAVE_LOCK_DIR"
    printf "%s %s\n" "$$" "$(tt_save_proc_starttime "$$")" > "$TT_SAVE_LOCK_DIR/pid"
    ( tt_save_main quiet ); rc=$?
    lock=absent; [ -d "$TT_SAVE_LOCK_DIR" ] && lock=present
    printf "CASE:w_lock_busy rc=%s runs=%s lock=%s\n" "$rc" "$(runs)" "$lock"

    # dead owner の取り残し lock は横取りして保存し、終了時に自分の lock を解放する
    reset
    sleep 100 & WDEAD=$!; kill "$WDEAD" 2>/dev/null; wait "$WDEAD" 2>/dev/null || true
    mkdir -p "$TT_SAVE_LOCK_DIR"
    printf "%s %s\n" "$WDEAD" "sometime" > "$TT_SAVE_LOCK_DIR/pid"
    ( tt_save_main quiet ); rc=$?
    lock=absent; [ -d "$TT_SAVE_LOCK_DIR" ] && lock=present
    printf "CASE:w_lock_stale rc=%s runs=%s lock=%s\n" "$rc" "$(runs)" "$lock"

    # 同一秒再保存ガードの配線: last の実体名が「現在秒」(date 固定スタブ) と同名のとき、
    # tt_save_main は save.sh 起動前に一度だけ待ってから保存する (サブシェル内で数えるため
    # sleep はファイルへ記録する)
    reset; _T_SESSIONS="proj"; _T_SOCKET=""
    TT_SAVE_LOCK_WAIT_SECONDS=15
    date() { case "${1:-}" in "+%s") command date +%s ;; *) printf "20990101T000000\n" ;; esac; }
    sleep() { echo s >> "$RDIR/slept"; }
    : > "$RDIR/slept"
    ln -sf "tmux_resurrect_20990101T000000.txt" "$RDIR/last"
    ( tt_save_main quiet ); rc=$?
    printf "CASE:w_same_second rc=%s runs=%s slept=%s\n" "$rc" "$(runs)" "$(wc -l < "$RDIR/slept" | tr -d " ")"
    unset -f date sleep
    rm -f "$RDIR/last" "$RDIR/slept"
  ' 2>/dev/null
)"
OUT="$OUT
$WRAP_OUT"

# ---- 検証 -------------------------------------------------------------------
case_line() { printf '%s\n' "$OUT" | grep "CASE:$1 " || true; }

assert_eq_line() {
  local id="$1" expect="$2" msg="$3" line
  line="$(case_line "$id")"
  if [[ "$line" != "CASE:$id $expect" ]]; then
    printf '✗ %s\n  expected: CASE:%s %s\n  actual:   %s\n' "$msg" "$id" "$expect" "$line"
    # CI flake の観測 (2026-07-11): wrap 系ケースの失敗時は xtrace を出し、
    # どのガード (restore_in_progress / only_hold / default_server / lock) で
    # 弾かれたかをログから特定できるようにする
    if [[ -s "${WRAP_TRACE:-}" ]]; then
      printf -- '---- wrap xtrace (guard 判定の追跡用) ----\n'
      cat "$WRAP_TRACE"
      printf -- '---- end wrap xtrace ----\n'
    fi
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
assert_eq_line fixb_zero_small "last=prev.txt archive=ORIGINAL bak=0" "全喪失 (2→0): prev がしきい値未満でも last と archive を退行前へ戻す"
assert_eq_line fixb_fail "rc=1 archive=ORIGINAL bak=0" "save 失敗 (rc≠0): 中途半端な archive をバックアップへ書き戻し、退避も残さない"

printf '\n## Fix B (window 軸): 単一セッション運用での window 壊滅検知\n'
assert_eq_line fixw_regress "last=prev.txt" "window 壊滅 (12→2, セッション数不変) でも last を退行前へ戻す"
assert_eq_line fixw_ok      "last=new.txt"  "通常の window 減 (12→8) は新保存を採用する"
assert_eq_line fixw_zero_small    "last=prev.txt" "全喪失 (6→0): prev がしきい値未満でも window 0 件は無条件に退行扱い"
assert_eq_line fixw_partial_small "last=new.txt"  "部分喪失 (6→2, しきい値未満): 正当な kill と区別できないので新保存を採用"
assert_eq_line fixw_gc "dead=no live=yes" "退避コピー GC: 死亡 pid の残置は掃除し、生存 pid のものは残す"

printf '\n## wrapper choke-point 統合ガード (hold のみ / 第 2 サーバ)\n'
assert_eq_line w_hold_only      "rc=1 runs=0" "hold のみ (bootstrap) は wrapper が保存せず非 0"
assert_eq_line w_hold_with_real "rc=0 runs=1" "hold 残置でも実セッションがあれば保存する"
assert_eq_line w_second_server  "rc=1 runs=0" "第 2 サーバ (別 socket) からは保存しない"
assert_eq_line w_default_server "rc=0 runs=1" "default socket のサーバは保存する"
assert_eq_line w_no_realsave    "rc=1 runs=0" "REAL_SAVE が実行不可なら保存せず非 0 を返す"

printf '\n## 同一秒再保存ガード (last dangling 化の遮断)\n'
assert_eq_line ss_same   "slept=1" "last の実体名が現在秒と同名なら次の秒まで待つ"
assert_eq_line ss_old    "slept=0" "過去秒の last では待たない"
assert_eq_line ss_nolast "slept=0" "last が無ければ待たない"

printf '\n## wrapper の lock 取得ループ (bounded-wait)\n'
assert_eq_line w_lock_busy  "rc=1 runs=0 lock=present" "生存 owner の lock は奪わず、待ち切れなければ保存せず非 0 (lock は残す)"
assert_eq_line w_lock_stale "rc=0 runs=1 lock=absent"  "dead owner の取り残し lock は横取りして保存し、終了時に解放する"
assert_eq_line w_same_second "rc=0 runs=1 slept=1" "tt_save_main は同一秒ガードを経由してから保存する (配線検証)"

printf '\nAll resurrect-save lock tests passed successfully!\n'
