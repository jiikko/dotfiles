package lanmgr

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
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

// holdObserve は次からの観測を、返した release を呼ぶまで止める (偽の観測はすぐ返るので、止めないと「読み込み中に押したキー」の前に
// 観測が終わりうる。キーの入口の処理が重くなった版で、-race の下で順序が逆になって落ちた)。
func holdObserve(t *testing.T, h *harness) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	prev := h.m.o.Observe
	h.m.o.Observe = func(ctx context.Context, d setup.Dest) setup.PeerNet {
		<-gate
		return prev(ctx, d)
	}
	return release
}

// setSelfStatic はこの Mac の有線を帯の固定 IP にした状態にする (この Mac の網の読み直しも、その状態を返す)。
func setSelfStatic(m *Model) {
	awaitNetLoad(m) // 走っている読み直し (手順から戻ったときのもの) が後から届いて上書きしないように、先に取り込む
	for i := range m.self.Services {
		if m.self.Services[i].Device == "en13" {
			m.self.Services[i].Method, m.self.Services[i].IP = "manual", "192.168.100.10"
		}
	}
	snap := m.self
	m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return snap, nil }
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

// 初期設定を最初から最後まで流す: 鍵 → この Mac の固定 IP → 相手の固定 IP → 新しい IP で確認 → 台帳 (鍵が先: 人が相手の指紋を
// 確かめるまでどちらの Mac も変えない)。各手順は確認の板の後に、
// 端末を明け渡して起こす (台帳だけは手元の書き込み)。戻るたびに相手を読み直す。
func TestSetupWalksAllSteps(t *testing.T) {
	obs := &setup.PeerNet{}
	h, dests := setupHarness(t, obs)
	if d := dests.destsCopy()[0]; d != (setup.Dest{User: "koji", IP: "192.168.50.7", HostKeyAlias: "m2macmini.local"}) {
		t.Fatalf("観測の宛先 = %+v (Wi-Fi の IP・推測のユーザー・台帳の鍵)", d)
	}

	argv := runStep(t, h, nil)
	if argv[0] != "sh" || !strings.Contains(argv[2], "ssh-copy-id") {
		t.Fatalf("鍵 = %q", argv)
	}
	setObs(t, h, dests, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"})
	h.keys("r")
	awaitSetup(h.m)

	argv = runStep(t, h, nil)
	if len(argv) < 6 || argv[0] != "sudo" || argv[3] != "Thunderbolt Ethernet" || !strings.HasPrefix(argv[4], "192.168.100.") || argv[5] != "255.255.255.0" {
		t.Fatalf("この Mac の固定 IP = %q", argv)
	}
	setSelfStatic(h.m)

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

	// 有線の名前: この Mac → 相手。どちらも広告する Mac の launchd の状態で済みを決める (名前の解決では見ない)
	advert := setup.NameState{}
	h.m.o.ReadAdvert = func(context.Context) (setup.NameState, error) { return advert, nil }
	h.m.startNetLoad()
	awaitNetLoad(h.m)
	peerObs := setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: c.static.IP, AdvertKnown: true}
	setObs(t, h, dests, peerObs)
	h.keys("r")
	awaitSetup(h.m)
	advert = setup.NameState{Running: true, Name: "kojim3macbook-pro.wired.local", IP: "192.168.100.10"} // 入れた後に読む値
	argv = runStep(t, h, nil)
	awaitNetLoad(h.m)
	if len(argv) != 3 || argv[0] != "sh" || !strings.Contains(argv[2], "kojim3macbook-pro.wired.local") || !strings.Contains(argv[2], "192.168.100.10") {
		t.Fatalf("この Mac の名前の広告 = %q", argv)
	}
	peerObs.Advert = setup.NameState{Running: true, Name: "m2macmini.wired.local", IP: c.static.IP}
	setObs(t, h, dests, peerObs) // 入れた後の観測が返す値
	argv = runStep(t, h, nil)
	if i := slices.Index(argv, "--"); i < 0 || argv[i+1] != "koji@192.168.50.7" || !strings.Contains(argv[len(argv)-1], "m2macmini.wired.local") ||
		!strings.Contains(argv[len(argv)-1], c.static.IP) {
		t.Fatalf("相手の名前の広告 = %q", argv)
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
	h2.keys("h", "enter") // この Mac のタブ
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

// 相手を一度も読めていない (観測の口が無い版を含む) ときは x で何も起こさない (本物の sudo / ssh-copy-id を、何も分からないまま起こした)。
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
// 期限切れは抜けられず、Busy が立ちっぱなしだった)。
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
	// 確認の期限の直後は、相手の上の戻しがまだ終わっていない: その観測 (まだ新しい IP に見える) で未確認の印を解かない
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

// 2 台目の相手には、1 台目に割り当てた番号を選ばない (台帳に載る前の新しい IP は、台帳にも Bonjour の行にもまだ無い)。
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
	steps := h.m.setupSteps()
	if st, _ := nextStep(steps); st.ID != setup.StepKey || st.Blocked != "" {
		t.Errorf("鍵の手順 (169.254 で入る) が先に来ない: %+v", st)
	}
	selfStep := func() setup.Step {
		steps := h.m.setupSteps()
		return steps[slices.IndexFunc(steps, func(s setup.Step) bool { return s.ID == setup.StepSelfStatic })]
	}
	// 帯が 169.254 の中なら止めない: マスクは /16 で、固定した後も 169.254 の相手と同じ網に居る
	h.m.pf.Band = "169.254.100.0/24"
	if st := selfStep(); st.Blocked != "" {
		t.Errorf("169.254 の帯で、Wi-Fi で入れない相手のためにこの Mac の固定を止めた: %+v", st)
	}
	h.m.pf.Band = "192.168.100.0/24" // 169.254 の外の帯: 固定すると 169.254 の相手に届かなくなる
	if st := selfStep(); !strings.Contains(st.Blocked, "Wi-Fi") {
		t.Errorf("169.254 の外の帯で、Wi-Fi で入れない相手でこの Mac の固定を止めない: %+v", st)
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

// 有線でしか入れない相手は、確認待ちの間に古い IP の観測が届かなくても、確認の手順は断らない (確認は新しい IP へ入る)。
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
// 変わっていた場合も同じ (台帳の手順が確認を知らずに観測だけを信じた)。期限の後の板の y では確認を起こさない。
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

// 有線だけの相手は、確認した後に閉じて開き直しても、確認した新しい IP へ入る (行の 169.254 は古い)。
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

// 一覧の space (台帳に登録) も、未確認の固定 IP の相手は登録しない (台帳に書く経路が 2 本あり、片方が未確認の印を見なかった)。
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
// (閉じたときに exec の印を残していたので、後の無関係な戻りが確認に流れて台帳に書けた)。
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
// しない (discover はサービスの出し直しのときしか A レコードを引き直さず、相手が元に戻っても行に古い番号が残る)。
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
// (試した番号を 1 つしか持たず、2 回目で上書きされて 1 回目の守りが消えた)。
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
// 台帳の手順を断り続けない (試した番号を確認でしか外さず、正しく固定された相手を台帳に書けなくなった)。
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
// (読めない観測で印を解き、後で固定が見えても試した番号が外れず、台帳に書けなくなった)。
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

// 期限の後の観測が読めずに未確認の印が残ったら、画面に r で読み直す 1 行を出す (tick が止まり、断りの文言を読んだ人にしか届かなかった)。
func TestSetupOverdueUnconfAsksReload(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	setSelfStatic(h.m)
	runStep(t, h, nil)
	if strings.Contains(strings.Join(h.m.View(), "\n"), "確認が済んでいない") {
		t.Fatal("前提: 期限の前から読み直しを促した")
	}
	runStep(t, h, errors.New("exit status 1"))
	setObs(t, h, ob, setup.PeerNet{KeyErr: fmt.Errorf("%w: timeout", setup.ErrUnknown)})
	at := t0.Add(setup.PeerDelay + setup.PeerWindow + unconfMargin + 10*time.Second)
	h.m.o.Now = func() time.Time { return at }
	h.m.Advance(at)
	awaitSetup(h.m)
	if !strings.Contains(strings.Join(h.m.View(), "\n"), "確認が済んでいない") {
		t.Error("期限の後の観測が読めず印が残ったのに、読み直しを促さない")
	}
}

// 初めての相手 (ホスト鍵をまだ知らない) でも x を断らない: 鍵を置く手順 (指紋を聞ける ssh-copy-id) へ進めるように
// (「判定できない」と同じに断っていて、初めての相手では初期設定を始められなかった。air5 で実測)。
func TestSetupFirstContactNotRefused(t *testing.T) {
	obs := &setup.PeerNet{HostKeyUnknown: true}
	h, _ := setupHarness(t, obs)
	setSelfStatic(h.m)
	h.keys("x")
	if h.m.ov != ovConfirm || h.m.ask == nil || h.m.ask.title != "相手に公開鍵を置く" {
		t.Fatalf("初めての相手で鍵を置く手順の板が出ない (ov=%v ask=%+v notices=%+v)", h.m.ov, h.m.ask, h.m.TakeNotices())
	}
	if !strings.Contains(strings.Join(h.m.ask.body, "\n"), "指紋") {
		t.Error("初めての相手で指紋を確かめる案内を出さない")
	}
}

// この Mac の有線を固定 IP にする手順から戻ったら、この Mac の網も読み直して次の手順へ進む (相手だけを読み直していたので、固定 IP に
// しても「未固定」のまま進まなかった。ユーザー報告 2026-10-16)。
func TestSetupSelfStaticReloadsSelf(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	if st, _ := nextStep(h.m.setupSteps()); st.ID != setup.StepSelfStatic {
		t.Fatalf("前提: 次の手順 = %v", st.ID)
	}
	h.keys("x", "y")
	r, ok := h.m.TakeExec()
	if !ok || len(r.Argv) < 5 || r.Argv[0] != "sudo" {
		t.Fatalf("前提: この Mac の固定 IP を起こさない: %+v", r)
	}
	ip := r.Argv[4]
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { // 実際に固定 IP になった後のこの Mac
		s := selfNet()
		s.Services[0].Method, s.Services[0].IP = "manual", ip
		return s, nil
	}
	h.m.ExecDone(nil)
	awaitNetLoad(h.m)
	awaitSetup(h.m)
	if st, _ := nextStep(h.m.setupSteps()); st.ID == setup.StepSelfStatic {
		t.Errorf("この Mac を固定した後も次の手順がこの Mac の固定のまま")
	}
}

// 読み込み中の x は断らず (toast を出さず) 予約し、読み終えたら次の手順の板を出す。r も toast を出さない。手順から戻った後、
// その結果を確かめている間は進捗のモーダルを出す (何待ちか分からなかった。ユーザー報告 2026-10-16)。
func TestSetupProgressModal(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	h.m.TakeNotices()
	release := holdObserve(t, h)
	h.keys("r", "x")
	if n := h.m.TakeNotices(); len(n) != 0 {
		t.Errorf("読み込み中の r / x で toast を出した: %+v", n)
	}
	if !h.m.progressShown() || !strings.Contains(strings.Join(h.m.View(), "\n"), "を確認中") {
		t.Fatal("読み込み中の x で進捗のモーダルを出さない")
	}
	release()
	awaitSetup(h.m)
	h.m.Advance(t0)
	if h.m.ov != ovConfirm || h.m.ask == nil {
		t.Fatalf("読み終えたのに予約した x の板を出さない (ov=%v)", h.m.ov)
	}
	h.keys("y")
	if _, ok := h.m.TakeExec(); !ok {
		t.Fatal("前提: 手順のプロセスが無い")
	}
	h.m.ExecDone(nil)
	if v := strings.Join(h.m.View(), "\n"); !h.m.progressShown() || !strings.Contains(v, "結果を確かめている") {
		t.Errorf("手順から戻った後に進捗のモーダルを出さない")
	}
	awaitNetLoad(h.m)
	awaitSetup(h.m)
	h.m.Advance(t0)
	if h.m.progressShown() {
		t.Error("読み終えても進捗のモーダルを閉じない")
	}
}

// 初期設定の手順は、端末を渡す前に板を出してから起こし、失敗したら終了コードを出して Enter を待つ (端末を渡すと TUI の枠が消え、
// 失敗の出力も戻った瞬間に消えた。ユーザー報告)。終了コードはそのまま返す。s / o (板の無いプロセス) は argv のまま。
func TestRunArgvBanner(t *testing.T) {
	if got := (ExecRequest{Argv: []string{"ssh", "x"}}).RunArgv(); !slices.Equal(got, []string{"ssh", "x"}) {
		t.Errorf("板の無いプロセスを包んだ: %q", got)
	}
	r := ExecRequest{Argv: []string{"/bin/sh", "-c", "echo 本体; exit 3"}, Banner: "【板】100%d"} // % は printf の書式に入れない
	argv := r.RunArgv()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader("\n")
	out, err := cmd.Output()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("終了コードを返さない: %v", err)
	}
	if s := string(out); !strings.Contains(s, "【板】100%d") || strings.Index(s, "【板】") > strings.Index(s, "本体") || !strings.Contains(s, "終了コード 3") {
		t.Errorf("板 → 本体 → 失敗の案内の順に出ない: %q", s)
	}
	ok := ExecRequest{Argv: []string{"/bin/sh", "-c", "exit 0"}, Banner: "【板】"}.RunArgv()
	if out, err := exec.Command(ok[0], ok[1:]...).Output(); err != nil || strings.Contains(string(out), "Enter") {
		t.Errorf("成功でも Enter を待った: %q %v", out, err)
	}
}

// 読み込み中に予約した x は、読み終わりと同時に押した q / esc で板を一覧に残さない (キーの入口で予約を走らせていたので、板を出した
// 直後に同じ q が画面を閉じ、一覧に残った板の y が相手の名前の無い手順を起こした)。この Mac の網だけを読んでいる間の
// x も予約になる (古い網で計画を組み、この Mac の固定の板がもう一度出た)。
func TestSetupQueuedXRaces(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	freeObs := holdObserve(t, h)
	h.keys("r", "x")
	freeObs()
	awaitSetup(h.m) // 読み終えたが、まだ tick が来ていない
	h.keys("q")
	if h.m.ov != ovNone || h.m.ask != nil || h.m.su != nil {
		t.Fatalf("画面を閉じた後に初期設定の板が残った (ov=%v ask=%v)", h.m.ov, h.m.ask)
	}

	h2, _ := setupHarness(t, obs)
	h2.keys("x", "y") // この Mac の固定
	h2.m.TakeExec()
	release := make(chan struct{}) // この Mac の網の読み直しを手で離すまで止める (すぐ戻ると、キーの時点で取り込まれて順序が運で決まった)
	defer close(release)
	h2.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { <-release; return selfNet(), nil }
	h2.m.ExecDone(nil) // この Mac の網と相手を読み直す
	awaitSetup(h2.m)   // 相手は読み終えたが、この Mac の網はまだ
	h2.keys("esc", "x")
	if h2.m.ov == ovConfirm {
		t.Error("この Mac の網を読み直している間の x で、古い網の板を出した")
	}
	if !h2.m.su.queuedX {
		t.Error("この Mac の網を読み直している間の x を予約しない")
	}
}

// 初期設定の手順の板は、初期設定の画面の外では y で起こさない (閉じる経路が板を残しても、相手の名前の無い手順を起こさない)。
func TestSetupBoardNeedsSetupScreen(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	h.keys("x")
	if h.m.ov != ovConfirm || h.m.ask == nil || h.m.ask.exec == nil {
		t.Fatal("前提: 手順の板が出ない")
	}
	h.m.su = nil // 閉じる経路が板を残した形
	if h.keys("y") == Exec {
		t.Error("初期設定の画面の外で手順を起こした")
	}
}

// 帯がこの Mac のメインの網 (Wi-Fi) と重なるときは、この Mac の固定を止める (同じ番号の網が有線と Wi-Fi の両方に乗り、メインの網の
// 宛先が有線へ流れる)。戻し方の案内は、今が手動の固定なら今の IP とマスクへ戻すコマンドにする (-setdhcp だと元に戻らない)。
func TestSetupBandOverlapAndRestore(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	h.m.pf.Band = "192.168.50.0/24" // selfNet の Wi-Fi は 192.168.50.250
	steps := h.m.setupSteps()
	if i := slices.IndexFunc(steps, func(s setup.Step) bool { return s.ID == setup.StepSelfStatic }); i < 0 || !strings.Contains(steps[i].Blocked, "メインの網") {
		t.Errorf("Wi-Fi と重なる帯でこの Mac の固定を止めない: %+v", steps)
	}
	h.m.pf.Band = "192.168.100.0/24"
	for i := range h.m.self.Services {
		if h.m.self.Services[i].Device == "en13" {
			h.m.self.Services[i].Method, h.m.self.Services[i].IP, h.m.self.Services[i].Mask = "manual", "10.0.0.5", "255.0.0.0"
		}
	}
	h.keys("x")
	if h.m.ask == nil || !strings.Contains(strings.Join(h.m.ask.body, "\n"), "-setmanual 'Thunderbolt Ethernet' 10.0.0.5 255.0.0.0") {
		t.Errorf("手動の固定を今の IP とマスクへ戻す案内を出さない: %+v", h.m.ask)
	}
}

// 片方だけ固定して網が分かれた相手 (この Mac は帯の番号・相手は 169.254、またはその逆) には、有線ではなく Wi-Fi の IP で繋ぐ
// (送り元が固定の番号になり、169.254 のままの相手は返事を Wi-Fi 側へ出して届かない。air5 で実測)。同じ網なら有線のまま。
func TestConnectIPSkipsLanOnOtherNetwork(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini}) // mini は Wi-Fi 192.168.50.7・有線 169.254.217.100
	ssh := func() string {
		h.m.Advance(t0) // 走査の結果 (相手の番号) を取り込む
		h.m.tab = 1     // 未管理のタブ
		h.cursorTo(t, "m2macmini.local")
		if h.keys("s") != Exec {
			t.Fatal("前提: s で ssh を起こさない")
		}
		r, _ := h.m.TakeExec()
		return r.Argv[len(r.Argv)-1]
	}
	if got := ssh(); got != "koji@169.254.217.100" {
		t.Errorf("両方 169.254 なのに有線を使わない: %s", got)
	}
	setSelfStatic(h.m) // この Mac は 192.168.100.10 (帯) に固定済み
	if got := ssh(); got != "koji@192.168.50.7" {
		t.Errorf("この Mac だけ固定したのに 169.254 の相手へ有線で繋いだ: %s", got)
	}
	p := mini
	p.Addrs = map[int]string{14: "192.168.50.7", 27: "192.168.100.12"}
	h.scan.peers = []discover.Peer{p}
	if got := ssh(); got != "koji@192.168.100.12" {
		t.Errorf("両方帯の中なのに有線を使わない: %s", got)
	}
}

// 相手の固定 IP の板は、相手で走らせる戻しの仕掛け (数十行の sh) の全文ではなく要約を出す (全文が画面を埋めた。ユーザー報告)。
func TestPeerStaticBoardIsSummary(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	setSelfStatic(h.m)
	h.keys("x")
	if h.m.ask == nil || h.m.ask.title != "相手の有線を固定 IP にする" {
		t.Fatalf("前提: 相手の固定 IP の板が出ない: %+v", h.m.ask)
	}
	body := strings.Join(h.m.ask.body, "\n")
	if strings.Contains(body, "while [") || len(body) > 600 || !strings.Contains(body, "→ sudo networksetup -setmanual") {
		t.Errorf("板に戻しの仕掛けの全文を出した / 要約が無い (%d 字): %s", len(body), body)
	}
}

// 手順の確認の板で c を押すと、実行するコマンドの全文 (板は要約) をコピーし、板は閉じない (うまくいかないときに相談できるように)。
func TestConfirmBoardCopiesCommand(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	setSelfStatic(h.m)
	h.keys("x")
	if h.m.ask == nil || h.m.ask.exec == nil {
		t.Fatal("前提: 手順の板が出ない")
	}
	h.keys("c")
	if h.m.ov != ovConfirm {
		t.Error("c で板を閉じた")
	}
	if len(h.copied) != 1 || !strings.HasPrefix(h.copied[0], "ssh ") || !strings.Contains(h.copied[0], "networksetup -setmanual") || !strings.Contains(h.copied[0], "while") {
		t.Errorf("実行するコマンドの全文をコピーしない: %q", h.copied)
	}
}

// 操作のログ: キーとそのときの画面、相手を読んだ結果、起こしたコマンドと戻りを 1 行ずつ渡す。入力欄に打った文字は伏せる
// (ユーザー要望「俺の操作ログを目で確認できるように。何が詰まっているのか見えて欲しい」)。
func TestOperationLog(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	var lines []string
	h := newHarness(t, nil, []discover.Peer{mini})
	h.m.o.Log = func(l string) { lines = append(lines, l) }
	h.m.o.Observe = func(context.Context, setup.Dest) setup.PeerNet { return *obs }
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	h.keys("enter")
	awaitSetup(h.m)
	h.keys("x", "y")
	h.m.TakeExec()
	h.m.ExecDone(errors.New("exit status 1"))
	awaitNetLoad(h.m)
	awaitSetup(h.m)
	h.m.Advance(t0)                                     // 読み終えて進捗を閉じる
	h.keys("q", "/", "h", "i", "m", "i", "t", "s", "u") // 画面を閉じ、絞り込みに打つ
	all := strings.Join(lines, "\n")
	for _, want := range []string{"key enter  [一覧 行=m2macmini.local]", "observe start m2macmini.local (koji@192.168.50.7)", "observe done m2macmini.local key_ok=true",
		"key y  [初期設定 m2macmini.local", "exec sudo networksetup -setmanual", "exec done err=exit status 1"} {
		if !strings.Contains(all, want) {
			t.Errorf("ログに %q が無い:\n%s", want, all)
		}
	}
	if strings.Contains(all, "key h  [一覧") || strings.Contains(all, "key s  [一覧") {
		t.Errorf("絞り込みの入力欄に打った文字を記録した:\n%s", all)
	}
}

// 台帳に既に載っている相手は、台帳の手順で user と内部 IP だけを書く (開いた時の一覧の行で丸ごと書き戻すと、別のプロセス
// (単体と glogx の L) がその後に変えた別名を古い値に戻した)。
func TestSetupLedgerKeepsAliasChangedElsewhere(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	h.store.hosts = []ledger.Host{{Host: "m2macmini.local", User: "koji", Alias: "旧い別名"}}
	h.m.reloadLedger()
	setSelfStatic(h.m)
	runStep(t, h, nil) // 相手の固定 IP
	ip := h.m.conf().static.IP
	setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: ip, Mask: "255.255.255.0"})
	runStep(t, h, nil) // 確認
	awaitSetup(h.m)
	h.store.hosts[0].Alias, h.store.hosts[0].User = "新しい別名", "admin" // 別のプロセスが変えた (別名と u のユーザー名)
	h.keys("x")
	if h.m.ov != ovConfirm || h.m.ask == nil || h.m.ask.yes == nil {
		t.Fatalf("前提: 台帳の手順の確認の板が出ない: %s", strings.Join(h.m.View(), "\n"))
	}
	h.keys("y")
	if len(h.store.hosts) != 1 || h.store.hosts[0].LAN != ip || h.store.hosts[0].Alias != "新しい別名" || h.store.hosts[0].User != "admin" {
		t.Errorf("台帳 = %+v (内部 IP %s と別のプロセスの別名を残すはず)", h.store.hosts, ip)
	}
}

// 読み込み中に押した x の予約で自動で出た板は、Enter では実行しない (進捗のモーダルの間に押し続けた Enter が、裏で開いた板を
// そのまま実行した)。y では実行する。
func TestSetupQueuedBoardIgnoresEnter(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	release := holdObserve(t, h)
	h.keys("r", "x", "enter")
	release()
	awaitSetup(h.m)
	h.m.Advance(t0)
	if h.m.ov != ovConfirm {
		t.Fatal("前提: 予約した x の板が出ない")
	}
	if a := h.keys("enter"); a == Exec || h.m.ov != ovConfirm {
		t.Fatalf("自動で出た板を Enter で実行した / 閉じた: action=%v ov=%v", a, h.m.ov)
	}
	if a := h.keys("y"); a != Exec {
		t.Errorf("自動で出た板を y で実行しない: %v", a)
	}
}

// 自分で板を開いたら予約は済み: 取り消した板を次の tick で開き直さない。
func TestSetupManualBoardClearsQueuedX(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	release := holdObserve(t, h)
	h.keys("r", "x")
	release()
	awaitSetup(h.m) // 読み終えたが tick はまだ
	h.keys("x")     // 自分で板を開く
	h.keys("n")     // 取り消す
	h.m.Advance(t0)
	if h.m.ov == ovConfirm {
		t.Error("取り消した板を次の tick で開き直した")
	}
}

// 失敗で戻った固定 (相手の上では変わっていうる) の番号を、別の相手に出さない。
func TestSetupDoesNotOfferIPTriedOnOtherPeer(t *testing.T) {
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
	runStep(t, h, errors.New("exit status 1"))
	var tried string
	for ip := range h.m.tried["m2macmini.local"] {
		tried = ip
	}
	if tried == "" {
		t.Fatal("前提: 試した番号が無い")
	}
	h.keys("q")
	h.cursorTo(t, "air.local")
	h.keys("enter")
	awaitSetup(h.m)
	h.keys("x")
	if h.m.ask == nil {
		t.Fatal("前提: 板が出ない")
	}
	if strings.Contains(strings.Join(h.m.ask.body, "\n"), tried+" ") {
		t.Errorf("別の相手で試した %s を出した: %q", tried, h.m.ask.body[0])
	}
}

// ssh の stderr が複数行でも、初期設定の画面は 1 行に収め、狭い端末で「高さが足りない」に落ちない。
func TestSetupKeyErrFitsOneLine(t *testing.T) {
	msg := "exit status 255: ssh: connect to host 169.254.217.100 port 22: Operation timed out" +
		strings.Repeat("\nWarning: Permanently added '169.254.217.100' (ED25519) to the list of known hosts.", 3)
	obs := &setup.PeerNet{KeyErr: fmt.Errorf("%w: %w", setup.ErrUnknown, errors.New(msg))}
	h, _ := setupHarness(t, obs)
	h.m.Resize(80, 24)
	v := strings.Join(h.m.View(), "\n")
	if h.m.small || strings.Contains(v, "Warning: Permanently") {
		t.Errorf("stderr の 2 行目以降を出した / 足りない画面になった: small=%v", h.m.small)
	}
}

// 見出しは何を待っているかを出す: 走査が終わった後の確認待ちを「Bonjour で探しています」と出さない。
func TestSetupHeaderSaysWhatItWaitsFor(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, _ := setupHarness(t, obs)
	setSelfStatic(h.m)
	runStep(t, h, nil)
	awaitNetLoad(h.m)
	h.scan.busy = false
	h.m.Advance(t0.Add(30 * time.Second))
	if !h.m.Busy() {
		t.Fatal("前提: 確認待ちで Busy でない")
	}
	if hd := h.m.header(200); strings.Contains(hd, "Bonjour で探しています") {
		t.Errorf("走査が終わっているのに Bonjour で探していると出した: %q", hd)
	}
}

// 開いた後に別のプロセスが台帳から外した Mac を、開いた時の一覧の値で作り直さない。
func TestSetupLedgerDoesNotResurrectRemovedMac(t *testing.T) {
	obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
	h, ob := setupHarness(t, obs)
	h.store.hosts = []ledger.Host{{Host: "m2macmini.local", User: "koji", Alias: "旧い別名"}}
	h.m.reloadLedger()
	setSelfStatic(h.m)
	runStep(t, h, nil)
	ip := h.m.conf().static.IP
	setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: ip, Mask: "255.255.255.0"})
	runStep(t, h, nil)
	awaitSetup(h.m)
	h.store.hosts = nil // 別のプロセスが外した
	h.keys("x")
	if h.m.ask == nil {
		t.Fatalf("前提: 板が出ない %s", strings.Join(h.m.View(), "\n"))
	}
	h.keys("y")
	if len(h.store.hosts) != 0 {
		t.Errorf("外された Mac を作り直した: %+v", h.store.hosts)
	}
}

// 台帳の手順は、既にある行の user を触らない: 別のプロセスが u で空 (既定) にした値を埋め戻さない。使えない形の user だけは
// 板に出した user で直す。
func TestSetupLedgerUserHandling(t *testing.T) {
	for _, c := range []struct{ name, stored, changeTo, want string }{
		{"空にした user", "admin", "", ""},
		{"使えない user", "-oProxyCommand=x", "-oProxyCommand=x", "koji"},
	} {
		t.Run(c.name, func(t *testing.T) {
			obs := &setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"}
			h, ob := setupHarness(t, obs)
			h.store.hosts = []ledger.Host{{Host: "m2macmini.local", User: c.stored}}
			h.m.reloadLedger()
			h.keys("esc", "h")
			h.cursorTo(t, "m2macmini.local")
			h.keys("enter")
			awaitSetup(h.m)
			setSelfStatic(h.m)
			runStep(t, h, nil)
			ip := h.m.conf().static.IP
			setObs(t, h, ob, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "manual", IP: ip, Mask: "255.255.255.0"})
			runStep(t, h, nil)
			awaitSetup(h.m)
			h.store.hosts[0].User = c.changeTo // 別のプロセスが変えた
			h.keys("x")
			if h.m.ask == nil {
				t.Fatalf("前提: 板が出ない %s", strings.Join(h.m.View(), "\n"))
			}
			h.keys("y")
			if got := h.store.hosts[0]; got.User != c.want || got.LAN != ip {
				t.Errorf("台帳 = %+v (user %q / 内部 IP %s のはず)", got, c.want, ip)
			}
		})
	}
}

// この Mac の名前の手順が止まっていても、相手の名前の手順へ進める (止まっている手順を飛ばすのは名前の手順どうしだけ)。
func TestNextStepSkipsBlockedSelfName(t *testing.T) {
	steps := []setup.Step{
		{ID: setup.StepLedger, Done: true},
		{ID: setup.StepSelfName, Blocked: "読めていない"},
		{ID: setup.StepPeerName},
	}
	if st, _ := nextStep(steps); st.ID != setup.StepPeerName {
		t.Errorf("次の手順 = %s (相手の名前のはず)", st.ID)
	}
	steps[2].Blocked = "読めていない"
	if st, _ := nextStep(steps); st.ID != setup.StepSelfName {
		t.Errorf("両方止まっているときの次 = %s (先の理由を出す)", st.ID)
	}
	core := []setup.Step{{ID: setup.StepKey, Blocked: "リモートログインがオフ"}, {ID: setup.StepSelfStatic}}
	if st, _ := nextStep(core); st.ID != setup.StepKey {
		t.Errorf("鍵が止まっているのに %s へ進んだ (指紋を確かめる前にこの Mac を変える)", st.ID)
	}
}

// この Mac を固定した後の網の読み直しが失敗したら、残した古い網 (固定の前) で計画を組まない: 組むと「この Mac を固定する」の板 (sudo) が
// もう一度出た。r でこの Mac の網も読み直し、読めたら次の手順へ進む。
func TestSetupRefusesStaleSelfAfterFailedReread(t *testing.T) {
	obs := &setup.PeerNet{}
	h, dests := setupHarness(t, obs)
	runStep(t, h, nil) // 鍵
	setObs(t, h, dests, setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Ethernet", Method: "DHCP", IP: "169.254.217.100"})
	h.keys("r")
	awaitSetup(h.m)
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return netinfo.Snapshot{}, errors.New("networksetup が失敗した") }
	if argv := runStep(t, h, nil); len(argv) == 0 || argv[0] != "sudo" { // この Mac の固定 (戻ると網を読み直し、失敗する)
		t.Fatalf("この Mac の固定 = %q", argv)
	}
	awaitNetLoad(h.m)
	h.m.TakeNotices()
	if h.keys("x"); h.m.ov == ovConfirm {
		t.Fatalf("読み直せなかった古い網で次の板を出した: %q / %+v", h.m.ask.title, h.m.ask.body)
	}
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "読み直せなかった") {
		t.Errorf("読み直せなかったことで断らない: %+v", n)
	}
	setSelfStatic(h.m)
	h.keys("r")
	awaitNetLoad(h.m)
	awaitSetup(h.m)
	if h.m.selfErr != nil {
		t.Fatalf("r でこの Mac の網を読み直さない: %v", h.m.selfErr)
	}
	if h.keys("x"); h.m.ov == ovConfirm && strings.Contains(h.m.ask.title, "この Mac") {
		t.Errorf("読み直した後もこの Mac の固定の板が出た: %q", h.m.ask.title)
	}
}
