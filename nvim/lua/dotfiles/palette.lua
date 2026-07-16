-- カスタム色の hex↔cterm ペアの唯一の出典 (テーマカラーを変えるときの入口。変更手順・
-- tmux 側との対応表は docs/theme-colors.md)。
--
-- 主環境は termguicolors=off の 256色運用 (規律の一次情報は dotfiles/hl.lua) で、gui 色 (hex)
-- が設計の真・cterm はその忠実な近似。各所への手書きコピペで drift した実例があるため、ここへ一元化している。
--
-- 3 節構成 (色を足すときは意味の合う節へ):
--   トップレベル = gruvbox 公式パレット (名前も公式呼称。テーマの基調色)
--   accent      = tmux と意味を共有するツール横断アクセント (gruvbox 外。tmux 側定数と対で変える)
--   diag        = 診断サイン (coc 時代踏襲の非 gruvbox 色。cterm 完全一致で drift 余地なし)
-- nvim-notify の blend 基底 #000000 はテーマ色でないため対象外 (_nviminit.lua 側コメント参照)。
local M = {
  dark0_hard    = { hex = "#1d2021", cterm = 234 },
  dark0         = { hex = "#282828", cterm = 235 },
  dark1         = { hex = "#3c3836", cterm = 237 },
  dark3         = { hex = "#665c54", cterm = 245 },
  light1        = { hex = "#ebdbb2", cterm = 223 },
  light4        = { hex = "#a89984", cterm = 250 },
  bright_red    = { hex = "#fb4934", cterm = 203 },
  bright_orange = { hex = "#fe8019", cterm = 208 },
  bright_yellow = { hex = "#fabd2f", cterm = 214 },
  bright_purple = { hex = "#d3869b", cterm = 175 },
}

M.accent = {
  -- Claude Code 風オレンジ基調テーマのアクセント (経緯・変更手順は docs/theme-colors.md)。
  -- current_accent は「現在地」の統一色: tmux の current window 島 (_tmux.conf の @cur-accent =
  -- colour202) と同一。bufferline の選択タブが参照し、tmux バーと nvim タブラインで
  -- 「いまここ = 蛍光オレンジ」の色言語を揃える。
  -- 変えるときは tmux 側 @cur-accent と対で (docs/theme-colors.md のペア表)。
  current_accent = { hex = "#FF5F00", cterm = 202 },
  -- Visual (選択テキスト) 用の暖色。現在地 Coral より一段落ち着いたトーン (tmux ペアなし)。
  kraft = { hex = "#D4A27F", cterm = 180 },
}

M.diag = {
  -- coc 時代のサイン配色 (エラー=白字/赤地・警告=黒字/橙地) の踏襲。意図的に gruvbox 外。
  error_fg = { hex = "#ffffff", cterm = 231 },
  error_bg = { hex = "#ff0000", cterm = 196 },
  warn_fg  = { hex = "#000000", cterm = 16 },
  warn_bg  = { hex = "#d78700", cterm = 172 },
}

return M
