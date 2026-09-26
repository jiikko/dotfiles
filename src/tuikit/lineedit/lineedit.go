// Package lineedit は 1 行の入力欄の編集 (カーソルと readline の編集キー)。描画フレームワークに依存しない
// (キーは bubbletea の KeyPressMsg.String() と同じ表記の文字列)。
//
// キーの語彙は docs/glogx-ui-guide.md の「入力欄の編集キー」が正本:
//
//	backspace ctrl+h        カーソルの前の 1 文字を消す (ctrl+h は端末によって backspace が 0x08 で届くため)
//	delete ctrl+d           カーソルの位置の 1 文字を消す
//	left ctrl+b / right ctrl+f   1 文字動く
//	home ctrl+a / end ctrl+e     先頭 / 末尾へ
//	alt+b / alt+f           1 語動く
//	ctrl+w alt+backspace    カーソルの前の 1 語を消す
//	ctrl+u                  カーソルの前を全部消す
//	ctrl+k                  カーソルの後ろを全部消す
//
// 🚨 入力中は ctrl+b / ctrl+f / ctrl+u / ctrl+d が**編集**の意味になる (一覧の移動の語彙ではない)。
// 入力欄を持つ画面は、入力中のキーを先にここへ渡し、Enter / Esc / Tab だけを自分で捌く。
package lineedit

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"tuikit/termwidth"
)

// Line は 1 行の入力。カーソルは rune の位置 (0 = 先頭、len = 末尾)。
type Line struct {
	r   []rune
	cur int
}

func (l *Line) String() string { return string(l.r) }
func (l *Line) Cursor() int    { return l.cur }
func (l *Line) Empty() bool    { return len(l.r) == 0 }
func (l *Line) Reset()         { *l = Line{} }

// View はカーソルの位置に mark を差し込んだ文字列 (描画用)。
func (l *Line) View(mark string) string { return string(l.r[:l.cur]) + mark + string(l.r[l.cur:]) }

// Window は幅 w の欄に出す文字列と、その中のキャレットの桁 (欄の頭から。全角は 2 と数える)。
// キャレットが欄の中 (w-1 桁目まで) に収まるよう、長ければ前を切る。後ろは切らない (幅に収めるのは描く側)。
// 🚨 キャレットを欄の外に置くと、IME の変換中の文字も欄の外に出る。桁は caret.At に「欄の左端 + 桁」で渡す。
func (l *Line) Window(w int) (text string, col int) {
	before, after := string(l.r[:l.cur]), string(l.r[l.cur:])
	for before != "" && termwidth.Of(before) > w-1 {
		_, size := utf8.DecodeRuneInString(before)
		before = before[size:]
	}
	return before + after, termwidth.Of(before)
}

// Insert はカーソルの位置に s を入れる (打鍵とペースト)。1 行なので改行とタブは空白にし、他の制御文字は落とす。
func (l *Line) Insert(s string) {
	var add []rune
	for _, c := range s {
		switch {
		case c == '\n' || c == '\r' || c == '\t':
			add = append(add, ' ')
		case unicode.IsControl(c):
		default:
			add = append(add, c)
		}
	}
	l.r = append(l.r[:l.cur], append(add, l.r[l.cur:]...)...)
	l.cur += len(add)
}

// Key は編集キーを適用する。key は KeyPressMsg.String()、text はその打鍵が入力する文字 (KeyPressMsg.Text)。
// 編集キーか文字の入力なら true。Enter / Esc / Tab などは false を返す (呼び出し側が捌く)。
func (l *Line) Key(key, text string) bool {
	switch key {
	case "backspace", "ctrl+h":
		if l.cur > 0 {
			l.r = append(l.r[:l.cur-1], l.r[l.cur:]...)
			l.cur--
		}
	case "delete", "ctrl+d":
		if l.cur < len(l.r) {
			l.r = append(l.r[:l.cur], l.r[l.cur+1:]...)
		}
	case "left", "ctrl+b":
		l.cur = max(0, l.cur-1)
	case "right", "ctrl+f":
		l.cur = min(len(l.r), l.cur+1)
	case "home", "ctrl+a":
		l.cur = 0
	case "end", "ctrl+e":
		l.cur = len(l.r)
	case "alt+b":
		l.cur = l.wordStart()
	case "alt+f":
		l.cur = l.wordEnd()
	case "ctrl+w", "alt+backspace":
		s := l.wordStart()
		l.r = append(l.r[:s], l.r[l.cur:]...)
		l.cur = s
	case "ctrl+u":
		l.r = l.r[l.cur:]
		l.cur = 0
	case "ctrl+k":
		l.r = l.r[:l.cur]
	default:
		if text == "" || strings.HasPrefix(key, "ctrl+") || strings.HasPrefix(key, "alt+") {
			return false
		}
		l.Insert(text)
	}
	return true
}

// wordStart はカーソルの前の語の先頭 (空白を飛ばしてから、空白でない並びの先頭)。
func (l *Line) wordStart() int {
	i := l.cur
	for i > 0 && unicode.IsSpace(l.r[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(l.r[i-1]) {
		i--
	}
	return i
}

// wordEnd はカーソルの後ろの語の末尾。
func (l *Line) wordEnd() int {
	i := l.cur
	for i < len(l.r) && unicode.IsSpace(l.r[i]) {
		i++
	}
	for i < len(l.r) && !unicode.IsSpace(l.r[i]) {
		i++
	}
	return i
}
