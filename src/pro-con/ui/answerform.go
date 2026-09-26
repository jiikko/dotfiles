package ui

// 選択肢つきの質問の回答フォーム (issue 493)。質問待ちのカードで r を押すと、問いに選択肢があれば 1 行の入力欄の代わりにこれを開く。
// 見た目は 2026-09-26 に人が見本から選んだ形: 画面の中央の枠に問いを縦に全部並べる。radio は (•) / ( )、checkbox は [x] / [ ]、
// 推奨はオレンジの「推奨」で初めから選んでおく、カーソルは ›。どの問いにも「その他 (自由に書く)」、最後に「補足 (任意)」の欄。
// 答えは card.FormatAnswer の自由文にして、今の回答 (backend.Answer) で送る。

import (
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/caret"
	"tuikit/layout"
	"tuikit/lineedit"
	"tuikit/termwidth"

	"pro-con/backend"
	"pro-con/card"
)

const (
	formMaxWidth  = 84 // 枠の幅の上限 (広い端末でも読む幅に収める)
	formDescShift = 9  // 説明を次の行へ折り返すときの字下げ (選択肢の名前の頭に揃える)
	formFieldMin  = 12 // 「その他」の欄をその行に置ける最小の幅 (足りなければ次の行へ)
)

// answerForm は回答フォームの状態。問いは開いたときのカードの Wait.Questions を写して持つ (開いている間に記録が変わっても揺らさない)。
type answerForm struct {
	cardID   string
	preamble string // Wait.Question のうち、問いを文にした部分 (QuestionsText) より前
	qs       []card.Question
	radio    []int    // 1 つ選ぶ問いで選んだもの (-1 = まだ。len(Options) = その他)
	checked  [][]bool // 複数選べる問いで選んだもの (その他は、書いた文が空でないこと)
	other    []lineedit.Line
	note     lineedit.Line
	cur      int // formRows の添字
	// 描いたときのキャレット (画面の座標)。caret が端末のカーソルを置く (IME の変換中の文字をここに出す)
	caretX, caretY int
	caretOK        bool
}

// formRow はカーソルが止まる行。opt が len(Options) なら「その他」、q が len(qs) なら補足。
type formRow struct{ q, opt int }

func newAnswerForm(c card.Card) answerForm {
	qs := c.Wait.Questions
	f := answerForm{cardID: c.ID, qs: qs, radio: make([]int, len(qs)), checked: make([][]bool, len(qs)), other: make([]lineedit.Line, len(qs))}
	f.preamble = strings.TrimSpace(strings.TrimSuffix(c.Wait.Question, card.QuestionsText(qs)))
	for i, q := range qs {
		f.radio[i] = -1
		f.checked[i] = make([]bool, len(q.Options))
		for j, o := range q.Options {
			if !o.Recommended {
				continue
			}
			if q.MultiSelect {
				f.checked[i][j] = true
			} else {
				f.radio[i] = j
			}
		}
	}
	return f
}

func (f *answerForm) rows() []formRow {
	var out []formRow
	for i, q := range f.qs {
		for j := range len(q.Options) + 1 {
			out = append(out, formRow{i, j})
		}
	}
	return append(out, formRow{q: len(f.qs)})
}

func (f *answerForm) row() formRow { return f.rows()[f.cur] }

// field は今の行の書く欄 (その他・補足)。選択肢の行なら nil。
func (f *answerForm) field() *lineedit.Line {
	r := f.row()
	switch {
	case r.q == len(f.qs):
		return &f.note
	case r.opt == len(f.qs[r.q].Options):
		return &f.other[r.q]
	}
	return nil
}

// jump は次 (d=1) / 前 (d=-1) の問いの先頭の行へ移る (補足の欄も 1 つの問いとして数える)。
func (f *answerForm) jump(d int) {
	rows := f.rows()
	q := min(max(rows[f.cur].q+d, 0), len(f.qs))
	for i, r := range rows {
		if r.q == q {
			f.cur = i
			return
		}
	}
}

// toggle は選択肢の行で space を押した: radio はそれだけを選び、checkbox は選ぶ / 外すを切り替える。
func (f *answerForm) toggle() {
	r := f.row()
	if f.qs[r.q].MultiSelect {
		f.checked[r.q][r.opt] = !f.checked[r.q][r.opt]
	} else {
		f.radio[r.q] = r.opt
	}
}

// typed は書く欄が変わった後。「その他」に書き始めたら、1 つ選ぶ問いではその他を選んだことにする。
func (f *answerForm) typed() {
	r := f.row()
	if r.q < len(f.qs) && !f.qs[r.q].MultiSelect && f.otherFilled(r.q) {
		f.radio[r.q] = len(f.qs[r.q].Options)
	}
}

// otherFilled は問い q の「その他」に書いてあるか (空白だけは書いていない)。選んだ印・送る答え・未回答の判定はどれもこれで決める
// (表示と送る答えで基準がずれると、[x] に見えるのに答えから消える)。
func (f *answerForm) otherFilled(q int) bool { return strings.TrimSpace(f.other[q].String()) != "" }

func (f *answerForm) picks() []card.Pick {
	out := make([]card.Pick, len(f.qs))
	for i, q := range f.qs {
		if q.MultiSelect {
			for j, on := range f.checked[i] {
				if on {
					out[i].Chosen = append(out[i].Chosen, j)
				}
			}
			out[i].Other = f.other[i].String()
			continue
		}
		switch r := f.radio[i]; {
		case r == len(q.Options):
			out[i].Other = f.other[i].String()
		case r >= 0:
			out[i].Chosen = []int{r}
		}
	}
	return out
}

// unanswered は答えていない最初の問い (-1 = 全部答えた)。
func (f *answerForm) unanswered() int {
	for i, p := range f.picks() {
		if len(p.Chosen) == 0 && strings.TrimSpace(p.Other) == "" {
			return i
		}
	}
	return -1
}

// openAnswerForm は選択肢つきの質問に答えるフォームを開く。
func (m *Model) openAnswerForm(c card.Card) {
	m.form = newAnswerForm(c)
	m.mode = modeForm
}

// handleFormKey は回答フォームを開いている間のキー。選択肢の行では j / k / space が移動と選択、書く欄では文字の入力。
// ↑ / ↓・tab / shift+tab・enter・esc はどの行でも同じ意味 (書く欄に居ても抜けられる)。
func (m *Model) handleFormKey(k tea.KeyPressMsg) tea.Cmd {
	f := &m.form
	n := len(f.rows())
	fld := f.field()
	switch key := k.String(); key {
	case "ctrl+c":
		return m.requestQuit()
	case "esc":
		m.mode = modeBoard
		m.info("回答を取り消した (送っていない)")
	case "enter":
		m.submitForm()
	case "up", "ctrl+p":
		f.cur = max(f.cur-1, 0)
	case "down", "ctrl+n":
		f.cur = min(f.cur+1, n-1)
	case "tab":
		f.jump(1)
	case "shift+tab":
		f.jump(-1)
	default:
		switch {
		case fld != nil:
			if fld.Key(key, k.Text) {
				f.typed()
			}
		case key == "k":
			f.cur = max(f.cur-1, 0)
		case key == "j":
			f.cur = min(f.cur+1, n-1)
		case key == "space":
			f.toggle()
		}
	}
	return nil
}

// pasteForm はペーストを今の書く欄に入れる (選択肢の行では捨てる。キー操作として解釈しない)。
func (m *Model) pasteForm(s string) {
	if fld := m.form.field(); fld != nil {
		fld.Insert(s)
		m.form.typed()
	}
}

// submitForm は答えを自由文にして、送る前の確認に載せる (sendconfirm.go)。答えていない問いがあれば送らず、その問いへカーソルを移す。
func (m *Model) submitForm() {
	f := &m.form
	text, err := card.FormatAnswer(f.qs, f.picks(), f.note.String())
	if err != nil {
		if errors.Is(err, card.ErrUnanswered) {
			if i := f.unanswered(); i >= 0 {
				f.cur = 0
				f.jump(i)
			}
		}
		m.refuse(err.Error())
		return
	}
	m.askSend(backend.Answer{CardID: f.cardID, Text: text, From: "人間"}, sendConfirm{title: f.cardID + " へ回答", body: strings.Split(text, "\n")})
}

// formHints は回答フォームの案内 (今の行で効くキーだけ)。
func (m *Model) formHints() []string {
	if m.form.field() != nil {
		return []string{"文字を打つと書く", "↑ / ↓ 選択", "tab 次の問い", "enter 送信", "esc 取り消し"}
	}
	return []string{"j / k 選択", "space 選ぶ / 外す", "tab 次の問い", "enter 送信", "esc 取り消し"}
}

// formLine は枠の中の 1 行。caret は書く欄のキャレットの桁 (-1 = 無い)。
type formLine struct {
	text  string
	cur   bool // カーソルの行 (枠が入り切らないとき、ここが見えるようにずらす)
	caret int
}

// formWidth は枠の幅と中身の幅 ("│ " と " │" を除く)。
func formWidth(total int) (width, inner int) {
	width = max(min(formMaxWidth, total-4), 20)
	return width, width - 4
}

// formLines は枠の中身を組む (どの行も幅 inner に収める)。
func (f *answerForm) formLines(inner int) []formLine {
	var out []formLine
	add := func(s string) { out = append(out, formLine{text: s, caret: -1}) }
	if f.preamble != "" {
		for _, l := range strings.Split(ansi.Wrap(f.preamble, inner, ""), "\n") {
			add(fg(244) + l + sgrReset)
		}
		add("")
	}
	cur := f.row()
	for i, q := range f.qs {
		for _, l := range strings.Split(ansi.Wrap(sgrBold+fg(231)+strconv.Itoa(i+1)+". "+q.Question+sgrReset+"  "+fg(244)+"("+q.Kind()+")"+sgrReset, inner, ""), "\n") {
			add(l)
		}
		for j := range len(q.Options) + 1 {
			out = append(out, f.optionLines(i, j, cur == formRow{i, j}, inner)...)
		}
		add("")
	}
	add(sgrBold + fg(250) + "補足 (任意)" + sgrReset)
	on := cur.q == len(f.qs)
	head := "   "
	if on {
		head = " " + fg(214) + "›" + sgrReset + " "
	}
	s, c := fieldView(&f.note, inner-3, on, "ここに書く (空でよい)")
	out = append(out, formLine{text: head + s, cur: on, caret: caretIf(on, 3+c)})
	return out
}

// optionLines は問い q の選択肢 j (len(Options) ならその他) の行。説明が収まらなければ次の行へ折り返す。
func (f *answerForm) optionLines(q, j int, on bool, inner int) []formLine {
	opts := f.qs[q].Options
	other := j == len(opts)
	var name, desc string
	var chosen, rec bool
	if other {
		name = "その他 (自由に書く)"
		chosen = f.otherFilled(q) && (f.qs[q].MultiSelect || f.radio[q] == j)
	} else {
		name, desc, rec = opts[j].Label, opts[j].Description, opts[j].Recommended
		if f.qs[q].MultiSelect {
			chosen = f.checked[q][j]
		} else {
			chosen = f.radio[q] == j
		}
	}
	mark := map[[2]bool]string{{false, false}: "( )", {false, true}: "(•)", {true, false}: "[ ]", {true, true}: "[x]"}[[2]bool{f.qs[q].MultiSelect, chosen}]
	ptr, nameSGR := " ", fg(252)
	if on {
		ptr, nameSGR = fg(214)+"›"+sgrReset, sgrBold+fg(231)
	}
	markSGR := fg(244)
	if chosen {
		markSGR = fg(46)
	}
	line := " " + ptr + " " + markSGR + mark + sgrReset + " " + nameSGR + name + sgrReset
	if rec {
		line += " " + fg(214) + "推奨" + sgrReset
	}
	var out []formLine
	if other {
		if f.other[q].Empty() && !on {
			return []formLine{{text: line, caret: -1}}
		}
		room := inner - termwidth.Of(line) - 2
		if room >= formFieldMin {
			s, c := fieldView(&f.other[q], room, on, "")
			return []formLine{{text: line + "  " + s, cur: on, caret: caretIf(on, termwidth.Of(line)+2+c)}}
		}
		s, c := fieldView(&f.other[q], inner-formDescShift, on, "")
		return []formLine{{text: line, cur: on, caret: -1}, {text: strings.Repeat(" ", formDescShift) + s, caret: caretIf(on, formDescShift+c)}}
	}
	switch {
	case desc == "":
		out = append(out, formLine{text: line, cur: on, caret: -1})
	case termwidth.Of(line)+4+termwidth.Of(desc) <= inner:
		out = append(out, formLine{text: line + fg(244) + "  — " + desc + sgrReset, cur: on, caret: -1})
	default:
		out = append(out, formLine{text: line, cur: on, caret: -1})
		for _, l := range strings.Split(ansi.Wrap(desc, inner-formDescShift, ""), "\n") {
			out = append(out, formLine{text: strings.Repeat(" ", formDescShift) + fg(244) + l + sgrReset, caret: -1})
		}
	}
	return out
}

func caretIf(on bool, c int) int {
	if on {
		return c
	}
	return -1
}

// fieldView は書く欄を幅 w で描き、キャレットの桁 (欄の頭から) を返す。キャレットが見えるよう、長ければ前を切る。
func fieldView(l *lineedit.Line, w int, on bool, placeholder string) (string, int) {
	w = max(w, 4)
	text, col := l.Window(w - 1) // 頭の空白 1 桁のぶん
	if l.Empty() && !on && placeholder != "" {
		return bg(236) + fg(244) + fit(" "+placeholder, w) + sgrReset, 1 + col
	}
	return bg(236) + fg(231) + fit(" "+text, w) + sgrReset, 1 + col
}

// overlayForm は回答フォームの枠を領域の中央に重ねる。入り切らなければ、カーソルの行が見えるよう中身をずらす。
func (m *Model) overlayForm(region []string) []string {
	if m.mode != modeForm {
		return region
	}
	f := &m.form
	width, inner := formWidth(m.width)
	lines := f.formLines(inner)
	room := max(len(region)-2, 1) // 上下の罫線
	if len(lines) > room {
		at := 0
		for i, l := range lines {
			if l.cur {
				at = i
			}
		}
		// カーソルの行と、その後ろ (折り返した説明・欄) が見えるように
		top := min(max(at-room/2, 0), len(lines)-room)
		lines = lines[top : top+room]
	}
	box := formBox(" "+f.cardID+" へ回答 ", lines, width, inner)
	start := min(max((len(region)-len(box))/2, 0), max(len(region)-len(box), 0))
	left := max((m.width-width)/2, 0)
	f.caretOK = false
	for i, l := range lines {
		if l.caret >= 0 {
			f.caretX, f.caretY, f.caretOK = left+2+l.caret, headerRows+start+1+i, true
		}
	}
	out := make([]string, len(region))
	copy(out, region)
	return layout.OverlayCentered(out, box, m.width, len(out), true)
}

// formBox は角の丸いオレンジの罫線で囲む (見本の形)。
func formBox(title string, lines []formLine, width, inner int) []string {
	border := fg(202)
	t := termwidth.Truncate(title, width-4, "…")
	out := []string{border + "╭─" + sgrBold + t + sgrReset + border + strings.Repeat("─", max(width-3-termwidth.Of(t), 0)) + "╮" + sgrReset}
	for _, l := range lines {
		out = append(out, border+"│"+sgrReset+" "+fit(l.text, inner)+sgrReset+" "+border+"│"+sgrReset)
	}
	return append(out, border+"╰"+strings.Repeat("─", width-2)+"╯"+sgrReset)
}

// formCaret は回答フォームの書く欄に置く端末のカーソル (書く欄の行でなければ nil)。
func (m *Model) formCaret() *tea.Cursor {
	if !m.form.caretOK {
		return nil
	}
	return caret.At(m.form.caretX, m.form.caretY, m.width, m.height)
}
