package ui

import (
	"strings"
	"testing"
	"time"

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

// join の画面 (issue 481): quit のダイアログは「この画面だけ閉じる」。人が止めた dispatcher は起こさない (c を出さず、押すと理由つきで断る)。
// 止まっているときは起こし方を出す。
func TestJoinScreenQuitAndNoResume(t *testing.T) {
	be := &joinSpy{stopSpy: &stopSpy{spy: newSpy()}}
	be.snap.Screens = []backend.Screen{{ID: "aaaaaa", Join: true, Self: true}}
	be.snap.DispatcherHeld = true
	m := New(be, nil)
	if l := quitText(m); !strings.Contains(l, "join の画面 (--join)\nこの画面だけ閉じる") || strings.Contains(l, "止めて閉じる") {
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
	be.rejected = []backend.Rejected{{Kind: "answer", CardID: "W1", Why: "質問待ちではない (今は 着手待ち。既に回答済みの可能性)"}}
	m.poll()
	if got := m.toasts.Text(); !strings.Contains(got, "W1 への answer") || !strings.Contains(got, "既に回答済みの可能性") {
		t.Fatalf("除けた理由を出さない: %q", got)
	}
	if len(be.rejected) != 0 {
		t.Fatal("渡した分を取らない")
	}
}

// 持ち主の画面が無い join の画面 (issue 543): 罫線の上に全幅の帯を出す。dispatcher が動いている間は「止まっても誰も起こさない」、
// 止まったら「カードは進まない」。持ち主が居る・画面を数えられない・持ち主の画面では出さない。
func TestOwnerlessJoinBand(t *testing.T) {
	be := &joinSpy{stopSpy: &stopSpy{spy: newSpy()}}
	be.snap.Screens = []backend.Screen{{ID: "aaaaaa", Join: true, Self: true}, {ID: "bbbbbb", Join: true}}
	band := func(m *Model) string {
		m.width, m.height = 200, 30
		lines := strings.Split(ansi.Strip(m.render()), "\n")
		if len(lines) != m.height {
			t.Fatalf("帯を足して画面の高さが %d 行 (期待 %d)", len(lines), m.height)
		}
		if !strings.HasPrefix(lines[m.headerRows()-1], "───") {
			t.Fatalf("ヘッダの最後の行が罫線でない: %q", lines[m.headerRows()-1])
		}
		if m.headerRows() == 4 {
			return ""
		}
		return lines[3]
	}
	cases := []struct {
		name  string
		setup func()
		want  []string // 空なら帯を出さない
	}{
		{"動いている", func() {}, []string{"⚠ 持ち主の画面が無い", "止まっても誰も (supervisor も) 起こし直さない", "持ち主の画面 (pro-con) を開くと見張りが戻る"}},
		{"落ちた", func() { be.snap.DispatcherTick, be.snap.DispatcherGone = be.snap.Now.Add(-3*time.Minute), true }, []string{"■ カードは進まない", "止まっている (最後の Tick 3分前)", "開くと起きる"}},
		// プロセスは居るのに回っていない: lock を持ったままなので、持ち主の画面を開いても起きない (「開くと起きる」と言わない)
		{"固まった", func() { be.snap.DispatcherTick = be.snap.Now.Add(-3 * time.Minute) }, []string{"■ カードは進まない", "3分前から回っていない", "抜けるまでは起きない"}},
		{"1 度も回っていない", func() { be.snap.DispatcherTick = time.Time{} }, []string{"■ カードは進まない", "1 度も回っていない"}},
		{"人が止めた", func() { be.snap.DispatcherHeld = true }, []string{"■ カードは進まない", "人が止めてある", "c で起こす"}},
		{"持ち主が居る", func() {
			be.snap.DispatcherTick = be.snap.Now.Add(-3 * time.Minute)
			be.snap.Screens = append(be.snap.Screens, backend.Screen{ID: "cccccc"})
		}, nil},
		{"数えられない", func() { be.snap.DispatcherTick, be.snap.Screens = be.snap.Now.Add(-3*time.Minute), nil }, nil},
	}
	base := be.snap
	for _, c := range cases {
		be.snap = base
		be.snap.Screens = append([]backend.Screen(nil), base.Screens...)
		c.setup()
		got := band(New(be, nil))
		if c.want == nil && got != "" {
			t.Fatalf("%s: 帯を出した: %q", c.name, got)
		}
		if c.name == "固まった" && strings.Contains(got, "開くと起きる") {
			t.Fatalf("固まった dispatcher を「開くと起きる」と案内した: %q", got)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Fatalf("%s: 帯に %q が無い: %q", c.name, w, got)
			}
		}
	}
	// 持ち主の画面 (join でない backend) は、join の画面しか数えられなくても出さない (持ち主の画面そのものは数えに入っている)
	owner := newSpy()
	owner.snap.Screens = base.Screens
	if got := band(New(owner, nil)); got != "" {
		t.Fatalf("持ち主の画面に帯を出した: %q", got)
	}
}
