#!/bin/sh
# tmux: window 切替時に current 島を「点火」させる (暗赤 → 蛍光オレンジへ ~0.2 秒のランプ)。
# _tmux.conf の after-select-window[1] / client-session-changed hook から run-shell -b で呼ばれる。
#
# 仕組み: @ignite (一時 option) へフレームごとに色を書き、refresh-client -S で status だけ再描画する。
#   window-status-current-format は #{E:@cur-live} (= @ignite があればそれ、無ければ @cur-accent) を
#   参照するので、アニメ中だけ島の色が差し替わる。定数 @cur-accent は純粋なまま
#   (テーマ変更は従来どおり @cur-accent だけ触ればよい。docs/theme-colors.md)。
# なぜフレーク駆動か: status-interval は下限 1 秒で sub-second のフレーム源にならない。
#   イベント駆動の明示 refresh なら可能 (同思想の前例: vendor/tmux-plugins/tmux-smooth-scroll)。
#   CPU は切替の瞬間だけ ~5 fork で、常時負荷はゼロ (放置フェードと同じ哲学)。
# 意味論: 離れた window は紫の残光で冷めていき (放置フェード)、入った window は点火する。対の演出。
#
# 連打対策 (世代トークン): 起動ごとに @ignite-gen を自分の PID で上書きし、フレーム前に自分が
#   最新世代かを確認する。負けた古いアニメは即座に降りる (unset は勝者に任せる = 進行中の
#   アニメを消さない)。lockfile 不要で tmux option だけで完結する。
# 失敗モード: 勝者がフレーム中に kill されると @ignite が残留し島が中間色で固まる。
#   trap で自世代のときだけ unset して回収する (SIGKILL は防げないが、窓は ~0.2 秒で
#   次の window 切替が新世代として上書き・完走するため自己回復する)。

gen=$$
tmux set -g @ignite-gen "$gen" 2>/dev/null || exit 0

cleanup() {
  # 自分が最新世代のときだけ後始末する (負けた側が勝者のアニメを消さない)
  if [ "$(tmux show -gv @ignite-gen 2>/dev/null)" = "$gen" ]; then
    tmux set -gu @ignite 2>/dev/null
    tmux refresh-client -S 2>/dev/null
  fi
}
trap cleanup EXIT INT TERM HUP

# ランプ: 52(#5f0000 暗赤) → 94 → 130 → 166 → (unset = @cur-accent 202 蛍光)。
# cube の r 上昇対角 (g=1, b=0)。フレーム間隔 45ms × 4 = 体感 ~0.2 秒。
# 調整ノブ: 色列を変えれば軌跡が、sleep を変えれば速度が変わる (docs/theme-colors.md)。
for c in 52 94 130 166; do
  [ "$(tmux show -gv @ignite-gen 2>/dev/null)" = "$gen" ] || exit 0
  tmux set -g @ignite "colour$c"
  tmux refresh-client -S
  sleep 0.045
done
# 最終フレーム (@ignite unset = 本来の @cur-accent へ) は trap cleanup が担う
