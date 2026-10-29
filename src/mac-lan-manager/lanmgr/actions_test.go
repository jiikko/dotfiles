package lanmgr

import (
	"context"
	"regexp"
	"slices"
	"strings"
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
		seen, cardHits := 0, 0
		for tab := range numTabs {
			h.m.tab = tab
			for i := range len(h.m.visible()) {
				for _, a := range rowActions {
					if a.label == nil {
						continue
					}
					h.m.cur[tab] = i
					r := h.m.current()
					card := stripView(strings.Join(h.m.detailLines(r, 120, false), "\n"))
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
					// 詳細のカードも、効かないキーを案内しない (t / p はカードの「未測定」「未確認」の行に keycap を添える)
					if k := cardKey[a.key]; k != nil && k.MatchString(card) {
						cardHits++
					}
					if k := cardKey[a.key]; k != nil && !hinted && k.MatchString(card) {
						t.Errorf("%s: %q のカードが効かない %s を案内した:\n%s", state, cmpHost(r), a.key, card)
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
		if cardHits == 0 { // カードの照合の正規表現が描き方とずれて空振りしていない
			t.Errorf("%s: カードの t / p の keycap が一度も見つからない", state)
		}
		if seen < 5*8 { // 5 行 × 案内のある 8 キー (行が組まれずに空振りしていない)
			t.Fatalf("%s: 確かめた組が %d しかない", state, seen)
		}
	}
}

var cardKey = map[string]*regexp.Regexp{"t": regexp.MustCompile(`t\s+(測る|測定の進捗)`), "p": regexp.MustCompile(`p\s+確かめる`)}

// 測定中でも、行の無いタブ (絞り込みで 0 件) では t を案内しない (t は「このタブには Mac が無い」で断る)
func TestNoActionHintsOnEmptyTab(t *testing.T) {
	h, _ := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t", "y", "esc")
	if h.m.speedRun == nil {
		t.Fatal("前提: 測定が始まらない")
	}
	if !slices.ContainsFunc(h.m.hints(), func(k keyHint) bool { return k.key == "t" }) {
		t.Fatal("前提: 行がある間は t を案内する (下の否定の対照)")
	}
	h.keys("/", "z", "z", "z", "z", "enter")
	if len(h.m.visible()) != 0 {
		t.Fatalf("前提: 絞り込みで 0 件にならない (%d 行)", len(h.m.visible()))
	}
	if hs := h.m.hints(); slices.ContainsFunc(hs, func(k keyHint) bool { return k.key == "t" }) {
		t.Errorf("行の無いタブで t を案内した: %v", hs)
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

// 疎通を確かめる口が無い画面では、詳細のカードにも p を案内しない (p は「この画面では疎通を確かめられない」で断る)
func TestReachCardKeyNeedsReach(t *testing.T) {
	h, _ := reachHarness(t)
	h.cursorTo(t, "m2macmini.local")
	card := func() string { return stripView(strings.Join(h.m.detailLines(h.m.current(), 120, false), "\n")) }
	if !cardKey["p"].MatchString(card()) {
		t.Fatalf("前提: 確かめられる画面のカードに p が無い (下の否定の対照):\n%s", card())
	}
	h.m.o.Reach = nil
	if c := card(); cardKey["p"].MatchString(c) {
		t.Errorf("確かめる口が無いのにカードが p を案内した:\n%s", c)
	}
}

// この Mac のネットワークを読む前は、詳細のカードも網の要るキー (s Y o a y O t p) を案内しない (キーは「確認中」で断る。最下行も出さない)
func TestCardHidesNetKeysBeforeNetRead(t *testing.T) {
	h, _ := reachHarness(t)
	h.m.o.SpeedTest = func(ctx context.Context, _ sshdest.Dest, _ func() time.Time) speedtest.Result {
		<-ctx.Done()
		return speedtest.Result{}
	}
	h.cursorTo(t, "m2macmini.local")
	keys := regexp.MustCompile(`(^|\s)(s\s+繋ぐ|Y\s+コピー|o\s+開く|t\s+測る|p\s+確かめる)`)
	card := func() string { return stripView(strings.Join(h.m.detailLines(h.m.current(), 120, false), "\n")) }
	if got := keys.FindAllString(card(), -1); len(got) < 4 {
		t.Fatalf("前提: 網を読めた後のカードに動作のキーが揃わない (下の否定の対照): %q\n%s", got, card())
	}
	panel := func() string { return stripView(strings.Join(h.m.detailLines(h.m.current(), 120, true), "\n")) }
	tail := regexp.MustCompile(`space\s+台帳から外す|enter\s+初期設定`)
	if got := tail.FindAllString(panel(), -1); len(got) != 2 {
		t.Fatalf("前提: 網を読めた後のパネルに space / enter が無い: %q", got)
	}
	h.m.selfOK = false
	if got := keys.FindAllString(card(), -1); len(got) > 0 {
		t.Errorf("網を読む前にカードが %q を案内した", got)
	}
	if got := tail.FindAllString(panel(), -1); len(got) > 0 {
		t.Errorf("網を読む前にパネルが %q を案内した (space も パネルの enter も「確認中」で断る)", got)
	}
}
