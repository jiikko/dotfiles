-- dotfiles.folds が行数の上限 (max_lines) を超えたバッファで fold を計算しないことの headless 検証 (issue 612)。
-- test_folds_size_limit.sh から dofile される。
--
-- 上限ちょうどのバッファでは今までどおり fold が作られ、上限 + 1 行では作られない (foldmethod は manual のまま)。
-- 上限ちょうどの側を見ないと、fold の計算が丸ごと死んだ状態でも「上限超えで作られない」が緑になる。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local max = require("dotfiles.folds").max_lines
if type(max) ~= "number" or max < 100 then
  fail("dotfiles.folds.max_lines が読めない: " .. tostring(max))
end

-- 3 行で 1 つの fold になる lua の関数を、ちょうど n 行になるまで並べたバッファで BufWinEnter を起こし、
-- 2 行目 (最初の関数の中) の foldlevel を返す
local function fold_level_with(n)
  vim.cmd("enew")
  local lines = {}
  for i = 1, n do
    local k = (i - 1) % 3
    lines[i] = (k == 0) and ("local function f%d()"):format(i) or (k == 1) and "  return 1" or "end"
  end
  vim.api.nvim_buf_set_lines(0, 0, -1, false, lines)
  vim.bo.filetype = "lua"
  vim.cmd("doautocmd BufWinEnter")
  return vim.fn.foldlevel(2), vim.wo.foldmethod
end

local at, at_method = fold_level_with(max)
if at < 1 then
  fail(("上限ちょうど (%d 行) のバッファで fold が作られない: foldlevel(2)=%d"):format(max, at))
end
if at_method ~= "manual" then
  fail("fold を計算した後の foldmethod が manual に戻っていない: " .. at_method)
end

local over, over_method = fold_level_with(max + 1)
if over ~= 0 then
  fail(("上限 + 1 行 (%d 行) のバッファで fold を計算した: foldlevel(2)=%d"):format(max + 1, over))
end
if over_method ~= "manual" then
  fail("上限を超えたバッファの foldmethod が manual でない: " .. over_method)
end

print(("OK folds size limit: %d 行は fold あり (level %d) / %d 行は fold なし"):format(max, at, max + 1))
