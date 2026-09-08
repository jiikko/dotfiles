-- LSP の索引などの進捗表示 (nvim/lua/dotfiles/lsp.lua の M.progress_status と、_nviminit.lua の
-- lualine への配線) の headless 検証。test_lsp_progress.sh から dofile される。
-- 守っている不変条件:
--
--   1. LspProgress の begin / report で、statusline に出す文字列が入る。
--      索引に数分かかるサーバ (ruby-lsp の大きな Rails project) で「押しても無反応」に
--      見えるのを避けるための表示そのもの。
--   2. kind == "end" で空へ戻る。戻さないと、終わった索引の文字列が出っぱなしになる。
--   3. 実行中の要求 (LspRequest) を出す。pending で出て complete / cancel で消える。
--      対象は M.request_labels に載せた「ユーザーが明示的に起こす操作」だけ:
--      CursorHold ごとに飛ぶ documentHighlight まで出すと点滅するだけで情報にならない。
--      ruby-lsp の references は大きな Rails project で 10 秒級かかる (実測) ので、
--      これが無いと「押したのに無反応」に見える。
--   4. 索引 ($/progress) は実行中の要求より優先して出す。索引中はどのみち要求が返らないため。
--   5. **client が死んでも表示が残らない**。nvim は client 終了時に in-flight の要求へ
--      complete を投げないので、掃除しないと「参照を検索中…」が永久に残る (敵対レビュー P2-2)。
--   6. **表示が変わらないときは再描画しない**。lualine の statusline は関数評価で、再描画のたびに
--      lualine_c の relative_path_from_git_root (中で vim.fs.root) が走る。索引中の $/progress は
--      高頻度なので、無条件に redrawstatus を呼ぶと再描画が跳ねる (敵対レビュー P2-5)。
--   7. 🚨 statusline 側 (_nviminit.lua の lualine) が vim.lsp.status() を **直接呼ばない**。
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

-- 3/4. 実行中の要求 (LspRequest)
local function emit_req(id, method, type_)
  vim.api.nvim_exec_autocmds("LspRequest", {
    data = { client_id = 1, request_id = id, request = { type = type_, bufnr = 0, method = method } },
  })
end

if lsp.request_labels["textDocument/references"] == nil then
  fail("request_labels に textDocument/references が無い。参照検索が最も遅いので必ず出す")
end
if lsp.request_labels["textDocument/documentHighlight"] ~= nil then
  fail("request_labels に documentHighlight が入っている。CursorHold ごとに飛ぶので点滅する")
end

emit_req(1, "textDocument/references", "pending")
if lsp.progress_status() ~= "LSP: " .. lsp.request_labels["textDocument/references"] then
  fail(("pending 中の表示が %q。実行中の要求を出すこと"):format(lsp.progress_status()))
end
emit_req(1, "textDocument/references", "complete")
if lsp.progress_status() ~= "" then
  fail(("complete 後も %q が残っている。消すこと"):format(lsp.progress_status()))
end

-- cancel でも消える
emit_req(2, "textDocument/references", "pending")
emit_req(2, "textDocument/references", "cancel")
if lsp.progress_status() ~= "" then
  fail(("cancel 後も %q が残っている。消すこと"):format(lsp.progress_status()))
end

-- 対象外のメソッドは出さない。表示が空かどうかだけでは足りない (ラベル表に無いメソッドは
-- どのみち nil になるので、表示は空のまま通る)。効いているのは「再描画を起こさないこと」なので
-- そちらを数える。documentHighlight は CursorHold ごとに飛ぶため、ここで redrawstatus を呼ぶと
-- 常時再描画になる。
local orig_cmd = vim.cmd
local redraws = 0
vim.cmd = setmetatable({}, {
  __index = function(_, k)
    if k == "redrawstatus" then
      return function() redraws = redraws + 1 end
    end
    return orig_cmd[k]
  end,
  __call = function(_, ...) return orig_cmd(...) end,
})
local function fail_cmd(msg) vim.cmd = orig_cmd; restore(); fail(msg) end

redraws = 0
emit_req(3, "textDocument/documentHighlight", "pending")
if lsp.progress_status() ~= "" then
  fail_cmd(("documentHighlight で %q を出した。対象外のメソッドは出さないこと"):format(lsp.progress_status()))
end
if redraws ~= 0 then
  fail_cmd(("documentHighlight で redrawstatus を %d 回呼んだ。CursorHold ごとに飛ぶので 0 回であること"):format(redraws))
end
emit_req(3, "textDocument/documentHighlight", "complete")
if redraws ~= 0 then
  fail_cmd(("documentHighlight の complete で redrawstatus を %d 回呼んだ。0 回であること"):format(redraws))
end

-- 対象のメソッドでは再描画する (上の 0 回が「そもそも数えていない」ではないことの対照)
redraws = 0
emit_req(5, "textDocument/references", "pending")
if redraws == 0 then
  fail_cmd("references の pending で redrawstatus が呼ばれていない。表示が更新されない")
end
emit_req(5, "textDocument/references", "complete")

-- 6. 同じ内容で通知が続いても再描画しない (索引中の $/progress は高頻度で飛ぶ)
vim.lsp.status = function() return "Ruby LSP: indexing files: 5% completed" end
emit("begin")
redraws = 0
emit("report")
emit("report")
if redraws ~= 0 then
  fail_cmd(("同じ内容の通知で redrawstatus を %d 回呼んだ。表示が変わったときだけ呼ぶこと"):format(redraws))
end
vim.lsp.status = function() return "Ruby LSP: indexing files: 6% completed" end
emit("report")
if redraws ~= 1 then
  fail_cmd(("内容が変わったのに redrawstatus が %d 回。1 回であること"):format(redraws))
end
emit("end")
vim.cmd = orig_cmd

-- 5. client が死んだら (LspDetach) 表示を掃除する
vim.lsp.status = function() return "Ruby LSP: indexing files: 42% completed" end
emit("begin")
emit_req(9, "textDocument/references", "pending")
if lsp.progress_status() == "" then
  fail_restoring("前提が作れていない: 索引中 + 要求中で表示が空になっている")
end
vim.api.nvim_exec_autocmds("LspDetach", { data = { client_id = 1 } })
if lsp.progress_status() ~= "" then
  fail_restoring(("LspDetach の後も %q が残っている。client が死んだら掃除すること"):format(
    lsp.progress_status()))
end
-- 別 client の要求は残す (巻き添えで消さない)
emit_req(11, "textDocument/references", "pending")
vim.api.nvim_exec_autocmds("LspDetach", { data = { client_id = 2 } })
if lsp.progress_status() == "" then
  fail_restoring("別 client (id=2) の detach で、生きている client の表示まで消した")
end
vim.api.nvim_exec_autocmds("LspDetach", { data = { client_id = 1 } })

-- 4. 索引が実行中の要求より優先される
vim.lsp.status = function() return "Ruby LSP: indexing files: 10% completed" end
emit(  "begin")
emit_req(4, "textDocument/references", "pending")
if lsp.progress_status() ~= "Ruby LSP: indexing files: 10% completed" then
  fail(("索引中なのに %q を出した。索引を優先すること"):format(lsp.progress_status()))
end
emit("end")
if lsp.progress_status() ~= "LSP: " .. lsp.request_labels["textDocument/references"] then
  fail(("索引が終わった後の表示が %q。まだ実行中の要求が残っている"):format(lsp.progress_status()))
end
emit_req(4, "textDocument/references", "complete")
restore()

-- 5. statusline 側の配線を静的に固定する
local init = table.concat(vim.fn.readfile(vim.env.DOTFILES_INIT or (vim.env.HOME .. "/dotfiles/_nviminit.lua")), "\n")
local lualine_x = init:match("lualine_x = (%b{})")
if not lualine_x then
  fail("_nviminit.lua に lualine_x のテーブルが見つからない (lualine の設定が動いた?)")
end
if not lualine_x:find("progress_status", 1, true) then
  fail("lualine_x が dotfiles.lsp の progress_status を参照していない。索引の進捗が出なくなる")
end
-- 🚨 射程は lualine_x の中だけでは足りない。lualine_c へ移す / ヘルパー関数に包む書き換えが
-- 素通りするため、_nviminit.lua 全体で禁止する (statusline の評価経路のどこからも呼ばせない)。
-- コメント行は除く: この禁止事項そのものをコメントで説明しているので、素の部分一致だと自分に当たる
for i, line in ipairs(vim.fn.readfile(vim.env.DOTFILES_INIT or (vim.env.HOME .. "/dotfiles/_nviminit.lua"))) do
  if not line:match("^%s*%-%-") and line:find("vim.lsp.status", 1, true) then
    fail(("_nviminit.lua:%d が vim.lsp.status() を直接呼んでいる。ring buffer を pop するので描画のたびに取りこぼす: %s"):format(
      i, vim.trim(line)))
  end
end

print("OK lsp progress: 索引の begin/report/end・status() は 1 通知 1 回・実行中の要求・対象外メソッドの抑止・索引の優先・同内容では再描画しない・LspDetach の掃除・lualine の配線")
