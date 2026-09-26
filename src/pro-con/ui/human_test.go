package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// humanSnap は質問待ちに PM の番の質問 1 枚と権限の確認 1 枚、レビューに人に回したもの 1 枚を置いた画面。
func humanSnap(t *testing.T) *Model {
	t.Helper()
	be := newSpy()
	now := be.snap.Now
	be.snap.Cards = []card.Card{
		{ID: "Q1", State: card.Waiting, Since: now, Wait: card.Wait{Kind: card.WaitQuestion, Question: "?"}},
		{ID: "P1", State: card.Waiting, Since: now, Wait: card.Wait{Kind: card.WaitPermission}},
		{ID: "V1", State: card.Review, Since: now, History: []card.Event{{At: now, Text: card.HandoffText("取り込みの係", "master と衝突")}}},
	}
	return New(be, nil)
}

// 人の番のカードは、バッジ行の先頭に「!人の番」を黄で出す。PM が先に受ける質問は印を付けず、黄にもしない (黄は人の番だけ)。
func TestBadgeMarksHumansTurnOnly(t *testing.T) {
	m := humanSnap(t)
	for _, c := range m.snap.Cards {
		b := m.badgeColored(c)
		human := c.ID != "Q1"
		if got := strings.HasPrefix(ansi.Strip(b), "!人の番 "); got != human {
			t.Errorf("%s: 人の番の印 = %v (期待 %v): %q", c.ID, got, human, ansi.Strip(b))
		}
		if got := strings.Contains(b, sgrYellow); got != human {
			t.Errorf("%s: 黄で出す = %v (期待 %v)", c.ID, got, human)
		}
	}
}

// ゲージと列の見出しに人の番の枚数を出す。PM を起こさないなら、PG の質問も人の番に数える。
func TestGaugeAndHeaderCountHumansTurn(t *testing.T) {
	m := humanSnap(t)
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "!人の番 2") {
		t.Fatalf("ゲージに人の番の枚数が無い: %q", g)
	}
	cols := m.lanes()
	head := func(s card.State) string {
		i := slices.Index(card.Columns, s)
		return ansi.Strip(m.columnBlock(i, s, cols[i], 40, 1, false)[0])
	}
	if h := head(card.Waiting); !strings.Contains(h, "(2) !人 1") {
		t.Fatalf("質問待ちの見出しに人の番の枚数が無い: %q", h)
	}
	if h := head(card.Running); strings.Contains(h, "!人") {
		t.Fatalf("人の番の無い列に印を出した: %q", h)
	}
	m.snap.Roles.PMOff = true
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "!人の番 3") {
		t.Fatalf("PM を起こさないのに PG の質問を人の番に数えない: %q", g)
	}
	if g := ansi.Strip(New(newSpy(), nil).gauge()); strings.Contains(g, "!人") {
		t.Fatalf("人の番が無いのに印を出した: %q", g)
	}
}

// 狭い列 (120 桁の端末で 6 列 = 19 桁) でも、見出しは名前を削って人の番の印を残す。
func TestHeaderKeepsHumanMarkWhenNarrow(t *testing.T) {
	m := humanSnap(t)
	i := slices.Index(card.Columns, card.Waiting)
	if h := ansi.Strip(m.columnBlock(i, card.Waiting, m.lanes()[i], 19, 1, false)[0]); !strings.Contains(h, "!人 1") {
		t.Fatalf("狭い列で人の番の印が切れた: %q", h)
	}
	var many []*card.Card // 人の番が 2 桁でも、列の枚数を削って印を残す
	for range 12 {
		many = append(many, &m.snap.Cards[1])
	}
	if h := ansi.Strip(m.columnBlock(i, card.Waiting, many, 19, 1, false)[0]); !strings.Contains(h, "!人 12") {
		t.Fatalf("人の番が 2 桁で印が切れた: %q", h)
	}
}
