-- カーソルホバー (CursorHold) した先が画像 URL なら Quick Look でプレビューする (macOS 専用)。
--
-- なぜ Quick Look か: Terminal.app は端末グラフィックスプロトコル (kitty graphics / sixel /
-- iTerm2 inline images) を一切持たないため、nvim バッファ内へのインライン表示 (image.nvim 等)
-- は構造的に不可能。端末の外 (Quick Look 窓) に出すのが唯一の原寸プレビュー経路。
--
-- 1 window 契約 (ユーザー要望 2026-08-09: 「window が散乱して欲しくない」):
--   - プレビュー窓は常に 1 枚だけ。別の画像 URL をホバーしたら既存の qlmanage を kill して
--     新しい 1 枚に置き換える (qlmanage に表示中ファイルを差し替える API は無いので
--     kill + respawn が「使い回し」の実装になる)
--   - 同じ URL の再ホバーは no-op (窓が点滅しない)
--   - nvim 終了時 (VimLeavePre) に窓も畳む
--
-- ダウンロードは非同期 (curl)。URL の sha256 でキャッシュし、同じ URL の再表示は再取得しない。
-- 失敗 (非画像・404・タイムアウト) は無音でスキップする (ホバーは常時発火するイベントなので、
-- 失敗のたびに通知を出すとノイズになる)。
local M = {}

-- 画像として扱う拡張子 (URL の path 部末尾で判定。クエリ/フラグメントは無視)
local IMAGE_EXT = { png = true, jpg = true, jpeg = true, gif = true, webp = true, bmp = true, heic = true }

local CURL_TIMEOUT_SEC = "10"

-- 状態 (1 window 契約の正本): url = 表示中 (取得中) の URL / ql = qlmanage のプロセスハンドル
local state = { url = nil, ql = nil }

-- テスト差し替え点: プロセス起動 (vim.system 互換) と「macOS か」
M._system = vim.system
M._is_mac = function() return vim.uv.os_uname().sysname == "Darwin" end

-- s..e (1-origin) がカーソル桁 col0 (0-origin) を覆っているか
local function covers(s, e, col0)
  return col0 + 1 >= s and col0 + 1 <= e
end

-- 画像拡張子か (クエリ/フラグメントを落としてから path 末尾で判定)
local function is_image_path(path)
  local ext = path:gsub("[?#].*$", ""):match("%.([%w]+)$")
  return ext ~= nil and IMAGE_EXT[ext:lower()]
end

-- カーソル下の画像 URL を返す (無ければ nil)。行内の URL 全マッチから「カーソル桁を
-- 覆っているもの」を選ぶ: <cWORD> だと markdown の "](http://…)" 等で URL 境界とズレる。
function M.extract_image_url(line, col0)
  local init = 1
  while true do
    local s, e = line:find("https?://[^%s)>\"'`%]]+", init)
    if not s then return nil end
    if covers(s, e, col0) then
      local url = line:sub(s, e)
      if is_image_path(url) then return url end
      return nil -- カーソルは URL 上だが画像ではない
    end
    init = e + 1
  end
end

-- カーソル下のローカル画像パスを返す (無ければ nil)。~ / 絶対 / 相対 (バッファの
-- ディレクトリ基準 base_dir で解決) を受け、実在チェックは呼び出し側が行う。
-- トークン境界は「空白・引用符・括弧類の外側」(markdown の ![](path) や "path" を想定)。
function M.extract_image_file(line, col0, base_dir)
  local init = 1
  while true do
    local s, e = line:find("[^%s\"'`()%[%]<>]+", init)
    if not s then return nil end
    if covers(s, e, col0) then
      local tok = line:sub(s, e)
      if not is_image_path(tok) or tok:match("^https?://") then return nil end
      local path = tok:gsub("[?#].*$", "")
      if path:sub(1, 1) == "~" then
        path = vim.fn.expand(path)
      elseif path:sub(1, 1) ~= "/" then
        path = (base_dir or ".") .. "/" .. path
      end
      return path
    end
    init = e + 1
  end
end

local function cache_path(url)
  local path = url:gsub("[?#].*$", "")
  local ext = path:match("%.([%w]+)$") or "img"
  local dir = vim.fn.stdpath("cache") .. "/image-hover"
  vim.fn.mkdir(dir, "p")
  return dir .. "/" .. vim.fn.sha256(url) .. "." .. ext
end

-- 既存のプレビュー窓を畳む (qlmanage を殺すと窓ごと閉じる)
local function close_preview()
  if state.ql and not state.ql:is_closing() then
    state.ql:kill(15)
  end
  state.ql = nil
end

-- 窓位置の保存/復元 (best-effort。ユーザー要望 2026-08-09: 開き直しで位置を変えない)。
-- qlmanage には表示差し替えも位置指定も無いため、kill 前に System Events で座標を読み、
-- respawn 後に書き戻す。ターミナルにアクセシビリティ権限が無い環境では osascript が
-- 失敗するだけで、プレビュー自体は従来どおり成立する (無音でスキップ)。
-- 🚨 再試行ループは張らない (rules/no-osascript-…: 2 回で打ち切る)。respawn 直後は窓が
-- まだ無いので 400ms / 900ms の 2 点だけ試す
local OSA_GET_POS = 'tell application "System Events" to tell process "qlmanage" to get {position, size} of front window'
local function osa_set_pos(x, y)
  return ('tell application "System Events" to tell process "qlmanage" to set position of front window to {%d, %d}'):format(x, y)
end

local function capture_position(cb)
  if not (state.ql and not state.ql:is_closing()) then return cb(nil) end
  M._system(
    { "osascript", "-e", OSA_GET_POS },
    { text = true },
    vim.schedule_wrap(function(res)
      if res.code ~= 0 then return cb(nil) end
      -- 出力は "x, y, w, h" 形式
      local x, y = tostring(res.stdout or ""):match("^%s*(-?%d+),%s*(-?%d+)")
      cb(x and { x = tonumber(x), y = tonumber(y) } or nil)
    end)
  )
end

local function restore_position(pos)
  if not pos then return end
  for _, delay in ipairs({ 400, 900 }) do
    vim.defer_fn(function()
      if state.ql and not state.ql:is_closing() then
        M._system({ "osascript", "-e", osa_set_pos(pos.x, pos.y) }, { stdout = false, stderr = false })
      end
    end, delay)
  end
end

-- Quick Look で file を表示する (1 window 契約: 既存の窓を畳んでから 1 枚だけ出す)。
-- qlmanage はフォーカスを奪うので、Terminal.app のときは奪い返す (タイピングの流れを
-- 切らない。osascript は activate 程度の軽い補助のみ許容 = rules/no-osascript-… の例外枠)
local function show_preview(file)
  capture_position(function(pos)
    close_preview()
    state.ql = M._system({ "qlmanage", "-p", file }, { stdout = false, stderr = false })
    restore_position(pos)
    if vim.env.TERM_PROGRAM == "Apple_Terminal" then
      vim.defer_fn(function()
        M._system({ "osascript", "-e", 'tell application "Terminal" to activate' }, { stdout = false, stderr = false })
      end, 400)
    end
  end)
end

local function on_hover()
  local line = vim.api.nvim_get_current_line()
  local col0 = vim.api.nvim_win_get_cursor(0)[2]
  local url = M.extract_image_url(line, col0)
  if not url then
    -- ローカル画像パス (~ / 絶対 / バッファ基準の相対)。実在するときだけ扱う
    local file = M.extract_image_file(line, col0, vim.fn.expand("%:p:h"))
    if not (file and vim.uv.fs_stat(file)) then
      return -- 画像以外のホバーでは表示中の窓に触らない (閉じない)
    end
    if state.url == file and state.ql and not state.ql:is_closing() then return end
    state.url = file
    show_preview(file)
    return
  end
  -- 同じ URL の再ホバーは no-op (窓の点滅防止)。窓を手で閉じた後 (プロセス終了済み) は出し直す
  if state.url == url and state.ql and not state.ql:is_closing() then return end
  state.url = url
  local file = cache_path(url)
  if vim.uv.fs_stat(file) then
    show_preview(file)
    return
  end
  M._system(
    { "curl", "-fsSL", "--max-time", CURL_TIMEOUT_SEC, "-o", file, url },
    { stdout = false, stderr = false },
    vim.schedule_wrap(function(res)
      -- 取得中に別 URL へホバーが移っていたら捨てる (後勝ち。古い画像で上書きしない)
      if res.code ~= 0 or state.url ~= url then return end
      show_preview(file)
    end)
  )
end

function M.setup()
  if not M._is_mac() then return end
  local group = vim.api.nvim_create_augroup("dotfiles_image_hover", { clear = true })
  -- CursorHold は updatetime (basic.lua で 500ms) 停止で 1 回だけ発火する
  vim.api.nvim_create_autocmd("CursorHold", { group = group, callback = on_hover })
  vim.api.nvim_create_autocmd("VimLeavePre", { group = group, callback = close_preview })
end

-- テスト用: 内部状態の観測 (プレビュー窓の実体はプロセスなので、テストは状態遷移だけ検査する)
function M._state() return state end
function M._on_hover() return on_hover() end

return M
