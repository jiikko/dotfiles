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
	order := []string{"未読", "#dev dev-new", "#ops ops-mid", "既読", "#dev dev-old"}
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

// 新着のタブで x で隠しても並べ直さず、隠した行だけを抜く (読んだ話題がまとめて動いてカーソルが無関係な行に乗らない)。
// 話題を選んでいないときの r と d は、前に見ていたチャンネルに効かせず断る。
func TestStreamHideKeepsOrder(t *testing.T) {
	m := streamModel(t,
		[]board.Topic{topicAt("a", day0), topicAt("b", day0.Add(-time.Hour)), topicAt("c", day0.Add(-2*time.Hour))}, nil, map[string]int{})
	run(m, press(m, "tab"))
	run(m, press(m, "tab")) // 新着のタブ (並び: 未読 a b c)
	press(m, "j")           // b を読む (既読になるが並びは動かない)
	press(m, "x")           // b を隠す → カーソルは次の c
	if tp, ok := m.selected(); !ok || tp.Title != "c" {
		t.Errorf("隠した後のカーソルが次の話題に無い: %q\n%s", tp.Title, strings.Join(leftPlain(m), "\n"))
	}
	empty := streamModel(t, nil, nil, map[string]int{})
	run(empty, press(empty, "tab"))
	run(empty, press(empty, "tab")) // 話題の無い新着のタブ
	if cmd := empty.key("r"); cmd != nil || empty.chans[1].loading || !strings.Contains(empty.LastToast(), "選んでから") {
		t.Errorf("話題を選んでいない r が、前に見ていたタブを読み直した: %q", empty.LastToast())
	}
	empty.SetSubscriptions(&Subscriptions{Save: func([]subs.Channel) error { return nil }})
	if empty.key("d"); empty.unsub != "" {
		t.Error("話題を選んでいない d が、前に見ていたタブの購読を外そうとした")
	}
}

// +N は言及も数え、本文の区切りは他の人の発言の前にだけ入れる (自分の発言だけ増えた単位は区切らない)。
func TestStreamNewCountAndMarkRules(t *testing.T) {
	tp := topicAt("x", day0.Add(-time.Hour))
	u := tp.Units[0]
	mark := u.Msgs[0].Ts
	mine := board.Msg{Ts: tsAt(day0), Name: "me", Text: "自分", New: true, Mine: true}
	u.Msgs = append(u.Msgs, mine)
	tp.Units[0] = u
	ms := map[string][]render.Mention{u.ID: {{Unit: u.ID, Ts: tsAt(day0.Add(time.Minute)), Name: "sato"}}}
	if n := newCount(tp, map[string]string{u.ID: mark}, ms); n != 1 {
		t.Errorf("+N = %d, want 1 (他の人の言及 1・自分の発言は数えない)", n)
	}
	p := render.Page{}
	if body := strings.Join(p.BodySince(tp, false, map[string]string{u.ID: mark}), "\n"); strings.Contains(body, strings.TrimSpace(render.NewMark)) {
		t.Errorf("自分の発言だけ増えた単位に区切りを入れた:\n%s", body)
	}
}

// 新着のタブで購読を保存しても、選んでいた話題に戻す (並びが変わって同じ添字の別の話題の本文に替えない)。
// メンバーを変えたグループは、新着のタブにいるならすぐ読み直す (前のメンバーの板を並べ続けない)。
func TestStreamKeepsSelectionAndReloadsChangedGroups(t *testing.T) {
	m := streamModel(t, []board.Topic{topicAt("a", day0.Add(-time.Hour)), topicAt("b", day0.Add(-2*time.Hour)), topicAt("c", day0.Add(-3*time.Hour))},
		[]board.Topic{topicAt("ops", day0)}, map[string]int{})
	run(m, press(m, "tab"))
	run(m, press(m, "tab")) // 新着: ops a b c (b は最後でない: 添字が収まっただけで b に当たらないように)
	press(m, "j")
	press(m, "j") // b
	press(m, "l")
	m.setChannels([]subs.Channel{m.chans[0].ch}) // ops を外す
	if tp, ok := m.selected(); !ok || tp.Title != "b" || !m.body {
		t.Errorf("購読を保存した後に、選んでいた b の本文から替わった: %q body=%v", tp.Title, m.body)
	}

	g := subs.Channel{ID: "grp0123abcd", Name: "times", Members: []subs.Channel{{ID: "C1", Name: "dev"}}}
	m.setChannels([]subs.Channel{m.chans[0].ch, g})
	run(m, m.reload(1))
	g2 := g
	g2.Members = append(g2.Members, subs.Channel{ID: "C2", Name: "ops"})
	if cmd := m.setChannels([]subs.Channel{m.chans[0].ch, g2}); cmd == nil || !m.chans[1].loading {
		t.Error("新着のタブにいるのに、メンバーを変えたグループを読み直さない")
	}
}

// 新着のタブで最後の話題を隠したら、見出しだけを残さない。+N は話題の中の発言から貼られた言及を 2 回数えない。
// 新着のタブにいる間に足したタブも、すぐ読み始める。
func TestStreamRound2(t *testing.T) {
	loads := map[string]int{}
	m := streamModel(t, []board.Topic{topicAt("a", day0)}, nil, loads)
	run(m, press(m, "tab"))
	run(m, press(m, "tab")) // 新着: 未読 a (ops は話題なし)
	press(m, "x")
	for _, r := range m.rows() {
		if r.header() {
			t.Errorf("話題の無い見出しが残った: %+v", m.rows())
		}
	}

	tp := topicAt("q", day0.Add(-time.Hour))
	reply := board.Unit{ID: tsAt(day0), Msgs: []board.Msg{{Ts: tsAt(day0), Name: "sato", Text: "これ", New: true}}}
	tp.Units = append(tp.Units, reply)
	ms := map[string][]render.Mention{tp.Units[0].ID: {{Unit: tp.Units[0].ID, Ts: reply.Msgs[0].Ts, Name: "sato"}}}
	read := map[string]string{tp.Units[0].ID: tp.Units[0].Msgs[0].Ts}
	if n := newCount(tp, read, ms); n != 1 {
		t.Errorf("+N = %d, want 1 (話題の中の発言からの言及を 2 回数えた)", n)
	}

	m.SetSubscriptions(&Subscriptions{Save: func([]subs.Channel) error { return nil }})
	m.setChannels([]subs.Channel{m.chans[0].ch, m.chans[1].ch, {ID: "C3", Name: "qa"}})
	if !m.chans[2].loading { // 読み始めた (loading) ことを見る (load は Cmd を走らせたときに呼ばれる)
		t.Errorf("新着のタブにいる間に足したタブを読み始めない: loading=%v", m.chans[2].loading)
	}
}

// 話題の中の発言から貼られた言及は、その発言を既読で数えていないなら数える (● なのに +0 にしない)。
func TestStreamMentionFromReadMessageCounts(t *testing.T) {
	tp := topicAt("q", day0.Add(-time.Hour))
	cite := board.Unit{ID: tsAt(day0), Msgs: []board.Msg{{Ts: tsAt(day0), Name: "sato", Text: "これ", New: true}}}
	tp.Units = append(tp.Units, cite)
	ms := map[string][]render.Mention{tp.Units[0].ID: {{Unit: tp.Units[0].ID, Ts: cite.Msgs[0].Ts, Name: "sato"}}}
	read := map[string]string{tp.Units[0].ID: tp.Units[0].Msgs[0].Ts, cite.ID: cite.Msgs[0].Ts} // 引用した発言は既読
	if !topicUnread(tp, read, ms) || newCount(tp, read, ms) != 1 {
		t.Errorf("未読 %v / +N %d: 言及で未読なら +1", topicUnread(tp, read, ms), newCount(tp, read, ms))
	}
}
