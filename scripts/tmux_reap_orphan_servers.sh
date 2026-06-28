#!/bin/sh
#
# 孤児化した tmux サーバ（listening socket ファイルが消えたのにプロセスだけ生存）を回収する。
#
# 背景 (2026-06-28 診断):
#   TMUX_TMPDIR=$(mktemp -d) で起こした tmux サーバが、その後 `rm -rf "$TMUX_TMPDIR"` で
#   socket ファイルだけ消され、プロセスは kill されずに残る（launchd に里子化, ppid=1）。
#   この孤児は `ps` に `^tmux` として残り続けるため、tmux-continuum の自動復元 Gate2
#   を恒久的に破る:
#     vendor/tmux-plugins/tmux-continuum/scripts/helpers.sh の all_tmux_processes は
#     `ps | grep '^tmux'` を socket 生存に関係なく数え、number_tmux_processes_except_current_server
#     が 1 を超えると another_tmux_server_running_on_startup が真になり、
#     continuum_restore.sh:25 が auto-restore を丸ごと skip する。
#   実害: 2026-06-11〜06-28 の 17 日間 restore が不発（tt-restore-duration.log が凍結）。
#   原因は mktemp socket 上の孤児 scratch サーバ 2 台が ^tmux 数を押し上げていたこと。
#
# 不変条件:
#   - 生存 socket を 1 つでも持つプロセス（実サーバ・接続中 client）は絶対に kill しない。
#   - lsof で見える tmux unix socket のパスが「全て存在しない」プロセスだけを孤児と判定して
#     TERM する。判定の主キーは「socket ファイルの実在」であり、argv やセッション名ではない。
#
# 呼び出し位置: tmux サーバ起動より前（= continuum の Gate2 判定より前）に実行すること。
#   zshlib/_tmux_session.zsh の _tt_impl がサーバ未起動を検知した直後に呼ぶ。
#   手動でも安全に実行できる（孤児が無ければ何もしない）。
#
# DRY_RUN=1 で「kill せず対象だけ列挙」する（検証用）。

set -u

# lsof が無い環境では socket 生存を判定できない。誤って実サーバを kill しないため何もしない。
if ! command -v lsof >/dev/null 2>&1; then
  exit 0
fi

uid=$(id -u)

# `^tmux` にマッチするプロセス（サーバも client も含む）を列挙。
# pgrep -x はプロセス名 == "tmux" の厳密一致（argv 全体ではない）。
pids=$(pgrep -U "$uid" -x tmux 2>/dev/null || true)
[ -n "$pids" ] || exit 0

reaped=0
for pid in $pids; do
  # この PID が開いている tmux unix socket のパスを取得（fd が複数ありうる）。
  # lsof -a -U で unix domain socket に限定し、tmux-<uid>/ を含む NAME(最終列) だけ拾う。
  socks=$(lsof -p "$pid" -a -U 2>/dev/null | awk -v u="$uid" '$0 ~ ("tmux-" u "/") {print $NF}')
  # tmux socket を 1 つも開いていない → サーバでも接続中 client でもない（起動途中等）。触らない。
  [ -n "$socks" ] || continue
  alive=0
  for s in $socks; do
    [ -S "$s" ] && { alive=1; break; }
  done
  # 生存 socket を 1 つでも持つ = 実サーバ or 接続中 client。絶対に保護する。
  [ "$alive" -eq 1 ] && continue
  # ここに来た = 開いている tmux socket が全て消滅 = 孤児サーバ。回収する。
  if [ "${DRY_RUN:-0}" = "1" ]; then
    socks_oneline=$(printf '%s' "$socks" | tr '\n' ' ')
    printf 'would reap orphan tmux pid=%s socket(s)=%s\n' "$pid" "$socks_oneline"
  else
    kill -TERM "$pid" 2>/dev/null || true
  fi
  reaped=$((reaped + 1))
done

# 観測ログに残す（_tmux.conf Fix C / tmux_resurrect_save.sh Fix B と同じファイル）。
if [ "$reaped" -gt 0 ] && [ "${DRY_RUN:-0}" != "1" ]; then
  { mkdir -p "$HOME/.cache" && printf '%s\treaped-orphan-servers n=%s\n' \
      "$(date +%FT%T)" "$reaped" >> "$HOME/.cache/tt-restore-trigger.log"; } 2>/dev/null || true
fi

exit 0
