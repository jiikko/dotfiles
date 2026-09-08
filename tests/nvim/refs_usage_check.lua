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
--   4. **filetype を一緒に記録し、集計で層別する**。LSP 経路には Ruby 以外の全 filetype の
--      <C-k> が入るので、ft を落とすと「LSP N 回 = 定数を引いた回数」(issue 334 の判断基準) が
--      Go/TS の回数で水増しされ、しかも事後に分離できない (敵対レビュー P1-1)。
--   5. **ログが際限なく伸びない**。中身は「押した語」= 仕事の repo の識別子なので、
--      上限を超えたら古い半分を捨てる (敵対レビュー P3-6)。
--   6. lsp.lua の <C-k> / <leader>K が実際に record を **ft つきで** 呼ぶ。関数だけ正しくても
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
if usage.record("ripgrep", "wrap_error", "ruby") ~= true then
  fail_cleanup("record が false を返した (書き込めていない)")
end
clock = clock + 1000
usage.record("lsp", "SomeConstant", "ruby")
local c = usage.stats()
if c.ripgrep ~= 1 or c.lsp ~= 1 or c.fallback ~= 0 then
  fail_cleanup(("記録が ripgrep=%d lsp=%d fallback=%d。1/1/0 であること"):format(c.ripgrep, c.lsp, c.fallback))
end

-- 2. 同じ語を窓の内側で引き直したら fallback
clock = clock + 1000
usage.record("ripgrep", "perform", "ruby")
clock = clock + 5000 -- 既定の窓 (30s) の内側
usage.record("lsp", "perform", "ruby")
c = usage.stats()
if c.fallback ~= 1 then
  fail_cleanup(("同じ語を 5 秒後に引き直したのに fallback=%d。1 であること"):format(c.fallback))
end
if c.lsp ~= 1 then
  fail_cleanup(("fallback を lsp にも二重計上している (lsp=%d, 1 のままであること)"):format(c.lsp))
end

-- 2-a. 語が違えば fallback ではない
clock = clock + 1000
usage.record("ripgrep", "alpha", "ruby")
clock = clock + 1000
usage.record("lsp", "beta", "ruby")
c = usage.stats()
if c.fallback ~= 1 then
  fail_cleanup(("別の語を引いたのに fallback が %d に増えた。1 のままであること"):format(c.fallback))
end

-- 2-b. 窓の外なら fallback ではない
clock = clock + 1000
usage.record("ripgrep", "gamma", "ruby")
clock = clock + usage.fallback_window_ms + 1
usage.record("lsp", "gamma", "ruby")
c = usage.stats()
if c.fallback ~= 1 then
  fail_cleanup(("窓の外 (%d ms 後) なのに fallback が %d に増えた。1 のままであること"):format(
    usage.fallback_window_ms + 1, c.fallback))
end

-- 4. filetype の層別。Ruby 以外の <C-k> が Ruby の数字に混ざらないこと。
-- 前のセクションの記録が残っていると件数の期待値がずれるので、ここだけ新しいログで始める
usage.path = tmp .. "/refs_usage_ft.jsonl"
usage.record("lsp", "SomeConst", "ruby")
usage.record("lsp", "WrapError", "go")
usage.record("lsp", "wrapError", "typescript")
usage.record("ripgrep", "helper", "eruby")
local st = usage.stats()
if st.by_ft == nil then
  fail_cleanup("stats() が by_ft を返していない。filetype で層別できない")
end
if st.by_ft.ruby == nil or st.by_ft.ruby.lsp ~= 1 then
  fail_cleanup(("by_ft.ruby.lsp = %s。Ruby の LSP 経路は 1 件のはず (go/typescript を混ぜないこと)"):format(
    vim.inspect(st.by_ft.ruby and st.by_ft.ruby.lsp)))
end
if st.by_ft.go == nil or st.by_ft.go.lsp ~= 1 then
  fail_cleanup("by_ft.go.lsp が 1 でない。Ruby 以外も記録は残すこと (層別できることが要点)")
end
if st.lsp ~= 3 then
  fail_cleanup(("全体の lsp = %d。Ruby 1 + go 1 + typescript 1 = 3 のはず"):format(st.lsp))
end
-- ft を省いた行は層別に使えないので、その旨を数える
usage.record("ripgrep", "legacy")
st = usage.stats()
if st.no_ft ~= 1 then
  fail_cleanup(("no_ft = %s。ft 無しの行を 1 件数えること"):format(vim.inspect(st.no_ft)))
end
usage.record("ripgrep", "y", "ruby")
st = usage.stats()
if (st.by_ft.ruby and st.by_ft.ruby.ripgrep) ~= 1 then
  fail_cleanup(("by_ft.ruby.ripgrep = %s。ft 無しの行が ruby に混ざっている"):format(
    vim.inspect(st.by_ft.ruby and st.by_ft.ruby.ripgrep)))
end
-- 表示は Ruby の行を先に出す (判断に使う数字がどれかを取り違えないため)
local text = usage.format_stats()
if not text:match("^Ruby:") then
  fail_cleanup(("format_stats が %q で始まる。Ruby の行を先に出すこと"):format(text:sub(1, 20)))
end

-- 5. ログのローテーション
usage.path = tmp .. "/refs_usage_rotate.jsonl"
usage.max_bytes = 2000
for i = 1, 200 do
  usage.record("ripgrep", ("word_%d"):format(i), "ruby")
end
local size = (vim.uv.fs_stat(usage.path) or {}).size or 0
if size == 0 then
  fail_cleanup("ローテーションの検査でログが空。前提が作れていない")
end
if size > usage.max_bytes * 2 then
  fail_cleanup(("ログが %d バイト。上限 %d を大きく超えている (古い行を捨てていない)"):format(size, usage.max_bytes))
end
-- 捨てた後も集計は壊れない (新しい行は残っている)
local rot = usage.stats()
if rot.ripgrep == 0 then
  fail_cleanup("ローテーション後に集計が 0 件。新しい行まで捨てている")
end
if rot.ripgrep >= 200 then
  fail_cleanup(("ローテーション後も %d 件。古い行が捨てられていない"):format(rot.ripgrep))
end
usage.max_bytes = 512 * 1024

-- 3. 書けない場所でも例外を投げない。
-- 🚨 fixture は「なぜ書けないか」がコードから読める形にする。以前は /proc/... を使っていたが、
-- macOS には /proc が無く、たまたま / が読み取り専用で失敗していただけだった (理由が偶然)。
-- この repo は macOS 専用 (CLAUDE.md「対象プラットフォーム」) なので、書き込み不可の
-- ディレクトリを自分で作って使う。
local ro = tmp .. "/readonly"
vim.fn.mkdir(ro, "p")
vim.fn.setfperm(ro, "r-xr-xr-x")
usage.path = ro .. "/nested/refs_usage.jsonl"
local ok, err = pcall(usage.record, "ripgrep", "x")
if not ok then
  fail_cleanup(("書けない場所で record が例外を投げた: %s"):format(tostring(err)))
end
if err ~= false then
  fail_cleanup(("書けない場所で record が %s を返した。false であること"):format(tostring(err)))
end

vim.fn.setfperm(ro, "rwxr-xr-x") -- 権限を戻してから消す (でないと rf でも残る)
vim.fn.delete(tmp, "rf")

-- 4. lsp.lua の配線を静的に固定する (押して観測するには client と telescope が要るため)
local src = table.concat(vim.fn.readfile(vim.env.DOTFILES_LSP_LUA), "\n")
local ck = src:match('map%("n", "<C%-k>".-\n  end, "[^"]*"%)')
if not ck then
  fail("lsp.lua に <C-k> のマッピングが見つからない")
end
if not ck:find("record(", 1, true) then
  fail("<C-k> が record を呼んでいない。使用実績が 0 件のままになる")
end
-- 🚨 ft を渡しているか。落とすと層別できず、issue 334 の判断基準が測れない
if not ck:find("record(.-,%s*word,%s*ft%s*)") then
  fail("<C-k> の record 呼び出しに filetype が渡っていない (record(kind, word, ft) であること)")
end
local lk = src:match('map%("n", "<leader>K".-\n  end, "[^"]*"%)')
if not lk then
  fail("lsp.lua に <leader>K (LSP で引き直す) のマッピングが無い。fallback の観測点が消えている")
end
if not lk:find('record("lsp"', 1, true) then
  fail("<leader>K が record(\"lsp\", ...) を呼んでいない。引き直しが記録されない")
end
if not lk:find("filetype", 1, true) then
  fail("<leader>K の record 呼び出しに filetype が渡っていない")
end
if not src:find('require("dotfiles.refs_usage").setup()', 1, true) then
  fail("lsp.setup が refs_usage.setup を呼んでいない。:DotfilesRefsStats が生えない")
end

print("OK refs usage: 記録 / fallback の窓と語の一致 / filetype の層別 / ログのローテーション / 書き込み失敗の握り潰し / <C-k>・<leader>K の配線")
