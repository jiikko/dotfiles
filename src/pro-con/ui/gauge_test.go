package ui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	g := ansi.Strip(New(be, nil).gauge())
	if !strings.Contains(g, "PG 0/3 │") || strings.Contains(g, "上限") || strings.Contains(g, "枠 96%") {
		t.Fatalf("止まった dispatcher の最後の絞りを出した: %q", g)
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

// 2 つ以上の画面が開いていればゲージに数を出し、終了の見出しは「この画面だけ閉じる」になる。
func TestScreensShownAndQuitLabel(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	be.snap.Screens = 2
	m := New(be, nil)
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "画面 2") {
		t.Fatalf("画面の数を出さない: %q", g)
	}
	if l := m.quitLabel(); !strings.Contains(l, "ほかに 1 画面") || strings.Contains(l, "止めて閉じる") {
		t.Fatalf("ほかの画面が開いているのに止めると案内した: %q", l)
	}
	be.snap.DispatcherTick = be.snap.Now.Add(-dispatcherStale - time.Second)
	if g := ansi.Strip(New(be, nil).gauge()); !strings.Contains(g, "画面 2") {
		t.Fatalf("dispatcher が止まっていると画面の数を隠した: %q", g)
	}
	be.snap.Screens = 1
	m = New(be, nil)
	if g := ansi.Strip(m.gauge()); strings.Contains(g, "画面 ") {
		t.Fatalf("画面が 1 つなのに数を出した: %q", g)
	}
	if l := m.quitLabel(); !strings.Contains(l, "止めて閉じる") {
		t.Fatalf("最後の画面なのに止めると案内しない: %q", l)
	}
}
