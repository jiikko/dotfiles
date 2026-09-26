package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
)

// newFormSpy は W2 を選択肢つきの質問にした spy (問 1 は 1 つ選ぶ・推奨あり、問 2 は複数選べる・推奨あり)。
func newFormSpy() *spy {
	be := newSpy()
	w, err := card.AskWait("2 点決めてください", []card.Question{
		{Question: "形", Header: "形", Options: []card.Option{{Label: "丸", Recommended: true}, {Label: "角"}}},
		{Question: "直すもの", MultiSelect: true, Options: []card.Option{{Label: "色", Recommended: true}, {Label: "幅"}}},
	})
	if err != nil {
		panic(err)
	}
	be.snap.Cards[2].Wait = w
	return be
}

func formKey(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	return key(s)
}

func pressForm(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(formKey(k))
	}
}

func openForm(t *testing.T, be *spy) *Model {
	t.Helper()
	m := New(be, nil)
	press(m, "right", "down", "r") // 質問待ちの W2
	if m.mode != modeForm {
		t.Fatalf("選択肢つきの質問で回答フォームが開かない: mode=%v", m.mode)
	}
	return m
}

func sentAnswer(t *testing.T, be *spy) backend.Answer {
	t.Helper()
	if len(be.applied) != 1 {
		t.Fatalf("backend に届いた操作は 1 つのはず: %#v", be.applied)
	}
	a, ok := be.applied[0].(backend.Answer)
	if !ok || a.CardID != "W2" || a.From != "人間" {
		t.Fatalf("届いた回答が違う: %#v", be.applied[0])
	}
	return a
}

// 推奨は初めから選んである: 開いてすぐ enter で、推奨のままの答えが問いごとの行で届く。
func TestAnswerFormSendsRecommendedAsIs(t *testing.T) {
	be := newFormSpy()
	m := openForm(t, be)
	press(m, "enter")
	if a := sentAnswer(t, be); a.Text != "1. 形: 丸\n2. 直すもの: 色" || m.mode != modeBoard {
		t.Fatalf("推奨のままの答えが違う: %q (mode=%v)", a.Text, m.mode)
	}
}

// j / space で radio を選び直し、tab で次の問いへ移って checkbox を足す / 外す。「その他」と補足に書いた文も答えに入る。
func TestAnswerFormSelectsTypesAndSends(t *testing.T) {
	be := newFormSpy()
	m := openForm(t, be)
	pressForm(m, "j", "space")   // 問 1: 丸 → 角
	pressForm(m, "tab", "space") // 問 2: 色を外す
	pressForm(m, "j", "space")   // 問 2: 幅を選ぶ
	pressForm(m, "j")            // 問 2 のその他の欄
	typeText(m, "余白 j k")        // 書く欄では j / k / 空白も文字
	pressForm(m, "tab")          // 補足
	typeText(m, "急がない")
	press(m, "enter")
	if a := sentAnswer(t, be); a.Text != "1. 形: 角\n2. 直すもの: 幅 / その他: 余白 j k\n補足: 急がない" {
		t.Fatalf("答えの文が違う: %q", a.Text)
	}
}

// 1 つ選ぶ問いの「その他」に書くと、その他を選んだことになる (選んでいた推奨は外れる)。
func TestAnswerFormOtherReplacesRadioChoice(t *testing.T) {
	be := newFormSpy()
	m := openForm(t, be)
	pressForm(m, "j", "j") // 問 1 のその他
	typeText(m, "楕円")
	press(m, "enter")
	if a := sentAnswer(t, be); a.Text != "1. 形: その他: 楕円\n2. 直すもの: 色" {
		t.Fatalf("その他が選ばれていない: %q", a.Text)
	}
}

// 答えていない問いがあれば送らず、フォームを開いたまま、その問いへカーソルを移す。esc は何も送らずに閉じる。
func TestAnswerFormRefusesUnansweredAndEscCancels(t *testing.T) {
	be := newFormSpy()
	m := openForm(t, be)
	pressForm(m, "tab", "space") // 問 2 の推奨 (色) を外す → 問 2 が空
	pressForm(m, "up", "up")     // 問 1 へ戻っておく
	press(m, "enter")
	if len(be.applied) != 0 || m.mode != modeForm {
		t.Fatalf("答えていない問いがあるのに送った / 閉じた: %#v mode=%v", be.applied, m.mode)
	}
	if r := m.form.row(); r.q != 1 {
		t.Fatalf("答えていない問い (問 2) へ移っていない: %+v", r)
	}
	press(m, "esc")
	if len(be.applied) != 0 || m.mode != modeBoard {
		t.Fatalf("esc で送った / 閉じない: %#v mode=%v", be.applied, m.mode)
	}
}

// 選択肢の無い質問は今までどおり 1 行の入力欄 (フォームは選択肢つきの質問だけ)。
func TestPlainQuestionStillUsesInputLine(t *testing.T) {
	m := New(newFormSpy(), nil)
	press(m, "right", "r") // W1 は自由文の質問
	if m.mode != modeInput {
		t.Fatalf("自由文の質問で 1 行の入力欄が開かない: mode=%v", m.mode)
	}
}

// 枠は画面の中央に出て、問い・推奨の印・選んだ印が見える。書く欄に居るときは端末のカーソルをそこへ置く (IME)。
func TestAnswerFormRendersAndPlacesCaret(t *testing.T) {
	be := newFormSpy()
	m := openForm(t, be)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := m.render()
	for _, want := range []string{"W2 へ回答", "2 点決めてください", "1. 形", "(•) 丸 推奨", "[x] 色 推奨", "( ) 角", "その他 (自由に書く)", "補足 (任意)"} {
		if !strings.Contains(ansi.Strip(out), want) {
			t.Errorf("フォームに %q が無い", want)
		}
	}
	if m.caret() != nil {
		t.Fatal("選択肢の行なのに端末のカーソルを出している")
	}
	pressForm(m, "tab", "tab") // 補足の欄
	typeText(m, "あ")
	m.render()
	c := m.caret()
	if c == nil {
		t.Fatal("書く欄に居るのに端末のカーソルが無い")
	}
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	if row := lines[c.Y]; !strings.Contains(row, "あ") {
		t.Fatalf("カーソルの行 %d が補足の欄でない: %q", c.Y, row)
	}
}

// 「その他」に空白だけ書いても選んだことにしない: 印は付かず、1 つ選ぶ問いの選択も変えない (表示と送る答えを同じ基準で決める)。
func TestAnswerFormBlankOtherIsNotChosen(t *testing.T) {
	be := newFormSpy()
	m := openForm(t, be)
	pressForm(m, "j", "j") // 問 1 のその他
	typeText(m, " ")
	pressForm(m, "tab", "j", "j") // 問 2 のその他
	typeText(m, "  ")
	for _, l := range m.form.formLines(76) {
		if s := ansi.Strip(l.text); strings.Contains(s, "その他") && (strings.Contains(s, "(•)") || strings.Contains(s, "[x]")) {
			t.Fatalf("空白だけのその他に選んだ印が付いた: %q", s)
		}
	}
	press(m, "enter")
	if a := sentAnswer(t, be); a.Text != "1. 形: 丸\n2. 直すもの: 色" {
		t.Fatalf("空白だけのその他で答えが変わった: %q", a.Text)
	}
}
