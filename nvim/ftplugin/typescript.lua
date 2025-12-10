local map = vim.keymap.set
local opts = { buffer = true, silent = true }

map("n", "<leader>bi", "odebugger<Esc>", opts)
