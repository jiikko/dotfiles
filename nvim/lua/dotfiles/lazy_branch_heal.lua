-- lazy-lock.json の branch が plugin spec の branch と食い違っていたら、起動時にそのプラグインだけ
-- spec の branch へ update して lock を書き直す (自己修復)。
--
-- なぜ要るか: spec を `branch = "main"` に変えても、lock が旧 branch の commit を固定したままだと
-- lazy は旧 branch を checkout し続ける。実例 2026-09-24: nvim-treesitter-textobjects の lock が
-- master のまま残り、master 版が削除済みの nvim-treesitter.configs を require して毎起動で落ちた。
-- これは luac キャッシュの問題ではないので require_resilient (init.lua) では直らない。
--
-- 修復した回の起動では既にエラーが出ている (config 実行後にしか検出できない) ため、通知で再起動を促す。
-- ネットワークが無ければ update が失敗するだけで起動は止めない。
local M = {}

-- 純関数: spec の branch と lock の branch が両方あり、かつ異なるプラグイン名を返す (名前順)。
-- plugins: { [name] = { branch = "main" | nil } } / lock: { [name] = { branch = "..." } }
function M.mismatches(plugins, lock)
  local out = {}
  for name, p in pairs(plugins) do
    local l = lock[name]
    if p.branch and l and l.branch and l.branch ~= p.branch then
      table.insert(out, name)
    end
  end
  table.sort(out)
  return out
end

local function read_lock(path)
  local f = io.open(path, "r")
  if not f then return nil end
  local raw = f:read("*a")
  f:close()
  local ok, t = pcall(vim.json.decode, raw)
  return (ok and type(t) == "table") and t or nil
end

function M.setup()
  -- headless (テスト・CI) ではネットワーク update を走らせない
  if #vim.api.nvim_list_uis() == 0 and vim.env.DOTFILES_LAZY_HEAL_FORCE ~= "1" then return end
  local ok, cfg = pcall(require, "lazy.core.config")
  if not ok then return end
  local lock = read_lock(cfg.options.lockfile)
  if not lock then return end
  local names = M.mismatches(cfg.plugins, lock)
  if #names == 0 then return end
  local msg = table.concat(names, ", ")
  vim.notify(("lazy-lock の branch が spec と食い違い: %s。spec の branch へ更新します"):format(msg),
    vim.log.levels.WARN)
  require("lazy").update({ plugins = names, show = false, wait = true })
  local after = read_lock(cfg.options.lockfile) or {}
  local left = M.mismatches(cfg.plugins, after)
  if #left == 0 then
    vim.notify(("%s を spec の branch へ揃えました。nvim を再起動してください"):format(msg), vim.log.levels.WARN)
  else
    vim.notify(("自動更新に失敗 (ネットワーク?): %s。:Lazy update で手動更新してください"):format(
      table.concat(left, ", ")), vim.log.levels.ERROR)
  end
end

return M
