package ui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// readyModel は新版ありの状態の Model (ctrl+r で切り替えられる)。keep は終了の直前に呼ばれた回数。
func readyModel(t *testing.T) (*Model, *clock, *int) {
	t.Helper()
	reply := spawnOK
	m, _, _ := upgradeModel(t, &reply)
	m.up.state = upReady
	clk := &clock{t: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)}
	m.now = clk.now
	keep := 0
	m.OnSwitch(func() { keep++ })
	return m, clk, &keep
}

// ctrl+r は暗くしてから終了する: 暗転の途中はキーを受けず終了もしない。暗くなりきったら alt screen を残す合図 (keep) を出して終了し、
// 最後に描いた暗い 1 枚を引き継ぐ状態に入れる。
func TestSwitchDimsThenQuits(t *testing.T) {
	m, clk, keep := readyModel(t)
	bright := m.View().Content
	if isQuit(m.pressCtrlR()) {
		t.Fatal("暗転を挟まずに終了した")
	}
	clk.t = clk.t.Add(switchDuration / 2)
	if _, cmd := m.Update(frameMsg{}); isQuit(cmd) {
		t.Fatal("暗転の途中で終了した")
	}
	half := m.View().Content
	if half == bright || !strings.Contains(ansi.Strip(half), switchLabel) {
		t.Fatalf("暗転の途中で暗くならない / 文言が無い: %q", ansi.Strip(half)[:80])
	}
	col := m.col
	press(m, "l")
	if m.col != col {
		t.Fatal("暗転の途中のキーで画面が動いた")
	}
	clk.t = clk.t.Add(switchDuration)
	_, cmd := m.Update(frameMsg{})
	if !isQuit(cmd) || !m.UpgradeRequested() || *keep != 1 {
		t.Fatalf("暗くなりきったら終了して alt screen を残すはず: quit=%v requested=%v keep=%d", isQuit(cmd), m.UpgradeRequested(), *keep)
	}
	dark := m.View().Content
	data, err := m.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	b := New(newSpy(), nil)
	if err := b.ImportState(data); err != nil {
		t.Fatal(err)
	}
	if got := b.View().Content; got != dark {
		t.Fatal("新版の最初の 1 枚が旧版の最後の 1 枚と違う (切り替えで画面が跳ぶ)")
	}
}

// 切り替えに失敗して旧版のまま続けるときは、暗いままにしない (文言も消し、最後の 1 枚も持たない)。
func TestSwitchFailureRestoresBrightness(t *testing.T) {
	m, clk, _ := readyModel(t)
	m.pressCtrlR()
	clk.t = clk.t.Add(switchDuration)
	m.Update(frameMsg{})
	m.UpgradeFailed(os.ErrPermission)
	if got := m.View().Content; strings.Contains(ansi.Strip(got), switchLabel) || !m.fade.leaving.IsZero() || m.fade.lastView != "" {
		t.Fatal("失敗したのに暗転の状態が残った")
	}
	if m.framing || m.spinning {
		t.Fatal("前の Program の tick が回っている印が残った (起こし直した Program で演出のコマを予約せず、暗転が進まない)")
	}
	if isQuit(m.pressCtrlR()) {
		t.Fatal("失敗の後の ctrl+r で暗転を挟まずに終了した")
	}
	clk.t = clk.t.Add(switchDuration)
	if _, cmd := m.Update(frameMsg{}); !isQuit(cmd) {
		t.Fatal("失敗の後の 2 回目の切り替えが終わらない")
	}
}

// 新版は受け取った 1 枚を、最初の読み込み (知らせ) が届くまで出し、届いたら明るく戻して自分のボードになる。
// 待っている間にキーを打ったら待たずに戻す。
func TestArriveWaitsThenBrightens(t *testing.T) {
	be := &notifySpy{spy: newSpy(), ch: make(chan struct{})}
	m := New(be, nil)
	clk := &clock{t: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)}
	m.now = clk.now
	board := m.View().Content
	m.startArriving("OLD FRAME")
	if got := m.View().Content; got != "OLD FRAME" {
		t.Fatalf("受け取った 1 枚を出さない: %q", got)
	}
	m.Update(changedMsg{})
	clk.t = clk.t.Add(switchDuration / 2)
	mid := m.View().Content
	if mid == "OLD FRAME" || mid == board || !strings.Contains(ansi.Strip(mid), arrivedLabel) {
		t.Fatal("知らせが届いたのに明転しない")
	}
	clk.t = clk.t.Add(switchDuration)
	if got := m.View().Content; got != m.render() {
		t.Fatal("明転し終えても自分のボードに戻らない")
	}

	k := New(be, nil)
	k.now = clk.now
	k.startArriving("OLD FRAME")
	press(k, "l")
	if k.View().Content == "OLD FRAME" {
		t.Fatal("待っている間にキーを打っても戻らない")
	}
}

// 知らせない backend (模擬) は最初から中身があるので、すぐ明転を始める。
func TestArriveCmdWithoutNotifier(t *testing.T) {
	m := New(newSpy(), nil)
	if m.arriveCmd() != nil {
		t.Fatal("受け取っていないのに明転の合図を出した")
	}
	m.startArriving("X")
	cmd := m.arriveCmd()
	if cmd == nil {
		t.Fatal("明転の合図が無い")
	}
	if _, ok := cmd().(arriveGoMsg); !ok {
		t.Fatal("模擬ではすぐ明転を始めるはず")
	}
}

// 色を背景へ寄せる: 256 色・truecolor・基本色・既定の前景を暗くし、既定の地と色以外の装飾は残す。
func TestDimANSI(t *testing.T) {
	fg, bg := rgb{200, 200, 200}, rgb{0, 0, 0}
	cases := []struct{ in, want string }{
		{"a", "\x1b[38;2;100;100;100;49ma"},                                                              // 既定の前景
		{"\x1b[38;5;196mx", "\x1b[38;2;100;100;100;49m\x1b[38;2;128;0;0;49mx"},                           // 256 色 (196 = 255,0,0)
		{"\x1b[1;38;2;10;20;30;48;5;17mx", "\x1b[38;2;100;100;100;49m\x1b[1;38;2;5;10;15;48;2;0;0;48mx"}, // 太字は残す・地も寄せる (17 = 0,0,95)
		{"\x1b[31mx\x1b[0my", "\x1b[38;2;100;100;100;49m\x1b[38;2;103;0;0;49mx\x1b[0;38;2;100;100;100;49my"},
		{"\x1b[mx", "\x1b[38;2;100;100;100;49m\x1b[0;38;2;100;100;100;49mx"},                                                       // 空の SGR = reset
		{"\x1b[Kx", "\x1b[38;2;100;100;100;49m\x1b[Kx"},                                                                            // SGR 以外の制御はそのまま
		{"\x1b[38;5mx", "\x1b[38;2;100;100;100;49m\x1b[38;2;100;100;100;49mx"},                                                     // 引数の足りない色は捨てる (足す色と繋げない)
		{"\x1b[1;38;2;10;20mx", "\x1b[38;2;100;100;100;49m\x1b[1;38;2;100;100;100;49mx"},                                           // 同上 (1 は残す)
		{"\x1b[38:2::255:0:0mx", "\x1b[38;2;100;100;100;49m\x1b[38;2;100;100;100;49mx"},                                            // : 区切りは読めないので既定の色
		{"\x1b[48;5;22ma\nb", "\x1b[38;2;100;100;100;49m\x1b[38;2;100;100;100;48;2;0;48;0ma\n\x1b[38;2;100;100;100;48;2;0;48;0mb"}, // 行ごとに置き直す
	}
	for _, c := range cases {
		if got := dimANSI(c.in, 0.5, fg, bg); got != c.want {
			t.Errorf("dimANSI(%q)\n got  %q\n want %q", c.in, got, c.want)
		}
	}
	if got := dimANSI("\x1b[31mx", 1, fg, bg); got != "\x1b[31mx" {
		t.Errorf("k=1 で書き換えた: %q", got)
	}
	// 背景が明るい端末では明るい方へ寄せる (既定の地は端末の地そのものなので触らない)
	if got := dimANSI("a", 0.5, rgb{0, 0, 0}, rgb{255, 255, 255}); got != "\x1b[38;2;128;128;128;49ma" {
		t.Errorf("明るい背景: %q", got)
	}
}

// 子 (attach・エディタ) の stdout は端末のまま (画面の出力を包んでも、パイプを挟ませない)。
func TestExecOnTerminalKeepsStdout(t *testing.T) {
	c := exec.Command("true")
	_ = execOnTerminal(c, func(error) tea.Msg { return nil })
	if c.Stdout != os.Stdout {
		t.Fatalf("stdout が端末でない: %T", c.Stdout)
	}
}

// 暗転に入った後に attach の照合が終わっても、端末を渡さない (暗転の途中・alt screen を残す終了と重ねない)。
func TestAttachIsDroppedDuringSwitch(t *testing.T) {
	m, _, _ := readyModel(t)
	m.pressCtrlR()
	ran := false
	m.execProcess = func(*exec.Cmd, tea.ExecCallback) tea.Cmd { ran = true; return nil }
	m.Update(attachReadyMsg{cardID: m.selected, cmd: exec.Command("true")})
	if ran {
		t.Fatal("暗転の途中に attach へ端末を渡した")
	}
}

// 最初の View で 1 度だけ知らせる (旧版から受け取った alt screen を bubbletea に任せる印)。
func TestOnFirstViewOnce(t *testing.T) {
	m := New(newSpy(), nil)
	n := 0
	m.OnFirstView(func() { n++ })
	m.View()
	m.View()
	if n != 1 {
		t.Fatalf("最初の View で 1 度だけのはず: %d", n)
	}
}
