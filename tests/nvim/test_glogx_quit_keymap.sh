#!/usr/bin/env zsh
# glogx が起こした nvim の <C-g> (nvim/lua/dotfiles/basic.lua の set_keymaps) のテスト。
# 本物の nvim を全設定で起こし、終了コードとファイルの中身で見る:
#   1. GLOGX_QUIT_CODE があれば、全部保存してからその値で抜ける (値を basic.lua に写していないことを、glogx の既定と違う値で見る)
#   2. 保存できないバッファ (名前の無い変更) があれば抜けない (cquit は未保存の変更を捨てる)
#      外から書き換えられたファイルの上書き確認を断ったときも抜けない
#      nvim の子プロセスには変数を継がない (:terminal の git の editor が 87 で抜ける・入れ子の glogx が F の ! へ渡す)
#   3. GLOGX_QUIT_CODE が無ければ <C-g> は何もしない (普段の nvim に影響しない)

set -euo pipefail
unset CDPATH
# glogx の中から走らせても、ケース 3 (変数が無いとき) を変数が無い状態で測る
unset GLOGX_QUIT_CODE

NVIM_BIN=${NVIM:-nvim}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONFIG_FILE="$ROOT_DIR/_nviminit.lua"

if ! command -v "$NVIM_BIN" >/dev/null 2>&1; then
  print -u2 "Error: nvim binary not found. Install Neovim or set \$NVIM."
  exit 1
fi

source "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR"

# 🚨 rtp は cwd 依存 (basic_checktime_check.lua のヘッダ参照)。repo root で走らせて、この checkout の nvim/lua を読ませる
cd "$ROOT_DIR" || exit 1

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() { print -u2 "FAIL: $1"; exit 1; }

# run_nvim <file> <-c の前段>…: 前段を打ってから <C-g> を押し、抜けなければ qa! で終える (抜けなかったときの rc は 0)
run_nvim() {
  local file=$1; shift
  local -a pre=()
  local c
  for c in "$@"; do pre+=(-c "$c"); done
  "$RUNTIMEOUT" 30 "$NVIM_BIN" --headless -u "$CONFIG_FILE" "${pre[@]}" \
    -c 'execute "normal \<C-g>"' -c 'qa!' -- "$file" >"$work/out" 2>&1
}

code=93

# 1. 保存してから GLOGX_QUIT_CODE で抜ける
print -r -- "old" >"$work/a.md"
rc=0; GLOGX_QUIT_CODE=$code run_nvim "$work/a.md" 'call setline(1, "new")' || rc=$?
[ "$rc" -eq "$code" ] || { cat "$work/out" >&2; fail "1: rc=$rc, want $code (<C-g> で抜けていない)"; }
[ "$(cat "$work/a.md")" = "new" ] || fail "1: 保存されていない: $(cat "$work/a.md")"

# 2. 名前の無い変更があれば抜けない
print -r -- "old" >"$work/b.md"
rc=0; GLOGX_QUIT_CODE=$code run_nvim "$work/b.md" 'call setline(1, "new")' 'enew' 'call setline(1, "scratch")' || rc=$?
[ "$rc" -eq 0 ] || { cat "$work/out" >&2; fail "2: rc=$rc, want 0 (保存できないバッファを捨てて抜けた)"; }

# 2b. 外から書き換えられたファイルの上書き確認を断ったら抜けない (:wall は例外を投げずに変更を残す)
print -r -- "old" >"$work/d.md"
rc=0; GLOGX_QUIT_CODE=$code run_nvim "$work/d.md" 'call setline(1, "new")' \
  'call writefile(["ext"], expand("%"))' 'call system("touch -t 203001010000 " . shellescape(expand("%")))' || rc=$?
[ "$rc" -eq 0 ] || { cat "$work/out" >&2; fail "2b: rc=$rc, want 0 (上書きを断ったのに変更を捨てて抜けた)"; }
[ "$(cat "$work/d.md")" = "ext" ] || fail "2b: 上書き確認を経ずに書いた: $(cat "$work/d.md")"

# 2c. 子プロセス (:terminal / :! で起こす git の editor や入れ子の glogx) に変数を継がない
rc=0; GLOGX_QUIT_CODE=$code run_nvim "$work/a.md" 'call writefile([trim(system("printenv GLOGX_QUIT_CODE || echo none"))], "'"$work"'/env")' || rc=$?
[ "$(head -1 "$work/env")" = "none" ] || fail "2c: 子プロセスに GLOGX_QUIT_CODE が継がれた: $(cat "$work/env")"

# 3. GLOGX_QUIT_CODE が無ければ何もしない
print -r -- "old" >"$work/c.md"
rc=0; run_nvim "$work/c.md" 'call setline(1, "new")' || rc=$?
[ "$rc" -eq 0 ] || { cat "$work/out" >&2; fail "3: rc=$rc, want 0 (GLOGX_QUIT_CODE が無いのに抜けた)"; }
[ "$(cat "$work/c.md")" = "old" ] || fail "3: GLOGX_QUIT_CODE が無いのに保存した"

print "[test-glogx-quit-keymap] OK (5 cases)"
