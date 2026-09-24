package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// 作業中・質問待ちのカードがあるとき、q / ctrl+c はすぐ終了せず、中央に確認のダイアログを出す。
// 入力欄・y/N 確認・issue の板の ctrl+c も同じ確認を通る。
func TestQuitAsksWhenCardsAreBusy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
		key   string
	}{
		{"ボードの q", func(*Model) {}, "q"},
		{"ボードの ctrl+c", func(*Model) {}, "ctrl+c"},
		{"入力欄の ctrl+c", func(m *Model) { press(m, "n") }, "ctrl+c"},
		{"issue の板の ctrl+c", func(m *Model) { m.picker.open = true }, "ctrl+c"},
	} {
		m := New(newSpy(), nil)
		tc.setup(m)
		if isQuit(press(m, tc.key)) {
			t.Fatalf("%s: 作業中のカードがあるのに確認なしで終了した", tc.name)
		}
		if !m.quitAsk || !strings.Contains(ansi.Strip(m.render()), "本当に終了しますか") {
			t.Fatalf("%s: 確認のダイアログが出ない", tc.name)
		}
	}
}

// 確認では y / Enter / もう一度の ctrl+c で終了し、それ以外は取り消し (知らないキーで終了しない)。
func TestQuitDialogKeys(t *testing.T) {
	for _, tc := range []struct {
		key  string
		quit bool
	}{{"y", true}, {"enter", true}, {"ctrl+c", true}, {"n", false}, {"esc", false}, {"j", false}, {"Y", false}} {
		m := New(newSpy(), nil)
		press(m, "q")
		got := isQuit(press(m, tc.key))
		if got != tc.quit || m.quitAsk {
			t.Fatalf("確認中の %q: 終了=%v (期待 %v) / まだ確認中=%v", tc.key, got, tc.quit, m.quitAsk)
		}
	}
}

// 動いているカードが無ければ、確認を出さずにすぐ終了する。
func TestQuitImmediatelyWhenIdle(t *testing.T) {
	be := newSpy()
	be.snap.Cards = []card.Card{{ID: "D1", State: card.Done, Ending: card.EndAnswered}, {ID: "P1", State: card.Planned}}
	m := New(be, nil)
	if !isQuit(press(m, "q")) || m.quitAsk {
		t.Fatal("動いているカードが無いのに確認を出した")
	}
}

// ダイアログの文は板の幅で切れない (切れると、何件動いているか・何が起きるかが読めない)。
func TestQuitDialogFitsWithoutTruncation(t *testing.T) {
	m := New(newSpy(), nil)
	press(m, "q")
	out := ansi.Strip(m.render())
	for _, want := range []string{"作業中 1 枚・質問待ち 2 枚", "本当に終了しますか?", "終了すると進み具合は消えます"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ダイアログに %q が切れずに出ていない:\n%s", want, out)
		}
	}
}
