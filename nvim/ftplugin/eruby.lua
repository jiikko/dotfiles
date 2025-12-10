local map = vim.keymap.set
local opts = { buffer = true, silent = true }

map("n", "<leader>bi", "o<% binding.pry %><Esc>", opts)
