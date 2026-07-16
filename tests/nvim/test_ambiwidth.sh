#!/usr/bin/env zsh

set -euo pipefail
unset CDPATH

NVIM_BIN=${NVIM:-nvim}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
AW_DIR="$ROOT_DIR/vendor/nvim-plugins/ambiwidth.nvim"
# false-pass 防御 (pcall+cquit guard / log backstop) は lib に集約 (rationale は lib 側コメント参照)。
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

check="$tmp_root/check_ambiwidth.lua"
# vendored ambiwidth.nvim (旧 vim-ambiwidth の Lua 移植) の回帰テスト。setup() の設定分岐と pcall ガードを固定する。
# データ (95 レンジの中身) はテストせず、設定による件数の関係と失敗時の挙動だけを検証する。
# 依存を避けるため -u NONE で vendored module を直接 require する。
#
# +luafile のエラー握り潰し (exit 0 化) 対策は lib/guard.lua の pcall+cquit に集約。
# require も guard の内側に置く (外だと throw が exit 0 に化けて空振り PASS になる)。
cat >"$check" <<EOF
local guard = dofile([[$SCRIPT_DIR/lib/guard.lua]])
guard("ambiwidth test failed", function()
  vim.opt.rtp:append("$AW_DIR")
  local aw = require("ambiwidth")

  local BASE = 32 -- 常時適用の base レンジ数 (上流生成物のスナップショット)
  local CICA = 63 -- Cica/Nerd Font PUA レンジ数
  local DEFAULT = BASE + CICA -- 未設定 = cica on

  local function apply(cica, add)
    vim.g.ambiwidth_cica_enabled = cica
    vim.g.ambiwidth_add_list = add
    vim.fn.setcellwidths({})
    aw.setup()
    return #vim.fn.getcellwidths()
  end

  -- 1) 既定 (未設定 = cica on): base+cica、かつ ambiwidth=single
  local n = apply(nil, nil)
  assert(vim.o.ambiwidth == "single", "ambiwidth must be 'single', got " .. vim.o.ambiwidth)
  assert(n == DEFAULT, string.format("default expected %d ranges, got %d", DEFAULT, n))

  -- 2) cica off: false でも 0 でも base のみ (原版 falsy 踏襲)
  for _, off in ipairs({ false, 0 }) do
    local m = apply(off, nil)
    assert(m == BASE, string.format("cica off (%s) expected %d, got %d", tostring(off), BASE, m))
  end

  -- 3) cica 明示 on (true/1) は既定と同じ
  for _, on in ipairs({ true, 1 }) do
    local m = apply(on, nil)
    assert(m == DEFAULT, string.format("cica on (%s) expected %d, got %d", tostring(on), DEFAULT, m))
  end

  -- 4) add_list は指定した本数だけ増える
  local a = apply(nil, { { 0x1234, 0x1234, 2 }, { 0x1236, 0x1236, 2 } })
  assert(a == DEFAULT + 2, string.format("add_list expected %d, got %d", DEFAULT + 2, a))

  -- 5) 不正 add_list (width=3 は無効) は throw せず WARN 通知し、既定 (base+cica) へ
  --    フォールバックして生かす (all-or-nothing で全レンジ不適用にしない)
  apply(nil, nil) -- 正常適用しておく
  local defaults_count = #vim.fn.getcellwidths()
  local notified = false
  local orig = vim.notify
  vim.notify = function(msg, lvl)
    if lvl == vim.log.levels.WARN and tostring(msg):match("add_list") then
      notified = true
    end
  end
  vim.g.ambiwidth_add_list = { { 0x3000, 0x3000, 3 } }
  local guarded = pcall(aw.setup)
  vim.notify = orig
  assert(guarded, "setup must not throw on invalid add_list")
  assert(notified, "setup must warn on invalid add_list")
  assert(#vim.fn.getcellwidths() == defaults_count, "invalid add_list must fall back to defaults, not wipe them")

  -- 6) 既定レンジと重複する add_list (E1113 相当) でも既定は生きる
  -- (WARN 通知はここでも出るため抑止する。素通しすると headless の stderr に乗り
  --  スクリプト側のエラー grep に誤検知される)
  vim.g.ambiwidth_add_list = { { 0xfe566, 0xfe568, 2 } } -- 既定 cica に含まれる重複
  vim.notify = function() end
  aw.setup()
  vim.notify = orig
  assert(#vim.fn.getcellwidths() == defaults_count, "overlapping add_list must fall back to defaults")
  assert(vim.fn.strdisplaywidth("℃") == 2, "defaults must stay effective after overlapping add_list")
end)
EOF

log="$tmp_root/check_ambiwidth.log"
print "[test-nvim:zsh] ambiwidth (Lua port) setup()"
if ! "$NVIM_BIN" --headless -u NONE "+luafile $check" +qall >"$log" 2>&1; then
  cat "$log" >&2
  exit 1
fi
# cquit を確実に踏むための backstop (握り潰し経路が残っても stderr のエラーで検出する)。
tt_nvim_log_backstop "$log" "ambiwidth check"
print "[test-nvim:zsh] ambiwidth ok"
