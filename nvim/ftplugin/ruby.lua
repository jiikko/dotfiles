local map = vim.keymap.set
local opts = { buffer = true, silent = true }

map("n", "<leader>bi", "obinding.pry<Esc>", opts)
local caller_file = "/tmp/ruby_caller"
map("n", "<leader>rw", ([[obegin; raise; rescue => e; File.write("%s", e.backtrace.join("\n")) && raise; end<Esc>]]):format(caller_file), opts)
map("n", "<leader>rr", (":cfile %s<CR>:cw<Esc>"):format(caller_file), opts)
map("n", "<leader>re", (":e %s<Esc>"):format(caller_file), opts)
map("n", "<leader>ds", ":e db/schema.rb<Esc>", opts)
map("n", "<leader>yr", "o@return []<Esc>", opts)
map("n", "<leader>yp", "o@param []<Esc>", opts)
