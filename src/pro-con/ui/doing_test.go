package ui

import (
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// doingModel は作業中の R1 に、dispatcher が collected に集めた様子 (裏の shell とその子) を持たせる。
func doingModel(t *testing.T, collected time.Time) (*Model, *clock) {
	t.Helper()
	be := newSpy()
	now := be.snap.Now
	for i := range be.snap.Cards {
		if be.snap.Cards[i].ID == "R1" {
			be.snap.Cards[i].Since = now.Add(-23 * time.Minute)
			be.snap.Cards[i].DoingAt = collected
			be.snap.Cards[i].Doing = []card.Doing{
				{Kind: card.DoingProcess, Text: "bash /j/tmp/mut.sh", Since: now.Add(-13 * time.Minute)},
				{Kind: card.DoingProcess, Text: "bin/mutate-verify --name hints-ro", Since: now.Add(-2 * time.Minute), Depth: 1},
				{Kind: card.DoingAgent, Text: "敵対的レビュー", Since: now.Add(-5 * time.Minute)},
			}
		}
	}
	clk := &clock{t: now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 160, 40
	m.selected = "R1"
	return m, clk
}

// ボードのカードに、PG が直接起こしたもののうちいちばん長く走っているものを 1 行で出す。詳細には木の形で全部出す。
func TestDoingShownOnBoardAndDrawer(t *testing.T) {
	m, clk := doingModel(t, time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	if out := screen(m); !strings.Contains(out, "23分 ▸ mut.sh 13分") {
		t.Fatalf("ボードのカードに今走っているものの 1 行が無い:\n%s", out)
	}
	press(m, "enter")
	clk.t = clk.t.Add(drawerDuration)
	m.onFrame()
	out := screen(m)
	for _, want := range []string{"今走っているもの (dispatcher が集めた様子)", "13分  bash /j/tmp/mut.sh", "    2分  bin/mutate-verify --name hints-ro", "5分  サブエージェント: 敵対的レビュー"} {
		if !strings.Contains(out, want) {
			t.Fatalf("詳細に %q が無い:\n%s", want, out)
		}
	}
}

// 集め直されていない古い様子は、ボードには出さず、詳細では古いと添えて出す (終わったものを走っていると見せ続けない)。
func TestStaleDoingHiddenOnBoard(t *testing.T) {
	m, clk := doingModel(t, time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC).Add(-card.DoingStale-time.Second))
	if out := screen(m); strings.Contains(out, "▸ mut.sh") {
		t.Fatalf("古い様子をボードに出した:\n%s", out)
	}
	press(m, "enter")
	clk.t = clk.t.Add(drawerDuration)
	m.onFrame()
	if out := screen(m); !strings.Contains(out, "1分前に集めたまま") || !strings.Contains(out, "bash /j/tmp/mut.sh") {
		t.Fatalf("詳細は古いと添えて出すはず:\n%s", out)
	}
}
