// 1 行のテキスト編集状態。bubbletea の textinput は使わない: 本物のカーソルを隠して偽カーソルを
// 描くため、IME の未確定文字が入力位置に出ない (2026-08-27 の報告)。
// ここは「文字列とカーソル位置」だけを持ち、描画側が本物のカーソル位置を tea.View に載せる。
//
// ⚠️ 幅の計算は必ず ansi (書記素クラスタ単位) で行う。rune ごとに足すと、結合文字や
//
//	VS16 (❤️) で実際の表示幅とずれ、行が画面幅を超える / スクロールが 1 文字も進まず
//	無限ループになる (敵対的レビュー 2026-08-28 で UI のハングとして再現)。
package main

import (
	"unicode"

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
// 出る場所) が行数分ずれる。
//
// 左右の切り出しは ansi.TruncateLeft / ansi.Truncate に任せる (書記素クラスタ単位で切るので、
// 全角や結合文字を半端に割らない。自前で rune を数えると幅がずれ、ずれた分だけ無限に
// スクロールしようとして返らなくなる)。
func (e *editor) viewport(width int, focused bool) (string, int) {
	if width < 1 {
		return "", 0
	}
	v := e.value()
	if !focused {
		return truncate(v, width), 0
	}
	col := e.cursorCol()
	// カーソルが右端を越えたぶんだけ左を捨てる。カーソルは常に見える位置 (width-1 列目まで) に置く
	start := 0
	if col > width-1 {
		start = col - (width - 1)
	}
	shown := truncate(ansi.TruncateLeft(v, start, ""), width)
	// TruncateLeft は書記素の途中では切らないので、実際に落ちた幅を測り直す
	dropped := ansi.StringWidth(v) - ansi.StringWidth(ansi.TruncateLeft(v, start, ""))
	cur := col - dropped
	if cur < 0 {
		cur = 0
	}
	if cur > width {
		cur = width
	}
	return shown, cur
}

// truncate は表示幅で切る (書記素クラスタ単位。全角や結合文字を半端に割らない)。
//
// ⚠️ ansi.Truncate の結果を ansi.StringWidth で測り直して詰める。両者の数え方が一致しない
//
//	書記素があり (キーキャップ 1️⃣ など、Truncate は 1 と数えるが StringWidth は 2 と答える)、
//	切ったつもりで倍の幅が残る。この UI は「StringWidth で測った幅」で桁を組むので、
//	そちらに合わせて確実に収める (末尾から rune を落とすので必ず止まる)。
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	out := ansi.Truncate(s, width, "")
	for ansi.StringWidth(out) > width {
		r := []rune(out)
		if len(r) == 0 {
			return ""
		}
		out = string(r[:len(r)-1])
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
		if !acceptable(text) {
			return false
		}
		e.insert(text)
	}
	return true
}

// acceptable は入力として受ける文字列か。制御文字と書式文字 (Cf) を弾く:
//   - 見えないのに送信される (RLO/RLM のような表示順の反転はコマンドを偽装できる)
//   - 幅 0 なので、幅で位置を決める描画とカーソル計算をずらす
//
// 見える文字だけを受ける方が、この UI (打った文字列がそのまま pane へ流れる) の性質に合う。
func acceptable(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		// Cc/Cf に加えて Zl/Zp (U+2028/2029) も弾く: 行区切りとして解釈される端末があり、
		// 打った本人には見えないまま送信される
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return false
		}
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
