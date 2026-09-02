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
#   - mode (copy-mode 等) に入っている pane へは、抜けてから送る (mode 中はキーが届かない)
#   - .pid の数字だけで kill しない: 無関係なプロセスが同じ pid を持っていても kill せず、stale として掃く
#   - 表示文字列 (シェル / UI とも) に絵文字・曖昧幅の記号を混ぜない
#   - tmux の prefix を UI へ渡す (popup 中は prefix が UI に素通りするので、そこで閉じる)
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
# ⚠️ XDG_STATE_HOME を隔離する: 実行環境に設定があると STATE_ROOT がそちらへ向き、
#    「socket ごとの dir」の検査が TMP_DIR の外を見て false red になる上、job を外へ書き残す
#    (敵対的レビュー 2026-09-03 の P3-4 で実測)
unset XDG_STATE_HOME

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/bin_nogum"
# stub tmux: 呼び出しを記録し、スクリプトが使う照会にだけ応える。STUB_PANE_GONE=1 で %5 消滅を模す
cat > "$TMP_DIR/bin/tmux" <<'EOS2'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
[ -n "${TMUX:-}" ] && echo "env TMUX=$TMUX" >> "$CALLS"
case "$*" in
  "display-message -p #{socket_path}")
    # STUB_NO_SOCK=1 で「tmux が socket を答えない」を模す (空 + rc=0)
    [ "${STUB_NO_SOCK:-0}" = 1 ] && exit 0
    printf '%s\n' "${STUB_SOCK:-/tmp/sk-sock}" ;;
  "display-message -p #{pid}")
    if [ -n "${STUB_SRVPID_FILE:-}" ] && [ -f "$STUB_SRVPID_FILE" ]; then cat "$STUB_SRVPID_FILE"; else printf '%s\n' "${STUB_SRVPID:-4242}"; fi ;;
  "run-shell -b "*)
    # 本物の sleeper は起動直後に <id>.pid を書く。呼び出し側はそれを「起きた証拠」に使うので、
    # stub でも同じ形を作る (STUB_NO_SLEEPER=1 で「起きなかった」を模す)
    if [ "${STUB_NO_SLEEPER:-0}" != 1 ]; then
      sid=$(printf '%s' "$*" | sed -n "s/.*fire '\([^']*\)'.*/\1/p")
      # 置き場は run-shell のコマンド文字列に入っている (本物の fire もそこから受け取る)。
      # 無ければ env にフォールバック (dir を直接指定するテスト用の上書き経路)
      sdir=$(printf '%s' "$*" | sed -n "s/.*TMUX_SCHEDULE_KEYS_DIR='\([^']*\)'.*/\1/p")
      [ -n "$sdir" ] || sdir="${TMUX_SCHEDULE_KEYS_DIR:-}"
      [ -n "$sid" ] && [ -n "$sdir" ] && echo $$ > "$sdir/$sid.pid"
    fi
    exit "${STUB_RUNSHELL_EXIT:-0}" ;;
  "display-message -p #{pane_id}") printf '%%5\n' ;;
  "display-message -p -t %5 "*)
    # ⚠️ 実 tmux は消えた pane への問い合わせでも rc=0 を返し、window 名の部分が空の ": " になる
    #    (実測 2026-08-28)。rc=1 を返す stub は実物より厳しく、pane_label の縮退分岐を隠していた
    case "$*" in
      *pane_id*)      [ "${STUB_PANE_GONE:-0}" = 1 ] && { printf '\n'; exit 0; }; printf '%%5\n' ;;
      *pane_in_mode*) printf '%s\n' "${STUB_PANE_IN_MODE:-0}" ;;
      *)              [ "${STUB_PANE_GONE:-0}" = 1 ] && { printf ': \n'; exit 0; }
                      printf '%s\n' "${STUB_WINDOW_LABEL:-main:3 claude}" ;;
    esac ;;
  "show-options -gv prefix") printf 'C-t\n' ;;
  "send-keys -t %5 -l"*)
    # 実 tmux は pane 不在で stderr にエラーを出して rc=1 (この stderr が run-shell 経由で view-mode に積まれる)
    [ "${STUB_PANE_GONE:-0}" = 1 ] && { echo "can't find pane: %5" >&2; exit 1; }
    # 送信の途中に割り込む窓を作る (trap のテスト用)
    [ -n "${STUB_SEND_DELAY:-}" ] && /bin/sleep "$STUB_SEND_DELAY" ;;
  "send-keys -t %5 "*)
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
  confirm)
    # 確認を読んでいる間に発火した状況を作る (fire は送信前に job を消す)
    [ -n "${STUB_GUM_FIRE_JOB:-}" ] && rm -f "$STUB_GUM_FIRE_JOB"
    # sleeper だけが後始末をせずに死んだ状況 (crash / OOM)
    [ -n "${STUB_GUM_KILL_PID:-}" ] && kill -9 "$STUB_GUM_KILL_PID" 2>/dev/null
    exit "${STUB_GUM_EXIT:-1}" ;;
  style)   shift; while [ $# -gt 0 ] && [ "${1#-}" != "$1" ]; do shift 2; done; printf '%s\n' "$*"; exit 0 ;;
esac
exit 0
EOS2
chmod +x "$TMP_DIR/bin/gum"

# stub schedkeys (対話 UI): 渡された --jobs を控え、結果を --out へ書く。
# 1 回きりなら STUB_UI_RESULT、対話の流れ (取消 → 一覧 → 閉じる 等) は STUB_UI_RESULTS に
# 改行区切りで積む (1 起動につき 1 行を消費し、尽きたら "abort")。
# ⚠️ 同じ結果を返し続ける stub にしないこと: 呼び出し側が「取消したら一覧へ戻る」ループを持つので、
#    実際に無限に回った (2026-08-28)。対話の終わりまで模す
# 中止は out の "abort"、UI が動かない場合は STUB_UI_EXIT で 0 以外を返す
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
[ -n "$jobs" ] && [ -f "$jobs" ] && cp "$jobs" "$STUB_UI_JOBS_COPY.$$" && mv "$STUB_UI_JOBS_COPY.$$" "$STUB_UI_JOBS_COPY"
[ -n "$jobs" ] && [ -f "$jobs" ] && cat "$jobs" >> "$STUB_UI_JOBS_ALL"
result="${STUB_UI_RESULT:-}"
if [ -n "${STUB_UI_QUEUE:-}" ]; then
  if [ -s "$STUB_UI_QUEUE" ]; then
    result="$(head -n1 "$STUB_UI_QUEUE")"
    tail -n +2 "$STUB_UI_QUEUE" > "$STUB_UI_QUEUE.tmp" && mv "$STUB_UI_QUEUE.tmp" "$STUB_UI_QUEUE"
  else
    result="abort"
  fi
fi
# 中止でも out に中身を残す形を模す (mktemp の使い回し・途中書き)。呼び出し側は
# 「終了コードが非 0 なら結果を使わない」ことで守る
printf '%s\n' "$result" > "$out"
exit "${STUB_UI_EXIT:-0}"
EOS2
chmod +x "$TMP_DIR/bin/schedkeys"
cp "$TMP_DIR/bin/schedkeys" "$TMP_DIR/bin_nogum/schedkeys"

# ⚠️ date は stub しない。以前 STUB_NOW / STUB_NOW_HM で固定する stub があったが、どこからも
# 設定されておらず (監査 2026-08-28)、「決定論化している」というコメントだけが残っていた。
# 時刻に依存する fixture は実時刻からの相対 (+3600 等) と touch による back-date で作る。

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
export STUB_UI_JOBS_ALL="$TMP_DIR/ui_jobs_all.tsv"
export STUB_UI_QUEUE=""

# ui_queue は「UI が起動ごとに返す結果」を積む (対話の流れを模す)。
ui_queue() {
  STUB_UI_QUEUE="$TMP_DIR/ui_queue"
  : > "$STUB_UI_QUEUE"
  local r
  for r in "$@"; do printf '%s\n' "$r" >> "$STUB_UI_QUEUE"; done
}
RUN_OUT="$TMP_DIR/out.log"; RUN_ERR="$TMP_DIR/err.log"
# shellcheck source=tests/tmux/lib/stub_assert_helper.sh
. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
jobs_count() { find "$TMUX_SCHEDULE_KEYS_DIR" -name '*.job' 2>/dev/null | wc -l | tr -d ' '; }
reset_state() {
  rm -rf "$TMUX_SCHEDULE_KEYS_DIR"
  rm -f "$STUB_UI_JOBS_COPY" "$STUB_UI_JOBS_ALL"
  STUB_UI_QUEUE=""
  reset_calls
}

# shellcheck source=tests/tmux/lib/stub_env.sh
. "$ROOT_DIR/tests/tmux/lib/stub_env.sh"
# 本物の sleeper を起こす (pid_is_sleeper は ps の command line で「自分の fire <id>」を確かめるため、
# 偽 pid では代用できない)。at は十分先、sleep は実物
# write_job は job の書式を 1 箇所に集約する。⚠️ socket と サーバ pid は **stub が返す既定値から
# 導く**: ここに直値を書くと、stub 側の既定を変えたときに fire_claim が全 fixture を fail-closed で
# 弾き、assert_not_called だけのブロックが「別の理由で緑」になる (監査 2026-08-28)
write_job() {  # $1=id $2=発火 epoch $3=文字列 [$4=socket] [$5=server pid]
  printf '%%5\n%s\n%s\n%s\n%s\n' "$2" "$3" "${4-${STUB_SOCK:-/tmp/sk-sock}}" "${5-${STUB_SRVPID:-4242}}" \
    > "$TMUX_SCHEDULE_KEYS_DIR/$1.job"
}

spawn_sleeper() {  # $1=id  → SLEEPER_PID
  write_job "$1" "$(( $(/bin/date +%s) + 3600 ))" "make test"
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
[[ "$(sed -n 4p "$job")" == "/tmp/sk-sock" ]] || { printf '✗ socket が保存されていない: %s\n' "$(sed -n 4p "$job")"; exit 1; }
[[ "$(sed -n 5p "$job")" == "4242" ]] || { printf '✗ サーバ pid が保存されていない: %s\n' "$(sed -n 5p "$job")"; exit 1; }
printf '✓ job に socket とサーバ pid が残る (サーバが入れ替わったら送らないため)\n'
printf '✓ job = 固定 pane / UI の epoch / 文字列 (空白保持)\n'
assert_called "tmux run-shell -b TMUX_SCHEDULE_KEYS_DIR='$TMUX_SCHEDULE_KEYS_DIR' '$SCRIPT' fire '$id'" \
  "sleeper を run-shell -b (サーバの子) として起動し、置き場を env で渡す (fire は自分で解決できない)"
assert_called "schedkeys --label" "対話 UI (bin/schedkeys) に送り先の表示名を渡して起動"
assert_not_called "gum input" "文字列・時刻の入力は UI が持つ (gum は使わない)"
# popup 中は prefix が tmux のキーテーブルへ届かず UI に素通りするので、起動キーの再入力で
# 閉じられるよう prefix を UI へ渡す (実測 2026-08-28)
assert_called "--toggle-prefix C-t" "tmux の prefix を UI へ渡す (起動キーの再入力で閉じるため)"
# 成功の通知は UI のトーストが出す (シェルは二重に出さない)。失敗したときだけ知らせる
assert_not_called "display-message 予約" "成功時にシェルからも通知しない (UI のトーストと二重になる)"

printf '\n## wizard: 予約を作れなかったら失敗を知らせる (UI は「予約しました」と出して閉じている)\n'
reset_state
chmod 500 "$TMUX_SCHEDULE_KEYS_DIR" 2>/dev/null || mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
chmod 500 "$TMUX_SCHEDULE_KEYS_DIR"
STUB_UI_RESULT="new	4600	make test" run "$STUB_PATH" "$SCRIPT" wizard
chmod 700 "$TMUX_SCHEDULE_KEYS_DIR"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 書けないはずの状態で job が出来た\n'; exit 1; }
assert_called "display-message 予約に失敗しました" "job を作れなかったら失敗を知らせる"
assert_not_called "run-shell" "作れなかったら sleeper も起こさない"

printf '\n## wizard: 中止と「UI が動かない」を分ける\n'
# ⚠️ 中止は out の "abort"、異常は 0 以外の rc。一緒くたにすると、ビルド失敗やバイナリ不在が
# 「ユーザーが閉じた」と同じ扱いになり、押しても何も起きないキーになる (監査 2026-08-28)
reset_state
STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 中止で job が出来た\n'; exit 1; }
# 通知は「-p の無い display-message」。-p は問い合わせ (pane 名の取得) なので数えない
if grep -E '^tmux display-message [^-]' "$CALLS" >/dev/null; then
  printf '✗ 中止なのに通知を出した:\n%s\n' "$(grep -E '^tmux display-message [^-]' "$CALLS")"; exit 1
fi
printf '✓ 中止は黙って閉じる\n'
reset_state
STUB_UI_EXIT=127 STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard
assert_called "display-message 予約入力の画面を開けませんでした" "UI が動かないときは理由を知らせる"
grep -q 'ui: 起動できない (rc=127)' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ UI の失敗がログに残らない\n'; exit 1; }
printf '✓ UI の起動失敗はログと通知の両方に残る\n'

printf '\n## wizard: 中止・壊れた結果では何も作らない\n'
for tc in "exit:UI が中止 (abort)" "empty:結果が空" "garbage:未知の action" "badepoch:epoch が数値でない" "notext:文字列が空"; do
  reset_state
  case "${tc%%:*}" in
    exit)     STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard ;;
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
printf '%%5\n900\nEnter C-c \\ "q"\n/tmp/sk-sock\n4242\n' > "$TMUX_SCHEDULE_KEYS_DIR/j1.job"   # 発火時刻は過去
run "$STUB_PATH" "$SCRIPT" fire j1
[[ "$RC" -eq 0 ]] || { printf '✗ fire が exit %s\n' "$RC"; exit 1; }
# ⚠️ 本文と Enter は 1 回の呼び出しで送る。分けると、同時刻に発火した別の予約が割り込んで
# pane で 2 つの文字列が 1 行に連結される (実測 2026-08-28)
assert_called 'tmux send-keys -t %5 -l -- Enter C-c \ "q" ; send-keys -t %5 Enter' "本文と Enter を 1 回の tmux 呼び出しで送る (割り込ませない)"
[[ "$(grep -c 'send-keys' "$CALLS")" == 1 ]] || { printf '✗ send-keys が 2 回に分かれている:\n%s\n' "$(grep 'send-keys' "$CALLS")"; exit 1; }
printf '✓ 送信は 1 回の呼び出し\n'
[[ "$(jobs_count)" == 0 && ! -f "$TMUX_SCHEDULE_KEYS_DIR/j1.pid" ]] || { printf '✗ 送信後に job/pid が残っている\n'; exit 1; }
printf '✓ 送信後は job/pid を掃く\n'
assert_not_called "sleep" "発火時刻を過ぎていれば待たない"

printf '\n## fire: 発火時刻まで送らない (実 sleep)\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job j2 "$(( $(/bin/date +%s) + 2 ))" "ls"
( trap - EXIT; PATH="$STUB_PATH" STUB_REAL_SLEEP=1 exec "$SCRIPT" fire j2 ) >/dev/null 2>&1 &
fire_pid=$!
/bin/sleep 0.5
assert_not_called "send-keys" "起動 0.5 秒後: まだ送っていない"
[[ "$(cat "$TMUX_SCHEDULE_KEYS_DIR/j2.pid" 2>/dev/null)" == "$fire_pid" ]] || { printf '✗ .pid が sleeper 自身の pid でない\n'; exit 1; }
printf '✓ .pid = sleeper の pid (取消の kill 先)\n'
wait "$fire_pid" || { printf '✗ fire が非 0 で終了\n'; exit 1; }
assert_called 'tmux send-keys -t %5 -l -- ls' "発火時刻後に送信"

printf '\n## fire: 予約したサーバでなければ送らない\n'
# サーバが異常終了すると sleeper だけ生き残り、同じ socket に立った別サーバの pane へ届く
# (pane id は振り直されるので「存在しない」に逃げられない)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job j7 900 "make test"
STUB_SRVPID=9999 run "$STUB_PATH" "$SCRIPT" fire j7   # 別サーバが立っている
# ⚠️ fire は冒頭で exec </dev/null >/dev/null 2>&1 する (issue 129) ので、stdout/stderr が空なのは
#    **構造的に真**であり、この行の主役は rc=0 の方 (view-mode を積む支配的な要因は rc≠0。111 の実測)。
#    リダイレクトそのものは tests/tmux/test_runshell_silence.sh が「実処理より前にあるか」で守る
[[ "$RC" -eq 0 && ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ 無音 exit 0 でない (RC=%s)\n' "$RC"; exit 1; }
assert_not_called "send-keys" "予約したサーバが居なければ送らない (別サーバの pane を叩かない)"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 破棄した job が残っている\n'; exit 1; }
grep -q '予約したサーバを確かめられない' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 破棄の理由がログに残らない\n'; exit 1; }
printf '✓ サーバが入れ替わっていたら破棄する\n'
# ⚠️ 判定は「眠る前」ではなく「起きた直後・送る直前」であること。眠る前に見ても、壊れる経路
#    (眠っている間にサーバが死んで別のサーバが立つ) を検出できない (実機で確認 2026-08-28)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
export STUB_SRVPID_FILE="$TMP_DIR/srvpid"; echo 4242 > "$STUB_SRVPID_FILE"
write_job j9 "$(( $(/bin/date +%s) + 2 ))" "make test"
( trap - EXIT; CALLS="$CALLS" PATH="$STUB_PATH" STUB_REAL_SLEEP=1 STUB_SRVPID_FILE="$STUB_SRVPID_FILE" exec "$SCRIPT" fire j9 ) >/dev/null 2>&1 &
j9pid=$!; FAKE_PIDS+=("$j9pid")
/bin/sleep 0.5; echo 9999 > "$STUB_SRVPID_FILE"   # 眠っている間にサーバが入れ替わった
wait "$j9pid" 2>/dev/null || true
assert_not_called "send-keys -t %5 -l" "眠っている間にサーバが入れ替わったら送らない (判定は送る直前)"
unset STUB_SRVPID_FILE
printf '✓ サーバの同一性は送る直前に見る\n'
# 旧形式 (サーバ pid を持たない job) は「確かめられない」ので送らない (fail-open にしない)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n900\nmake test\n/tmp/sk-sock\n' > "$TMUX_SCHEDULE_KEYS_DIR/legacy.job"
run "$STUB_PATH" "$SCRIPT" fire legacy
assert_not_called "send-keys" "サーバ pid を持たない job は送らない (確かめられないものを送らない)"
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job j8 900 "make test"
STUB_SRVPID=4242 run "$STUB_PATH" "$SCRIPT" fire j8   # 同じサーバ
assert_called "tmux send-keys -t %5 -l -- make test ; send-keys -t %5 Enter" "同じサーバなら送る"
grep -q 'env TMUX=/tmp/sk-sock,0,0' "$CALLS" || { printf '✗ 予約時の socket を $TMUX に載せていない\n'; cat "$CALLS"; exit 1; }
printf '✓ 予約時の socket へ向ける ($TMUX)\n'

printf '\n## fire: 送信の途中で TERM が来ても両方送る\n'
# 文字列だけ打たれて Enter が届かないと、pane に中途半端なコマンドが残る。送信直前に TERM/INT/HUP を
# 無視するのはそのため (取消が間に合わなかったときの半端送信を防ぐ)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job trapped 900 "make test"
( trap - EXIT; CALLS="$CALLS" PATH="$STUB_PATH" STUB_SEND_DELAY=1 exec "$SCRIPT" fire trapped ) >/dev/null 2>&1 &
trap_pid=$!; FAKE_PIDS+=("$trap_pid")
/bin/sleep 0.5; kill -TERM "$trap_pid" 2>/dev/null   # 1 回目の送信中に割り込む
wait "$trap_pid" 2>/dev/null || true
assert_called "tmux send-keys -t %5 -l -- make test ; send-keys -t %5 Enter" "TERM が来ても本文と Enter を送り切る (半端な入力を残さない)"

printf '\n## fire: 眠っている間に取り消されたら送らない\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job canc "$(( $(/bin/date +%s) + 2 ))" "make test"
( trap - EXIT; CALLS="$CALLS" PATH="$STUB_PATH" STUB_REAL_SLEEP=1 exec "$SCRIPT" fire canc ) >/dev/null 2>&1 &
canc_pid=$!; FAKE_PIDS+=("$canc_pid")
/bin/sleep 0.5; rm -f "$TMUX_SCHEDULE_KEYS_DIR/canc.job"   # 眠っている間に取り消された
wait "$canc_pid" 2>/dev/null || true
assert_not_called "send-keys -t %5 -l" "job が消えていたら送らない (kill が間に合わなくても止まる)"
# ⚠️ 送らずに抜けるときも .pid を残さない (prune は *.job しか見ないので、残ると誰も回収しない)
[[ -z "$(find "$TMUX_SCHEDULE_KEYS_DIR" -name '*.pid' 2>/dev/null)" ]] || { printf '✗ 送らずに抜けたのに .pid が残っている\n'; ls "$TMUX_SCHEDULE_KEYS_DIR"; exit 1; }
printf '✓ 送らずに抜けても後片付けする\n'

printf '\n## fire: 時刻が取れなくても無音で終わる (無音契約)\n'
# date が壊れている環境でも stdout/stderr へ出さない (run-shell の子の出力は view-mode に積まれる)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job nodate 900 "make test"
cat > "$TMP_DIR/bin_nodate_date" <<'EOS2'
#!/bin/sh
exit 0
EOS2
chmod +x "$TMP_DIR/bin_nodate_date"
mkdir -p "$TMP_DIR/bin_nodate"; cp "$TMP_DIR/bin/tmux" "$TMP_DIR/bin/sleep" "$TMP_DIR/bin_nodate/" 2>/dev/null
cp "$TMP_DIR/bin_nodate_date" "$TMP_DIR/bin_nodate/date"
run "$TMP_DIR/bin_nodate:/usr/bin:/bin" "$SCRIPT" fire nodate
# ⚠️ fire は冒頭で exec </dev/null >/dev/null 2>&1 する (issue 129) ので、stdout/stderr が空なのは
#    **構造的に真**であり、この行の主役は rc=0 の方 (view-mode を積む支配的な要因は rc≠0。111 の実測)。
#    リダイレクトそのものは tests/tmux/test_runshell_silence.sh が「実処理より前にあるか」で守る
[[ "$RC" -eq 0 && ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ 時刻が取れないときに無音 exit 0 でない (RC=%s)\n' "$RC"; cat "$RUN_ERR"; exit 1; }
assert_not_called "send-keys" "時刻が取れなければ送らない"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 破棄した job が残っている\n'; exit 1; }
printf '✓ 時刻が取れなくても無音で破棄する\n'

printf '\n## fire: 末尾の ; が食われない\n'
# tmux は引数の末尾の ; をコマンド区切りとして食う (-- では守れない)。最後の 1 個だけ \; にする
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job semi 900 "echo hello ;"
run "$STUB_PATH" "$SCRIPT" fire semi
assert_called 'tmux send-keys -t %5 -l -- echo hello \; ; send-keys -t %5 Enter' "末尾の ; をエスケープして送る"
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job semi2 900 "echo a ; echo b"
run "$STUB_PATH" "$SCRIPT" fire semi2
assert_called 'tmux send-keys -t %5 -l -- echo a ; echo b ; send-keys -t %5 Enter' "途中の ; は素通し (escape するとバックスラッシュが残る)"

printf '\n## fire: mode 中の pane は抜けてから送る\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job j5 900 "make test"
STUB_PANE_IN_MODE=1 run "$STUB_PATH" "$SCRIPT" fire j5
assert_called "tmux send-keys -t %5 -X cancel" "copy-mode 等に入っていたら抜けてから送る (mode 中はキーが届かない)"
# 順序: cancel が literal 送信より先
first_sk="$(grep 'send-keys' "$CALLS" | head -n1)"
[[ "$first_sk" == *"-X cancel"* ]] || { printf '✗ mode を抜けるのが送信より後: %s\n' "$first_sk"; exit 1; }
printf '✓ mode を抜けてから送る\n'
assert_called "tmux send-keys -t %5 -l -- make test" "抜けたあと本文を送る"
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job j6 900 "make test"
STUB_PANE_IN_MODE=0 run "$STUB_PATH" "$SCRIPT" fire j6
assert_not_called "-X cancel" "mode に入っていなければ余計な cancel を送らない"

printf '\n## fire: 壊れた job (文字列行なし) は送らず掃く\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job j4 900 ""
run "$STUB_PATH" "$SCRIPT" fire j4
# ⚠️ fire は冒頭で exec </dev/null >/dev/null 2>&1 する (issue 129) ので、stdout/stderr が空なのは
#    **構造的に真**であり、この行の主役は rc=0 の方 (view-mode を積む支配的な要因は rc≠0。111 の実測)。
#    リダイレクトそのものは tests/tmux/test_runshell_silence.sh が「実処理より前にあるか」で守る
[[ "$RC" -eq 0 && ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ 壊れた job で無音 exit 0 でない (RC=%s)\n' "$RC"; exit 1; }
assert_not_called "send-keys" "文字列行の無い job → 素の Enter を送らない"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 壊れた job が残っている\n'; exit 1; }
printf '✓ 壊れた job は掃く\n'

printf '\n## fire: 送り先 pane 消滅 → 無音で破棄\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job j3 900 "make test"
rm -f "$XDG_CACHE_HOME/tt-schedule-keys.log"   # 前ブロックの成功ログを持ち越さない
STUB_PANE_GONE=1 run "$STUB_PATH" "$SCRIPT" fire j3
[[ "$RC" -eq 0 ]] || { printf '✗ pane 消滅で exit %s (run-shell 経由なら view-mode が積まれる)\n' "$RC"; exit 1; }
# ⚠️ fire は冒頭で exec </dev/null >/dev/null 2>&1 する (issue 129) ので、stdout/stderr が空なのは
#    **構造的に真**であり、この行の主役は rc=0 の方 (view-mode を積む支配的な要因は rc≠0。111 の実測)。
#    リダイレクトそのものは tests/tmux/test_runshell_silence.sh が「実処理より前にあるか」で守る
[[ ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ pane 消滅で stdout/stderr に出力 (無音契約違反)\n'; cat "$RUN_OUT" "$RUN_ERR"; exit 1; }
assert_called 'tmux send-keys -t %5 -l -- make test ; send-keys -t %5 Enter' "pane 消滅は send-keys の失敗で検知する (事前チェックとの TOCTOU を作らない)"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ pane 消滅の job が残っている\n'; exit 1; }
grep -q 'へ送れず破棄' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 破棄がログに残らない\n'; exit 1; }
! grep -q 'sent to' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 送れていないのに成功ログ\n'; exit 1; }
# 本文と Enter は 1 呼び出しなので、失敗しても「本文だけ入った」状態は構造的に起きない
[[ "$(grep -c 'send-keys' "$CALLS")" == 1 ]] || { printf '✗ 送信が 2 回に分かれている\n'; exit 1; }
printf '✓ 送信に失敗しても半端な入力を残さない (1 呼び出し)\n'
printf '✓ 無音 exit 0 + job 掃除 + ログ記録 (成功ログ無し)\n'

printf '\n## wizard: sleeper を起こせなかったら job を残さない\n'
# ⚠️ tmux の run-shell -b は子の失敗を rc に返さない (exec 失敗も exit 1 も rc=0。実測 2026-08-28)。
# 「起きた証拠」= sleeper が書く .pid で判定していること
reset_state
STUB_NO_SLEEPER=1 STUB_UI_RESULT="new	4600	make test" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$(jobs_count)" == 0 ]] || { printf '✗ sleeper が起きていないのに job が残った\n'; exit 1; }
assert_called "display-message 予約に失敗しました" "sleeper が起きなければ知らせる (UI は予約したと言っている)"
printf '✓ sleeper が起きない → job を残さず知らせる (run-shell の rc は当てにしない)\n'
reset_state
STUB_RUNSHELL_EXIT=0 STUB_NO_SLEEPER=1 STUB_UI_RESULT="new	4600	make test" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$(jobs_count)" == 0 ]] || { printf '✗ rc=0 でも sleeper が居なければ予約は不成立であるべき\n'; exit 1; }
printf '✓ rc=0 でも証拠が無ければ予約しない\n'

printf '\n## wizard: 結果行のフィールド数を検証する\n'
for bad in "new	4600" "cancel" "cancel	a	b" "new	4600	x	y"; do
  reset_state
  STUB_UI_RESULT="$bad" run "$STUB_PATH" "$SCRIPT" wizard
  [[ "$(jobs_count)" == 0 ]] || { printf '✗ 壊れた結果 [%s] で job が出来た\n' "$bad"; exit 1; }
  assert_not_called "run-shell" "壊れた結果 [$bad] → 何もしない"
done

printf '\n## cancel: 発火済み・不在の予約は「取り消した」と言わない\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
ui_queue "cancel	vanished"; STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
assert_called "display-message その予約はもうありません" "既に無い予約の取消は、その旨を出す (黙って何もしない、にしない)"
assert_not_called "display-message 予約を取り消した" "取り消していないのに成功を出さない"

printf '\n## cancel: 確認を読んでいる間に発火したら「取り消した」と言わない\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper racy; racy_pid=$SLEEPER_PID
ui_queue "cancel	racy"; STUB_GUM_EXIT=0 STUB_GUM_FIRE_JOB="$TMUX_SCHEDULE_KEYS_DIR/racy.job" run "$STUB_PATH" "$SCRIPT" wizard
assert_called "display-message 取り消せませんでした" "確認中に発火していたら、その事実を伝える"
assert_not_called "display-message 予約を取り消した" "止めていないのに「取り消した」と言わない"
kill "$racy_pid" 2>/dev/null || true

printf '\n## cancel: 取り消したら一覧へ戻る (popup を閉じない)\n'
# ⚠️ 取消の実行はシェル側 (gum の確認つき) なので、UI をいったん閉じる必要がある。閉じたまま
# 終わると「1 件消すたびに popup が閉じる」ことになる (ユーザー要望 2026-08-28)。
# 更新した一覧で UI を開き直し、--start pick で一覧の画面から始める
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper keep1
spawn_sleeper drop1
ui_queue "cancel	drop1" "abort"   # 1 件消して、次の一覧で閉じる
STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
[[ "$RC" -eq 0 ]] || { printf '✗ wizard が exit %s\n' "$RC"; cat "$RUN_ERR"; exit 1; }
# UI が 2 回起動し、2 回目は一覧から始まること
ui_starts=$(grep -c '^schedkeys --label' "$CALLS")
[[ "$ui_starts" == 2 ]] || { printf '✗ UI の起動が %s 回 (取消後に開き直していない)\n%s\n' "$ui_starts" "$(grep '^schedkeys' "$CALLS")"; exit 1; }
grep -q '^schedkeys .*--start pick' "$CALLS" || { printf '✗ 2 回目が一覧から始まっていない:\n%s\n' "$(grep '^schedkeys' "$CALLS")"; exit 1; }
printf '✓ 取消のあと一覧を開き直す (--start pick)\n'
# 開き直した一覧から、消した予約が消えていること
grep -q 'drop1' "$STUB_UI_JOBS_COPY" && { printf '✗ 取り消した予約が一覧に残っている:\n%s\n' "$(cat "$STUB_UI_JOBS_COPY")"; exit 1; }
grep -q 'keep1' "$STUB_UI_JOBS_COPY" || { printf '✗ 消していない予約まで一覧から消えた:\n%s\n' "$(cat "$STUB_UI_JOBS_COPY")"; exit 1; }
printf '✓ 開き直した一覧は取消を反映している\n'
[[ "$(jobs_count)" == 1 ]] || { printf '✗ job が %s 件 (1 件だけ消えるべき)\n' "$(jobs_count)"; exit 1; }

printf '\n## cancel: 続けて取り消せる (毎回閉じない)\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper a1
spawn_sleeper a2
ui_queue "cancel	a1" "cancel	a2" "abort"
STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
[[ "$RC" -eq 0 ]] || { printf '✗ wizard が exit %s\n' "$RC"; exit 1; }
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 2 件目が消えていない (%s 件)\n' "$(jobs_count)"; exit 1; }
[[ "$(grep -c '^schedkeys --label' "$CALLS")" == 3 ]] || { printf '✗ UI の起動回数が想定と違う:\n%s\n' "$(grep -c '^schedkeys --label' "$CALLS")"; exit 1; }
printf '✓ 閉じずに続けて取り消せる\n'

printf '\n## wizard: UI が同じ結果を返し続けても止まる\n'
# ⚠️ ループに上限が無いと、壊れた UI で無限に回る (実際に踏んだ 2026-08-28)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper stuck
STUB_GUM_EXIT=0 STUB_UI_RESULT="cancel	stuck" run "$STUB_PATH" "$SCRIPT" wizard   # キューを使わない = 同じ結果を返し続ける
[[ "$RC" -ne 0 ]] || { printf '✗ 同じ結果を返し続ける UI で成功として終わった\n'; exit 1; }
grep -q '上限' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 打ち切りがログに残らない\n'; exit 1; }
printf '✓ 上限で打ち切る (無限に回らない)\n'

printf '\n## cancel: 成否は「job を先に取れたか」で決まる\n'
# ⚠️ kill の成否では決めない。fire は送信直前に TERM を無視するので、kill が exit 0 でも送信は
# 完走しうる (監査 2026-08-28)。逆に sleeper が既に死んでいても、job を取れたなら送られないので
# 「取り消した」は嘘ではない。判定は rename (原子的な claim) の成否
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper crashed; crashed_pid=$SLEEPER_PID
ui_queue "cancel	crashed"; STUB_GUM_EXIT=0 STUB_GUM_KILL_PID="$crashed_pid" run "$STUB_PATH" "$SCRIPT" wizard
assert_called "display-message 予約を取り消した" "sleeper が死んでいても job を取れたなら取り消し成立 (送られないので嘘ではない)"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 後片付けができていない\n'; exit 1; }
[[ -z "$(find "$TMUX_SCHEDULE_KEYS_DIR" -name '*.cancelled' -o -name '*.claimed' 2>/dev/null)" ]] \
  || { printf '✗ claim 用の一時ファイルが残っている\n'; ls "$TMUX_SCHEDULE_KEYS_DIR"; exit 1; }
printf '✓ 取消の後始末 (claim 用ファイルも残さない)\n'

printf '\n## read_job: 開けない job の値が前の job から漏れない\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job aaa "$(( $(/bin/date +%s) + 3600 ))" "git push --force"
write_job bbb "$(( $(/bin/date +%s) + 7200 ))" "ls"
sed -i.bak '1s/.*/%%9/' "$TMUX_SCHEDULE_KEYS_DIR/bbb.job" && rm -f "$TMUX_SCHEDULE_KEYS_DIR/bbb.job.bak"
chmod 000 "$TMUX_SCHEDULE_KEYS_DIR/bbb.job"
STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard
chmod 644 "$TMUX_SCHEDULE_KEYS_DIR/bbb.job"
if grep -q 'git push --force' <<< "$(grep '^bbb' "$STUB_UI_JOBS_COPY" 2>/dev/null || true)"; then
  printf '✗ 開けない job の行に別の予約の値が漏れた:\n%s\n' "$(cat "$STUB_UI_JOBS_COPY")"; exit 1
fi
printf '✓ 開けない job は一覧に出さない (前の値を引きずらない)\n'

printf '\n## cancel: UI が選んだ予約を確認してから取り消す\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
ui_queue "cancel	live"; STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
[[ "$RC" -eq 0 ]] || { printf '✗ wizard (cancel) が exit %s\n' "$RC"; cat "$RUN_ERR"; exit 1; }
/bin/sleep 0.3
kill -0 "$live_pid" 2>/dev/null && { printf '✗ 取消したのに sleeper が生きている\n'; exit 1; }
[[ "$(jobs_count)" == 0 && ! -f "$TMUX_SCHEDULE_KEYS_DIR/live.pid" ]] || { printf '✗ 取消後に job/pid が残っている\n'; exit 1; }
printf '✓ 取消 → sleeper kill + job/pid 削除\n'
grep -E '^gum confirm .*--default=false' "$CALLS" >/dev/null || { printf '✗ 取消確認が --default=false でない\n'; exit 1; }
printf '✓ 取消確認は --default=false (Enter 素通しでは消えない)\n'

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
ui_queue "cancel	live"; STUB_GUM_EXIT=1 run "$STUB_PATH" "$SCRIPT" wizard
kill -0 "$live_pid" 2>/dev/null || { printf '✗ 取消を拒否したのに sleeper が死んだ\n'; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 取消拒否で job が消えた\n'; exit 1; }
printf '✓ 取消拒否 → 何もしない\n'
kill "$live_pid" 2>/dev/null || true

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
ui_queue "cancel	live"; run "$NOGUM_PATH" "$SCRIPT" wizard
kill -0 "$live_pid" 2>/dev/null || { printf '✗ gum 未導入なのに取り消された (fail-safe の && 短絡)\n'; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ gum 未導入で job が消えた\n'; exit 1; }
printf '✓ gum 未導入 (exit 127) → 取り消さない\n'
kill "$live_pid" 2>/dev/null || true

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
ui_queue "cancel	no-such-id"; STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
kill -0 "$live_pid" 2>/dev/null || { printf '✗ 存在しない id の取消で無関係な sleeper が死んだ\n'; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 存在しない id の取消で job が消えた\n'; exit 1; }
assert_not_called "gum confirm" "存在しない id → 確認まで進まない"
printf '✓ 存在しない id の取消は何もしない\n'
kill "$live_pid" 2>/dev/null || true

printf '\n## prune: sleeper が居ない予約だけを掃く\n'
# pid 再利用: .pid が無関係な生きたプロセスを指す → kill せず、stale として掃く
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_spawn_fake_proc; foreign_pid=$REPLY_PID
write_job reused "$(( $(/bin/date +%s) + 3600 ))" "make test"
printf '%s\n' "$foreign_pid" > "$TMUX_SCHEDULE_KEYS_DIR/reused.pid"
STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard
kill -0 "$foreign_pid" 2>/dev/null || { printf '✗ 無関係なプロセス (pid 再利用) を kill した\n'; exit 1; }
[[ "$(jobs_count)" == 0 ]] || { printf '✗ sleeper 不在 (pid 再利用) の job が掃かれない\n'; exit 1; }
[[ ! -s "$STUB_UI_JOBS_COPY" ]] || { printf '✗ 掃いた job が UI の一覧に出ている\n'; exit 1; }
printf '✓ 無関係なプロセスは kill せず、その job は掃く\n'

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_free_pid; dead_pid=$REPLY_PID
write_job stale "$(( $(/bin/date +%s) + 3600 ))" "make test"
printf '%s\n' "$dead_pid" > "$TMUX_SCHEDULE_KEYS_DIR/stale.pid"
STUB_UI_RESULT="" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$RC" -eq 0 && "$(jobs_count)" == 0 ]] || { printf '✗ pid が死んだ stale job が掃かれない\n'; exit 1; }
printf '✓ stale job (pid 死亡) を掃く\n'

# .pid 未作成 + 作成直後の job は掃かない (fire が書く前の猶予)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job fresh "$(( $(/bin/date +%s) + 3600 ))" "make test"
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
# bind は キーの手前に -n / -r / -N "注記" を任意個取る。フラグを吸収しないと 1 件も拾えず、
# この検査が空振りする (pipefail 下では代入ごと silent に死ぬ。2026-08-28 に -N 追加で実際に踏んだ)。
# 注記も popup タイトルと同じ行に乗るので、検査対象に含まれるのが正しい
BIND_FLAGS='( +(-[nr]|-N +"[^"]*"))*'
bind_lines="$(grep -E "^bind${BIND_FLAGS} +(m|Enter|C-m) " "$CONF" || true)"
[[ -n "$bind_lines" ]] || { printf '✗ bind m/Enter/C-m の行が 1 件も取れない (検査が空振り)\n'; exit 1; }
bad="$(printf '%s\n' "$bind_lines" | perl -CSD -ne "print if /$WIDE_RE/")"
[[ -z "$bad" ]] || { printf '✗ popup タイトル (bind) に絵文字:\n%s\n' "$bad"; exit 1; }
printf '✓ popup タイトル・注記に絵文字なし\n'

printf '\n## prune: 猶予の境界 (両側を見る)\n'
# ⚠️ 「作った直後は消さない」だけでは、猶予を 0 にしても 99999999 にしても緑になる (監査 2026-08-28)。
# mtime を back-date して両側を通す
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
# ⚠️ epoch → 日時の変換は BSD と GNU で別物。BSD (macOS) は `date -r <epoch>` だが、GNU
#   (Linux = CI) の -r は「参照ファイルの時刻」なので epoch をファイル名として探して失敗し、
#   stdout は空・rc=1 になる (実測 2026-08-28)。素の `date -r` だけだと `touch -t ''` になり、
#   **CI でだけこのテストが死ぬ** (run 33136841310)。両方試して先に成功した方を採る
#   (`_claude/statusline-command.sh` の fmt_epoch と同じ形)。
backdate() { # $1=対象パス $2=何秒前にするか
  local at=$(( $(/bin/date +%s) - $2 )) stamp
  stamp="$(/bin/date -r "$at" +%Y%m%d%H%M.%S 2>/dev/null || /bin/date -d "@$at" +%Y%m%d%H%M.%S 2>/dev/null)"
  # 変換できないまま touch を撃たない。backdate が no-op になると、猶予の内側/外側を分ける
  # 検査が「どちらも作りたて」を見ることになり意味を失う
  [[ -n "$stamp" ]] || { printf '✗ backdate: epoch を日時へ変換できない (BSD/GNU どちらの date でもない)\n'; exit 1; }
  touch -t "$stamp" "$1"
}
write_job inside "$(( $(/bin/date +%s) + 3600 ))" "make test"; backdate "$TMUX_SCHEDULE_KEYS_DIR/inside.job" 30
write_job outside "$(( $(/bin/date +%s) + 3600 ))" "make test"; backdate "$TMUX_SCHEDULE_KEYS_DIR/outside.job" 300
STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard
[[ -f "$TMUX_SCHEDULE_KEYS_DIR/inside.job" ]] || { printf '✗ 猶予の内側 (30 秒前) の job を消した\n'; exit 1; }
[[ ! -f "$TMUX_SCHEDULE_KEYS_DIR/outside.job" ]] || { printf '✗ 猶予の外側 (300 秒前) の job を消していない\n'; exit 1; }
printf '✓ .pid が無い job は、猶予の内側は残し外側は掃く\n'

printf '\n## prune/cancel: sleeper の照合は id の完全一致\n'
# ⚠️ 前方一致だと id "j-5" のつもりで "j-55" の sleeper に当たる。実害が出るのは **pid が再利用された
# とき**: j-5 の .pid に別 sleeper の pid が入っていると、j-5 の取消が j-55 を殺す (監査 2026-08-28)。
# id が全部別単語の fixture では前方一致に変えても緑のままなので、その状況を作って確かめる
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper j-55; long_pid=$SLEEPER_PID
write_job j-5 "$(( $(/bin/date +%s) + 3600 ))" "make test"
printf '%s\n' "$long_pid" > "$TMUX_SCHEDULE_KEYS_DIR/j-5.pid"   # pid 再利用を模す
ui_queue "cancel	j-5"; STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
/bin/sleep 0.3
kill -0 "$long_pid" 2>/dev/null || { printf '✗ j-5 の取消が別 id (j-55) の sleeper を殺した\n'; exit 1; }
[[ -f "$TMUX_SCHEDULE_KEYS_DIR/j-55.job" ]] || { printf '✗ 別 id (j-55) の job まで消した\n'; exit 1; }
printf '✓ id の完全一致で照合する (pid を取り違えても別の予約を巻き込まない)\n'
kill "$long_pid" 2>/dev/null || true

printf '\n## 送り先の表示に「実行中のコマンド」が入る\n'
# ⚠️ window 名だけでは送り先が claude なのか shell なのか分からない (実測 2026-08-28:
# Claude の pane は cmd=claude.exe / window名=「✳ タスク名」、shell は cmd=zsh)。
# stub は tmux の format を解釈できないので、問い合わせの形を pin する
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper labeled
STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard
grep -q 'display-message -p -t %5 #{session_name}:#{window_index} #{pane_current_command} #{window_name}' "$CALLS" \
  || { printf '✗ 送り先の問い合わせに pane_current_command が入っていない:\n%s\n' "$(grep 'display-message -p -t' "$CALLS" | head -3)"; exit 1; }
printf '✓ 送り先の表示は session:index + 実行中のコマンド + window 名\n'

printf '\n## 一覧: 消えた pane は「消滅」と出す\n'
# ⚠️ 実 tmux は消えた pane への問い合わせでも rc=0 を返し、window 名が空の ": " になる
# (実測 2026-08-28)。rc では判定できないので中身で見る
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper goneone
STUB_PANE_GONE=1 STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard
grep -q '(消滅)' "$STUB_UI_JOBS_COPY" 2>/dev/null \
  || { printf '✗ 消えた pane の一覧表示が「消滅」でない:\n%s\n' "$(cat -v "$STUB_UI_JOBS_COPY" 2>/dev/null)"; exit 1; }
printf '✓ 消えた pane は「消滅」と出る (空の ": " をそのまま見せない)\n'

printf '\n## 通知: 予約文字列の # がフォーマットとして展開されない\n'
# display-message は #{...} や #H を展開する。予約にはコメントや #{} が普通に入る
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper hashy
write_job hashy "$(( $(/bin/date +%s) + 3600 ))" 'echo #{pane_id} # note'
ui_queue "cancel	hashy"; STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
assert_called 'display-message 予約を取り消した: echo ##{pane_id} ## note' "# を ## にして展開を止める"

printf '\n## 一覧: 表示名のタブで列がずれない\n'
# window 名は任意の文字列。タブが入ると TSV の列が割れ、別の予約の文字列が別の id に結びつく
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job tabbed "$(( $(/bin/date +%s) + 3600 ))" "make test"
STUB_WINDOW_LABEL="$(printf 'main:3\tclaude')" STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard
[[ -s "$STUB_UI_JOBS_COPY" ]] || { printf '✗ 一覧が空\n'; exit 1; }
fields=$(awk -F'\t' '{print NF}' "$STUB_UI_JOBS_COPY" | sort -u)
[[ "$fields" == 4 ]] || { printf '✗ TSV の列数が 4 でない (%s):\n%s\n' "$fields" "$(cat -v "$STUB_UI_JOBS_COPY")"; exit 1; }
printf '✓ 表示名のタブを潰して 4 列を保つ\n'

printf '\n## 取消の確認に残り時間が出る\n'
# シェル側の fmt_remaining は Go の formatRemaining とは別実装。ここでしか使われないので、
# 潰しても他のどのテストも落ちない (監査 2026-08-28)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper remain
# ⚠️ 発火までを分の境界ちょうど (3540 = 59m00s) に置かない。fmt_remaining は切り捨てなので、
#    fixture 作成から gum confirm までに 1 秒でも経つと 58m になる (CI で実発生 2026-08-29
#    run 33260033544)。59m59s に置けば、59 秒経つまで 59m のまま
write_job remain "$(( $(/bin/date +%s) + 3599 ))" "make test"
ui_queue "cancel	remain"; STUB_GUM_EXIT=1 run "$STUB_PATH" "$SCRIPT" wizard
assert_called "gum confirm" "確認を出す"
grep -E '^gum confirm .*59m' "$CALLS" >/dev/null \
  || { printf '✗ 確認に残り時間が出ていない:\n%s\n' "$(grep '^gum confirm' "$CALLS")"; exit 1; }
printf '✓ 確認に残り時間 (59m) が出る\n'

printf '\n## ps の桁切り対策 (Linux で生きた予約を消さないため)\n'
# GNU ps は既定 80 桁で command 列を切る。切られると pid_is_sleeper が常に偽になり、prune が
# 生きている予約を消す。macOS の ps は切らないので**挙動としては観測できない** → 配線を静的に pin する
grep -q 'ps -ww -o command= -p' "$SCRIPT" || { printf '✗ ps に -ww が無い (GNU ps で command が切られる)\n'; exit 1; }
printf '✓ ps -ww で command 全体を見る\n'

printf '\n## pane 表示: 予約の有無を pane オプション @schedkeys-at に写す\n'
# ⚠️ 表示の正本は .job で、pane オプションはその写し。**状態を書き換える全経路** (new / cancel /
#    fire の claim・drop / prune) で set / unset が一致していないと、予約が無い pane に
#    幽霊表示が残る。1 経路ずつ見るのはそのため (どれか 1 つで unset を落とすと red になる)。
# ⚠️ 値の HH:MM は `date -r <epoch>` (BSD) で作る。この repo は macOS 専用 (CLAUDE.md)
fmt_hm() { /bin/date -r "$1" '+%H:%M' 2>/dev/null || /bin/date -d "@$1" '+%H:%M' 2>/dev/null; }

reset_state
sk_at=$(( $(/bin/date +%s) + 3600 )); sk_hm="$(fmt_hm "$sk_at")"
[[ -n "$sk_hm" ]] || { printf '✗ epoch を HH:MM へ変換できない (検査が空振り)\n'; exit 1; }
STUB_UI_RESULT="new	$sk_at	make test" run "$STUB_PATH" "$SCRIPT" wizard
assert_called "tmux set-option -p -t %5 @schedkeys-at $sk_hm" "new: 発火時刻 (HH:MM) を pane オプションへ書く"

printf '\n## pane 表示: 同じ pane に複数あるときは最早の時刻と残件数\n'
# ⚠️ **id の並び順を入れ替えて 2 回見る**。*.job の glob 順で「最後に見た job」を採る実装でも、
#    早い方が最後に来る fixture では緑になる (変異検証 2026-09-02 で実際に素通りした)。
#    片方の並びだけでは「最早を選ぶ」を何も守っていない
sk_early=$(( $(/bin/date +%s) + 600 )); sk_late=$(( $(/bin/date +%s) + 7200 ))
for order in early-first early-last; do
  reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
  if [[ "$order" == early-first ]]; then
    write_job aaa "$sk_early" "echo early"; write_job zzz "$sk_late" "echo late"
  else
    write_job aaa "$sk_late" "echo late"; write_job zzz "$sk_early" "echo early"
  fi
  # 3 件目を取り消して refresh を起こす (残り 2 件の最早が表示になる)
  spawn_sleeper mmm   # at = now+3600 なので最早でも最遅でもない
  ui_queue "cancel	mmm" "abort"; STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
  assert_called "tmux set-option -p -t %5 @schedkeys-at $(fmt_hm "$sk_early") ほか1件" "[$order] 最早の時刻 + 残件数"
  assert_not_called "@schedkeys-at $(fmt_hm "$sk_late")" "[$order] 遅い方の時刻で上書きしない"
done

printf '\n## pane 表示: 取消で消える / 残りがあれば最早へ繰り上がる\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper only1
ui_queue "cancel	only1" "abort"; STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
assert_called "tmux set-option -pu -t %5 @schedkeys-at" "cancel: 最後の予約が消えたら表示も消す"

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper dropy
sk_keep=$(( $(/bin/date +%s) + 1800 ))
write_job keepy "$sk_keep" "echo keep"
ui_queue "cancel	dropy" "abort"; STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" wizard
assert_called "tmux set-option -p -t %5 @schedkeys-at $(fmt_hm "$sk_keep")" "cancel: 残りがあれば最早へ繰り上げる (消さない)"

printf '\n## pane 表示: fire (送信) と破棄で消える\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job sent "$(( $(/bin/date +%s) - 1 ))" "make test"
run "$STUB_PATH" "$SCRIPT" fire sent
assert_called "tmux set-option -pu -t %5 @schedkeys-at" "fire: 送る時点で表示を消す (送信後も残ると幽霊になる)"

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job dropped "$(( $(/bin/date +%s) - 1 ))" "make test"
STUB_SRVPID=9999 run "$STUB_PATH" "$SCRIPT" fire dropped   # 予約時と別サーバ = 送らずに破棄
assert_not_called "send-keys -t %5 -l" "別サーバなら送らない (前提の確認)"
# ⚠️ 別サーバなら**表示にも触らない**。今この socket に居るのは別サーバで、そこの %5 は
#    無関係な pane (pane id はサーバごとに振り直される)。unset すると他人の枠を消す
#    (敵対的レビュー 2026-09-02 の P2)
assert_not_called "set-option" "別サーバのときは表示を触らない (無関係な pane を消さない)"

# ⚠️ 上のブロックは fire_drop の refresh を守っていない: 別サーバ判定は **claim の後**なので、
#    claim 側の refresh だけで緑になる (変異検証 2026-09-02)。fire_drop 固有の経路は
#    「claim より前に降りる」= 時刻が取れないとき。そこだけ date を壊して通す
#    (このブロック限定の stub。他は実 date を使う)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
mkdir -p "$TMP_DIR/bin_nodate"
cp "$TMP_DIR/bin/tmux" "$TMP_DIR/bin/sleep" "$TMP_DIR/bin_nodate/"
cat > "$TMP_DIR/bin_nodate/date" <<'EOS2'
#!/bin/sh
# +%s (現在時刻) だけを壊す。ログの日時整形と -r <epoch> は実物に通す
case "$1" in +%s) exit 1 ;; esac
exec /bin/date "$@"
EOS2
chmod +x "$TMP_DIR/bin_nodate/date"
write_job notime "$(( $(/bin/date +%s) - 1 ))" "make test"
run "$TMP_DIR/bin_nodate:/usr/bin:/bin" "$SCRIPT" fire notime
[[ "$RC" -eq 0 ]] || { printf '✗ 時刻が取れないときに fire が exit %s (無音契約は exit 0)\n' "$RC"; exit 1; }
assert_not_called "send-keys -t %5 -l" "時刻が取れなければ送らない (claim より前に降りる)"
assert_called "tmux set-option -pu -t %5 @schedkeys-at" "claim 前に降りる破棄でも表示を消す (fire_drop 側)"

printf '\n## pane 表示: 別サーバの予約は同じ pane id でも数えない\n'
# ⚠️ $STATE_DIR は全サーバで共有 (-L の隔離サーバも同じディレクトリ)。pane id はサーバごとに
#    振り直されるので、文字列一致だけで数えると別サーバの予約の時刻をこちらの枠に出す
#    (敵対的レビュー 2026-09-02 の P1)。同一性は job の socket + サーバ pid で見る
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
sk_other=$(( $(/bin/date +%s) + 300 ))
write_job other "$sk_other" "echo other" "/tmp/other-sock" "7777"   # 同じ %5 だが別サーバ
sk_mine=$(( $(/bin/date +%s) + 3600 ))
STUB_UI_RESULT="new	$sk_mine	make test" run "$STUB_PATH" "$SCRIPT" wizard
assert_called "tmux set-option -p -t %5 @schedkeys-at $(fmt_hm "$sk_mine")" "自サーバの予約だけを数える"
assert_not_called "@schedkeys-at $(fmt_hm "$sk_other")" "別サーバの予約の時刻を出さない"
assert_not_called "ほか1件" "別サーバの予約を件数に数えない"

printf '\n## pane 表示: サーバを同定できなければ何も書かない (fail-closed)\n'
# ⚠️ socket / サーバ pid が取れないときに書くと、どのサーバの pane を触っているのか
#    分からないまま set / unset することになる。相手が分からない pane は触らない
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
STUB_NO_SOCK=1 STUB_UI_RESULT="new	$(( $(/bin/date +%s) + 3600 ))	make test" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 前提が崩れている (予約が成立していない)\n'; exit 1; }
assert_not_called "set-option" "socket が取れなければ表示を書かない (予約自体は成立させる)"

printf '\n## pane 表示: stale 掃除で消える (サーバ再起動で sleeper だけ死んだ形)\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
write_job gone "$(( $(/bin/date +%s) + 3600 ))" "make test"
backdate "$TMUX_SCHEDULE_KEYS_DIR/gone.job" 300   # 猶予の外 = stale
STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 前提が崩れている (stale を掃いていない)\n'; exit 1; }
assert_called "tmux set-option -pu -t %5 @schedkeys-at" "prune: 掃いた予約の表示も消す"

printf '\n## _tmux.conf: pane-border-format が @schedkeys-at を見る\n'
# ⚠️ シェル側が set-option するだけでは何も出ない。表示は conf 側の format が担うので、両方を固定する。
#    #() で job を読む形に戻していないことも見る (status-interval 1 の毎秒再描画で fork する)
grep -q 'pane-border-format .*@schedkeys-at' "$CONF" \
  || { printf '✗ pane-border-format が @schedkeys-at を参照していない\n'; exit 1; }
grep -q '#(' <<< "$(grep -E '^set -g pane-border-format' "$CONF")" \
  && { printf '✗ pane-border-format が #() で外部コマンドを呼んでいる (毎秒 fork する)\n'; exit 1; }
printf '✓ pane-border-format が @schedkeys-at を見る (fork なしの format 展開のみ)\n'

printf '\n## 置き場: 既定は tmux サーバ (socket) ごとに分かれる\n'
# ⚠️ 全サーバで 1 つの dir を共有すると、pane id はサーバごとに振り直されるので一覧・取消・
#    失効件数が別サーバの予約を混ぜる (issue 189。取消は別サーバの sleeper を実際に kill できた)。
#    以降のテストは TMUX_SCHEDULE_KEYS_DIR で dir を直接指定する形 (テスト用の上書き) なので、
#    **既定の解決経路はここだけが見ている**
sk_home="$TMP_DIR/home_sockdir"; mkdir -p "$sk_home"
sk_root="$sk_home/.local/state/tmux-schedule-keys"
reset_calls
STUB_SOCK="/tmp/tmux-501/mysock" \
  HOME="$sk_home" STUB_UI_RESULT="new	$(( $(/bin/date +%s) + 3600 ))	make test" \
  run "$STUB_PATH" env -u TMUX_SCHEDULE_KEYS_DIR "$SCRIPT" wizard
sk_expect="$sk_root/%tmp%tmux-501%mysock"
[[ -d "$sk_expect" ]] || { printf '✗ socket ごとの dir が作られない (期待: %s)\n%s\n' "$sk_expect" "$(find "$sk_root" -maxdepth 1 2>/dev/null)"; exit 1; }
[[ "$(find "$sk_expect" -name '*.job' | wc -l | tr -d ' ')" == 1 ]] \
  || { printf '✗ job が socket ごとの dir に置かれていない\n%s\n' "$(find "$sk_root" 2>/dev/null)"; exit 1; }
[[ -z "$(find "$sk_root" -maxdepth 1 -name '*.job' 2>/dev/null)" ]] \
  || { printf '✗ 共有 dir (root 直下) に job が置かれている\n'; exit 1; }
printf '✓ 既定の置き場は socket ごとの dir (共有 root 直下には置かない)\n'
assert_called "run-shell -b TMUX_SCHEDULE_KEYS_DIR='$sk_expect'" "fire には socket ごとの dir を env で渡す"

printf '\n## 置き場: 別サーバの dir と旧共有 dir の予約は見えない\n'
# ⚠️ fixture は **退行したら見えるようになる場所**にも置く。別 socket の dir だけに置くと、
#    共有 root を見る退行 (= 修正前の姿) では最初から不可視なので、このブロックは何も守らない
#    (敵対的レビュー 2026-09-03 の P2-2 で、変異を当てても緑のまま通ることを実証された)。
#    旧共有 dir (root 直下) は「移さない」と決めた置き場なので、そこに置くのが退行の観測点になる
rm -f "$sk_expect"/*.job "$sk_expect"/*.pid
other_dir="$sk_root/%tmp%tmux-501%othersock"; mkdir -p "$other_dir"
printf '%%5\n%s\necho other\n/tmp/tmux-501/othersock\n9999\n' "$(( $(/bin/date +%s) + 600 ))" > "$other_dir/foreign.job"
printf '%%5\n%s\necho legacy\n/tmp/tmux-501/mysock\n4242\n' "$(( $(/bin/date +%s) + 900 ))" > "$sk_root/legacy.job"
reset_calls; rm -f "$STUB_UI_JOBS_COPY"
STUB_SOCK="/tmp/tmux-501/mysock" HOME="$sk_home" STUB_UI_RESULT="abort" \
  run "$STUB_PATH" env -u TMUX_SCHEDULE_KEYS_DIR "$SCRIPT" wizard
grep -q 'foreign' "$STUB_UI_JOBS_COPY" && { printf '✗ 別サーバの予約が一覧に出た:\n%s\n' "$(cat "$STUB_UI_JOBS_COPY")"; exit 1; }
grep -q 'legacy' "$STUB_UI_JOBS_COPY" && { printf '✗ 旧共有 dir の予約が一覧に出た:\n%s\n' "$(cat "$STUB_UI_JOBS_COPY")"; exit 1; }
[[ -f "$other_dir/foreign.job" ]] || { printf '✗ 別サーバの予約を掃いてしまった\n'; exit 1; }
[[ -f "$sk_root/legacy.job" ]] || { printf '✗ 旧共有 dir の予約を掃いてしまった (移さない・触らないと決めた)\n'; exit 1; }
assert_not_called "display-message サーバ再起動" "別サーバ・旧共有 dir の予約を失効件数に数えない"
printf '✓ 別サーバの dir と旧共有 dir は覗かない・掃かない\n'

printf '\n## 置き場: socket が取れなければ予約を作らない (fail-closed)\n'
# ⚠️ 共有 dir へ落とすと混ざりが戻る。どのサーバの予約か分からないものは作らない
reset_calls
rm -f "$sk_root"/*.job   # 前のブロックが置いた旧共有 dir の fixture を片付ける
STUB_NO_SOCK=1 HOME="$sk_home" STUB_UI_RESULT="new	$(( $(/bin/date +%s) + 3600 ))	make test" \
  run "$STUB_PATH" env -u TMUX_SCHEDULE_KEYS_DIR "$SCRIPT" wizard
[[ "$RC" -ne 0 ]] || { printf '✗ socket が取れないのに成功で終わった\n'; exit 1; }
assert_not_called "run-shell" "socket が取れなければ sleeper を起こさない"
assert_called "display-message 予約入力を開けませんでした" "理由を知らせる (押しても何も起きないキーにしない)"
[[ -z "$(find "$sk_root" -maxdepth 1 -name '*.job' 2>/dev/null)" ]] \
  || { printf '✗ 共有 root 直下に job を作った\n'; exit 1; }

printf '\n## 置き場: 同じ socket に立ち直ったサーバは前任の予約を失効させる\n'
# ⚠️ 入れ物を分けても socket が同じなら dir は同じ。pane id は振り直されているので、前任の job は
#    一覧に**今のサーバの pane 名で**並び、発火時に fire_claim が拒否するまで待たされる
#    (敵対的レビュー 2026-09-03 の P2-3 で実験で再現)。kill はしない: job を消せば前任の
#    sleeper は claim に失敗して静かに降りる
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper mine                                        # 今のサーバ (stub の pid=4242) の予約
# ⚠️ 前任の job にも**本物の sleeper**を付ける。偽 pid だと通常の stale 掃除が先に消すので、
#    前任判定を外しても緑のまま通る (変異検証 2026-09-03 で実際に素通りした)
spawn_sleeper oldsrv
sed -i.bak '5s/.*/1234/' "$TMUX_SCHEDULE_KEYS_DIR/oldsrv.job" && rm -f "$TMUX_SCHEDULE_KEYS_DIR/oldsrv.job.bak"
rm -f "$STUB_UI_JOBS_COPY"
STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard
[[ ! -f "$TMUX_SCHEDULE_KEYS_DIR/oldsrv.job" ]] || { printf '✗ 前任サーバの予約が残っている (一覧に他人の pane 名で並ぶ)\n'; exit 1; }
[[ -f "$TMUX_SCHEDULE_KEYS_DIR/mine.job" ]] || { printf '✗ 今のサーバの予約まで消した\n'; exit 1; }
grep -q 'oldsrv' "$STUB_UI_JOBS_COPY" && { printf '✗ 前任サーバの予約が一覧に出た:\n%s\n' "$(cat "$STUB_UI_JOBS_COPY")"; exit 1; }
assert_called "display-message サーバ再起動などで 1 件の予約が失効しました" "前任サーバの予約は失効として知らせる (黙って消さない)"
printf '✓ 前任サーバの予約を失効させ、今のサーバの予約は残す\n'

printf '\n## 置き場: サーバ pid が取れないときは前任判定をしない\n'
# ⚠️ 確かめられないものを消さない。ここで消すと、tmux が答えないだけの状況で全予約が飛ぶ
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
# ⚠️ 本物の sleeper を起こしてから job のサーバ pid だけ前任の値に書き換える。偽 pid だと
#    通常の stale 掃除で消えてしまい、「前任判定をしていない」ことの検査にならない
spawn_sleeper keepit
sed -i.bak '5s/.*/1234/' "$TMUX_SCHEDULE_KEYS_DIR/keepit.job" && rm -f "$TMUX_SCHEDULE_KEYS_DIR/keepit.job.bak"
: > "$TMP_DIR/empty_srvpid"   # tmux が pid を答えない形 (空 + rc=0)
STUB_SRVPID_FILE="$TMP_DIR/empty_srvpid" STUB_UI_RESULT="abort" run "$STUB_PATH" "$SCRIPT" wizard
[[ -f "$TMUX_SCHEDULE_KEYS_DIR/keepit.job" ]] || { printf '✗ サーバ pid が取れないのに前任扱いで消した\n'; exit 1; }
printf '✓ サーバ pid が取れなければ前任判定をしない\n'

printf '\n## 置き場: 改行を含む置き場は扱わない (run-shell のコマンド文字列が割れる)\n'
# ⚠️ env 指定の経路は socket 由来の検査を通らないので、ここで同じ検査をかけている
#    (敵対的レビュー 2026-09-03 の P3-2)。改行が入ると run-shell へ渡す 1 行が割れる
reset_calls
nl_dir="$TMP_DIR/nl
dir"
mkdir -p "$nl_dir" 2>/dev/null || true
TMUX_SCHEDULE_KEYS_DIR="$nl_dir" STUB_UI_RESULT="new	$(( $(/bin/date +%s) + 3600 ))	make test" \
  run "$STUB_PATH" "$SCRIPT" wizard
assert_not_called "run-shell" "改行を含む置き場では sleeper を起こさない"
[[ -z "$(find "$nl_dir" -name '*.job' 2>/dev/null)" ]] || { printf '✗ 改行を含む置き場に job を作った\n'; exit 1; }
printf '✓ 改行を含む置き場は扱わない\n'

printf '\n## 置き場: socket 名の潰し方で別 socket が衝突しない\n'
# ⚠️ `/` → `%` だけだと /tmp/a%b と /tmp/a/b が同じ dir 名になり、直したはずの混ざりが黙って戻る
#    (敵対的レビュー 2026-09-03 の P3-1)。% を先に %25 へ逃がす
for pair in "/tmp/x/y:%tmp%x%y" "/tmp/x%y:%tmp%x%25y"; do
  sk_in="${pair%%:*}"; sk_want="${pair##*:}"
  reset_calls; rm -rf "$sk_home/.local/state/tmux-schedule-keys"
  STUB_SOCK="$sk_in" HOME="$sk_home" STUB_UI_RESULT="new	$(( $(/bin/date +%s) + 3600 ))	make test" \
    run "$STUB_PATH" env -u TMUX_SCHEDULE_KEYS_DIR "$SCRIPT" wizard
  [[ -d "$sk_root/$sk_want" ]] || { printf '✗ socket %s の dir 名が %s でない:\n%s\n' "$sk_in" "$sk_want" "$(find "$sk_root" -maxdepth 1)"; exit 1; }
done
printf '✓ %% を先に逃がすので /tmp/x/y と /tmp/x%%y が同じ dir にならない\n'

printf '\n## 置き場: socket path の #{...} が展開されない (wizard と fire でズレない)\n'
# ⚠️ run-shell のコマンド文字列は **tmux がフォーマット展開してから** sh へ渡すので、socket path に
#    #{pane_id} が入ると wizard の置き場と fire の置き場が食い違う (tmux 3.7b で実測。P2-1)
reset_calls; rm -rf "$sk_home/.local/state/tmux-schedule-keys"
STUB_SOCK='/tmp/x#{pane_id}y' HOME="$sk_home" STUB_UI_RESULT="new	$(( $(/bin/date +%s) + 3600 ))	make test" \
  run "$STUB_PATH" env -u TMUX_SCHEDULE_KEYS_DIR "$SCRIPT" wizard
assert_called "TMUX_SCHEDULE_KEYS_DIR='$sk_root/%tmp%x##{pane_id}y'" "run-shell へ渡す前に # を ## にする (tmux の展開を止める)"
[[ -d "$sk_root/%tmp%x#{pane_id}y" ]] || { printf '✗ wizard 側の dir 名が socket そのままでない:\n%s\n' "$(find "$sk_root" -maxdepth 1)"; exit 1; }
printf '✓ #{...} を含む socket でも wizard と fire が同じ dir を見る\n'

printf '\n## 置き場: fire は env が無ければ黙って降りる\n'
# ⚠️ 置き場を渡されなかった fire (この変更より前に起きた sleeper / 手打ち) が走ると、
#    STATE_DIR が空のまま "/<id>.job" を見て「破棄しました」と通知してしまう。
#    存在しない予約の破棄を知らせるのは嘘なので、何もせず exit 0 する
reset_calls
run "$STUB_PATH" env -u TMUX_SCHEDULE_KEYS_DIR "$SCRIPT" fire someid
[[ "$RC" -eq 0 ]] || { printf '✗ 置き場なしの fire が exit %s (無音契約は exit 0)\n' "$RC"; exit 1; }
assert_not_called "send-keys" "置き場が無ければ何も送らない"
assert_not_called "display-message" "存在しない予約の破棄を通知しない (嘘をつかない)"

printf '\n## UI (Go) の配線\n'
grep -q 'bin/schedkeys' "$SCRIPT" || { printf '✗ シェルが bin/schedkeys を参照していない\n'; exit 1; }
[[ -x "$ROOT_DIR/bin/schedkeys" ]] || { printf '✗ bin/schedkeys が無い / 実行不可\n'; exit 1; }
grep -q 'go_autobuild_exec "' "$ROOT_DIR/bin/schedkeys" || { printf '✗ bin/schedkeys が go_autobuild 経由でない\n'; exit 1; }
grep -q 'go_autobuild_exec --async' "$ROOT_DIR/bin/schedkeys" && { printf '✗ UI のビルドが --async (古い UI の結果を新コードの結果と誤認する)\n'; exit 1; }
printf '✓ シェル → bin/schedkeys (同期ビルド) → src/schedkeys\n'

printf '\n## _tmux.conf: bind m / Enter / C-m が同じウィザードを指す\n'
for k in m Enter C-m; do
  grep -Eq "^bind${BIND_FLAGS} +$k +display-popup .*tmux_schedule_keys\.sh" "$CONF" || { printf '✗ bind %s が tmux_schedule_keys.sh を指していない\n' "$k"; exit 1; }
  printf '✓ bind %s → display-popup → tmux_schedule_keys.sh\n' "$k"
  # 注記 (-N) が付いていること。prefix+? (list-keys -N) の一覧はこれが唯一の出典で、
  # 注記を消すとキーが一覧から**黙って消える** (エラーにならず、押せば動くので気づけない)。
  # 3 キーとも別々に書くので、1 つだけ落とす編集も検出できるよう個別に見る
  grep -Eq "^bind( +-[nr])* +-N +\"[^\"]+\"( +-[nr])* +$k +display-popup" "$CONF" \
    || { printf '✗ bind %s に -N の注記が無い (prefix+? の一覧から消える)\n' "$k"; exit 1; }
  printf '✓ bind %s に -N 注記あり (prefix+? に出る)\n' "$k"
done
# 旧 launcher (display-menu) が復活していないこと。prefix+Enter の席はウィザードが上書きしている
! grep -Eq "^bind(-key)?${BIND_FLAGS} +(-T prefix +)?Enter +display-menu" "$CONF" || { printf '✗ prefix+Enter に launcher (display-menu) が復活している\n'; exit 1; }
printf '✓ prefix+Enter は launcher ではない\n'

printf '\n[test-schedule-keys] all ok\n'
