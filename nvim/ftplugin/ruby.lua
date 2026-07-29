local map = vim.keymap.set
local opts = { buffer = true, silent = true }

map("n", "<leader>bi", "obinding.pry<Esc>", opts)
local caller_file = "/tmp/ruby_caller"
map("n", "<leader>rw", ([[obegin; raise; rescue => e; File.write("%s", e.backtrace.join("\n")) && raise; end<Esc>]]):format(caller_file), opts)
-- cmdline を <CR> で確定する (旧 <Esc> 終端はマッピング経由だと :h c_<Esc> の macro 挙動で
-- 「実行」になり動いていたが、読み手には「キャンセル」に見える誤解を招く書き方だった)
map("n", "<leader>rr", (":cfile %s<CR>:cw<CR>"):format(caller_file), opts)
map("n", "<leader>re", (":e %s<CR>"):format(caller_file), opts)
map("n", "<leader>ds", ":e db/schema.rb<CR>", opts)
map("n", "<leader>yr", "o@return []<Esc>", opts)
map("n", "<leader>yp", "o@param []<Esc>", opts)
