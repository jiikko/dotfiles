// 画面の組み立て。この UI で何度も壊れたのは「行が幅を超える / 画面が高さを超える /
// カーソルが枠の外」の 3 つで、いずれも**書く側が毎回気をつける**形だったのが原因だった。
// ここに frame として集約し、行を足す経路を 1 本にすることで、気をつけなくても守られるようにする。
//
//	f := newFrame(width, height)
//	f.add("見出し")                  // 幅で切られる
//	f.addAt("> 入力", col)           // 幅で切られ、その行の col にカーソルを置く
//	body, cur := f.render()          // 高さに収め、カーソルを枠内へ入れる
package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type frame struct {
	width, height int
	lines         []string
	curRow        int // カーソルを置く行 (-1 = 無し)
	curCol        int
}

func newFrame(width, height int) *frame {
	return &frame{width: width, height: height, curRow: -1}
}

// add は 1 行足す (幅を超える分は切る)。
// ⚠️ 埋め込みの改行は空白に潰す。1 回の add が 2 行になると、addAt が覚えた行番号と
//
//	実際の行がずれる (= カーソルが入力欄と違う行に出る)。
func (f *frame) add(s string) {
	s = strings.ReplaceAll(s, "\n", " ")
	f.lines = append(f.lines, truncateSGR(s, f.width))
}

// addAt は 1 行足し、その行の col 列にカーソルを置く。
func (f *frame) addAt(s string, col int) {
	f.curRow = len(f.lines)
	f.curCol = col
	f.add(s)
}

// render は高さに収めた本文と、枠の中に収めたカーソルを返す。
// ⚠️ 高さに収めるときは、カーソルのある行を必ず残す (末尾から切るだけだと、入力欄ごと
//
//	切られてカーソルだけが空行の上に残る = 入力が見えないのに IME の未確定文字はそこに出る)。
func (f *frame) render() (string, *tea.Cursor) {
	var cur *tea.Cursor
	if f.curRow >= 0 {
		col := f.curCol
		// ⚠️ 端末の最終列は width-1。width に置くと画面の外になり、そこへ IME の未確定文字が出る
		//    (幅 9 のような極端に狭い端末で実際に起きた)
		if col > f.width-1 {
			col = f.width - 1
		}
		if col < 0 {
			col = 0
		}
		cur = tea.NewCursor(col, f.curRow)
	}
	return fitHeight(strings.Join(f.lines, "\n"), f.height, cur)
}

// fitHeight は画面の高さに収め、カーソル行が窓に残るよう上を捨てる。
func fitHeight(s string, height int, cur *tea.Cursor) (string, *tea.Cursor) {
	if height <= 0 {
		return s, cur
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s, cur
	}
	start := 0
	if cur != nil && cur.Y >= height {
		start = cur.Y - height + 1
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
		start = end - height
	}
	if cur != nil {
		c := *cur
		c.Y -= start
		cur = &c
	}
	return strings.Join(lines[start:end], "\n"), cur
}

// truncateSGR は装飾を含む文字列を表示幅で切り、切ったら装飾を閉じる。
// ⚠️ 幅の保証は fitWidth に任せる (ansi.Truncate だけだと、数え方の食い違う書記素で幅が残る。
//
//	frame の「行は幅を超えない」は、ここが弱いと丸ごと嘘になる)。
func truncateSGR(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	return fitWidth(s, width) + "\x1b[0m"
}

// help はキー説明を幅に収める (入らないものから落とす。折り返して行数を増やさない)。
func help(width int, items ...string) string {
	out := ""
	for _, it := range items {
		cand := out
		if cand != "" {
			cand += "   "
		}
		cand += it
		if ansi.StringWidth(cand) > width {
			break
		}
		out = cand
	}
	return out
}

// pad は表示幅で右詰めする。byte 数で詰めると日本語で崩れる。
func pad(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
