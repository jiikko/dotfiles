#!/usr/bin/env zsh

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
cleanup() {
  rm -rf "$tmp_root"
}
trap cleanup EXIT

test_log="$tmp_root/nvim-test.log"
print "[test-nvim:zsh] verifying config loads headlessly"
"$NVIM_BIN" --headless -u "$CONFIG_FILE" "+lua vim.cmd('qa')" >"$test_log" 2>&1 || {
  cat "$test_log" >&2
  exit 1
}
# nvim は startup error があっても +qall で exit 0 を返すため、exit code だけでは不十分。
# stderr にエラーが出ていないかを併せて検査する (E123: 形式 / lua chunk error / traceback)。
if grep -qE 'E[0-9]{2,}:|Error detected while processing|stack traceback' "$test_log"; then
  print -u2 "[test-nvim:zsh] config load produced errors:"
  cat "$test_log" >&2
  exit 1
fi

lazy_check="$tmp_root/lazy_check.lua"
# +qall は lua の error を握り潰し exit 0 にするため cquit で非0終了させる。require だけでなく
# lazy.stats() の throw / count 欠落 (nil) も握り潰さないよう、検査全体を pcall で捕捉する。
cat <<'EOF' > "$lazy_check"
local ok, err = pcall(function()
  local lazy = require('lazy')
  local stats = lazy.stats()
  assert(stats and type(stats.count) == 'number' and stats.count > 0, 'lazy.nvim returned empty/invalid stats')
end)
if not ok then
  vim.api.nvim_err_writeln('lazy check failed: ' .. tostring(err))
  vim.cmd('cquit 1')
end
EOF

lazy_log="$tmp_root/nvim-lazy.log"
print "[test-nvim:zsh] checking lazy.nvim availability"
"$NVIM_BIN" --headless -u "$CONFIG_FILE" "+lua dofile([[$lazy_check]])" +qall >"$lazy_log" 2>&1 || {
  cat "$lazy_log" >&2
  exit 1
}
# cquit を確実に踏むための backstop (config-load 検査と同様、stderr のエラーも検出する)。
if grep -qE 'E[0-9]{2,}:|Error detected while processing|stack traceback' "$lazy_log"; then
  print -u2 "[test-nvim:zsh] lazy check produced errors:"
  cat "$lazy_log" >&2
  exit 1
fi

print "[test-nvim:zsh] done"
