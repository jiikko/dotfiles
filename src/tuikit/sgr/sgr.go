// Package sgr は端末の基本 SGR (Select Graphic Rendition) シーケンスを 1 箇所に置く。
//
// 同じ意味の色を画面ごとに別の名前で定義すると、揃っているかどうかが人の目に依存する
// (glogx で 3 パッケージが ansiRed / cRed を別々に持っていた。issue 106)。値はここだけが持つ。
//
// 置くのは**基本の色と装飾だけ**。「この色は何を意味するか」(状態・時間・テーマ) は使う側が
// 自分の名前で持つ: 意味の割り当ては画面ごとに違い、ここに置くと別の画面の都合が混ざる。
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
	// BrightBlue は Green と並べるときの青。Cyan は Green と隣り合うと見分けが付かない
	// (実測 2026-08-31)。
	BrightBlue  = "\x1b[94m"
	BrightWhite = "\x1b[97m"
	// BrightBlack は地に沈ませる灰。Dim と違い、背景色付きのセルと隣り合っても明度が安定する。
	BrightBlack = "\x1b[90m"
)
