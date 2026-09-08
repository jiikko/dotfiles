-- <C-k> (参照一覧) の振り分け (nvim/lua/dotfiles/lsp.lua の M.use_ripgrep_references と、
-- on_attach の <C-k> マッピング) の headless 検証。test_lsp_references_dispatch.sh から dofile。
-- 守っている不変条件:
--
--   1. Ruby の **メソッド / ローカル** (小文字始まり) だけ ripgrep へ回す。ruby-lsp の
--      references は索引を使わず全ファイルを再パースし (references.rb:63)、しかも一致条件は
--      名前一致だけ (reference_finder.rb:285) なので、11 秒かけて grep と同じ精度しか出ない。
--   2. **定数・クラス (大文字始まり / :: を含む) は LSP に残す**。こちらは index.resolve で
--      名前空間を解決するので grep より正確。ここを取り違えると精度が落ちる。
--   3. **Ruby 以外は LSP のまま**。gopls / ts_ls の references は型解析つきで速いので、
--      grep へ回すのは劣化にしかならない。
--   4. <C-k> のマッピングがこの判定を実際に通る。判定関数だけ正しくても、マッピングから
--      呼ばれていなければ 1 mm も効かない (実行時は「速いはずが遅い」としてしか現れない)。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local lsp = require("dotfiles.lsp")

if type(lsp.use_ripgrep_references) ~= "function" then
  fail("M.use_ripgrep_references が無い。<C-k> の振り分けが消えている")
end

local cases = {
  { ft = "ruby", word = "wrap_error", want = true, why = "Ruby のメソッド" },
  { ft = "ruby", word = "account", want = true, why = "Ruby のローカル変数" },
  { ft = "ruby", word = "_private_helper", want = true, why = "_ 始まりのメソッド" },
  { ft = "ruby", word = "ApplicationRecord", want = false, why = "定数 (index.resolve の方が正確)" },
  -- <cword> は :: を含まない (iskeyword に : が無い。実測) ので、名前空間つきの参照でも
  -- 判定に来るのは末尾の定数名だけ。大文字始まりの規則がそれを覆う
  { ft = "ruby", word = "BaseLoader", want = false, why = "名前空間つき参照でも <cword> は定数名だけ" },
  { ft = "ruby", word = "", want = false, why = "カーソル下に語が無い" },
  { ft = "eruby", word = "wrap_error", want = true, why = "eruby も Ruby と同じ" },
  { ft = "go", word = "wrapError", want = false, why = "gopls は型解析つきで速い" },
  { ft = "typescript", word = "wrapError", want = false, why = "ts_ls も同上" },
  { ft = "", word = "wrap_error", want = false, why = "filetype なし" },
}

local checked = 0
for _, c in ipairs(cases) do
  local got = lsp.use_ripgrep_references(c.ft, c.word)
  if got ~= c.want then
    fail(("use_ripgrep_references(%q, %q) = %s, want %s (%s)"):format(c.ft, c.word, tostring(got), tostring(c.want), c.why))
  end
  checked = checked + 1
end

-- 4. マッピング側がこの判定を通ることを静的に固定する。
-- 実際に <C-k> を押す形にはしない: client の attach と telescope のロードが要り、
-- 押した結果は picker が開くかどうかでしか観測できないため (どちらの経路でも picker は開く)。
local src = table.concat(vim.fn.readfile(vim.env.DOTFILES_LSP_LUA), "\n")
local ck = src:match('map%("n", "<C%-k>".-\n  end, "[^"]*"%)')
if not ck then
  fail("lsp.lua に <C-k> のマッピングが見つからない (書き方が変わった?)")
end
if not ck:find("use_ripgrep_references", 1, true) then
  fail("<C-k> のマッピングが use_ripgrep_references を通っていない。判定が効かず常に LSP になる")
end
if not ck:find("grep_string", 1, true) then
  fail("<C-k> のマッピングに grep_string が無い。ripgrep 経路が消えている")
end
if not ck:find("lsp_references", 1, true) then
  fail("<C-k> のマッピングに lsp_references が無い。定数と他言語の経路が消えている")
end

print(("OK lsp references dispatch: %d ケース (Ruby のメソッドは rg / 定数と他言語は LSP) + <C-k> の配線を静的に固定"):format(checked))
