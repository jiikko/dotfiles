local map = vim.keymap.set
local opts = { buffer = true, silent = true }

map("n", "<leader>bi", "obinding.pry<Esc>", opts)
map("n", "<leader>rw", [[obegin; raise; rescue => e; File.write("/tmp/ruby_caller", e.backtrace.join("\n")) && raise; end<Esc>]], opts)
map("n", "<leader>rr", [[:cfile /tmp/ruby_caller<CR>:cw<Esc>]], opts)
map("n", "<leader>re", ":e /tmp/ruby_caller<Esc>", opts)
map("n", "<leader>ds", ":e db/schema.rb<Esc>", opts)
map("n", "<leader>yr", "o@return []<Esc>", opts)
map("n", "<leader>yp", "o@param []<Esc>", opts)
