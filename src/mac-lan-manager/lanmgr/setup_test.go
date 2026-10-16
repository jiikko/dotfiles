package lanmgr

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/setup"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// setupHarness は初期設定の画面を開いた状態 (相手は mini。観測は obs が返す値)。
func setupHarness(t *testing.T, obs *setup.PeerNet) (*harness, *[]setup.Dest) {
	t.Helper()
	h := newHarness(t, nil, []discover.Peer{mini})
	var dests []setup.Dest
	h.m.o.Observe = func(_ context.Context, d setup.Dest) setup.PeerNet { dests = append(dests, d); return *obs }
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	h.keys("enter")
	if h.m.sc != scSetup || h.m.su == nil {
		t.Fatalf("Enter で初期設定の画面にならない")
	}
	awaitSetup(h.m)
	return h, &dests
}

// awaitSetup は裏の観測を待って取り込む (テストを観測の速さに依らせない)。
func awaitSetup(m *Model) {
	if ch := m.su.ch; ch != nil {
		m.su.obs, m.su.seen, m.su.ch, m.su.stop = <-ch, true, nil, nil
	}
}

// setSelfStatic はこの Mac の有線を帯の固定 IP にした状態にする。
func setSelfStatic(m *Model) {
	for i := range m.self.Services {
		if m.self.Services[i].Device == "en13" {
			m.self.Services[i].Method, m.self.Services[i].IP = "manual", "192.168.100.10"
		}
	}
}

// runStep は x → y で次の手順を走らせ、Exec ならその引数を返して、戻った (err) ことにする。
func runStep(t *testing.T, h *harness, err error) []string {
	t.Helper()
	if a := h.keys("x"); a != None || h.m.ov != ovConfirm {
		t.Fatalf("x で確認の板が出ない (ov=%v notices %+v)", h.m.ov, h.m.TakeNotices())
	}
	if a := h.keys("y"); a != Exec {
		return nil
	}
	r, ok := h.m.TakeExec()
	if !ok {
		t.Fatal("Exec なのにプロセスが無い")
	}
	h.m.ExecDone(err)
	awaitSetup(h.m)
	return r.Argv
}

// 初期設定を最初から最後まで流す: この Mac の固定 IP → 鍵 → 相手の固定 IP → 新しい IP で確認 → 台帳。各手順は確認の板の後に、
// 端末を明け渡して起こす (台帳だけは手元の書き込み)。戻るたびに相手を読み直す。
func TestSetupWalksAllSteps(t *testing.T) {
	obs := &setup.PeerNet{}
	h, dests := setupHarness(t, obs)
	if d := (*dests)[0]; d != (setup.Dest{User: "koji", IP: "192.168.50.7", HostKeyAlias: "m2macmini.local"}) {
		t.Fatalf("観測の宛先 = %+v (Wi-Fi の IP・推測のユーザー・台帳の鍵)", d)
	}

	argv := runStep(t, h, nil)
	if len(argv) < 6 || argv[0] != "sudo" || argv[3] != "Thunderbolt Ethernet" || !strings.HasPrefix(argv[4], "192.168.100.") {
		t.Fatalf("この Mac の固定 IP = %q", argv)
	}
	setSelfStatic(h.m)

	argv = runStep(t, h, nil)
	if argv[0] != "sh" || !strings.Contains(argv[2], "ssh-copy-id") {
		t.Fatalf("鍵 = %q", argv)
	}
	*obs = setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h.keys("r")
	awaitSetup(h.m)

	argv = runStep(t, h, nil)
	if !slices.Contains(argv, "-t") || argv[slices.Index(argv, "--")+1] != "koji@192.168.50.7" {
		t.Fatalf("相手の固定 IP = %q", argv)
	}
	c := h.m.su.conf
	if c == nil || c.static.IP == "192.168.100.10" || !strings.HasPrefix(c.static.IP, "192.168.100.") {
		t.Fatalf("確認待ちにならない・この Mac と同じ IP を選んだ: %+v", c)
	}
	if st, _ := nextStep(h.m.setupSteps()); st.ID != stepConfirm {
		t.Fatalf("次の手順 = %v (確認が先)", st.ID)
	}
	if !h.m.Busy() {
		t.Error("確認待ちの間に Busy でない (残り時間の表示が進まない)")
	}

	argv = runStep(t, h, nil)
	if argv[slices.Index(argv, "--")+1] != "koji@"+c.static.IP {
		t.Fatalf("確認は新しい IP で入る: %q", argv)
	}
	if h.m.su.conf != nil {
		t.Fatal("確認の後も確認待ちのまま")
	}
	*obs = setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: c.static.IP}
	h.keys("r")
	awaitSetup(h.m)

	if argv := runStep(t, h, nil); argv != nil {
		t.Fatalf("台帳は手元の書き込み (プロセスを起こさない): %q", argv)
	}
	if len(h.store.hosts) != 1 || h.store.hosts[0] != (ledger.Host{Host: "m2macmini.local", User: "koji", LAN: c.static.IP}) {
		t.Fatalf("台帳 = %+v", h.store.hosts)
	}
	if _, ok := nextStep(h.m.setupSteps()); ok {
		t.Errorf("全部済んだのに次の手順がある: %+v", h.m.setupSteps())
	}
}

// 相手を固定 IP にするプロセスが失敗したら (sudo のパスワードを間違えた等)、確認待ちにしない (変わっていない相手に新しい IP で入りに行かない)。
func TestSetupPeerStaticFailureDoesNotWaitConfirm(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	setSelfStatic(h.m)
	h.keys("x", "y")
	if _, ok := h.m.TakeExec(); !ok {
		t.Fatal("相手の固定 IP のプロセスが無い")
	}
	if msg := h.m.ExecDone(exitCodeError{}); msg == "" {
		t.Error("失敗を知らせない (終了コード ≠ 0 は失敗として返す)")
	}
	awaitSetup(h.m)
	if h.m.su.conf != nil {
		t.Fatalf("失敗したのに確認待ちにした: %+v", h.m.su.conf)
	}
}

// 止まっている手順・読み込み中は x で何も起こさず、理由を知らせる。この Mac の行の Enter は画面を開かない。
func TestSetupRefusals(t *testing.T) {
	obs := &setup.PeerNet{KeyErr: errors.Join(setup.ErrUnknown, errors.New("timeout"))}
	h, _ := setupHarness(t, obs)
	h.m.pf.Band = "" // 帯が無い: 1 つ目の手順が止まる
	h.keys("x")
	if h.m.ov != ovNone {
		t.Fatal("止まっている手順で確認の板を出した")
	}
	h.m.pf.Band = "192.168.100.0/24"
	h.m.su.ch = make(chan setup.PeerNet) // 読み込み中
	h.keys("x")
	if h.m.ov != ovNone {
		t.Fatal("読み込み中に確認の板を出した")
	}
	h.m.su.ch = nil
	h.keys("q")
	if h.m.sc != scList || h.m.su != nil {
		t.Fatal("q で一覧に戻らない")
	}

	h2 := newHarness(t, nil, []discover.Peer{mini})
	h2.m.o.Observe = func(context.Context, setup.Dest) setup.PeerNet {
		t.Fatal("この Mac を観測した")
		return setup.PeerNet{}
	}
	h2.keys("l", "enter") // 未管理のタブの先頭はこの Mac
	if h2.m.sc == scSetup {
		t.Error("この Mac の行で初期設定の画面を開いた")
	}
}

// 初期設定の画面はどの幅・高さでも高さちょうどの行を返し、幅を超えない (足りなければ抜け方だけの画面)。
func TestSetupViewFits(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	for _, w := range []int{minWidth, 60, 100, 200} {
		for hgt := 5; hgt <= 45; hgt++ {
			h.m.Resize(w, hgt)
			lines := h.m.View()
			if len(lines) != hgt {
				t.Fatalf("%dx%d: %d 行", w, hgt, len(lines))
			}
			for i, l := range lines {
				if lw := termwidth.Of(l); lw > w {
					t.Fatalf("%dx%d: %d 行目が %d 桁", w, hgt, i, lw)
				}
			}
		}
	}
	h.m.Resize(120, 45)
	view := termwidth.StripSGR(strings.Join(h.m.View(), "\n"))
	for _, want := range []string{"初期設定", "この Mac の有線を固定 IP にする", "この手順で実行するもの", "x 実行"} {
		if !strings.Contains(view, want) {
			t.Errorf("画面に %q が無い:\n%s", want, view)
		}
	}
}
