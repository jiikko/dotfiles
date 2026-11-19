#!/usr/bin/env bash
# scratch popup の開閉を観測ログ (~/.cache/tt-restore-trigger.log) に 1 行ずつ書く。
# 呼び出し元は scripts/tmux_scratch_popup.sh だけ (開く側は popup の中の shell から、閉じる側は C-t t の経路から)。
# usage: tmux_scratch_popup_log.sh <event> <client> [key=value ...]
#   event:  start (popup の中の shell が動き始めた) / end (中の attach が終わった) / close-key (popup の中で C-t t を押した)
#   client: 外側のクライアントの名前 (start / end) か、popup の中のクライアントの名前 (close-key)
#
# 🚨 呼び出し元は背景 (&) で起こす。ここは tmux に問い合わせるので (本番のサーバで約 40 ミリ秒。固まっていると返らない)、
#    待つと popup が開く・閉じるのがその分遅れる。書き手は HUP / TERM を無視して起こされるので、問い合わせの最中に
#    サーバが固まると返らずに残る。上限 (runtimeout) は付けていない: 残るのは起動から問い合わせまでの約 40ms に
#    固まりが始まったときだけで、サーバが戻るか死ねば返る。固まりの後に書き手が溜まっていたら付ける
# 🚨 行に `pid=` / `epoch=` を書かない。watchdog の死因分類はこの 2 つのキーで行を拾う (docs/tmux-plugins.md の
#    「観測ログの読み方」)。shell の pid は `shell=` で書く。
set -u
unset CDPATH
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh" || exit 0
# -L の隔離サーバも同じ conf の bind を持つ。そこで開いた popup を本番のログに書かない。
# tt_on_default_server はサーバに届かないと本番と見なすので (隔離サーバを popup ごと kill した後の end が通る)、
# 先に TMUX の socket の字面を比べる
case "${TMUX:-}" in
  "") ;;
  "$(tt_default_socket_path),"*) ;;
  *) exit 0 ;;
esac
tt_on_default_server || exit 0

event="${1:-?}"
client="${2:-}"
shift 2 2>/dev/null || shift $#

# 外側のクライアントのセッション・大きさ・端末。popup は外側の 80% x 75% で描かれるので、見えないときに大きさを疑える。
# end でクライアントが見つからなければ、外側が先に消えた (Terminal の窓を閉じた) ことが分かる。
# close-key は引かない: client は popup の中のクライアントで、書く時点では detach で消えていることが多い
geom=""
if [ "$event" != "close-key" ]; then
  geom="$(tmux list-clients -F '#{client_name}	session=#{client_session} size=#{client_width}x#{client_height} term=#{client_termname}' 2>/dev/null \
    | awk -F'\t' -v c="$client" '$1 == c { print $2; exit }')"
  [ -n "$geom" ] || geom="client-gone"
fi

# popup の中の shell の pty。list-clients の scratch のクライアント (client_tty) と突き合わせられる
tty=""
for a in "$@"; do
  case "$a" in shell=*) tty="$(ps -o tty= -p "${a#shell=}" 2>/dev/null | tr -d ' ')" ;; esac
done
case "$tty" in ''|'??') tty="" ;; *) tty="/dev/$tty" ;; esac

tt_trigger_log "scratch-popup-$event client=${client:-?}${geom:+ $geom}${tty:+ tty=$tty} $*"
