package lanmgr

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/reach"
)

// fakeReach は Reach の偽物。呼ばれた名前と ctx を控え、results に送った結果を返す (送るまで確かめている途中のまま)。
type fakeReach struct {
	calls   atomic.Int32
	names   chan []string
	ctx     chan context.Context
	results chan []reach.Name
}

func reachHarness(t *testing.T) (*harness, *fakeReach) {
	t.Helper()
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100", Alias: "mini"}},
		[]discover.Peer{mini, {Host: "air.local", SSH: true, Addrs: map[int]string{14: "192.168.50.44"}}})
	f := &fakeReach{names: make(chan []string, 4), ctx: make(chan context.Context, 4), results: make(chan []reach.Name, 4)}
	h.m.o.Reach = func(ctx context.Context, names []string) []reach.Name {
		f.calls.Add(1)
		f.names <- names
		f.ctx <- ctx
		select {
		case r := <-f.results:
			return r
		case <-ctx.Done():
			return nil
		}
	}
	h.m.Resize(160, 50)
	return h, f
}

// started は確認が起こされるのを待つ (起こされなければ 10 秒で赤にする)。
func (f *fakeReach) started(t *testing.T) ([]string, context.Context) {
	t.Helper()
	select {
	case n := <-f.names:
		return n, <-f.ctx
	case <-time.After(10 * time.Second):
		t.Fatal("疎通の確認が起こされない")
		return nil, nil
	}
}

// awaitReach は結果が取り込まれるまで Advance する (上限 10 秒)。
func awaitReach(t *testing.T, h *harness) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); h.m.reachRun != nil; {
		if time.Now().After(deadline) {
			t.Fatal("結果が取り込まれない")
		}
		h.m.Advance(t0)
		time.Sleep(time.Millisecond) // sleep-ok: tick: 条件 (reachRun が空く) を待つ刻み
	}
}

var (
	reachOK   = reach.Addr{IP: "192.168.50.7", Ping: reach.Ping{Sent: 3, Recv: 3, AvgMs: 4.2}}
	reachDead = reach.Addr{IP: "169.254.217.100", Ping: reach.Ping{Sent: 3}, SSH: errors.New("i/o timeout")}
)

// 未管理の相手・この Mac は確かめない。
func TestReachOnlyManaged(t *testing.T) {
	h, f := reachHarness(t)
	h.keys("l") // 未管理のタブ
	h.cursorTo(t, "air.local")
	h.keys("p")
	if f.calls.Load() != 0 || h.m.reachRun != nil {
		t.Fatalf("未管理の相手を確かめた: calls=%d", f.calls.Load())
	}
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "管理対象") {
		t.Errorf("知らせ = %+v", n)
	}
	if strings.Contains(h.screen(), "疎通") {
		t.Error("未管理の相手の詳細カードに疎通の行を出した")
	}
}

// p で Bonjour の名前と有線の名前を確かめ、確かめている間は Busy・2 本目は断る。名前が引けても届かなければ ✗ と知らせを出す
// (有線の名前の広告だけが残ると、誰も持っていない内部 IP に引ける)。
func TestReachRunsAndShowsResult(t *testing.T) {
	h, f := reachHarness(t)
	h.cursorTo(t, "m2macmini.local")
	if !strings.Contains(h.screen(), "未確認") {
		t.Errorf("確かめる前の詳細カードに「未確認」が無い:\n%s", h.screen())
	}
	h.keys("p")
	names, _ := f.started(t)
	if want := []string{"m2macmini.local", "m2macmini.wired.local"}; !slices.Equal(names, want) {
		t.Fatalf("確かめた名前 = %v, want %v", names, want)
	}
	if !h.m.Busy() || !strings.Contains(h.screen(), "確かめている") {
		t.Errorf("確かめている間: Busy=%v\n%s", h.m.Busy(), h.screen())
	}
	h.keys("p")
	if f.calls.Load() != 1 {
		t.Errorf("確かめている途中に 2 本目を起こした: calls=%d", f.calls.Load())
	}
	h.m.TakeNotices()
	f.results <- []reach.Name{
		{Name: "m2macmini.local", Addrs: []reach.Addr{reachOK}},
		{Name: "m2macmini.wired.local", Addrs: []reach.Addr{reachDead}},
	}
	awaitReach(t, h)
	if h.m.Busy() {
		t.Error("取り込んだ後も Busy")
	}
	sc := h.screen()
	if !strings.Contains(sc, "✓ m2macmini.local → 192.168.50.7 ping 4.2ms (3/3)  ssh 開") ||
		!strings.Contains(sc, "✗ m2macmini.wired.local → 169.254.217.100 ping 届かない (0/3)  ssh 閉") {
		t.Errorf("詳細カードの疎通の行:\n%s", sc)
	}
	if strings.Contains(sc, "と違う") {
		t.Errorf("内部 IP に引けた有線の名前に「違う」を付けた:\n%s", sc)
	}
	if n := h.m.TakeNotices(); len(n) == 0 || n[len(n)-1].OK || !strings.Contains(n[len(n)-1].Text, "m2macmini.wired.local") {
		t.Errorf("届かない名前の知らせ = %+v", n)
	}
}

// 有線の名前が内部 IP と違う IP に引けたら、その旨を付ける。
func TestReachMarksWiredNameElsewhere(t *testing.T) {
	h, f := reachHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("p")
	f.started(t)
	f.results <- []reach.Name{
		{Name: "m2macmini.local", Addrs: []reach.Addr{reachOK}},
		{Name: "m2macmini.wired.local", Addrs: []reach.Addr{{IP: "192.168.100.99", Ping: reach.Ping{Sent: 3, Recv: 3, AvgMs: 1}}}},
	}
	awaitReach(t, h)
	if sc := h.screen(); !strings.Contains(sc, "内部 IP (169.254.217.100) と違う") {
		t.Errorf("違う IP に引けた有線の名前の行:\n%s", sc)
	}
}

// glogx の板を閉じたら (Close) 確かめている途中の回を取り消す。
func TestReachCancelledOnClose(t *testing.T) {
	h, f := reachHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("p")
	_, ctx := f.started(t)
	h.m.Close()
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("Close で取り消されない")
	}
	if h.m.reachRun != nil {
		t.Error("Close の後も確かめている途中のまま")
	}
}
