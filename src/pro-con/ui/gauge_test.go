package ui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
	"tuikit/termwidth"
)

// ゲージの PG の数は今の同時実行数。利用枠で上限より絞っていれば上限と理由も出す。dispatcher が 1 度も回っていない /
// 長く回っていなければ、そうと分かる文を出す。
func TestGaugeShowsCapAndDispatcherLiveness(t *testing.T) {
	be := newSpy()
	be.snap.Limit, be.snap.LimitMax, be.snap.LimitWhy = 1, 3, "枠 85%: 同時に 1 本まで"
	m := New(be, nil)
	g := ansi.Strip(m.gauge())
	if !strings.Contains(g, "/1 (上限 3) 枠 85%: 同時に 1 本まで") {
		t.Fatalf("絞った上限と理由を出さない: %q", g)
	}
	if strings.Contains(g, "止まっている") || strings.Contains(g, "未起動") {
		t.Fatalf("回っているのに止まっている扱い: %q", g)
	}
	be.snap.DispatcherTick = be.snap.Now.Add(-dispatcherStale - time.Second)
	m.snap = be.snap
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "止まっている?") || strings.Contains(g, "枠 85%") {
		t.Fatalf("長く回っていないのに知らせない / 止まった dispatcher の最後の理由を出した: %q", g)
	}
	be.snap.DispatcherTick = time.Time{}
	m.snap = be.snap
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "dispatcher 未起動") {
		t.Fatalf("1 度も回っていないのに知らせない: %q", g)
	}
}

// 絞っていなければ上限を添えない。
func TestGaugeOmitsMaxWhenNotCapped(t *testing.T) {
	be := newSpy()
	be.snap.Limit, be.snap.LimitMax = 3, 3
	if g := ansi.Strip(New(be, nil).gauge()); strings.Contains(g, "上限") {
		t.Fatalf("絞っていないのに上限を出した: %q", g)
	}
	be.snap.Limit, be.snap.LimitMax = 3, 2 // 上限を下げて起動し直した直後など、絞りの外で数が上限を超えた形
	if g := ansi.Strip(New(be, nil).gauge()); strings.Contains(g, "上限") {
		t.Fatalf("絞っていないのに上限を出した: %q", g)
	}
}

// 止まった dispatcher の最後の絞り (0 本など) を今の値として出さない。上限だけを出す。
func TestGaugeStoppedDispatcherShowsMax(t *testing.T) {
	be := newSpy()
	be.snap.Limit, be.snap.LimitMax, be.snap.LimitWhy = 0, 3, "枠 96%: 新しく起動・再開しない"
	be.snap.DispatcherTick = be.snap.Now.Add(-dispatcherStale - time.Second)
	be.snap.SlotsAt = be.snap.DispatcherTick
	g := ansi.Strip(New(be, nil).gauge())
	if !strings.Contains(g, "PG ?/3 │") || strings.Contains(g, "上限") || strings.Contains(g, "枠 96%") {
		t.Fatalf("止まった dispatcher の最後の絞りを出した: %q", g)
	}
}

// dispatcher の新版への入れ替え (505) と起動時の確かめ (483) は、回っている dispatcher の間だけゲージに出す (止まった dispatcher の古い様子は出さない)。
func TestGaugeShowsDispatcherUpgrade(t *testing.T) {
	be := newSpy()
	be.snap.Startup, be.snap.Upgrade = "起動時: 復旧は要らない", "dispatcher 新版 abc1234 09-26 08:02 → def5678 09-26 16:51"
	m := New(be, nil)
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "起動時: 復旧は要らない │ dispatcher 新版 abc1234 09-26 08:02 → def5678") {
		t.Fatalf("入れ替えを出さない: %q", g)
	}
	be.snap.DispatcherTick = be.snap.Now.Add(-dispatcherStale - time.Second)
	m.snap = be.snap
	if g := ansi.Strip(m.gauge()); strings.Contains(g, "新版") || strings.Contains(g, "起動時") {
		t.Fatalf("止まった dispatcher の古い様子を出した: %q", g)
	}
}

// 絞りの理由は 1 行に潰して limitWhyCells で切る (長い理由・改行でゲージの後ろの警告を押し出さない / ヘッダの行を増やさない)。
func TestGaugeBoundsLimitWhy(t *testing.T) {
	be := newSpy()
	be.snap.Limit, be.snap.LimitMax, be.snap.LimitWhy = 3, 3, "枠を読めない:\nexit status 1 "+strings.Repeat("x", 300) // 改行は切る幅より手前
	g := New(be, nil).gauge()
	if strings.Contains(g, "\n") {
		t.Fatal("理由の改行がゲージに入った (ヘッダの行が増える)")
	}
	plain := ansi.Strip(g)
	i := strings.Index(plain, "枠を読めない")
	j := strings.Index(plain[i:], " │ ")
	if i < 0 || j < 0 || ansi.StringWidth(plain[i:i+j]) > limitWhyCells {
		t.Fatalf("理由を %d セルで切らない: %q", limitWhyCells, plain)
	}
}

// notifySpy は変化を知らせる backend (backend.Notifier)。
type notifySpy struct {
	*spy
	ch chan struct{}
}

func (n notifySpy) Changed() <-chan struct{} { return n.ch }

// backend が変化を知らせたら、1 秒の tick を待たずに取り込み、次の知らせを待ち直す。
func TestChangedMsgPollsAndRearms(t *testing.T) {
	be := notifySpy{spy: newSpy(), ch: make(chan struct{}, 1)}
	m := New(be, nil)
	if m.waitChanged() == nil {
		t.Fatal("Notifier なのに知らせを待たない")
	}
	be.snap.Cards = be.snap.Cards[:1]
	_, cmd := m.Update(changedMsg{})
	if len(m.snap.Cards) != 1 {
		t.Fatalf("知らせで取り込まない: %d 枚", len(m.snap.Cards))
	}
	be.ch <- struct{}{}
	if !hasChanged(cmd) {
		t.Fatal("知らせの後に次の知らせを待ち直さない")
	}
}

// hasChanged は cmd (Batch を含む) を実行して changedMsg が出るか。
func hasChanged(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case changedMsg:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if hasChanged(c) {
				return true
			}
		}
	}
	return false
}

// 起動したら backend の知らせを待ち始める (待ち始めないと、知らせは 1 度も届かず 1 秒の tick に戻る)。
func TestInitWaitsForChanges(t *testing.T) {
	be := notifySpy{spy: newSpy(), ch: make(chan struct{}, 1)}
	be.ch <- struct{}{}
	if !hasChanged(New(be, nil).Init()) {
		t.Fatal("起動しても backend の知らせを待たない")
	}
}

// 2 つ以上の画面が開いていればゲージに一覧を出し、終了のダイアログは「この画面だけ閉じる」になる。
func TestScreensShownAndQuitLabel(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	be.snap.Screens = []backend.Screen{{ID: "aaaaaa", Self: true}, {ID: "bbbbbb"}}
	m := New(be, nil)
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "aaaaaa 持ち主 (この画面)") || !strings.Contains(g, "bbbbbb 持ち主") {
		t.Fatalf("画面の一覧を出さない: %q", g)
	}
	if l := quitText(m); !strings.Contains(l, "ほかに持ち主の画面が 1 開いている\nこの画面だけ閉じる") || strings.Contains(l, "止めて閉じる") {
		t.Fatalf("ほかの画面が開いているのに止めると案内した: %q", l)
	}
	be.snap.DispatcherTick = be.snap.Now.Add(-dispatcherStale - time.Second)
	if g := ansi.Strip(New(be, nil).gauge()); !strings.Contains(g, "bbbbbb") {
		t.Fatalf("dispatcher が止まっていると画面の一覧を隠した: %q", g)
	}
	be.snap.Screens = be.snap.Screens[:1]
	m = New(be, nil)
	if g := ansi.Strip(m.gauge()); strings.Contains(g, "画面 ") {
		t.Fatalf("画面が 1 つなのに一覧を出した: %q", g)
	}
	if l := quitText(m); !strings.Contains(l, "止めて閉じる") {
		t.Fatalf("最後の画面なのに止めると案内しない: %q", l)
	}
}

// ゲージの「PG n/m」は、dispatcher が枠に数えたカード (様子の Slots) の数を読むだけ (issue 557)。画面の側で一覧 (Consumers) から
// card.HoldsPGSlot を組み直さない (母数が dispatcher と違う)。dispatcher が数え直していなければ ? (読めないのを 0 に見せない)。
func TestGaugeCountsOnlyPGsHoldingSlot(t *testing.T) {
	be := newSpy()
	now := be.snap.Now
	be.snap.Cards = append(be.snap.Cards,
		card.Card{ID: "R2", State: card.Running, Session: "s-r2", Since: now, Run: "make test"},
		card.Card{ID: "R3", State: card.Running, Session: "s-r3", Since: now, Run: "make test"})
	be.snap.Consumers = []backend.Consumer{
		{Session: "s-r1", CardID: "R1", Status: "busy"},
		{Session: "s-r2", CardID: "R2", Status: "idle"}, // 結果待ちで turn を終えた: 数えない
		{Session: "s-r3", CardID: "R3", Status: "busy"}, // 頼んだが turn の途中: 数える
	}
	be.snap.Slots = []string{"R1", "R3"} // dispatcher が数えたもの (R2 は結果待ちで idle なので数えていない)
	m := New(be, nil)
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "PG 2/2") {
		t.Fatalf("dispatcher が数えた枠を出さない: %q", g)
	}
	be.snap.SlotsAt = be.snap.Now.Add(-dispatcherStale - time.Second) // 一覧を取れない Tick が続いて数え直していない
	if g := ansi.Strip(New(be, nil).gauge()); !strings.Contains(g, "PG ?/2") {
		t.Fatalf("古い数を今の値として出した: %q", g)
	}
}

// join の画面は持ち主の数に入れない: 最後の持ち主の quit は、join が残っていても止めると案内し、join が残ることを添える (issue 481)。
// 画面が多ければゲージは数だけにする。
func TestJoinScreensNotCountedAsOwners(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	be.snap.Screens = []backend.Screen{{ID: "aaaaaa", Self: true}, {ID: "bbbbbb", Join: true, Label: "review"}}
	m := New(be, nil)
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "bbbbbb join review") {
		t.Fatalf("join の画面を見分けて出さない: %q", g)
	}
	if l := quitText(m); !strings.Contains(l, "止めて閉じる") || !strings.Contains(l, "join の画面は、止めた後は表示が止まる") {
		t.Fatalf("最後の持ち主なのに止めると案内しない / join が残ることを出さない: %q", l)
	}
	for _, id := range []string{"cccccc", "dddddd"} {
		be.snap.Screens = append(be.snap.Screens, backend.Screen{ID: id, Join: true})
	}
	if g := ansi.Strip(New(be, nil).gauge()); !strings.Contains(g, "画面 4 (持ち主 1・join 3。s で一覧)") {
		t.Fatalf("多い画面を数にまとめない: %q", g)
	}
}

// ゲージの行にレーンごとの枚数を出さない (枠の見出し「3 作業中 (2)」と同じ数。issue 558)。行は残りの項目から始まり、
// 頭に余分な │ が付かない。狭い端末でも頭の項目 (人の番・最古の待ち・PG) は画面に入る。
func TestGaugeOmitsLaneCounts(t *testing.T) {
	m := humanSnap(t)
	g := ansi.Strip(m.gauge())
	if !strings.HasPrefix(g, " !人の番 2 │ 最古の待ち ") {
		t.Fatalf("行の頭が人の番・最古の待ちでない: %q", g)
	}
	for i, st := range card.Columns {
		if n := len(m.lanes()[i]); strings.Contains(g, fmt.Sprintf("%s %d", st.Label(), n)) {
			t.Errorf("ゲージにレーンの枚数 %q が残っている: %q", fmt.Sprintf("%s %d", st.Label(), n), g)
		}
	}
	if g := ansi.Strip(New(newSpy(), nil).gauge()); !strings.HasPrefix(g, " 最古の待ち ") {
		t.Fatalf("人の番が無いとき行の頭が最古の待ちでない: %q", g)
	}
	for _, w := range []int{40, 60, 80, 200} {
		m.width, m.height = w, 30
		lines := strings.Split(ansi.Strip(m.render()), "\n")
		if len(lines) != m.height {
			t.Fatalf("幅 %d: 画面の行数 %d (期待 %d)", w, len(lines), m.height)
		}
		if got := termwidth.Truncate(lines[2], w, ""); !strings.HasPrefix(got, " !人の番 2 │ 最古の待ち ") {
			t.Errorf("幅 %d: ゲージの頭が画面に入らない: %q", w, got)
		}
		if w >= 60 && !strings.Contains(termwidth.Truncate(lines[2], w, ""), "PG ") {
			t.Errorf("幅 %d: PG が画面に入らない: %q", w, lines[2])
		}
		screen := strings.Join(lines, "\n")
		for i, st := range card.Columns { // 枠の見出しの枚数は残す
			if want := fmt.Sprintf(" (%d)", len(m.lanes()[i])); w == 200 && !strings.Contains(screen, st.Label()+want) {
				t.Errorf("幅 %d: 枠の見出しに %q が無い:\n%s", w, st.Label()+want, screen)
			}
		}
	}
}
