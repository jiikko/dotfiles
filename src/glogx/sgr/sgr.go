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
	// BrightBlue は「使えるのに使っていない」= 余裕・使い残し。Cyan は Green と隣り合うと
	// 見分けが付かない (実測 2026-08-31) ため、緑と並べる用途ではこちらを使う。
	BrightBlue = "\x1b[94m"
	// BrightWhite は「時間」の色。使用率の状態色 (赤/黄/緑/青/マゼンタ) と衝突しない唯一の
	// 明るい色なので、状態と時間を同じ盤に描き分けるのに使う。
	BrightWhite = "\x1b[97m"
	// BrightBlack は「まだ来ていない未来」を地の色として沈ませる灰 (Dim と違い、
	// 背景色付きのセルと隣り合っても明度が安定する)。
	BrightBlack = "\x1b[90m"
	// ペースゲージの背景色。前景 (Dim / Cyan) では 1 カラム = 半スロットの塗り分けができない
	// (色の付いた空白は前景色では見えない) ため、消化量は背景で描く。
	// _claude/statusline-command.sh の bg_in / bg_over と同じ配色。
	BgGreenOnBlack = "\x1b[42;30m" // 想定内の消化
	BgRedOnBlack   = "\x1b[41;30m" // 前借り
	// 盤 (ratelimit ダッシュボード) の地と帯。前景の弧はドット 2 本 = 細い線にしかならないので、
	// 円周を太く見せたいところは背景色で塗る (背景は 1 セルが最小単位なので必ず太くなる)。
	// ⚠️ BgFace だけ 256 色なのは、dotfiles のテーマで「一段浮かせた地」が 235 に決まっている
	// ため (docs/theme-colors.md: 234 = pane 地 / 235 = tmux バー地)。基本 8 色に該当が無い。
	BgFace  = "\x1b[48;5;235m" // 盤の内側 (円盤としての地)
	BgTrack = "\x1b[100m"      // 円周のうち「もう過ぎた」ぶん
	BgWhite = "\x1b[47m"       // 円周のうち「残っている時間」ぶん
	// UnderlineBold は現在位置の目印。背景色と反転は競合するので下線を使う。
	UnderlineBold = "\x1b[4;1m"
)
