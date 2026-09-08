-- 参照検索 (<C-k>) の使用実績を記録する。
--
-- なぜ: issue 332 で Ruby のメソッドの参照検索を LSP (11.2s) から ripgrep (0.104s) へ
-- 振り分けた。rg は AST を見ないのでコメント・文字列・シンボルを拾う。その false positive で
-- **実際に困るのか**が分からないまま、呼び出し側索引のような重い仕組みを作りたくない
-- (issue 334 の段階 1)。困った回数を数えて、段階 2 に進むかを数字で決める。
--
-- 「困った」の観測点は **LSP 版へのフォールバック** (<leader>K)。rg で引いた直後に同じ語を
-- LSP で引き直したなら、rg の結果では足りなかったということ。時間と語が一致したときだけ
-- fallback として数える (別の語を引いただけなら、それは通常の LSP 利用)。
--
-- 記録は state ディレクトリの JSONL。repo には入れない (個人の操作ログなので)。
local M = {}

-- テストから差し替えられるようにフィールドで持つ。既定値の決定は初回アクセス時
-- (stdpath の評価を module ロード時にやらない)。
M.path = nil
M.fallback_window_ms = 30000
-- ログの上限。超えたら古い半分を捨てる。段階 1 の判断が出たら :DotfilesRefsReset で消す前提だが、
-- 忘れても際限なく溜まらないようにする (中身は「押した語」= 仕事の repo のクラス名・メソッド名)。
M.max_bytes = 512 * 1024
M.now = function()
  return vim.uv.now()
end

--: -> string
function M.log_path()
  if M.path then return M.path end
  M.path = vim.fn.stdpath("state") .. "/dotfiles/refs_usage.jsonl"
  return M.path
end

-- 直前の ripgrep 検索 (語と時刻)。fallback 判定にだけ使う
local last_ripgrep = nil

-- 記録は「あれば嬉しい」もので、失敗しても <C-k> を壊してはいけない。
-- 書き込めない環境 (read-only な state ディレクトリ等) でも黙って諦める。
--: (table) -> boolean
-- 上限を超えていたら古い半分を捨てる。書き込みのたびに全行を読まないよう、判定は fs_stat の
-- サイズだけで行う (超えたときだけ読み直す)。
local function rotate_if_needed(path)
  local st = vim.uv.fs_stat(path)
  if not st or st.size <= M.max_bytes then return end
  local lines = vim.fn.readfile(path)
  local keep = {}
  for i = math.floor(#lines / 2) + 1, #lines do
    table.insert(keep, lines[i])
  end
  vim.fn.writefile(keep, path)
end

local function append(entry)
  local ok = pcall(function()
    local path = M.log_path()
    vim.fn.mkdir(vim.fs.dirname(path), "p")
    vim.fn.writefile({ vim.json.encode(entry) }, path, "a")
    rotate_if_needed(path)
  end)
  return ok
end

-- kind: "ripgrep" (rg 経路) / "lsp" (LSP 経路) / "fallback" (rg の直後に LSP で引き直した)
-- 🚨 filetype を必ず一緒に残す。issue 334 は「LSP 経路の回数 = 定数を引いた回数」で sidecar 化を
--    判断すると決めているが、LSP 経路には **Ruby 以外の全 filetype の <C-k>** も入る
--    (use_ripgrep_references が false を返すため)。ft を落とすと後から分離できず、判断が
--    「Ruby の定数で困っている」とは無関係な数字の上に乗る (敵対レビュー P1-1)。
--: (string, string, string?) -> boolean
function M.record(kind, word, filetype)
  local now = M.now()
  if kind == "ripgrep" then
    last_ripgrep = { word = word, at = now }
  elseif kind == "lsp" and last_ripgrep
    and last_ripgrep.word == word
    and (now - last_ripgrep.at) <= M.fallback_window_ms then
    kind = "fallback"
    last_ripgrep = nil
  end
  return append({
    kind = kind,
    word = word,
    ft = (filetype ~= nil and filetype ~= "") and filetype or nil,
    at = os.date("%Y-%m-%dT%H:%M:%S%z"), -- オフセット付き (マシンを跨いでも並べ替えられる)
  })
end

-- 集計。全体と、filetype 別 (判断に使うのは Ruby の行だけ) を返す。
-- fallback 率の分母は rg を使った回数 (fallback は「rg を引いた後に引き直した」ものなので、
-- rg の回数に対する割合が「rg で足りなかった率」になる)。
local function new_bucket()
  return { ripgrep = 0, lsp = 0, fallback = 0 }
end

local function with_rate(b)
  b.fallback_rate = b.ripgrep > 0 and (b.fallback / b.ripgrep) or 0
  return b
end

--: -> table
function M.stats()
  local total = new_bucket()
  local by_ft = {}
  local no_ft = 0 -- ft を記録していなかった頃の行 (層別に使えない)
  local ok, lines = pcall(vim.fn.readfile, M.log_path())
  if ok then
    for _, line in ipairs(lines) do
      local decoded, entry = pcall(vim.json.decode, line)
      if decoded and type(entry) == "table" and total[entry.kind] ~= nil then
        total[entry.kind] = total[entry.kind] + 1
        if type(entry.ft) == "string" and entry.ft ~= "" then
          by_ft[entry.ft] = by_ft[entry.ft] or new_bucket()
          by_ft[entry.ft][entry.kind] = by_ft[entry.ft][entry.kind] + 1
        else
          no_ft = no_ft + 1
        end
      end
    end
  end
  for _, b in pairs(by_ft) do with_rate(b) end
  local out = with_rate(total)
  out.by_ft = by_ft
  out.no_ft = no_ft
  return out
end

--: -> string
function M.format_stats()
  local c = M.stats()
  -- 判断に使うのは Ruby の行 (issue 334)。全体は参考値で、Ruby 以外の <C-k> も入っている
  local ruby = c.by_ft.ruby or new_bucket()
  local eruby = c.by_ft.eruby
  if eruby then
    for _, k in ipairs({ "ripgrep", "lsp", "fallback" }) do ruby[k] = ruby[k] + eruby[k] end
  end
  with_rate(ruby)
  local lines = {
    ("Ruby: ripgrep %d / LSP (定数など) %d / rg の直後に LSP へ引き直し %d (%.1f%%)  ← 判断に使う数字"):format(
      ruby.ripgrep, ruby.lsp, ruby.fallback, ruby.fallback_rate * 100),
    ("全体: ripgrep %d / LSP %d / 引き直し %d  (Ruby 以外の <C-k> も含む)"):format(
      c.ripgrep, c.lsp, c.fallback),
  }
  if c.no_ft > 0 then
    table.insert(lines, ("ft 未記録 %d 件 (層別に使えない古い行)"):format(c.no_ft))
  end
  return table.concat(lines, "\n")
end

--: -> void
function M.setup()
  vim.api.nvim_create_user_command("DotfilesRefsStats", function()
    vim.notify(M.format_stats())
  end, { desc = "参照検索 (<C-k>) の使用実績 (issue 334 段階 1)" })
  -- 段階 1 の判断が出たら消すためのもの。ログには押した語 (仕事の repo の識別子) が平文で残る
  vim.api.nvim_create_user_command("DotfilesRefsReset", function()
    local path = M.log_path()
    local ok = pcall(vim.fn.delete, path)
    vim.notify(ok and ("参照検索の記録を削除した: " .. path) or ("削除できなかった: " .. path))
  end, { desc = "参照検索の使用実績ログを削除する (issue 334 段階 1)" })
end

return M
