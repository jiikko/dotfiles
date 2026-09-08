-- LSP の索引などの進捗表示 (nvim/lua/dotfiles/lsp.lua の M.progress_status と、_nviminit.lua の
-- lualine への配線) の headless 検証。test_lsp_progress.sh から dofile される。
-- 守っている不変条件:
--
--   1. LspProgress の begin / report で、statusline に出す文字列が入る。
--      索引に数分かかるサーバ (ruby-lsp の大きな Rails project) で「押しても無反応」に
--      見えるのを避けるための表示そのもの。
--   2. kind == "end" で空へ戻る。戻さないと、終わった索引の文字列が出っぱなしになる。
--   3. 🚨 statusline 側 (_nviminit.lua の lualine) が vim.lsp.status() を **直接呼ばない**。
--      vim.lsp.status() は client.progress (vim.ringbuf) を pop しながら読むので、1 回の再描画で
--      2 回評価されると 2 回目は必ず空になる。実行時には「たまに消える」形でしか出ず、
--      再現条件が描画回数に依存するため、静的に固定するのが唯一の現実的な検査になる。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local lsp = require("dotfiles.lsp")

if type(lsp.progress_status) ~= "function" then
  fail("M.progress_status が無い。statusline が vim.lsp.status() を直接呼ぶ形に戻っている")
end

-- autocmd を張るのは M.setup。capabilities は nil で足りる (進捗の保持に無関係)
lsp.setup(nil)

local orig_status = vim.lsp.status
local function restore() vim.lsp.status = orig_status end
local function fail_restoring(msg) restore(); fail(msg) end

local calls = 0
vim.lsp.status = function()
  calls = calls + 1
  return "Ruby LSP: indexing files: 42% completed"
end

local function emit(kind)
  vim.api.nvim_exec_autocmds("LspProgress", { data = { params = { value = { kind = kind } } } })
end

if lsp.progress_status() ~= "" then
  fail_restoring(("初期値が %q。空であること"):format(lsp.progress_status()))
end

for _, kind in ipairs({ "begin", "report" }) do
  calls = 0
  emit(kind)
  if lsp.progress_status() ~= "Ruby LSP: indexing files: 42% completed" then
    fail_restoring(("kind=%s の後の表示が %q。vim.lsp.status() の結果が入ること"):format(kind, lsp.progress_status()))
  end
  -- 1 通知につき status() は 1 回だけ (ring buffer を pop するので、余分に呼ぶと取りこぼす)
  if calls ~= 1 then
    fail_restoring(("kind=%s で vim.lsp.status() を %d 回呼んだ。1 回であること (pop するため)"):format(kind, calls))
  end
end

calls = 0
emit("end")
if lsp.progress_status() ~= "" then
  fail_restoring(("kind=end の後も %q が残っている。空へ戻すこと"):format(lsp.progress_status()))
end
if calls ~= 0 then
  fail_restoring(("kind=end で vim.lsp.status() を %d 回呼んだ。end では呼ばないこと"):format(calls))
end

restore()

-- 3. statusline 側の配線を静的に固定する
local init = table.concat(vim.fn.readfile(vim.env.DOTFILES_INIT or (vim.env.HOME .. "/dotfiles/_nviminit.lua")), "\n")
local lualine_x = init:match("lualine_x = (%b{})")
if not lualine_x then
  fail("_nviminit.lua に lualine_x のテーブルが見つからない (lualine の設定が動いた?)")
end
if not lualine_x:find("progress_status", 1, true) then
  fail("lualine_x が dotfiles.lsp の progress_status を参照していない。索引の進捗が出なくなる")
end
if lualine_x:find("vim.lsp.status", 1, true) then
  fail("lualine_x が vim.lsp.status() を直接呼んでいる。ring buffer を pop するので描画のたびに取りこぼす")
end

print("OK lsp progress: begin/report で表示・end で消去・status() は 1 通知 1 回・lualine の配線を静的に固定")
