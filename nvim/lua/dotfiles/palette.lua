-- gruvbox 色の hex↔cterm ペアの唯一の出典。
--
-- 主環境は termguicolors=off の 256色運用 (規律の一次情報は dotfiles/hl.lua) で、gui 色 (hex)
-- が設計の真・cterm はその忠実な 256色近似。かつてこの対応が各所 (bufferline highlights /
-- incline render / hl.set 呼び出し) に手書きコピペされ、bufferline 内で同じ #1d2021 に
-- ctermbg=234 と 237 (=#3c3836 の対応値) が混在する drift が起きていた (2026-07-16 に 234 へ
-- 統一)。ここを参照すれば hex と cterm の組が構造的にズレなくなる。
--
-- 名前は gruvbox 公式パレットの呼称 (dark0_hard 等)。値を変える/色を足すときはここだけ触る。
-- 対象は gruvbox 色のみ: lsp.lua の診断サイン (coc 時代踏襲の #ffffff/#ff0000 等。cterm が
-- 完全一致の非 gruvbox 色で drift 余地なし) と nvim-notify の blend 基底 #000000 は対象外。
-- hl.set を通すか (ColorScheme 再適用) はここと直交する別規律 (hl.lua 参照)。
return {
  dark0_hard    = { hex = "#1d2021", cterm = 234 },
  dark0         = { hex = "#282828", cterm = 235 },
  dark1         = { hex = "#3c3836", cterm = 237 },
  dark4         = { hex = "#665c54", cterm = 245 },
  light1        = { hex = "#ebdbb2", cterm = 223 },
  light4        = { hex = "#a89984", cterm = 250 },
  bright_red    = { hex = "#fb4934", cterm = 203 },
  bright_orange = { hex = "#fe8019", cterm = 208 },
  bright_yellow = { hex = "#fabd2f", cterm = 214 },
  bright_purple = { hex = "#d3869b", cterm = 175 },
}
