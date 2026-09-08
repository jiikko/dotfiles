-- 参照検索 (<C-k>) の使用実績記録 (nvim/lua/dotfiles/refs_usage.lua と、lsp.lua の配線) の
-- headless 検証。test_refs_usage.sh から dofile される。守っている不変条件:
--
--   1. rg 経路と LSP 経路がそれぞれ記録される。数えられないと issue 334 の段階 2 に進むかを
--      数字で決められない (この記録の存在理由そのもの)。
--   2. **rg の直後に同じ語を LSP で引き直したときだけ** fallback として数える。
--      これが「rg の結果で足りなかった」の観測点。語が違う / 時間が離れているものを
--      fallback に数えると、率が水増しされて判断を誤る。
--   3. 記録の失敗が <C-k> を壊さない。書けない環境でも例外を投げず false を返すだけ。
--      (ログは「あれば嬉しい」もので、参照検索が動かなくなる方が害が大きい)
--   4. lsp.lua の <C-k> / <leader>K が実際に record を呼ぶ。関数だけ正しくても
--      呼ばれていなければ 0 件のまま「困っていない」と誤読する。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local usage = require("dotfiles.refs_usage")

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
usage.path = tmp .. "/refs_usage.jsonl"

local clock = 0
usage.now = function() return clock end

local function fail_cleanup(msg)
  vim.fn.delete(tmp, "rf")
  fail(msg)
end

-- 1. rg と LSP がそれぞれ記録される
if usage.record("ripgrep", "wrap_error") ~= true then
  fail_cleanup("record が false を返した (書き込めていない)")
end
clock = clock + 1000
usage.record("lsp", "SomeConstant")
local c = usage.stats()
if c.ripgrep ~= 1 or c.lsp ~= 1 or c.fallback ~= 0 then
  fail_cleanup(("記録が ripgrep=%d lsp=%d fallback=%d。1/1/0 であること"):format(c.ripgrep, c.lsp, c.fallback))
end

-- 2. 同じ語を窓の内側で引き直したら fallback
clock = clock + 1000
usage.record("ripgrep", "perform")
clock = clock + 5000 -- 既定の窓 (30s) の内側
usage.record("lsp", "perform")
c = usage.stats()
if c.fallback ~= 1 then
  fail_cleanup(("同じ語を 5 秒後に引き直したのに fallback=%d。1 であること"):format(c.fallback))
end
if c.lsp ~= 1 then
  fail_cleanup(("fallback を lsp にも二重計上している (lsp=%d, 1 のままであること)"):format(c.lsp))
end

-- 2-a. 語が違えば fallback ではない
clock = clock + 1000
usage.record("ripgrep", "alpha")
clock = clock + 1000
usage.record("lsp", "beta")
c = usage.stats()
if c.fallback ~= 1 then
  fail_cleanup(("別の語を引いたのに fallback が %d に増えた。1 のままであること"):format(c.fallback))
end

-- 2-b. 窓の外なら fallback ではない
clock = clock + 1000
usage.record("ripgrep", "gamma")
clock = clock + usage.fallback_window_ms + 1
usage.record("lsp", "gamma")
c = usage.stats()
if c.fallback ~= 1 then
  fail_cleanup(("窓の外 (%d ms 後) なのに fallback が %d に増えた。1 のままであること"):format(
    usage.fallback_window_ms + 1, c.fallback))
end

-- 3. 書けない場所でも例外を投げない
usage.path = "/proc/definitely-not-writable/refs_usage.jsonl"
local ok, err = pcall(usage.record, "ripgrep", "x")
if not ok then
  fail_cleanup(("書けない場所で record が例外を投げた: %s"):format(tostring(err)))
end
if err ~= false then
  fail_cleanup(("書けない場所で record が %s を返した。false であること"):format(tostring(err)))
end

vim.fn.delete(tmp, "rf")

-- 4. lsp.lua の配線を静的に固定する (押して観測するには client と telescope が要るため)
local src = table.concat(vim.fn.readfile(vim.env.DOTFILES_LSP_LUA), "\n")
local ck = src:match('map%("n", "<C%-k>".-\n  end, "[^"]*"%)')
if not ck then
  fail("lsp.lua に <C-k> のマッピングが見つからない")
end
for _, kind in ipairs({ "ripgrep", "lsp" }) do
  if not ck:find(('record("%s"'):format(kind), 1, true) then
    fail(("<C-k> が record(%q, ...) を呼んでいない。その経路が 0 件のままになる"):format(kind))
  end
end
local lk = src:match('map%("n", "<leader>K".-\n  end, "[^"]*"%)')
if not lk then
  fail("lsp.lua に <leader>K (LSP で引き直す) のマッピングが無い。fallback の観測点が消えている")
end
if not lk:find('record("lsp"', 1, true) then
  fail("<leader>K が record(\"lsp\", ...) を呼んでいない。引き直しが記録されない")
end
if not src:find('require("dotfiles.refs_usage").setup()', 1, true) then
  fail("lsp.setup が refs_usage.setup を呼んでいない。:DotfilesRefsStats が生えない")
end

print("OK refs usage: 記録 / fallback の窓と語の一致 / 書き込み失敗の握り潰し / <C-k>・<leader>K の配線")
