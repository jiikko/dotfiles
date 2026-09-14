-- CursorHold の checktime (nvim/lua/dotfiles/basic.lua の M.can_checktime と、set_autocmds が
-- 張る FocusGained/BufEnter/CursorHold/CursorHoldI) の headless 検証。
-- 守っている不変条件:
--
--   1. **cmdwin (q: / q/) の中では checktime を打たない**。cmdwin では `mode()` が "n" を
--      返すのでモード判定だけのガードは素通りし、checktime が E11 で落ちる。CursorHold は
--      updatetime (500ms) ごとに来るので、q: を開いて放置するとエラーが出続ける。
--   2. **cmdwin の外では従来どおり打つ**。ガードを固くして、ファイル変更の検出そのものを
--      殺さないこと (checktime が来なくなると、外部で書き換えられたバッファが黙って古くなる)。
--   3. **判定が autocmd の経路から効く**。M.can_checktime が正しくても、autocmd が
--      それを通らなければ 1 mm も効かない。cmdwin の中で実際に CursorHold を発火させて見る。
--
-- 🚨 このテストは `-u <repo>/_nviminit.lua` の rtp が **cwd 依存**であることに乗っている。
--    `-u` で起動した nvim は $MYVIMRC を設定しないので、_nviminit.lua の
--    `fnamemodify(resolve(vim.env.MYVIMRC or ""), ":p:h")` は cwd を返す (実測 2026-09-14)。
--    worktree の cwd で走らせれば worktree の lua が読まれる。テストランナーはそうしている。
local function fail(msg)
  io.stderr:write("FAIL: " .. msg .. "\n")
  os.exit(1)
end

local basic = require("dotfiles.basic")

if type(basic.can_checktime) ~= "function" then
  fail("M.can_checktime が無い。cmdwin のガードが消えている")
end

-- 2. cmdwin の外では打つ
if basic.can_checktime() ~= true then
  fail(("cmdwin の外で can_checktime() = %s。ファイル変更の検出が死ぬ"):format(tostring(basic.can_checktime())))
end

-- 1 と 3. cmdwin の中: 判定が false で、CursorHold を発火させてもエラーが出ない
local seen_type, guarded, fired_ok, fired_err
vim.api.nvim_create_autocmd("CmdwinEnter", {
  once = true,
  callback = function()
    seen_type = vim.fn.getcmdwintype()
    guarded = basic.can_checktime()
    -- production と同じ経路 (autocmd) を通す。ガードが無いと basic.lua の callback が
    -- checktime を打ち、E11 がここまで上がってくる
    fired_ok, fired_err = pcall(vim.cmd, "doautocmd CursorHold")
    vim.cmd("quit")
  end,
})
vim.fn.feedkeys("q:", "x")

-- canary: cmdwin に入れていないなら、下の 2 つは「検査していない」だけ (緑に畳まない)
if seen_type ~= ":" then
  fail(("cmdwin に入れていない (getcmdwintype=%q)。ハーネスの失敗であって合格ではない"):format(tostring(seen_type)))
end
if guarded ~= false then
  fail("cmdwin の中で can_checktime() が true。mode() は cmdwin でも \"n\" を返す")
end
if not fired_ok then
  fail(("cmdwin で CursorHold を発火させたらエラー: %s"):format(tostring(fired_err):gsub("%s+", " ")))
end

-- cmdwin を開くと headless でも ":" が stdout へ出るので、OK を行頭に置くために改行を挟む
-- (check_log.sh は `^OK` でアンカーする)
print("\nOK basic checktime: cmdwin 外で打つ / cmdwin 内で打たない / autocmd 経路で E11 が出ない")
