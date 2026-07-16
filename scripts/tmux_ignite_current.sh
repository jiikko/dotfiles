#!/bin/sh
# tmux: window 切替時に current 島を「点火」させる (切替直前の表示色 → 暗く沈む → 蛍光へ点火、
# の V 字輝度エンベロープで ~0.25 秒)。
# _tmux.conf の after-select-window[1] / client-session-changed hook から run-shell -b で呼ばれる。
#
# $1 = 切替直前の表示色 (hook が fire 時に #{?#{E:@busy},#{E:@fade-hot-bg},#{E:@fade-ramp-color}} を
#      window 文脈で展開して渡す = 直前まで非 current セルが放置フェードで表示していた色そのもの。
#      消灯 (bucket=max) は式が colour16 を返す)。
#
# なぜ V 字 (一度暗くする) か: 人の注意は色相差より輝度差に強く反応する。前の色→現在地色の
#   直行スイープ (v1 補間) は全フレーム高輝度のまま色相だけ変わり「分かりにくい」実感だった
#   (ユーザーフィードバック 2026-07-16) ため、前の色を数フレームで暗く沈めてから固定の点火
#   ランプ (暗→蛍光) で立ち上げる。既に暗い起点 (消灯 16 / 残光の尾) は沈む工程を自動スキップ
#   して初版の点火ランプと同じ挙動に縮退する。
# 例: 残光 hot 201 → 164→127→90→53 (紫が沈む) → 52→88→130→166 (点火) → 202 /
#     消灯 16 → 52→88→130→166 → 202。
#
# 仕組み: @ignite (一時 option) へフレームごとに色を書き、refresh-client -S で status だけ再描画する。
#   window-status-current-format は #{E:@cur-live} (= @ignite があればそれ、無ければ @cur-accent) を
#   参照するので、アニメ中だけ島の色が差し替わる。定数 @cur-accent は純粋なまま
#   (テーマ変更は従来どおり @cur-accent だけ触ればよい。docs/theme-colors.md)。
# なぜフレーム駆動か: status-interval は下限 1 秒で sub-second のフレーム源にならない。
#   イベント駆動の明示 refresh なら可能 (同思想の前例: vendor/tmux-plugins/tmux-smooth-scroll)。
#   CPU は切替の瞬間だけ数 fork で、常時負荷はゼロ (放置フェードと同じ哲学)。
# 連打対策 (世代トークン): 起動ごとに @ignite-gen を自分の PID で上書きし、各フレームは
#   「世代一致なら実行」を tmux if -F でサーバ側判定する。負けた古いアニメは残りが no-op になる。
#   失敗時 (フレーム中の kill) は trap + 次の切替の新世代で自己回復する。
#
# TT_IGNITE_DRYRUN=1: tmux への書き込みと sleep をせず、フレーム色を 1 行ずつ print する
#   (経路算術の決定的な検証用)。

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

# --- 経路の構築: 沈む D フレーム (前の色→暗) + 点火 A フレーム (暗→@cur-accent) ---
# 速度 = 下の sleep 値 / 形 = D・A。調整ノブは docs/theme-colors.md 参照。
# D=A=4 は 256色 cube の量子化上限 (これ以上増やしても中間色が存在せず dedup で消える)。
# 滑らかさはフレーム数と等間隔ペーシングで稼ぐ (hot 起点で最大 8 フレーム × 35ms ≈ 0.28 秒)。
D=4
A=4
frames=""
last=""
add() { [ "$1" = "$last" ] && return 0; frames="$frames $1"; last="$1"; }

# 沈む工程: 起点が既に暗い (座標和<=2: 消灯16 / 52 / 53 等) ならスキップして点火だけにする
if [ $((r0 + g0 + b0)) -gt 2 ]; then
  j=1
  while [ "$j" -le "$D" ]; do
    r=$(((r0 * (D + 1 - j) + (D + 1) / 2) / (D + 1)))
    g=$(((g0 * (D + 1 - j) + (D + 1) / 2) / (D + 1)))
    b=$(((b0 * (D + 1 - j) + (D + 1) / 2) / (D + 1)))
    add "colour$((16 + 36 * r + 6 * g + b))"
    j=$((j + 1))
  done
fi
# 点火工程: 暗 (0,0,0) → @cur-accent の途中点
i=1
while [ "$i" -le "$A" ]; do
  r=$(((r1 * i + (A + 1) / 2) / (A + 1)))
  g=$(((g1 * i + (A + 1) / 2) / (A + 1)))
  b=$(((b1 * i + (A + 1) / 2) / (A + 1)))
  add "colour$((16 + 36 * r + 6 * g + b))"
  i=$((i + 1))
done

if [ -n "${TT_IGNITE_DRYRUN:-}" ]; then
  for c in $frames; do echo "$c"; done
  exit 0
fi

gen=$$
tmux set -g @ignite-gen "$gen" 2>/dev/null || exit 0

# 後始末も if -F でサーバ側に条件判定させる (自分が最新世代のときだけ unset = 勝者のアニメを消さない)
cleanup() {
  tmux if -F "#{==:#{@ignite-gen},$gen}" 'set -gu @ignite ; refresh-client -S' 2>/dev/null
}
trap cleanup EXIT INT TERM HUP

# フレーム = 「世代一致なら set+refresh」を tmux if -F でサーバ側原子実行 (1 フレーム 1 fork)。
# 旧実装の show(世代確認)+set+refresh の 3 fork/フレームから削減 = フレームを倍にしても
# 総 fork は減る。preempt された古いアニメは残りフレームが no-op になるだけで無害
# (最大 ~0.3 秒 sleep して trap の条件付き後始末で終わる)。
for c in $frames; do
  tmux if -F "#{==:#{@ignite-gen},$gen}" "set -g @ignite $c ; refresh-client -S" 2>/dev/null
  sleep 0.035
done
# 最終フレーム (@ignite unset = 本来の @cur-accent へ) は trap cleanup が担う
