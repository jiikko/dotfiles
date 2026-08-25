#!/usr/bin/env bash
# conf source ごとに「復元ゲートの入力」を観測ログへ記録する。
#
# 記録する値:
#   tmux_procs=N  ^tmux にマッチするプロセス数。かつて continuum の Gate2 (他サーバ検知) の
#                 入力そのものだった値。Gate2 は default socket 判定へ置換済み (vendor パッチ)
#                 なので復元の可否には効かなくなったが、-L の残骸テストサーバが溜まっている
#                 ことの診断値として引き続き有用 (この値が大きいときは掃除の合図)。
#   last=...      その時点の last symlink の実体。復元不発の後追いに使う。
#
# なぜスクリプトに切り出したか (2026-07-30 の監査指摘):
#   以前は _tmux.conf に run-shell のインラインで書いており、共有の観測ログへ書く 6 経路のうち
#   ここだけが default socket ゲートを通っていなかった。-L の隔離テストサーバが実 _tmux.conf を
#   source すると本番の観測ログを汚す (ログ量の 73% を占める最大の書き手でもあった)。
#   scripts/CLAUDE.md の不変条件「共有の観測ログに書くスクリプトは tt_on_default_server を通す」に揃える。
#
# 無音契約: run-shell -b 経由なので縮退時は無音で exit 0 (scripts/CLAUDE.md 参照)。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"

tt_on_default_server || exit 0

# ⚠️ pgrep へ置き換えないこと (SC2009 を意図的に抑制): この値は「かつて continuum の Gate2 が
# 数えていた ps ベースのカウント」を再現する診断値で、判定式を変えると過去ログとの比較が壊れる。
# vendor/tmux-plugins/tmux-continuum/scripts/helpers.sh の all_tmux_processes と同じ数え方に揃える。
# shellcheck disable=SC2009
procs="$(ps -u "$(id -u)" -o command= 2>/dev/null | grep '^tmux' | grep -cv '^tmux source')" || procs=0
last="$(readlink "$(tt_resurrect_dir)/last" 2>/dev/null || true)"

tt_trigger_log "$(printf 'conf-source pid=%s tmux_procs=%s last=%s epoch=%s' \
  "$(tmux display-message -p '#{pid}' 2>/dev/null)" "$procs" "$last" "$(date +%s)")"

exit 0
