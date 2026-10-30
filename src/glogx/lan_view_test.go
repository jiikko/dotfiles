package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"glogx/internal/gitdata"
	"glogx/internal/lanview"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/lanmgr/lanmgrtest"
	"mac-lan-manager/live"
	"mac-lan-manager/netinfo"

	"testwait"
)

// lanScans はテストの中で起こした走査 (最後が今の走査)。
var (
	lanScansMu sync.Mutex
	lanScans   []*lanmgrtest.Scan
	lanPeers   = []discover.Peer{{Host: "m2macmini.local", Instances: []string{"テストの Mac mini"}, SSH: true, SMB: true, Addrs: map[int]string{14: "192.168.1.20"}}}
)

func lastLanScan(t *testing.T) *lanmgrtest.Scan {
	t.Helper()
	lanScansMu.Lock()
	defer lanScansMu.Unlock()
	if len(lanScans) == 0 {
		t.Fatal("走査が起きていない")
	}
	return lanScans[len(lanScans)-1]
}

// fakeLanOptions は本物の台帳・設定・ログの置き場所 (TestMain が MAC_LAN_MANAGER_CONFIG_DIR で一時領域へ向けた) に、偽の走査と
// 固定のホスト名をつなぐ。
// 🚨 live.Options() から危ない口を 1 つずつ外す形にしない。本物から写すのは一時領域を指す 4 つだけで、残りは空 (= lanmgr は起こさない)。
// 外す形だと lanmgr に口が増えるたびに外し忘れが本物のコマンドを起こす (ReadAdvert の launchctl が CI の no-real-commands で 4 回落ちた)。
func fakeLanOptions() (lanmgr.Options, error) {
	real, err := live.Options()
	if err != nil {
		return lanmgr.Options{}, err
	}
	o := lanmgr.Options{Ledger: real.Ledger, PrefsPath: real.PrefsPath, SavePrefs: real.SavePrefs, Log: real.Log}
	o.LocalHost = func() string { return "glogx-test.local" }
	o.IfName = func(int) string { return "" }
	o.User = "tester"
	o.StartScan = func(time.Time) lanmgr.Scanner {
		s := lanmgrtest.NewScan(lanPeers...)
		s.SetWorking(true)
		lanScansMu.Lock()
		lanScans = append(lanScans, s)
		lanScansMu.Unlock()
		return s
	}
	return o, nil
}

func openLan(t *testing.T) *browseModel {
	t.Helper()
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) // 80×10 では一覧の行が窓に入らない
	m.handleKey("L")
	if m.activeFullScreen() != FullScreenLan {
		t.Fatalf("L で開かない (active=%v)", m.activeFullScreen())
	}
	return m
}

// L で開いて、Bonjour で見えた Mac が出る。L でもう一度押すと閉じて一覧へ戻り、走査を止める。
func TestLanOpensAndClosesWithL(t *testing.T) {
	m := openLan(t)
	scan := lastLanScan(t)
	m.handleKey("l") // 未管理のタブ (見えた Mac は台帳に無いのでこちら)
	if view := term.StripANSI(strings.Join(m.lanV.Lines(m.contentWidth(), m.pageSize()), "\n")); !strings.Contains(view, "m2macmini") {
		t.Errorf("見えた Mac が出ていない:\n%s", view)
	}
	m.handleKey("L")
	if m.activeFullScreen() != FullScreenNone {
		t.Fatalf("L で閉じない (active=%v)", m.activeFullScreen())
	}
	if scan.Closes() == 0 {
		t.Error("閉じても走査 (dns-sd) を止めていない")
	}
}

// q / Esc は板を 1 段戻る (ガイド §1。glogx ごとは終えない)。
func TestLanQuitKeysGoBackToList(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		m := openLan(t)
		_, cmd := m.handleKey(k)
		if m.activeFullScreen() != FullScreenNone {
			t.Errorf("%s で閉じない", k)
		}
		if isQuitCmd(cmd) || m.done {
			t.Errorf("%s で glogx ごと終えた (板を 1 段戻るだけのはず)", k)
		}
	}
}

// s は ssh (lanmgr の Binds) で、status の横断キーに取られない。i は issues へ横断する。
// 絞り込みの入力中は i も文字として入る (横断で閉じると打っている文字が消える)。
func TestLanBindsBeatCrossKeys(t *testing.T) {
	m := openLan(t)
	execs := testenv.StubEditorCapture(t)
	m.handleKey("l") // 未管理のタブ (相手の Mac だけ。この Mac は自分のタブ)
	m.handleKey("s")
	if m.activeFullScreen() != FullScreenLan {
		t.Errorf("s で板を離れた (active=%v)。ssh のはず", m.activeFullScreen())
	}
	if len(*execs) != 1 {
		t.Errorf("s で ssh を頼んでいない (前景の起動 %d 回)", len(*execs))
	}

	m = openLan(t)
	m.handleKey("/")
	m.handleKey("i")
	if m.activeFullScreen() != FullScreenLan {
		t.Fatalf("絞り込みの入力中に i で横断した (active=%v)", m.activeFullScreen())
	}
	m.handleKey("esc")
	m.handleKey("i")
	if m.activeFullScreen() != FullScreenIssues {
		t.Errorf("i で issues へ横断しない (active=%v)", m.activeFullScreen())
	}
	if m.lanV.Visible() {
		t.Error("横断元の板が開いたまま")
	}
}

// 開き直すと走査を起こし直す (閉じている間に増えた Mac を出す)。走査の合図は今の走査のチャネルからだけ待ち直す。
func TestLanReopenRestartsScanAndRearmsOnlyCurrent(t *testing.T) {
	m := openLan(t)
	first := lastLanScan(t)
	m.handleKey("L")
	m.handleKey("L")
	second := lastLanScan(t)
	if first == second || second.Closes() > 0 {
		t.Fatal("開き直しで走査を起こし直していない")
	}
	if m.lanV.Rearm(lanview.FixtureChangedMsg(first.Changed(), true)) != nil {
		t.Error("古い走査の合図で待ち直した (待ちが 2 本になる)")
	}
	if m.lanV.Rearm(lanview.FixtureChangedMsg(second.Changed(), true)) == nil {
		t.Error("今の走査の合図で待ち直さない (後から見つかった Mac が出ない)")
	}
}

// 走査中は tick が回り (spinnerActive)、終了 (cancelAll) で走査を止める。
func TestLanBusyTicksAndCancelAllStopsScan(t *testing.T) {
	m := openLan(t)
	if !m.spinnerActive() {
		t.Error("走査中なのに tick が回らない (途中の結果が取り込まれない)")
	}
	m.cancelAll()
	if lastLanScan(t).Closes() == 0 {
		t.Error("cancelAll で走査 (dns-sd) を止めていない")
	}
}

// 走査の合図を受けたら取り込み、今の走査のチャネルを待ち直す。閉じた走査の合図 (ok=false) では待ち直さない。
// 判定は張った待ちが読むチャネルで見る (cmd が nil かどうかは maybeTick の single-flight の状態で変わる)。
func TestLanChangedMsgRearms(t *testing.T) {
	m := openLan(t)
	scan := lastLanScan(t)
	cur := scan.Changed()
	if m.lanV.Fixture().Watching() != cur {
		t.Fatal("開いたときに今の走査の合図を待っていない")
	}
	cmd := m.lanV.Rearm(lanview.FixtureChangedMsg(cur, true))
	if cmd == nil {
		t.Fatal("合図の後に待ち直さない (後から見つかった Mac が出ない)")
	}
	scan.Close()
	if got, ok := cmd().(lanview.LanChangedMsg); !ok || got != lanview.FixtureChangedMsg(cur, false) {
		t.Errorf("待ち直した待ちが今の走査のチャネルを読んでいない: %#v", got)
	}
	if m.lanV.Rearm(lanview.FixtureChangedMsg(cur, false)) != nil {
		t.Error("閉じた走査の合図で待ち直した")
	}
}

// 本物の Space の打鍵 (KeyPressMsg) で台帳の登録の確認が開く。glogx は Space を " " に書き換えてから板へ渡すので、
// "space" を直接渡すテストではこの経路を通らない (2026-10-15)。
func TestLanRealSpaceKeyOpensLedgerConfirm(t *testing.T) {
	m := openLan(t)
	m.handleKey("l")
	m.handleKey("j")
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if !m.lanV.OwnsKeys() {
		t.Error("Space で台帳の登録の確認が開かない (半ページ送りに化けている)")
	}
}

// 板の中の r は走査を起こし直す。新しい走査の合図を待ち直す (待たないと、後から見つかった Mac が次のキーまで出ない)。
func TestLanReloadKeyRearmsWatch(t *testing.T) {
	m := openLan(t)
	first := lastLanScan(t)
	m.handleKey("r")
	second := lastLanScan(t)
	if first == second {
		t.Fatal("r で走査を起こし直していない")
	}
	if m.lanV.Fixture().Watching() != second.Changed() {
		t.Error("r の後も古い走査の合図を待っている")
	}
}

// 絞り込みの入力中は同じ字の連打を押しっぱなしとみなして飲まない ("ss" の 2 字目が消えない)。
func TestLanTypingDoesNotSwallowRepeats(t *testing.T) {
	m := openLan(t)
	m.handleKey("/")
	m.handleKey("s")
	m.handleKey("s")
	m.handleKey("i")
	m.handleKey("i")
	m.lanV.Fixture().Model().HandleKey("enter")
	if view := term.StripANSI(strings.Join(m.lanV.Lines(m.contentWidth(), m.pageSize()), "\n")); !strings.Contains(view, "ssii") {
		t.Errorf("絞り込みに ssii が入っていない (連打が飲まれた):\n%s", view)
	}
}

// tmux の prefix が入力欄の編集キー (C-a) でも、L の絞り込みに打っている間は入力欄に届く。入力欄の外では今までどおり案内を出す。
func TestLanTmuxPrefixReachesFilterWhileTyping(t *testing.T) {
	m := openLan(t)
	m.tmuxPrefix = "ctrl+a"
	for _, k := range []string{"/", "i", "n", "ctrl+a", "m"} { // 先頭へ動いてから m
		m.handleKey(k)
	}
	m.lanV.Fixture().Model().HandleKey("enter")
	if view := term.StripANSI(strings.Join(m.lanV.Lines(m.contentWidth(), m.pageSize()), "\n")); !strings.Contains(view, "/ min") { // 絞り込みの欄 ("min" だけだと Mac mini の行でも満たせた)
		t.Errorf("入力中の C-a (prefix と同じ) が編集として効かない:\n%s", view)
	}
	if strings.Contains(m.toast.Text(), "tmux prefix") {
		t.Errorf("入力中に prefix の案内を出した: %q", m.toast.Text())
	}
	m.handleKey("ctrl+a")
	if !strings.Contains(m.toast.Text(), "tmux prefix") {
		t.Errorf("入力欄の外で prefix の案内が出ない: toast=%q", m.toast.Text())
	}
}

// L は、この Mac のネットワークの読み込み (networksetup。約 420 ms) を待たずに開く。読み込み中は走査中として tick が回り、
// 読み終えたら tick で取り込む (Update の中で同期に読むと、L を押すたびに glogx が止まった。ユーザー報告 2026-10-15)。
func TestLanOpensWithoutWaitingForNetwork(t *testing.T) {
	release := make(chan struct{})
	restore := lanview.FixtureSetOptions(func() (lanmgr.Options, error) {
		o, err := fakeLanOptions()
		o.ReadNet = func(ctx context.Context) (netinfo.Snapshot, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return netinfo.Snapshot{}, nil
		}
		return o, err
	})
	t.Cleanup(restore)
	m := openLan(t) // ReadNet が止まっていても戻る
	view := func() string { return term.StripANSI(strings.Join(m.lanV.Lines(m.contentWidth(), m.pageSize()), "\n")) }
	if !m.lanV.Busy() || !m.spinnerActive() || !strings.Contains(view(), "ネットワーク設定を確認中") {
		t.Fatalf("読み込み中なのに tick が回らない / 読んでいる印が無い: busy=%v\n%s", m.lanV.Busy(), view())
	}
	close(release)
	testwait.Until(t, "読み終えても tick で取り込まない", func() bool { return !strings.Contains(view(), "ネットワーク設定を確認中") },
		testwait.Within(5*time.Second), testwait.Step(func() { fireTick(m) }))
}

// キーの案内は枠の外の最下行に出し (他の全画面と同じ場所)、板の内側には描かない。中身は lanmgr が今の画面で効くキー。
// 一覧では lanmgr の q を「L/q: 閉じる」にし、層 (設定・初期設定・板) が開いている間は lanmgr の抜け方をそのまま出す
// (L も ? も lanmgr が取り、q / esc は 1 段戻るだけ。「L/q: 閉じる」は嘘になる)。
func TestLanHintFollowsLayers(t *testing.T) {
	m := openLan(t)
	h := term.StripANSI(m.lanV.Hint(120))
	if !strings.Contains(h, "L/q: 閉じる") || !strings.Contains(h, "j/k: 移動") || strings.Contains(h, " q: 閉じる") {
		t.Fatalf("一覧の最下行 = %q", h)
	}
	if v := term.StripANSI(strings.Join(m.lanV.Lines(120, 30), "\n")); strings.Contains(v, "j/k 移動") {
		t.Errorf("板の内側にも案内を描いた:\n%s", v)
	}
	m.handleKey(",") // 設定
	if h := term.StripANSI(m.lanV.Hint(120)); strings.Contains(h, "L/q") || !strings.Contains(h, "q/esc/,: 閉じる") || !strings.Contains(h, "space: 入/切") {
		t.Errorf("設定の画面の最下行 = %q", h)
	}
	// 足りない画面では q / esc が L ごと閉じ、L も glogx に渡る: 層が開いていても「L/q: 閉じる」
	lm := m.lanV.Fixture().Model()
	lm.Resize(20, 6)
	lm.View()
	if h := term.StripANSI(m.lanV.Hint(80)); !strings.Contains(h, "L/q: 閉じる") {
		t.Errorf("足りない画面の最下行 = %q", h)
	}
}
