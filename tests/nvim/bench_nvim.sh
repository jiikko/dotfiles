#!/usr/bin/env zsh
# nvim の起動時間 + 操作レイテンシ (イベントハンドラコスト) のベンチマーク。
#
# 使い方:
#   tests/nvim/bench_nvim.sh                # フル config で計測
#   BENCH_BASELINE=1 tests/nvim/bench_nvim.sh   # --clean ベースラインも並記
#   DOTFILES_NVIM_DISABLE=nvim-scrollview tests/nvim/bench_nvim.sh  # A/B (プラグイン無効化)
#
# 出力: "metric=<name> ms=<value>" 行の列挙。CI では report-only で使う
# (絶対値は環境依存のため閾値 fail はさせない。経時トレンドと A/B 比較用)。

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
  # 起動時間 (3 回の中央値ではなく単純に 3 回出す。パースは呼び出し側の裁量)
  local i st_log="$tmp_root/st.log"
  for i in 1 2 3; do
    "$@" --headless --startuptime "$st_log" "$bench_file" "+qa!" >/dev/null 2>&1
    print -r -- "metric=startup ms=$(awk '/NVIM STARTED/{t=$1} END{print t}' "$st_log")"
    rm -f "$st_log"
  done
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
