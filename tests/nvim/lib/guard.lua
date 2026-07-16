-- tests/nvim/lib/guard.lua — headless Lua check の false-pass 防御 (dofile して使う)。
--
-- nvim は +luafile/dofile 内の Lua エラー (assert/error/require 失敗) を終了コードに
-- 伝えず、後続の +qall/+qall! が exit 0 にしてしまう。この guard は check 本体
-- (require 含む全て) を pcall で捕捉し、失敗時に nvim_err_writeln + cquit 1 で確実に
-- 非0終了させる。かつてこの pcall+cquit 契約が各テストへ手書きコピペされ、適用漏れが
-- 実 false-pass を2回起こした (f51f0b0 で導入 / 54dbc81 で残存経路を閉鎖) ため一元化した。
--
-- 使い方 (shell 側 heredoc の先頭で):
--   local guard = dofile([[<このファイルの絶対パス>]])
--   guard("<label>", function()
--     ... check 本体。require もこの中に書く (外に置くと throw が exit 0 に化ける) ...
--   end)
--
-- ⚠️ 呼び出し側 shell は log の grep backstop (lib/check_log.sh の tt_nvim_log_backstop)
--    も必ず併用すること。cquit 経路をすり抜けて stderr にだけ出るエラー
--    (ftplugin/autocmd 内のエラー等は "Error detected while processing" で exit 0) の最終防衛線。
return function(label, fn)
  local ok, err = pcall(fn)
  if not ok then
    vim.api.nvim_err_writeln(label .. ": " .. tostring(err))
    vim.cmd("cquit 1")
  end
end
