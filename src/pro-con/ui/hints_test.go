package ui

import (
	"strings"
	"testing"

	"pro-con/card"
)

// dimmed は案内の中の項目 text が暗く (押しても効かない色で) 出ているか。見つからなければ落とす。
func dimmed(t *testing.T, m *Model, text string) bool {
	t.Helper()
	for _, h := range m.hints() {
		if strings.Contains(h, text) {
			return strings.HasPrefix(h, fg(239))
		}
	}
	t.Fatalf("案内に %q が無い: %q", text, m.hints())
	return false
}

// カードへの操作は、選んでいるカードで効くかどうかで色が変わる。
func TestHintsDimUnavailableCardActions(t *testing.T) {
	be := newSpy()
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "N1", State: card.Planned, Since: be.snap.Now}) // session も issue も無い
	m := New(be, nil)

	m.selected = "W1" // 質問待ち
	if dimmed(t, m, "r 回答") || dimmed(t, m, "a attach") {
		t.Fatal("質問待ちで session のあるカードなのに r / a が暗い")
	}
	m.selected = "R1" // 作業中
	if !dimmed(t, m, "r 回答") {
		t.Fatal("作業中のカードで r が明るい (押しても回答できない)")
	}
	m.selected = "N1"
	if !dimmed(t, m, "a attach") || !dimmed(t, m, "e issue を開く") || !dimmed(t, m, "y パス") {
		t.Fatal("session も issue も無いカードで a / e / y が明るい")
	}
	if dimmed(t, m, "+ 追加オーダー") || dimmed(t, m, "Y 内容") {
		t.Fatal("カードを選んでいれば + / Y は効くのに暗い")
	}
	if !dimmed(t, m, "x 完了を片付け") {
		t.Fatal("完了のカードが無いのに x が明るい")
	}
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "D1", State: card.Done, Ending: card.EndAnswered})
	m.setSnap(be.snap)
	if dimmed(t, m, "x 完了を片付け") {
		t.Fatal("完了のカードがあるのに x が暗い")
	}
}

// 詳細を開いている間は、案内がスクロールと隣のカードへの送りに切り替わり、カンバンの移動は出さない。
func TestHintsInDrawer(t *testing.T) {
	m, _, clk := drawerModel(t)
	open(t, m, clk)
	line := strings.Join(m.hints(), "  ")
	if !strings.Contains(line, "j / k スクロール") || !strings.Contains(line, "J / K 隣のカード") || strings.Contains(line, "hjkl 選択") {
		t.Fatalf("詳細の案内になっていない: %q", line)
	}
}
