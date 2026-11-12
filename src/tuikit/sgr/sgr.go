// Package sgr は端末の基本 SGR (Select Graphic Rendition) シーケンスを 1 箇所に置く。
//
// 同じ意味の色を画面ごとに別の名前で定義すると、揃っているかどうかが人の目に依存する
// (glogx で 3 パッケージが ansiRed / cRed を別々に持っていた)。値はここだけが持つ。
//
// 置くのは**基本の色と装飾と、256 色の番号が表す RGB (`XtermRGB`) だけ**。「この色は何を意味するか」(状態・時間・テーマ) は使う側が
// 自分の名前で持つ: 意味の割り当ては画面ごとに違い、ここに置くと別の画面の都合が混ざる。
package sgr

const (
	Reset     = "\x1b[0m"
	Bold      = "\x1b[1m"
	Dim       = "\x1b[2m"
	Italic    = "\x1b[3m"
	Underline = "\x1b[4m"
	Reverse   = "\x1b[7m"
	// BoldOff は太字と Dim の両方を戻す (SGR 22 は太字だけを戻す番号を持たない)。
	BoldOff      = "\x1b[22m"
	UnderlineOff = "\x1b[24m"
	ReverseOff   = "\x1b[27m"
	// DefaultFg は文字色だけを端末の色へ戻す (Reset と違い、地の色と装飾は残す)。
	DefaultFg = "\x1b[39m"
	Strike    = "\x1b[9m"
	Red       = "\x1b[31m"
	Green     = "\x1b[32m"
	Yellow    = "\x1b[33m"
	Magenta   = "\x1b[35m"
	Cyan      = "\x1b[36m"
	// BrightBlue は Green と並べるときの青。Cyan は Green と隣り合うと見分けが付かない
	// (実測 2026-08-31)。
	BrightBlue  = "\x1b[94m"
	BrightWhite = "\x1b[97m"
	// BrightBlack は地に沈ませる灰。Dim と違い、背景色付きのセルと隣り合っても明度が安定する。
	BrightBlack = "\x1b[90m"
)
