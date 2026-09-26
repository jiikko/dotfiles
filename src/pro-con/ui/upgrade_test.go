package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// upgradeModel は一時ディレクトリの repo の形 (src/pro-con/{go.mod,pro-con} + bin/lib/go_autobuild.zsh) から
// 起動したことにした Model。shim への問い合わせの結果は *reply で決め、呼ばれた回数を数える。
func upgradeModel(t *testing.T, reply *error) (*Model, string, *int) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "src", "pro-con")
	for _, d := range []string{dir, filepath.Join(root, "bin", "lib")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{filepath.Join(dir, "go.mod"): "module pro-con\n", filepath.Join(dir, "pro-con"): "old",
		filepath.Join(root, "bin", "lib", "go_autobuild.zsh"): "#"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "pro-con"), old, old) // 動いている版は 1 時間前にビルドしたもの
	calls := 0
	run := func(context.Context, string, ...string) error { calls++; return *reply }
	m := New(newSpy(), nil)
	if err := m.EnableUpgrade(filepath.Join(dir, "pro-con"), run); err != nil {
		t.Fatal(err)
	}
	return m, m.up.src.Exe, &calls
}

var (
	spawnOK   = error(nil)
	notNeeded = exec.Command("sh", "-c", "exit 1").Run() // shim の rc=1 (要らない / backoff)
)

func cycle(m *Model) { m.onUpgradeCheck(m.checkUpgrade()().(upgradeCheckMsg)) }

func (m *Model) pressCtrlR() tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	return cmd
}

func stampFailure(t *testing.T, exe string, at time.Time) {
	t.Helper()
	p := filepath.Join(filepath.Dir(exe), ".autobuild.failed")
	if err := os.WriteFile(p, []byte("fp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

// shim に頼んでビルド中 → バイナリが差し替わったら新版あり → ctrl+r で終了して main に切り替えを任せる。
func TestUpgradeFlow(t *testing.T) {
	reply := spawnOK
	m, exe, calls := upgradeModel(t, &reply)
	cycle(m)
	if *calls != 1 || m.up.state != upBuilding {
		t.Fatalf("shim に頼んでビルド中になるはず: calls=%d state=%v", *calls, m.up.state)
	}
	if isQuit(m.pressCtrlR()) {
		t.Fatal("新版が無いのに ctrl+r で終了した")
	}
	if err := os.WriteFile(exe+".new", []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(exe+".new", exe); err != nil { // shim の mv -f と同じ差し替え
		t.Fatal(err)
	}
	cycle(m)
	if m.up.state != upReady || *calls != 1 {
		t.Fatalf("差し替わったら新版ありで、もう shim には頼まない: state=%v calls=%d", m.up.state, *calls)
	}
	if isQuit(m.pressCtrlR()) || m.UpgradeRequested() {
		t.Fatal("ctrl+r で暗転を挟まずに終了した (issue 509)")
	}
	if !isQuit(leaveFully(m)) || !m.UpgradeRequested() {
		t.Fatal("暗くなりきったら切り替えを頼んで終了するはず")
	}
}

// leaveFully は暗転の所要を過ぎたところまで時計を進めてコマを 1 つ送る (その Cmd を返す)。
func leaveFully(m *Model) tea.Cmd {
	at := m.now().Add(switchDuration)
	m.now = func() time.Time { return at }
	_, cmd := m.Update(frameMsg{})
	return cmd
}

// ビルドが失敗した後も shim には尋ね続け、ソースを直して shim が再挑戦すればビルド中に戻る (2 周目 P1 の回帰)。
func TestUpgradeRecoversAfterFailure(t *testing.T) {
	reply := spawnOK
	m, exe, calls := upgradeModel(t, &reply)
	cycle(m) // 頼んだ → ビルド中
	stampFailure(t, exe, time.Now().Add(time.Second))
	reply = notNeeded // 同じ入力なので shim は backoff
	cycle(m)
	if m.up.state != upFailed {
		t.Fatalf("頼んだビルドより後の失敗を出さない: %v", m.up.state)
	}
	reply = spawnOK // ソースを直した: 指紋が変わり shim が再挑戦する
	cycle(m)
	if m.up.state != upBuilding || *calls != 3 {
		t.Fatalf("失敗の後も尋ねてビルド中に戻るはず: state=%v calls=%d", m.up.state, *calls)
	}
}

// 起動したバイナリより新しい失敗の記録は最初から出す (動いている版より新しいソースが落ちている)。古い記録は出さない。
func TestUpgradeFailureBeforeStart(t *testing.T) {
	reply := notNeeded
	m, exe, _ := upgradeModel(t, &reply)
	stampFailure(t, exe, time.Now().Add(-2*time.Hour))
	cycle(m)
	if m.up.state != upNone {
		t.Fatalf("動いている版より古い失敗を出した: %v", m.up.state)
	}
	stampFailure(t, exe, time.Now().Add(-time.Minute))
	cycle(m)
	if m.up.state != upFailed {
		t.Fatalf("動いている版より新しい失敗を出さない: %v", m.up.state)
	}
}

// shim に尋ねられない (zsh が無い等) ときは「要らない」と区別して知らせ、状態は変えない。
func TestUpgradeSpawnErrorIsShown(t *testing.T) {
	reply := errors.New("zsh が無い")
	m, _, _ := upgradeModel(t, &reply)
	cycle(m)
	if m.up.state != upNone || !strings.Contains(m.toasts.Text(), "確認ができない") {
		t.Fatalf("尋ねられなかったことを知らせていない: state=%v flash=%q", m.up.state, m.toasts.Text())
	}
}

func startChild(t *testing.T, m *Model) (release func()) {
	t.Helper()
	ch := make(chan struct{})
	cmd := m.child(func() tea.Msg { <-ch; return nil })
	go cmd()
	for i := 0; m.children.Load() == 0; i++ {
		if i > 200 {
			t.Fatal("裏の処理が走り始めない")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return func() { close(ch) }
}

// exec の前に、裏で外部コマンドを起こしている処理の終わりを待つ (待ち切れなければ false)。
// 作っただけで走らなかった処理 (終了の直前に bubbletea に捨てられた Cmd) は数に入らない (2 周目 P2 の回帰)。
func TestWaitChildren(t *testing.T) {
	m := New(newSpy(), nil)
	_ = m.child(func() tea.Msg { return nil }) // 作ったが走らない
	if !m.WaitChildren(20 * time.Millisecond) {
		t.Fatal("走っていない処理を待った")
	}
	release := startChild(t, m)
	if m.WaitChildren(20 * time.Millisecond) {
		t.Fatal("走っている処理があるのに待ち終わった")
	}
	release()
	if !m.WaitChildren(time.Second) {
		t.Fatal("終わったのに待ち続ける")
	}
}

// UI の状態 (タブ・レーン・選択・開いている板・書きかけの入力とカーソル) は引き継ぐ。確認中 (y/N) は引き継がない。
func TestUIStateRoundTrip(t *testing.T) {
	a := New(newSpy(), nil)
	press(a, "right", "enter")
	press(a, "+")
	typeText(a, "abcd")
	a.Update(ctrl('b'))
	a.Update(ctrl('b'))
	data, err := a.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	b := New(newSpy(), nil)
	if err := b.ImportState(data); err != nil {
		t.Fatal(err)
	}
	if b.selected != a.selected || b.col != a.col || !b.showDetail {
		t.Fatalf("選択か開いている板が戻らない: %q/%d detail=%v", b.selected, b.col, b.showDetail)
	}
	if b.mode != modeInput || b.inputKind != inputOrder || b.line.String() != "abcd" || b.line.Cursor() != 2 {
		t.Fatalf("書きかけの入力が戻らない: mode=%v kind=%v %q cur=%d", b.mode, b.inputKind, b.line.String(), b.line.Cursor())
	}
	// 設定画面は開いたまま、同じタブで戻る (入力欄とは同時に開かない: 設定画面はキーを全部受ける)
	e := New(newSpy(), nil)
	press(e, "s")
	e.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	data, _ = e.ExportState()
	f := New(newSpy(), nil)
	if err := f.ImportState(data); err != nil || !f.set.open || f.set.tab != e.set.tab || f.set.tab != tabProcs {
		t.Fatalf("設定画面が戻らない: err=%v open=%v tab=%v (元 %v)", err, f.set.open, f.set.tab, e.set.tab)
	}
	c := New(newSpy(), nil)
	press(c, "+")
	c.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	typeText(c, "B 案")
	press(c, "enter") // y/N 確認中
	data, _ = c.ExportState()
	d := New(newSpy(), nil)
	_ = d.ImportState(data)
	if d.mode == modeConfirm {
		t.Fatal("確認中の状態を引き継いだ")
	}
}

// 範囲の外のカーソル (壊れた・書き換えられたファイル) でも止まらずに読み戻す。
func TestImportClampsCursor(t *testing.T) {
	for _, cur := range []int{-5, 999} {
		m := New(newSpy(), nil)
		data := []byte(`{"input":true,"inputKind":3,"line":"abc","cursor":` + itoa(cur) + `}`)
		fin := make(chan struct{})
		go func() { _ = m.ImportState(data); close(fin) }()
		select {
		case <-fin:
		case <-time.After(2 * time.Second):
			t.Fatalf("cursor=%d で止まった", cur)
		}
		if c := m.line.Cursor(); c < 0 || c > 3 {
			t.Fatalf("cursor=%d がはみ出した: %d", cur, c)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// attach の照合 (live は claude agents を呼ぶ) も「裏で外部コマンドを起こす処理」として数え、exec の前に待つ。
func TestAttachCheckIsTracked(t *testing.T) {
	release := make(chan struct{})
	be := &blockingAttach{spy: newSpy(), release: release}
	m := New(be, nil)
	press(m, "right") // 質問待ちの W1 (session がある)
	cmd := press(m, "a")
	go cmd()
	for i := 0; m.children.Load() == 0; i++ {
		if i > 200 {
			t.Fatal("照合が数に入らない")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if m.WaitChildren(30 * time.Millisecond) {
		t.Fatal("照合の途中なのに待ち終わった")
	}
	close(release)
	if !m.WaitChildren(time.Second) {
		t.Fatal("照合が終わっても待ち終わらない")
	}
}

// blockingAttach は release が閉じるまで AttachCommand を返さない backend (照合に時間がかかる live の代わり)。
type blockingAttach struct {
	*spy
	release chan struct{}
}

func (b *blockingAttach) AttachCommand(id string) (*exec.Cmd, error) {
	<-b.release
	return b.spy.AttachCommand(id)
}

// 切り替えの前に裏の処理を待つ。待ち切れなければ書き出さずにエラー (exec させない)。
func TestPrepareSwitchWaitsForChildren(t *testing.T) {
	m := New(newSpy(), nil)
	release := startChild(t, m)
	if data, err := m.PrepareSwitch(20 * time.Millisecond); err == nil || data != nil {
		t.Fatalf("裏の処理が走っているのに書き出した: %v", err)
	}
	release()
	if data, err := m.PrepareSwitch(time.Second); err != nil || len(data) == 0 {
		t.Fatalf("終わった後は書き出すはず: %v", err)
	}
}

// 書きかけの入力は宛先のカードが同じときだけ戻す。無くなっていたら戻さず、書いた文を通知に出す (別のカードへ送らない)。
func TestImportDropsInputForVanishedCard(t *testing.T) {
	m := New(newSpy(), nil)
	data := []byte(`{"selected":"GONE","input":true,"inputKind":0,"line":"遅延ロードで","cursor":6}`) // 0 = 回答
	if err := m.ImportState(data); err != nil {
		t.Fatal(err)
	}
	if m.mode == modeInput {
		t.Fatalf("宛先の無い回答の入力を戻した (選択は %s)", m.selected)
	}
	if !strings.Contains(m.sticky, "遅延ロードで") {
		t.Fatalf("書いた文を残していない: %q", m.sticky)
	}
}

// 最後に頼んだビルドより前の失敗の記録は出さない (その後に頼み直している)。
func TestFailureBeforeLastSpawnIsIgnored(t *testing.T) {
	reply := spawnOK
	m, exe, _ := upgradeModel(t, &reply)
	cycle(m)                                           // 今頼んだ
	stampFailure(t, exe, time.Now().Add(-time.Second)) // 起動 (1 時間前) より後、頼んだより前の失敗
	reply = notNeeded
	cycle(m)
	if m.up.state == upFailed {
		t.Fatal("頼み直す前の失敗を出した")
	}
}

// 捨てた書きかけの文は、消すまで残る欄 (sticky) に置く。後から来る通知 (設定の警告・ビルドの失敗など) で消えず、
// ボードの esc で消える (3 周目 P2-2 / 4 周目 P2-1)。
func TestDroppedInputSurvivesNotify(t *testing.T) {
	m := New(newSpy(), nil)
	_ = m.ImportState([]byte(`{"selected":"GONE","input":true,"inputKind":0,"line":"遅延ロードで"}`))
	m.Notify("設定の警告 1 件")
	m.fail("新版のビルドに失敗した") // 起動直後の通知で上書きされる
	if !strings.Contains(m.sticky, "遅延ロードで") || !strings.Contains(ansi.Strip(m.render()), "遅延ロードで") {
		t.Fatalf("書いた文が消えた: sticky=%q", m.sticky)
	}
	press(m, "esc")
	if m.sticky != "" {
		t.Fatal("esc で消えない")
	}
}

// 宛先が変わっていたら書きかけの入力は戻さない: 新しい依頼はタブ、回答は「まだ質問待ちか」も見る。
func TestImportChecksInputTargets(t *testing.T) {
	cases := []struct {
		name string
		st   string
	}{
		{"新しい依頼のタブが無い", `{"tab":"nope","input":true,"inputKind":3,"line":"x"}`},
		{"回答の宛先がもう質問待ちでない", `{"selected":"R1","col":2,"input":true,"inputKind":0,"line":"x"}`},
	}
	for _, tc := range cases {
		m := New(newSpy(), nil)
		_ = m.ImportState([]byte(tc.st))
		if m.mode == modeInput {
			t.Fatalf("%s: 入力を戻した", tc.name)
		}
	}
	// 宛先が同じなら戻す (誤って捨てない)
	m := New(newSpy(), nil)
	_ = m.ImportState([]byte(`{"selected":"W1","col":3,"input":true,"inputKind":0,"line":"遅延で"}`))
	if m.mode != modeInput || m.line.String() != "遅延で" {
		t.Fatalf("宛先が同じ回答を戻さない: mode=%v %q", m.mode, m.line.String())
	}
}

// バイナリが見えなくなったら知らせる。回復した後にまた失敗したら、もう一度知らせる。
func TestUpgradeCheckErrorsAreShownAgainAfterRecovery(t *testing.T) {
	reply := notNeeded
	m, exe, _ := upgradeModel(t, &reply)
	data, _ := os.ReadFile(exe)
	info, _ := os.Stat(exe)
	_ = os.Remove(exe)
	cycle(m)
	if !strings.Contains(m.toasts.Text(), "バイナリを見られない") {
		t.Fatalf("バイナリが消えたことを知らせない: %q", m.toasts.Text())
	}
	_ = os.WriteFile(exe, data, 0o755)
	_ = os.Chtimes(exe, info.ModTime(), info.ModTime())
	m.toasts.Clear()
	cycle(m) // 回復 (差し替わった扱いになるが、ここでは通知の忘れ方だけを見る)
	_ = os.Remove(exe)
	m.toasts.Clear()
	cycle(m)
	if !strings.Contains(m.toasts.Text(), "バイナリを見られない") {
		t.Fatalf("回復した後の失敗を知らせない: %q", m.toasts.Text())
	}
}

func TestUpgradeFailedNilDoesNotPanic(t *testing.T) {
	reply := spawnOK
	m, _, _ := upgradeModel(t, &reply)
	m.UpgradeFailed(nil)
	if m.UpgradeRequested() || m.toasts.Text() == "" {
		t.Fatalf("nil でも旧版のまま続けて知らせるはず: %q", m.toasts.Text())
	}
}
