package lanmgr

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/setup"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// setupHarness は初期設定の画面を開いた状態 (相手は mini。観測は obs が返す値)。観測は裏の goroutine で走るので、obs は
// setObs でだけ書き換え、宛先の記録は dests で読む (直に書くと、走っている観測とどちらの値を読むかが決まらなかった。-race で検出)。
func setupHarness(t *testing.T, obs *setup.PeerNet) (*harness, *observed) {
	t.Helper()
	h := newHarness(t, nil, []discover.Peer{mini})
	o := &observed{v: obs}
	h.m.o.Observe = func(_ context.Context, d setup.Dest) setup.PeerNet {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.dests = append(o.dests, d)
		return *o.v
	}
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	h.keys("enter")
	if h.m.sc != scSetup || h.m.su == nil {
		t.Fatalf("Enter で初期設定の画面にならない")
	}
	awaitSetup(h.m)
	return h, o
}

// observed は偽物の観測が返す値と、観測した宛先の記録。
type observed struct {
	mu    sync.Mutex
	v     *setup.PeerNet
	dests []setup.Dest
}

// setObs は次の観測が返す値を替える。観測が走っている間は替えない (どちらの値で読むかが決まらない)。
func setObs(t *testing.T, h *harness, o *observed, p setup.PeerNet) {
	t.Helper()
	if h.m.su != nil && h.m.su.ch != nil {
		t.Fatal("観測が走っている間に観測の値を替えようとした (先に awaitSetup で取り込む)")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	*o.v = p
}

func (o *observed) destsCopy() []setup.Dest {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.dests)
}

// awaitSetup は裏の観測を待って取り込む (テストを観測の速さに依らせない)。
func awaitSetup(m *Model) {
	if ch := m.su.ch; ch != nil {
		m.applyObserve(<-ch)
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
	if d := dests.destsCopy()[0]; d != (setup.Dest{User: "koji", IP: "192.168.50.7", HostKeyAlias: "m2macmini.local"}) {
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
	setObs(t, h, dests, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"})
	h.keys("r")
	awaitSetup(h.m)

	argv = runStep(t, h, nil)
	if !slices.Contains(argv, "-t") || argv[slices.Index(argv, "--")+1] != "koji@192.168.50.7" {
		t.Fatalf("相手の固定 IP = %q", argv)
	}
	c := h.m.conf()
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
	if h.m.conf() != nil {
		t.Fatal("確認の後も確認待ちのまま")
	}
	setObs(t, h, dests, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: c.static.IP})
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
	if h.m.conf() != nil {
		t.Fatalf("失敗したのに確認待ちにした: %+v", h.m.conf())
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

	// 止まっている手順の理由だけで断る形 (コマンドは組める): ssh を広告していない相手では「公開鍵を置く」が止まる
	vnc := discover.Peer{Host: "vnc.local", Instances: []string{"画面共有だけ"}, VNC: true, Addrs: map[int]string{14: "192.168.50.8"}}
	h3 := newHarness(t, nil, []discover.Peer{vnc})
	h3.m.o.Observe = func(context.Context, setup.Dest) setup.PeerNet { return setup.PeerNet{} }
	setSelfStatic(h3.m)
	h3.keys("l")
	h3.cursorTo(t, "vnc.local")
	h3.keys("enter")
	awaitSetup(h3.m)
	if st, _ := nextStep(h3.m.setupSteps()); st.ID != setup.StepKey || st.Blocked == "" {
		t.Fatalf("前提: 公開鍵の手順が止まっていない: %+v", st)
	}
	h3.keys("x")
	if h3.m.ov != ovNone {
		t.Fatal("止まっている手順 (リモートログインがオフ) で確認の板を出した")
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

// 相手を一度も読めていない (観測の口が無い版を含む) ときは x で何も起こさない (本物の sudo / ssh-copy-id を、何も分からないまま起こした。敵対レビュー)。
func TestSetupRefusesWithoutObservation(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	h.keys("enter")
	if h.m.sc != scSetup {
		t.Fatal("前提: 初期設定の画面が開かない")
	}
	h.keys("x")
	if h.m.ov != ovNone {
		t.Fatal("観測の無いまま確認の板を出した")
	}
}

// 確認待ちは画面を閉じて開き直しても残り、期限が過ぎたら片付く (画面に持っていたころは開き直すと消え、未確認の IP を台帳に書けた。
// 期限切れは抜けられず、Busy が立ちっぱなしだった。敵対レビュー)。
func TestSetupConfirmSurvivesReopenAndExpires(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	setSelfStatic(h.m)
	runStep(t, h, nil)
	c := h.m.conf()
	if c == nil {
		t.Fatal("前提: 確認待ちにならない")
	}
	setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: c.static.IP}) // 窓の間は新しい IP に見える
	h.keys("q", "enter")
	awaitSetup(h.m)
	if st, _ := nextStep(h.m.setupSteps()); st.ID != stepConfirm {
		t.Fatalf("開き直したら確認を飛ばした: 次の手順 = %v", st.ID)
	}
	late := t0.Add(setup.PeerDelay + setup.PeerWindow + time.Second)
	h.m.o.Now = func() time.Time { return late }
	h.m.Advance(late)
	if h.m.su.ch == nil {
		t.Fatal("期限が過ぎても読み直さない (窓の間の観測のまま次の手順を組む)")
	}
	awaitSetup(h.m)
	// 確認の期限の直後は、相手の上の戻しがまだ終わっていない: その観測 (まだ新しい IP に見える) で未確認の印を解かない (敵対レビュー)
	if _, ok := h.m.unconf["m2macmini.local"]; !ok {
		t.Fatal("確認の期限の直後の観測で未確認の印を解いた")
	}
	if !h.m.Busy() {
		t.Fatal("未確認の印が残る間に Busy でない (tick が止まり、戻しが終わる頃の読み直しが次のキーまで遅れる)")
	}
	h.keys("x")
	if h.m.ov == ovConfirm && h.m.ask != nil && h.m.ask.title == "台帳に内部 IP 付きで載せる" {
		t.Fatal("確認の期限の直後に台帳の板を出した")
	}
	after := late.Add(unconfMargin + time.Second)
	h.m.o.Now = func() time.Time { return after }
	h.m.Advance(after) // 戻しが終わる頃に読み直す
	awaitSetup(h.m)
	if _, ok := h.m.unconf["m2macmini.local"]; ok {
		t.Error("戻しが終わる頃の観測でも未確認のまま")
	}
	if h.m.conf() != nil || h.m.Busy() {
		t.Fatalf("期限が過ぎても確認待ちのまま (conf=%+v busy=%v)", h.m.conf(), h.m.Busy())
	}
	if _, ok := h.m.claimed["m2macmini.local"]; ok {
		t.Error("期限が過ぎた番号を割り当て済みのまま")
	}
}

// 2 台目の相手には、1 台目に割り当てた番号を選ばない (台帳に載る前の新しい IP は、台帳にも Bonjour の行にもまだ無い。敵対レビュー)。
func TestSetupSecondPeerGetsDifferentIP(t *testing.T) {
	air := discover.Peer{Host: "air.local", Instances: []string{"air"}, SSH: true, Addrs: map[int]string{14: "192.168.50.9"}}
	h := newHarness(t, nil, []discover.Peer{mini, air})
	h.m.o.Observe = func(context.Context, setup.Dest) setup.PeerNet {
		return setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.1.1"}
	}
	setSelfStatic(h.m)
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	h.keys("enter")
	awaitSetup(h.m)
	runStep(t, h, nil)
	first := h.m.conf().static.IP
	h.keys("q")
	h.cursorTo(t, "air.local")
	h.keys("enter")
	awaitSetup(h.m)
	argv := runStep(t, h, nil)
	second := h.m.conf().static.IP
	if second == first || argv == nil {
		t.Fatalf("2 台目に 1 台目と同じ番号 (%s / %s)", first, second)
	}
}

// 相手に Wi-Fi で入れない (有線の 169.254 でしか見えない) ときは、この Mac の固定を止める (先に固定すると相手に届かなくなる)。
// 台帳の別の Mac と同じ内部 IP は台帳に書かない。
func TestSetupGuards(t *testing.T) {
	wired := discover.Peer{Host: "wired.local", Instances: []string{"wired"}, SSH: true, Addrs: map[int]string{27: "169.254.9.9"}}
	h := newHarness(t, nil, []discover.Peer{wired})
	h.m.o.Observe = func(context.Context, setup.Dest) setup.PeerNet { return setup.PeerNet{} }
	h.keys("l")
	h.cursorTo(t, "wired.local")
	h.keys("enter")
	awaitSetup(h.m)
	if st, _ := nextStep(h.m.setupSteps()); st.ID != setup.StepSelfStatic || !strings.Contains(st.Blocked, "Wi-Fi") {
		t.Errorf("Wi-Fi で入れない相手でこの Mac の固定を止めない: %+v", st)
	}

	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: "192.168.100.12"}
	h2, _ := setupHarness(t, obs)
	setSelfStatic(h2.m)
	h2.m.hosts = append(h2.m.hosts, ledger.Host{Host: "studio.local", LAN: "192.168.100.12"})
	h2.keys("x")
	if h2.m.ov != ovNone {
		t.Fatal("台帳の別の Mac と同じ内部 IP を書く確認の板を出した")
	}
}

// 読みに行ったが届かない (判定できない) 相手では x を断る (この Mac の固定は自動では戻らないので、届かない相手に向けて始めない)。
// 手元の期限を過ぎて 0 で戻った確認も確認済みとして扱い、割り当てた番号を残す (0 は相手の待ちが印を受け取ったこと)。
func TestSetupUnreachableAndLateConfirm(t *testing.T) {
	obs := &setup.PeerNet{KeyErr: fmt.Errorf("%w: timeout", setup.ErrUnknown)}
	h, ob := setupHarness(t, obs)
	h.keys("x")
	if h.m.ov != ovNone {
		t.Fatal("届かない相手で確認の板を出した")
	}

	setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"})
	h.keys("r")
	awaitSetup(h.m)
	setSelfStatic(h.m)
	runStep(t, h, nil)
	c := h.m.conf()
	if c == nil {
		t.Fatal("前提: 確認待ちにならない")
	}
	late := c.deadline.Add(time.Second)
	h.m.o.Now = func() time.Time { return late }
	// 実際の TUI の順: 端末を渡している間の tick (Advance) が、プロセスから戻る (ExecDone) より先に届く
	h.keys("x", "y")
	if _, ok := h.m.TakeExec(); !ok {
		t.Fatal("確認のプロセスが無い")
	}
	h.m.Advance(late)
	h.m.ExecDone(nil)
	awaitSetup(h.m)
	if h.m.conf() != nil {
		t.Error("確認の後も確認待ちのまま")
	}
	if h.m.claimed["m2macmini.local"] != c.static.IP {
		t.Errorf("間際の確認で番号を落とした: %+v", h.m.claimed)
	}
	if h.m.confirmed["m2macmini.local"] != c.static.IP {
		t.Error("間際の確認を確認済みにしない (相手は固定のまま、台帳に書けなくなった。敵対レビュー)")
	}
	if why := h.m.ledgerBlocked("m2macmini.local", c.static.IP); why != "" {
		t.Errorf("間際に確認した番号を台帳に書けない: %s", why)
	}
}

// 有線でしか入れない相手は、確認待ちの間に古い IP の観測が届かなくても、確認の手順は断らない (確認は新しい IP へ入る。敵対レビュー)。
func TestSetupWiredOnlyConfirmNotBlockedByUnreachable(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	setSelfStatic(h.m)
	runStep(t, h, nil)
	if h.m.conf() == nil {
		t.Fatal("前提: 確認待ちにならない")
	}
	setObs(t, h, ob, setup.PeerNet{KeyErr: fmt.Errorf("%w: timeout", setup.ErrUnknown)}) // 相手は新しい IP に移り、古い宛先の観測が届かない
	h.keys("r")
	awaitSetup(h.m)
	h.keys("x")
	if h.m.ov != ovConfirm {
		t.Fatalf("確認待ちの間に、届かないことを理由に確認まで断った (notices %+v)", h.m.TakeNotices())
	}
}

// 固定 IP にした後、確認しないまま期限が切れても、窓の間の古い観測 (新しい IP に見える) で台帳に書かない。失敗で戻ったが相手の上で
// 変わっていた場合も同じ (台帳の手順が確認を知らずに観測だけを信じた。敵対レビュー)。期限の後の板の y では確認を起こさない。
func TestSetupLedgerNeedsConfirm(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	setSelfStatic(h.m)
	h.keys("x", "y")
	h.m.TakeExec()
	setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: "192.168.100.11"})
	h.m.ExecDone(exitCodeError{}) // 失敗で戻ったが、相手の上では変わっていた
	awaitSetup(h.m)
	h.keys("x")
	if h.m.ov == ovConfirm && h.m.ask != nil && h.m.ask.yes != nil {
		h.keys("y")
	}
	if len(h.store.hosts) != 0 {
		t.Fatalf("確認していない固定 IP を台帳に書いた: %+v", h.store.hosts)
	}

	obs2 := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h2, _ := setupHarness(t, obs2)
	setSelfStatic(h2.m)
	runStep(t, h2, nil)
	h2.keys("x") // 確認の板
	if h2.m.ov != ovConfirm {
		t.Fatal("前提: 確認の板が出ない")
	}
	late := t0.Add(setup.PeerDelay + setup.PeerWindow + time.Second)
	h2.m.Advance(late)
	if a := h2.keys("y"); a == Exec {
		t.Error("期限が過ぎた確認の板の y で確認を起こした")
	}
}

// 有線だけの相手は、確認した後に閉じて開き直しても、確認した新しい IP へ入る (行の 169.254 は古い。敵対レビュー)。
func TestSetupWiredOnlyReopenUsesConfirmedIP(t *testing.T) {
	wired := discover.Peer{Host: "wired.local", Instances: []string{"wired"}, SSH: true, Addrs: map[int]string{27: "169.254.9.9"}}
	h := newHarness(t, nil, []discover.Peer{wired})
	var dests []setup.Dest
	h.m.o.Observe = func(_ context.Context, d setup.Dest) setup.PeerNet {
		dests = append(dests, d)
		return setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.9.9"}
	}
	setSelfStatic(h.m)
	h.keys("l")
	h.cursorTo(t, "wired.local")
	h.keys("enter")
	awaitSetup(h.m)
	runStep(t, h, nil) // 相手の固定 IP
	ip := h.m.conf().static.IP
	runStep(t, h, nil) // 確認
	h.keys("q", "enter")
	awaitSetup(h.m)
	if got := dests[len(dests)-1].IP; got != ip {
		t.Errorf("開き直した宛先 = %s (確認した %s)", got, ip)
	}
}

// 一覧の space (台帳に登録) も、未確認の固定 IP の相手は登録しない (台帳に書く経路が 2 本あり、片方が未確認の印を見なかった。敵対レビュー)。
// 固定 IP のプロセスの最中に画面を閉じても、戻ったら未確認の印を立てる。
func TestSetupLedgerGuardsAllPaths(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	setSelfStatic(h.m)
	h.keys("x", "y")
	if _, ok := h.m.TakeExec(); !ok {
		t.Fatal("前提: 固定 IP のプロセスが無い")
	}
	h.m.Close() // 端末を渡す前に閉じられた (glogx の L の先打ち)
	h.m.ExecDone(nil)
	if _, ok := h.m.unconf["m2macmini.local"]; !ok {
		t.Fatal("閉じた後に戻ったプロセスで未確認の印を立てない")
	}
	h.m.Reload()
	h.keys("q") // 一覧へ
	h.m.sc = scList
	h.cursorTo(t, "m2macmini.local")
	h.keys("space")
	if h.m.ov == ovConfirm {
		t.Fatal("未確認の固定 IP の相手を一覧の space で台帳に登録しようとした")
	}
}

// 確認のプロセスを渡した後に閉じられ、その戻りが届かないまま一覧の s (ssh) を起こしても、ssh の戻りを確認の成功として扱わない
// (閉じたときに exec の印を残していたので、後の無関係な戻りが確認に流れて台帳に書けた。敵対レビュー)。
func TestSetupStaleExecMarkNotReused(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	setSelfStatic(h.m)
	runStep(t, h, nil) // 相手の固定 IP → 確認待ち
	h.keys("x", "y")   // 確認のプロセスを渡す
	if _, ok := h.m.TakeExec(); !ok {
		t.Fatal("前提: 確認のプロセスが無い")
	}
	h.m.Close() // 戻りが届かない (glogx は閉じた板に ExecDone を渡さない)
	h.m.Reload()
	h.cursorTo(t, "m2macmini.local")
	if h.keys("s") != Exec {
		t.Fatal("前提: s で ssh を起こさない")
	}
	h.m.TakeExec()
	h.m.ExecDone(nil)
	if _, ok := h.m.confirmed["m2macmini.local"]; ok {
		t.Error("ssh の戻りを確認の成功として扱った")
	}
	if why := h.m.ledgerBlocked("m2macmini.local", ""); why == "" {
		t.Error("確認していないのに台帳に書ける")
	}
}

// 固定 IP にしようとして確認しないまま期限が過ぎた相手を、一覧の space で台帳に書くとき、Bonjour の行に残った試した番号を内部 IP に
// しない (discover はサービスの出し直しのときしか A レコードを引き直さず、相手が元に戻っても行に古い番号が残る。敵対レビュー)。
func TestSetupListSpaceSkipsTriedIP(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	setSelfStatic(h.m)
	runStep(t, h, nil)
	c := h.m.conf()
	if c == nil {
		t.Fatal("前提: 確認待ちにならない")
	}
	p := mini
	p.Addrs = map[int]string{14: "192.168.50.7", 27: c.static.IP} // 窓の間に走査が新しい番号を拾った
	h.scan.peers = []discover.Peer{p}
	after := t0.Add(setup.PeerDelay + setup.PeerWindow + unconfMargin + 10*time.Second)
	h.m.o.Now = func() time.Time { return after }
	h.m.Advance(after) // 確認の期限が過ぎ、戻しの後に読み直す (相手は元の DHCP に見える)
	awaitSetup(h.m)
	if _, ok := h.m.unconf["m2macmini.local"]; ok {
		t.Fatal("前提: 戻しの後の観測で未確認の印が解けない")
	}
	h.keys("q")
	h.m.sc = scList
	h.m.Reload()
	h.cursorTo(t, "m2macmini.local")
	h.keys("space")
	if h.m.ov == ovConfirm && h.m.ask != nil && strings.Contains(strings.Join(h.m.ask.body, "\n"), c.static.IP) {
		t.Fatalf("確認できなかった番号 %s を台帳に書こうとした: %q", c.static.IP, h.m.ask.body)
	}
}

// 2 回目に別の番号を試した後も、1 回目に試して確認できなかった番号を一覧の space で台帳に書かない
// (試した番号を 1 つしか持たず、2 回目で上書きされて 1 回目の守りが消えた。敵対レビュー)。
func TestSetupTriedIPsSurviveRetry(t *testing.T) {
	for _, confirm := range []bool{true, false} {
		t.Run(fmt.Sprintf("2 回目を確認=%v", confirm), func(t *testing.T) { triedIPsSurviveRetry(t, confirm) })
	}
}

func triedIPsSurviveRetry(t *testing.T, confirm bool) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	setSelfStatic(h.m)
	expire := func(at time.Time) {
		h.m.o.Now = func() time.Time { return at }
		h.m.Advance(at)
		awaitSetup(h.m)
	}
	runStep(t, h, nil)
	first := h.m.conf().static.IP
	p := mini
	p.Addrs = map[int]string{14: "192.168.50.7", 27: first} // 窓の間に走査が 1 回目の番号を拾い、そのまま残る
	h.scan.peers = []discover.Peer{p}
	t1 := t0.Add(setup.PeerDelay + setup.PeerWindow + unconfMargin + 10*time.Second)
	expire(t1)
	h.m.Reload()
	awaitNetLoad(h.m) // 読み直したこの Mac の網を取り込んでから固定を立て直す (取り込みが x の後に来ると、この Mac の手順に戻った)
	setSelfStatic(h.m)
	runStep(t, h, nil) // 2 回目
	c := h.m.conf()
	if c == nil || c.static.IP == first {
		t.Fatalf("前提: 2 回目が別の番号にならない (%+v)", c)
	}
	second := c.static.IP
	t2 := t1.Add(setup.PeerDelay + setup.PeerWindow + unconfMargin + 10*time.Second)
	if confirm {
		runStep(t, h, nil) // 2 回目は確認する: 確認した番号だけを外す (集合ごと消すと 1 回目の守りも消えた)
	} else {
		runStep(t, h, errors.New("exit status 1")) // 確認は受け取られなかったと返ったが、相手は固定のまま
		setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: second, Mask: "255.255.255.0"})
	}
	expire(t2)
	if why := h.m.ledgerBlocked("m2macmini.local", second); why != "" {
		t.Errorf("確定した 2 回目の番号 %s を台帳に書けない: %s", second, why)
	}
	h.keys("q")
	h.m.sc = scList
	h.m.Reload()
	h.cursorTo(t, "m2macmini.local")
	h.keys("space")
	if h.m.ov == ovConfirm && h.m.ask != nil && strings.Contains(strings.Join(h.m.ask.body, "\n"), first) {
		t.Fatalf("2 回目の後に 1 回目の番号 %s を台帳に書こうとした", first)
	}
}

// 確認が非 0 で戻っても (待ちの終わりと同時に読まれた印)、期限の後の観測で試した番号の手動の固定に見えれば、相手の上で確定している:
// 台帳の手順を断り続けない (試した番号を確認でしか外さず、正しく固定された相手を台帳に書けなくなった。敵対レビュー)。
func TestSetupFixedAfterFailedConfirmCanLedger(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	setSelfStatic(h.m)
	runStep(t, h, nil)
	ip := h.m.conf().static.IP
	runStep(t, h, errors.New("exit status 1")) // 確認は受け取られなかったと返った
	setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: ip, Mask: "255.255.255.0"})
	at := t0.Add(setup.PeerDelay + setup.PeerWindow + unconfMargin + 10*time.Second)
	h.m.o.Now = func() time.Time { return at }
	h.m.Advance(at)
	awaitSetup(h.m)
	if why := h.m.ledgerBlocked("m2macmini.local", ip); why != "" {
		t.Errorf("期限の後に固定が見えた相手を台帳に書けない: %s", why)
	}
}

// 期限の後の最初の観測が読めなくても未確認の印を解かず、次に読めた観測で固定が見えれば試した番号を外す
// (読めない観測で印を解き、後で固定が見えても試した番号が外れず、台帳に書けなくなった。敵対レビュー)。
func TestSetupFailedObserveKeepsUnconf(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	setSelfStatic(h.m)
	runStep(t, h, nil)
	ip := h.m.conf().static.IP
	runStep(t, h, errors.New("exit status 1"))
	setObs(t, h, ob, setup.PeerNet{KeyErr: fmt.Errorf("%w: timeout", setup.ErrUnknown)})
	at := t0.Add(setup.PeerDelay + setup.PeerWindow + unconfMargin + 10*time.Second)
	h.m.o.Now = func() time.Time { return at }
	h.m.Advance(at)
	awaitSetup(h.m)
	if _, ok := h.m.unconf["m2macmini.local"]; !ok {
		t.Fatal("読めなかった観測で未確認の印を解いた")
	}
	setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: ip, Mask: "255.255.255.0"})
	h.keys("r")
	awaitSetup(h.m)
	if why := h.m.ledgerBlocked("m2macmini.local", ip); why != "" {
		t.Errorf("読み直して固定が見えたのに台帳に書けない: %s", why)
	}
}
