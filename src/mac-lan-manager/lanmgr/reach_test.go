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
	for deadline := time.Now().Add(10 * time.Second); len(h.m.reachRuns) > 0; {
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
	if f.calls.Load() != 0 || len(h.m.reachRuns) > 0 {
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
	if row := h.listRow(t, "m2macmini.local"); !strings.Contains(row, "L✓ W✗") { // 届いても、内部 IP と違う所を指す有線の名前は ✗
		t.Errorf("一覧の印: %q", row)
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
	if len(h.m.reachRuns) > 0 {
		t.Error("Close の後も確かめている途中のまま")
	}
}

// listRow は一覧の中で host を含む行 (色を外した文字)。一覧は詳細カードより上にあるので、最初に host が出る行。
func (h *harness) listRow(t *testing.T, host string) string {
	t.Helper()
	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, host) {
			return l
		}
	}
	t.Fatalf("一覧に %s の行が無い:\n%s", host, h.screen())
	return ""
}

// 一覧を進めると (Advance) 管理対象の相手を自分で確かめ、「名前」の列に L (.local) と W (有線の名前) の印を出す。知らせは出さない
// (開くたびに相手の数だけ出る)。未管理の相手は確かめない。
func TestReachAutoOnListShowsIcons(t *testing.T) {
	h, f := reachHarness(t)
	h.m.Advance(t0)
	names, _ := f.started(t)
	if want := []string{"m2macmini.local", "m2macmini.wired.local"}; !slices.Equal(names, want) {
		t.Fatalf("自分で確かめた名前 = %v, want %v", names, want)
	}
	if row := h.listRow(t, "m2macmini.local"); !strings.Contains(row, "L… W…") {
		t.Errorf("確かめている途中の印が無い: %q", row)
	}
	h.m.Advance(t0)
	if f.calls.Load() != 1 {
		t.Errorf("確かめている途中にもう 1 本起こした: calls=%d", f.calls.Load())
	}
	h.m.TakeNotices()
	f.results <- []reach.Name{
		{Name: "m2macmini.local", Addrs: []reach.Addr{reachOK}},
		{Name: "m2macmini.wired.local", Addrs: []reach.Addr{reachDead}},
	}
	awaitReach(t, h)
	if row := h.listRow(t, "m2macmini.local"); !strings.Contains(row, "L✓ W✗") {
		t.Errorf("一覧の印: %q", row)
	}
	if n := h.m.TakeNotices(); len(n) != 0 {
		t.Errorf("自分で確かめた回に知らせを出した: %+v", n)
	}
	h.m.Advance(t0)
	if f.calls.Load() != 1 {
		t.Errorf("届かない結果の後も確かめ続けた: calls=%d", f.calls.Load())
	}
	h.keys("l") // 未管理のタブ
	if row := h.listRow(t, "air.local"); strings.Contains(row, "L…") || strings.Contains(row, "L✓") || strings.Contains(row, "L✗") {
		t.Errorf("未管理の相手に印を出した: %q", row)
	}
}

// 開き直す (Reload) と前の結果を捨てて確かめ直す (前に開いたときの ✓ を今の値と読ませない)。
func TestReachAutoAgainOnReload(t *testing.T) {
	h, f := reachHarness(t)
	h.m.Advance(t0)
	f.started(t)
	f.results <- []reach.Name{{Name: "m2macmini.local", Addrs: []reach.Addr{reachOK}}, {Name: "m2macmini.wired.local", Addrs: []reach.Addr{reachOK}}}
	awaitReach(t, h)
	h.m.Reload()
	awaitNetLoad(h.m)
	h.m.Advance(t0)
	f.started(t)
	if f.calls.Load() != 2 {
		t.Fatalf("開き直しても確かめ直さない: calls=%d", f.calls.Load())
	}
	if row := h.listRow(t, "m2macmini.local"); !strings.Contains(row, "L… W…") {
		t.Errorf("開き直した後に前の結果を出した: %q", row)
	}
}

// 確かめた後に行の名前が変わったら (内部 IP が固定になって有線の名前が増えた等) 確かめ直す。
func TestReachAutoAgainWhenNamesChange(t *testing.T) {
	h, f := reachHarness(t)
	h.m.Advance(t0)
	f.started(t)
	f.results <- []reach.Name{{Name: "m2macmini.local", Addrs: []reach.Addr{reachOK}}}
	awaitReach(t, h)
	// 確かめた名前が今の名前 (.local と有線の名前) と違う結果に差し替える
	res := h.m.reach["m2macmini.local"]
	res.asked = []string{"m2macmini.local"}
	h.m.reach["m2macmini.local"] = res
	h.m.Advance(t0)
	f.started(t)
	if f.calls.Load() != 2 {
		t.Fatalf("名前が変わっても確かめ直さない: calls=%d", f.calls.Load())
	}
}

// 走査の結果が変わった合図で 1 回だけ進めた (Advance) ときも、その回に取り込んだ行の名前で確かめ直す (走査が落ち着いた後は
// 合図 1 回しか来ず、取り込む前の行で決めると次のキーまで「…」のまま何も走らない)。
func TestReachAutoUsesPeersOfSameAdvance(t *testing.T) {
	peer := discover.Peer{Host: "new.local", Instances: []string{"new"}, SSH: true, Addrs: map[int]string{14: "192.168.50.60"}}
	h := newHarness(t, []ledger.Host{{Host: "new.local", User: "koji"}}, []discover.Peer{peer})
	f := &fakeReach{names: make(chan []string, 4), ctx: make(chan context.Context, 4), results: make(chan []reach.Name, 4)}
	h.m.o.Reach = func(ctx context.Context, names []string) []reach.Name {
		f.calls.Add(1)
		f.names <- names
		f.ctx <- ctx
		return []reach.Name{{Name: names[0], Addrs: []reach.Addr{reachOK}}}
	}
	h.m.Advance(t0)
	if names, _ := f.started(t); !slices.Equal(names, []string{"new.local"}) {
		t.Fatalf("1 回目の名前 = %v", names)
	}
	awaitReach(t, h)
	// 有線の I/F にも見えた (内部 IP が帯の番号で固定 = 有線の名前を持つ)。合図の 1 回の Advance だけで確かめ直す
	h.scan.peers = []discover.Peer{{Host: "new.local", Instances: []string{"new"}, SSH: true, Addrs: map[int]string{14: "192.168.50.60", 27: "192.168.100.20"}}}
	h.m.Advance(t0)
	select {
	case names := <-f.names:
		<-f.ctx
		if !slices.Equal(names, []string{"new.local", "new.wired.local"}) {
			t.Errorf("2 回目の名前 = %v", names)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("合図の 1 回の Advance で確かめ直さない")
	}
}

// 自分で確かめている途中に p を押したら、2 本目は起こさず、その回が終わったときに知らせる。
func TestReachKeyDuringAutoTellsResult(t *testing.T) {
	h, f := reachHarness(t)
	h.m.Advance(t0)
	f.started(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("p")
	if f.calls.Load() != 1 {
		t.Fatalf("2 本目を起こした: calls=%d", f.calls.Load())
	}
	h.m.TakeNotices()
	f.results <- []reach.Name{{Name: "m2macmini.local", Addrs: []reach.Addr{reachOK}}, {Name: "m2macmini.wired.local", Addrs: []reach.Addr{reachDead}}}
	awaitReach(t, h)
	if n := h.m.TakeNotices(); len(n) == 0 || n[len(n)-1].OK || !strings.Contains(n[len(n)-1].Text, "届かない") {
		t.Errorf("p を押した回の知らせ = %+v", n)
	}
}
