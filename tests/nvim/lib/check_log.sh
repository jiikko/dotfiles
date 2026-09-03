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

# tt_nvim_run_check <label> <check.lua のファイル名>
#   headless nvim で check スクリプトを 1 本走らせ、3 つの検査を **1 か所で**行う:
#     ① nvim 自体が異常終了していないか
#     ② FAIL: / Error executing / stack traceback — check の assert 失敗と、assert を通り抜けて
#        しまう scheduled callback 内の lua 例外 (これを見ないと OK が出て緑になる)
#     ③ **行頭の** OK マーカー (`^OK`)
#
#   🚨 ③ のアンカーを外さないこと。`grep -q "OK"` だと "not OK" のような出力でも通る。
#     実際 test_image_hover.sh だけアンカー無しに drift していた (issue 081)。この関数は
#     その drift を「1 か所しか無い」状態にするために作った。
#   🚨 `qa!` は必須。check スクリプトが無名バッファへ書き込んで modified になると、未保存拒否で
#     headless nvim が終了せず**永久にハングする** (2026-07-12 実測)。
#
#   要求する変数 (呼び出し側が設定済みであること): NVIM_BIN / SCRIPT_DIR / CONFIG_FILE
tt_nvim_run_check() {
  local label="$1" check_lua="$2" out
  : "${NVIM_BIN:?tt_nvim_run_check: NVIM_BIN が未設定}"
  : "${SCRIPT_DIR:?tt_nvim_run_check: SCRIPT_DIR が未設定}"
  : "${CONFIG_FILE:?tt_nvim_run_check: CONFIG_FILE が未設定}"
  out=$("$NVIM_BIN" --headless -u "$CONFIG_FILE" \
    "+lua vim.wait(300)" \
    "+lua dofile('$SCRIPT_DIR/$check_lua')" \
    "+lua vim.cmd('qa!')" 2>&1) || {
    printf '%s\n' "$out" >&2
    exit 1
  }
  if grep -qE "FAIL:|Error executing|stack traceback" <<< "$out"; then
    printf '%s\n' "$out" >&2
    exit 1
  fi
  if ! grep -q "^OK" <<< "$out"; then
    printf '%s\n' "[$label] expected OK marker (行頭), got:" >&2
    printf '%s\n' "$out" >&2
    exit 1
  fi
  printf '%s\n' "[$label] $out"
}
