package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
)

// joinSpy は加わった画面 (pro-con --join) の backend の偽物。c を受けず、除けた依頼を渡す。
type joinSpy struct {
	*stopSpy
	rejected []backend.Rejected
}

func (j *joinSpy) Joined()                    {}
func (j *joinSpy) Accepts(op backend.Op) bool { return op != backend.OpResume }
func (j *joinSpy) TakeRejected() []backend.Rejected {
	out := j.rejected
	j.rejected = nil
	return out
}

// join の画面 (issue 481): quit の見出しは「この画面だけ閉じる」。人が止めた dispatcher は起こさない (c を出さず、押すと理由つきで断る)。
// 止まっているときは起こし方を出す。
func TestJoinScreenQuitAndNoResume(t *testing.T) {
	be := &joinSpy{stopSpy: &stopSpy{spy: newSpy()}}
	be.snap.Screens = []backend.Screen{{ID: "aaaaaa", Join: true, Self: true}}
	be.snap.DispatcherHeld = true
	m := New(be, nil)
	if l := m.quitLabel(); !strings.Contains(l, "join の画面。この画面だけ閉じる") || strings.Contains(l, "止めて閉じる") {
		t.Fatalf("join の画面の quit が止めると案内した: %q", l)
	}
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "join からは起こせない") || strings.Contains(g, "c で起こす") || !strings.Contains(g, "aaaaaa join (この画面)") {
		t.Fatalf("join の画面のゲージ: %q", g)
	}
	if h := ansi.Strip(strings.Join(m.hints(), " ")); strings.Contains(h, "c dispatcher を起こす") {
		t.Fatalf("join の画面の案内に c を出した: %q", h)
	}
	press(m, "c")
	if m.mode != modeBoard || len(be.applied) != 0 || !strings.Contains(m.toasts.Text(), "join の画面からは dispatcher を起こさない") {
		t.Fatalf("join の画面で c を断らない: mode=%v applied=%v toast=%q", m.mode, be.applied, m.toasts.Text())
	}
	be.snap.DispatcherHeld = false
	be.snap.DispatcherTick = be.snap.Now.Add(-dispatcherStale - 1)
	if g := ansi.Strip(New(be, nil).gauge()); !strings.Contains(g, "起こすのは持ち主の画面か pro-con dispatcher") {
		t.Fatalf("止まっている dispatcher の起こし方を出さない: %q", g)
	}
}

// この画面が置いて除けられた依頼は、dispatcher が書いた理由を 1 度だけ出す (issue 481)。
func TestRejectedRequestShownOnce(t *testing.T) {
	be := &joinSpy{stopSpy: &stopSpy{spy: newSpy()}}
	m := New(be, nil)
	be.rejected = []backend.Rejected{{Kind: "answer", CardID: "W1", Why: "質問待ちではない (今は 分解済み。既に回答済みの可能性)"}}
	m.poll()
	if got := m.toasts.Text(); !strings.Contains(got, "W1 への answer") || !strings.Contains(got, "既に回答済みの可能性") {
		t.Fatalf("除けた理由を出さない: %q", got)
	}
	if len(be.rejected) != 0 {
		t.Fatal("渡した分を取らない")
	}
}
