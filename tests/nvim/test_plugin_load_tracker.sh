#!/usr/bin/env zsh
# プラグインロードトラッカーが headless セッションを数えないことのテスト。
# 検証内容と守っている不変条件は plugin_load_tracker_check.lua のヘッダ参照。

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

source "$SCRIPT_DIR/lib/check_log.sh"

# 🚨 rtp は cwd 依存 (basic_checktime_check.lua のヘッダ参照)
cd "$ROOT_DIR" || exit 1

sandbox=$(mktemp -d)
trap 'rm -rf "$sandbox"' EXIT
export XDG_STATE_HOME="$sandbox" TT_TRACKER_SANDBOX="$sandbox"
unset DOTFILES_PLUGIN_LOAD_TRACKER

print "[test-plugin-load-tracker] verifying headless sessions are not counted"
tt_nvim_run_check "test-plugin-load-tracker" "plugin_load_tracker_check.lua"
