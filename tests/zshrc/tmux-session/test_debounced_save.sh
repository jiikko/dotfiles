#!/usr/bin/env bash
# scripts/tmux_resurrect_debounced_save.sh の unit テスト。
#
# tmux / sleep の実挙動・hook 発火・実際の保存は実環境でしか確認できないため、
# ここではスクリプトが担う「保存して良いかの判定 (ガード) と debounce token の
# 採否」だけをスタブで固定する:
#   - tt_restore_in_progress / tt_hold_session_present / tt_should_save のガード
#   - tt_debounce_seconds の option 読み取り (未設定/非数値は既定 10)
#   - debounce token: 自分が最後なら保存・後続イベントがあれば保存しない
#   - tt_run_resurrect_save: @resurrect-save-script-path 解決と quiet 実行
#   - 復元中 / hold 存在中は tt_run_resurrect_save を呼ばないこと (last 保護)
#
# スクリプトは TT_DEBOUNCE_SOURCE_ONLY=1 で source すると main を実行しないので、
# 関数を直接呼んで検証する (tt の unit テストと同方式)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_resurrect_debounced_save.sh"
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
  TT_DEBOUNCE_STATE_DIR="$TMP_HOME/state" \
  TT_DEBOUNCE_SOURCE_ONLY=1 \
  bash -c '
    SCRIPT="'"$SCRIPT"'"

    # --- tmux スタブ -------------------------------------------------------
    # 状態: _T_INPROGRESS / _T_SESSIONS(空白区切り) / _T_DEBOUNCE_OPT /
    #       _T_SAVE_SCRIPT(@resurrect-save-script-path)
    # 記録: _LOG_SAVE_RUN(保存スクリプトが実行された回数を + で積む)
    tmux() {
      case "$1 $2 $3" in
        "show -gqv @tt-restore-in-progress") printf "%s\n" "$_T_INPROGRESS" ;;
        "show -gqv @tt-debounce-save-seconds") printf "%s\n" "$_T_DEBOUNCE_OPT" ;;
        "show -gqv @resurrect-save-script-path") printf "%s\n" "$_T_SAVE_SCRIPT" ;;
        "display-message -p #{socket_path}") printf "%s\n" "$_T_SOCKET" ;;
        "list-sessions -F"*) printf "%s\n" $_T_SESSIONS ;;
        "set-option -g @continuum-save-last-timestamp") echo set >> "'"$TMP_HOME"'/ts_sets" ;;
        *) : ;;
      esac
      return 0
    }
    sleep() { : ; }   # debounce 待ちを潰す

    source "$SCRIPT"

    ##########################################################################
    # debounce 秒数の読み取り
    ##########################################################################
    _T_DEBOUNCE_OPT="";   printf "CASE:dbs_default sec=%s\n"  "$(tt_debounce_seconds)"
    _T_DEBOUNCE_OPT="3";  printf "CASE:dbs_set sec=%s\n"      "$(tt_debounce_seconds)"
    _T_DEBOUNCE_OPT="abc";printf "CASE:dbs_invalid sec=%s\n"  "$(tt_debounce_seconds)"

    ##########################################################################
    # ガード: 復元中 / 復元フラグ stale / hold 存在 / 平常時
    #   @tt-restore-in-progress は「復元開始 epoch」。TTL 内なら復元中扱い。
    ##########################################################################
    _T_INPROGRESS="$(date +%s)"; _T_SESSIONS="proj"   # 直近 epoch → 復元中
    if tt_should_save; then printf "CASE:guard_inprogress save=yes\n"; else printf "CASE:guard_inprogress save=no\n"; fi

    _T_INPROGRESS="1"; _T_SESSIONS="proj"             # epoch=1(1970) → TTL 超過の降り損ね → 復元中でない
    if tt_should_save; then printf "CASE:guard_stale save=yes\n"; else printf "CASE:guard_stale save=no\n"; fi

    _T_INPROGRESS=""; _T_SESSIONS="__tt_hold_123"     # hold だけ = bootstrap → 抑止
    if tt_should_save; then printf "CASE:guard_hold_only save=yes\n"; else printf "CASE:guard_hold_only save=no\n"; fi

    _T_INPROGRESS=""; _T_SESSIONS="proj __tt_hold_123" # hold 残置でも実セッション有 → 保存再開
    if tt_should_save; then printf "CASE:guard_hold_with_real save=yes\n"; else printf "CASE:guard_hold_with_real save=no\n"; fi

    _T_INPROGRESS=""; _T_SESSIONS="proj other"
    if tt_should_save; then printf "CASE:guard_ok save=yes\n"; else printf "CASE:guard_ok save=no\n"; fi

    # 単一環境 gate (不変条件 5): default socket 以外のサーバからは保存しない。
    # 期待値は canonical /tmp 基準 + realpath 解決 (スクリプト側と同じ組み方)。
    _T_INPROGRESS=""; _T_SESSIONS="proj"
    _T_SOCKET="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"
    if tt_should_save; then printf "CASE:guard_default_sock save=yes\n"; else printf "CASE:guard_default_sock save=no\n"; fi

    _T_SOCKET="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/exp"   # 第 2 サーバ (tmux -L exp)
    if tt_should_save; then printf "CASE:guard_other_sock save=yes\n"; else printf "CASE:guard_other_sock save=no\n"; fi

    _T_SOCKET=""   # socket_path が取れない (古い tmux / スタブ) → fail-open で保存を殺さない
    if tt_should_save; then printf "CASE:guard_no_sock save=yes\n"; else printf "CASE:guard_no_sock save=no\n"; fi

    ##########################################################################
    # 保存スクリプト解決と実行
    ##########################################################################
    # 実行されたら _LOG_SAVE_RUN に + を積む偽 save スクリプトを用意
    fake_save="'"$TMP_HOME"'/fake_save.sh"
    printf "#!/usr/bin/env bash\necho ran >> '"$TMP_HOME"'/save_runs\n" > "$fake_save"
    chmod +x "$fake_save"

    : > "'"$TMP_HOME"'/save_runs"
    _T_SAVE_SCRIPT="$fake_save"
    tt_run_resurrect_save
    printf "CASE:save_run runs=%s\n" "$(wc -l < "'"$TMP_HOME"'/save_runs" | tr -d " ")"

    : > "'"$TMP_HOME"'/save_runs"
    _T_SAVE_SCRIPT=""        # 未設定 → 実行しない
    if tt_run_resurrect_save; then rc=0; else rc=1; fi
    printf "CASE:save_noscript rc=%s runs=%s\n" "$rc" "$(wc -l < "'"$TMP_HOME"'/save_runs" | tr -d " ")"

    ##########################################################################
    # continuum 最終保存時刻の更新規律 (核心不変条件: 成功時のみ進める)
    #   保存に失敗したのに timestamp を進めると、(a) イベントが保存されないまま
    #   (b) continuum 周期保存も次周期まで抑止される取りこぼし連鎖が起きる。
    ##########################################################################
    ts_count() { wc -l < "'"$TMP_HOME"'/ts_sets" 2>/dev/null | tr -d " "; }
    : > "'"$TMP_HOME"'/ts_sets"
    _T_SAVE_SCRIPT="$fake_save"          # 成功する保存 → timestamp を進める
    tt_run_resurrect_save
    printf "CASE:ts_on_success sets=%s\n" "$(ts_count)"

    fail_save="'"$TMP_HOME"'/fail_save.sh"
    printf "#!/usr/bin/env bash\nexit 1\n" > "$fail_save"
    chmod +x "$fail_save"
    : > "'"$TMP_HOME"'/ts_sets"
    _T_SAVE_SCRIPT="$fail_save"          # 失敗 (wrapper 非 0 = 保存せず) → 進めない
    if tt_run_resurrect_save; then rc=0; else rc=1; fi
    printf "CASE:ts_on_failure rc=%s sets=%s\n" "$rc" "$(ts_count)"

    ##########################################################################
    # main の debounce token 採否
    #  (sleep は no-op。main 内で token を書き、後で自分の token と比較する)
    ##########################################################################
    # (a) 自分が最後 → 保存可状態なら save が走る
    : > "'"$TMP_HOME"'/save_runs"
    _T_INPROGRESS=""; _T_SESSIONS="proj"; _T_SAVE_SCRIPT="$fake_save"
    tt_debounced_save_main
    printf "CASE:main_latest runs=%s\n" "$(wc -l < "'"$TMP_HOME"'/save_runs" | tr -d " ")"

    # (b) 自分の後に別イベントが来た → token 不一致で保存しない
    #     main 実行後に token ファイルを別値で上書きしてから…ではなく、
    #     sleep を「後続イベントを模す」フックに差し替える。
    : > "'"$TMP_HOME"'/save_runs"
    sleep() { printf "later-event\n" > "$TT_DEBOUNCE_TOKEN_FILE"; }  # 待機中に別 token が書かれた状況
    tt_debounced_save_main
    printf "CASE:main_superseded runs=%s\n" "$(wc -l < "'"$TMP_HOME"'/save_runs" | tr -d " ")"
    sleep() { : ; }

    # (c) 自分が最後でも復元中なら保存しない（直近 epoch = 復元中）
    : > "'"$TMP_HOME"'/save_runs"
    _T_INPROGRESS="$(date +%s)"
    tt_debounced_save_main
    printf "CASE:main_guarded runs=%s\n" "$(wc -l < "'"$TMP_HOME"'/save_runs" | tr -d " ")"
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

printf '\n## debounce 秒数\n'
assert_eq_line dbs_default "sec=10" "未設定 → 既定 10 秒"
assert_eq_line dbs_set     "sec=3"  "@tt-debounce-save-seconds=3 → 3 秒"
assert_eq_line dbs_invalid "sec=10" "非数値 → 既定 10 秒"

printf '\n## 保存ガード (last 保護の不変条件)\n'
assert_eq_line guard_inprogress     "save=no"  "復元中 (直近 epoch) は保存しない"
assert_eq_line guard_stale          "save=yes" "復元フラグが TTL 超過 (降り損ね) なら保存を再開する"
assert_eq_line guard_hold_only      "save=no"  "hold だけ (bootstrap) は保存しない"
assert_eq_line guard_hold_with_real "save=yes" "hold 残置でも実セッションがあれば保存再開 (永久抑止しない)"
assert_eq_line guard_ok             "save=yes" "平常時 (復元中でも hold でもない) は保存可"
assert_eq_line guard_default_sock   "save=yes" "default socket のサーバは保存可 (単一環境 gate)"
assert_eq_line guard_other_sock     "save=no"  "第 2 サーバ (別 socket) からは保存しない (last 上書き防止)"
assert_eq_line guard_no_sock        "save=yes" "socket_path が取れない環境は fail-open (保存を殺さない)"

printf '\n## 保存スクリプト解決\n'
assert_eq_line save_run      "runs=1"        "@resurrect-save-script-path を quiet 実行する"
assert_eq_line save_noscript "rc=1 runs=0"   "保存スクリプト未設定なら実行せず失敗を返す"

printf '\n## continuum 最終保存時刻の更新規律\n'
assert_eq_line ts_on_success "sets=1"      "保存成功時は @continuum-save-last-timestamp を進める (同秒二重起動の抑止)"
assert_eq_line ts_on_failure "rc=1 sets=0" "保存失敗 (wrapper 非 0) 時は timestamp を進めない (取りこぼし連鎖の防止)"

printf '\n## main の debounce token 採否\n'
assert_eq_line main_latest     "runs=1" "自分が最後のイベント+保存可 → 保存する"
assert_eq_line main_superseded "runs=0" "待機中に後続イベントが来たら保存しない (debounce)"
assert_eq_line main_guarded    "runs=0" "自分が最後でも復元中なら保存しない"

printf '\nAll debounced-save tests passed successfully!\n'
