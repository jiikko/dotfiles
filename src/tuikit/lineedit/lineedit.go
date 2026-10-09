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

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// Line は 1 行の入力。カーソルは rune の位置 (0 = 先頭、len = 末尾) で、常に書記素クラスタの境界に置く。
//
// 🚨 移動と削除は「見た目の 1 文字」= 書記素クラスタ単位で行う。rune 単位だと肌色・ZWJ・国旗・結合文字の
// 一部だけが消え、見た目はほぼ同じなのに別の文字列が送られる (👍🏽 の backspace 1 回で 👍 が残る。issue 589)。
// 境界は termwidth.FirstCluster (x/ansi) で求める。uniseg は Unicode の版が x/ansi と違い、判断が割れた実測がある
// (termwidth/width_fast_test.go の TestAcceptedSymbolsNeverCombineWithEachOther のコメント)。
type Line struct {
	r   []rune
	cur int
}

func (l *Line) String() string { return string(l.r) }
func (l *Line) Cursor() int    { return l.cur }
func (l *Line) Empty() bool    { return len(l.r) == 0 }
func (l *Line) Reset()         { *l = Line{} }

// SetCursor はカーソルを rune の位置 i に置く。範囲の外は端へ、クラスタの途中は後ろの境界へ寄せる
// (保存しておいた Cursor() を戻す用途。left を差の回数だけ押して戻すと、クラスタ単位で動くぶん戻りすぎる)。
func (l *Line) SetCursor(i int) { l.cur = l.snap(min(max(i, 0), len(l.r))) }

// View はカーソルの位置に mark を差し込んだ文字列 (描画用)。
func (l *Line) View(mark string) string { return string(l.r[:l.cur]) + mark + string(l.r[l.cur:]) }

// Window は幅 w の欄に出す文字列と、その中のキャレットの桁 (欄の頭から。全角は 2 と数える)。
// キャレットが欄の中 (w-1 桁目まで) に収まるよう、長ければ前を切る。後ろは切らない (幅に収めるのは描く側)。
// 🚨 キャレットを欄の外に置くと、IME の変換中の文字も欄の外に出る。桁は caret.At に「欄の左端 + 桁」で渡す。
//
// 前を切る単位も書記素クラスタ (欄の左端に肌色修飾子や VS16 だけが残らない)。走査はクラスタを 1 回数えるだけで、
// 入力の長さに対して線形 (前を 1 字削るたびに測り直すと O(n²) で、1 万字で 150 ms かかった。issue 589)。
func (l *Line) Window(w int) (text string, col int) {
	before := l.r[:l.cur]
	cs := clusters(before)
	start := len(before)
	for i := len(cs) - 1; i >= 0 && col+cs[i].width <= w-1; i-- {
		col += cs[i].width
		start -= cs[i].runes
	}
	return string(l.r[start:]), col
}

// cluster は書記素クラスタ 1 つの rune 数と表示幅。
type cluster struct{ runes, width int }

// clusters は r を書記素クラスタに分ける。幅の合計は termwidth.Of(string(r)) と一致する
// (termwidth の TestFirstClusterWidthsSumToOf が固定している契約)。
func clusters(r []rune) []cluster {
	var out []cluster
	for s := string(r); s != ""; {
		c, w := termwidth.FirstCluster(s)
		out = append(out, cluster{runes: utf8.RuneCountInString(c), width: w})
		s = s[len(c):]
	}
	return out
}

// prevBoundary は i より前で最も近いクラスタの境界 (i が 0 なら 0)。
func (l *Line) prevBoundary(i int) int {
	b := 0
	for _, c := range clusters(l.r) {
		if b+c.runes >= i {
			return b
		}
		b += c.runes
	}
	return b
}

// nextBoundary は i より後ろで最も近いクラスタの境界 (末尾なら len)。
func (l *Line) nextBoundary(i int) int {
	b := 0
	for _, c := range clusters(l.r) {
		b += c.runes
		if b > i {
			return b
		}
	}
	return len(l.r)
}

// snap は i がクラスタの途中なら、そのクラスタの後ろの境界へ寄せる。編集で前後の字が 1 つのクラスタに
// まとまったとき (e の後ろに U+0301 を入れる・間の字を消して国旗の 2 字が隣り合う) にカーソルを境界へ戻す。
func (l *Line) snap(i int) int {
	if i <= 0 {
		return 0
	}
	return l.nextBoundary(i - 1)
}

// Acceptable は入力欄に入れてよい文字か。弾くもの: 制御文字 (Cc)・文字の向きを変える制御文字 (U+202A-202E / U+2066-2069。
// 見えている文字列と中身の並びが食い違う。`!` のコマンド行では見えているものと実行されるものが別になる)・行と段落の区切り
// (U+2028 / 2029。行区切りとして解釈する端末がある。Insert は改行と同じく空白にする)。ZWJ と異体字セレクタは通す (絵文字の 1 文字として扱う。
// TestEditKeysKeepGraphemeClusters)。schedkeys はこれに加えて書式文字全般と異体字セレクタも弾く (issue 700 の 2)。
func Acceptable(r rune) bool {
	return !unicode.IsControl(r) && (r < 0x202a || r > 0x202e) && (r < 0x2066 || r > 0x2069) && r != 0x2028 && r != 0x2029
}

// Insert はカーソルの位置に s を入れる (打鍵とペースト)。1 行なので改行とタブは空白にし、他の制御文字は落とす。
func (l *Line) Insert(s string) {
	var add []rune
	for _, c := range s {
		switch {
		case c == '\n' || c == '\r' || c == '\t' || c == 0x2028 || c == 0x2029: // 行・段落の区切りも空白に (落とすと単語がくっつく)
			add = append(add, ' ')
		case !Acceptable(c):
		default:
			add = append(add, c)
		}
	}
	l.r = append(l.r[:l.cur], append(add, l.r[l.cur:]...)...)
	l.cur = l.snap(l.cur + len(add))
}

// Key は編集キーを適用する。key は KeyPressMsg.String()、text はその打鍵が入力する文字 (KeyPressMsg.Text)。
// 編集キーか文字の入力なら true。Enter / Esc / Tab などは false を返す (呼び出し側が捌く)。
func (l *Line) Key(key, text string) bool {
	switch key {
	case "backspace", "ctrl+h":
		if l.cur > 0 {
			p := l.prevBoundary(l.cur)
			l.r = append(l.r[:p], l.r[l.cur:]...)
			l.cur = l.snap(p)
		}
	case "delete", "ctrl+d":
		if l.cur < len(l.r) {
			l.r = append(l.r[:l.cur], l.r[l.nextBoundary(l.cur):]...)
			l.cur = l.snap(l.cur)
		}
	case "left", "ctrl+b":
		l.cur = l.prevBoundary(l.cur)
	case "right", "ctrl+f":
		l.cur = l.nextBoundary(l.cur)
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
		l.cur = l.snap(s)
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

// wordStart はカーソルの前の語の先頭 (空白を飛ばしてから、空白でない並びの先頭)。空白に結合文字が付いて
// 1 つのクラスタになっていると rune の位置はクラスタの途中になりうるので、手前の境界へ寄せる。
func (l *Line) wordStart() int {
	i := l.wordStartRune()
	if i == l.snap(i) {
		return i
	}
	return l.prevBoundary(i)
}

func (l *Line) wordStartRune() int {
	i := l.cur
	for i > 0 && unicode.IsSpace(l.r[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(l.r[i-1]) {
		i--
	}
	return i
}

// wordEnd はカーソルの後ろの語の末尾 (クラスタの途中なら後ろの境界へ寄せる)。
func (l *Line) wordEnd() int { return l.snap(l.wordEndRune()) }

func (l *Line) wordEndRune() int {
	i := l.cur
	for i < len(l.r) && unicode.IsSpace(l.r[i]) {
		i++
	}
	for i < len(l.r) && !unicode.IsSpace(l.r[i]) {
		i++
	}
	return i
}
