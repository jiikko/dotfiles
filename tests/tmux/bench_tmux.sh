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
#   - agent_panel_tick_forks / mark_seen_tmux_calls / agent_panel_tick_scale : プロセス数と、
#                        形に依存しない裏付けとしての「行数 10 倍で時間が何倍になるか」の予算
#                        (常駐 panel の 1 tick / window 切替 hook 1 回。表示行数・pane 数に
#                         比例した fork の再発を捕まえる。issue 083。単位は metric 名が持つ)
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

# --- agent_panel_tick_forks: パネル描画 1 tick が作る子プロセス数 -------------------
# 時間ではなく**プロセス数**を測る (issue 083)。この panel は 2 秒ごとに常駐で回るため、
# 表示行数に比例した fork を持つと「tmux-continuum の status interpolation を 5〜10 fork/秒
# だからと捨てた」この repo の基準に自分が抵触する。時間だけ見ていると気づけない
# (1 tick 数十 ms は体感に出ない) ので、予算はプロセス数で持つ。
#
# 測り方: tmux をスタブに差し替え、draw_once を**同じプロセスで** source して呼び、SIGCHLD を
# 数える。⚠️ 数えるのは直接の子プロセスだけ (サブシェルの中で exec された孫は数に入らない)
# ので、この値は下限。行数に比例する fork はすべて直接の子なので回帰検知には十分。
# 行数は 10 固定 (実運用のパネル高さ 14 に収まる典型)。
panel_stub_dir="$TMUX_TMPDIR/panel-stub"
mkdir -p "$panel_stub_dir"
cat > "$panel_stub_dir/tmux" <<'STUBEOF'
#!/bin/sh
# 決定論的な tmux スタブ: エージェント行を PANEL_STUB_ROWS 行返す (実サーバに触らない)
case "$*" in
  *"list-panes -a -F"*)
    i=1
    while [ "$i" -le "${PANEL_STUB_ROWS:-10}" ]; do
      # 1 行目だけ cur=1 (今表示中の pane) にする: 強調行は別の printf 経路を通るので、
      # 全行 cur=0 だとその分岐が fork 予算の被覆から外れる (red team 指摘 2026-08-21)
      cur=0; [ "$i" -eq 1 ] && cur=1
      printf '🔔 input\tsess:%d.0\tagent-%d\t1787280000\t%s\n' "$i" "$i" "$cur"
      i=$((i + 1))
    done
    ;;
  *"display-message -p"*) echo "${PANEL_STUB_HEIGHT:-14} 0" ;;
  *) : ;;
esac
exit 0
STUBEOF
chmod +x "$panel_stub_dir/tmux"
cat > "$TMUX_TMPDIR/panel-forks.sh" <<'COUNTEOF'
#!/usr/bin/env bash
set -uo pipefail
export TMUX=fake TMUX_PANE=%0
PATH="$1:$PATH"; export PATH
# shellcheck disable=SC1090
source "$2"          # dispatch はしない (source 時は関数定義だけ。panel 側の main のガード)
PANEL_BG_CURRENT=colour233
n=0
trap 'n=$((n+1))' SIGCHLD
draw_once >/dev/null 2>&1
wait
trap - SIGCHLD
printf '%s\n' "$n"
COUNTEOF
cat > "$TMUX_TMPDIR/panel-tick-ms.sh" <<'TIMEEOF'
#!/usr/bin/env bash
# 1 tick の所要時間 (μs) を min-of-3 で返す。初回は捨てる (初期化を測らない)。
set -uo pipefail
export TMUX=fake TMUX_PANE=%0
PATH="$1:$PATH"; export PATH
# shellcheck disable=SC1090
source "$2"
PANEL_BG_CURRENT=colour233
draw_once >/dev/null 2>&1
best=0
for _ in 1 2 3; do
  s=${EPOCHREALTIME/./}
  draw_once >/dev/null 2>&1
  e=${EPOCHREALTIME/./}
  d=$(( e - s ))
  if [ "$best" -eq 0 ] || [ "$d" -lt "$best" ]; then best=$d; fi
done
printf '%s\n' "$best"
TIMEEOF
panel_forks=$(PANEL_STUB_ROWS=10 bash "$TMUX_TMPDIR/panel-forks.sh" "$panel_stub_dir" "$ROOT_DIR/scripts/tmux_agent_panel.sh")
case "$panel_forks" in
  # ⚠️ 計測失敗を「0 = 予算内」に見せない。予算より大きい番兵を出して loud に落とす
  ''|*[!0-9]*) panel_forks=999 ;;
esac
report agent_panel_tick_forks_rows10 "$panel_forks"

# ⚠️ fork 数だけでは足りない: SIGCHLD は**直接の子**しか数えないので、行ごとの fork を
# パイプ (右辺がサブシェル) の中に置かれると数に出ない (red team 実測 2026-08-21: 行ループを
# パイプへ移して行ごとに外部コマンドを呼ぶ変異で、fork 数は 4 のまま = 素通りした)。
#
# そこで**行数を 10 倍にしたときの時間の伸び率**も予算化する。tick のコストが行数に比例し
# 始めれば、fork がどんな形で隠れていても伸び率に出る。絶対時間でなく比にするのは runner の
# 速度差・混雑を打ち消すため (どちらの測定にも同じ倍率で乗る)。
# 実測 (ローカル、min-of-3): 現行 1.5〜2.5 倍 / 行ごとの $( ) を復活させると 6.5 倍 /
# パイプに隠した変異で 7.6 倍。高さ 120 = 100 行すべてを描画させる (既定 14 だと 12 行で
# 打ち切られて行数比例が見えない)。
panel_us10=$(PANEL_STUB_ROWS=10 PANEL_STUB_HEIGHT=120 bash "$TMUX_TMPDIR/panel-tick-ms.sh" "$panel_stub_dir" "$ROOT_DIR/scripts/tmux_agent_panel.sh")
panel_us100=$(PANEL_STUB_ROWS=100 PANEL_STUB_HEIGHT=120 bash "$TMUX_TMPDIR/panel-tick-ms.sh" "$panel_stub_dir" "$ROOT_DIR/scripts/tmux_agent_panel.sh")
case "$panel_us10$panel_us100" in
  ''|*[!0-9]*) panel_scale=99999 ;;   # 計測失敗は番兵で落とす (0 = 予算内に見せない)
  *) if [ "$panel_us10" -gt 0 ]; then panel_scale=$(( panel_us100 * 100 / panel_us10 )); else panel_scale=99999; fi ;;
esac
report agent_panel_tick_scale_x100 "$panel_scale"

# --- mark_seen_tmux_calls: window 切替 hook が起動する tmux クライアント数 ------------
# 同じ思想でプロセス数を予算化する。pane ごとに tmux を起動していた頃は pane 数 + 1 回で、
# window 切替のたびに pane 数ぶんの fork が乗っていた (issue 083)。
# 期待は「list-panes 1 回 + まとめた if-shell 1 回」= 2 回 (pane 数に依存しない)。
ms_stub_dir="$TMUX_TMPDIR/ms-stub"
mkdir -p "$ms_stub_dir"
ms_calls_file="$TMUX_TMPDIR/ms-calls"
: > "$ms_calls_file"
# ⚠️ shim は tmux を**絶対パス**で呼ぶこと。$TMUX_BIN_PATH は既定が素の "tmux" なので、
# PATH 先頭に置いた shim 自身へ解決し直して無限再帰する (実測 2026-08-21: -L が延々と
# 積み重なった /bin/sh が CPU を焼き続けた)。
ms_real_tmux=$(command -v "$TMUX_BIN_PATH")
case "$ms_real_tmux" in
  "$ms_stub_dir"/*|'') print -u2 "Error: tmux の絶対パスを解決できない ($ms_real_tmux)"; exit 1 ;;
esac
cat > "$ms_stub_dir/tmux" <<EOS
#!/bin/sh
echo x >> "$ms_calls_file"
exec "$ms_real_tmux" -L "$SOCKET_NAME" "\$@"
EOS
chmod +x "$ms_stub_dir/tmux"
ms_win=$("${TMUX_CMD[@]}" new-window -d -t bench -P -F '#{window_id}')
for _ in {1..3}; do "${TMUX_CMD[@]}" split-window -d -t "$ms_win" -l 3; done
# ⚠️ $TMUX/$TMUX_PANE を消して呼ぶ (隣の mark_seen_direct_x20 と対称)。socket は shim が
# -L で明示するので漏れないことを確認済みだが、ambient な $TMUX を残す形をここに置かない。
TMUX= TMUX_PANE= PATH="$ms_stub_dir:$PATH" "$ROOT_DIR/_claude/hooks/tmux-mark-seen.sh" "$ms_win" >/dev/null 2>&1 || true
ms_calls=$(wc -l < "$ms_calls_file" | tr -d ' ')
case "$ms_calls" in ''|*[!0-9]*) ms_calls=999 ;; esac   # 計測失敗は番兵で落とす (上と同じ規律)
report mark_seen_tmux_calls "$ms_calls"

print -r -- "bench done"
