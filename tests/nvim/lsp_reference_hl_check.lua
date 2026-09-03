-- LSP 参照ハイライト (lsp.lua の CursorHold → documentHighlight) の視認性の headless 検証。
-- test_lsp_reference_hl.sh から dofile される。守っている不変条件:
--
--   1. nvim が documentHighlight の 3 kind に使う highlight group 名 (LspReferenceText /
--      Read / Write) が変わっていない。上流が改名したら _nviminit.lua の明示定義が無言で
--      効かなくなる (link 既定へ戻る) ため、実際に buf_highlight_references を呼んで
--      extmark に乗った group 名を pin する。LSP サーバは要らない。
--   2. LspReference 一族 (Target = hover 範囲を含む) が Visual と同じ地色に解決されない。
--      nvim 既定では Visual に link しており、Visual を上書きしている本構成では漏れる。
--   3. 実際に読める配色であること。地色を置く group は「その地 × 前景色」、地色を置かず
--      前景だけ変える group (gruvbox の Read/Write) は「Normal 地 × その前景」で
--      コントラスト 3.0:1 以上。「Visual と違う色」だけでは別の低コントラスト色への
--      差し替えを止められない。Kraft 漏れ時の実測は 1.1〜1.8:1、dark1 地では 3.2〜8.5:1。
--
-- cterm(256色) / gui(truecolor) のどちらを検査できるかは colorscheme 分岐に依る
-- (gruvbox は cterm 色を持たない)。呼び出し側が SUPPORT_TRUECOLOR を両方で回すので、
-- ここでは「検査できた側が 1 つ以上あること」を要求し、検査した側を出力に出す
-- (skip を沈黙させない)。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

-- Go コードで documentHighlight の範囲に入りうる前景色。colorscheme から動的に読む
-- (色番号をここへ写すと colorscheme 変更で無言に古くなる)。
local FG_GROUPS = { "Normal", "Keyword", "String", "Function", "Identifier", "Type", "Comment" }
local MIN_CONTRAST = 3.0

-- 色を検査する group。Text/Read/Write は下の extmark 経路で名前を pin できるが、Target は
-- hover (vim.lsp.buf.hover) 側で使われるため名前の pin は doc (hl-LspReferenceTarget) が根拠。
--
-- ⚠️ **「LspReference 一族」ではなく「Visual を link 経由で引きうる地塗り group」を検査する**
-- (issue 134)。ee5e2b7 は LspReference だけを直したが、同じ根 (nvim 既定の Visual への link +
-- 本構成の Visual 上書き) を持つ group が他にもあり、名前で pin していたので検出できなかった。
-- 実測 2026-09-03: SnippetTabstop は両分岐で、LspSignatureActiveParameter は 256色分岐で
-- `link=Visual` のまま Kraft (180) を引いていた (最悪 1.10:1)。
-- **新しい group を足すときは、その group が「Visual へ link する既定を持つか」を
-- `nvim_get_hl(0, {name=..., link=true})` で確かめてから足すこと** (推測で並べない)。
local COLOR_GROUPS = {
  "LspReferenceText", "LspReferenceRead", "LspReferenceWrite", "LspReferenceTarget",
  "SnippetTabstop",              -- vim.snippet がバッファ内の tabstop に塗る
  "LspSignatureActiveParameter", -- blink.cmp のシグネチャヘルプの現在引数
}

-- REVERSE_OK は「reverse で前景色を殺さない」方式を採った group。reverse は前景と背景を
-- 入れ替えるので**地色を持たない**が、その場合コントラストは Normal と同じで基準を満たす。
-- 地色が無いことを「検査漏れ」ではなく「意図した方式」として扱うために名前で持つ。
-- ⚠️ ここに足すのは reverse を**明示定義した**group だけ。link 先が偶然 reverse を持つ形
-- (retrobox の Search 等) を許すと、link が変わったときに無言で検査が緩む。
local REVERSE_OK = { LspSignatureActiveParameter = true }

-- xterm-256 の色番号 → RGB。termguicolors=off では端末が使うのはこちらなので、
-- gui の hex ではなくこの近似値でコントラストを測る必要がある。
local CUBE = { 0, 95, 135, 175, 215, 255 }
local function cterm_to_rgb(n)
  if n >= 16 and n <= 231 then
    local i = n - 16
    return CUBE[math.floor(i / 36) + 1], CUBE[math.floor(i / 6) % 6 + 1], CUBE[i % 6 + 1]
  end
  if n >= 232 and n <= 255 then
    local v = 8 + 10 * (n - 232)
    return v, v, v
  end
  -- 0-15 はパレット依存 (端末プロファイルで変わる) ため測れない
  return nil
end

local function gui_to_rgb(n)
  return math.floor(n / 65536) % 256, math.floor(n / 256) % 256, n % 256
end

local function relative_luminance(r, g, b)
  local function ch(c)
    c = c / 255
    return c <= 0.03928 and c / 12.92 or ((c + 0.055) / 1.055) ^ 2.4
  end
  return 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b)
end

local function contrast(fg, bg)
  local l1, l2 = relative_luminance(unpack(fg)), relative_luminance(unpack(bg))
  local hi, lo = math.max(l1, l2), math.min(l1, l2)
  return (hi + 0.05) / (lo + 0.05)
end

-- ---------------------------------------------------------------------------
-- 1. nvim が使う group 名を実際の描画経路から採取する
-- ---------------------------------------------------------------------------
local buf = vim.api.nvim_create_buf(false, true)
vim.api.nvim_buf_set_lines(buf, 0, -1, false, { "func aaaa", "read aaaa", "write aaa" })

local kinds = { Text = 1, Read = 2, Write = 3 }
local refs = {}
for i, kind in ipairs({ kinds.Text, kinds.Read, kinds.Write }) do
  refs[i] = {
    kind = kind,
    range = {
      start = { line = i - 1, character = 0 },
      ["end"] = { line = i - 1, character = 4 },
    },
  }
end
vim.lsp.util.buf_highlight_references(buf, refs, "utf-16")

local ns = vim.api.nvim_get_namespaces()["nvim.lsp.references"]
if not ns then
  fail("namespace 'nvim.lsp.references' が無い (nvim 上流が改名した可能性。_nviminit.lua の LspReferenceText 定義が届く先を再確認する)")
end
local marks = vim.api.nvim_buf_get_extmarks(buf, ns, 0, -1, { details = true })
if #marks ~= 3 then
  fail(("documentHighlight の extmark が 3 件でない (%d 件)。buf_highlight_references の契約が変わった"):format(#marks))
end

local seen = {}
for _, m in ipairs(marks) do
  local g = m[4] and m[4].hl_group
  if not g then
    fail("extmark に hl_group が無い")
  end
  seen[g] = true
end
for _, want in ipairs({ "LspReferenceText", "LspReferenceRead", "LspReferenceWrite" }) do
  if not seen[want] then
    local got = {}
    for g in pairs(seen) do got[#got + 1] = g end
    table.sort(got)
    fail(("%s が使われていない (実際: %s)。上流の group 名が変わると _nviminit.lua の明示定義が届かず Visual への link 既定に戻る"):format(want, table.concat(got, ", ")))
  end
end

-- ---------------------------------------------------------------------------
-- 2/3. 採取した group の解決色を検査する
-- ---------------------------------------------------------------------------
local visual = vim.api.nvim_get_hl(0, { name = "Visual", link = false })
local checked = { cterm = 0, gui = 0 }

local normal = vim.api.nvim_get_hl(0, { name = "Normal", link = false })

for _, group in ipairs(COLOR_GROUPS) do
  local hl = vim.api.nvim_get_hl(0, { name = group, link = false })

  -- reverse を採った group は地色を持たないのが正しい。**reverse が消えていたら落とす**
  -- (地色も reverse も無い = 何も塗らない状態に退行しても、下の地色検査は素通りする)
  if REVERSE_OK[group] then
    if not hl.reverse then
      fail(("%s は reverse で定義しているはずだが reverse が無い (link が復活したか定義が消えた)"):format(group))
    end
    if hl.bg ~= nil or hl.ctermbg ~= nil then
      fail(("%s は reverse なのに地色を持っている (方式が混ざっている)"):format(group))
    end
  end

  -- Visual 上書きの漏れ (nvim 既定の LspReferenceText → Visual link) の回帰検出
  if hl.ctermbg ~= nil and hl.ctermbg == visual.ctermbg then
    fail(("%s の ctermbg が Visual と同じ (%s)。Visual への link が復活したか、同じ色を直接指定している"):format(group, tostring(hl.ctermbg)))
  end
  if hl.bg ~= nil and hl.bg == visual.bg then
    fail(("%s の bg が Visual と同じ (#%06x)。Visual への link が復活したか、同じ色を直接指定している"):format(group, hl.bg))
  end

  for _, mode in ipairs({ "cterm", "gui" }) do
    -- ⚠️ `kind == "bg" and attrs.bg or attrs.fg` の 3 項イディオムで書かないこと: bg が nil の
    -- とき fg へフォールスルーし、「地色なし」の group を「地色あり」として測ってしまう。
    local function rgb_of(attrs, kind)
      local n, to_rgb
      if mode == "cterm" then
        to_rgb = cterm_to_rgb
        if kind == "bg" then n = attrs.ctermbg else n = attrs.ctermfg end
      else
        to_rgb = gui_to_rgb
        if kind == "bg" then n = attrs.bg else n = attrs.fg end
      end
      if n == nil then return nil end
      local r, g, b = to_rgb(n)
      if r == nil then return nil end
      return { r, g, b }
    end

    local bg_rgb = rgb_of(hl, "bg")
    if bg_rgb and bg_rgb[1] then
      -- 地色を置く group: その地の上に Go コードの前景色が乗る
      for _, fg_group in ipairs(FG_GROUPS) do
        local fg_rgb = rgb_of(vim.api.nvim_get_hl(0, { name = fg_group, link = false }), "fg")
        if fg_rgb and fg_rgb[1] then
          local ratio = contrast(fg_rgb, bg_rgb)
          if ratio < MIN_CONTRAST then
            fail(("%s (%s) 地に %s の前景が乗るとコントラスト %.2f:1 < %.1f:1"):format(
              group, mode, fg_group, ratio, MIN_CONTRAST))
          end
          checked[mode] = checked[mode] + 1
        end
      end
    else
      -- 地色を置かない group (前景だけ差し替える方式): Normal 地に対して測る。
      -- 前景も指定が無ければ元の色がそのまま残る = 視認性は変わらないので検査対象外。
      local fg_rgb = rgb_of(hl, "fg")
      local nbg_rgb = rgb_of(normal, "bg")
      if fg_rgb and fg_rgb[1] and nbg_rgb and nbg_rgb[1] then
        local ratio = contrast(fg_rgb, nbg_rgb)
        if ratio < MIN_CONTRAST then
          fail(("%s (%s) の前景が Normal 地に乗るとコントラスト %.2f:1 < %.1f:1"):format(
            group, mode, ratio, MIN_CONTRAST))
        end
        checked[mode] = checked[mode] + 1
      end
    end
  end
end

-- 「測れなかった」を緑に畳まない (adversarial-review-own-safeguards 節 2)
if checked.cterm == 0 and checked.gui == 0 then
  fail("cterm も gui も 1 件もコントラストを測れなかった (colorscheme が前景色を持たない?)")
end

print(("OK lsp reference highlight: contrast >= %.1f:1 (cterm %d 件 / gui %d 件, colorscheme=%s termguicolors=%s)"):format(
  MIN_CONTRAST, checked.cterm, checked.gui, tostring(vim.g.colors_name), tostring(vim.o.termguicolors)))
