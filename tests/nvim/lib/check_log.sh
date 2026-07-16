# shellcheck shell=bash
# tests/nvim/lib/check_log.sh — headless nvim の log に stderr エラーが残っていないか
# 検査する backstop (source して使う。zsh/bash 両対応)。
#
# なぜ必要か: nvim は startup/ftplugin/autocmd 内のエラーを stderr に出しても +qall で
# exit 0 を返す。lib/guard.lua の cquit 経路は「check 本体の Lua エラー」しか捕まえられず、
# check の外 (config ロード・ftplugin 評価) で出たエラーはこの grep だけが検知できる。
# かつてこの grep が各テストへ手書きコピペされ、貼り忘れが実 false-pass を起こした
# (54dbc81 が test_nvim.sh の lazy check への貼り忘れを閉鎖) ため一元化した。
#
#   tt_nvim_log_backstop <log_file> <label> [extra_alternation]
#     extra_alternation: 呼び出し文脈固有の追加パターン (例: lazy.nvim の 'Failed to run')。
#     検知したら log を stderr へ出して exit 1 (呼び出し元スクリプトごと落とす)。
tt_nvim_log_backstop() {
  local log_file="$1" label="$2" extra="${3:-}"
  local pattern='E[0-9]{2,}:|Error detected while processing|stack traceback'
  if [ -n "$extra" ]; then
    pattern="$pattern|$extra"
  fi
  if grep -qE "$pattern" "$log_file"; then
    echo "[test-nvim:zsh] $label produced errors:" >&2
    cat "$log_file" >&2
    exit 1
  fi
}
