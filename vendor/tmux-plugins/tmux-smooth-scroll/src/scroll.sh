#!/usr/bin/env bash
# Plugin integration layer
# Handles tmux configuration, scroll distance calculation, and calls animator

source "$(dirname "$0")/config.sh"

DIRECTION=$1
SCROLL_TYPE=$2

# Target pane: where the binding fired (mouse pane for wheel events, active
# pane for keyboard). tmux exports TMUX_PANE for run-shell -b bindings.
TARGET_PANE="${TMUX_PANE:-}"
TARGET_ARG=()
[ -n "$TARGET_PANE" ] && TARGET_ARG=(-t "$TARGET_PANE")

# [dotfiles patch] keyboard 経由では TMUX_PANE は来ない (tmux 3.7b 実測: run-shell は
# TMUX を設定するが TMUX_PANE は設定しない。上の upstream コメントの前提は誤りで、
# wheel バインドの明示 prefix 以外では未設定)。ここで押下時の pane を一度だけ確定して
# 以降の全コマンドを -t で固定する。固定しないと各フレームが「実行時のカレント pane」に
# 暗黙解決され、アニメ途中で pane を切り替えると残フレームが移動先の pane をスクロール
# する (nvim 側 dotfiles/smooth_scroll.lua の「押下時ウィンドウ捕捉」と同じ規律)。
if [ -z "$TARGET_PANE" ]; then
    TARGET_PANE=$(tmux display-message -p '#{pane_id}')
    TARGET_ARG=(-t "$TARGET_PANE")
fi

# [dotfiles patch] pane 情報と全設定を 1 回の tmux fork でまとめて読む。config__* を
# 都度呼ぶ元実装は押下ごとに tmux client fork ×5 になり、素通し経路の固定オーバーヘッドの
# 主因だった (実測)。format の #{@option} は global (set -g) の user option も解決する
# (tmux 3.7b 実測)。⚠️ 既定値 (:-) は config.sh の同名関数と同期を保つこと。
# 区切りは \x1f (unit separator): タブ等の IFS 空白文字だと未設定オプションの空フィールドが
# 潰れて後続がシフトする (実測でハマった)。非空白の IFS は空フィールドを保存する。
US=$'\x1f'
FMT="#{pane_height}${US}#{@smooth-scroll-speed}${US}#{@smooth-scroll-normal}${US}#{@smooth-scroll-halfpage}${US}#{@smooth-scroll-fullpage}${US}#{@smooth-scroll-easing}${US}#{@smooth-scroll-repeat-ms}${US}#{@smooth-scroll-max-steps}${US}#{@smooth-scroll-exit-copy-mode-at-bottom}"
IFS="$US" read -r PANE_HEIGHT OPT_SPEED OPT_NORMAL OPT_HALF OPT_FULL OPT_EASING OPT_REPEAT_MS OPT_MAX_STEPS OPT_EXIT_BOTTOM \
    < <(tmux display-message "${TARGET_ARG[@]}" -p "$FMT")

# Calculate scroll distance based on pane height
case "$SCROLL_TYPE" in
    halfpage)
        LINES="${OPT_HALF:-$((PANE_HEIGHT / 2))}"
        ;;
    fullpage)
        LINES="${OPT_FULL:-$PANE_HEIGHT}"
        ;;
    normal)
        LINES="${OPT_NORMAL:-3}"
        ;;
    small)
        LINES=1
        ;;
    *)
        LINES="$SCROLL_TYPE"
        ;;
esac

# Base delay per line: 0-100 maps to 1000µs - 10000µs linearly
BASE_DELAY=$((1000 + ${OPT_SPEED:-100} * 90))

# ---- [dotfiles patch] 押しっぱなし/連打の素通し + 進行中アニメの世代打ち切り ----
# upstream は run-shell -b が押下ごとに animator を並行起動するため、キーリピート
# (30-80ms 間隔) でアニメが重畳する (nvim 側で neoscroll を廃した原因と同型の構造問題)。
# per-pane の状態ファイル (内容: "gen last_press_ms anim_until_ms" の 1 行) で
# nvim の dotfiles/smooth_scroll.lua と同じ核を再現する:
#   - 前回押下から repeat_ms 未満、またはアニメ進行中の再押下 → アニメせず即時ジャンプ
#   - どの経路でも gen を進める → 進行中の animator は次フレーム前に検知して自殺
# 判定と状態更新は arbiter.pl が flock 下で原子的に行う (bash での read→判定→write は
# 連打時に並行インスタンスが同じ旧状態を読み、全員アニメ・同値 gen で打ち切り不発に
# なるレースを実測したため。詳細は arbiter.pl 冒頭コメント)。
STATE_DIR="${TMPDIR:-/tmp}/tmux-smooth-scroll-$(id -u)"
mkdir -p "$STATE_DIR"
STATE_FILE="$STATE_DIR/$TARGET_PANE"
# anim_until の上限 (ms) はアニメ所要時間の概算上界 + 余裕で渡す。固定値だと遅い設定
# (speed 大・fullpage) で実アニメ中に上限が切れ、「アニメ中の再押下は素通し」の仕様が
# 破れる。上界 = LINES × BASE_DELAY(µs) / velocity_min(0.2 = ×5) / 1000 + 250ms。
# 過大側に倒すのは安全 (animator が終了時に 0 へ戻すので、上限が効くのは異常終了時のみ)
CAP_MS=$((LINES * BASE_DELAY * 5 / 1000 + 250))
read -r GEN HELD < <(perl "$SRC_DIR/arbiter.pl" "$STATE_FILE" "${OPT_REPEAT_MS:-150}" "$CAP_MS")

if [ "$HELD" = "1" ]; then
    # Passthrough: instant native-like jump (and the gen bump kills any running animation)
    tmux send-keys "${TARGET_ARG[@]}" -X -N "$LINES" "scroll-$DIRECTION"
else
    # Delegate to pure animator
    "$SRC_DIR/animate.sh" "$DIRECTION" "$LINES" "$BASE_DELAY" "${OPT_EASING:-sine}" "$TARGET_PANE" "$STATE_FILE" "$GEN" "${OPT_MAX_STEPS:-0}"
fi
# ---- [dotfiles patch] ここまで (下の bottom 判定は素通し/アニメ両経路に共通で適用) ----

# After scrolling down, exit copy mode if we've reached the bottom
if [ "$DIRECTION" = "down" ] && [ "${OPT_EXIT_BOTTOM:-true}" = "true" ]; then
    AT_BOTTOM=$(tmux display-message "${TARGET_ARG[@]}" -p '#{&&:#{pane_in_mode},#{==:#{scroll_position},0}}')
    if [ "$AT_BOTTOM" = "1" ]; then
        tmux send-keys "${TARGET_ARG[@]}" -X cancel
    fi
fi
