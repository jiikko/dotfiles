package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

// 持ち主への切り替えは、暗転の文言も、開き直した画面の明転と通知も「新版」と言わない (敵対レビューの P3)。新版の切り替えは今までどおり。
func TestOwnerSwitchLabelsDoNotSayUpgrade(t *testing.T) {
	m, _, _ := joinModel(t)
	m.width, m.height = 120, 30
	press(m, "O", "y")
	at := m.now().Add(switchDuration / 2)
	m.now = func() time.Time { return at }
	if v := ansi.Strip(m.applySwitchFade(m.render())); !strings.Contains(v, ownerSwitchLabel) || strings.Contains(v, switchLabel) {
		t.Fatalf("持ち主への暗転の文言: %q", v)
	}
	leaveFully(m)
	data, err := m.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	owner, _, _ := joinModel(t)
	if err := owner.ImportState(data); err != nil {
		t.Fatal(err)
	}
	if got := owner.toasts.Text(); !strings.Contains(got, "持ち主の画面に切り替えた") || strings.Contains(got, "新版") {
		t.Fatalf("開き直した画面の通知: %q", got)
	}
	if !owner.fade.owner {
		t.Fatal("明転を持ち主の文言にしない")
	}
	// 新版の切り替え (ctrl+r) は ExportState を通しても Owner を書かず、今までどおり「新版に切り替えた」
	up, _, _ := joinModel(t)
	up.switchTo = switchUpgrade
	upData, err := up.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	plain, _, _ := joinModel(t)
	if err := plain.ImportState(upData); err != nil || !strings.Contains(plain.toasts.Text(), "新版に切り替えた") || plain.fade.owner {
		t.Fatalf("新版の切り替えの通知: %q owner=%v err=%v", plain.toasts.Text(), plain.fade.owner, err)
	}
}

// 帯が O を案内している間は、引き出し・設定画面・issue の一覧を開いていても O が効く (黙って飲み込まない。548 の敵対的レビュー 2/3)。
func TestOwnerSwitchFromPanels(t *testing.T) {
	for _, c := range []struct {
		name string
		open func(m *Model)
	}{
		{"引き出し", func(m *Model) { press(m, "enter") }},
		{"設定画面", func(m *Model) { press(m, "s") }},
		{"issue の一覧", func(m *Model) { m.picker.open = true }},
	} {
		m, _, _ := joinModel(t)
		c.open(m)
		press(m, "O")
		if m.mode != modeConfirm || !m.pendingOwner {
			t.Fatalf("%s: O で確認を出さない: mode=%v pendingOwner=%v", c.name, m.mode, m.pendingOwner)
		}
		if m.set.open || m.picker.open {
			t.Fatalf("%s: 確認を出したのに板を開いたまま: set=%v picker=%v", c.name, m.set.open, m.picker.open)
		}
		if !isQuit(func() tea.Cmd { press(m, "y"); return leaveFully(m) }()) || !m.OwnerRequested() {
			t.Fatalf("%s: y で持ち主へ切り替えない", c.name)
		}
	}
}

// O の印 (pendingOwner) は、確認を抜けるどの経路でも、別の確認を置くどの経路でも下りる (1 か所ずつ直接見る。敵対的レビュー 3/3 の P3:
// 前のテストは取り消しの後に askConfirm を通していたので、どちらか 1 か所を外しても緑だった)。
func TestPendingOwnerClearedOnEveryPath(t *testing.T) {
	for _, k := range []string{"n", "esc", "j"} {
		m, _, _ := joinModel(t)
		press(m, "O", k)
		if m.pendingOwner {
			t.Fatalf("%s で取り消したのに O の印が残った", k)
		}
	}
	m, _, _ := joinModel(t)
	press(m, "O", "ctrl+c")
	if m.pendingOwner {
		t.Fatal("ctrl+c で確認を抜けたのに O の印が残った")
	}
	// 確認の中身を置き換える口は、どれも O の印を下ろす (印が残る経路が将来できても、別の確認の y が切り替えに化けない)
	m, _, _ = joinModel(t)
	m.pendingOwner = true
	m.askConfirm(backend.ResumeDispatcher{}, "別の確認 [y/N]")
	if m.pendingOwner {
		t.Fatal("askConfirm が O の印を下ろさない")
	}
	m, _, _ = joinModel(t)
	m.pendingOwner = true
	m.askSend(backend.ResumeDispatcher{}, sendConfirm{title: "送る"})
	if m.pendingOwner {
		t.Fatal("askSend が O の印を下ろさない")
	}
}
