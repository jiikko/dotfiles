// 1 行のテキスト編集状態 (rune 単位)。bubbletea の textinput は使わない: 本物のカーソルを
// 隠して偽カーソルを描くため、IME の未確定文字が入力位置に出ない (2026-08-27 の報告)。
// ここは「文字列とカーソル位置」だけを持ち、描画側が本物のカーソル位置を tea.View に載せる。
package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type editor struct {
	runes []rune
	pos   int // カーソルの位置 (0..len(runes))
}

func (e *editor) value() string { return string(e.runes) }

func (e *editor) setValue(s string) {
	e.runes = []rune(s)
	e.pos = len(e.runes)
}

// cursorCol はカーソルまでの表示幅 (東アジア文字 = 2 セル)。描画側が本物のカーソルを置く列。
func (e *editor) cursorCol() int { return ansi.StringWidth(string(e.runes[:e.pos])) }

func (e *editor) insert(s string) {
	r := []rune(s)
	e.runes = append(e.runes[:e.pos], append(r, e.runes[e.pos:]...)...)
	e.pos += len(r)
}

// viewport は幅 width に収まる表示文字列と、その中でのカーソル列を返す。長い入力でも行が
// 画面幅を超えないようにする: 超えると端末が折り返し、本物のカーソル位置 (= IME の未確定文字が
// 出る場所) が行数分ずれる (2026-08-28 のユーザー報告の一因)。
// focused=false のときは末尾を省略記号でなく単純に切る (カーソルが無いので左端固定でよい)。
func (e *editor) viewport(width int, focused bool) (string, int) {
	if width < 4 {
		width = 4
	}
	if !focused {
		return truncate(e.value(), width), 0
	}
	// カーソルが右端に来るまでは左端固定。超えたぶんだけ左へずらす
	col := e.cursorCol()
	start := 0
	for col-start > width-1 {
		start += ansi.StringWidth(string(e.runes[startRune(e.runes, start)]))
	}
	shown := ""
	w := 0
	for _, r := range e.runes[startRune(e.runes, start):] {
		rw := ansi.StringWidth(string(r))
		if w+rw > width {
			break
		}
		shown += string(r)
		w += rw
	}
	return shown, col - start
}

// startRune は「表示幅 start までに何 rune 使うか」を返す (幅で切った位置を rune 境界に直す)。
func startRune(runes []rune, startWidth int) int {
	w := 0
	for i, r := range runes {
		if w >= startWidth {
			return i
		}
		w += ansi.StringWidth(string(r))
	}
	return len(runes)
}

// truncate は表示幅で切る (全角を半端に割らない)。
func truncate(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	out := ""
	w := 0
	for _, r := range s {
		rw := ansi.StringWidth(string(r))
		if w+rw > width {
			break
		}
		out += string(r)
		w += rw
	}
	return out
}

// handle は編集キーを処理する。扱ったら true (呼び出し側は他の解釈をしない)。
func (e *editor) handle(key string, text string) bool {
	switch key {
	case "backspace", "ctrl+h":
		if e.pos > 0 {
			e.runes = append(e.runes[:e.pos-1], e.runes[e.pos:]...)
			e.pos--
		}
	case "delete", "ctrl+d":
		if e.pos < len(e.runes) {
			e.runes = append(e.runes[:e.pos], e.runes[e.pos+1:]...)
		}
	case "left", "ctrl+b":
		if e.pos > 0 {
			e.pos--
		}
	case "right", "ctrl+f":
		if e.pos < len(e.runes) {
			e.pos++
		}
	case "home", "ctrl+a":
		e.pos = 0
	case "end", "ctrl+e":
		e.pos = len(e.runes)
	case "ctrl+u":
		e.runes = append([]rune{}, e.runes[e.pos:]...)
		e.pos = 0
	case "ctrl+k":
		e.runes = append([]rune{}, e.runes[:e.pos]...)
	case "ctrl+w":
		e.deleteWord()
	default:
		// 印字可能な入力 (IME の確定文字列を含む) だけを受ける。制御文字・修飾キーは無視する
		if text == "" || strings.ContainsFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return false
		}
		e.insert(text)
	}
	return true
}

func (e *editor) deleteWord() {
	i := e.pos
	for i > 0 && e.runes[i-1] == ' ' {
		i--
	}
	for i > 0 && e.runes[i-1] != ' ' {
		i--
	}
	e.runes = append(e.runes[:i], e.runes[e.pos:]...)
	e.pos = i
}
