package lanmgr

import (
	"context"
	"slices"
	"testing"
	"time"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/reach"
	"mac-lan-manager/speedtest"
	"mac-lan-manager/sshdest"
)

// 行の動作のキーは、最下行に案内しているときだけ効き、案内していないときは断る (どの行・どの状態でも)。
// 行は この Mac・管理対象 (見えている / 見えていない = 繋ぐ IP が無い)・未管理 (ssh だけ / 画面共有だけ)、状態は 普段・測定中・疎通を確かめている途中。
// 測定中は t がどの行でも進捗を開き直し、確かめている途中の行は台帳から外した後も p が効く (どちらもキーと案内を同じ述語にした)。
func TestRowHintsMatchKeys(t *testing.T) {
	for _, state := range []string{"普段", "測定中", "疎通の途中"} {
		h := newHarness(t, []ledger.Host{
			{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100", Alias: "mini"},
			{Host: "gone.local", User: "koji"}, // 台帳にだけある (見えていない。繋ぐ IP が無い)
		}, []discover.Peer{mini,
			{Host: "air.local", SSH: true, Addrs: map[int]string{14: "192.168.50.44"}},
			{Host: "vnc.local", VNC: true, SMB: true, Addrs: map[int]string{14: "192.168.50.8"}}})
		// 測定と疎通は止められるまで終わらない (状態を保ったままキーを押し比べる)
		h.m.o.SpeedTest = func(ctx context.Context, _ sshdest.Dest, _ func() time.Time) speedtest.Result {
			<-ctx.Done()
			return speedtest.Result{}
		}
		h.m.o.Reach = func(ctx context.Context, _ []string) []reach.Name { <-ctx.Done(); return nil }
		h.m.Resize(160, 50)
		t.Cleanup(h.m.Close)
		switch state {
		case "測定中":
			h.cursorTo(t, "m2macmini.local")
			h.keys("t", "y", "esc")
			if h.m.speedRun == nil {
				t.Fatal("前提: 測定が始まらない")
			}
		case "疎通の途中":
			h.cursorTo(t, "m2macmini.local")
			h.keys("p")
			if h.m.reachRuns["m2macmini.local"] == nil {
				t.Fatal("前提: 疎通を確かめ始めない")
			}
			h.m.hosts = slices.DeleteFunc(slices.Clone(h.m.hosts), func(x ledger.Host) bool { return x.Host == "m2macmini.local" }) // 台帳から外した
		}
		h.m.TakeNotices()
		seen := 0
		for tab := range numTabs {
			h.m.tab = tab
			for i := range len(h.m.visible()) {
				for _, a := range rowActions {
					if a.label == nil {
						continue
					}
					h.m.cur[tab] = i
					r := h.m.current()
					hinted := slices.ContainsFunc(h.m.rowHints(r), func(k keyHint) bool { return k.key == a.key })
					shown := h.m.speedRun != nil && h.m.speedRun.shown
					act := h.keys(a.key)
					effected := act != None || h.m.ov != ovNone || (h.m.speedRun != nil && h.m.speedRun.shown && !shown)
					for _, n := range h.m.TakeNotices() {
						effected = effected || n.OK
					}
					if hinted != effected {
						t.Errorf("%s: %q の %s: 案内=%v 効いた=%v", state, cmpHost(r), a.key, hinted, effected)
					}
					// 揃っているだけでなく、効く側に揃っていること (両方を断る形に揃えても上は緑になる)
					if !hinted && ((state == "測定中" && a.key == "t") || (state == "疎通の途中" && a.key == "p" && r.host == "m2macmini.local")) {
						t.Errorf("%s: %q の %s を断った (測定中の t はどの行でも進捗を開き直し、確かめている途中の p は終わったら知らせる)", state, cmpHost(r), a.key)
					}
					// 次のキーへ持ち越さない
					h.m.TakeExec()
					h.m.ov, h.m.ask, h.m.speedAsk = ovNone, nil, nil
					if h.m.speedRun != nil {
						h.m.speedRun.shown = false
					}
					seen++
				}
			}
		}
		if seen < 5*8 { // 5 行 × 案内のある 8 キー (行が組まれずに空振りしていない)
			t.Fatalf("%s: 確かめた組が %d しかない", state, seen)
		}
	}
}

func cmpHost(r row) string {
	if r.self {
		return "この Mac"
	}
	return r.host
}

// 台帳にあって繋ぐ IP が無い管理対象の行でも、p は効くので案内する (前は繋ぐ IP が無いと動作の案内をまとめて消し、効く p も消えた)
func TestReachHintWithoutConnectIP(t *testing.T) {
	h, _ := reachHarness(t)
	h.m.hosts = append(h.m.hosts, ledger.Host{Host: "gone.local", User: "koji"})
	h.cursorTo(t, "gone.local")
	if r := h.m.current(); r.connectIP() != "" {
		t.Fatalf("前提: 繋ぐ IP がある %+v", r)
	}
	if !slices.ContainsFunc(h.m.rowHints(h.m.current()), func(k keyHint) bool { return k.key == "p" }) {
		t.Errorf("繋ぐ IP の無い管理対象の行で p を案内しない: %v", h.m.rowHints(h.m.current()))
	}
}
