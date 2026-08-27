#!/usr/bin/env bash
# scripts/tmux_schedule_keys.sh (prefix+m 予約入力) の unit テスト。PATH stub 方式 (偽 tmux / gum / date
# が呼び出しを記録) で、実 tmux サーバには触れない。tty 必須の gum TUI 描画は対象外。
#
# 固定する不変条件:
#   - 対象の固定: 冒頭で解決した pane_id がそのまま job に書かれ、fire の send-keys -t に渡ること
#   - UI (bin/schedkeys) との契約: 結果行 "new <TAB> epoch <TAB> text" で job が出来、
#     "cancel <TAB> id" で確認 (gum confirm --default=false) を経て取り消される。UI が中止 (非 0) /
#     壊れた結果を返したら何も作らない。UI へ渡す一覧 TSV は job ファイルの中身と一致する
#   - fire はリテラル送信 (send-keys -l) + 別呼び出しの Enter。"Enter" という文字列がキー名に化けない
#   - fire は発火時刻まで送らない (早すぎる送信 = 予約の意味が無い)
#   - 送り先 pane が消えていたら (send-keys が失敗したら) Enter を送らず、無音 (stdout/stderr 空・
#     exit 0) で job を掃き、成功ログを書かない
#   - .pid の数字だけで kill しない: 無関係なプロセスが同じ pid を持っていても kill せず、stale として掃く
#   - 表示文字列 (シェル / UI とも) に絵文字・曖昧幅の記号を混ぜない
#
# 時刻の解釈・入力欄の挙動・IME のカーソル位置は Go 側 (src/schedkeys) のテストが持つ。ここは配線だけ。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_schedule_keys.sh"
CONF="$ROOT_DIR/_tmux.conf"
TMP_DIR="$(mktemp -d)"
FAKE_PIDS=()
cleanup() {
  for p in "${FAKE_PIDS[@]:-}"; do [[ -n "$p" ]] && kill "$p" 2>/dev/null || true; done
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

[[ -x "$SCRIPT" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"; exit 1; }

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
export HOME="$TMP_DIR/home"; mkdir -p "$HOME"
export TMUX_SCHEDULE_KEYS_DIR="$TMP_DIR/state"
export XDG_CACHE_HOME="$TMP_DIR/cache"

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/bin_nogum"
# stub tmux: 呼び出しを記録し、スクリプトが使う照会にだけ応える。STUB_PANE_GONE=1 で %5 消滅を模す
cat > "$TMP_DIR/bin/tmux" <<'EOS2'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$*" in
  "display-message -p #{pane_id}") printf '%%5\n' ;;
  "display-message -p -t %5 "*)
    [ "${STUB_PANE_GONE:-0}" = 1 ] && exit 1
    case "$*" in *pane_id*) printf '%%5\n' ;; *) printf 'main:3 claude\n' ;; esac ;;
  "send-keys -t %5 "*)
    # 実 tmux は pane 不在で stderr にエラーを出して rc=1 (この stderr が run-shell 経由で view-mode に積まれる)
    [ "${STUB_PANE_GONE:-0}" = 1 ] && { echo "can't find pane: %5" >&2; exit 1; } ;;
esac
exit 0
EOS2
chmod +x "$TMP_DIR/bin/tmux"; cp "$TMP_DIR/bin/tmux" "$TMP_DIR/bin_nogum/tmux"

# stub gum: confirm だけを使う (入力系は UI = schedkeys が持つ)。STUB_GUM_EXIT で承認/拒否
cat > "$TMP_DIR/bin/gum" <<'EOS2'
#!/bin/sh
echo "gum $*" >> "$CALLS"
case "$1" in
  confirm) exit "${STUB_GUM_EXIT:-1}" ;;
  style)   shift; while [ $# -gt 0 ] && [ "${1#-}" != "$1" ]; do shift 2; done; printf '%s\n' "$*"; exit 0 ;;
esac
exit 0
EOS2
chmod +x "$TMP_DIR/bin/gum"

# stub schedkeys (対話 UI): 渡された --jobs を控え、STUB_UI_RESULT を --out へ書く。
# STUB_UI_EXIT=1 で「中止」を模す (out には中身が残る = 終了コードで判断させる)
cat > "$TMP_DIR/bin/schedkeys" <<'EOS2'
#!/bin/sh
echo "schedkeys $*" >> "$CALLS"
out=''; jobs=''
while [ $# -gt 0 ]; do
  case "$1" in
    --out) out="$2"; shift 2 ;;
    --jobs) jobs="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$jobs" ] && [ -f "$jobs" ] && cp "$jobs" "$STUB_UI_JOBS_COPY"
# 中止でも out に中身を残す形を模す (mktemp の使い回し・途中書き)。呼び出し側は
# 「終了コードが非 0 なら結果を使わない」ことで守る
printf '%s\n' "${STUB_UI_RESULT:-}" > "$out"
exit "${STUB_UI_EXIT:-0}"
EOS2
chmod +x "$TMP_DIR/bin/schedkeys"
cp "$TMP_DIR/bin/schedkeys" "$TMP_DIR/bin_nogum/schedkeys"

# stub date: `date +%s` を STUB_NOW、`date +%H:%M` を STUB_NOW_HM で固定 (予約時刻の算術を決定論化)。他は実 date へ
cat > "$TMP_DIR/bin/date" <<'EOS2'
#!/bin/sh
[ -n "${STUB_NOW:-}" ] && [ "$*" = "+%s" ] && { printf '%s\n' "$STUB_NOW"; exit 0; }
[ -n "${STUB_NOW_HM:-}" ] && [ "$*" = "+%H:%M" ] && { printf '%s\n' "$STUB_NOW_HM"; exit 0; }
exec /bin/date "$@"
EOS2
chmod +x "$TMP_DIR/bin/date"; cp "$TMP_DIR/bin/date" "$TMP_DIR/bin_nogum/date"

# sleep も stub: fire の待機は STUB_SLEEP_LOG に記録するだけ (実時間を待たない)。
# 「発火時刻まで送らない」は実 sleep で別に 1 回だけ測る (下の F2)
cat > "$TMP_DIR/bin/sleep" <<'EOS2'
#!/bin/sh
echo "sleep $*" >> "$CALLS"
[ -n "${STUB_REAL_SLEEP:-}" ] && exec /bin/sleep "$@"
exit 0
EOS2
chmod +x "$TMP_DIR/bin/sleep"; cp "$TMP_DIR/bin/sleep" "$TMP_DIR/bin_nogum/sleep"

export TMUX_SCHEDULE_KEYS_UI="$TMP_DIR/bin/schedkeys"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
NOGUM_PATH="$TMP_DIR/bin_nogum:/usr/bin:/bin"
export STUB_UI_JOBS_COPY="$TMP_DIR/ui_jobs.tsv"
RUN_OUT="$TMP_DIR/out.log"; RUN_ERR="$TMP_DIR/err.log"
# shellcheck source=tests/tmux/lib/stub_assert_helper.sh
. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
jobs_count() { find "$TMUX_SCHEDULE_KEYS_DIR" -name '*.job' 2>/dev/null | wc -l | tr -d ' '; }
reset_state() { rm -rf "$TMUX_SCHEDULE_KEYS_DIR"; reset_calls; }

# shellcheck source=tests/tmux/lib/stub_env.sh
. "$ROOT_DIR/tests/tmux/lib/stub_env.sh"
# 本物の sleeper を起こす (pid_is_sleeper は ps の command line で「自分の fire <id>」を確かめるため、
# 偽 pid では代用できない)。at は十分先、sleep は実物
spawn_sleeper() {  # $1=id  → SLEEPER_PID
  printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/$1.job"
  # sleeper 自身の stub 呼び出しは別ログへ。共有の $CALLS へ書かせると、起動が遅れたときに
  # 次のブロックの reset_calls の後から "sleep 3600" が現れ、無関係な assert を落とす
  # (make test の負荷下で実発生 2026-08-27)
  ( trap - EXIT; CALLS="$TMP_DIR/calls_sleeper.log" PATH="$STUB_PATH" STUB_REAL_SLEEP=1 exec "$SCRIPT" fire "$1" ) >/dev/null 2>&1 &
  SLEEPER_PID=$!; FAKE_PIDS+=("$SLEEPER_PID")
  local i=0; while [[ ! -s "$TMUX_SCHEDULE_KEYS_DIR/$1.pid" && $i -lt 50 ]]; do /bin/sleep 0.1; i=$((i+1)); done
  [[ "$(cat "$TMUX_SCHEDULE_KEYS_DIR/$1.pid")" == "$SLEEPER_PID" ]] || { printf '✗ sleeper %s の .pid が書かれない\n' "$1"; exit 1; }
}
printf '## wizard: UI の結果で予約を作る\n'
reset_state
STUB_UI_RESULT="new	4600	make test" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$RC" -eq 0 ]] || { printf '✗ wizard が exit %s\n' "$RC"; cat "$RUN_ERR"; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ job が 1 件でない (%s)\n' "$(jobs_count)"; exit 1; }
job="$(ls "$TMUX_SCHEDULE_KEYS_DIR"/*.job)"; id="$(basename "$job" .job)"
[[ "$(sed -n 1p "$job")" == "%5" ]] || { printf '✗ job の pane が固定した %%5 でない: %s\n' "$(sed -n 1p "$job")"; exit 1; }
[[ "$(sed -n 2p "$job")" == "4600" ]] || { printf '✗ 発火 epoch が UI の返した値でない: %s\n' "$(sed -n 2p "$job")"; exit 1; }
[[ "$(sed -n 3p "$job")" == "make test" ]] || { printf '✗ 文字列が保存されていない: %s\n' "$(sed -n 3p "$job")"; exit 1; }
printf '✓ job = 固定 pane / UI の epoch / 文字列 (空白保持)\n'
assert_called "tmux run-shell -b '$SCRIPT' fire '$id'" "sleeper を run-shell -b (サーバの子) として起動"
assert_called "schedkeys --label" "対話 UI (bin/schedkeys) に送り先の表示名を渡して起動"
assert_not_called "gum input" "文字列・時刻の入力は UI が持つ (gum は使わない)"

printf '\n## wizard: 中止・壊れた結果では何も作らない\n'
for tc in "exit:UI が中止 (Esc)" "empty:結果が空" "garbage:未知の action" "badepoch:epoch が数値でない" "notext:文字列が空"; do
  reset_state
  case "${tc%%:*}" in
    exit)     STUB_UI_EXIT=1 STUB_UI_RESULT="new	4600	make test" run "$STUB_PATH" "$SCRIPT" wizard ;;
    empty)    STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard ;;
    garbage)  STUB_UI_RESULT="destroy	all" run "$STUB_PATH" "$SCRIPT" wizard ;;
    badepoch) STUB_UI_RESULT="new	soon	make test" run "$STUB_PATH" "$SCRIPT" wizard ;;
    notext)   STUB_UI_RESULT="new	4600	" run "$STUB_PATH" "$SCRIPT" wizard ;;
  esac
  [[ "$(jobs_count)" == 0 ]] || { printf '✗ %s なのに job が出来た\n' "${tc#*:}"; exit 1; }
  assert_not_called "run-shell" "${tc#*:} → sleeper を起こさない"
done

printf '\n## wizard: UI へ渡す一覧が job と一致する\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper listed
STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard
[[ -f "$STUB_UI_JOBS_COPY" ]] || { printf '✗ UI に --jobs が渡っていない\n'; exit 1; }
[[ "$(wc -l < "$STUB_UI_JOBS_COPY" | tr -d ' ')" == 1 ]] || { printf '✗ 一覧の行数が 1 でない:\n%s\n' "$(cat "$STUB_UI_JOBS_COPY")"; exit 1; }
IFS=$'\t' read -r f_id f_at f_label f_text < "$STUB_UI_JOBS_COPY"
[[ "$f_id" == "listed" && "$f_at" == "$(sed -n 2p "$TMUX_SCHEDULE_KEYS_DIR/listed.job")" && "$f_label" == "main:3 claude" && "$f_text" == "make test" ]] \
  || { printf '✗ 一覧の TSV が job と一致しない: %s\n' "$(cat "$STUB_UI_JOBS_COPY")"; exit 1; }
printf '✓ 一覧 TSV = id / 発火 epoch / 送り先の表示名 / 文字列\n'
kill "$SLEEPER_PID" 2>/dev/null || true

printf '\n## fire: 送信\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n900\nEnter C-c \\ "q"\n' > "$TMUX_SCHEDULE_KEYS_DIR/j1.job"   # 発火時刻は過去
run "$STUB_PATH" "$SCRIPT" fire j1
[[ "$RC" -eq 0 ]] || { printf '✗ fire が exit %s\n' "$RC"; exit 1; }
assert_called 'tmux send-keys -t %5 -l -- Enter C-c \ "q"' "リテラル送信 (-l): \"Enter\" がキー名に化けない"
assert_called 'tmux send-keys -t %5 Enter' "末尾の Enter は別呼び出し"
first_sk="$(grep 'send-keys' "$CALLS" | head -n1)"
[[ "$first_sk" == *' -l -- '* ]] || { printf '✗ -l 送信が Enter より先でない: %s\n' "$first_sk"; exit 1; }
printf '✓ 文字列 → Enter の順\n'
[[ "$(jobs_count)" == 0 && ! -f "$TMUX_SCHEDULE_KEYS_DIR/j1.pid" ]] || { printf '✗ 送信後に job/pid が残っている\n'; exit 1; }
printf '✓ 送信後は job/pid を掃く\n'
assert_not_called "sleep" "発火時刻を過ぎていれば待たない"

printf '\n## fire: 発火時刻まで送らない (実 sleep)\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n%s\nls\n' "$(( $(/bin/date +%s) + 2 ))" > "$TMUX_SCHEDULE_KEYS_DIR/j2.job"
( trap - EXIT; PATH="$STUB_PATH" STUB_REAL_SLEEP=1 exec "$SCRIPT" fire j2 ) >/dev/null 2>&1 &
fire_pid=$!
/bin/sleep 0.5
assert_not_called "send-keys" "起動 0.5 秒後: まだ送っていない"
[[ "$(cat "$TMUX_SCHEDULE_KEYS_DIR/j2.pid" 2>/dev/null)" == "$fire_pid" ]] || { printf '✗ .pid が sleeper 自身の pid でない\n'; exit 1; }
printf '✓ .pid = sleeper の pid (取消の kill 先)\n'
wait "$fire_pid" || { printf '✗ fire が非 0 で終了\n'; exit 1; }
assert_called 'tmux send-keys -t %5 -l -- ls' "発火時刻後に送信"

printf '\n## fire: 壊れた job (文字列行なし) は送らず掃く\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n900\n' > "$TMUX_SCHEDULE_KEYS_DIR/j4.job"
run "$STUB_PATH" "$SCRIPT" fire j4
[[ "$RC" -eq 0 && ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ 壊れた job で無音 exit 0 でない (RC=%s)\n' "$RC"; exit 1; }
assert_not_called "send-keys" "文字列行の無い job → 素の Enter を送らない"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 壊れた job が残っている\n'; exit 1; }
printf '✓ 壊れた job は掃く\n'

printf '\n## fire: 送り先 pane 消滅 → 無音で破棄\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n900\nmake test\n' > "$TMUX_SCHEDULE_KEYS_DIR/j3.job"
rm -f "$XDG_CACHE_HOME/tt-schedule-keys.log"   # 前ブロックの成功ログを持ち越さない
STUB_PANE_GONE=1 run "$STUB_PATH" "$SCRIPT" fire j3
[[ "$RC" -eq 0 ]] || { printf '✗ pane 消滅で exit %s (run-shell 経由なら view-mode が積まれる)\n' "$RC"; exit 1; }
[[ ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ pane 消滅で stdout/stderr に出力 (無音契約違反)\n'; cat "$RUN_OUT" "$RUN_ERR"; exit 1; }
assert_called 'tmux send-keys -t %5 -l -- make test' "pane 消滅は send-keys の失敗で検知する (事前チェックとの TOCTOU を作らない)"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ pane 消滅の job が残っている\n'; exit 1; }
grep -q 'pane %5 gone' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 破棄がログに残らない\n'; exit 1; }
! grep -q 'sent to' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 送れていないのに成功ログ\n'; exit 1; }
assert_not_called "send-keys -t %5 Enter" "文字列の送信に失敗したら Enter も送らない"
printf '✓ 無音 exit 0 + job 掃除 + ログ記録 (成功ログ無し)\n'

printf '\n## cancel: UI が選んだ予約を確認してから取り消す\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
STUB_GUM_EXIT=0 STUB_UI_RESULT="cancel	live" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$RC" -eq 0 ]] || { printf '✗ wizard (cancel) が exit %s\n' "$RC"; cat "$RUN_ERR"; exit 1; }
/bin/sleep 0.3
kill -0 "$live_pid" 2>/dev/null && { printf '✗ 取消したのに sleeper が生きている\n'; exit 1; }
[[ "$(jobs_count)" == 0 && ! -f "$TMUX_SCHEDULE_KEYS_DIR/live.pid" ]] || { printf '✗ 取消後に job/pid が残っている\n'; exit 1; }
printf '✓ 取消 → sleeper kill + job/pid 削除\n'
grep -E '^gum confirm .*--default=false' "$CALLS" >/dev/null || { printf '✗ 取消確認が --default=false でない\n'; exit 1; }
printf '✓ 取消確認は --default=false (Enter 素通しでは消えない)\n'

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
STUB_GUM_EXIT=1 STUB_UI_RESULT="cancel	live" run "$STUB_PATH" "$SCRIPT" wizard
kill -0 "$live_pid" 2>/dev/null || { printf '✗ 取消を拒否したのに sleeper が死んだ\n'; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 取消拒否で job が消えた\n'; exit 1; }
printf '✓ 取消拒否 → 何もしない\n'
kill "$live_pid" 2>/dev/null || true

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
STUB_UI_RESULT="cancel	live" run "$NOGUM_PATH" "$SCRIPT" wizard
kill -0 "$live_pid" 2>/dev/null || { printf '✗ gum 未導入なのに取り消された (fail-safe の && 短絡)\n'; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ gum 未導入で job が消えた\n'; exit 1; }
printf '✓ gum 未導入 (exit 127) → 取り消さない\n'
kill "$live_pid" 2>/dev/null || true

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
STUB_GUM_EXIT=0 STUB_UI_RESULT="cancel	no-such-id" run "$STUB_PATH" "$SCRIPT" wizard
kill -0 "$live_pid" 2>/dev/null || { printf '✗ 存在しない id の取消で無関係な sleeper が死んだ\n'; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 存在しない id の取消で job が消えた\n'; exit 1; }
assert_not_called "gum confirm" "存在しない id → 確認まで進まない"
printf '✓ 存在しない id の取消は何もしない\n'
kill "$live_pid" 2>/dev/null || true

printf '\n## prune: sleeper が居ない予約だけを掃く\n'
# pid 再利用: .pid が無関係な生きたプロセスを指す → kill せず、stale として掃く
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_spawn_fake_proc; foreign_pid=$REPLY_PID
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/reused.job"
printf '%s\n' "$foreign_pid" > "$TMUX_SCHEDULE_KEYS_DIR/reused.pid"
STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard
kill -0 "$foreign_pid" 2>/dev/null || { printf '✗ 無関係なプロセス (pid 再利用) を kill した\n'; exit 1; }
[[ "$(jobs_count)" == 0 ]] || { printf '✗ sleeper 不在 (pid 再利用) の job が掃かれない\n'; exit 1; }
[[ ! -s "$STUB_UI_JOBS_COPY" ]] || { printf '✗ 掃いた job が UI の一覧に出ている\n'; exit 1; }
printf '✓ 無関係なプロセスは kill せず、その job は掃く\n'

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_free_pid; dead_pid=$REPLY_PID
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/stale.job"
printf '%s\n' "$dead_pid" > "$TMUX_SCHEDULE_KEYS_DIR/stale.pid"
STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$RC" -eq 0 && "$(jobs_count)" == 0 ]] || { printf '✗ pid が死んだ stale job が掃かれない\n'; exit 1; }
printf '✓ stale job (pid 死亡) を掃く\n'

# .pid 未作成 + 作成直後の job は掃かない (fire が書く前の猶予)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/fresh.job"
STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 作成直後 (.pid 未作成) の job が掃かれた\n'; exit 1; }
printf '✓ 作成直後の .pid 未作成 job は掃かない\n'

printf '\n## 表示文字列に絵文字・曖昧幅の記号が無い (幅計算のずれでノイズになる)\n'
# 検査対象は「コードの引用文字列」(コメントは除く)。絵文字ブロック + 記号ブロック + 矢印・中黒 + VS16。
# シェルと Go (UI) の両方を見る: 片方だけだと UI 側に絵文字が戻ってきても気づけない。
# 判定は perl -CSD (BSD/GNU grep の -P と UTF モードの差に依存しない)
WIDE_RE='[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{2300}-\x{23FF}\x{2190}-\x{21FF}\x{00B7}\x{FE0F}]'
ui_sources=("$ROOT_DIR"/src/schedkeys/*.go)
[[ -f "${ui_sources[0]}" ]] || { printf '✗ UI のソースが見つからない (検査が空振り)\n'; exit 1; }
for f in "$SCRIPT" "${ui_sources[@]}"; do
  [[ "$f" == *_test.go ]] && continue
  code_strings="$(grep -vE '^[[:space:]]*(#|//)' "$f" | grep -oE '"[^"]*"|'"'"'[^'"'"']*'"'"'' || true)"
  [[ -n "$code_strings" ]] || { printf '✗ %s から引用文字列が抽出できない (検査が空振り)\n' "$f"; exit 1; }
  bad="$(printf '%s\n' "$code_strings" | perl -CSD -ne "print if /$WIDE_RE/")"
  [[ -z "$bad" ]] || { printf '✗ %s の表示文字列に絵文字/曖昧幅の記号:\n%s\n' "$f" "$bad"; exit 1; }
done
printf '✓ シェルと UI (src/schedkeys) の引用文字列に絵文字・曖昧幅記号なし\n'
bad="$(grep -E '^bind (m|Enter|C-m) ' "$CONF" | perl -CSD -ne "print if /$WIDE_RE/")"
[[ -z "$bad" ]] || { printf '✗ popup タイトル (bind) に絵文字:\n%s\n' "$bad"; exit 1; }
printf '✓ popup タイトルに絵文字なし\n'

printf '\n## UI (Go) の配線\n'
grep -q 'bin/schedkeys' "$SCRIPT" || { printf '✗ シェルが bin/schedkeys を参照していない\n'; exit 1; }
[[ -x "$ROOT_DIR/bin/schedkeys" ]] || { printf '✗ bin/schedkeys が無い / 実行不可\n'; exit 1; }
grep -q 'go_autobuild_exec "' "$ROOT_DIR/bin/schedkeys" || { printf '✗ bin/schedkeys が go_autobuild 経由でない\n'; exit 1; }
grep -q 'go_autobuild_exec --async' "$ROOT_DIR/bin/schedkeys" && { printf '✗ UI のビルドが --async (古い UI の結果を新コードの結果と誤認する)\n'; exit 1; }
printf '✓ シェル → bin/schedkeys (同期ビルド) → src/schedkeys\n'

printf '\n## _tmux.conf: bind m / Enter / C-m が同じウィザードを指す\n'
for k in m Enter C-m; do
  grep -Eq "^bind $k +display-popup .*tmux_schedule_keys\.sh" "$CONF" || { printf '✗ bind %s が tmux_schedule_keys.sh を指していない\n' "$k"; exit 1; }
  printf '✓ bind %s → display-popup → tmux_schedule_keys.sh\n' "$k"
done
# 旧 launcher (display-menu) が復活していないこと。prefix+Enter の席はウィザードが上書きしている
! grep -Eq '^bind(-key)? +(-T prefix +)?Enter +display-menu' "$CONF" || { printf '✗ prefix+Enter に launcher (display-menu) が復活している\n'; exit 1; }
printf '✓ prefix+Enter は launcher ではない\n'

printf '\n[test-schedule-keys] all ok\n'
