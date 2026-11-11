package view

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/subs"
)

// streamModel は 2 つのチャンネルのタブを持つ画面。dev は読み込み済み、ops は loadOps が呼ばれるまで板が無い。
func streamModel(t *testing.T, dev, ops []board.Topic, loads map[string]int) *Model {
	t.Helper()
	load := func(_ context.Context, ch subs.Channel) (render.Page, error) {
		loads[ch.ID]++
		ts := dev
		if ch.ID == "C2" {
			ts = ops
		}
		return render.Page{Channel: ch.ID, ChannelName: ch.Name, Since: day0, Until: day0, Lookback: day0, Topics: ts}, nil
	}
	m := quiet(New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}}, load, nil, func() time.Time { return day0 }))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	run(m, m.Init())
	return m
}

// 新着のタブ (タブの最後) は、全部のタブの話題を未読と既読に分け、それぞれ新しい順に並べ、どのタブの話題かを添える。
// 板がまだ無いタブは開いたときに読み始める。選んだ話題は元のタブの既読の印に書き、選んでも並びは動かさない (カーソルが跳ばない)。
func TestStreamAcrossTabs(t *testing.T) {
	loads := map[string]int{}
	m := streamModel(t,
		[]board.Topic{topicAt("dev-old", day0.Add(-2*time.Hour)), topicAt("dev-new", day0)},
		[]board.Topic{topicAt("ops-mid", day0.Add(-time.Hour))}, loads)
	m.chans[0].read[m.chans[0].page.Topics[0].Units[0].ID] = tsAt(day0) // dev-old は既読
	m.chans[0].unread = nil
	run(m, press(m, "tab")) // ops を読む
	run(m, press(m, "tab")) // 新着のタブ
	if !m.stream || !strings.Contains(sgrRe.ReplaceAllString(m.tabsLine(), ""), "🆕 新着  未読 2") {
		t.Fatalf("新着のタブに入っていない: %q", m.tabsLine())
	}
	got := strings.Join(leftPlain(m), "\n")
	order := []string{"未読 2", "#dev dev-new", "#ops ops-mid", "既読", "#dev dev-old"}
	at := 0
	for _, w := range order {
		i := strings.Index(got[at:], w)
		if i < 0 {
			t.Fatalf("新着の並びに %q が順に無い:\n%s", w, got)
		}
		at += i + len(w)
	}
	press(m, "u") // 先頭の dev-new を既読に (今のタブは最後に開いた ops。印は dev に書く)
	if len(m.chans[0].read) != 2 || len(m.chans[1].read) != 0 {
		t.Fatalf("新着のタブで既読にした話題を、元のタブでなく今のタブの印に書いた: dev=%v ops=%v", m.chans[0].read, m.chans[1].read)
	}
	press(m, "j") // ops-mid を選ぶ → ops の既読
	if tp, ok := m.selected(); !ok || tp.Title != "ops-mid" || len(m.chans[1].read) != 1 {
		t.Fatalf("選んだ話題を元のタブの既読に書いていない: %q read=%v", tp.Title, m.chans[1].read)
	}
	if again := strings.Join(leftPlain(m), "\n"); strings.Index(again, "ops-mid") > strings.Index(again, "既読") {
		t.Errorf("選んだだけで並べ直した (カーソルが跳ぶ):\n%s", again)
	}

	loads2 := map[string]int{}
	m2 := streamModel(t, []board.Topic{topicAt("a", day0)}, []board.Topic{topicAt("b", day0)}, loads2)
	run(m2, press(m2, "shift+tab")) // 最初のタブから戻ると新着のタブ
	if !m2.stream || loads2["C2"] != 1 {
		t.Errorf("新着のタブを開いても、板の無いタブを読み始めない: stream=%v loads=%v", m2.stream, loads2)
	}
}

// 前に読んだ後に増えた発言は、新着のタブで「+N」と数え、本文では「ここから新着」の区切りの後に出す (チャンネルのタブの本文も同じ)。
func TestStreamShowsWhatIsNew(t *testing.T) {
	tp := topicAt("障害", day0.Add(-time.Hour))
	u := &tp.Units[0]
	old := u.Msgs[0].Ts
	u.Thread = true
	u.Msgs = append(u.Msgs, board.Msg{Ts: tsAt(day0), Name: "sato", Text: "直りました", New: true})
	m := streamModel(t, []board.Topic{tp}, nil, map[string]int{})
	m.chans[0].read[u.ID] = old // 親の発言までは読んだ
	m.chans[0].unread = nil
	run(m, press(m, "tab"))
	run(m, press(m, "tab")) // 新着のタブ
	if got := strings.Join(leftPlain(m), "\n"); !strings.Contains(got, "+1") {
		t.Fatalf("増えた件数が出ない:\n%s", got)
	}
	press(m, "l")
	lines, _ := m.bodyLines()
	body := strings.Join(lines, "\n")
	mark := strings.Index(body, strings.TrimSpace(render.NewMark))
	if mark < 0 || mark > strings.Index(body, "直りました") || mark < strings.Index(body, "障害") {
		t.Errorf("「ここから新着」の区切りが、前に読んだ所と増えた発言の間に無い:\n%s", body)
	}
}
