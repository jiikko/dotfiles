-- image_hover (dotfiles.image_hover) の headless 検証。test_image_hover.sh から dofile される。
-- 実プロセス (curl / qlmanage) は起動せず M._system を stub し、状態遷移だけ検査する:
--   1. URL 抽出: カーソル桁が画像 URL を覆うときだけ返す (markdown 括弧・クエリ付きを含む)
--   2. 画像ホバー → curl 起動 → 成功で qlmanage が 1 枚起動する
--   3. 別 URL へのホバー → 既存 qlmanage を kill してから新しい 1 枚 (1 window 契約)
--   4. 同じ URL の再ホバー → no-op (窓が点滅しない)
--   5. 画像以外のホバー → 表示中の窓に触らない
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local hover = require("dotfiles.image_hover")

-- 1. URL 抽出 -----------------------------------------------------------------
local line = "before [img](https://example.com/a.png?w=100) after http://x.jp/doc.html"
local s = line:find("https://") - 1
if hover.extract_image_url(line, s) ~= "https://example.com/a.png?w=100" then
  fail("画像 URL (クエリ付き・markdown 括弧内) を抽出できない")
end
if hover.extract_image_url(line, 0) ~= nil then
  fail("URL 外のカーソルで抽出された")
end
if hover.extract_image_url(line, line:find("doc%.html") - 1) ~= nil then
  fail("非画像 URL が画像扱いされた")
end
if hover.extract_image_url("https://example.com/b.JPG", 3) ~= "https://example.com/b.JPG" then
  fail("大文字拡張子が画像扱いされない")
end

-- stub: 起動されたコマンドを記録し、curl は成功を即コールバック、qlmanage は kill 可能な
-- 偽ハンドルを返す
local calls = {}
local alive = {}
hover._system = function(cmd, _, on_exit)
  table.insert(calls, cmd)
  if cmd[1] == "curl" then
    -- ダウンロード先ファイルを実際に作る (fs_stat のキャッシュ判定と手を繋ぐ)
    local f = assert(io.open(cmd[6], "w"))
    f:write("x")
    f:close()
    if on_exit then on_exit({ code = 0 }) end
    return nil
  end
  local handle = { closed = false }
  alive[handle] = true
  handle.is_closing = function(self) return self.closed end
  handle.kill = function(self) self.closed = true; alive[self] = nil end
  return handle
end

local function ql_calls()
  local n = 0
  for _, c in ipairs(calls) do
    if c[1] == "qlmanage" then n = n + 1 end
  end
  return n
end
local function alive_count()
  local n = 0
  for _ in pairs(alive) do n = n + 1 end
  return n
end

-- 2. 画像ホバー → qlmanage 1 枚 ------------------------------------------------
vim.api.nvim_buf_set_lines(0, 0, -1, false, {
  "one https://example.com/a.png end",
  "two https://example.com/b.png end",
  "plain text without url",
})
vim.api.nvim_win_set_cursor(0, { 1, 10 }) -- a.png の URL 上
hover._on_hover()
vim.wait(200, function() return ql_calls() >= 1 end)
if ql_calls() ~= 1 or alive_count() ~= 1 then
  fail(("画像ホバーで qlmanage が 1 枚起動しない (ql=%d alive=%d)"):format(ql_calls(), alive_count()))
end

-- 3. 別 URL → 既存を畳んで 1 枚のまま (1 window 契約) ---------------------------
vim.api.nvim_win_set_cursor(0, { 2, 10 }) -- b.png の URL 上
hover._on_hover()
vim.wait(200, function() return ql_calls() >= 2 end)
if ql_calls() ~= 2 then fail("別 URL へのホバーで qlmanage が置き換わらない") end
if alive_count() ~= 1 then
  fail(("窓が 1 枚に保たれていない (alive=%d)"):format(alive_count()))
end

-- 4. 同じ URL の再ホバー → no-op -----------------------------------------------
hover._on_hover()
vim.wait(100)
if ql_calls() ~= 2 then fail("同じ URL の再ホバーで窓が作り直された (点滅)") end

-- 5. 画像以外のホバー → 表示中の窓に触らない -------------------------------------
vim.api.nvim_win_set_cursor(0, { 3, 2 })
hover._on_hover()
vim.wait(100)
if ql_calls() ~= 2 or alive_count() ~= 1 then fail("画像以外のホバーで窓が閉じた/増えた") end

print("OK image_hover: extract/1-window/no-op/keep の全検査を通過")
