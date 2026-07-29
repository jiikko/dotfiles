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
#   - select_window    : window 切替 (after-select-window hook が tmux-mark-seen.sh と
#                        tmux_ignite_current.sh (点火アニメ) を -b で fork する経路込み。
#                        アニメーターのフレーム refresh がサーバで直列化するため連打時は
#                        1 切替あたり数十 ms 相当になる = 意図したコスト。budget 参照)
#   - mark_seen_direct : tmux-mark-seen.sh の同期実行 1 回あたり (hook の実コスト)
#
# 出力: "metric=<name> ms=<value>" 行の列挙。CI では tests/check_bench_budgets.sh が
# tests/tmux/bench_budgets.ci の予算と突き合わせ、超過で fail する (デグレ検出ゲート)。
# 隔離方針は tests/tmux/test_tmux.sh と同一 (専用 socket / HOME ごと temp / resurrect 隔離)。

set -euo pipefail
unset CDPATH

# checker の数値検証 (^[0-9]+(\.[0-9]+)?$) は dot 小数前提。カンマ小数ロケールでは printf %.1f が
# "12,3" を出すため数値カテゴリだけ C に固定する (LC_ALL は全カテゴリを上書きするので unset が必要)
unset LC_ALL
export LC_NUMERIC=C

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
SOCKET_NAME="dotfiles-bench-$$"

# 状態隔離 (HOME/XDG/TT_DEBOUNCE を TMUX_TMPDIR 配下へ) は lib へ集約 (test_tmux/smooth_scroll と共通)。
source "$ROOT_DIR/tests/tmux/lib/isolate_env.sh"

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

# --- server_rss / server_boot_cpu: 起動直後のサーバプロセスのフットプリント -------
# RSS = boot + conf ロード + plugin (resurrect/continuum) ロード後の常駐メモリ。
# CPU = その時点までにサーバが消費した CPU 時間。壁時計 (server_boot) と別軸で
# 「速いが太る / 焼く」方向の回帰を捕まえる。単位は metric 名に持たせる (ms= は
# ハーネスのパース契約。budgets 側コメント参照)。
# Linux (/proc) は刈り取り済み子プロセス (plugin スクリプト) の CPU も含む。
# macOS は ps -o cputime の近似 (子プロセス分を含まない。CI 予算は Linux 実測基準)。
server_pid=$("${TMUX_CMD[@]}" display-message -p '#{pid}')
rss_kb=$(ps -o rss= -p "$server_pid" | tr -d ' ')
report server_rss_mb $(( rss_kb / 1024.0 ))
if [[ -r "/proc/$server_pid/stat" ]]; then
  cpu_ticks=$(awk '{print $14 + $15 + $16 + $17}' "/proc/$server_pid/stat")
  hz=$(getconf CLK_TCK 2>/dev/null || print 100)
  report server_boot_cpu_ms $(( cpu_ticks * 1000.0 / hz ))
else
  cput=$(ps -o cputime= -p "$server_pid" | tr -d ' ')   # macOS: "m:ss.cc"
  report server_boot_cpu_ms $(( ( ${cput%%:*} * 60 + ${cput#*:} ) * 1000.0 ))
fi

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

# --- split_pane / kill_pane: pane churn (after-split-window = toast + debounce、
#     pane-exited / after-kill-pane hook 込み)。toast はヘッドレスではクライアント不在の
#     無音経路 (bin/tmux-toast の契約) を通り、tmux 3.7+ ではフローティング pane を
#     作りうるため、kill 側は「消えた pane」レースを許容する (|| true)
# -l 5 の固定幅: 素の split は active pane を半減させ続け 8 回目前後で "no space" に
# なるため、1 回あたり 6 列 (5 + border) の線形消費にして 20 回を -x 200 に収める
t0=$(now_ms)
for _ in {1..20}; do
  "${TMUX_CMD[@]}" split-window -d -h -l 5 -t 'bench:^'
done
report split_pane_x20 $(( $(now_ms) - t0 ))

bench_panes=($("${TMUX_CMD[@]}" list-panes -t 'bench:^' -F '#{pane_id}'))
t0=$(now_ms)
for p in "${bench_panes[@]:1}"; do
  "${TMUX_CMD[@]}" kill-pane -t "$p" 2>/dev/null || true
done
report kill_pane_x20 $(( $(now_ms) - t0 ))

# --- select_window: after-select-window hook (mark-seen + 点火アニメ fork) 込みの切替 ---
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
