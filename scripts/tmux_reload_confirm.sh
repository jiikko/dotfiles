#!/usr/bin/env bash
# tmux: conf リロード (bind R) の uptime ゲート。サーバ起動から continuum の発火窓
# (@continuum-restore-max-delay) 以内のリロードは、continuum の just_started 判定が boot と
# reload を区別できず auto-restore を再発火しうる (docs/tmux-plugins.md の KNOWN LIMITATION。
# 根治の complete フラグ方式は continuum の autosave が死ぬため却下済み)。窓内のリロード
# だけ gum confirm を挟み、窓の外 (通常運用) は従来どおり即リロードする。
# 閾値は conf の @continuum-restore-max-delay を出典にする (未設定時のみ 60 へ fallback)。
#
# 呼び出し: bind R から `run-shell '... #{q:client_name}'` ($1=client)。popup 内の再帰呼び出しは
# $1=--confirm (tty を得てから gum を使うための二段構え。kill 系は bind から直接 display-popup
# するが、こちらは「窓外は popup を出さない」分岐が必要なので script 側で popup を開く)。
#
# ⚠️ set -e は使わない: fail-safe は `gum confirm && リロード` の && 短絡に依存 (kill_confirm と同型)。
set -uo pipefail

conf="$HOME/.tmux.conf"

if [ "${1:-}" = "--confirm" ]; then
  # popup 内 (tty あり)
  now=$(date +%s)
  start=$(tmux display-message -p '#{start_time}')
  gum confirm --default=false --affirmative "リロードする" --negative "やめる" \
    "サーバ起動から $(( now - ${start:-now} ))s。continuum の発火窓内のリロードは auto-restore を再発火しうる。続行する？" \
    && tmux source-file "$conf" \; display-message "Reload Config!!"
  exit 0
fi

client="${1:-}"
now=$(date +%s)
start=$(tmux display-message -p '#{start_time}')
max_delay=$(tmux show -gqv @continuum-restore-max-delay)
if [ $(( now - ${start:-0} )) -gt "${max_delay:-60}" ]; then
  exec tmux source-file "$conf" \; display-message "Reload Config!!"
fi
# shellcheck disable=SC2086 # ${client:+...} は client 空のとき引数ごと消す意図の word splitting
exec tmux display-popup -E ${client:+-c "$client"} -w 72 -h 8 -b rounded -S "fg=yellow" \
  -T " conf リロード確認 (restore 再発火リスク) " "$0 --confirm"
