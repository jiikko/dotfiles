-- プラグインロードトラッカー (nvim/lua/dotfiles/plugin_load_tracker.lua) の headless 検証。
-- 守っている不変条件:
--
--   1. **UI の付いていないセッションでは記録しない**。headless の nvim (tests/nvim/ の全テスト)
--      もキー操作・ファイルオープンでプラグインをロードするため、数えると count がテストの
--      実行回数で埋まり、棚卸しの根拠にならない (実測: 1 テスト実行で計測対象 7 本が揃って +1)。
--   2. **UI 接続より前に起きたロードも、UIEnter が来たら数える**。`nvim foo.md` の
--      render-markdown は UI 接続前にロードされる。1 を「UI が無ければ捨てる」で実装すると
--      これが永久に 0 になる。
--
-- 🚨 記録先は test_plugin_load_tracker.sh が XDG_STATE_HOME で隔離している。隔離が外れると
--    本物の ~/.local/state/nvim/plugin-loads.json を書き換えるので、最初に検査して止める。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local sandbox = vim.env.TT_TRACKER_SANDBOX
if not sandbox or sandbox == "" then
  fail("TT_TRACKER_SANDBOX が未設定 (test_plugin_load_tracker.sh から走らせる)")
end
local state_file = vim.fn.stdpath("state") .. "/plugin-loads.json"
if state_file:sub(1, #sandbox + 1) ~= sandbox .. "/" then
  fail("記録先が隔離先の外: " .. state_file)
end

local function read_db()
  local f = io.open(state_file, "r")
  if not f then return nil end
  local raw = f:read("*a")
  f:close()
  return vim.json.decode(raw)
end

if #vim.api.nvim_list_uis() ~= 0 then
  fail("前提が崩れている: headless なのに UI が付いている")
end

-- lazy は User LazyLoad を schedule で撃つ (load() の戻りより後)。届くまで待ってから見ないと、
-- トラッカーが何もしていなくても「記録されていない」で通る
local seen = {}
vim.api.nvim_create_autocmd("User", {
  pattern = "LazyLoad",
  callback = function(ev) seen[ev.data] = true end,
})
local function load(name)
  require("lazy").load({ plugins = { name } })
  if not vim.wait(2000, function() return seen[name] end, 10) then
    fail("LazyLoad " .. name .. " が 2 秒待っても来ない")
  end
end

-- 1. UI が無い間のロードはファイルに出ない
load("trouble.nvim")
if read_db() ~= nil then
  fail("UI の無いセッションで記録された: " .. vim.inspect(read_db()))
end

-- 2. UIEnter で溜めた分を書く
vim.api.nvim_exec_autocmds("UIEnter", {})
local db = read_db()
if not (db and db["trouble.nvim"] and db["trouble.nvim"].count == 1) then
  fail("UIEnter 後に UI 接続前のロードが記録されていない: " .. vim.inspect(db))
end

-- UIEnter 以降のロードはその場で書く
load("glance.nvim")
db = read_db()
if not (db["glance.nvim"] and db["glance.nvim"].count == 1) then
  fail("UIEnter 後のロードが記録されていない: " .. vim.inspect(db))
end

print("OK plugin load tracker: headless は記録せず、UIEnter で溜めた分を書く")
