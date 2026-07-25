#!/usr/bin/env bash
# glogx 最外周フレームの罫線色 候補プレビュー (実端末で実行)
# 採用したい色が決まったら src/glogx/render.go の ansiFrameBorder を差し替える。
set -u
demo() {
  local name="$1" c="$2" R=$'\033[0m' D=$'\033[2m' G=$'\033[32m'
  printf '  %s\n' "$name"
  printf '    %s╔══════════════════════════════╗%s \n' "$c" "$R"
  printf '    %s║%s %s✓%s commit 91a72bd  Fix calc  %s║%s%s█%s\n' "$c" "$R" "$G" "$R" "$c" "$R" "$D" "$R"
  printf '    %s║%s   Author: koji                %s║%s%s█%s\n' "$c" "$R" "$c" "$R" "$D" "$R"
  printf '    %s▖▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▗%s%s█%s\n' "$c" "$R" "$D" "$R"
  printf '      %s▓█████████████████████████████%s\n\n' "$D" "$R"
}
echo
echo "glogx 最外周フレームの罫線色 (落ち影 █ は中立 dim のまま)"
echo
demo "A: 現状の dim (変更前)"              $'\033[2m'
demo "B: マゼンタ 201 ← 今これを採用"      $'\033[38;5;201m'
demo "C: マゼンタ 213 (一段淡い)"          $'\033[38;5;213m'
demo "D: 蛍光オレンジ 202 (現在地の色)"    $'\033[38;5;202m'
demo "E: シアン 51 (通知の色)"             $'\033[38;5;51m'
demo "F: 緑 46 (アクティブ pane の色)"     $'\033[38;5;46m'
