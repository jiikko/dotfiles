-- JS/TS 系 ftplugin の共通バッファローカル設定。
-- require はキャッシュされるため、ftplugin 側からは setup() を毎バッファ呼ぶこと
-- (モジュールのトップレベルに buffer=true のマップを書くと最初の1バッファにしか効かない)。
local M = {}

function M.setup()
  local map = vim.keymap.set
  local opts = { buffer = true, silent = true }

  map("n", "<leader>bi", "odebugger<Esc>", opts)
end

return M
