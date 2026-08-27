#!/usr/bin/env bash
# tmux: 「N 時間 M 分後にこの pane へ文字列を入力する」予約ウィザード。_tmux.conf の bind m から
# display-popup -E 経由で呼ばれる。tmux 自体にタイマー機能は無いので、sleep + send-keys を
# tmux サーバの子 (run-shell -b) として走らせる。サーバが死ねば予約も消える = 送り先 pane も
# 消えているので整合する (nohup で外へ出すと pane 無しの幽霊 sleep が残る)。conf の reload
# (source-file) では死なない (隔離 -L サーバで実測 2026-08-27: source-file 後も sleeper 生存 /
# kill-server で終了)。
#
#   tmux_schedule_keys.sh            : ウィザード (新規予約 / 予約一覧・取消 を選ぶ)
#   tmux_schedule_keys.sh new        : 新規予約 (対象 = popup 直下のアクティブ pane)
#   tmux_schedule_keys.sh list       : 予約一覧。選んだものを取消
#   tmux_schedule_keys.sh fire <id>  : (内部) 予約 1 件の待機と送信。run-shell -b から呼ばれる
#
# 予約は 1 件 = 1 ファイル ($STATE_DIR/<id>.job、行順に pane_id / 発火 epoch / 文字列 / socket_path)。
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
set -uo pipefail

STATE_DIR="${TMUX_SCHEDULE_KEYS_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/tmux-schedule-keys}"
LOG="${XDG_CACHE_HOME:-$HOME/.cache}/tt-schedule-keys.log"
SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
TOAST="$(dirname "$SELF")/../bin/tmux-toast"
# 時間・分の上限 (桁数)。無制限だと 64bit の掛け算があふれて別の (短い) 時刻に予約される
MAX_DIGITS=5
# fire が .pid を書くまでの猶予。これより古い .pid 無し .job だけを stale 候補にする
PID_GRACE_SECS=60

mkdir -p "$STATE_DIR" "$(dirname "$LOG")" 2>/dev/null || true

# ログは観測用で、書けなくても本体を止めない。リダイレクト自体の失敗 (dir 無し等) も stderr に
# 出さない: fire は run-shell -b の無音契約下にある
log() { { printf '%s\t%s\n' "$(date '+%Y-%m-%dT%H:%M:%S')" "$*" >> "$LOG"; } 2>/dev/null || true; }

# 残り秒 → "1h23m" / "45m" / "30s"
fmt_remaining() {
  local s=$1
  if (( s <= 0 )); then printf 'まもなく'; return; fi
  if (( s >= 3600 )); then printf '%dh%02dm' $((s / 3600)) $((s % 3600 / 60))
  elif (( s >= 60 )); then printf '%dm' $((s / 60))
  else printf '%ds' "$s"; fi
}

# pane_id → "session:index name" (消えていれば "(消滅)")
pane_label() {
  tmux display-message -p -t "$1" '#{session_name}:#{window_index} #{window_name}' 2>/dev/null || printf '(消滅)'
}

# job ファイルを読む。REPLY_PANE / REPLY_AT / REPLY_TEXT / REPLY_SOCK に返す。壊れていれば 1
read_job() {
  local f=$1
  [[ -f "$f" ]] || return 1
  REPLY_SOCK=''
  { IFS= read -r REPLY_PANE; IFS= read -r REPLY_AT; IFS= read -r REPLY_TEXT; IFS= read -r REPLY_SOCK || true; } < "$f"
  [[ -n "$REPLY_PANE" && "$REPLY_AT" =~ ^[0-9]+$ && -n "$REPLY_TEXT" ]]
}

# pid が「この id の sleeper」として今も生きているか (pid 再利用を command line で弾く)
pid_is_sleeper() {
  local id=$1 pid=$2 cmd
  [[ -n "$pid" && "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  cmd="$(ps -o command= -p "$pid" 2>/dev/null)"
  [[ "$cmd" == *"tmux_schedule_keys.sh fire $id"* || "$cmd" == *"tmux_schedule_keys.sh fire '$id'"* ]]
}

job_mtime() { stat -f %m "$1" 2>/dev/null || stat -c %Y "$1" 2>/dev/null || date +%s; }

# sleeper が居ない予約 (サーバ再起動で sleeper だけ消えた形) を掃く。kill はしない
prune_stale() {
  local j id pid
  for j in "$STATE_DIR"/*.job; do
    [[ -f "$j" ]] || continue
    id="$(basename "$j" .job)"
    pid=$(cat "$STATE_DIR/$id.pid" 2>/dev/null || true)
    # .pid 無し = fire が起動直後 (書く前) かもしれないので、猶予内は触らない。
    # mtime が取れなければ「今」扱い (= 新しい側に倒す。古い側に倒すと作った直後の予約を消す)
    if [[ -z "$pid" ]]; then
      (( $(date +%s) - $(job_mtime "$j") > PID_GRACE_SECS )) || continue
    fi
    if ! pid_is_sleeper "$id" "$pid"; then
      log "prune stale $id pid=${pid:-none}"
      rm -f "$j" "$STATE_DIR/$id.pid"
    fi
  done
}

cmd_new() {
  local pane label sock h m text total at id
  pane="$(tmux display-message -p '#{pane_id}')" || return 1
  label="$(pane_label "$pane")"
  # fire は run-shell -b の子 (サーバ環境を継承) なので、bare tmux がどの socket へ行くか保証が無い。
  # 予約時の socket を job に残し、fire 側で $TMUX にして同じサーバへ向ける
  sock="$(tmux display-message -p '#{socket_path}' 2>/dev/null || true)"

  h="$(gum input --header "⏰ 何時間後？ (対象: $label)" --placeholder "0" --value "0")" || return 1
  m="$(gum input --header "⏰ 何分後？ (対象: $label)" --placeholder "0" --value "0")" || return 1
  h="${h:-0}"; m="${m:-0}"
  local re="^[0-9]{1,$MAX_DIGITS}\$"
  if ! [[ "$h" =~ $re && "$m" =~ $re ]]; then
    gum style --foreground 1 "✗ 時間・分は 0 以上 ${MAX_DIGITS} 桁以内の整数で" ; sleep 1.2; return 1
  fi
  total=$(( h * 3600 + m * 60 ))
  if (( total <= 0 )); then
    gum style --foreground 1 "✗ 0 時間 0 分は予約できない" ; sleep 1.2; return 1
  fi
  text="$(gum input --header "⌨️  入力する文字列 (末尾に Enter を送る)" --placeholder "make test" --width 60)" || return 1
  [[ -n "$text" ]] || { gum style --foreground 1 "✗ 空文字は予約できない"; sleep 1.2; return 1; }

  at=$(( $(date +%s) + total ))
  gum confirm --default=false --affirmative "予約する" --negative "やめる" \
    "$(printf '%s に %s後 (%s) に送る:\n  %s' "$label" "$(fmt_remaining "$total")" "$(date -r "$at" '+%H:%M' 2>/dev/null || date -d "@$at" '+%H:%M')" "$text")" \
    || return 1

  id="$(date +%s)-$$-$RANDOM"
  printf '%s\n%s\n%s\n%s\n' "$pane" "$at" "$text" "$sock" > "$STATE_DIR/$id.job"
  # id は英数と - だけなので run-shell の引用は安全
  tmux run-shell -b "'$SELF' fire '$id'"
  log "new $id pane=$pane at=$at text=$text"
  tmux display-message "予約: $(fmt_remaining "$total")後に $label へ送る"
}

cmd_list() {
  prune_stale
  local -a ids lines
  local j id now rem n=0
  now=$(date +%s)
  for j in "$STATE_DIR"/*.job; do
    [[ -f "$j" ]] || continue
    read_job "$j" || continue
    id="$(basename "$j" .job)"
    rem=$(( REPLY_AT - now ))
    n=$((n + 1))
    ids+=("$id")
    # 行頭の連番が選択の鍵。同じ pane・同じ文字列・同じ残り時間の予約は表示が一致するので、
    # 表示文字列で逆引きすると先頭の 1 件に化ける (敵対的レビュー 2026-08-27)
    lines+=("$(printf '%d) %s · %s · %s' "$n" "$(fmt_remaining "$rem")" "$(pane_label "$REPLY_PANE")" "$REPLY_TEXT")")
  done
  if (( ${#ids[@]} == 0 )); then
    gum style --foreground 3 "予約はありません"; sleep 1.2; return 0
  fi
  local sel idx
  sel="$(printf '%s\n' "${lines[@]}" | gum choose --header "📋 予約一覧 (選んで取消 / Esc で戻る)" --height 12)" || return 0
  idx="${sel%%)*}"
  [[ "$idx" =~ ^[0-9]+$ && idx -ge 1 && idx -le ${#ids[@]} ]] || return 0
  id="${ids[$((idx - 1))]}"
  gum confirm --default=false --affirmative "取消する" --negative "やめる" "この予約を取り消す？ $sel" || return 0
  cancel_job "$id"
  tmux display-message "予約を取り消した: $sel"
}

cancel_job() {
  local id=$1 pid
  pid=$(cat "$STATE_DIR/$id.pid" 2>/dev/null || true)
  if pid_is_sleeper "$id" "$pid"; then kill "$pid" 2>/dev/null; fi
  rm -f "$STATE_DIR/$id.job" "$STATE_DIR/$id.pid"
  log "cancel $id pid=${pid:-none}"
}

# 内部: 待機して送る。run-shell -b の子なので stdout/stderr へ出さない (無音契約)
cmd_fire() {
  local id=$1 job now wait_s
  job="$STATE_DIR/$id.job"
  read_job "$job" || { log "fire $id: job unreadable"; rm -f "$job" "$STATE_DIR/$id.pid"; exit 0; }
  printf '%s\n' "$$" > "$STATE_DIR/$id.pid"
  # 予約時と同じサーバへ向ける ($TMUX の 1 フィールド目が socket。bare tmux はこれを見る)
  [[ -n "$REPLY_SOCK" ]] && export TMUX="$REPLY_SOCK,0,0"
  now=$(date +%s)
  wait_s=$(( REPLY_AT - now ))
  (( wait_s > 0 )) && sleep "$wait_s"
  # 取消されていたら送らない (kill が sleep の隙間に届かなかった場合の保険)
  [[ -f "$job" ]] || exit 0
  rm -f "$job" "$STATE_DIR/$id.pid"
  # ここから先は割り込まれない: 文字列だけ打たれて Enter が届かない半端な状態を作らない
  trap '' TERM INT HUP
  # send-keys の stderr は捨てる (pane が直前に消えた場合の "can't find pane" が run-shell 経由で
  # アクティブ pane の view-mode に積まれる)。成否は rc で分ける
  if tmux send-keys -t "$REPLY_PANE" -l -- "$REPLY_TEXT" 2>/dev/null; then
    tmux send-keys -t "$REPLY_PANE" Enter 2>/dev/null || true
    log "fire $id: sent to $REPLY_PANE text=$REPLY_TEXT"
    toast "⏰ 予約入力を送信: $(pane_label "$REPLY_PANE") ← $REPLY_TEXT"
  else
    log "fire $id: pane $REPLY_PANE gone, dropped text=$REPLY_TEXT"
    toast "⏰ 予約入力を破棄: 送り先 pane が消えた ($REPLY_TEXT)"
  fi
  exit 0
}

# toast は装飾 (失敗しても送信結果には影響させない)。tmux-toast は「tmux 内か」を $TMUX の有無で
# 判定するが、run-shell -b の子はサーバ環境を継承するため $TMUX が無いことがある (socket を
# job に残せなかった場合)。判定を通すためだけに非空をセットする
toast() {
  [[ -x "$TOAST" ]] || return 0
  TMUX="${TMUX:-run-shell}" "$TOAST" -d 4 "$@" >/dev/null 2>&1 || true
}

cmd_wizard() {
  local n choice
  prune_stale
  n=$(find "$STATE_DIR" -name '*.job' 2>/dev/null | wc -l | tr -d ' ')
  choice="$(printf '%s\n' "新規予約" "予約一覧・取消 ($n 件)" | gum choose --header "⏰ 予約入力" --height 4)" || return 0
  case "$choice" in
    "新規予約") cmd_new ;;
    "予約一覧"*) cmd_list ;;
  esac
}

case "${1:-wizard}" in
  wizard) cmd_wizard ;;
  new)    cmd_new ;;
  list)   cmd_list ;;
  fire)   [[ -n "${2:-}" ]] || exit 0; cmd_fire "$2" ;;
  *) echo "usage: $0 [wizard|new|list|fire <id>]" >&2; exit 1 ;;
esac
