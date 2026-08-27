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
