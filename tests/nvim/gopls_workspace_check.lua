-- gopls workspace 判定の純関数と nvim LSP callback の配線を headless で検証する。
-- 守る契約:
--   1. git common dir が dotfiles と同じ、かつ対象ファイルが src/*/go.mod 配下の時だけ採用する。
--   2. root は各 checkout/worktree の toplevel、folders はその root の src modules だけ。
--   3. root 判定 / git / module 列挙の失敗は既定 root に戻り、例外を呼び出し元へ出さない。
--   4. before_init は client config にも folders と GOWORK=off を保存し、再起動後も維持する。
--   5. dotfiles の module 外には gopls client を流用せず、それ以外は nvim 0.12 の既定判定に従う。
--   6. gopls の起動・有無判定は Mason の絶対パスを使う。
local function fail(message)
  io.stderr:write("FAIL: " .. message .. "\n")
  os.exit(1)
end

local function expect(condition, message)
  if not condition then fail(message) end
end

local function same_list(got, want, message)
  expect(type(got) == "table" and #got == #want, message .. ": 件数が違う")
  for index, value in ipairs(want) do
    expect(got[index] == value, ("%s: [%d]=%s, want %s"):format(message, index, tostring(got[index]), value))
  end
end

local lsp = require("dotfiles.lsp")
local root = "/fixture/dotfiles-wt"
local module_a = root .. "/src/a"
local module_b = root .. "/src/b"
local module_dirs = {
  root .. "/src/ignored/nested",
  module_b,
  module_a,
  root .. "/outside/module",
  module_a,
}

local folders = lsp.gopls_module_folders(root, module_dirs)
same_list(folders, { module_a, module_b }, "src 直下の module folders を安定順にする")

local common = "/fixture/dotfiles/.git"
local decision = lsp.gopls_workspace_decision(
  root, module_a .. "/pkg/file.go", common, common, module_dirs
)
expect(decision ~= nil and decision.root_dir == root, "common dir 一致時に worktree root を選ぶ")
same_list(decision.folders, { module_a, module_b }, "decision の folders")
expect(lsp.gopls_workspace_decision(root, module_a .. "/pkg/file.go", "/fixture/other/.git", common, module_dirs) == nil,
  "同じ構造でも別 repo の common dir は対象外")
expect(lsp.gopls_workspace_decision(root, root .. "/tests/helper.go", common, common, module_dirs) == nil,
  "src module 外は対象外")
expect(lsp.gopls_workspace_decision(root, root .. "/src/not-a-module/file.go", common, common, module_dirs) == nil,
  "module として列挙されていない src 配下は対象外")
expect(lsp.gopls_workspace_decision(root, module_a .. "/pkg/file.go", common, common, {}) == nil,
  "module が無ければ workspace を作らない")

-- wiring は filesystem / git を stub して固定する。実際の外部 repo と Mason は起動しない。
local saved_default = lsp.gopls_default_root_dir
local saved_discover = lsp.gopls_workspace_for_buffer
local saved_requests = lsp.gopls_workspace_requests
local saved_settings = vim.deepcopy(lsp.servers.gopls.settings)
local function restore()
  lsp.gopls_default_root_dir = saved_default
  lsp.gopls_workspace_for_buffer = saved_discover
  lsp.gopls_workspace_requests = saved_requests
end
local function fail_restoring(message)
  restore()
  fail(message)
end

lsp.gopls_workspace_requests = {}
lsp.gopls_default_root_dir = function(_, on_dir) on_dir("/fixture/default-root") end
local decisions_by_buf = {}
lsp.gopls_workspace_for_buffer = function(bufnr)
  return decisions_by_buf[bufnr]
end

local init_paths = { module_a, module_b }
for index = 3, 17 do
  table.insert(init_paths, root .. ("/src/module%02d"):format(index))
end
table.sort(init_paths)
local init_decision = { root_dir = root, folders = init_paths }

local buf_a = vim.api.nvim_create_buf(false, true)
vim.api.nvim_buf_set_name(buf_a, module_a .. "/pkg/file.go")
decisions_by_buf[buf_a] = init_decision
local got_root
lsp.gopls_root_dir(buf_a, function(value) got_root = value end)
if got_root ~= root then fail_restoring(("root_dir=%s, want %s"):format(tostring(got_root), root)) end

local initial_folders = { { uri = vim.uri_from_fname(root), name = root } }
local config_a = { name = "gopls", root_dir = got_root, settings = vim.deepcopy(lsp.servers.gopls.settings) }
local params_a = { workspaceFolders = initial_folders }
lsp.gopls_before_init(params_a, config_a)
expect(config_a.settings.gopls.env.GOWORK == "off", "dotfiles client だけ GOWORK=off を持つ")
same_list(vim.tbl_map(function(folder) return vim.uri_to_fname(folder.uri) end, params_a.workspaceFolders),
  init_paths, "before_init が 17 module folders に差し替える")
expect(params_a.workspaceFolders == initial_folders, "workspaceFolders 配列は client 内部と共有したまま更新する")
expect(config_a.workspace_folders == params_a.workspaceFolders, "再起動用に config.workspace_folders も同じ配列を保持する")
expect(lsp.servers.gopls.settings.gopls.env == nil, "client settings 変更が M.servers.gopls に漏れない")

-- Client:_restart() は lsp.start(self.config, { attach = false }) を呼び、root_dir callback を通らない。
-- client.lua の初期化と同様に config.workspace_folders から params を作り、request 無しで再初期化する。
local restart_folders = vim.lsp._get_workspace_folders(config_a.workspace_folders or config_a.root_dir)
local restart_params = { workspaceFolders = restart_folders }
lsp.gopls_before_init(restart_params, config_a)
same_list(vim.tbl_map(function(folder) return vim.uri_to_fname(folder.uri) end, restart_params.workspaceFolders),
  init_paths, "request 無しの再起動でも 17 module folders を保つ")
expect(config_a.settings.gopls.env.GOWORK == "off", "request 無しの再起動でも config の GOWORK=off を保つ")

local worktree_b = "/fixture/dotfiles-wt-two"
local decision_b = {
  root_dir = worktree_b,
  folders = { worktree_b .. "/src/a", worktree_b .. "/src/b" },
}
local buf_b = vim.api.nvim_create_buf(false, true)
vim.api.nvim_buf_set_name(buf_b, decision_b.folders[1] .. "/file.go")
decisions_by_buf[buf_b] = decision_b
local got_root_b
lsp.gopls_root_dir(buf_b, function(value) got_root_b = value end)
local config_b = { name = "gopls", root_dir = got_root_b, settings = vim.deepcopy(lsp.servers.gopls.settings) }
local params_b = { workspaceFolders = { { uri = vim.uri_from_fname(worktree_b), name = worktree_b } } }
lsp.gopls_before_init(params_b, config_b)
expect(got_root_b == worktree_b, "2 つ目の worktree は独立した root を使う")
expect(config_b.settings.gopls.env.GOWORK == "off", "2 つ目の client にも GOWORK=off を渡す")
same_list(vim.tbl_map(function(folder) return vim.uri_to_fname(folder.uri) end, params_b.workspaceFolders),
  decision_b.folders, "2 つ目の worktree に 1 つ目の folders を混ぜない")

-- client reuse は実効 folders で判定する。別 worktree の client を誤再利用しない。
decisions_by_buf[buf_a] = init_decision
lsp.gopls_root_dir(buf_a, function() end)
local client_a = {
  name = "gopls",
  config = config_a,
  workspace_folders = params_a.workspaceFolders,
  is_stopped = function() return false end,
}
expect(lsp.gopls_reuse_client(client_a, config_a), "同じ workspace folders の client は再利用する")

decisions_by_buf[buf_b] = decision_b
lsp.gopls_root_dir(buf_b, function() end)
expect(not lsp.gopls_reuse_client(client_a, config_b), "別 worktree の gopls client は再利用しない")

-- fallback root が toplevel になる module 外のファイルは、保存済み module folders の client を使わない。
lsp.gopls_default_root_dir = function(_, on_dir) on_dir(root) end
local outside_buf = vim.api.nvim_create_buf(false, true)
vim.api.nvim_buf_set_name(outside_buf, root .. "/scratch.go")
decisions_by_buf[outside_buf] = nil
local outside_root
lsp.gopls_root_dir(outside_buf, function(value) outside_root = value end)
local outside_config = { name = "gopls", root_dir = outside_root, settings = vim.deepcopy(lsp.servers.gopls.settings) }
expect(outside_root == root, "module 外の file は既定 toplevel root を保つ")
expect(not lsp.gopls_reuse_client(client_a, outside_config), "module 外の file は dotfiles client を再利用しない")
local outside_params = { workspaceFolders = { { uri = vim.uri_from_fname(root), name = root } } }
lsp.gopls_before_init(outside_params, outside_config)
expect(outside_config.settings.gopls.env == nil, "module 外 fallback client に GOWORK=off を足さない")

-- Neovim 0.12.0 の reuse_client_default は両 config に workspace folder が無ければ再利用する。
lsp.gopls_default_root_dir = function(_, on_dir) on_dir(nil) end
local rootless_a = vim.api.nvim_create_buf(false, true)
local rootless_b = vim.api.nvim_create_buf(false, true)
vim.api.nvim_buf_set_name(rootless_a, "/fixture/no-root/one.go")
vim.api.nvim_buf_set_name(rootless_b, "/fixture/no-root/two.go")
local saved_fs_root = vim.fs.root
vim.fs.root = function(bufnr, markers)
  if bufnr == rootless_a or bufnr == rootless_b then return nil end
  return saved_fs_root(bufnr, markers)
end
local root_a, root_b = "unset", "unset"
lsp.gopls_root_dir(rootless_a, function(value) root_a = value end)
lsp.gopls_root_dir(rootless_b, function(value) root_b = value end)
vim.fs.root = saved_fs_root
expect(root_a == nil and root_b == nil, "祖先 marker のない Go file 2 つは root_dir=nil")
local rootless_client = {
  name = "gopls",
  config = { name = "gopls" },
  workspace_folders = nil,
  is_stopped = function() return false end,
}
expect(lsp.gopls_reuse_client(rootless_client, { name = "gopls", root_dir = root_b }),
  "両 config に root/workspace folder が無い Go file は同じ client を再利用する")
local ordinary_root = "/fixture/ordinary-go"
local ordinary_folders = vim.lsp._get_workspace_folders(ordinary_root)
local ordinary_client = {
  name = "gopls",
  config = { name = "gopls", root_dir = ordinary_root },
  workspace_folders = ordinary_folders,
  is_stopped = function() return false end,
}
expect(lsp.gopls_reuse_client(ordinary_client, { name = "gopls", root_dir = ordinary_root }),
  "dotfiles 判定が無い通常 root は nvim 既定どおり同じ folder の client を再利用する")

local expected_mason_gopls = lsp.mason_bin() .. "/gopls"
expect(lsp.servers.gopls.cmd[1] == expected_mason_gopls, "nvim gopls cmd は Mason の絶対パス")
lsp.setup(nil)
expect(vim.lsp.config.gopls.cmd[1] == expected_mason_gopls, "setup 後の実効 gopls cmd も Mason の絶対パス")
local executable_checked
local available = lsp.server_binary_available("gopls", {
  executable = function(path)
    executable_checked = path
    return path == "gopls" and 1 or 0 -- PATH に shim だけある状態を表す
  end,
})
expect(executable_checked == expected_mason_gopls and not available,
  "enable_available が参照する共通述語は PATH shim でなく Mason 実体を確認する")

-- dotfiles workspace 判定が失敗しても、root_dir の既定値に戻り例外を外へ出さない。
lsp.gopls_default_root_dir = function(_, on_dir) on_dir("/fixture/default-root") end
lsp.gopls_workspace_for_buffer = function() error("fixture git failure") end
local fallback_root
local fallback_ok = pcall(function()
  lsp.gopls_root_dir(buf_a, function(value) fallback_root = value end)
end)
expect(fallback_ok and fallback_root == "/fixture/default-root", "失敗時は既定 root に戻る")
local fallback_config = { name = "gopls", root_dir = fallback_root, settings = vim.deepcopy(lsp.servers.gopls.settings) }
local fallback_folders = { { uri = vim.uri_from_fname(fallback_root), name = fallback_root } }
local fallback_params = { workspaceFolders = fallback_folders }
lsp.gopls_before_init(fallback_params, fallback_config)
expect(fallback_config.settings.gopls.env == nil, "fallback client に dotfiles 設定を足さない")
same_list(fallback_params.workspaceFolders, fallback_folders, "fallback の folders を維持")
expect(vim.deep_equal(lsp.servers.gopls.settings, saved_settings), "M.servers.gopls settings は不変")

restore()
vim.api.nvim_buf_delete(buf_a, { force = true })
vim.api.nvim_buf_delete(buf_b, { force = true })
vim.api.nvim_buf_delete(outside_buf, { force = true })
vim.api.nvim_buf_delete(rootless_a, { force = true })
vim.api.nvim_buf_delete(rootless_b, { force = true })
print("OK gopls workspace pure decision and client wiring")
