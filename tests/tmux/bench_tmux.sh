#!/usr/bin/env zsh
# tmux 設定のパフォーマンスベンチマーク。
#
# 測るもの (この構成でボトルネックになりがちな箇所):
#   - server_boot      : サーバ起動 + _tmux.conf ロード (resurrect/continuum plugin 込み)
#   - conf_reload      : source-file の再ロード (設定を育てたときの肥大検知)
#   - status_render    : window-status-format + status-right の format 展開
#                        (status-interval=1 で毎秒 × window 数だけ走る C 側コスト。
#                         放置フェード等の式が重くなっていないかの回帰検知)
#   - new_window / kill_window : window churn (window-linked/unlinked hook が
#                        debounced_save を -b で fork する経路込みの体感コスト)
#   - select_window    : window 切替 (after-select-window hook が tmux-mark-seen.sh を
#                        -b で fork する経路込み)
#   - mark_seen_direct : tmux-mark-seen.sh の同期実行 1 回あたり (hook の実コスト)
#
# 出力: "metric=<name> ms=<value>" 行の列挙。CI では report-only (閾値 fail なし。
# shared runner のノイズが大きいため。経時トレンドと A/B 比較用)。
# 隔離方針は tests/tmux/test_tmux.sh と同一 (専用 socket / HOME ごと temp / resurrect 隔離)。

set -euo pipefail
unset CDPATH

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
SOCKET_NAME="dotfiles-bench-$$"

export HOME="$TMUX_TMPDIR/home"
export DOTFILES_DIR="$ROOT_DIR"
export XDG_DATA_HOME="$HOME/.local/share"
export TT_DEBOUNCE_STATE_DIR="$HOME/.cache/tt-debounce"
mkdir -p "$HOME" "$XDG_DATA_HOME" "$TT_DEBOUNCE_STATE_DIR"

if ! command -v "$TMUX_BIN_PATH" >/dev/null 2>&1; then
  print -u2 "Error: tmux binary not found. Install tmux or set \$TMUX_BIN."
  exit 1
fi

TMUX_CMD=("$TMUX_BIN_PATH" -L "$SOCKET_NAME" -f "$CONF_FILE")

cleanup() {
  "${TMUX_CMD[@]}" kill-server 2>/dev/null || true
  rm -rf "$TMUX_TMPDIR"
}
trap cleanup EXIT

# ミリ秒タイムスタンプ (zsh/datetime の EPOCHREALTIME、fork なし)
zmodload zsh/datetime
now_ms() { print -r -- $(( EPOCHREALTIME * 1000 )) }

report() { printf 'metric=%s ms=%.1f\n' "$1" "$2" }

# --- server_boot: サーバ起動 + conf ロード完了まで -----------------------------
t0=$(now_ms)
"${TMUX_CMD[@]}" new-session -d -s bench -x 200 -y 50
"${TMUX_CMD[@]}" display-message -p '#{session_name}' > /dev/null
report server_boot $(( $(now_ms) - t0 ))

# --- conf_reload: 設定の再 source ----------------------------------------------
t0=$(now_ms)
"${TMUX_CMD[@]}" source-file "$CONF_FILE"
report conf_reload $(( $(now_ms) - t0 ))

# --- tmux_rtt: クライアント 1 往復のベースライン --------------------------------
# 以降の display -p 系メトリクスは「クライアント fork + サーバ往復」を含む。
# format 自体のコストは (status_render - tmux_rtt) で読む。
t0=$(now_ms)
for _ in {1..100}; do
  "${TMUX_CMD[@]}" display-message -p 'x' > /dev/null
done
report tmux_rtt_x100 $(( $(now_ms) - t0 ))

# --- status_render: 実際の format 文字列を display -p で 200 回展開 --------------
# status-interval=1 のため、この式は毎秒 × (window 数 + status-right) 回サーバ内で
# 評価される。式が fork (#()) を持ち込む・肥大するとここが跳ねる。
wsf="$("${TMUX_CMD[@]}" show-options -gv window-status-format)"
srt="$("${TMUX_CMD[@]}" show-options -gv status-right)"
t0=$(now_ms)
for _ in {1..100}; do
  "${TMUX_CMD[@]}" display-message -p "$wsf" > /dev/null
  "${TMUX_CMD[@]}" display-message -p "$srt" > /dev/null
done
report status_render_x200 $(( $(now_ms) - t0 ))

# --- new_window / kill_window: hook (debounced_save fork) 込みの churn ----------
t0=$(now_ms)
for i in {1..20}; do
  "${TMUX_CMD[@]}" new-window -d -t bench -n "w$i"
done
report new_window_x20 $(( $(now_ms) - t0 ))

t0=$(now_ms)
for i in {1..20}; do
  "${TMUX_CMD[@]}" kill-window -t "bench:w$i"
done
report kill_window_x20 $(( $(now_ms) - t0 ))

# --- select_window: after-select-window hook (mark-seen fork) 込みの切替 --------
"${TMUX_CMD[@]}" new-window -d -t bench -n alt
t0=$(now_ms)
for _ in {1..50}; do
  "${TMUX_CMD[@]}" select-window -t bench:alt
  "${TMUX_CMD[@]}" select-window -t 'bench:^'
done
report select_window_x100 $(( $(now_ms) - t0 ))

# --- mark_seen_direct: window 切替 hook の実体を同期実行 -------------------------
# hook は -b (async) なので体感は fork コストだが、スクリプト自体が重くなると
# バックグラウンドの tmux コマンド渋滞として跳ね返る。1 回あたりの実コストを測る。
t0=$(now_ms)
for _ in {1..20}; do
  TMUX= "$ROOT_DIR/_claude/hooks/tmux-mark-seen.sh" > /dev/null 2>&1 || true
done
report mark_seen_direct_x20 $(( $(now_ms) - t0 ))

print -r -- "bench done"
