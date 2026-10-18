package lanmgr

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/setup"
	"mac-lan-manager/speedtest"
)

// fakeSpeed は SpeedTest の偽物。呼ばれた宛先と ctx を控え、results に送った結果を返す (送るまで測定中のまま)。
type fakeSpeed struct {
	calls   int
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

// speedHarness は管理対象の mini (有線で見えている) と未管理の air を並べた画面。
func speedHarness(t *testing.T) (*harness, *fakeSpeed) {
	t.Helper()
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100", Alias: "mini"}},
		[]discover.Peer{mini, {Host: "air.local", SSH: true, Addrs: map[int]string{14: "192.168.50.44"}}})
	f := newFakeSpeed()
	h.m.o.SpeedTest = func(ctx context.Context, d setup.Dest, now func() time.Time) speedtest.Result {
		f.calls++
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
	if h.m.ov == ovConfirm || f.calls != 0 {
		t.Fatalf("未管理の相手で確認の板を出した / 測った: ov=%v calls=%d", h.m.ov, f.calls)
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
	if f.calls != 0 || h.m.ov == ovConfirm {
		t.Fatalf("一覧を動かしただけで測った / 板を出した: calls=%d ov=%v", f.calls, h.m.ov)
	}
	h.cursorTo(t, "m2macmini.local")
	h.keys("t")
	if h.m.ov != ovConfirm || h.m.ask == nil || !h.m.ask.strict {
		t.Fatalf("確認の板が出ていない: ov=%v ask=%+v", h.m.ov, h.m.ask)
	}
	h.keys("enter")
	if h.m.ov != ovConfirm || f.calls != 0 {
		t.Fatalf("Enter で板を閉じた / 測った: ov=%v calls=%d", h.m.ov, f.calls)
	}
	h.keys("n")
	if h.m.ov == ovConfirm || f.calls != 0 || h.m.speedRun != nil {
		t.Errorf("n で測った: calls=%d run=%v", f.calls, h.m.speedRun)
	}
	h.keys("t", "esc")
	if h.m.ov == ovConfirm || f.calls != 0 {
		t.Errorf("Esc で測った / 板が残った: calls=%d ov=%v", f.calls, h.m.ov)
	}
	h.keys("t", "Y") // Y (一覧では ssh のコピー) は取り消し
	if h.m.ov == ovConfirm || f.calls != 0 || h.m.speedRun != nil {
		t.Errorf("Y で測った / 板が残った: calls=%d run=%v ov=%v", f.calls, h.m.speedRun, h.m.ov)
	}
}

// y で測り、測っている間は Busy・詳細カードは「測定中」、終わったら上り・下り・経路を出す。100 Mbps 付近なら頭打ちの一言を足す。
func TestSpeedRunsAndShowsResult(t *testing.T) {
	h, f := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("t", "y")
	ctx := <-f.ctx
	if f.calls != 1 || f.dest.IP != "169.254.217.100" || f.dest.HostKeyAlias != "m2macmini.local" || f.dest.User != "koji" {
		t.Fatalf("測った宛先 = %+v (calls=%d), want 内部 IP 169.254.217.100", f.dest, f.calls)
	}
	if !h.m.Busy() || !strings.Contains(h.screen(), "測定中") {
		t.Errorf("測っている間: Busy=%v\n%s", h.m.Busy(), h.screen())
	}
	h.keys("t") // 測定中は 2 本目を流さない
	if f.calls != 1 || h.m.ov == ovConfirm {
		t.Errorf("測定中に 2 本目の板を出した / 測った: calls=%d ov=%v", f.calls, h.m.ov)
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
	<-f.ctx
	f.results <- speedtest.Result{Err: speedtest.ErrNoKey}
	awaitSpeed(t, h.m)
	if s := h.screen(); !strings.Contains(s, "鍵で入れないので測れない") || strings.Contains(s, "Mbps") {
		t.Errorf("詳細カード:\n%s", s)
	}
	// ssh の stderr (相手の Banner・複数行・制御文字) を含む失敗は、最後の行 (原因) を無害化して末尾が残るように出す
	h.keys("t", "y")
	<-f.ctx
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
	ctx := <-f.ctx
	h.m.Close()
	if !errors.Is(ctx.Err(), context.Canceled) || h.m.speedRun != nil {
		t.Errorf("閉じた後も測定が続く: ctx=%v run=%v", ctx.Err(), h.m.speedRun)
	}
}

// awaitSpeed は裏の測定の結果が Advance で取り込まれるまで待つ (上限つき)。
func awaitSpeed(t *testing.T, m *Model) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); m.speedRun != nil; { // awaitWired と同じ形
		if time.Now().After(deadline) {
			t.Fatal("測定の結果が 10 秒待っても取り込まれない")
		}
		m.Advance(t0)
		runtime.Gosched()
	}
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

// 測定中は最下行に t を出さない (押しても「測定中」で断るので、案内と効くキーを揃える)。この Mac のネットワークを読めていない画面では
// t を断る (CLI と同じ規則。宛先と経路を決められない)。
func TestSpeedFooterAndRefusalFollowState(t *testing.T) {
	h, _ := speedHarness(t) // 偽の測定は結果を送るまで測定中のまま
	h.cursorTo(t, "m2macmini.local")
	h.keys("t", "y")
	if h.m.speedRun == nil {
		t.Fatal("前提: 測定が始まらない")
	}
	if foot := termwidth.StripSGR(h.m.footer()); strings.Contains(foot, "t 速度") {
		t.Errorf("測定中に t を案内した: %q", foot)
	}
	h.m.stopSpeed()
	h.m.selfOK = false
	h.m.TakeNotices()
	h.keys("t")
	if n := h.m.TakeNotices(); h.m.ov == ovConfirm || len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "ネットワークを読め") {
		t.Errorf("網を読めていないのに測ろうとした: ov=%v notices=%+v", h.m.ov, n)
	}
}
