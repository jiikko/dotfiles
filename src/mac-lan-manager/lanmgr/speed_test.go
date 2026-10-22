package lanmgr

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"mac-lan-manager/discover"
	"mac-lan-manager/internal/testwait"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/setup"
	"mac-lan-manager/speedtest"
)

// fakeSpeed は SpeedTest の偽物。呼ばれた宛先と ctx を控え、results に送った結果を返す (送るまで測定中のまま)。
type fakeSpeed struct {
	calls   atomic.Int32
	dest    setup.Dest
	ctx     chan context.Context
	results chan speedtest.Result
}

func newFakeSpeed() *fakeSpeed {
	return &fakeSpeed{ctx: make(chan context.Context, 4), results: make(chan speedtest.Result, 4)}
}

func (f *fakeSpeed) run(ctx context.Context, d setup.Dest, _ func() time.Time) speedtest.Result {
	f.dest = d
	f.ctx <- ctx
	select {
	case r := <-f.results:
		return r
	case <-ctx.Done():
		return speedtest.Result{Err: ctx.Err()}
	}
}

// started は測定が起こされるのを待つ (起こされなければ 10 秒で赤にする。待ち続けると suite ごと止まった)。
func (f *fakeSpeed) started(t *testing.T) context.Context {
	t.Helper()
	select {
	case ctx := <-f.ctx:
		return ctx
	case <-time.After(10 * time.Second):
		t.Fatal("測定が起こされない")
		return nil
	}
}

// speedHarness は管理対象の mini (有線で見えている) と未管理の air を並べた画面。
func speedHarness(t *testing.T) (*harness, *fakeSpeed) {
	t.Helper()
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100", Alias: "mini"}},
		[]discover.Peer{mini, {Host: "air.local", SSH: true, Addrs: map[int]string{14: "192.168.50.44"}}})
	f := newFakeSpeed()
	h.m.o.SpeedTest = func(ctx context.Context, d setup.Dest, now func() time.Time) speedtest.Result {
		f.calls.Add(1)
		return f.run(ctx, d, now)
	}
	h.m.Resize(160, 50)
	return h, f
}

func (h *harness) screen() string { return termwidth.StripSGR(strings.Join(h.m.View(), "\n")) }

// 未管理の相手は測らない (確認の板も出さない)。
func TestSpeedOnlyManaged(t *testing.T) {
	h, f := speedHarness(t)
	h.keys("l") // 未管理のタブ
	h.cursorTo(t, "air.local")
	h.keys("t")
	if h.m.ov == ovSpeed || f.calls.Load() != 0 {
		t.Fatalf("未管理の相手で確認の板を出した / 測った: ov=%v calls=%d", h.m.ov, f.calls.Load())
	}
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "管理対象") {
		t.Errorf("知らせ = %+v", n)
	}
	if strings.Contains(h.screen(), "通信速度") {
		t.Error("未管理の相手の詳細カードに通信速度の行を出した")
	}
}

// 帯域を使うので確認の板を通し、Enter と n では流さない。
func TestSpeedConfirmRejectsEnterAndNo(t *testing.T) {
	h, f := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	if !strings.Contains(h.screen(), "未測定") {
		t.Errorf("測る前の詳細カードに「未測定」が無い:\n%s", h.screen())
	}
	h.keys("j", "k", "g", "G", "g") // 一覧を動かしただけでは測らない
	if f.calls.Load() != 0 || h.m.ov == ovSpeed {
		t.Fatalf("一覧を動かしただけで測った / 板を出した: calls=%d ov=%v", f.calls.Load(), h.m.ov)
	}
	h.cursorTo(t, "m2macmini.local")
	h.keys("t")
	if h.m.ov != ovSpeed || h.m.speedAsk == nil {
		t.Fatalf("確認の板が出ていない: ov=%v ask=%+v", h.m.ov, h.m.speedAsk)
	}
	h.keys("enter")
	if h.m.ov != ovSpeed || f.calls.Load() != 0 {
		t.Fatalf("Enter で板を閉じた / 測った: ov=%v calls=%d", h.m.ov, f.calls.Load())
	}
	h.keys("n")
	if h.m.ov == ovSpeed || f.calls.Load() != 0 || h.m.speedRun != nil {
		t.Errorf("n で測った: calls=%d run=%v", f.calls.Load(), h.m.speedRun)
	}
	h.keys("t", "esc")
	if h.m.ov == ovSpeed || f.calls.Load() != 0 {
		t.Errorf("Esc で測った / 板が残った: calls=%d ov=%v", f.calls.Load(), h.m.ov)
	}
	h.keys("t", "Y") // Y (一覧では ssh のコピー) は取り消し
	if h.m.ov == ovSpeed || f.calls.Load() != 0 || h.m.speedRun != nil {
		t.Errorf("Y で測った / 板が残った: calls=%d run=%v ov=%v", f.calls.Load(), h.m.speedRun, h.m.ov)
	}
}

// y で測り、測っている間は Busy・詳細カードは「測定中」、終わったら上り・下り・経路を出す。100 Mbps 付近なら頭打ちの一言を足す。
func TestSpeedRunsAndShowsResult(t *testing.T) {
	h, f := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t", "y")
	ctx := f.started(t)
	if f.calls.Load() != 1 || f.dest.IP != "169.254.217.100" || f.dest.HostKeyAlias != "m2macmini.local" || f.dest.User != "koji" {
		t.Fatalf("測った宛先 = %+v (calls=%d), want 内部 IP 169.254.217.100", f.dest, f.calls.Load())
	}
	if !h.m.Busy() || !strings.Contains(h.screen(), "測定中") {
		t.Errorf("測っている間: Busy=%v\n%s", h.m.Busy(), h.screen())
	}
	h.keys("esc", "t") // 測定中は 2 本目を流さず、閉じた進捗のモーダルを開き直す
	if f.calls.Load() != 1 || h.m.ov == ovSpeed || !h.m.speedShown() {
		t.Errorf("測定中の t で 2 本目の板を出した / 測った / モーダルを開き直さない: calls=%d ov=%v shown=%v", f.calls.Load(), h.m.ov, h.m.speedShown())
	}
	f.results <- speedtest.Result{UpBps: 94e6, DownBps: 93e6}
	awaitSpeed(t, h.m)
	if h.m.Busy() || ctx.Err() == nil {
		t.Errorf("終わった後も Busy=%v / ctx を解放していない=%v", h.m.Busy(), ctx.Err() == nil)
	}
	s := h.screen()
	for _, want := range []string{"↑ 94 Mbps  ↓ 93 Mbps (有線)", "100 Mbps 付近で頭打ち"} {
		if !strings.Contains(s, want) {
			t.Errorf("詳細カードに %q が無い:\n%s", want, s)
		}
	}
}

// 鍵で入れない相手は理由を出す (0 Mbps と出さない)。
func TestSpeedShowsError(t *testing.T) {
	h, f := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t", "y")
	f.started(t)
	f.results <- speedtest.Result{Err: speedtest.ErrNoKey}
	awaitSpeed(t, h.m)
	if s := h.screen(); !strings.Contains(s, "鍵で入れないので測れない") || strings.Contains(s, "Mbps") {
		t.Errorf("詳細カード:\n%s", s)
	}
	// ssh の stderr (相手の Banner・複数行・制御文字) を含む失敗は、最後の行 (原因) を無害化して末尾が残るように出す
	h.keys("t", "y")
	f.started(t)
	f.results <- speedtest.Result{Err: errors.New("繋げなかった: exit status 255: Welcome to studio\n\x1b]0;evil\x07ssh: connect to host 192.168.100.12 port 22: Connection refused")}
	awaitSpeed(t, h.m)
	tail := false
	for _, l := range h.m.View() {
		if strings.Contains(l, "\x1b]0;") { // OSC 0 (窓の題) はこの画面で使わない。相手の文から来たもの
			t.Errorf("制御文字をそのまま描いた: %q", l)
		}
		p := termwidth.StripSGR(l)
		if strings.Contains(p, "Welcome") {
			t.Errorf("Banner (先頭の行) を出した: %q", p)
		}
		tail = tail || strings.Contains(p, "port 22: Connection refused")
	}
	if !tail {
		t.Errorf("原因 (最後の行の末尾) が出ていない:\n%s", h.screen())
	}
}

// 画面を閉じたら測定を取り消す (閉じた板の裏で帯域を使い続けない)。
func TestSpeedStopsOnClose(t *testing.T) {
	h, f := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t", "y")
	ctx := f.started(t)
	h.m.Close()
	if !errors.Is(ctx.Err(), context.Canceled) || h.m.speedRun != nil {
		t.Errorf("閉じた後も測定が続く: ctx=%v run=%v", ctx.Err(), h.m.speedRun)
	}
}

// awaitSpeed は裏の測定の結果が Advance で取り込まれるまで待つ (上限つき)。
func awaitSpeed(t *testing.T, m *Model) {
	t.Helper()
	testwait.Until(t, "測定の結果の取り込み", 10*time.Second, func() { m.Advance(t0) }, func() bool { return m.speedRun == nil })
}

// 最下行の案内に t を出すのは、t が効く行 (管理対象で測れる相手) だけ。未管理の行と、測る口が無い画面 (SpeedTest が nil) には出さない。
func TestSpeedFooterHint(t *testing.T) {
	h, _ := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	if foot := termwidth.StripSGR(h.m.footer()); !strings.Contains(foot, "t 速度") {
		t.Errorf("測れる管理対象の行で t を案内しない: %q", foot)
	}
	h.keys("l")
	h.cursorTo(t, "air.local")
	if foot := termwidth.StripSGR(h.m.footer()); strings.Contains(foot, "t 速度") {
		t.Errorf("未管理の行で t を案内した: %q", foot)
	}
	h.keys("h")
	h.cursorTo(t, "m2macmini.local")
	h.m.o.SpeedTest = nil
	if foot := termwidth.StripSGR(h.m.footer()); strings.Contains(foot, "t 速度") {
		t.Errorf("測る口が無いのに t を案内した: %q", foot)
	}
}

// 測定中は最下行の t を「測定の進捗」にする (2 本目は流さず、閉じたモーダルを開き直す。案内と効くキーを揃える)。この Mac のネットワークを読めていない画面では
// t を断る (CLI と同じ規則。宛先と経路を決められない)。
func TestSpeedFooterAndRefusalFollowState(t *testing.T) {
	h, _ := speedHarness(t) // 偽の測定は結果を送るまで測定中のまま
	h.cursorTo(t, "m2macmini.local")
	h.keys("t", "y")
	if h.m.speedRun == nil {
		t.Fatal("前提: 測定が始まらない")
	}
	h.keys("esc")
	if foot := termwidth.StripSGR(h.m.footer()); strings.Contains(foot, "t 速度") || !strings.Contains(foot, "t 測定の進捗") {
		t.Errorf("測定中の t の案内が「測定の進捗」でない: %q", foot)
	}
	h.m.stopSpeed()
	h.m.selfOK = false
	h.m.TakeNotices()
	h.keys("t")
	if n := h.m.TakeNotices(); h.m.ov == ovSpeed || len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "ネットワークを読め") {
		t.Errorf("網を読めていないのに測ろうとした: ov=%v notices=%+v", h.m.ov, n)
	}
}

// 測っている間は進捗のモーダル (回る印・相手・経路・経過秒) を出し、裏の一覧にキーを届かせない。esc で閉じても測定は続き、
// 結果は toast で出る。終わればモーダルは消える。最下行の案内は板・モーダルのキーにする (一覧のキーを残さない)。
func TestSpeedProgressModal(t *testing.T) {
	h, f := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t")
	if foot := termwidth.StripSGR(h.m.footer()); !strings.Contains(foot, "y 実行") || strings.Contains(foot, "t 速度") {
		t.Errorf("経路を選ぶ板の案内が板のキーでない: %q", foot)
	}
	h.m.selfOK = false // 板を開いた後で網を読めなくなっても、板で効く y は案内から落とさない
	if foot := termwidth.StripSGR(h.m.footer()); !strings.Contains(foot, "y 実行") {
		t.Errorf("網を読めないときに板の y を案内から落とした: %q", foot)
	}
	h.m.selfOK = true
	h.keys("y")
	ctx := f.started(t)
	s := h.screen()
	if !h.m.speedShown() || !strings.Contains(s, "通信速度を測っています") || !strings.Contains(s, "mini  169.254.217.100 (有線)") ||
		!strings.Contains(s, "経過 0 秒  (ssh で繋いでから上り・下りを 5 秒ずつ)") {
		t.Fatalf("測っている間に進捗のモーダルが無い:\n%s", s)
	}
	if !regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]`).MatchString(s) {
		t.Errorf("進捗のモーダルに回る印が無い:\n%s", s)
	}
	if foot := termwidth.StripSGR(h.m.footer()); !strings.Contains(foot, "esc 閉じる") || strings.Contains(foot, "j/k") {
		t.Errorf("測定中の案内がモーダルのキーでない: %q", foot)
	}
	for _, k := range []string{"L", "C", "i", "U"} { // glogx の開閉・横断キーもこの画面が取る (渡すと glogx が板を閉じ、Close が測定を取り消す)
		if !h.m.OwnsKeys() || !h.m.Binds(k) {
			t.Errorf("測定の進捗の間に %s を glogx へ渡す: OwnsKeys=%v Binds=%v", k, h.m.OwnsKeys(), h.m.Binds(k))
		}
	}
	h.m.TakeNotices()
	if a := h.keys("s"); a != None || h.m.ov != ovNone || !h.m.speedShown() { // 裏の一覧の s (ssh で繋ぐ)・space (台帳) を起こさない
		t.Errorf("モーダルの裏の一覧にキーが届いた: action=%v ov=%v", a, h.m.ov)
	}
	if h.keys("space"); h.m.ov != ovNone {
		t.Errorf("モーダルの裏で台帳の確認を出した: ov=%v", h.m.ov)
	}
	if n := h.m.TakeNotices(); len(n) == 0 || n[len(n)-1].OK || !strings.Contains(n[len(n)-1].Text, "esc で閉じる") {
		t.Errorf("捨てたキーを断る知らせが無い: %+v", n)
	}
	h.keys("esc")
	if h.m.speedShown() || h.m.speedRun == nil || ctx.Err() != nil || strings.Contains(h.screen(), "通信速度を測っています") {
		t.Fatalf("esc でモーダルを閉じて測定を続けていない: shown=%v run=%v ctxErr=%v", h.m.speedShown(), h.m.speedRun != nil, ctx.Err())
	}
	if h.m.OwnsKeys() || h.m.Binds("L") { // 閉じた後は一覧と同じ (L で glogx の板を閉じられる)
		t.Errorf("モーダルを閉じた後も全部のキーを取る: OwnsKeys=%v", h.m.OwnsKeys())
	}
	f.results <- speedtest.Result{UpBps: 94e6, DownBps: 93e6}
	awaitSpeed(t, h.m)
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "↑ 94 Mbps  ↓ 93 Mbps") {
		t.Errorf("閉じた後の結果を toast で出していない: %+v", n)
	}
	h.keys("t", "y") // 開いたまま終われば、モーダルは結果と一緒に消える
	f.started(t)
	f.results <- speedtest.Result{UpBps: 90e6, DownBps: 90e6}
	awaitSpeed(t, h.m)
	if h.m.speedShown() || strings.Contains(h.screen(), "通信速度を測っています") {
		t.Errorf("測り終えた後もモーダルが残った:\n%s", h.screen())
	}
}

// 呼び出し側が案内を描く (HostHints) と、View は最下行の案内とその上の空行を描かず、その 2 行を本文に回す。Hints は板・モーダルの
// 間はその板のキーだけを返す (一覧のキーを残さない)。
func TestHostHints(t *testing.T) {
	h, f := speedHarness(t)
	h.m.Resize(120, 40)
	h.cursorTo(t, "m2macmini.local")
	before := h.m.bodyHeight()
	if !strings.Contains(h.screen(), "j/k 移動") {
		t.Fatal("前提: 単体では最下行に案内を描く")
	}
	h.m.o.HostHints = true
	if s := h.screen(); strings.Contains(s, "j/k 移動") || h.m.bodyHeight() != before+2 {
		t.Errorf("HostHints で案内を描いた / 本文に 2 行回していない (%d → %d):\n%s", before, h.m.bodyHeight(), s)
	}
	keys := func() string {
		var ks []string
		for _, x := range h.m.Hints() {
			ks = append(ks, x.Key+" "+x.Label)
		}
		return strings.Join(ks, " | ")
	}
	if k := keys(); !strings.Contains(k, "j/k 移動") || !strings.Contains(k, "q 閉じる") {
		t.Errorf("一覧の Hints = %q", k)
	}
	h.keys("t")
	if k := keys(); k != "j/k 経路 | y 実行 | n/esc キャンセル" {
		t.Errorf("経路を選ぶ板の Hints = %q", k)
	}
	h.keys("y")
	f.started(t)
	if k := keys(); k != "esc 閉じる (測定は裏で続く)" {
		t.Errorf("測定の進捗の Hints = %q", k)
	}
	h.keys("esc")
	h.m.stopSpeed()
	h.keys("?")
	if k := keys(); k != "どのキーでも 閉じる" {
		t.Errorf("キーの一覧の Hints = %q", k)
	}
	h.keys("x", "e")
	if k := keys(); !strings.Contains(k, "e/esc 閉じる") || strings.Contains(k, "移動") {
		t.Errorf("メッセージ一覧の Hints = %q", k)
	}
}

// 経路は板で選べる。既定は有線 (内部 IP)、j で Wi-Fi のメイン IP。進捗のモーダルに経路を出し、結果は経路ごとに有線から並べる。
func TestSpeedChoosesPath(t *testing.T) {
	h, f := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t")
	if a := h.m.speedAsk; a == nil || len(a.paths) != 2 || a.paths[a.cur].ip != "169.254.217.100" {
		t.Fatalf("既定の経路が有線でない: %+v", a)
	}
	box := termwidth.StripSGR(h.m.speedBox())
	for _, want := range []string{"▶ 169.254.217.100 (有線)", "192.168.50.7 (無線)"} {
		if !strings.Contains(box, want) {
			t.Errorf("板に %q が無い:\n%s", want, box)
		}
	}
	h.keys("j", "y")
	f.started(t)
	if f.dest.IP != "192.168.50.7" {
		t.Errorf("j で選んだ経路で測っていない: %+v", f.dest)
	}
	if s := h.screen(); !strings.Contains(s, "通信速度を測っています") || !strings.Contains(s, "mini  192.168.50.7 (無線)") {
		t.Errorf("進捗のモーダルに選んだ経路が無い:\n%s", s)
	}
	f.results <- speedtest.Result{UpBps: 40e6, DownBps: 35e6}
	awaitSpeed(t, h.m)
	h.keys("t", "y") // 既定 (有線) に戻っている
	f.started(t)
	if f.dest.IP != "169.254.217.100" {
		t.Errorf("2 回目の既定が有線でない: %+v", f.dest)
	}
	f.results <- speedtest.Result{UpBps: 900e6, DownBps: 850e6}
	awaitSpeed(t, h.m)
	s := h.screen()
	wired, wifi := strings.Index(s, "↑ 900 Mbps  ↓ 850 Mbps (有線)"), strings.Index(s, "↑ 40 Mbps  ↓ 35 Mbps (無線)")
	if wired < 0 || wifi < 0 || wired > wifi {
		t.Errorf("経路ごとの結果が有線から並んでいない (有線 %d / 無線 %d):\n%s", wired, wifi, s)
	}
}

// 有線の経路が先頭でなくても既定は有線。台帳の内部 IP が帯の番号 (この Mac は 169.254 のまま) で網が分かれていると内部 IP は外れ、
// 候補は Wi-Fi のメイン IP → 有線の I/F で見えたその他の IP の順になる。
func TestSpeedDefaultsToWiredEvenIfNotFirst(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "192.168.100.13", Alias: "mini"}}, []discover.Peer{mini})
	h.m.o.SpeedTest = newFakeSpeed().run
	h.cursorTo(t, "m2macmini.local")
	h.keys("t")
	a := h.m.speedAsk
	if a == nil || len(a.paths) < 2 || a.paths[0].link == netinfo.LinkWired {
		t.Fatalf("前提: 先頭が有線でない候補にならない: %+v", a)
	}
	if p := a.paths[a.cur]; p.link != netinfo.LinkWired || p.ip != "169.254.217.100" {
		t.Errorf("既定の経路 = %+v, want 有線の 169.254.217.100", p)
	}
	if slices.ContainsFunc(a.paths, func(p speedPath) bool { return p.ip == "192.168.100.13" }) {
		t.Errorf("網が分かれて届かない内部 IP を候補に出した: %+v", a.paths)
	}
}

// その他の IP は、それが見えた I/F にこの Mac が持つ IP と比べて届かないもの (この Mac は 169.254、相手は帯の番号) を外し、
// 届くもの (同じ I/F でこの Mac も相手も手動の 10.0.0.x) は残す (この Mac の内部 IP 1 つと比べると、後者まで外れた)。
func TestSpeedSkipsUnreachableOthers(t *testing.T) {
	for _, c := range []struct {
		selfIP, peerIP string
		want           bool
	}{{"169.254.14.1", "192.168.100.20", false}, {"10.0.0.1", "10.0.0.2", true}} {
		h, _ := speedHarness(t)
		net := selfNet()
		net.Services = append(net.Services, netinfo.Service{Name: "USB LAN", Port: "USB 10/100/1G/2.5G LAN", Device: "en14", Active: true, IP: c.selfIP, Method: "manual", Read: true})
		net.IPs = append(net.IPs, netinfo.IP{Device: "en14", Addr: c.selfIP, Kind: netinfo.KindWired})
		h.m.self = net
		h.m.o.IfName = func(i int) string { return map[int]string{14: "en0", 27: "en13", 31: "en14"}[i] }
		p := mini
		p.Addrs = map[int]string{14: "192.168.50.7", 27: "169.254.217.100", 31: c.peerIP}
		h.scan.SetPeers([]discover.Peer{p}...)
		h.m.Advance(t0)
		h.cursorTo(t, "m2macmini.local")
		h.keys("t")
		a := h.m.speedAsk
		if a == nil {
			t.Fatal("板が出ていない")
		}
		if got := slices.ContainsFunc(a.paths, func(p speedPath) bool { return p.ip == c.peerIP }); got != c.want {
			t.Errorf("この Mac %s / 相手 %s: 候補に出した = %v, want %v (%+v)", c.selfIP, c.peerIP, got, c.want, a.paths)
		}
	}
}

// 同じ IP が線の分からない内部 IP (台帳の値) とメイン IP の両方に居たら、分かる方の線を採る。
func TestSpeedPathKeepsKnownLink(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "air.local", User: "koji", LAN: "169.254.9.9"}},
		[]discover.Peer{{Host: "air.local", SSH: true, Addrs: map[int]string{14: "169.254.9.9"}}})
	for _, r := range h.m.rows() {
		if r.host == "air.local" {
			ps := h.m.speedPaths(r)
			if len(ps) != 1 || ps[0].link != netinfo.LinkWireless {
				t.Errorf("経路 = %+v, want 169.254.9.9 (無線) の 1 つ", ps)
			}
		}
	}
}

// 今の経路に無い IP の結果は出さない (169.254 が付け直されると古い IP の行が「有線」の札のまま溜まった)。
// 板を出している間に IP が変わったら、y で古い IP を測らない。
func TestSpeedDropsStaleIPs(t *testing.T) {
	h, f := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t", "j", "y") // Wi-Fi のメイン IP (192.168.50.7) を測る
	f.started(t)
	f.results <- speedtest.Result{UpBps: 40e6, DownBps: 35e6}
	awaitSpeed(t, h.m)
	if !strings.Contains(h.screen(), "40 Mbps") {
		t.Fatalf("前提: 結果が出ていない:\n%s", h.screen())
	}
	moved := mini
	moved.Addrs = map[int]string{14: "192.168.50.8", 27: "169.254.217.100"}
	h.keys("t", "j") // 板で Wi-Fi の 192.168.50.7 を選んだ後に、相手のメイン IP が変わる
	h.scan.SetPeers([]discover.Peer{moved}...)
	h.m.Advance(t0)
	h.keys("y")
	if h.m.speedRun != nil || f.calls.Load() != 1 {
		t.Errorf("見えなくなった IP を測った: run=%+v calls=%d", h.m.speedRun, f.calls.Load())
	}
	// 今の経路に無い IP の結果は消さずに「今は見えていない」と付ける (隠すと「未測定」と出て、走査のたびに出たり消えたりした)
	if s := h.screen(); !regexp.MustCompile(`40 Mbps.*192\.168\.50\.7\)  今は見えていない`).MatchString(s) {
		t.Errorf("今の経路に無い IP の結果に印が無い:\n%s", s)
	}
	// 今見えている経路の結果を、見えていない経路の結果より上に並べる。同じ無線どうしで IP の順なら古い .7 が新しい .8 より上に出た
	h.keys("t", "j", "y")
	f.started(t)
	f.results <- speedtest.Result{UpBps: 30e6, DownBps: 25e6}
	awaitSpeed(t, h.m)
	if s := h.screen(); !strings.Contains(s, "30 Mbps") || strings.Index(s, "30 Mbps") > strings.Index(s, "40 Mbps") {
		t.Errorf("今の経路 (.8) の結果が見えていない経路 (.7) より下に出た:\n%s", s)
	}
	// Bonjour が相手を一時的に見失っても、測った結果は「未測定」にならない
	h.scan.SetPeers(nil...)
	h.m.Advance(t0)
	if s := h.screen(); strings.Contains(s, "未測定") || !strings.Contains(s, "40 Mbps") {
		t.Errorf("見失った間に結果が消えた:\n%s", s)
	}
}

// 板を出している間に経路の線が分かったら、y の時点の線で測る (板の写しの「分からない」を結果の札に残さない)。
func TestSpeedUsesLinkAtRunTime(t *testing.T) {
	h, f := speedHarness(t)
	early := mini
	early.Addrs = map[int]string{14: "192.168.50.7"} // 台帳の内部 IP はまだ有線で見えていない
	h.scan.SetPeers([]discover.Peer{early}...)
	h.m.Advance(t0)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t")
	if a := h.m.speedAsk; a == nil || a.paths[a.cur].ip != "169.254.217.100" || a.paths[a.cur].link != netinfo.LinkUnknown {
		t.Fatalf("前提: 内部 IP の線が分からない板にならない: %+v", h.m.speedAsk)
	}
	h.scan.SetPeers(mini) // 有線で見えた
	h.m.Advance(t0)
	h.keys("y")
	f.started(t)
	if h.m.speedRun == nil || h.m.speedRun.link != netinfo.LinkWired {
		t.Errorf("y の時点の線で測っていない: %+v", h.m.speedRun)
	}
}
