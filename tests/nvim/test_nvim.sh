#!/usr/bin/env zsh

set -euo pipefail
unset CDPATH

NVIM_BIN=${NVIM:-nvim}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONFIG_FILE="$ROOT_DIR/_nviminit.lua"
# false-pass 防御 (pcall+cquit guard / log backstop) は lib に集約 (手書きコピペの適用漏れが
# 実 false-pass を起こした f51f0b0/54dbc81 の再発防止。rationale は lib 側コメント参照)。
source "$SCRIPT_DIR/lib/check_log.sh"

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
# lazy.nvim はプラグイン init/config の失敗を xpcall で捕捉し、エラー通知を vim.schedule() で
# 次イベントループへ遅延する (lazy/core/util.lua M.try)。即 qa するとその通知が flush される前に
# プロセスが終わり exit 0 / ログ空になるため、qa の前に vim.wait でイベントループを pump して
# 遅延通知を stderr に吐かせてから終了する (下の grep が拾えるようにする)。
"$NVIM_BIN" --headless -u "$CONFIG_FILE" "+lua vim.wait(500)" "+lua vim.cmd('qa')" >"$test_log" 2>&1 || {
  cat "$test_log" >&2
  exit 1
}
# nvim は startup error があっても +qall で exit 0 を返すため、exit code だけでは不十分。
# stderr のエラー検査は backstop へ (lazy.nvim の "Failed to run `config`" 形式はこの検査固有の追加)。
tt_nvim_log_backstop "$test_log" "config load" 'Failed to run'

lazy_check="$tmp_root/lazy_check.lua"
# 検査全体 (require 含む) を lib/guard.lua の pcall+cquit で捕捉する (+qall の握り潰し対策)。
# guard のパスだけ interpolated 1 行で注入し、本体は quoted heredoc (test_ftplugins の go 検査と同型)。
print -r -- "local guard = dofile([[$SCRIPT_DIR/lib/guard.lua]])" >"$lazy_check"
cat <<'EOF' >> "$lazy_check"
guard('lazy check failed', function()
  local lazy = require('lazy')
  local stats = lazy.stats()
  assert(stats and type(stats.count) == 'number' and stats.count > 0, 'lazy.nvim returned empty/invalid stats')

  -- プラグイン init/config の失敗検出:
  -- lazy.nvim は失敗を xpcall で捕捉し vim.notify(ERROR) を vim.schedule() で遅延発行する
  -- (exit code にも stderr にも出ない)。さらに本構成では nvim-notify が vim.notify を
  -- 横取りして UI 通知にするため、headless の stderr grep では観測できない。
  -- → イベントループを pump して通知を発火させ、nvim-notify の history から ERROR を検査する。
  -- (エラーが 1 件でもあれば nvim-notify 自体はその通知でロード済みになるので、
  --  package.loaded を見るだけで「未ロード = ERROR 通知ゼロ」と判定できる)
  local function assert_no_error_notifications(phase)
    if not package.loaded['notify'] then return end
    local errors = {}
    for _, rec in ipairs(require('notify').history()) do
      if rec.level == 'ERROR' or rec.level == vim.log.levels.ERROR then
        table.insert(errors, table.concat(rec.message, ' '))
      end
    end
    assert(#errors == 0, 'ERROR notifications during ' .. phase .. ': ' .. table.concat(errors, ' / '))
  end
  vim.wait(500)
  assert_no_error_notifications('startup')

  -- 遅延プラグインの config 検査:
  -- headless では VeryLazy 等のイベントが発火せず、遅延プラグイン (bufferline / noice /
  -- toggle.nvim 等) の config は起動検査の死角になる。全プラグインを強制ロードして
  -- config エラーと未ロード残りを検査する。
  vim.cmd('Lazy! load all')
  vim.wait(500)
  assert_no_error_notifications('Lazy! load all')
  local not_loaded = {}
  for name, p in pairs(require('lazy.core.config').plugins) do
    if not p._.loaded then table.insert(not_loaded, name) end
  end
  assert(#not_loaded == 0, 'plugins failed to load: ' .. table.concat(not_loaded, ', '))
end)
EOF

lazy_log="$tmp_root/nvim-lazy.log"
print "[test-nvim:zsh] checking lazy.nvim availability"
"$NVIM_BIN" --headless -u "$CONFIG_FILE" "+lua vim.wait(500)" "+lua dofile([[$lazy_check]])" +qall >"$lazy_log" 2>&1 || {
  cat "$lazy_log" >&2
  exit 1
}
# cquit を確実に踏むための backstop (config-load 検査と同様、stderr のエラーも検出する)。
tt_nvim_log_backstop "$lazy_log" "lazy check"

print "[test-nvim:zsh] done"
