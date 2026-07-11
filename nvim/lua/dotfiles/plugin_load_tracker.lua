-- プラグインロードトラッカー: lazy.nvim の `User LazyLoad` を拾い、「ロード = 使用」と
-- みなせるプラグインだけロード回数を永続化する。棚卸し時に :PluginLoadStats で count=0 の
-- プラグインを削除候補として洗い出すのが目的。
--
-- 計測できるのはトリガーゲート (keys / cmd / ft / パターン付き event) のプラグインのみ。
-- VeryLazy / パターン無し event (BufReadPre 等) のプラグインは毎セッション無条件に
-- ロードされるため「ロード = 使用」が成立せず、計測対象から除外する (カウントしても
-- 起動回数にしかならない)。この制約と読み方は docs/nvim-plugin-load-tracker.md 参照。
--
-- count は「そのプラグインを使ったセッション数」に近い (lazy のロードはセッション中
-- 1 回だけ発火するため、キー押下ごとの加算ではない)。
--
-- off にする: 環境変数 DOTFILES_PLUGIN_LOAD_TRACKER=0 (記録・コマンド登録ごと止まる)。
local M = {}

local state_file = vim.fn.stdpath("state") .. "/plugin-loads.json"

local function read_db()
  local f = io.open(state_file, "r")
  if not f then return {} end
  local raw = f:read("*a")
  f:close()
  local ok, db = pcall(vim.json.decode, raw)
  return (ok and type(db) == "table") and db or {}
end

local function write_db(db)
  local f = io.open(state_file, "w")
  if not f then return end
  f:write(vim.json.encode(db))
  f:close()
end

-- event 一覧を table に正規化する
local function event_list(plugin)
  local ev = plugin.event
  if type(ev) == "string" then return { ev } end
  if type(ev) == "table" then return ev end
  return {}
end

-- 「ロード = 使用」が成立するか。
--   - keys / cmd / ft ゲート → 成立 (ユーザー操作・特定 ft でのみロード)
--   - パターン付き event ("BufReadPre *.md") → 成立 (特定ファイル種を開いた)
--   - ただしパターン無し event (VeryLazy / BufReadPre / InsertEnter 等) が 1 つでも
--     あれば不成立 (使わなくてもロードされる経路がある = カウントが汚染される)
--   - eager (lazy=false 相当) → 不成立
function M.is_trackable(plugin)
  if plugin.lazy == false then return false end
  for _, e in ipairs(event_list(plugin)) do
    if type(e) == "string" and not e:find("%s") then
      return false -- パターン無し event: 無条件ロード経路
    end
    if type(e) == "table" then
      return false -- {event=..., pattern=...} 形式は現状の spec に無い。増えたら対応を再評価
    end
  end
  if plugin.keys or plugin.cmd or plugin.ft then return true end
  return #event_list(plugin) > 0 -- パターン付き event のみで構成 (render-markdown 型)
end

local function record(name)
  local db = read_db()
  local rec = db[name] or { count = 0 }
  rec.count = rec.count + 1
  rec.last_used = os.date("%Y-%m-%d %H:%M")
  db[name] = rec
  write_db(db)
end

function M.report()
  local plugins = require("lazy.core.config").plugins
  local db = read_db()
  local rows = {}
  for name, plugin in pairs(plugins) do
    if M.is_trackable(plugin) then
      local rec = db[name]
      table.insert(rows, {
        name = name,
        count = rec and rec.count or 0,
        last = rec and rec.last_used or "-",
      })
    end
  end
  table.sort(rows, function(a, b)
    if a.count ~= b.count then return a.count < b.count end
    return a.name < b.name
  end)
  local lines = { ("計測対象 %d 件 (少ない順。count=使用セッション数)"):format(#rows) }
  for _, r in ipairs(rows) do
    table.insert(lines, ("%4d  %-30s last: %s"):format(r.count, r.name, r.last))
  end
  vim.notify(table.concat(lines, "\n"), vim.log.levels.INFO, { title = "PluginLoadStats" })
end

function M.reset()
  if vim.fn.delete(state_file) == 0 then
    vim.notify("プラグインロード実績をリセットしました: " .. state_file, vim.log.levels.INFO)
  else
    vim.notify("実績ファイルはありません (既にリセット済み): " .. state_file, vim.log.levels.INFO)
  end
end

function M.setup()
  if vim.env.DOTFILES_PLUGIN_LOAD_TRACKER == "0" then return end
  vim.api.nvim_create_autocmd("User", {
    pattern = "LazyLoad",
    group = vim.api.nvim_create_augroup("dotfiles_plugin_load_tracker", { clear = true }),
    callback = function(ev)
      local plugin = require("lazy.core.config").plugins[ev.data]
      if plugin and M.is_trackable(plugin) then
        record(ev.data)
      end
    end,
  })
  vim.api.nvim_create_user_command("PluginLoadStats", M.report, {
    desc = "プラグインのロード回数を表示 (少ない順)",
  })
  vim.api.nvim_create_user_command("PluginLoadStatsReset", M.reset, {
    desc = "プラグインのロード実績 (plugin-loads.json) を削除して計測をやり直す",
  })
end

return M
