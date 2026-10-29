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
	yOnly bool           // y だけを実行にする (Enter も取り消し)。打鍵と関係なく裏から出る板は、移動中の Enter が答えになりうる
	yes   func(m *Model) // 実行する
	no    func(m *Model) // 取り消したとき (nil なら何もしない)
}

// askKey は確認の板のキー。y / Enter (yOnly なら y) だけが実行で、それ以外 (ctrl+c も) は取り消し。
func (m *Model) askKey(k string) {
	a := m.ask
	m.ask = nil
	switch {
	case a.yOnly && k == "y", !a.yOnly && confirm.IsYesStrict(k):
		a.yes(m)
	case a.no != nil:
		a.no(m)
	}
}

// yesKeys は実行になるキー (案内に出す)。
func (a *askBoard) yesKeys() string {
	if a.yOnly {
		return "y"
	}
	return "y/enter"
}

// drawAsk は確認の板 (中央)。名前は切らずに折り返す (切れた先が確認の対象そのものだと、実行前の唯一の確認に何も出ない)。
func (m *Model) drawAsk(c *canvas) {
	a := m.ask
	w := min(confirm.MaxWidth+8, m.w-2)
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
	hint := confirm.HintYesOther
	if a.yOnly {
		hint = "y: 実行   その他: キャンセル"
	}
	body = append(body, "", hint)
	if w < 20 || len(body)+2 > m.h-1 {
		// 板が収まらないと、答え方の行 (末尾) が枠の外へ押し出される (名前は最低 1 行残すので、lead が折れると高さ 7 でも起きる)。
		// それでもキーは板が取る (askKey) ので、見えないまま Enter で実行しないよう、答えのキーを先にして最下行に出す
		m.bottomHint(c, a.yesKeys()+" "+a.verb+" · その他 キャンセル · "+a.title+": "+a.name)
		return
	}
	h := len(body) + 2
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

// bottomHint は、全部のキーを取る層を描けない大きさのとき、その層が開いていることを最下行に出す (最下行は高さ 1 から在る)。
// 幅が足りなければ後ろから切るので、効くキーを先に書く。
func (m *Model) bottomHint(c *canvas, s string) {
	y := m.h - 1
	c.clear(0, y, m.w-1, y, cBar)
	c.put(0, y, termwidth.Truncate(s, m.w, "…"), cText, true)
}
