// Package gutter は本文の左に出す行番号の溝 (nvim の number)。glogx の issues viewer の本文と treefiler のタイルが共有する。
//
// 🚨 番号は「ソースの行番号」であって表示行の連番ではない。折り返しの続き行には番号を出さない (src=0 を渡す)。
// 同じ番号を続き行にも並べると「その行がそこにある」と読めてしまい、外 (nvim / Claude Code) へ持ち出したとき指す先がずれる。
package gutter

import (
	"strconv"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// minDigits は番号の最低の桁数 (行数が少なくても詰まって見えないように)。
const minDigits = 2

// Separator は番号と本文の間の区切り (1 桁)。
const Separator = " "

// Width は溝の桁数 (番号 + 区切り)。lines は出しうる番号の最大値 (ソースの行数)。行数が増えても揃うよう桁数で決める。
func Width(lines int) int {
	return max(len(strconv.Itoa(max(lines, 1))), minDigits) + termwidth.Of(Separator)
}

// Format は 1 行ぶんの溝 (width 桁・色なし) = Number + Separator。src<=0 は空白だけ。
func Format(src, width int) string {
	if width <= 0 {
		return ""
	}
	return Number(src, width) + Separator
}

// Number は溝のうち番号の部分 (width から区切りを引いた桁・右寄せ・色なし。src<=0 は空白)。番号だけを塗りたい
// 使う側は、Format の出力を区切りの長さで切らずにこちらを使う (区切りの字を変えても壊れない)。
func Number(src, width int) string {
	w := max(width-termwidth.Of(Separator), 0)
	if src <= 0 {
		return termwidth.PadSpaces(w)
	}
	return termwidth.FillLeft(strconv.Itoa(src), w)
}
