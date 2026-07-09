#!/usr/bin/env zsh

set -euo pipefail
unset CDPATH

NVIM_BIN=${NVIM:-nvim}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
AW_DIR="$ROOT_DIR/vendor/nvim-plugins/ambiwidth.nvim"

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
# 重要: +luafile のエラーは終了コードに伝わらず後続の +qall が exit 0 にしてしまう
# (nvim の仕様)。assert を pcall で捕捉し、失敗時は cquit で明示的に非0終了させる。
cat >"$check" <<EOF
local ok, err = pcall(function()
  -- require も pcall の内側に置く。モジュールが壊れ/改名/構文エラーだと require が throw するが、
  -- pcall の外だと +qall が exit 0 にして空振り PASS になる (回帰テストの意味が消える)。
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

  -- 5) 不正 add_list (width=3 は無効) は throw せず WARN 通知し、直前の適用を破壊しない
  apply(nil, nil) -- 正常適用しておく
  local before = #vim.fn.getcellwidths()
  local notified = false
  local orig = vim.notify
  vim.notify = function(msg, lvl)
    if lvl == vim.log.levels.WARN and tostring(msg):match("setcellwidths") then
      notified = true
    end
  end
  vim.g.ambiwidth_add_list = { { 0x3000, 0x3000, 3 } }
  local guarded = pcall(aw.setup)
  vim.notify = orig
  assert(guarded, "setup must not throw on invalid add_list")
  assert(notified, "setup must warn on invalid add_list")
  assert(#vim.fn.getcellwidths() == before, "invalid add_list must not corrupt prior cell widths")
end)

if not ok then
  vim.api.nvim_err_writeln("ambiwidth test failed: " .. tostring(err))
  vim.cmd("cquit 1")
end
EOF

log="$tmp_root/check_ambiwidth.log"
print "[test-nvim:zsh] ambiwidth (Lua port) setup()"
if ! "$NVIM_BIN" --headless -u NONE "+luafile $check" +qall >"$log" 2>&1; then
  cat "$log" >&2
  exit 1
fi
# cquit を確実に踏むための backstop (握り潰し経路が残っても stderr のエラーで検出する)。
if grep -qE 'E[0-9]{2,}:|Error detected while processing|stack traceback' "$log"; then
  print -u2 "[test-nvim:zsh] ambiwidth check produced errors:"
  cat "$log" >&2
  exit 1
fi
print "[test-nvim:zsh] ambiwidth ok"
