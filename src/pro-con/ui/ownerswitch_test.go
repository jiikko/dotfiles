package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
)

// joinModel は持ち主の画面が無い join の画面 (帯が出る)。m.up は持たせない (bin/pro-con 以外から起動した = ctrl+r が無効でも切り替えられる)。
func joinModel(t *testing.T) (*Model, *joinSpy, *int) {
	t.Helper()
	be := &joinSpy{stopSpy: &stopSpy{spy: newSpy()}}
	be.snap.Screens = []backend.Screen{{ID: "aaaaaa", Join: true, Self: true}}
	m := New(be, nil)
	at := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return at }
	keep := 0
	m.OnSwitch(func() { keep++ })
	return m, be, &keep
}

// join の画面の O → y は、暗くしてから終了して持ち主の画面への切り替えを main に頼む (issue 548)。新版への切り替えとは取り違えない。
func TestJoinOwnerSwitchConfirmsThenQuits(t *testing.T) {
	m, be, keep := joinModel(t)
	// 80 桁でも帯の O の案内が切れない (帯は幅で切り詰める)
	m.width = 80
	if band := ansi.Strip(m.ownerlessBand()); !strings.Contains(band, "O で持ち主の画面に切り替える") {
		t.Fatalf("80 桁の帯に O の案内が無い: %q", band)
	}
	if h := ansi.Strip(strings.Join(m.hints(), " ")); !strings.Contains(h, "O 持ち主の画面にする") {
		t.Fatalf("join の画面の案内に O が無い: %q", h)
	}
	press(m, "O")
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "持ち主の画面に切り替えます") || !strings.Contains(m.confirmText, "dispatcher と PG を止める") {
		t.Fatalf("O で確認を出さない: mode=%v text=%q", m.mode, m.confirmText)
	}
	if isQuit(press(m, "y")) || m.OwnerRequested() {
		t.Fatal("暗くなりきる前に終了した")
	}
	if !isQuit(leaveFully(m)) || !m.OwnerRequested() || m.UpgradeRequested() || *keep != 1 {
		t.Fatalf("暗くなりきったら持ち主への切り替えとして終了するはず: owner=%v upgrade=%v keep=%d", m.OwnerRequested(), m.UpgradeRequested(), *keep)
	}
	if len(be.applied) != 0 {
		t.Fatalf("切り替えで backend に送った: %v", be.applied)
	}
}

// N / esc / 知らないキーでは何も変わらない。取り消した後の別の確認の y で持ち主へ切り替えない (確認の中身を取り違えない)。
func TestJoinOwnerSwitchCancel(t *testing.T) {
	for _, k := range []string{"n", "N", "esc", "j"} {
		m, be, _ := joinModel(t)
		press(m, "O", k)
		if m.mode != modeBoard || m.OwnerRequested() || m.leavingProgress(m.now()) >= 0 || len(be.applied) != 0 {
			t.Fatalf("%s で取り消さない: mode=%v owner=%v leaving=%v applied=%v", k, m.mode, m.OwnerRequested(), m.leavingProgress(m.now()), be.applied)
		}
	}
	m, be, _ := joinModel(t)
	be.snap.Cards = append(be.snap.Cards, be.snap.Cards...) // 選べるカードを持つ (newSpy の既定)
	press(m, "O", "esc")
	m.askConfirm(backend.ResumeDispatcher{}, "別の確認 [y/N]")
	press(m, "y")
	if m.leavingProgress(m.now()) >= 0 || m.OwnerRequested() || len(be.applied) != 1 {
		t.Fatalf("別の確認の y が切り替えになった: leaving=%v applied=%v", m.leavingProgress(m.now()), be.applied)
	}
}

// 持ち主の画面では O は切り替えない (確認も出さない)。案内にも出さない。
func TestOwnerScreenIgnoresOwnerSwitch(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	m := New(be, nil)
	press(m, "O")
	if m.mode != modeBoard || m.pendingOwner {
		t.Fatalf("持ち主の画面で O が確認を出した: mode=%v", m.mode)
	}
	if h := ansi.Strip(strings.Join(m.hints(), " ")); strings.Contains(h, "O 持ち主の画面にする") {
		t.Fatalf("持ち主の画面の案内に O を出した: %q", h)
	}
}

// 切り替えの exec が失敗して戻ったら、join の画面のまま続ける (暗転を戻し、要求を下ろす)。
func TestJoinOwnerSwitchFailedKeepsJoin(t *testing.T) {
	m, _, _ := joinModel(t)
	press(m, "O", "y")
	leaveFully(m)
	m.SwitchFailed(nil)
	if m.OwnerRequested() || m.leavingProgress(m.now()) >= 0 || !strings.Contains(m.toasts.Text(), "join の画面のまま続ける") {
		t.Fatalf("失敗の後: owner=%v leaving=%v toast=%q", m.OwnerRequested(), m.leavingProgress(m.now()), m.toasts.Text())
	}
}

// 帯のどの状態でも、80 桁で O の案内が見える (固まった dispatcher は切り替えても起きないので案内しない)。
func TestOwnerlessBandShowsOwnerKeyAt80(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*joinSpy)
		want  bool
	}{
		{"動いている", func(*joinSpy) {}, true},
		{"1 度も回っていない", func(be *joinSpy) { be.snap.DispatcherTick = time.Time{} }, true},
		{"落ちた", func(be *joinSpy) {
			be.snap.DispatcherTick, be.snap.DispatcherGone = be.snap.Now.Add(-3*time.Minute), true
		}, true},
		{"人が止めた", func(be *joinSpy) { be.snap.DispatcherHeld = true }, true},
		{"固まった", func(be *joinSpy) { be.snap.DispatcherTick = be.snap.Now.Add(-3 * time.Minute) }, false},
	} {
		_, be, _ := joinModel(t)
		c.setup(be)
		m := New(be, nil) // setup の後の snap で作り直す
		m.width = 80
		band := ansi.Strip(m.ownerlessBand())
		if band == "" || strings.Contains(band, "O で持ち主の画面に切り替え") != c.want {
			t.Fatalf("%s: 80 桁の帯 = %q (O の案内を出す = %v)", c.name, band, c.want)
		}
	}
}
