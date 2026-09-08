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
local function append(entry)
  local ok = pcall(function()
    local path = M.log_path()
    vim.fn.mkdir(vim.fs.dirname(path), "p")
    vim.fn.writefile({ vim.json.encode(entry) }, path, "a")
  end)
  return ok
end

-- kind: "ripgrep" (rg 経路) / "lsp" (LSP 経路) / "fallback" (rg の直後に LSP で引き直した)
--: (string, string) -> boolean
function M.record(kind, word)
  local now = M.now()
  if kind == "ripgrep" then
    last_ripgrep = { word = word, at = now }
  elseif kind == "lsp" and last_ripgrep
    and last_ripgrep.word == word
    and (now - last_ripgrep.at) <= M.fallback_window_ms then
    kind = "fallback"
    last_ripgrep = nil
  end
  return append({ kind = kind, word = word, at = os.date("%Y-%m-%dT%H:%M:%S") })
end

-- 集計。件数と、fallback 率 (= rg の結果で足りなかった割合) を返す。
--: -> table
function M.stats()
  local counts = { ripgrep = 0, lsp = 0, fallback = 0 }
  local ok, lines = pcall(vim.fn.readfile, M.log_path())
  if ok then
    for _, line in ipairs(lines) do
      local decoded, entry = pcall(vim.json.decode, line)
      if decoded and type(entry) == "table" and counts[entry.kind] ~= nil then
        counts[entry.kind] = counts[entry.kind] + 1
      end
    end
  end
  -- 分母は rg を使った回数。fallback は「rg を引いた後に引き直した」ものなので、
  -- rg の回数に対する割合が「rg で足りなかった率」になる
  counts.fallback_rate = counts.ripgrep > 0 and (counts.fallback / counts.ripgrep) or 0
  return counts
end

--: -> string
function M.format_stats()
  local c = M.stats()
  return ("参照検索: ripgrep %d 回 / LSP %d 回 / rg の直後に LSP へ引き直し %d 回 (%.1f%%)"):format(
    c.ripgrep, c.lsp, c.fallback, c.fallback_rate * 100)
end

--: -> void
function M.setup()
  vim.api.nvim_create_user_command("DotfilesRefsStats", function()
    vim.notify(M.format_stats())
  end, { desc = "参照検索 (<C-k>) の使用実績 (issue 334 段階 1)" })
end

return M
