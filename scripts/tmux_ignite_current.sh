#!/bin/sh
# tmux: window 切替時に current 島を「点火」させる (切替直前にそのセルが表示していた色から
# 現在地色 @cur-accent へ、256色 cube 座標の線形補間で ~0.2 秒の色相スイープ)。
# _tmux.conf の after-select-window[1] / client-session-changed hook から run-shell -b で呼ばれる。
#
# $1 = 切替直前の表示色 (hook が fire 時に #{?#{E:@busy},#{E:@fade-hot-bg},#{E:@fade-ramp-color}} を
#      window 文脈で展開して渡す = 直前まで非 current セルが放置フェードで表示していた色そのもの。
#      消灯 (bucket=max) は式が colour16 を返し、暗地からの点火として自然に振る舞う)。
# 例: 残光 hot 201 → 200→199→204→203 → 202 (紫→ピンク→サーモン→蛍光橙の色相スイープ) /
#     消灯 16 → 52→88→130→166 → 202 (暗赤から点火)。
#
# 仕組み: @ignite (一時 option) へフレームごとに色を書き、refresh-client -S で status だけ再描画する。
#   window-status-current-format は #{E:@cur-live} (= @ignite があればそれ、無ければ @cur-accent) を
#   参照するので、アニメ中だけ島の色が差し替わる。定数 @cur-accent は純粋なまま
#   (テーマ変更は従来どおり @cur-accent だけ触ればよい。docs/theme-colors.md)。
# なぜフレーム駆動か: status-interval は下限 1 秒で sub-second のフレーム源にならない。
#   イベント駆動の明示 refresh なら可能 (同思想の前例: vendor/tmux-plugins/tmux-smooth-scroll)。
#   CPU は切替の瞬間だけ ~5 fork で、常時負荷はゼロ (放置フェードと同じ哲学)。
# 意味論: 離れた window は紫の残光で冷めていき、入った window は残光の色から点火する。対の演出。
#
# 連打対策 (世代トークン): 起動ごとに @ignite-gen を自分の PID で上書きし、フレーム前に自分が
#   最新世代かを確認する。負けた古いアニメは即座に降りる (unset は勝者に任せる = 進行中の
#   アニメを消さない)。失敗時 (フレーム中の kill) は trap + 次の切替の新世代で自己回復する。
#
# TT_IGNITE_DRYRUN=1: tmux への書き込みと sleep をせず、フレーム色を 1 行ずつ print する
#   (補間算術の決定的な検証用。tests から参照される)。

start="${1:-}"

# colourN (16..231 の cube 内) → "r g b" (各 0..5)。cube 外 (グレー 232+/名前色/空) は
# 暗地 (0,0,0)=colour16 扱い (バー地はほぼ黒なので視覚的に等価な起点)。
cube_of() {
  n="${1#colour}"
  case "$n" in '' | *[!0-9]*) echo "0 0 0"; return ;; esac
  if [ "$n" -ge 16 ] && [ "$n" -le 231 ]; then
    c=$((n - 16))
    echo "$((c / 36)) $(((c % 36) / 6)) $((c % 6))"
  else
    echo "0 0 0"
  fi
}

end_colour=$(tmux show -gv @cur-accent 2>/dev/null)
# shellcheck disable=SC2046 # cube_of の出力 "r g b" を positional へ分解する意図的な word splitting
set -- $(cube_of "$start"); r0=$1 g0=$2 b0=$3
# shellcheck disable=SC2046
set -- $(cube_of "$end_colour"); r1=$1 g1=$2 b1=$3

# 開始 == 終了 (current 色の window を再選択した等) はアニメ不要
[ "$r0 $g0 $b0" = "$r1 $g1 $b1" ] && exit 0

# F 分割の線形補間 (フレームは i=1..F-1 の 4 枚。最終色は unset = @cur-accent 本体が担う)。
# 速度 = sleep 値 / 滑らかさ = F。調整ノブは docs/theme-colors.md 参照。
F=5

if [ -n "${TT_IGNITE_DRYRUN:-}" ]; then
  i=1
  while [ "$i" -lt "$F" ]; do
    r=$(((r0 * (F - i) + r1 * i + F / 2) / F))
    g=$(((g0 * (F - i) + g1 * i + F / 2) / F))
    b=$(((b0 * (F - i) + b1 * i + F / 2) / F))
    echo "colour$((16 + 36 * r + 6 * g + b))"
    i=$((i + 1))
  done
  exit 0
fi

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

i=1
while [ "$i" -lt "$F" ]; do
  [ "$(tmux show -gv @ignite-gen 2>/dev/null)" = "$gen" ] || exit 0
  r=$(((r0 * (F - i) + r1 * i + F / 2) / F))
  g=$(((g0 * (F - i) + g1 * i + F / 2) / F))
  b=$(((b0 * (F - i) + b1 * i + F / 2) / F))
  tmux set -g @ignite "colour$((16 + 36 * r + 6 * g + b))"
  tmux refresh-client -S
  sleep 0.045
  i=$((i + 1))
done
# 最終フレーム (@ignite unset = 本来の @cur-accent へ) は trap cleanup が担う
