package view

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"github.com/jiikko/dotfiles/src/tuikit/tickchain"
	"slack-bbs/board"
	"slack-bbs/subs"
)

// bodyPlain は右の欄の中身の行 (色と枠の縦線を外す。上下の枠線は除く)。
func bodyPlain(m *Model) []string {
	var out []string
	for _, l := range m.rightLines()[1 : 1+m.bodyRows()] {
		l = sgrRe.ReplaceAllString(l, "")
		r := []rune(l)
		out = append(out, string(r[1:len(r)-1])) // 左右の枠の縦線 (┃ か │) を 1 文字ずつ外す。中身の右端の │ はバーの溝
	}
	return out
}

// lastCell は行の最後の 1 文字 (bodyPlain の行ではスクロールバーの列)。
func lastCell(l string) string {
	r := []rune(l)
	if len(r) == 0 {
		return ""
	}
	return string(r[len(r)-1])
}

// 本文が欄に収まらないときだけ、右端にスクロールバー (溝 │ と つまみ █) が出る。末尾まで送るとつまみが下端に付く。
// 本文の行はバーの桁を引いた幅で折り返すので、バーを足しても行は切れない (… に化けない)。
func TestBodyHasScrollbarOnlyWhenOverflowing(t *testing.T) {
	m := datedModel(t, func() []board.Topic {
		long := topicAt("long", day0)
		long.Units[0].Msgs[0].Text = strings.Repeat("長い本文を折り返して欄からはみ出させる。", 30)
		return []board.Topic{long, topicAt("short", day0.Add(-time.Hour))}
	})
	press(m, "l")
	rows := bodyPlain(m)
	if lastCell(rows[0]) != "█" {
		t.Fatalf("長い本文の先頭で、右端の 1 行目につまみが無い:\n%s", strings.Join(rows, "\n"))
	}
	if lastCell(rows[len(rows)-1]) != "│" {
		t.Errorf("先頭を見ているのに、下端がつまみ: %q", rows[len(rows)-1])
	}
	for _, l := range m.Lines() {
		if termwidth.Of(l) > 90 {
			t.Errorf("バーで行が幅を超えた: %q", l)
		}
	}
	for _, l := range rows {
		if strings.Contains(l, "…") {
			t.Errorf("バーの桁を引かずに折り返して、行が切れた: %q", l)
		}
	}
	press(m, "G")
	rows = bodyPlain(m)
	if lastCell(rows[len(rows)-1]) != "█" || lastCell(rows[0]) != "│" {
		t.Errorf("末尾まで送ったのに、つまみが下端に無い:\n%s", strings.Join(rows, "\n"))
	}
	press(m, "h")
	press(m, "j") // 短い話題
	for _, l := range bodyPlain(m) {
		if c := lastCell(l); c == "█" || c == "│" {
			t.Errorf("収まる本文にバーが出た: %q", l)
		}
	}
}

// キーは TUI ガイドに揃える: Enter は本文の欄の開閉、q は 1 段戻る (本文の欄なら一覧へ、一覧なら終わる)、Ctrl-C はすぐ終わる、
// Ctrl-F は → の別名、Esc は本文の欄を閉じる。
func TestKeysFollowTUIGuide(t *testing.T) {
	m := datedModel(t, func() []board.Topic { return []board.Topic{topicAt("a", day0)} })
	steps := []struct {
		key        string
		body, quit bool
	}{
		{"enter", true, false},
		{"enter", false, false},
		{"ctrl+f", true, false},
		{"q", false, false},
		{"l", true, false},
		{"esc", false, false},
		{"l", true, false},
		{"q", false, false},
		{"q", false, true},
	}
	for i, s := range steps {
		press(m, s.key)
		if m.body != s.body || m.quitting != s.quit {
			t.Fatalf("%d: %s の後 body=%v quitting=%v, want body=%v quitting=%v", i, s.key, m.body, m.quitting, s.body, s.quit)
		}
	}
	m2 := datedModel(t, func() []board.Topic { return []board.Topic{topicAt("a", day0)} })
	press(m2, "l")
	press(m2, "ctrl+c")
	if !m2.quitting {
		t.Error("本文の欄で Ctrl-C を押しても終わらない")
	}
}

// 本文の欄を開いたまま J / K で隣の話題へ送る: 日付の見出しは飛ばし、本文は先頭から出し、移った話題を既読にする。端では止めて ✗ で知らせる。
func TestJKStepsTopicsWithBodyOpen(t *testing.T) {
	m := datedModel(t, func() []board.Topic {
		a := topicAt("a", day0)
		a.Units[0].Msgs[0].Text = strings.Repeat("長い本文。", 200)
		return []board.Topic{a, topicAt("b", day0.Add(-time.Hour)), topicAt("c", day0.Add(-48*time.Hour))}
	})
	title := func() string { t, _ := m.selected(); return t.Title }
	press(m, "l")
	press(m, "G")
	if m.pager.Offset == 0 {
		t.Fatal("前提: 本文を末尾へ送れていない")
	}
	before := m.unreadCount(0)
	press(m, "J")
	if title() != "b" || !m.body || m.pager.Offset != 0 {
		t.Fatalf("J の後: 話題 %q body=%v offset=%d, want b / 本文の欄のまま / 先頭", title(), m.body, m.pager.Offset)
	}
	if m.unreadCount(0) != before-1 {
		t.Errorf("J で移った話題が既読にならない: 未読 %d → %d", before, m.unreadCount(0))
	}
	press(m, "J") // 見出し (別の日) を飛ばす
	if title() != "c" {
		t.Fatalf("見出しを飛ばして c へ移らない: %q", title())
	}
	press(m, "J")
	if title() != "c" || m.LastToast() != "これが最後の話題です" || m.toast.OK() || m.toast.Info() {
		t.Errorf("末尾の J: 話題 %q 知らせ %q (ok=%v info=%v)", title(), m.LastToast(), m.toast.OK(), m.toast.Info())
	}
	press(m, "K")
	press(m, "K")
	press(m, "K")
	if title() != "a" || m.LastToast() != "これが最初の話題です" {
		t.Errorf("先頭の K: 話題 %q 知らせ %q", title(), m.LastToast())
	}
	press(m, "h")
	press(m, "J") // 一覧では何もしない (隣へ送るのは本文の欄を開いているときだけ)
	if title() != "a" || m.body {
		t.Errorf("一覧の J で動いた: 話題 %q body=%v", title(), m.body)
	}
}

// 読み直している間は、状態の欄に回る印と経過秒を出し、印の周期で描き直しの tick を張る。印の tick が張ってある間に知らせが出たら、
// 知らせの速い tick を張り直す (遅い tick を待たせない)。読み終われば印の tick は張らない。
func TestReloadShowsSpinnerAndElapsed(t *testing.T) {
	now := day0
	m := datedModel(t, func() []board.Topic { return []board.Topic{topicAt("a", day0)} })
	m.now = func() time.Time { return now }
	var armed []time.Duration
	var last tea.Msg
	m.after = func(d time.Duration, f func(time.Time) tea.Msg) tea.Cmd {
		if msg := f(now); msg != nil {
			if _, ok := msg.(tickMsg); ok {
				armed, last = append(armed, d), msg
			}
		}
		return nil
	}
	m.ticks = tickchain.Chain{} // quiet が捨てた tick が「張ってある」まま残っている (本番では届いて空く)
	pending := press(m, "r")
	m.toast.Clear()
	now = now.Add(12 * time.Second)
	if s := m.state(); !strings.Contains(s, "12 秒 読み直し中") {
		t.Errorf("読み直し中の状態に経過秒が無い: %q", s)
	}
	frame := strings.Fields(m.state())[0]
	now = now.Add(spinnerStep)
	if next := strings.Fields(m.state())[0]; next == frame {
		t.Errorf("時刻が進んでも回る印が変わらない: %q", next)
	}
	armed = nil
	m.Update(last)
	if len(armed) != 1 || armed[0] != spinnerStep {
		t.Fatalf("読み直し中の tick: %v, want [%v]", armed, spinnerStep)
	}
	press(m, "u") // 知らせ (この話題を既読にした) が出る
	if len(armed) != 2 || armed[1] != frameInterval {
		t.Errorf("印の tick が張ってある間に出た知らせの tick: %v, want [%v %v]", armed, spinnerStep, frameInterval)
	}
	run(m, pending)
	m.toast.Clear()
	armed = nil
	m.Update(last)
	if len(armed) != 0 {
		t.Errorf("読み終わった後も tick を張る: %v", armed)
	}
}

// Ctrl-C は購読の画面 (検索の欄の中も) からでもすぐ終わる。検索の欄は貼り付けと、2 つ以上の rune でできた 1 文字を受ける。
// 購読の一覧を読んでいる間は回る印と経過秒を出す。小さい端末では q ですぐ終わる (見えない本文の欄を閉じるだけにしない)。
func TestPickerAndTinyTerminalFollowTUIGuide(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	now := t0
	m := subsModel(l, &saved, &opened)
	m.now = func() time.Time { return now }
	run(m, m.Init())
	cmd := press(m, "s")
	now = now.Add(7 * time.Second)
	if n := m.noteLine(); !strings.Contains(n, "7 秒 チャンネルの一覧を読んでいます") {
		t.Errorf("一覧を読んでいる間の注記に経過秒が無い: %q", n)
	}
	run(m, cmd)
	if m.picker == nil {
		t.Fatal("前提: 購読の画面が開かない")
	}
	press(m, "/")
	m.Update(tea.PasteMsg{Content: "de\nv"})
	m.Update(tea.KeyPressMsg{Code: '👍', Text: "👍🏽"})
	if got := m.picker.Lines()[1]; !strings.Contains(got, "de v👍🏽") {
		t.Errorf("貼り付けと絵文字が検索の欄に入らない: %q", got)
	}
	if _, cmd := m.Update(keyMsg("ctrl+c")); !m.quitting || cmd == nil {
		t.Error("検索の欄の中の Ctrl-C で終わらない")
	}

	tiny := datedModel(t, func() []board.Topic { return []board.Topic{topicAt("a", day0)} })
	press(tiny, "l")
	tiny.Update(tea.WindowSizeMsg{Width: 30, Height: 6})
	if !strings.Contains(tiny.Lines()[0], "q で終わる") {
		t.Errorf("小さい端末の行に抜け方が無い: %q", tiny.Lines()[0])
	}
	press(tiny, "q")
	if !tiny.quitting {
		t.Error("小さい端末で、本文の欄を開いたまま q を押しても終わらない")
	}
}
