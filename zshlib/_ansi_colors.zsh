# shellcheck shell=bash
# ------------------------------------------------------------------------------
# _ansi_colors.zsh — 端末色を ANSI へ解決した共通パレット
#
# なぜ必要か (issue 089):
#   `print -P` は「書式」と「データ」を同じ文字列で受け取るため、ファイル名などの
#   データを埋めると prompt 展開がその中の `$(...)` を**実行する** (実測: 名前に
#   `$(touch pwned)` を含むファイルを処理すると pwned が作られる)。色を 1 度だけ
#   ANSI へ解決しておけば出力は `print -r --` で済み、データが展開器を通らない。
#
# 値は zsh の prompt 展開が出すバイトと一致させている (2026-08-22 実測)。
#   %F{green}=ESC[32m %F{red}=ESC[31m %F{cyan}=ESC[36m %F{yellow}=ESC[33m
#   %F{white}=ESC[37m %B=ESC[1m %b=ESC[0m %f=ESC[39m
# `_C_NOBOLD` が ESC[0m (全属性リセット) なのは `%b` の実挙動に合わせたため。
# ずれたら tests/zshrc/test_ansi_colors.sh が落ちる (print -P の出力と突き合わせている)。
#
# 色を無効化する仕組みは持たない (従来の `print -P` も端末判定なしで常に色を出していた
# ため、挙動を変えない)。必要になったらここで一括して空文字にできる。
# ------------------------------------------------------------------------------

typeset -g _C_GREEN=$'\e[32m'
typeset -g _C_RED=$'\e[31m'
typeset -g _C_CYAN=$'\e[36m'
typeset -g _C_YELLOW=$'\e[33m'
typeset -g _C_WHITE=$'\e[37m'
typeset -g _C_BOLD=$'\e[1m'
typeset -g _C_NOBOLD=$'\e[0m'
typeset -g _C_OFF=$'\e[39m'

# 色名から引く形 (実行時に色を選ぶ箇所用。例: 成否で green/yellow を切り替える)
typeset -gA _C
# zsh の連想配列リテラルは (key value ...) で、bash の ([k]=v) ではない
# shellcheck disable=SC2190
_C=(
  green  "$_C_GREEN"
  red    "$_C_RED"
  cyan   "$_C_CYAN"
  yellow "$_C_YELLOW"
  white  "$_C_WHITE"
)
