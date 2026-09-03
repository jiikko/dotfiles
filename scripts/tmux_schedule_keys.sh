#!/usr/bin/env bash
# tmux: 「N 時間 M 分後にこの pane へ文字列を入力する」予約ウィザード。_tmux.conf の bind m から
# display-popup -E 経由で呼ばれる。tmux 自体にタイマー機能は無いので、sleep + send-keys を
# tmux サーバの子 (run-shell -b) として走らせる。サーバが死ねば予約も消える = 送り先 pane も
# 消えているので整合する (nohup で外へ出すと pane 無しの幽霊 sleep が残る)。conf の reload
# (source-file) では死なない (隔離 -L サーバで実測 2026-08-27: source-file 後も sleeper 生存 /
# kill-server で終了)。
#
#   tmux_schedule_keys.sh            : ウィザード (bin/schedkeys を起こし、結果で予約を作る / 取り消す)
#   tmux_schedule_keys.sh fire <id>  : (内部) 予約 1 件の待機と送信。run-shell -b から呼ばれる
#
# 予約は 1 件 = 1 ファイル ($STATE_DIR/<id>.job、行順に pane_id / 発火 epoch / 文字列 / socket_path /
# サーバの pid)。
# 発火プロセスは起動直後に <id>.pid を書く。list は sleeper が居ない .job を stale として掃く
# (サーバ再起動で sleeper だけ死んだ形)。
#
# ⚠️ popup 内では #{...} フォーマットが展開されない (TMUX_PANE も無い) ため、対象 pane は
#    `tmux display-message -p` で解決し、冒頭で $pane に固定してから確認する
#    (「確認した相手」と「送る相手」の一致。tmux_kill_confirm.sh と同じ)。
# ⚠️ 送信は `send-keys -l` (リテラル)。無いと "Enter" / "C-c" のような文字列がキー名として
#    解釈される。末尾の Enter は別呼び出しで送る。
# ⚠️ fire は無音契約 (scripts/CLAUDE.md): run-shell -b から呼ばれるので、縮退時も stdout/stderr
#    へ出さず exit 0。結果は toast (bin/tmux-toast) で知らせ、失敗はログへ書く。
# ⚠️ .pid の数字だけを信じて kill しない。sleeper が異常死した後に OS が同じ pid を別プロセスへ
#    再利用しうるので、kill / 生存判定の前に「そのプロセスの command line が自分の fire <id> か」
#    を ps で確かめる (pid_is_sleeper)。敵対的レビュー 2026-08-27 で無関係な sleep が kill された
#
# 対話 UI は bin/schedkeys (Go / bubbletea v2。実装は src/schedkeys/) が持ち、このスクリプトは
# 「状態 (job ファイル) と tmux への副作用」だけを持つ。UI を Go に出した理由:
#   - gum (bubbletea v1) は本物のカーソルを隠して偽カーソルを描くため、IME の未確定文字が入力位置に
#     出ない (pty で実測 2026-08-27)。bubbletea v2 は tea.View.Cursor で本物のカーソルを置ける
#   - 「いつ送る」と「文字列」を 1 画面にまとめ、発火時刻をその場で見せられる
# 破壊的な取消は UI では実行しない (schedkeys は id を返すだけ。確認 gum confirm --default=false と
# 実行はここに残す)。
set -uo pipefail

# 予約の置き場は **tmux サーバ (socket) ごとに分ける**。pane id はサーバごとに振り直されるので、
# 全サーバで 1 つのディレクトリを共有すると、一覧・取消・失効件数が別サーバの予約を混ぜる
# (取消は別サーバの sleeper を実際に kill できた。issue 189 で実験で再現)。
# 読む側で socket を照合する形は絞り忘れが 1 箇所でも残ると漏れるので、入れ物で分ける。
# ⚠️ dir の解決は wizard だけが行う。fire は run-shell のコマンド文字列で
#    TMUX_SCHEDULE_KEYS_DIR を受け取るので、tmux へ問い合わせない (子は $TMUX を持たないことがある)
# ⚠️ 二度と使われない socket の dir に残った .job / .pid は**誰も掃かない** (prune は自 dir しか
#    見ない)。掃除機構を置かないのは意図的: 残骸は数バイトのテキストで、使い捨ての -L サーバは
#    popup と UI を経ないので予約をほぼ作らない。掃除は破壊的操作の新設なので、溜まった証拠が
#    出てから作る。再開の trigger は「予約していないのに
#    `find "$STATE_ROOT" -name '*.job'` が非空」(issue 189 done の P2-4)
STATE_ROOT="${XDG_STATE_HOME:-$HOME/.local/state}/tmux-schedule-keys"
STATE_DIR="${TMUX_SCHEDULE_KEYS_DIR:-}"

# resolve_state_dir は今のサーバの socket から置き場を決める。決められなければ 1 を返す
# (呼び出し側は予約を作らない = fail-closed。相手の分からない状態を共有 dir に混ぜない)。
# ⚠️ 旧版の共有 dir ($STATE_ROOT 直下の *.job) は移さない。移すと眠っている sleeper の
#    claim (job の rename) が失敗し、予約が黙って発火しなくなる。古い予約はそのまま発火し、
#    一覧には出なくなる (issue 189 の移行の項)
resolve_state_dir() {
  local sock enc
  if [[ -n "$STATE_DIR" ]]; then
    # env 指定 (テストと使い捨てスクリプト用の上書き)。⚠️ この経路は socket 由来の検査を
    # 通らないので、ここで同じ検査をかける。サーバ環境にこの変数が居ると全サーバが同じ dir を
    # 使う = 修正が無効になるため、効いていることをログに残す
    case "$STATE_DIR" in *$'\n'*) return 1 ;; esac
    log "置き場は env の指定を使う ($STATE_DIR)"
    return 0
  fi
  sock="$(tmux display-message -p '#{socket_path}' 2>/dev/null || true)"
  [[ -n "$sock" ]] || return 1
  case "$sock" in *$'\n'*) return 1 ;; esac
  # ⚠️ **% を先に逃がしてから / を潰す**。`/` → `%` だけだと `/tmp/a%b` と `/tmp/a/b` が
  #    同じ dir 名になり、直したはずの混ざりが黙って戻る (敵対的レビュー 2026-09-03 の P3-1)
  enc="${sock//%/%25}"
  STATE_DIR="$STATE_ROOT/${enc//\//%}"
  return 0
}

# sh_quote は sh のコマンド文字列へ埋めるための ' 囲み (中の ' は '\'' で閉じ直す)。
# tmux_escape は tmux のフォーマット展開を止める (# を ## にする)。
# ⚠️ **両方が必要**。run-shell のコマンド文字列は tmux がフォーマット展開してから sh へ渡すので、
#    socket path に `#{pane_id}` が入っていると wizard と fire で置き場がズレる
#    (tmux 3.7b で実測。敵対的レビュー 2026-09-03 の P2-1)
sh_quote() { printf "'%s'" "${1//\'/\'\\\'\'}"; }
tmux_escape() { printf '%s' "${1//\#/##}"; }
LOG="${XDG_CACHE_HOME:-$HOME/.cache}/tt-schedule-keys.log"
SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
TOAST="$(dirname "$SELF")/../bin/tmux-toast"
# 対話 UI (Go)。ビルドは同期 (bin/schedkeys 参照)
UI="${TMUX_SCHEDULE_KEYS_UI:-$(dirname "$SELF")/../bin/schedkeys}"
# 表示文字列 (toast / display-message) に絵文字・曖昧幅の記号を使わない: 端末と描画側の幅計算が
# 食い違い、行ごとに左右へずれてノイズになる (2026-08-27 ユーザー報告)。UI 側 (src/schedkeys) も同じ規律。
# tests/tmux/test_schedule_keys.sh が両方を静的に検査する
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/tmux_resurrect_guards.sh"

# fire が .pid を書くまでの猶予。これより古い .pid 無し .job だけを stale 候補にする
PID_GRACE_SECS=60

mkdir -p "$(dirname "$LOG")" 2>/dev/null || true
[[ -n "$STATE_DIR" ]] && mkdir -p "$STATE_DIR" 2>/dev/null || true

# ログは観測用で、書けなくても本体を止めない。リダイレクト自体の失敗 (dir 無し等) も stderr に
# 出さない: fire は run-shell -b の無音契約下にある
# LOG_MAX_LINES: 観測ログの上限。予約 1 件あたり数行なので増加は緩いが、rotate が無いと単調増加
# する (scripts/tmux_periodic_save.sh が tt-restore-trigger.log に対して同じことをしている)。
LOG_MAX_LINES="${TMUX_SCHEDULE_KEYS_LOG_MAX_LINES:-2000}"

log() {
  { printf '%s\t%s\n' "$(date '+%Y-%m-%dT%H:%M:%S')" "$*" >> "$LOG"; } 2>/dev/null || true
  log_rotate
}

# log_rotate は上限を超えたら末尾だけ残す。fire からも呼ばれるので無音・非破壊的に失敗する。
log_rotate() {
  local n tmp
  [[ -f "$LOG" ]] || return 0
  n="$(wc -l < "$LOG" 2>/dev/null | tr -d ' ')"
  [[ "$n" =~ ^[0-9]+$ ]] || return 0
  (( n > LOG_MAX_LINES )) || return 0
  tmp="$LOG.prune.$$"
  if tail -n "$LOG_MAX_LINES" "$LOG" > "$tmp" 2>/dev/null; then
    mv -f "$tmp" "$LOG" 2>/dev/null || rm -f "$tmp" 2>/dev/null
  else
    rm -f "$tmp" 2>/dev/null
  fi
}

# 残り秒 → "1h23m" / "45m" / "30s"
fmt_remaining() {
  local s=$1
  if (( s <= 0 )); then printf 'まもなく'; return; fi
  if (( s >= 3600 )); then printf '%dh%02dm' $((s / 3600)) $((s % 3600 / 60))
  elif (( s >= 60 )); then printf '%dm' $((s / 60))
  else printf '%ds' "$s"; fi
}

# pane_id → "session:index cmd window名"。
# ⚠️ **実行中のコマンドを入れる**: 送り先が claude なのか shell なのかは window 名から分からない
#    (実測 2026-08-28: Claude の pane は cmd=claude.exe / window名=「✳ タスク名」、shell は cmd=zsh)。
#    順序も意図的で、一覧の列は幅で切られるため、先に出したコマンドだけは残る。
# ⚠️ 消えた pane でも tmux は rc=0 を返す (実測) ので、rc では消滅を判定できない。中身が
#    区切り文字だけなら「消滅」と書く
pane_label() {
  local out bare
  out="$(tmux display-message -p -t "$1" '#{session_name}:#{window_index} #{pane_current_command} #{window_name}' 2>/dev/null || true)"
  bare="${out//[: ]/}"
  if [[ -z "$bare" ]]; then printf '(消滅)'; else printf '%s' "$out"; fi
}

# job ファイルを読む。REPLY_PANE / REPLY_AT / REPLY_TEXT / REPLY_SOCK / REPLY_SRVPID に返す。
# 壊れていれば 1。
# ⚠️ 先頭で全ての REPLY_* を空にする: ファイルを開けなかったとき (消えた・権限が無い) は
#    read が 1 回も走らず前の job の値が残り、「A を取り消す」と表示して B を消す事故になる
#    (敵対的レビュー 2026-08-28 で再現)
read_job() {
  local f=$1
  REPLY_PANE='' REPLY_AT='' REPLY_TEXT='' REPLY_SOCK='' REPLY_SRVPID=''
  [[ -f "$f" ]] || return 1
  { IFS= read -r REPLY_PANE; IFS= read -r REPLY_AT; IFS= read -r REPLY_TEXT
    IFS= read -r REPLY_SOCK || true; IFS= read -r REPLY_SRVPID || true; } < "$f"
  [[ -n "$REPLY_PANE" && "$REPLY_AT" =~ ^[0-9]+$ && -n "$REPLY_TEXT" ]]
}

# pid が「この id の sleeper」として今も生きているか (pid 再利用を command line で弾く)
pid_is_sleeper() {
  local id=$1 pid=$2 cmd
  [[ -n "$pid" && "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  # -ww: GNU ps は既定 80 桁で command 列を切る。切られると「自分の sleeper」を見失い、
  # 生きている予約を stale として消してしまう
  cmd="$(ps -ww -o command= -p "$pid" 2>/dev/null)"
  # id は command line の末尾に来る。部分一致だと id "…-5" が "…-55" の sleeper に当たる
  [[ "$cmd" == *"tmux_schedule_keys.sh fire $id" || "$cmd" == *"tmux_schedule_keys.sh fire '$id'" ]]
}

# mtime は guards.sh の tt_mtime_of に集約 (stat の GNU/BSD 方言差の罠と実測はあちらに記録)。
# ⚠️ ここに `stat -f %m ... || stat -c %Y ...` を書き戻さないこと。GNU では `-f` が
#   ファイルシステム情報を **stdout に出しつつ** rc=1 で終わるため、フォールバックの epoch と
#   連結されて数値でなくなり、prune が Linux で一切走らなくなる (CI run 33138075381 で実証)。
# mtime が取れなければ「今」扱い (= 新しい側に倒す。古い側に倒すと作った直後の予約を消す)
job_mtime() {
  local m; m="$(tt_mtime_of "$1")"
  case "$m" in ''|*[!0-9]*) m="$(date +%s)" ;; esac
  printf '%s\n' "$m"
}

# refresh_pane_indicator は「この pane に残っている予約」を pane オプション @schedkeys-at に写す
# (値 = "HH:MM" / 複数なら "HH:MM ほかN件")。表示は _tmux.conf の pane-border-format が
# このオプションを見て出す。正本は .job のままで、これは表示用の写し。
# ⚠️ 予約の状態を書き換える全経路 (new / cancel / fire の claim・drop / prune) から呼ぶ。
#    1 経路でも漏れると、予約が無い pane に幽霊表示が残り続ける (pane が消えればオプションも消える
#    ので pane 側の後始末は要らない)。
# ⚠️ read_job を使わない: 呼び出し側 (fire_send / cancel_selected) が REPLY_* を後で使うので、
#    ここで上書きすると「A を送ったと表示して B の文字列を出す」形の事故になる。
# ⚠️ **pane id の文字列一致だけで数えない**。置き場は socket ごとに分けたが (STATE_ROOT の注記)、
#    同じ socket にサーバが立ち直ると前任の job が同じ dir に残る。pane id は振り直されているので、
#    一致だけで絞るとその時刻をこちらの枠に出す。同一性は job に記録した socket + サーバ pid で見る
#    (fire_claim が送信前に見ているものと同じ。敵対的レビュー 2026-09-02)。
#    確かめられないときは何も書かない (fail-closed: 相手が分からない pane を触らない)
# ⚠️ 表示は装飾。失敗しても予約の成立・送信・取消には影響させない (fire の無音契約と同じ)
refresh_pane_indicator() {
  local pane=$1 j p at sock srvpid earliest='' n=0 hm v now_sock now_srvpid
  [[ -n "$pane" ]] || return 0
  now_sock="$(tmux display-message -p '#{socket_path}' 2>/dev/null || true)"
  now_srvpid="$(tmux display-message -p '#{pid}' 2>/dev/null || true)"
  [[ -n "$now_sock" && -n "$now_srvpid" ]] || return 0
  for j in "$STATE_DIR"/*.job; do
    [[ -f "$j" ]] || continue
    # 3 行目 (文字列) は読み飛ばす: ここでは使わない
    { IFS= read -r p; IFS= read -r at; IFS= read -r _
      IFS= read -r sock || true; IFS= read -r srvpid || true; } < "$j" 2>/dev/null || continue
    [[ "$p" == "$pane" && "$at" =~ ^[0-9]+$ ]] || continue
    [[ "$sock" == "$now_sock" && "$srvpid" == "$now_srvpid" ]] || continue
    n=$((n + 1))
    if [[ -z "$earliest" ]] || (( at < earliest )); then earliest="$at"; fi
  done
  # 時刻に化けたら (date が壊れた) 表示を消す: 嘘の時刻を出すより無い方が安全
  if (( n > 0 )); then hm="$(date -r "$earliest" '+%H:%M' 2>/dev/null || true)"; fi
  if (( n == 0 )) || [[ -z "${hm:-}" ]]; then
    tmux set-option -pu -t "$pane" @schedkeys-at 2>/dev/null || true
    return 0
  fi
  v="$hm"
  (( n > 1 )) && v="$hm ほか$((n - 1))件"
  tmux set-option -p -t "$pane" @schedkeys-at "$v" 2>/dev/null || true
}

# sleeper が居ない予約 (サーバ再起動で sleeper だけ消えた形) を掃く。kill はしない
prune_stale() {
  local j id pid n=0 nowsrv srvpid
  # ⚠️ 同じ socket にサーバが立ち直ると、前任サーバの job が同じ dir に残る。pane id は
  #    振り直されているので、その job は一覧に**今のサーバの pane 名で**並び、発火時には
  #    fire_claim が拒否する (= 待たされた末に届かない)。入れ物を分けても socket が同じなら
  #    混ざるので、ここで失効させる (敵対的レビュー 2026-09-03 の P2-3)。
  #    kill はしない: job を消せば前任の sleeper は claim に失敗して静かに降りる
  nowsrv="$(tmux display-message -p '#{pid}' 2>/dev/null || true)"
  for j in "$STATE_DIR"/*.job; do
    [[ -f "$j" ]] || continue
    id="$(basename "$j" .job)"
    pid=$(cat "$STATE_DIR/$id.pid" 2>/dev/null || true)
    # 前任サーバの job (記録された pid が今のサーバと違う) は .pid の有無に関わらず失効。
    # サーバ pid が取れないときは触らない (確かめられないものを消さない)
    srvpid="$(sed -n 5p "$j" 2>/dev/null || true)"
    if [[ -n "$nowsrv" && -n "$srvpid" && "$srvpid" != "$nowsrv" ]]; then
      log "prune 前任サーバの予約 $id (job=$srvpid now=$nowsrv)"
      local opane; opane="$(head -n 1 "$j" 2>/dev/null || true)"
      rm -f "$j" "$STATE_DIR/$id.pid"
      refresh_pane_indicator "$opane"
      n=$((n + 1))
      continue
    fi
    # .pid 無し = fire が起動直後 (書く前) かもしれないので、猶予内は触らない。
    # mtime が取れなければ「今」扱い (= 新しい側に倒す。古い側に倒すと作った直後の予約を消す)
    if [[ -z "$pid" ]]; then
      (( $(date +%s) - $(job_mtime "$j") > PID_GRACE_SECS )) || continue
    fi
    if ! pid_is_sleeper "$id" "$pid"; then
      log "prune stale $id pid=${pid:-none}"
      # 消す前に送り先を控える (消した後では読めない)
      local pane; pane="$(head -n 1 "$j" 2>/dev/null || true)"
      rm -f "$j" "$STATE_DIR/$id.pid"
      refresh_pane_indicator "$pane"
      n=$((n + 1))
    fi
  done
  # ⚠️ 黙って消さない: ユーザーは一覧を開いた時点で「無い」ことしか分からず、待ち続ける
  #    (サーバ再起動で失効するのが主因)。件数だけでも伝える
  (( n > 0 )) && notify "サーバ再起動などで ${n} 件の予約が失効しました"
  # 対応する .job が無い .pid も掃く (prune は *.job を起点に走るので、孤児は誰も回収しない)
  for j in "$STATE_DIR"/*.pid; do
    [[ -f "$j" ]] || continue
    [[ -f "${j%.pid}.job" ]] && continue
    pid=$(cat "$j" 2>/dev/null || true)
    id="$(basename "$j" .pid)"
    pid_is_sleeper "$id" "$pid" || rm -f "$j"
  done
  return 0
}

# ui_run は対話 UI を起こし、結果行 (action <TAB> ...) を REPLY_UI に返す。
# 戻り値: 0 = 結果あり / 1 = ユーザーが閉じた (中止) / 2 = UI が動かなかった (異常)
# ⚠️ 中止と異常を分ける。UI はビルド失敗でも不在でも 0 以外で終わるので、一緒くたにすると
#    「押しても何も起きないキー」になり、原因がどこにも残らない (監査 2026-08-28)。
#    中止は UI が out へ "abort" と書いて exit 0 で知らせる契約
ui_run() {
  local jobs_file=$1 label=$2 start=${3:-} out rc=0
  out="$(mktemp "${TMPDIR:-/tmp}/schedkeys.XXXXXX")" || return 1
  # popup が開いている間 prefix は tmux のキーテーブルへ届かず UI に素通りする (実測 2026-08-28)。
  # prefix キーを渡しておくと、起動キー (prefix+m / Enter / C-m) の再入力で閉じられる
  local prefix_key
  prefix_key="$(tmux show-options -gv prefix 2>/dev/null || true)"
  "$UI" --label "$label" --jobs "$jobs_file" --out "$out" --toggle-prefix "$prefix_key" --start "$start" || rc=$?
  REPLY_UI=''
  if (( rc == 0 )); then IFS= read -r REPLY_UI < "$out" || REPLY_UI=''; fi
  rm -f "$out"
  if (( rc != 0 )); then
    log "ui: 起動できない (rc=$rc)"
    return 2
  fi
  [[ -n "$REPLY_UI" && "$REPLY_UI" != "abort" ]]
}

# new_reservation は UI が返した発火 epoch と文字列で予約を作る。
# 成功時に何も表示しないのは、UI 側がトーストを出してから閉じているため (二重に出さない)
new_reservation() {
  local pane=$1 label=$2 at=$3 text=$4 sock id
  [[ "$at" =~ ^[0-9]+$ && -n "$text" ]] || { log "new: 不正な UI 結果 at=$at text=$text"; return 1; }
  # fire は run-shell -b の子 (サーバ環境を継承) なので、bare tmux がどの socket へ行くか保証が無い。
  # 予約時の socket を job に残し、fire 側で $TMUX にして同じサーバへ向ける
  local srvpid
  sock="$(tmux display-message -p '#{socket_path}' 2>/dev/null || true)"
  # ⚠️ サーバの pid も残す: サーバが異常終了 (SIGKILL) すると sleeper だけが孤児として生き残り、
  #    同じ socket に立った**別のサーバ**の pane へ送ってしまう (pane id は振り直されるので
  #    「%5 は存在しない」に逃げられない。敵対的レビュー 2026-08-28 で再現)
  srvpid="$(tmux display-message -p '#{pid}' 2>/dev/null || true)"
  id="$(date +%s)-$$-$RANDOM"
  printf '%s\n%s\n%s\n%s\n%s\n' "$pane" "$at" "$text" "$sock" "$srvpid" > "$STATE_DIR/$id.job" 2>/dev/null \
    || { log "new: job を書けない ($STATE_DIR/$id.job)"; return 1; }
  # id は英数と - だけなので run-shell の引用は安全
  # ⚠️ run-shell の失敗を握らない: 失敗すると sleeper が居ないまま job だけ残り、UI は
  #    「予約しました」と言い切っている
  # ⚠️ 置き場を env で渡す: fire は自分では解決できない (run-shell の子は $TMUX を持たないことが
  #    ある)。sh の引用と tmux のフォーマット展開の二段を通るので、両方を潰す (sh_quote / tmux_escape)
  tmux run-shell -b "$(tmux_escape "TMUX_SCHEDULE_KEYS_DIR=$(sh_quote "$STATE_DIR") $(sh_quote "$SELF") fire $(sh_quote "$id")")" 2>/dev/null || true
  # ⚠️ run-shell -b の終了コードは当てにならない (子の exec 失敗も exit 1 も rc=0。実測 2026-08-28)。
  #    sleeper が起きた証拠は「.pid を書いたか」で見る。書かれなければ予約は成立していない
  local i=0
  while [[ ! -s "$STATE_DIR/$id.pid" && $i -lt 30 ]]; do sleep 0.1; i=$((i + 1)); done
  if [[ ! -s "$STATE_DIR/$id.pid" ]]; then
    log "new: sleeper が起きない ($id)"
    rm -f "$STATE_DIR/$id.job" "$STATE_DIR/$id.pid"
    return 1
  fi
  log "new $id pane=$pane at=$at text=$text"
  refresh_pane_indicator "$pane"
}

# jobs_tsv は今ある予約を UI 用の TSV (id / 発火 epoch / 送り先の表示名 / 文字列) に書き出す。
# job ファイルの書式を知るのはこのスクリプトだけに保つ
jobs_tsv() {
  local out=$1 j id lbl
  : > "$out"
  for j in "$STATE_DIR"/*.job; do
    [[ -f "$j" ]] || continue
    read_job "$j" || continue
    id="$(basename "$j" .job)"
    # 表示名にタブが入ると列がずれる (window 名は任意の文字列)。改行は行そのものが割れる
    lbl="$(pane_label "$REPLY_PANE")"
    lbl="${lbl//$'\t'/ }"; lbl="${lbl//$'\n'/ }"
    printf '%s\t%s\t%s\t%s\n' "$id" "$REPLY_AT" "$lbl" "$REPLY_TEXT" >> "$out"
  done
}

# cancel_selected は UI が選んだ予約を確認してから取り消す (破壊的操作はシェル側に残す)
cancel_selected() {
  local id=$1 j
  j="$STATE_DIR/$id.job"
  if ! read_job "$j"; then
    # 一覧を出してから選ぶまでの間に発火した / 既に消えた
    tmux display-message "その予約はもうありません (発火済みか取消済み)"
    return 0
  fi
  local grc=0
  gum confirm --default=false --affirmative "取消する" --negative "やめる" \
    "この予約を取り消す？ $(fmt_remaining $(( REPLY_AT - $(date +%s) ))) $(pane_label "$REPLY_PANE") $REPLY_TEXT" || grc=$?
  # ⚠️ 1 (やめる) / 130 (Ctrl-C) 以外は「確認できなかった」= gum 不在や端末を掴めない等。
  #    黙って閉じると、取消手段が壊れていることに気づけない (監査 2026-08-28)
  case "$grc" in
    0) ;;
    1|130) return 0 ;;
    *) log "cancel $id: 確認できない (gum rc=$grc)"; notify "取消の確認ができませんでした (gum: rc=$grc)"; return 0 ;;
  esac
  # ⚠️ 確認ダイアログを読んでいる間に発火しうる。job が消えていたら「取り消した」と言わない
  #    (取消の動機は「もう実行したくない」なので、嘘は実害に直結する)
  if [[ ! -f "$j" ]]; then
    log "cancel $id: 確認中に発火済み"
    tmux display-message "$(msg_escape "取り消せませんでした (確認中に送信されました): $REPLY_TEXT")"
    return 0
  fi
  if cancel_job "$id"; then
    tmux display-message "$(msg_escape "予約を取り消した: $REPLY_TEXT")"
  else
    tmux display-message "$(msg_escape "取り消せませんでした (既に送信されたか、予約が消えています): $REPLY_TEXT")"
  fi
}

# msg_escape は display-message のフォーマット展開を止める (# を ## にする)。
# 予約の文字列には # が普通に入る (コメント・#{...}) ので、そのまま渡すと化ける
msg_escape() { printf '%s' "${1//\#/##}"; }

# cancel_job は sleeper を止めて後片付けする。実際に止められたら 0、既に居なければ 1。
# ⚠️ 「止められなかった」= 発火済みか prune 済み。呼び出し側はそれを「取り消した」と言ってはいけない
#    (fire は送信直前に trap で TERM を無視するので、kill が届いても送信は完走する)
cancel_job() {
  local id=$1 job pid rc pane
  job="$STATE_DIR/$id.job"
  # 消す前に送り先を控える (表示の更新に使う。read_job は呼び出し側の REPLY_* を壊すので使わない)
  pane="$(head -n 1 "$job" 2>/dev/null || true)"
  # ⚠️ 成否は kill の rc ではなく **claim を勝ち取れたか** で決める。fire は送信直前に TERM を
  #    無視するので、kill が exit 0 でも送信は完走しうる。job を rename できた = fire はまだ
  #    claim していない = 確実に止められた、と言える (監査 2026-08-28)
  if mv "$job" "$job.cancelled" 2>/dev/null; then rc=0; else rc=1; fi
  pid=$(cat "$STATE_DIR/$id.pid" 2>/dev/null || true)
  if pid_is_sleeper "$id" "$pid"; then kill "$pid" 2>/dev/null || true; fi
  rm -f "$job" "$job.cancelled" "$STATE_DIR/$id.pid"
  log "cancel $id pid=${pid:-none} claimed=$rc"
  refresh_pane_indicator "$pane"
  return "$rc"
}

# 内部: 待機して送る。run-shell -b の子なので stdout/stderr へ出さない (無音契約)
# 内部: 待機して送る。run-shell -b の子なので stdout/stderr へ出さない (無音契約)。
# 流れは「自分を記録 → 眠る → 送る権利を取る → 送る」の 4 段。権利の取得と送信は下の 2 つに分けて
# ある (安全側の判断と、tmux へキーを流す作法を混ぜない)
cmd_fire() {
  local id=$1 job now wait_s
  # 無音契約 (issue 129)。⚠️ **ここに置く。ファイル先頭には置けない**: このスクリプトは対話の
  # wizard も兼ねており、先頭で塞ぐと popup の gum が端末を掴めなくなる。
  # fire は run-shell -b の子として最長 30 日生きるので、その間 tmux のパイプを掴んだままにしない
  # (掴んだままだと run-shell がずっと active で、サーバ死亡時に SIGPIPE を受ける)。
  # ⚠️ view-mode を積む支配的な要因は rc≠0 の方で、exec では塞げない (issue 111 の実測)。
  #    この関数が exit 0 以外で抜けないことは、下の各経路 (fire_drop / 失敗時の exit 0) が担う
  exec </dev/null >/dev/null 2>&1
  job="$STATE_DIR/$id.job"
  read_job "$job" || fire_drop "$id" "job を読めない" ""
  # ⚠️ ここも stderr を出さない (無音契約)。書けなければ予約は成立しないので、new 側の
  #    「.pid が現れない」検出に任せて静かに降りる
  printf '%s\n' "$$" > "$STATE_DIR/$id.pid" 2>/dev/null \
    || { log "fire $id: .pid を書けない"; exit 0; }
  # 予約時と同じサーバへ向ける ($TMUX の 1 フィールド目が socket。bare tmux はこれを見る)
  [[ -n "$REPLY_SOCK" ]] && export TMUX="$REPLY_SOCK,0,0"

  # ⚠️ date の出力を算術へ直に入れない。空を返した瞬間に「operand expected」が stderr へ出て
  #    rc=1 になり、無音契約 (run-shell の子は stdout/stderr へ出さず exit 0) を破る
  now="$(date +%s 2>/dev/null || true)"
  [[ "$now" =~ ^[0-9]+$ ]] || fire_drop "$id" "時刻が取れない" "$REPLY_TEXT"
  wait_s=$(( REPLY_AT - now ))
  (( wait_s > 0 )) && sleep "$wait_s"

  # claim を取れない = 取消された (取消側が通知するのでここは黙る) か、既に誰かが送った
  fire_claim "$id" "$job" || { log "fire $id: claim を取れず終了 (取消済み)"; rm -f "$STATE_DIR/$id.pid"; exit 0; }
  # ここから先は割り込まれない: 文字列だけ打たれて Enter が届かない半端な状態を作らない
  trap '' TERM INT HUP
  fire_send "$id"
  exit 0
}

# fire_claim は「この予約を送る権利を取る」。取れたら 0、取れなければ理由をログに残して 1。
# ⚠️ 名前のとおり判定だけでなく**所有権の移動**をする: 取れた時点で job / pid ファイルを消す。
#    取消側はファイルの有無で「もう止められない」を知るので、判定と削除は分けられない
#    (分けると、判定を通ってから削除するまでの間に取消が「取り消した」と嘘をつく窓ができる)。
fire_claim() {
  local id=$1 job=$2 nowpid
  # ⚠️ 「在るか確かめてから消す」ではなく **rename で取る**。確認と削除の間に取消が入ると、
  #    取消側は「消せた = 止めた」と誤解して「取り消した」と表示しつつ、こちらは送信を完走する
  #    (監査 2026-08-28)。rename は原子的なので、勝った側だけが先へ進む
  mv "$job" "$job.claimed" 2>/dev/null || return 1
  rm -f "$job.claimed" "$STATE_DIR/$id.pid"
  # ⚠️ サーバの同一性は「起きた後・送る直前」に見る。眠る前に見ても意味が無い (壊れるのは
  #    眠っている間にサーバが死んで別のサーバが立つ経路。実機で確認 2026-08-28)。
  #    socket が同じでも中身が別サーバなら、pane id は振り直されていて送り先は別物。
  #    記録が無い job も送らない (fail-closed): 確かめられないものを送らない
  nowpid="$(tmux display-message -p '#{pid}' 2>/dev/null || true)"
  if [[ -z "$REPLY_SRVPID" || "$nowpid" != "$REPLY_SRVPID" ]]; then
    log "fire $id: 予約したサーバを確かめられない (job=${REPLY_SRVPID:-none} now=${nowpid:-none})"
    # ⚠️ 表示にも触らない。今この socket に居るのは別サーバで、そこの $REPLY_PANE は
    #    無関係な pane (pane id は振り直される)。空にして fire_drop の refresh を no-op にする
    REPLY_PANE=''
    fire_drop "$id" "予約したときの tmux サーバがもう居ません" "$REPLY_TEXT"
  fi
  # 表示の更新は同一性を確かめた後 (確かめる前に書くと、別サーバの無関係な pane を触る)
  refresh_pane_indicator "$REPLY_PANE"
}

# fire_drop は「送らずに終わる」唯一の出口。⚠️ 破棄はログだけにしない: ユーザーは来ない入力を
# 待ち続けることになる (監査 2026-08-28。送信失敗だけ通知され、他の破棄は無音だった)。
# 呼んだら戻らない (exit 0。無音契約のため終了コードは常に 0)。
fire_drop() {
  local id=$1 why=$2 text=$3
  log "fire $id: ${why}。破棄 text=$text"
  rm -f "$STATE_DIR/$id.job" "$STATE_DIR/$id.pid"
  # job を読めなかった経路では REPLY_PANE が空 (その場合は何もしない)
  refresh_pane_indicator "${REPLY_PANE:-}"
  notify "予約入力を破棄: $why${text:+ ($text)}"
  exit 0
}

# fire_send は実際に pane へ流す。ここは tmux へキーを送る作法だけを持つ。
fire_send() {
  local id=$1 send_text
  # copy-mode 等に入っている pane へはキーが届かない (mode のキーテーブルへ行き、リテラル送信は
  # "not in a mode" で rc=1 になる。tmux 3.7b で実測 2026-08-28)。人が打つときと同じように
  # mode を抜けてから送る。抜けられなくても送信は試す (判定は send-keys の rc に任せる)
  if [[ "$(tmux display-message -p -t "$REPLY_PANE" '#{pane_in_mode}' 2>/dev/null)" == 1 ]]; then
    log "fire $id: pane $REPLY_PANE が mode 中なので抜ける"
    tmux send-keys -t "$REPLY_PANE" -X cancel 2>/dev/null || true
  fi
  # ⚠️ 末尾の ; は tmux のコマンド区切りとして食われる (`--` では守れない。実測 2026-08-28:
  #    "echo a ;" は "echo a" として届く)。最後の 1 個だけ \; にすると通る (途中の ; は素通しで、
  #    そこを escape すると逆にバックスラッシュが残る)
  send_text="$REPLY_TEXT"
  case "$send_text" in *\;) send_text="${send_text%;}\\;" ;; esac
  # ⚠️ 本文と Enter は **1 回の tmux 呼び出し** (コマンドリスト) で送る。2 回に分けると、同時刻に
  #    発火した別の予約が間に割り込み、pane では 2 つの文字列が 1 行に連結されて実行される
  #    (実測 2026-08-28: 同じ HH:MM の予約 2 件で "BBBB…AAAA…" が 1 行になった。parseClock は秒を
  #    0 に落とすので同時刻は簡単に作れる)。1 呼び出しなら tmux のコマンドキューで 1 単位になる。
  #    分けないことで「本文は送れたが Enter が失敗」も構造的に消える。
  # send-keys の stderr は捨てる (pane が直前に消えた場合の "can't find pane" が run-shell 経由で
  # アクティブ pane の view-mode に積まれる)。成否は rc で分ける
  if tmux send-keys -t "$REPLY_PANE" -l -- "$send_text" ';' send-keys -t "$REPLY_PANE" Enter 2>/dev/null; then
    log "fire $id: sent to $REPLY_PANE text=$REPLY_TEXT"
    toast "予約入力を送信: $(pane_label "$REPLY_PANE") <- $REPLY_TEXT"
  else
    log "fire $id: pane $REPLY_PANE へ送れず破棄 text=$REPLY_TEXT"
    notify "予約入力を破棄: 送り先へ送れませんでした ($REPLY_TEXT)"
  fi
}

# notify はユーザーに必ず届ける通知。⚠️ toast (bin/tmux-toast) は 2 秒の再入ガードと agent panel の
# 沈黙窓を持ち、**黙って落ちる**ので失敗の通知には使えない (監査 2026-08-28)。status 行の
# display-message はレート制限が無い。# はフォーマットとして展開されるので潰す
notify() { tmux display-message "$(msg_escape "$1")" 2>/dev/null || true; }

# toast は装飾 (失敗しても送信結果には影響させない)。tmux-toast は「tmux 内か」を $TMUX の有無で
# 判定するが、run-shell -b の子はサーバ環境を継承するため $TMUX が無いことがある (socket を
# job に残せなかった場合)。判定を通すためだけに非空をセットする
toast() {
  [[ -x "$TOAST" ]] || return 0
  TMUX="${TMUX:-run-shell}" "$TOAST" -d 4 "$@" >/dev/null 2>&1 || true
}

cmd_wizard() {
  local pane label jobs_file tab start="" ui_rc f1 f2 f3 extra
  # 置き場が決まらなければ何もしない (どのサーバの予約か分からないまま共有 dir へ混ぜない)
  if ! resolve_state_dir; then
    log "wizard: socket が取れないので置き場を決められない"
    notify "予約入力を開けませんでした (tmux サーバを特定できません)"
    return 1
  fi
  mkdir -p "$STATE_DIR" 2>/dev/null || true
  tab=$'\t'
  # ⚠️ popup が外から閉じられる (ウィンドウを閉じる / kill-session) と成功パスの rm を通らず、
  #    全予約の文字列を含む一時ファイルが TMPDIR に残る (監査 2026-08-28)
  jobs_file=''
  trap 'rm -f "${jobs_file:-}" 2>/dev/null' EXIT
  pane="$(tmux display-message -p '#{pane_id}')" || return 1
  label="$(pane_label "$pane")"
  jobs_file="$(mktemp "${TMPDIR:-/tmp}/schedkeys-jobs.XXXXXX")" || return 1

  # ⚠️ 取消したら**一覧へ戻す** (ユーザー要望 2026-08-28)。取消の実行はここ (gum の確認つき) に
  #    あるので、UI をいったん閉じ、更新した一覧でもう一度開く。1 件消すたびに popup を閉じない。
  #
  # ⚠️ 回る回数に上限を置く。UI が同じ結果を返し続けると無限に回る (対話 UI なら起きないが、
  #    壊れた UI・stub では実際に起きた)。上限は「今ある予約を全部消せる回数 + 余裕」
  local rounds=0 max_rounds
  max_rounds=$(( $(find "$STATE_DIR" -name '*.job' 2>/dev/null | wc -l) + 5 ))
  while (( rounds < max_rounds )); do
    rounds=$((rounds + 1))
    prune_stale
    jobs_tsv "$jobs_file"
    ui_rc=0
    ui_run "$jobs_file" "$label" "$start" || ui_rc=$?
    case "$ui_rc" in
      0) ;;
      1) return 0 ;;  # ユーザーが閉じた
      *) notify "予約入力の画面を開けませんでした (~/.cache/tt-schedule-keys.log)"; return 1 ;;
    esac

    # ⚠️ フィールド数ごと検証する。%%/# の展開で切り出すと、区切りが足りない行 ("new<TAB>4600") が
    #    epoch をそのまま文字列として通してしまう (敵対的レビュー 2026-08-28 で再現)
    IFS="$tab" read -r f1 f2 f3 extra <<< "$REPLY_UI"
    case "$f1" in
      new)
        if [[ -z "$f3" || -n "$extra" ]]; then log "wizard: new の形が違う: $REPLY_UI"; return 0; fi
        # UI は確定した時点で「予約しました」とトーストを出して閉じている。ここで失敗したら
        # その表示が嘘になるので、失敗だけを目立つ形で知らせる (成功は UI のトーストが担当)
        new_reservation "$pane" "$label" "$f2" "$f3" \
          || notify "予約に失敗しました (~/.cache/tt-schedule-keys.log)"
        return 0
        ;;
      cancel)
        if [[ -z "$f2" || -n "$f3" || -n "$extra" ]]; then log "wizard: cancel の形が違う: $REPLY_UI"; return 0; fi
        cancel_selected "$f2"
        start="pick"   # 取り消したら一覧へ戻る (残りが 0 件なら UI 側がメニューを出す)
        ;;
      *) log "wizard: 未知の UI 結果: $REPLY_UI"; return 0 ;;
    esac
  done
  log "wizard: 上限 ($max_rounds 回) まで回った。UI が同じ結果を返し続けている"
  notify "予約入力を閉じました (画面が応答していません)"
  return 1
}

case "${1:-wizard}" in
  wizard) cmd_wizard ;;
  # fire は置き場を env で受け取る (run-shell のコマンド文字列。上の new_reservation 参照)。
  # 無ければ動けないので黙って降りる (無音契約)
  fire)   [[ -n "${2:-}" && -n "$STATE_DIR" ]] || exit 0; cmd_fire "$2" ;;
  *) echo "usage: $0 [wizard|fire <id>]" >&2; exit 1 ;;
esac
