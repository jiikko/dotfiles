package view

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/subs"
)

// manyTopics は話題を 30 件並べたモデルと、次の読み直しで一覧の先頭へ増やす話題の数を決める口を返す。
func manyTopics(t *testing.T) (*Model, func(int), func(time.Duration)) {
	m, addTop, _, _, adv := reorderableTopics(t)
	return m, addTop, adv
}

// reorderableTopics は manyTopics に、次の読み直しで指した番号の話題を板の先頭 (増やした話題より上) へ移す口と、指した番号の話題を板の 8 番目へ (窓の中で下へ) 移す口を足したもの
// (話題は増えない。同じ日の中は板の順に並ぶ)。
func reorderableTopics(t *testing.T) (*Model, func(int), func(int), func(int), func(time.Duration)) {
	t.Helper()
	added, raise, sink := 0, -1, -1
	m := datedModel(t, func() []board.Topic {
		var ts []board.Topic
		for i := range added {
			ts = append(ts, topicAt(fmt.Sprintf("新しい話題%02d", i), day0.Add(time.Duration(added-i)*time.Minute)))
		}
		n := len(ts)
		for i := range 30 {
			ts = append(ts, topicAt(fmt.Sprintf("話題%02d", i), day0.Add(-time.Duration(i)*time.Minute)))
		}
		if raise >= 0 {
			up := ts[n+raise]
			ts = append(ts[:n+raise], ts[n+raise+1:]...)
			ts = append([]board.Topic{up}, ts...)
		}
		if sink >= 0 {
			ts2 := ts[n+sink]
			ts = append(ts[:n+sink], ts[n+sink+1:]...)
			ts = append(ts[:n+8], append([]board.Topic{ts2}, ts[n+8:]...)...)
		}
		return ts
	})
	m.toast.Clear()
	adv := clockAt(m, day0)
	return m, func(n int) { added = n }, func(i int) { raise = i }, func(i int) { sink = i }, adv
}

// cursorAt はカーソルの話題の題と、それが窓の何行目に出ているか。
func cursorAt(m *Model) (string, int) {
	tp, _ := m.selected()
	return tp.Title, m.list.Cursor - m.list.Offset
}

// 一覧を下へ送って見ている間に読み直しで上へ話題が増えても、カーソルは同じ話題のまま、同じ画面の行に出る
// (窓を作り直すと、カーソルが窓の一番下へ寄って一覧がスクロールしたように見えた)。
func TestReloadKeepsCursorRowWhileScrolled(t *testing.T) {
	m, addTop, _ := manyTopics(t)
	for range 20 {
		press(m, "j")
	}
	for range 8 {
		press(m, "k") // カーソルを窓の上の方へ
	}
	m.View()
	title, row := cursorAt(m)
	if m.list.Offset == 0 {
		t.Fatalf("前提: 一覧を下へ送っていない (offset 0)")
	}
	addTop(1)
	run(m, press(m, "r"))
	m.View()
	if gotT, gotR := cursorAt(m); gotT != title || gotR != row {
		t.Errorf("読み直しでカーソルが動いた: %s の %d 行目 → %s の %d 行目", title, row, gotT, gotR)
	}
	if m.drop.active {
		t.Error("下へ送って見ている間に、降らせる演出が始まった (増えた話題は窓の上の外に積む)")
	}
}

// 一覧の先頭を見ている間に上へ話題が増えたら、読み直しの直後は見え方を変えず、増えた行を listDropStep ごとに 1 行ずつ上から降らせ、
// 先頭に着地する。カーソルはずっと同じ話題。
func TestReloadDropsNewTopicsFromTop(t *testing.T) {
	m, addTop, adv := manyTopics(t)
	press(m, "j")
	press(m, "j")
	m.View()
	title, row := cursorAt(m)
	addTop(2)
	run(m, press(m, "r"))
	m.View()
	if gotT, gotR := cursorAt(m); gotT != title || gotR != row {
		t.Fatalf("読み直しの直後に見え方が変わった: %s の %d 行目 → %s の %d 行目", title, row, gotT, gotR)
	}
	if !m.drop.active || m.list.Offset != 2 {
		t.Fatalf("降らせる演出が始まらない (active=%v offset=%d, want offset 2)", m.drop.active, m.list.Offset)
	}
	var offsets []int
	for range 3 {
		adv(listDropStep)
		m.View()
		offsets = append(offsets, m.list.Offset)
		if gotT, _ := cursorAt(m); gotT != title {
			t.Fatalf("降らせている間にカーソルの話題が変わった: %s → %s", title, gotT)
		}
	}
	if fmt.Sprint(offsets) != "[1 0 0]" || m.drop.active {
		t.Errorf("1 行ずつ降りて先頭に着地しない: 窓の位置 %v (active=%v)", offsets, m.drop.active)
	}
	if tp := m.rows()[1]; m.chans[0].page.Topics[tp.topic].Title != "新しい話題00" {
		t.Errorf("着地した一覧の先頭が増えた話題でない: %q", m.chans[0].page.Topics[tp.topic].Title)
	}
}

// 増えた話題が多くても、降らせるのは listDropMax 行まで (超えた分は最初から所定の位置に置く)。カーソルが窓の下へ押し出される分は降らせず、
// カーソルが窓の一番下に残る位置に着地する。
func TestReloadDropIsCapped(t *testing.T) {
	m, addTop, _ := manyTopics(t)
	m.View()
	addTop(12)
	run(m, press(m, "r"))
	if !m.drop.active || m.drop.from-m.drop.to != listDropMax || m.list.Offset != m.drop.from {
		t.Errorf("降らせる行が上限で止まらない: %d 行目から %d 行目へ (want %d 行)", m.drop.from, m.drop.to, listDropMax)
	}
	if _, row := cursorAt(m); row < 0 {
		t.Fatalf("カーソルが窓の上に出た: %d 行目", row)
	}
	m.list.Offset = m.drop.to
	if _, row := cursorAt(m); row >= m.listRows() {
		t.Errorf("着地するとカーソルが窓の下に出る: %d 行目 (窓は %d 行)", row, m.listRows())
	}
}

// 降らせている途中にキーを押すと着地させてからキーを効かせる (動いている窓の上でカーソルを動かさない)。
// 降りる行の数は壁時計で決め、知らせが滑る間の速い tick が何回来ても速くならない。
func TestReloadDropFollowsClockAndYieldsToKeys(t *testing.T) {
	m, addTop, adv := manyTopics(t)
	m.View()
	addTop(3)
	run(m, press(m, "r"))
	for range 4 {
		adv(frameInterval) // 64ms: まだ 1 行も降りない
	}
	if m.list.Offset != 3 {
		t.Errorf("listDropStep より前に降りた (tick の回数で数えている): offset %d", m.list.Offset)
	}
	press(m, "j")
	if m.drop.active || m.list.Offset != 0 {
		t.Errorf("キーを押しても着地しない: active=%v offset %d", m.drop.active, m.list.Offset)
	}
}

// 先頭を見ている間に、話題が増えずに並びだけが変わった (カーソルより下の話題が先頭へ移った) ときは降らせず、窓も先頭に置いたまま
// (窓を下げると、先頭の見出しと移ってきた話題が窓の上の外に隠れる)。カーソルは同じ話題。
func TestReloadDoesNotDropWhenOnlyReordered(t *testing.T) {
	m, _, raise, _, _ := reorderableTopics(t)
	press(m, "j")
	press(m, "j")
	m.View()
	title, _ := cursorAt(m)
	raise(10)
	run(m, press(m, "r"))
	m.View()
	if m.drop.active {
		t.Errorf("話題は増えていないのに降らせる演出が始まった (%d 行目から %d 行目へ)", m.drop.from, m.drop.to)
	}
	if m.list.Offset != 0 {
		t.Errorf("先頭を見ていたのに窓が下がった (offset %d。先頭の見出しが隠れる)", m.list.Offset)
	}
	if got, _ := cursorAt(m); got != title {
		t.Errorf("並びが変わった読み直しでカーソルの話題が変わった: %s → %s", title, got)
	}
}

// カーソルの話題が窓の中で下へ移り、先頭に 1 件増えたときは降らせない (前の見え方に一番近いのは窓を動かさない位置。
// 新しい話題は前の一番上の話題の下に現れる)。
func TestReloadDropCountsOnlyAddedTopics(t *testing.T) {
	for _, h := range []int{20, 30} { // 窓が高いと、窓の下側がそろう位置が照合で勝つ
		m, addTop, _, sinkCursor, _ := reorderableTopics(t)
		m.Update(tea.WindowSizeMsg{Width: 90, Height: h})
		press(m, "j")
		press(m, "j")
		m.View()
		sinkCursor(2) // カーソルの話題 (話題02)
		addTop(1)
		run(m, press(m, "r"))
		if m.drop.active {
			t.Errorf("高さ %d: カーソルの話題が沈んだ分を降らせる (%d 行。元からある行が上へ跳ねてから降りる)", h, m.drop.from-m.drop.to)
		}
	}
}

// 一覧が窓に収まるほど短いときは降らせない (描画が窓を先頭へ詰めるので、降らせても見えず tick だけが回る)。
func TestReloadDoesNotDropShortList(t *testing.T) {
	added := 0
	m := datedModel(t, func() []board.Topic {
		var ts []board.Topic
		for i := range added {
			ts = append(ts, topicAt(fmt.Sprintf("新しい話題%02d", i), day0.Add(time.Duration(added-i)*time.Minute)))
		}
		return append(ts, topicAt("a", day0), topicAt("b", day0.Add(-time.Minute)))
	})
	clockAt(m, day0)
	m.View()
	added = 2
	run(m, press(m, "r"))
	if m.drop.active {
		t.Errorf("窓に収まる一覧で降らせる演出が始まった (%d 行目から %d 行目へ)", m.drop.from, m.drop.to)
	}
}

// 新着のタブでも、読み直しで先頭に増えた話題を上から降らせ、カーソルは同じ話題に置く。
func TestStreamReloadDropsNewTopics(t *testing.T) {
	m, addTop, _ := manyTopics(t)
	press(m, "tab") // 新着のタブ
	m.View()
	if !m.stream {
		t.Fatal("前提: 新着のタブでない")
	}
	title, _ := cursorAt(m)
	addTop(2)
	run(m, m.reload(0))
	m.View()
	if !m.drop.active || m.drop.from-m.drop.to != 2 {
		t.Errorf("新着のタブで先頭に増えた 2 件を降らせない (active=%v %d 行)", m.drop.active, m.drop.from-m.drop.to)
	}
	if got, _ := cursorAt(m); got != title {
		t.Errorf("新着のタブの読み直しでカーソルの話題が変わった: %s → %s", title, got)
	}
}

// 降らせている途中に (キーを押さずに) 次の読み直しが届いても、演出を途中で止めず先頭に着地する
// (途中の窓の位置を「下へ送って見ている」と読むと、増えた話題が窓の上の外に隠れたまま止まる)。
func TestReloadDuringDropStillLands(t *testing.T) {
	m, addTop, adv := manyTopics(t)
	press(m, "j")
	m.View()
	addTop(3)
	run(m, press(m, "r"))
	adv(listDropStep)
	if !m.drop.active || m.list.Offset == 0 {
		t.Fatalf("前提: 降らせている途中でない (active=%v offset %d)", m.drop.active, m.list.Offset)
	}
	run(m, m.reload(0)) // キーを押さずに届く読み直し (新着のタブで別のタブの読み直しが届く・起動のときの読み直し)
	m.View()
	if m.list.Offset != 0 {
		t.Errorf("降らせている途中の読み直しで、先頭に着地しない: offset %d (active=%v)", m.list.Offset, m.drop.active)
	}
}

// 降らせている途中に窓の大きさが変わったら着地させる (着地の位置は変わる前の窓の行数で決めた)。カーソルを動かす reselect
// (起動のときに前回の話題へ戻す等) も着地させる (前のカーソルで決めた着地の位置へ降らせ続けず、途中の位置にも止めない)。
func TestReloadDropStopsOnResizeAndReselect(t *testing.T) {
	m, addTop, _ := manyTopics(t)
	m.View()
	addTop(3)
	run(m, press(m, "r"))
	if !m.drop.active {
		t.Fatal("前提: 降らせていない")
	}
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	if m.drop.active {
		t.Error("窓の大きさが変わっても降らせ続ける")
	}
	addTop(6)
	run(m, press(m, "r"))
	if !m.drop.active {
		t.Fatal("前提: 2 回目も降らせていない")
	}
	m.reselect(m.selKey())
	if m.drop.active || m.list.Offset != m.drop.to {
		t.Errorf("reselect でカーソルを置き直しても、着地せずに止まる (active=%v offset %d, 着地は %d)", m.drop.active, m.list.Offset, m.drop.to)
	}
}

// reloadBoard は読み直しのたびに中身を変えられる板 (チャンネルごと)。元からある話題は 30 件。
type reloadBoard struct {
	added map[string]int // チャンネル → 先頭に増やす話題の数
	gone  map[string]int // チャンネル → 消す話題の番号
	day   time.Time      // 元からある話題の日
}

func boardModel(t *testing.T, chans ...string) (*Model, *reloadBoard, func(time.Duration)) {
	t.Helper()
	b := &reloadBoard{added: map[string]int{}, gone: map[string]int{}, day: day0}
	load := func(_ context.Context, ch subs.Channel) (render.Page, error) {
		var ts []board.Topic
		for i := range b.added[ch.ID] {
			ts = append(ts, topicAt(fmt.Sprintf("%s-新しい話題%02d", ch.ID, i), day0.Add(time.Duration(b.added[ch.ID]-i)*time.Minute)))
		}
		for i := range 30 {
			if g, ok := b.gone[ch.ID]; ok && g == i {
				continue
			}
			ts = append(ts, topicAt(fmt.Sprintf("%s-話題%02d", ch.ID, i), b.day.Add(-time.Duration(i)*time.Minute)))
		}
		return render.Page{Channel: ch.ID, ChannelName: ch.ID, Since: day0, Until: day0, Lookback: day0, Topics: ts}, nil
	}
	var cs []subs.Channel
	for _, c := range chans {
		cs = append(cs, subs.Channel{ID: c, Name: c})
	}
	m := quiet(New(context.Background(), cs, load, nil, func() time.Time { return day0 }))
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	run(m, m.Init())
	m.toast.Clear()
	return m, b, clockAt(m, day0)
}

// 先頭を見ていてもカーソルが窓の一番下にいて、先頭に増えた行で着地の位置が下がるときは、降らせずに着地の位置へ置く
// (着地の位置より上から降らせ始めると、最初の 1 枚で中身が上へ跳び、降ってくるのは元からある話題になる)。
func TestReloadDoesNotJumpWhenCursorNearBottom(t *testing.T) {
	m, addTop, _ := manyTopics(t)
	for range 9 {
		press(m, "j")
	}
	m.View()
	if m.list.Offset != 0 || m.list.Cursor-m.list.Offset != m.listRows()-1 {
		t.Fatalf("前提: 先頭を見ていてカーソルが窓の一番下にない (cursor %d offset %d)", m.list.Cursor, m.list.Offset)
	}
	addTop(2)
	run(m, press(m, "r"))
	if m.drop.active || m.list.Offset != m.list.Cursor-m.listRows()+1 {
		t.Errorf("上へ跳んでから降らせる (active=%v %d 行目から %d 行目へ・offset %d)", m.drop.active, m.drop.from, m.drop.to, m.list.Offset)
	}
}

// 日が替わって「今日」の見出しと新しい話題が先頭に増えても、読み直しの直後は前と同じ見え方から降らせ始める (見出しの行も数える)。
func TestReloadDropCountsNewHeaderRows(t *testing.T) {
	m, b, _ := boardModel(t, "C1")
	b.day = day0.Add(-24 * time.Hour)
	run(m, press(m, "r"))
	m.toast.Clear()
	m.View()
	before := pane(m)
	b.added["C1"] = 2
	run(m, press(m, "r"))
	m.toast.Clear()
	if !m.drop.active {
		t.Fatal("前提: 降らせていない")
	}
	if got := pane(m); strings.Join(got, "\n") != strings.Join(before, "\n") {
		t.Errorf("読み直しの直後の見え方が前と違う (増えた見出しの行を数えていない):\n%s\n---\n%s", strings.Join(before, "\n"), strings.Join(got, "\n"))
	}
}

// 別のタブの読み直しが届いても、今のタブで降らせている演出は着地させない (窓を控えるのは今見ている一覧が組み直されるときだけ)。
func TestOtherTabReloadKeepsDrop(t *testing.T) {
	m, b, _ := boardModel(t, "C1", "C2")
	m.View()
	b.added["C1"] = 2
	run(m, press(m, "r"))
	if !m.drop.active {
		t.Fatal("前提: 降らせていない")
	}
	off := m.list.Offset
	run(m, m.reload(1))
	if !m.drop.active || m.list.Offset != off {
		t.Errorf("別のタブの読み直しで、降らせている演出が着地した (active=%v offset %d → %d)", m.drop.active, off, m.list.Offset)
	}
}

// 新着のタブで見ていた話題が読み直しで消えても、カーソルは先頭へ戻さない (前から行の番号の付近に残していた)。
func TestStreamKeepsCursorWhenTopicGone(t *testing.T) {
	m, b, _ := boardModel(t, "C1")
	press(m, "tab")
	if !m.stream {
		t.Fatal("前提: 新着のタブでない")
	}
	for range 5 {
		press(m, "j")
	}
	m.View()
	tp, _ := m.selected()
	cur := m.list.Cursor
	var gone int
	if _, err := fmt.Sscanf(tp.Title, "C1-話題%02d", &gone); err != nil {
		t.Fatalf("前提: カーソルの話題 %q", tp.Title)
	}
	b.gone["C1"] = gone
	run(m, m.reload(0))
	if m.list.Cursor != cur {
		t.Errorf("見ていた話題が消えたら、カーソルが %d から %d へ動いた", cur, m.list.Cursor)
	}
}

// 窓の中の下にあった話題が先頭へ移り、同時に新しい話題が先頭に増えたときも、読み直しの直後は前の見え方 (カーソルの話題の画面の行) から、
// 先頭に入った行 (新しい話題と上へ移ってきた話題) を降らせる。上へ移ってきた話題は最初の 1 枚で元の行より上に出るが、着地でも上なので
// 跳ね返りに数えない。移った話題より下の行が窓の中にあると、その行は跳ね返るので降らせない (TestReloadDoesNotDropWhenSomeRowsWouldBounce)。
func TestReloadDropsWhenAddedWithReorder(t *testing.T) {
	m, addTop, raise, _, _ := reorderableTopics(t)
	m.View()
	title, row := cursorAt(m)
	raise(9) // 前の窓の一番下の行 (それより下の行は窓の外なので、跳ね返る行が無い)
	addTop(1)
	run(m, press(m, "r"))
	if !m.drop.active {
		t.Fatalf("先頭に 1 件増えたのに降らせない (上へ移ってきた話題を跳ね返る行に数えた? %d 行目から %d 行目へ)", m.drop.from, m.drop.to)
	}
	// 窓の一番上の見出しの行は残せない (見出しの下に行が増えるので、窓の位置をずらすと見出しが窓の上へ出る)。それより下は前と同じ
	if gotT, gotR := cursorAt(m); gotT != title || gotR != row {
		t.Errorf("読み直しの直後の見え方が前と違う: %s の %d 行目 → %s の %d 行目", title, row, gotT, gotR)
	}
}

// 一番上の話題が並び替えで下へ沈み、同時に新しい話題が先頭に 1 件増えたら降らせない (新しい話題は沈んだ話題の場所に現れる。
// 降らせると、元からある行が上へ跳ねてから降りる。新着のタブで一番上の話題を読んで既読の組へ移した後に新しい話題が
// 届く・分類し直しで要対応が外れる、が同じ形)。
func TestReloadDoesNotCountSunkTopTopic(t *testing.T) {
	for _, h := range []int{20, 30} { // 窓が高いと、窓の下側がそろう位置が照合で勝つ
		m, addTop, _, sink, _ := reorderableTopics(t)
		m.Update(tea.WindowSizeMsg{Width: 90, Height: h})
		for range 3 {
			press(m, "j") // カーソルは話題03 (一番上の話題00 とは別)
		}
		m.View()
		sink(0)
		addTop(1)
		run(m, press(m, "r"))
		if m.drop.active {
			t.Errorf("高さ %d: 一番上の話題が沈んだ分を降らせる (%d 行。元からある行が上へ跳ねてから降りる)", h, m.drop.from-m.drop.to)
		}
	}
}

// 見出しの下に新しい話題が 1 件だけ増えても降らせる (新しい話題は最初の 1 枚で窓の一番上に出て、降らせて現れるのは見出しだけになる。
// 降ってくる行だけを見ると新しい話題が無いと読んで降らせない。読み直しで一番よく起きる形)。
func TestReloadDropsSingleNewTopic(t *testing.T) {
	for _, h := range []int{20, 30} {
		m, addTop, _ := manyTopics(t)
		m.Update(tea.WindowSizeMsg{Width: 90, Height: h})
		m.View()
		addTop(1)
		run(m, press(m, "r"))
		if !m.drop.active || m.drop.from-m.drop.to != 1 {
			t.Errorf("高さ %d: 1 件増えたのに 1 行降らせない (active=%v %d 行目から %d 行目へ)", h, m.drop.active, m.drop.from, m.drop.to)
		}
	}
}

// 高い窓で、一番上の話題が窓の中で沈み、同時に 2 件増えると、窓の下側 (2 行ずれた行) がそろう位置が照合で勝つ。そこから降らせると、
// 1 行だけずれた行 (沈んだ話題より上にあった話題) が最初の 1 枚で上へ跳ねてから降りるので、降らせない
// (着地の位置でそろう行だけを見ると、どの行も着地の位置でそろわないので素通りする)。
func TestReloadDoesNotDropWhenSomeRowsWouldBounce(t *testing.T) {
	m, addTop, _, sink, _ := reorderableTopics(t)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	for range 3 {
		press(m, "j")
	}
	m.View()
	sink(0)
	addTop(2)
	run(m, press(m, "r"))
	if m.drop.active {
		t.Errorf("元からある行が上へ跳ねてから降りる形で %d 行降らせる (%d 行目から %d 行目へ)", m.drop.from-m.drop.to, m.drop.from, m.drop.to)
	}
}

// 一番上に残る話題 (その日の要対応・返信が続くスレッド) の下に新しい話題が 2 件以上入っても、降らせない。降らせると、その話題は
// 最初の 1 枚で窓の上に隠れてから元の行へ降りてくる (跳ね返る。最初の 1 枚に見えない行も数える)。
func TestReloadDoesNotDropUnderKeptTopTopic(t *testing.T) {
	added := 0
	m := datedModel(t, func() []board.Topic {
		ts := []board.Topic{topicAt("上に残る話題", day0.Add(-time.Second))}
		for i := range added {
			ts = append(ts, topicAt(fmt.Sprintf("新しい話題%02d", i), day0.Add(time.Duration(added-i)*time.Minute)))
		}
		for i := range 30 {
			ts = append(ts, topicAt(fmt.Sprintf("話題%02d", i), day0.Add(-time.Duration(i+1)*time.Minute)))
		}
		return ts
	})
	m.toast.Clear()
	clockAt(m, day0)
	press(m, "j")
	press(m, "j") // カーソルは上に残る話題より下 (話題00)
	m.View()
	added = 2
	run(m, press(m, "r"))
	if m.drop.active {
		t.Errorf("上に残る話題が隠れてから降りてくる形で %d 行降らせる (%d 行目から %d 行目へ)", m.drop.from-m.drop.to, m.drop.from, m.drop.to)
	}
}

// 先頭でない見出し (昨日の見出し) が最初の 1 枚で窓の上に隠れてから降りてくる形は降らせない (隠れる見出しで飛ばすのは、前の窓の
// 一番上の見出しだけ)。今日の唯一の話題が消えて新しい話題に替わり、昨日の組の先頭に話題が 3 件入ると、昨日の話題がそろう位置が照合で勝つ。
func TestReloadDoesNotDropWhenHiddenHeaderBounces(t *testing.T) {
	after := false
	yesterday := day0.Add(-24 * time.Hour)
	m := datedModel(t, func() []board.Topic {
		var ts []board.Topic
		if after {
			ts = append(ts, topicAt("今日の新しい話題", day0))
			for i := range 3 {
				ts = append(ts, topicAt(fmt.Sprintf("昨日の新しい話題%d", i), yesterday.Add(time.Duration(3-i)*time.Minute)))
			}
		} else {
			ts = append(ts, topicAt("今日の話題", day0.Add(-time.Second)))
		}
		for i := range 30 {
			ts = append(ts, topicAt(fmt.Sprintf("昨日の話題%02d", i), yesterday.Add(-time.Duration(i)*time.Minute)))
		}
		return ts
	})
	m.toast.Clear()
	clockAt(m, day0)
	for range 4 {
		press(m, "j") // カーソルは昨日の話題
	}
	m.View()
	after = true
	run(m, press(m, "r"))
	if m.drop.active {
		t.Errorf("昨日の見出しが隠れてから降りてくる形で %d 行降らせる (%d 行目から %d 行目へ)", m.drop.from-m.drop.to, m.drop.from, m.drop.to)
	}
}

// 上限を超えて増えたときの跳ね返りの判定は、上限で切った位置 (実際の最初の 1 枚) で行う。12 件増えて同時に一番上の話題が沈むと、
// 切る前の位置では沈んだ話題より下の行が上へ出るが、実際の最初の 1 枚では下へ動くだけなので降らせる。
func TestReloadJudgesBounceAtCappedStart(t *testing.T) {
	m, addTop, _, sink, _ := reorderableTopics(t)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30}) // 窓が高いと、照合で一番そろう位置が上限 (着地 + 8) を超える
	press(m, "j")
	press(m, "j") // カーソルは話題02 (沈める話題00 とは別)
	m.View()
	sink(0)
	addTop(12)
	if m.drop.active {
		t.Fatal("前提: 読み直しの前に降らせている")
	}
	run(m, press(m, "r"))
	if !m.drop.active {
		t.Errorf("上限で切った位置では跳ね返らないのに降らせない (%d 行目から %d 行目へ)", m.drop.from, m.drop.to)
	}
}
