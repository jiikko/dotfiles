-- gf のキーマップ (nvim/lua/dotfiles/basic.lua の set_keymaps) の headless 検証。
-- 守っている不変条件:
--
--   1. **normal の gf が行番号まで飛ぶ** (既定の gF の挙動)。マッピングが消えても既定の gf が
--      ファイルを開くところは同じなので、「開けたか」だけを見る検査は退行を素通りする。
--      カーソルの行番号まで見ること。
--   2. **visual の gf も、選択したファイル名の直後の行番号まで飛ぶ**。n だけに張る形へ戻すと
--      既定の v_gf が走り、ファイルは開くが 1 行目に居る (= 1 と同じ理由で行番号を見る)。
--
-- 🚨 マッピングを通す経路で叩くこと。`normal!` はマッピングを無視するので、gf を
--    どう張っていても既定の gf が走り、有無で結果の変わらない観測になる。
-- 🚨 rtp は cwd 依存 (basic_checktime_check.lua のヘッダ参照)。ランナーが repo root へ cd する。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
local target = tmp .. "/target.txt"
vim.fn.writefile({ "l1", "l2", "l3", "l4", "l5" }, target)

-- 起点となる scratch バッファ (1 行目に "<target>:<lnum> rest" を置く)
local function scratch(line)
  vim.cmd("enew")
  vim.bo.buftype = "nofile"
  -- ケース間で状態を共有しない: 前のケースが開いた target のバッファごと捨てる
  -- (カーソル位置が残ると、行番号を動かさない退行が「前の期待値」で緑になりうる)
  local prev = vim.fn.bufnr(target)
  if prev ~= -1 then vim.cmd("bwipeout! " .. prev) end
  vim.api.nvim_buf_set_lines(0, 0, -1, false, { line })
  vim.api.nvim_win_set_cursor(0, { 1, 0 })
  -- canary: 起点が目的のファイルだと、下の検査が自明に通る
  if vim.fn.expand("%:t") == "target.txt" then
    fail("起点のバッファが既に target.txt。ハーネスの失敗であって合格ではない")
  end
end

local function jumped(keys, want_lnum, label)
  local ok, err = pcall(vim.cmd, "normal " .. keys)
  if not ok then
    fail(("%s: gf でファイルを開けなかった: %s"):format(label, tostring(err):gsub("%s+", " ")))
  end
  if vim.fn.expand("%:t") ~= "target.txt" then
    fail(("%s: gf が target.txt を開いていない (%q)"):format(label, vim.fn.expand("%:p")))
  end
  local lnum = vim.api.nvim_win_get_cursor(0)[1]
  if lnum ~= want_lnum then
    fail(("%s: gf の後のカーソルが %d 行目 (期待 %d)。行番号まで飛んでいない"):format(label, lnum, want_lnum))
  end
end

-- 1. normal: カーソル下の "<target>:3" で 3 行目へ
scratch(target .. ":3")
jumped("gf", 3, "normal")

-- 2. visual: ファイル名だけを選択し、その直後の ":4" で 4 行目へ
--    (選択範囲にファイル名だけを入れる = v_gf / v_gF 共通の作法。":4" まで選ぶと
--     どちらの挙動でも E447 になり、この検査が何も主張しなくなる)
scratch(target .. ":4 rest")
jumped(("0v%dlgf"):format(#target - 1), 4, "visual")

vim.fn.delete(tmp, "rf")
print("OK gf line jump: normal は file:3 で 3 行目 / visual は選択直後の :4 で 4 行目")
