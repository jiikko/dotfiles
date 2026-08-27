// Package sgr は端末の基本 SGR (Select Graphic Rendition) シーケンスを 1 箇所に置く。
//
// main / issues / usage の 3 パッケージが同じ意味の色を別の名前 (ansiRed / cRed) で定義し、
// 「glogx 本体の割り当てを手書きで写す」とコメントで宣言して揃えていた (issue 106)。
// 揃っているかどうかが人の目に依存するので、値はここだけが持つ。
// 256 色のテーマ固有色 (カーソル行の bg・影・最外周フレーム) は main の render.go に残す:
// それらは dotfiles のテーマ意味マップ (docs/theme-colors.md) との対応であって、基本色ではない。
package sgr

const (
	Reset     = "\x1b[0m"
	Bold      = "\x1b[1m"
	Dim       = "\x1b[2m"
	Italic    = "\x1b[3m"
	Underline = "\x1b[4m"
	Strike    = "\x1b[9m"
	Red       = "\x1b[31m"
	Green     = "\x1b[32m"
	Yellow    = "\x1b[33m"
	Magenta   = "\x1b[35m"
	Cyan      = "\x1b[36m"
)
