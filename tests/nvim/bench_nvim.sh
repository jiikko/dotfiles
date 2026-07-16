#!/usr/bin/env zsh
# nvim の起動時間 + 操作レイテンシ (イベントハンドラコスト) のベンチマーク。
#
# 使い方:
#   tests/nvim/bench_nvim.sh                # フル config で計測
#   BENCH_BASELINE=1 tests/nvim/bench_nvim.sh   # --clean ベースラインも並記
#   DOTFILES_NVIM_DISABLE=nvim-scrollview tests/nvim/bench_nvim.sh  # A/B (プラグイン無効化)
#
# BENCH_BASELINE はローカルの人間用 (baseline も full config と同名 metric を出すため、
# その出力を check_bench_budgets.sh に食わせない。CI は BENCH_BASELINE なしの full config のみ照合)。
#
# 出力: "metric=<name> ms=<value>" 行の列挙。CI では tests/check_bench_budgets.sh が
# tests/nvim/bench_budgets.ci の予算と突き合わせ、超過で fail する (デグレ検出ゲート)。

set -euo pipefail
unset CDPATH

NVIM_BIN=${NVIM:-nvim}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONFIG_FILE="$ROOT_DIR/_nviminit.lua"

if ! command -v "$NVIM_BIN" >/dev/null 2>&1; then
  print -u2 "Error: nvim binary not found. Install Neovim or set \$NVIM."
  exit 1
fi

tmp_root=$(mktemp -d)
cleanup() { rm -rf "$tmp_root"; }
trap cleanup EXIT

# ベンチ対象: 5000 行の lua ファイル (treesitter / LSP / インデントガイドが全部反応する)
bench_file="$tmp_root/bench_target.lua"
{
  print -r -- "local M = {}"
  for i in {1..1000}; do
    print -r -- "function M.func_$i(a, b)"
    print -r -- "  local result = a + b * $i  -- コメント $i"
    print -r -- "  if result > 100 then result = result - 100 end"
    print -r -- "  return result"
    print -r -- "end"
  done
  print -r -- "return M"
} > "$bench_file"
cp "$bench_file" "$bench_file.alt.lua"

run_bench() {
  local label="$1"; shift
  print -r -- "--- $label ---"
  # 起動時間: 5 回計測して min を metric として 1 本出す (生値は下の注記行)。
  # shared runner のノイズは片側性 (遅くなる方にしか出ない) なので min が真の速度の最良推定。
  # 生サンプルを個別に metric で出すと checker が 1 本ずつ予算照合し、単発スパイク
  # (CI 実測で 2 倍近く飛ぶ) に耐える粗い予算しか張れない。min 集約が予算を締める前提。
  local i st_log="$tmp_root/st.log" samples="" ms
  for i in 1 2 3 4 5; do
    "$@" --headless --startuptime "$st_log" "$bench_file" "+qa!" >/dev/null 2>&1
    ms=$(awk '/NVIM STARTED/{t=$1} END{print t}' "$st_log")
    # 空 ms を素通しすると "metric=startup ms=" になり checker が黙って pass する (false-pass 防止)
    [[ -n "$ms" ]] || { print -u2 "startup 計測失敗: NVIM STARTED が startuptime log に無い ($label)"; return 1; }
    samples="$samples $ms"
    rm -f "$st_log"
  done
  print -r -- "startup samples (ms):$samples"
  print -r -- "metric=startup ms=$(print -r -- "$samples" | tr ' ' '\n' | sort -n | grep -m1 .)"
  # 操作レイテンシ (bench_lib.lua)
  BENCH_FILE="$bench_file" "$@" --headless "+lua dofile([[$SCRIPT_DIR/bench_lib.lua]])" "+qa!" 2>&1 \
    | grep -E "^metric=" || {
      print -u2 "bench_lib failed under: $label"
      return 1
    }
}

run_bench "full config" "$NVIM_BIN" -u "$CONFIG_FILE"

if [[ "${BENCH_BASELINE:-0}" == "1" ]]; then
  run_bench "clean baseline (--clean)" "$NVIM_BIN" --clean
fi
