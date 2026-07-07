#!/usr/bin/env bash
# tt / t / _tt_wait_for_restore の unit テスト。
#
# tmux / continuum / resurrect の実挙動やタイミング依存（restore_from_scratch、
# post-restore-all フックの発火、別サーバ判定など）は実環境でしか確認できないため、
# ここでは「_zshrc(lib) が担う zsh の制御フロー・分岐ロジック」だけをスタブで固定する:
#   - _tt_wait_for_restore の戻り値判定 (0/1/2/3) と resurrect 保存先の解決規則
#   - _tt_impl の rc → 分岐 (hold を畳むか / attach のみで抜けるか / 新規作成するか)
#   - _t_impl の挙動 (5 窓作成 / select / 引数なしのユニーク名)
#   - 公開ラッパー t/tt が「呼ばれるたびに lib を再 source して最新実体で動く」こと
#
# ロジックは実体 (_t_impl / _tt_impl / _tt_wait_for_restore) に集約されているので、
# スタブ注入が必要なテストは実体を直接呼ぶ。ラッパー t/tt の再評価挙動は別途検証する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
ZSH_LIB="$ROOT_DIR/zshlib/_tmux_session.zsh"
TMP_HOME="$(mktemp -d)"
cleanup() { rm -rf "$TMP_HOME"; }
trap cleanup EXIT

# _tt_impl はサーバ未起動時に孤児 tmux サーバ reap (scripts/tmux_reap_orphan_servers.sh) を
# 呼ぶが、それは実 pgrep/lsof/kill で動き下の tmux スタブでは傍受できない。unit テストが実環境の
# プロセスを触らないよう reap を無効化する（子 zsh -c に export で伝播させる）。
export TT_SKIP_REAP=1
export TT_ASSUME_TTY=1

if [[ ! -f "$ZSH_LIB" ]]; then
  printf '✗ lib が存在しません: %s\n' "$ZSH_LIB"
  exit 1
fi
printf '✓ %s exists\n' "${ZSH_LIB#$ROOT_DIR/}"

# ============================================================================
# Part 1: 実体ロジック（tmux/sleep をスタブし、実体を直接呼ぶ）
# 各ケースは "CASE:<id> ..." を1行で出力し、bash 側で検証する。
# ============================================================================
OUT="$(HOME="$TMP_HOME" zsh -c '
  sleep() { : }   # 待機を潰してタイムアウト経路も即時化する

  # --- tmux スタブ ---------------------------------------------------------
  # 状態: _T_SERVER(非空=サーバ起動中) / _T_SESSIONS(存在セッション) /
  #       _T_RDIR/_T_CONT/_T_HOOK/_T_FLAG(show -gqv の返り値)
  # 記録: _LOG_NEW/_LOG_NEWWIN/_LOG_SELECT/_LOG_KILL/_LOG_ATTACH/_LOG_T
  tmux() {
    case "$1" in
      has-session)
        if [[ "$2" == "-t" ]]; then
          # 実 tmux の =prefix (exact match 指定) をモデル化: = を剥がして厳密一致で判定。
          # attach 等のログは raw ($3 そのまま) で記録するため、「= が付いていること」自体は
          # 期待値側 (attach=[ =proj] 等) が強制する。
          local _t="${3#=}"
          [[ " $_T_SESSIONS " == *" $_t "* ]] && return 0 || return 1
        fi
        [[ -n "$_T_SERVER" ]] && return 0 || return 1 ;;
      show)
        case "$3" in
          @resurrect-dir) print -r -- "$_T_RDIR" ;;
          @continuum-restore) print -r -- "$_T_CONT" ;;
          @resurrect-hook-post-restore-all) print -r -- "$_T_HOOK" ;;
          @resurrect-restore-script-path) print -r -- "$_T_RESTORE_SCRIPT" ;;
          @tt-restore-complete) print -r -- "$_T_FLAG" ;;
        esac ;;
      new-session)   _T_SERVER=1; _T_SESSIONS="$_T_SESSIONS ${4}"; _LOG_NEW="$_LOG_NEW ${4}" ;;
      new-window)    _LOG_NEWWIN="$_LOG_NEWWIN ${3}" ;;   # new-window -t <name>
      select-window) _LOG_SELECT="$_LOG_SELECT ${3}" ;;   # select-window -t <name>:0
      kill-session)  _LOG_KILL="$_LOG_KILL ${3}" ;;
      attach-session) _LOG_ATTACH="$_LOG_ATTACH ${3}" ;;
      list-sessions) print -r -- "$_T_GC_SESSIONS" ;;   # GC 用: "name wins attached" の行群
      list-panes)    # GC 用: -t =<name> の pane 数を _T_GC_PANES_<name> から返す ($3 = "=name")
        local _gn="${3#=}"; local _pv; eval "_pv=\"\${_T_GC_PANES_${_gn}:-1}\""
        local _k; for _k in $(seq 1 "$_pv"); do print -r -- "pane$_k"; done ;;
    esac
    return 0
  }

  source "'"$ZSH_LIB"'"
  reset_log() { _LOG_NEW=""; _LOG_NEWWIN=""; _LOG_SELECT=""; _LOG_KILL=""; _LOG_ATTACH=""; _LOG_T=""; }

  ##########################################################################
  # 0. _t_impl 本体 (5 窓作成 / select / 引数なしのユニーク名)
  ##########################################################################
  _T_SERVER=""; _T_SESSIONS=""; reset_log
  _t_impl myproj
  print "CASE:t_named new=[$_LOG_NEW] newwin=[$_LOG_NEWWIN] select=[$_LOG_SELECT] attach=[$_LOG_ATTACH]"

  _T_SERVER=""; _T_SESSIONS=""; reset_log
  _t_impl   # 引数なし → s<unixtime> のユニーク名で作成
  print "CASE:t_auto new=[$_LOG_NEW] attach=[$_LOG_ATTACH]"

  # t を dotted 名で直接呼ぶ経路 (tt 経由でない) でも . : を置換すること。
  # tmux 3.1+ は new-session -s で . : を _ にサイレント置換するため、こちらが置換しないと
  # 「作られた名前 (a_b_c)」と「new-window/attach の target (a.b:c)」が食い違い全 target が
  # 解決失敗する (9b04822 で tt 側だけ直して t 側が漏れていた回帰の防止)。
  _T_SERVER=""; _T_SESSIONS=""; reset_log
  _t_impl "a.b:c"
  print "CASE:t_dotted new=[$_LOG_NEW] newwin=[$_LOG_NEWWIN] select=[$_LOG_SELECT] attach=[$_LOG_ATTACH]"

  # 以降 _t_impl をスタブ化（_tt_impl が新規作成経路に落ちたかを観測する）
  _t_impl() { _LOG_T="$_LOG_T $1"; }

  ##########################################################################
  # A. _tt_wait_for_restore の戻り値判定 (本物を使用)
  ##########################################################################
  _T_CONT="on"; _T_HOOK="x"; _T_FLAG=""; _T_RESTORE_SCRIPT="x"

  _T_RDIR=""; XDG_DATA_HOME="/nonexistent_a"
  _tt_wait_for_restore; print "CASE:rc_nolast rc=$?"

  XDG_DATA_HOME="$(mktemp -d)"; mkdir -p "$XDG_DATA_HOME/tmux/resurrect"
  : > "$XDG_DATA_HOME/tmux/resurrect/last"

  _T_FLAG="1"; _tt_wait_for_restore; print "CASE:rc_flag rc=$?"
  _T_FLAG="";  _tt_wait_for_restore; print "CASE:rc_timeout rc=$?"
  _T_HOOK="";  _tt_wait_for_restore; print "CASE:rc_nohook rc=$?"; _T_HOOK="x"
  _T_CONT="off"; _tt_wait_for_restore; print "CASE:rc_contoff rc=$?"; _T_CONT="on"
  _T_RESTORE_SCRIPT=""; _tt_wait_for_restore; print "CASE:rc_noscript rc=$?"; _T_RESTORE_SCRIPT="x"
  : > "$HOME/tmux_no_auto_restore"
  _tt_wait_for_restore; print "CASE:rc_halt rc=$?"
  rm -f "$HOME/tmux_no_auto_restore"

  ##########################################################################
  # B. 保存先解決規則 (resurrect 本体 helpers.sh resurrect_dir と同じか)
  ##########################################################################
  custom="$HOME/.custom_resurrect"; mkdir -p "$custom"; : > "$custom/last"
  _T_RDIR="~/.custom_resurrect"; _T_FLAG="1"
  _tt_wait_for_restore; print "CASE:dir_tilde rc=$?"

  empty="$HOME/.empty_resurrect"; mkdir -p "$empty"
  _T_RDIR="$empty"
  _tt_wait_for_restore; print "CASE:dir_isolated rc=$?"
  _T_RDIR=""

  ##########################################################################
  # C. _tt_impl の rc → 分岐 (_tt_wait_for_restore をスタブして rc を注入)
  ##########################################################################
  cd "$HOME"

  # C1: サーバ起動中 + 目的有 → hold を作らず目的に attach、新規作成しない
  _T_SERVER=1; _T_SESSIONS="proj"; reset_log
  _tt_impl proj
  print "CASE:srv_exist new=[$_LOG_NEW] attach=[$_LOG_ATTACH] t=[$_LOG_T]"

  # C2: サーバ起動中 + 目的無 → 新規作成
  _T_SERVER=1; _T_SESSIONS=""; reset_log
  _tt_impl proj
  print "CASE:srv_missing new=[$_LOG_NEW] t=[$_LOG_T]"

  # 以降サーバ未起動。_tt_wait_for_restore をスタブして rc 注入。
  # ※ new-session スタブが _T_SERVER=1 にするため、各ケースで _T_SERVER="" を再設定する。

  # C3: 未起動 + rc=0 + 目的有 → hold 作成 → hold を畳む → 目的に attach
  _tt_wait_for_restore() { return 0; }
  _T_SERVER=""; _T_SESSIONS="proj"; reset_log
  _tt_impl proj
  print "CASE:rc0_exist new=[$_LOG_NEW] kill=[$_LOG_KILL] attach=[$_LOG_ATTACH] t=[$_LOG_T]"

  # C4: 未起動 + rc=0 + 目的無 → hold を畳む → 新規作成
  _T_SERVER=""; _T_SESSIONS=""; reset_log
  _tt_impl proj
  print "CASE:rc0_missing kill=[$_LOG_KILL] t=[$_LOG_T]"

  # C5: 未起動 + rc=3 → hold を畳む → 新規作成
  _tt_wait_for_restore() { return 3; }
  _T_SERVER=""; _T_SESSIONS=""; reset_log
  _tt_impl proj
  print "CASE:rc3 kill=[$_LOG_KILL] t=[$_LOG_T]"

  # C6: 未起動 + rc=1 + 目的有 → hold を畳まず・新規作成せず、目的に attach
  _tt_wait_for_restore() { return 1; }
  _T_SERVER=""; _T_SESSIONS="proj"; reset_log
  _tt_impl proj
  print "CASE:rc1_exist kill=[$_LOG_KILL] t=[$_LOG_T] attach=[$_LOG_ATTACH]"

  # C7: 未起動 + rc=2 + 目的無 → 警告 + hold に attach、新規作成しない
  _tt_wait_for_restore() { return 2; }
  _T_SERVER=""; _T_SESSIONS=""; reset_log
  _tt_impl proj 2>"$HOME/.warn"
  print "CASE:rc2_missing kill=[$_LOG_KILL] t=[$_LOG_T] attach=[$_LOG_ATTACH] warn=[$(cat "$HOME/.warn")]"

  ##########################################################################
  # D. _tt_impl の名前算出 (引数あり置換 / 引数なし basename)
  ##########################################################################
  _T_SERVER=1; _T_SESSIONS=""; reset_log
  _tt_impl "a.b.c"
  print "CASE:name t=[$_LOG_T]"

  # コロンも置換される (tmux target の区切り文字。3.1+ は new-session 側でも同置換される)
  _T_SERVER=1; _T_SESSIONS=""; reset_log
  _tt_impl "a:b.c"
  print "CASE:name_colon t=[$_LOG_T]"

  mkdir -p "$HOME/x.y"; cd "$HOME/x.y"   # 引数なし → basename "$PWD"
  _T_SERVER=1; _T_SESSIONS=""; reset_log
  _tt_impl
  print "CASE:name_pwd t=[$_LOG_T]"

  ##########################################################################
  # GC. _tt_gc_stale_holds の三重条件 (pid死亡 + 非attach + pristine のみ kill)
  ##########################################################################
  # 死んだ pid を用意 (起動即 kill。テストは <1s で終わるので再利用はまず起きない)
  sleep 100 & DEADPID=$!; kill "$DEADPID" 2>/dev/null; wait "$DEADPID" 2>/dev/null || true
  ALIVEPID=$$   # 自分は生きている

  # (kill 対象) 死 pid + 非attach + 1win/1pane → kill される
  reset_log
  _T_GC_SESSIONS="__tt_hold_${DEADPID} 1 0
proj 3 1"
  _tt_gc_stale_holds
  print "CASE:gc_stale kill=[$_LOG_KILL]"

  # (保護) 生存 pid の hold → 並行 tt。触らない
  reset_log
  _T_GC_SESSIONS="__tt_hold_${ALIVEPID} 1 0"
  _tt_gc_stale_holds
  print "CASE:gc_alive kill=[$_LOG_KILL]"

  # (保護) 死 pid だが attach 中 → 採用された作業セッション。触らない
  reset_log
  _T_GC_SESSIONS="__tt_hold_${DEADPID} 1 1"
  _tt_gc_stale_holds
  print "CASE:gc_attached kill=[$_LOG_KILL]"

  # (保護) 死 pid・非attach だが 2 window → 作業で育った hold。触らない
  reset_log
  _T_GC_SESSIONS="__tt_hold_${DEADPID} 2 0"
  _tt_gc_stale_holds
  print "CASE:gc_multiwin kill=[$_LOG_KILL]"

  # (保護) 死 pid・非attach・1 window だが 2 pane → split した作業 hold。触らない
  reset_log
  _T_GC_SESSIONS="__tt_hold_${DEADPID} 1 0"
  eval "_T_GC_PANES___tt_hold_${DEADPID}=2"
  _tt_gc_stale_holds
  print "CASE:gc_multipane kill=[$_LOG_KILL]"
  eval "unset _T_GC_PANES___tt_hold_${DEADPID}"

  ##########################################################################
  # E. 非TTYガード (TT_ASSUME_TTY 未設定なら tmux に到達しない)
  ##########################################################################
  source "'"$ZSH_LIB"'"
  unset TT_ASSUME_TTY

  _T_SERVER=""; _T_SESSIONS=""; reset_log
  _t_impl guard_t 2>"$HOME/.guard_t_warn"
  rc=$?
  print "CASE:guard_t rc=$rc new=[$_LOG_NEW] warn=[$(cat "$HOME/.guard_t_warn")]"

  _T_SERVER=""; _T_SESSIONS=""; reset_log
  _tt_impl guard.tt 2>"$HOME/.guard_tt_warn"
  rc=$?
  print "CASE:guard_tt rc=$rc new=[$_LOG_NEW] warn=[$(cat "$HOME/.guard_tt_warn")]"
' 2>/dev/null)"

# ============================================================================
# Part 2: 公開ラッパー t/tt の「毎回再評価」検証
#   _TMUX_SESSION_LIB を temp lib に差し替え、その中身を書き換えて tt を 2 回呼ぶ。
#   1 回目 V1 / 書き換え後の 2 回目 V2 が出れば「再読込なしで最新反映」を証明できる。
# ============================================================================
LIVE_LIB="$TMP_HOME/live_lib.zsh"
RELOAD_OUT="$(HOME="$TMP_HOME" zsh -c '
  source "'"$ZSH_LIB"'"                       # 本物の t/tt ラッパーを得る
  typeset -g _TMUX_SESSION_LIB="'"$LIVE_LIB"'" # 再 source 先を temp lib に差し替え

  print "_tt_impl() { print RELOAD_V1 }" > "'"$LIVE_LIB"'"
  tt                                          # ラッパーが temp を source → V1
  print "_tt_impl() { print RELOAD_V2 }" > "'"$LIVE_LIB"'"
  tt                                          # 再 source → 最新の V2
' 2>/dev/null)"

# ---- 検証 -------------------------------------------------------------------
case_line() { printf '%s\n' "$OUT" | grep "CASE:$1 " || true; }

assert_eq_line() {  # 行全体が期待文字列と一致するか
  local id="$1" expect="$2" msg="$3" line
  line="$(case_line "$id")"
  if [[ "$line" != "CASE:$id $expect" ]]; then
    printf '✗ %s\n  expected: CASE:%s %s\n  actual:   %s\n' "$msg" "$id" "$expect" "$line"
    exit 1
  fi
  printf '✓ %s\n' "$msg"
}

assert_line_has() {  # 行が部分文字列を含むか（hold 名のプレフィックスや date 依存値用）
  local id="$1" needle="$2" msg="$3" line
  line="$(case_line "$id")"
  if [[ "$line" != *"$needle"* ]]; then
    printf '✗ %s\n  expected to find: %s\n  in: %s\n' "$msg" "$needle" "$line"
    exit 1
  fi
  printf '✓ %s\n' "$msg"
}

assert_has() {  # 任意の出力に部分文字列が含まれるか
  local haystack="$1" needle="$2" msg="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    printf '✗ %s\n  expected to find: %s\n  in:\n%s\n' "$msg" "$needle" "$haystack"
    exit 1
  fi
  printf '✓ %s\n' "$msg"
}

printf '\n## _t_impl 本体\n'
assert_eq_line  t_named "new=[ myproj] newwin=[ myproj myproj myproj myproj] select=[ myproj:0] attach=[ myproj]" \
  "t: 1 セッション + 4 window 追加 + select :0 + attach"
assert_line_has t_auto  "new=[ s"  "t 引数なし: s<unixtime> のユニーク名で作成"
assert_eq_line  t_dotted "new=[ a_b_c] newwin=[ a_b_c a_b_c a_b_c a_b_c] select=[ a_b_c:0] attach=[ a_b_c]" \
  "t 直呼び: ドット/コロンを置換して作成名と target を一致させる"

printf '\n## _tt_wait_for_restore 戻り値判定\n'
assert_eq_line rc_nolast  "rc=3" "last 無し → rc=3"
assert_eq_line rc_flag    "rc=0" "完了フラグあり → rc=0"
assert_eq_line rc_timeout "rc=1" "フラグ立たず → rc=1 (タイムアウト)"
assert_eq_line rc_nohook  "rc=2" "完了検知フック未設定 → rc=2"
assert_eq_line rc_contoff "rc=3" "@continuum-restore off → rc=3"
assert_eq_line rc_noscript "rc=3" "@resurrect-restore-script-path 未設定 (plugin 未ロード) → rc=3"
assert_eq_line rc_halt    "rc=3" "halt file 存在 → rc=3"

printf '\n## 保存先解決規則\n'
assert_eq_line dir_tilde    "rc=0" "@resurrect-dir の ~ を展開して last を発見"
assert_eq_line dir_isolated "rc=3" "@resurrect-dir 指定先のみ参照 (XDG にフォールバックしない)"

printf '\n## _tt_impl の rc → 分岐\n'
assert_eq_line  srv_exist   "new=[] attach=[ =proj] t=[]" "既存サーバ+目的有: hold作らず目的に exact attach、新規作成しない"
assert_eq_line  srv_missing "new=[] t=[ proj]"            "既存サーバ+目的無: hold作らず新規作成"
assert_line_has rc0_exist   "new=[ __tt_hold_"           "rc=0: hold を作成する"
assert_line_has rc0_exist   "kill=[ __tt_hold_"          "rc=0: hold を畳む"
assert_line_has rc0_exist   "attach=[ =proj] t=[]"       "rc=0+目的有: 目的に exact attach、新規作成しない"
assert_line_has rc0_missing "kill=[ __tt_hold_"          "rc=0+目的無: hold を畳む"
assert_line_has rc0_missing "t=[ proj]"                  "rc=0+目的無: 新規作成"
assert_line_has rc3         "kill=[ __tt_hold_"          "rc=3: hold を畳む"
assert_line_has rc3         "t=[ proj]"                  "rc=3: 新規作成"
assert_eq_line  rc1_exist   "kill=[] t=[] attach=[ =proj]" "rc=1+目的有: holdを畳まず・新規作成せず、目的に exact attach"
assert_line_has rc2_missing "t=[] attach=[ =__tt_hold_"  "rc=2+目的無: 新規作成せず hold に exact attach"
assert_line_has rc2_missing "@resurrect-hook-post-restore-all" "rc=2: フック未設定の警告を出す"

printf '\n## 名前算出\n'
assert_eq_line name     "t=[ a_b_c]" "引数のドットをアンダースコアに置換"
assert_eq_line name_colon "t=[ a_b_c]" "引数のコロンもアンダースコアに置換"
assert_eq_line name_pwd "t=[ x_y]"   "引数なし → basename \$PWD + ドット置換"

printf '\n## stale hold GC (三重条件)\n'
assert_line_has gc_stale     "kill=[ =__tt_hold_" "死pid+非attach+pristine の hold は kill する"
assert_eq_line  gc_alive     "kill=[]"           "生存pid の hold は触らない (並行 tt)"
assert_eq_line  gc_attached  "kill=[]"           "attach 中の hold は触らない (採用された作業)"
assert_eq_line  gc_multiwin  "kill=[]"           "2 window の hold は触らない (育った作業)"
assert_eq_line  gc_multipane "kill=[]"           "2 pane の hold は触らない (split した作業)"

printf '\n## 非TTYガード\n'
assert_line_has guard_t  "rc=1 new=[]" "非TTYの _t_impl: return 1 かつ new-session しない"
assert_line_has guard_t  "対話端末ではありません" "非TTYの _t_impl: stderr に警告を出す"
assert_line_has guard_tt "rc=1 new=[]" "非TTYの _tt_impl: return 1 かつ new-session しない"
assert_line_has guard_tt "対話端末ではありません" "非TTYの _tt_impl: stderr に警告を出す"

printf '\n## 公開ラッパーの毎回再評価\n'
assert_has "$RELOAD_OUT" "RELOAD_V1" "tt: 1 回目は lib を source して実行 (V1)"
assert_has "$RELOAD_OUT" "RELOAD_V2" "tt: lib 書き換え後の 2 回目は再 source で最新反映 (V2)"

printf '\nAll tt tests passed successfully!\n'
