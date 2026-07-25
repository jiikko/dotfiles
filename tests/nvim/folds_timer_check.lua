-- dotfiles.folds の debounce timer が uv handle を漏らさないことの headless 検証。
-- test_folds_timer.sh から dofile される。
--
-- 回帰の中身: vim.defer_fn の timer は自分の callback が走ったときだけ close される。
-- debounce を撃ち直すたびに stop() だけして捨てると handle が開いたまま残り、編集
-- (TextChanged) の連打で単調に増える。ここでは編集を連打して「生存 timer の増分」を見る。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local function live_timers()
  local n = 0
  vim.uv.walk(function(h)
    if h:get_type() == "timer" and not h:is_closing() then
      n = n + 1
    end
  end)
  return n
end

-- 実ファイル相当のバッファ (buftype="" かつ loaded) にする。folds.eligible の条件。
vim.api.nvim_buf_set_lines(0, 0, -1, false, { "local x = 1" })

local before = live_timers()
-- debounce (400ms) より速く 30 回編集する = 撃ち直しが 29 回起きる
for i = 1, 30 do
  vim.api.nvim_buf_set_lines(0, -1, -1, false, { ("-- line %d"):format(i) })
  vim.cmd("doautocmd TextChanged")
end
local after = live_timers()

-- 正しい実装なら生存するのは「最後に撃った 1 本」だけ。漏れていると 30 本近く増える。
local delta = after - before
if delta > 2 then
  fail(("debounce の撃ち直しで uv timer が漏れている: 増分 %d 本 (期待 <=2)"):format(delta))
end

-- timer が発火した後も handle が残らない (自然 close される) ことを見る
vim.wait(800, function() return false end)
if live_timers() > before + 1 then
  fail(("発火後も timer が残っている: %d 本 (開始時 %d 本)"):format(live_timers(), before))
end

print("OK folds timer: 撃ち直し 29 回で増分 " .. delta .. " 本")
