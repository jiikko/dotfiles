package filer

import (
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// askBoard は中央の確認の板 (開いている間は全部のキーを取る)。d (ゴミ箱へ移す) と、> で開く項目が多いとき (explode.go) が開く。
type askBoard struct {
	title string         // 枠の題
	lead  string         // 1 行目 (何をするか)
	name  string         // 対象の名前。切らずに折り返して出す (空なら出さない)
	verb  string         // y / Enter の案内の語 (LayerHints)
	yes   func(m *Model) // y / Enter で実行する
}

// askKey は確認の板のキー。y / Enter だけが実行で、それ以外 (ctrl+c も) は取り消し。
func (m *Model) askKey(k string) {
	a := m.ask
	m.ask = nil
	if confirm.IsYesStrict(k) {
		a.yes(m)
	}
}

// drawAsk は確認の板 (中央)。名前は切らずに折り返す (切れた先が確認の対象そのものだと、実行前の唯一の確認に何も出ない)。
func (m *Model) drawAsk(c *canvas) {
	a := m.ask
	w := min(confirm.MaxWidth+8, m.w-2)
	if w < 20 || m.h < 7 {
		return
	}
	inner := w - 4
	body := termwidth.Wrap(a.lead, inner, false)
	if a.name != "" {
		names := termwidth.Wrap(a.name, inner, false)
		if room := m.h - 1 - 2 - len(body) - 2; len(names) > room { // 枠 2 行と、題・空行・案内を残す (案内が画面から落ちないように)
			names = names[:max(room, 1)]
			names[len(names)-1] = termwidth.Truncate(names[len(names)-1], inner-1, "") + "…"
		}
		body = append(body, names...)
	}
	body = append(body, "", confirm.HintYesOther)
	h := min(len(body)+2, m.h-1)
	x0, y0 := (m.w-w)/2, max((m.h-1-h)/2, 0)
	c.frame(rect{x0, y0, w, h}, cAccRoute)
	c.put(x0+2, y0, " "+a.title+" ", cText, true)
	for i, s := range body {
		y := y0 + 1 + i
		if y >= y0+h-1 {
			break
		}
		col := cText
		if i == len(body)-1 {
			col = cMuted
		}
		c.put(x0+2, y, termwidth.Truncate(s, inner, "…"), col, false)
	}
}
