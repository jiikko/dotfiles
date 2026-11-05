package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.Local)

// fakeRun は名前 (と最初の引数) から出力を返す。登録の無い呼び出しは起動の失敗にする。
type fakeOut struct {
	out, err string
	rc       int
}

func fakeRun(t *testing.T, m map[string]fakeOut) func(ctx context.Context, name string, args ...string) (string, string, int, error) {
	t.Helper()
	return func(_ context.Context, name string, args ...string) (string, string, int, error) {
		key := filepath.Base(name)
		if len(args) > 0 {
			if o, ok := m[key+" "+args[0]]; ok {
				return o.out, o.err, o.rc, nil
			}
		}
		if o, ok := m[key]; ok {
			return o.out, o.err, o.rc, nil
		}
		return "", "", -1, errors.New("fake: 登録なし " + key)
	}
}

// psLine は ps の 1 行を組む (comm は空白を含んでよい)。
func psLine(pid, ppid int, stat string, cpu float64, rssKB int64, etime, comm string) string {
	return strings.Join([]string{itoa(pid), itoa(ppid), stat, ftoa(cpu), itoa(int(rssKB)), etime, "??", comm}, "  ")
}

func itoa(n int) string { return strconv.Itoa(n) }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

func findCheck(t *testing.T, cs []Check, id string) Check {
	t.Helper()
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("項目 %s が無い: %+v", id, cs)
	return Check{}
}

func TestParsePSKeepsSpacesInCommAndCountsBadLines(t *testing.T) {
	out := psLine(10, 1, "Ss", 1.5, 2048, "01:02:03", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome") + "\n" +
		"garbage line\n" +
		psLine(11, 10, "Z", 0, 0, "25-22:22:16", "<defunct>") + "\n"
	ps, bad := parsePS(out)
	if bad != 1 || len(ps) != 2 {
		t.Fatalf("ps=%d bad=%d; want 2, 1", len(ps), bad)
	}
	if ps[0].Exe != "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" {
		t.Errorf("comm の空白が保たれていない: %q", ps[0].Exe)
	}
	if ps[0].Name() != "?" {
		t.Errorf("ucomm が無いのに argv[0] から名前を作った: %q", ps[0].Name())
	}
	if ps[0].Elapsed != time.Hour+2*time.Minute+3*time.Second || ps[0].RSSKB != 2048 || ps[0].CPU != 1.5 {
		t.Errorf("数の列が違う: %+v", ps[0])
	}
	if !ps[1].zombie() {
		t.Errorf("Z をゾンビと読まない")
	}
}

func snapOf(first, second []proc) *psSnapshot {
	return &psSnapshot{First: first, Second: second, Interval: 5 * time.Second}
}

func TestRunawayNeedsBothSamplesAndAge(t *testing.T) {
	old := 2 * time.Hour
	first := []proc{
		{PID: 1, CPU: 99, Elapsed: old, Exe: "/x/spin", UName: "spin"},
		{PID: 2, CPU: 99, Elapsed: old, Exe: "/x/spike", UName: "spike"},
		{PID: 3, CPU: 99, Elapsed: time.Minute, Exe: "/x/young", UName: "young"},
		{PID: 4, CPU: 3, Elapsed: old, Exe: "/x/rising", UName: "rising"},
	}
	second := []proc{
		{PID: 1, CPU: 98, Elapsed: old, Exe: "/x/spin", UName: "spin"},
		{PID: 2, CPU: 3, Elapsed: old, Exe: "/x/spike", UName: "spike"},
		{PID: 3, CPU: 99, Elapsed: time.Minute, Exe: "/x/young", UName: "young"},
		{PID: 4, CPU: 99, Elapsed: old, Exe: "/x/rising", UName: "rising"},
	}
	c := runaway(snapOf(first, second))
	if c.Status != StatusWarn || !strings.Contains(c.Value, "spin") || strings.Contains(c.Value, "spike") || strings.Contains(c.Value, "young") || strings.Contains(c.Value, "rising") {
		t.Fatalf("張り付き = 2 回とも高い・起動から 10 分以上 だけ: %+v", c)
	}
	if c := runaway(snapOf(second[1:2], second[1:2])); c.Status != StatusOK {
		t.Errorf("張り付きが無いのに %s", c.Status)
	}
}

func TestZombiesGroupByParentAndWarnOnlyWhenStale(t *testing.T) {
	ps := []proc{
		{PID: 1223, PPID: 1, Exe: "/System/Applications/Utilities/Screen Sharing.app/Contents/MacOS/Screen Sharing", UName: "Screen Sharing"},
		{PID: 5, PPID: 1223, Stat: "Z", Elapsed: 25 * 24 * time.Hour, Exe: "<defunct>"},
		{PID: 6, PPID: 1223, Stat: "Z", Elapsed: time.Minute, Exe: "<defunct>"},
	}
	c := zombies(ps)
	if c.Status != StatusWarn || !strings.Contains(c.Value, "Screen Sharing (pid 1223) の下に 2 個") || c.Fix == "" {
		t.Fatalf("親ごとにまとめ、古いものがあれば注意: %+v", c)
	}
	tmuxParent := []proc{{PID: 61035, PPID: 1, UName: "tmux"}, {PID: 7, PPID: 61035, Stat: "Z", Elapsed: 2 * time.Hour}}
	if c := zombies(tmuxParent); c.Status != StatusWarn || !strings.Contains(c.Fix, "tmux は再起動の対象にしない") {
		t.Errorf("tmux のサーバの再起動は勧めない: %+v", c)
	}
	if c := zombies(ps[:1]); c.Status != StatusOK {
		t.Errorf("ゾンビが無いのに %s", c.Status)
	}
	if c := zombies([]proc{ps[0], ps[2]}); c.Status != StatusInfo {
		t.Errorf("1 時間以内のゾンビだけなら情報: %s", c.Status)
	}
}

func TestMemGrowthNeedsAbsoluteAndRatio(t *testing.T) {
	first := []proc{
		{PID: 1, RSSKB: 1024 * 1024, Exe: "/x/leaky", UName: "leaky"},
		{PID: 2, RSSKB: 20 * 1024 * 1024, Exe: "/x/huge", UName: "huge"},
		{PID: 3, RSSKB: 1024, Exe: "/x/tiny", UName: "tiny"},
	}
	second := []proc{
		{PID: 1, RSSKB: 1024*1024 + 300*1024, Exe: "/x/leaky", UName: "leaky"},  // +300MB (+29%)
		{PID: 2, RSSKB: 20*1024*1024 + 300*1024, Exe: "/x/huge", UName: "huge"}, // +300MB だが +1.5%
		{PID: 3, RSSKB: 100 * 1024, Exe: "/x/tiny", UName: "tiny"},              // ×100 だが +99MB
	}
	c := memGrowth(snapOf(first, second))
	if c.Status != StatusWarn || !strings.Contains(c.Value, "leaky") || strings.Contains(c.Value, "huge") || strings.Contains(c.Value, "tiny") {
		t.Fatalf("増え方 = 量と割合の両方: %+v", c)
	}
}

func TestParsePingAndWildcardListen(t *testing.T) {
	ok := "--- 1.1.1.1 ping statistics ---\n3 packets transmitted, 3 packets received, 0.0% packet loss\nround-trip min/avg/max/stddev = 5.773/5.835/5.906/0.055 ms\n"
	if loss, avg, found := parsePing(ok); !found || loss != 0 || avg != 5.835 {
		t.Errorf("parsePing = %v %v %v", loss, avg, found)
	}
	lost := "--- 10.0.0.1 ping statistics ---\n3 packets transmitted, 0 packets received, 100.0% packet loss\n"
	if loss, _, found := parsePing(lost); !found || loss != 100 {
		t.Errorf("全部失われた統計を読めない: %v %v", loss, found)
	}
	if _, _, found := parsePing("ping: cannot resolve x: Unknown host\n"); found {
		t.Errorf("統計の無い出力を読めたことにしない")
	}
	lsof := "COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME\n" +
		"node 1 koji 20u IPv6 0x1 0t0 TCP *:3000 (LISTEN)\n" +
		"node 1 koji 21u IPv6 0x1 0t0 TCP *:3000 (LISTEN)\n" +
		"mysqld 2 koji 20u IPv4 0x1 0t0 TCP 127.0.0.1:3306 (LISTEN)\n" +
		"redis-server 3 koji 7u IPv6 0x1 0t0 TCP [::1]:6379 (LISTEN)\n" +
		"x 4 koji 7u IPv6 0x1 0t0 TCP [::]:8080 (LISTEN)\n"
	got := parseWildcardListen(lsof)
	if strings.Join(got, ",") != "node:3000,x:8080" {
		t.Errorf("* と [::] だけを 1 度ずつ: %v", got)
	}
}

func TestBatteryThresholds(t *testing.T) {
	js := func(health, cap string, cycles int) string {
		return `{"SPPowerDataType":[{"_name":"spbattery_information","sppower_battery_health_info":{"sppower_battery_cycle_count":` +
			itoa(cycles) + `,"sppower_battery_health":"` + health + `","sppower_battery_health_maximum_capacity":"` + cap + `"}}]}`
	}
	for _, tc := range []struct {
		name string
		out  string
		want Status
	}{
		{"健康", js("Good", "100%", 134), StatusOK},
		{"容量が減った", js("Good", "79%", 300), StatusWarn},
		{"回数が多い", js("Good", "90%", 1000), StatusWarn},
		{"交換を勧められた", js("Service Recommended", "85%", 500), StatusWarn},
		{"デスクトップ", `{"SPPowerDataType":[{"_name":"sppower_ac_charger_information"}]}`, StatusInfo},
		{"ノートなのに健康の欄が無い", `{"SPPowerDataType":[{"_name":"spbattery_information","sppower_battery_new_key":{}}]}`, StatusUnknown},
		{"節が無い", `{"SPPowerDataType":[]}`, StatusUnknown},
		{"壊れた JSON", `{`, StatusUnknown},
	} {
		s := &scanner{o: Options{Run: fakeRun(t, map[string]fakeOut{"system_profiler": {out: tc.out}}), Now: func() time.Time { return now }}}
		if c := s.battery(context.Background())[0]; c.Status != tc.want {
			t.Errorf("%s: %s (%+v); want %s", tc.name, c.Status, c, tc.want)
		}
	}
}

// stubAppExists は .app・版のディレクトリが実在するかの判定をテストの間だけ差し替える。
func stubAppExists(t *testing.T, exists bool) {
	t.Helper()
	orig := appExists
	appExists = func(string) bool { return exists }
	t.Cleanup(func() { appExists = orig })
}

func TestChromeVersionMismatchWantsRestart(t *testing.T) {
	stubAppExists(t, true)
	h := func(ver, kind string) proc {
		return proc{PID: 2, PPID: 1, Exe: chromeApp + "/Contents/Frameworks/Google Chrome Framework.framework/Versions/" + ver + "/Helpers/Google Chrome Helper" + kind + ".app/Contents/MacOS/Google Chrome Helper" + kind}
	}
	main := proc{PID: 1, PPID: 1, Elapsed: 25 * 24 * time.Hour, Exe: chromeApp + "/Contents/MacOS/Google Chrome"}
	if c := chromeChecks([]proc{main, h("154.0.8037.98", " (Renderer)")}, "156.0.8078.81", "")[0]; c.Status != StatusWarn || c.Fix == "" {
		t.Errorf("版が違えば再起動を勧める: %+v", c)
	}
	if c := chromeChecks([]proc{main, h("156.0.8078.81", "")}, "156.0.8078.81", "")[0]; c.Status != StatusOK {
		t.Errorf("同じ版なら ok: %+v", c)
	}
	if c := chromeChecks(nil, "156.0.8078.81", "")[0]; c.Status != StatusInfo {
		t.Errorf("動いていなければ情報: %+v", c)
	}
	if c := chromeChecks([]proc{main}, "156.0.8078.81", "")[0]; c.Status != StatusUnknown {
		t.Errorf("動いている版が読めないのを ok にしない: %+v", c)
	}
	if c := chromeChecks([]proc{main, h("154.0.8037.98", "")}, "", "defaults が rc=1")[0]; c.Status != StatusUnknown {
		t.Errorf("入っている版が読めないのを ok にしない: %+v", c)
	}
}

func TestNvimOrphansAndLSP(t *testing.T) {
	// 実機の ps は comm に argv[0] (素の "nvim" / "node") を、ucomm に実行ファイルの名前を出す
	ps := []proc{
		{PID: 10, PPID: 5, TTY: "ttys001", Exe: "nvim", UName: "nvim"},
		{PID: 11, PPID: 10, TTY: "??", RSSKB: 1024, Exe: "nvim", UName: "nvim"}, // 画面の子の本体
		{PID: 12, PPID: 11, TTY: "??", RSSKB: 1024, Exe: "/Users/k/.local/share/nvim/mason/packages/gopls/gopls", UName: "gopls"},
		{PID: 13, PPID: 40, TTY: "??", Exe: "nvim", UName: "nvim"}, // Neovide が起こした本体 = 使用中
		{PID: 40, PPID: 1, TTY: "??", Exe: "/Applications/Neovide.app/Contents/MacOS/neovide", UName: "neovide"},
		{PID: 20, PPID: 1, TTY: "??", Exe: "nvim", UName: "nvim"},                            // 親が死んだ
		{PID: 30, PPID: 1, TTY: "??", Exe: "gopls", UName: "gopls"},                          // 親を失った言語サーバ
		{PID: 31, PPID: 1, TTY: "??", Exe: "lua-language-server", UName: "lua-language-ser"}, // ucomm は 16 文字で切れる
		{PID: 32, PPID: 1, TTY: "??", Exe: "node", UName: "node"},                            // node の言語サーバは見分けられない
	}
	c := nvimInstances(ps)
	if c.Status != StatusWarn || c.Fix == "" || !strings.HasPrefix(c.Fix, "kill 20 ") {
		t.Errorf("残骸は親が死んだ 20 だけ (Neovide の本体 13 や画面の子 11 は数えない): %+v", c)
	}
	if !strings.Contains(c.Value, "画面 1 個") || !strings.Contains(c.Value, "gopls ×1") {
		t.Errorf("画面の数と子の内訳: %q", c.Value)
	}
	if c := orphanLSP(ps); c.Status != StatusWarn || c.Fix != "kill 30 31" {
		t.Errorf("親を失った言語サーバは 30 と 31 (切れた ucomm も照合する): %+v", c)
	}
}

func TestSpoofedArgv0NeverReachesReport(t *testing.T) {
	secret := "/tmp/fake --token=SECRET123"
	spoof := proc{PID: 9, PPID: 1, CPU: 99, RSSKB: 9 << 20, Elapsed: time.Hour, Exe: secret, UName: "sleep"}
	grown := spoof
	grown.RSSKB += 1 << 20
	snap := snapOf([]proc{spoof}, []proc{grown})
	for _, c := range []Check{runaway(snap), memGrowth(snap), topRSS(snap.Second), appsTop(snap.Second), nvimInstances(snap.Second)} {
		if strings.Contains(c.Value+c.Note+c.Fix, "SECRET") {
			t.Errorf("argv[0] がレポートに出た: %+v", c)
		}
	}
	// /Applications で始まるように偽った argv[0] も、その .app が実在しなければ名前に使わない
	stubAppExists(t, false)
	fake := proc{PID: 8, RSSKB: 1 << 30, Exe: "/Applications/ghp_SECRET.app/Contents/MacOS/x", UName: "sleep"}
	if c := appsTop([]proc{fake}); strings.Contains(c.Value, "SECRET") {
		t.Errorf("実在しない .app の名前が出た: %+v", c)
	}
	real := proc{PID: 7, RSSKB: 1 << 30, Exe: "/Applications/Slack.app/Contents/MacOS/Slack", UName: "Slack"}
	dotdot := proc{PID: 6, RSSKB: 1 << 30, Exe: "/Applications/../Users/x/ghp_SECRET2.app/y", UName: "y"}
	stubAppExists(t, true)
	if c := appsTop([]proc{real, dotdot}); !strings.Contains(c.Value, "Slack") || strings.Contains(c.Value, "SECRET2") {
		t.Errorf("実在する .app は名前を出し、/Applications/.. で抜けたものは出さない: %+v", c)
	}
	stubAppExists(t, false)
	main := proc{PID: 1, PPID: 1, Exe: chromeApp + "/Contents/MacOS/Google Chrome"}
	dot := proc{PID: 3, Exe: chromeApp + "/Contents/Frameworks/Google Chrome Framework.framework/Versions/./x"}
	if c := chromeChecks([]proc{main, dot}, "156.0.8078.81", "")[0]; c.Status != StatusUnknown {
		t.Errorf("版の形でないものを版に数えた: %+v", c)
	}
	spoofVer := proc{PID: 2, Exe: chromeApp + "/Contents/Frameworks/Google Chrome Framework.framework/Versions/SECRET/x"}
	if c := chromeChecks([]proc{main, spoofVer}, "156.0", "")[0]; strings.Contains(c.Value, "SECRET") || c.Status != StatusUnknown {
		t.Errorf("実在しない版のディレクトリを動いている版に数えた: %+v", c)
	}
	if got := parseUcomm(" 9 sleep\n 1212 Google Chrome He  \n"); got[9] != "sleep" || got[1212] != "Google Chrome He" {
		t.Errorf("parseUcomm: %q", got)
	}
}

func TestTmuxPanesAndServers(t *testing.T) {
	c := tmuxPanes("100\t30000\t0\n200\t30000\t1\n")
	if c.Status != StatusWarn || !strings.Contains(c.Value, "2 個 / スクロールバック計 300 行") || !strings.Contains(c.Value, "残った pane 1 個") {
		t.Errorf("tmuxPanes: %+v", c)
	}
	big := []proc{{PID: 61035, PPID: 1, RSSKB: 2 * 1024 * 1024, Exe: "tmux", UName: "tmux"}, {PID: 3, PPID: 2, Exe: "tmux", UName: "tmux"}}
	if c := tmuxServers(big); c.Status != StatusWarn || !strings.Contains(c.Value, "1 個") {
		t.Errorf("attach しているクライアントはサーバに数えず、大きいサーバは注意: %+v", c)
	}
}

// touch は DiagnosticReports の fixture を作り、更新時刻を ago だけ前にする。
func touch(t *testing.T, dir, name string, ago time.Duration) {
	t.Helper()
	touchWith(t, dir, name, "{}", ago)
}

func touchWith(t *testing.T, dir, name, body string, ago time.Duration) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now.Add(-ago), now.Add(-ago)); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticReports(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "panic-full-2026-11-01-000000.panic", 5*24*time.Hour)
	touch(t, dir, ".contents.panic", 5*24*time.Hour)                     // 同じパニックの写し
	touch(t, dir, "panic-full-2026-09-01-000000.panic", 60*24*time.Hour) // 30 日より前
	for i := range 3 {
		touch(t, dir, "Foo-2026-11-0"+itoa(i+1)+"-000000.ips", time.Duration(i+1)*time.Hour)
	}
	touch(t, dir, "Bar-2026-11-01-000000.ips", time.Hour)
	touch(t, dir, "ExcUserFault_Google Chrome-2026-11-05-000000.ips", time.Hour)
	touchWith(t, dir, "spotlightknowledged_2026-11-05-000000_host.cpu_resource.diag", "Date/Time: x\nCommand:          spotlightknowledged\nEvent:            cpu usage\n", time.Hour)
	// macOS 27 は書き込みの超過を素の .diag の名前で書く (種類は中身の Event の行にしか無い)
	touchWith(t, dir, "Claude Helper_2026-11-02-133001_host.diag", "Command:          Claude Helper\nEvent:            disk writes\n", time.Hour)
	s := &scanner{o: Options{DiagDirs: []string{dir}, Now: func() time.Time { return now }}}

	if c := s.panics(); c.Status != StatusBad || c.Value != "1 回" {
		t.Errorf("panics: %+v", c)
	}
	if c := s.crashes(); c.Status != StatusWarn || c.Value != "Foo ×3" {
		t.Errorf("crashes (3 回以上だけ・ExcUserFault は別に数える): %+v", c)
	}
	if c := s.overwrittenApps(); c.Status != StatusWarn || c.Value != "Google Chrome ×1" {
		t.Errorf("overwrittenApps: %+v", c)
	}
	if c := s.resourceDiag(); c.Status != StatusWarn || c.Value != "Claude Helper (disk writes) ×1 / spotlightknowledged (cpu usage) ×1" || strings.Contains(c.Value, "host") {
		t.Errorf("resourceDiag (マシン名を出さない): %+v", c)
	}
	none := &scanner{o: Options{DiagDirs: []string{filepath.Join(dir, "無い")}, Now: func() time.Time { return now }}}
	for _, c := range []Check{none.panics(), none.crashes(), none.overwrittenApps(), none.resourceDiag()} {
		if c.Status != StatusUnknown {
			t.Errorf("読めない置き場を「なし」にしない: %+v", c)
		}
	}
}

// rcOneEmpty は全部のコマンドが rc=1 で何も出さない (lsof の「無い」・route の「経路無し」と同じ形)。
func rcOneEmpty(context.Context, string, ...string) (string, string, int, error) {
	return "", "", 1, nil
}

// 外部コマンドが全部失敗する環境で、どの項目も ok にならない (判定不能を ok に丸めない)。
// 失敗の形は 2 つ: 起動できない (err) と、起動したが rc=1 で空。
func TestFailingCommandsNeverYieldOK(t *testing.T) {
	for name, run := range map[string]func(context.Context, string, ...string) (string, string, int, error){
		"起動できない":  fakeRun(t, nil),
		"rc=1 で空": rcOneEmpty,
	} {
		t.Run(name, func(t *testing.T) { assertNoOK(t, run) })
	}
}

func assertNoOK(t *testing.T, run func(context.Context, string, ...string) (string, string, int, error)) {
	t.Helper()
	home := t.TempDir()
	rep := Scan(context.Background(), Options{
		Run:      run,
		Now:      func() time.Time { return now },
		Sleep:    func(context.Context, time.Duration) error { return nil },
		DiagDirs: []string{filepath.Join(home, "無い")},
		Home:     home,
	})
	if len(rep.Checks) < 20 {
		t.Fatalf("項目が少なすぎる (観点が回っていない): %d", len(rep.Checks))
	}
	for _, c := range rep.Checks {
		if c.Status == StatusOK {
			t.Errorf("コマンドが失敗したのに ok: %+v", c)
		}
	}
	if rep.Counts()[StatusUnknown] == 0 {
		t.Errorf("判定不能が 1 件も無い")
	}
}

func TestScanHealthyMachine(t *testing.T) {
	home := t.TempDir()
	diag := t.TempDir()
	ps := psLine(1, 0, "Ss", 0.5, 20000, "25-22:24:23", "/sbin/launchd") + "\n"
	run := fakeRun(t, map[string]fakeOut{
		"ps":              {out: ps},
		"memory_pressure": {out: "x\nSystem-wide memory free percentage: 71%\n"},
		"sysctl -n":       {out: "total = 7168.00M  used = 0.00M  free = 7168.00M  (encrypted)\n"},
		"zprint":          {out: "data.kalloc.1024            1024          0K          0K          0           0       15733     0K      0\n"},
	})
	rep := Scan(context.Background(), Options{
		Run: run, Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) error { return nil },
		Categories: []Category{CatProc}, DiagDirs: []string{diag}, Home: home,
	})
	for _, id := range []string{"proc.runaway", "proc.zombie", "proc.memgrowth", "proc.pressure", "proc.kalloc", "proc.resource"} {
		if c := findCheck(t, rep.Checks, id); c.Status != StatusOK {
			t.Errorf("%s: %+v", id, c)
		}
	}
	for _, c := range rep.Checks {
		if c.Category != CatProc {
			t.Errorf("選んでいない観点が混ざった: %+v", c)
		}
	}
}

func TestMemPressureAndKallocThresholds(t *testing.T) {
	for _, tc := range []struct {
		pct  string
		want Status
	}{{"71", StatusOK}, {"24", StatusWarn}, {"9", StatusBad}} {
		s := &scanner{o: Options{Run: fakeRun(t, map[string]fakeOut{"memory_pressure": {out: "System-wide memory free percentage: " + tc.pct + "%\n"}})}}
		if c := s.memPressure(context.Background()); c.Status != tc.want {
			t.Errorf("空き %s%%: %s; want %s", tc.pct, c.Status, tc.want)
		}
	}
	s := &scanner{o: Options{Run: fakeRun(t, map[string]fakeOut{"memory_pressure": {out: "no such line\n"}})}}
	if c := s.memPressure(context.Background()); c.Status != StatusUnknown {
		t.Errorf("行が無いのを ok にしない: %+v", c)
	}
	k := &scanner{o: Options{Run: fakeRun(t, map[string]fakeOut{"zprint": {out: "data.kalloc.1024  1024  0K  0K  0  0  16000000  0K  0\n"}})}}
	if c := k.kalloc(context.Background()); c.Status != StatusBad {
		t.Errorf("1,500 万個以上は異常: %+v", c)
	}
}

func TestParseCategories(t *testing.T) {
	if cs, err := ParseCategories(""); err != nil || len(cs) != len(AllCategories) {
		t.Errorf("空なら全部: %v %v", cs, err)
	}
	if cs, err := ParseCategories("net, net,tmux"); err != nil || len(cs) != 2 {
		t.Errorf("重複を落とす: %v %v", cs, err)
	}
	if _, err := ParseCategories("net,bogus"); err == nil {
		t.Errorf("知らない観点を通した")
	}
}

func TestFormatEscapesPipeAndSanitizes(t *testing.T) {
	rep := SanitizeForDisplay(Report{ScannedAt: now, Checks: []Check{{Category: CatNet, ID: "x", Label: "a|b", Value: "v\x1b[31mred", Status: StatusWarn}}})
	out := Format(rep)
	if !strings.Contains(out, `a\|b`) || strings.Contains(out, "\x1b") || !strings.Contains(out, "| 注意 |") {
		t.Errorf("Format:\n%s", out)
	}
}

func TestNetFallbacks(t *testing.T) {
	gw := Check{ID: "net.gateway", Status: StatusBad, Fix: "x"}
	got := gatewayIgnoresICMP([]Check{gw}, Check{Status: StatusOK})
	if got[0].Status != StatusInfo || got[0].Fix != "" {
		t.Errorf("外へ届くならゲートウェイの無応答は情報: %+v", got[0])
	}
	if got := gatewayIgnoresICMP([]Check{gw}, Check{Status: StatusBad}); got[0].Status != StatusBad {
		t.Errorf("外にも届かないなら異常のまま: %+v", got[0])
	}
	s := &scanner{o: Options{Run: rcOneEmpty}}
	if c := s.listen(context.Background()); c.Status != StatusInfo || !strings.Contains(c.Note, "自分のユーザーのプロセスだけ") {
		t.Errorf("見つからなくても ok にしない (他のユーザーの待ち受けは見えない): %+v", c)
	}
	for out, want := range map[string]Status{
		`{"SPAirPortDataType":[]}`:                                                   StatusUnknown,
		`{"SPAirPortDataType":[{"spairport_airport_interfaces":[]}]}`:                StatusInfo,
		`{"SPAirPortDataType":[{"spairport_airport_interfaces":[{"_name":"en0"}]}]}`: StatusInfo,
		`{"SPAirPortDataType":[{"spairport_airport_interfaces":[{"spairport_current_network_information":{"spairport_signal_noise":"-80 dBm / -90 dBm"}}]}]}`: StatusWarn,
	} {
		w := &scanner{o: Options{Run: fakeRun(t, map[string]fakeOut{"system_profiler": {out: out}})}}
		if c := w.wifi(context.Background()); c.Status != want {
			t.Errorf("wifi %s: %+v; want %s", out, c, want)
		}
	}
}

func TestTmuxDeadSocketIsNotRunning(t *testing.T) {
	s := &scanner{o: Options{
		Run:   fakeRun(t, map[string]fakeOut{"ps": {out: psLine(1, 0, "Ss", 0, 1, "01:00", "/sbin/launchd") + "\n"}, "tmux": {rc: 1, err: "error connecting to /private/tmp/tmux-501/default (No such file or directory)\n"}}),
		Sleep: func(context.Context, time.Duration) error { return nil },
	}}
	if c := findCheck(t, s.tmux(context.Background()), "tmux.panes"); c.Status != StatusInfo || c.Value != "サーバが動いていない" {
		t.Errorf("socket が無いのはサーバが無いのと同じ: %+v", c)
	}
	denied := &scanner{o: Options{
		Run:   fakeRun(t, map[string]fakeOut{"ps": {out: psLine(1, 0, "Ss", 0, 1, "01:00", "/sbin/launchd") + "\n"}, "tmux": {rc: 1, err: "error connecting to /private/tmp/tmux-501/default (Permission denied)\n"}}),
		Sleep: func(context.Context, time.Duration) error { return nil },
	}}
	if c := findCheck(t, denied.tmux(context.Background()), "tmux.panes"); c.Status != StatusUnknown {
		t.Errorf("権限で拒まれたのを「動いていない」に丸めた: %+v", c)
	}
}

func TestNvimSwapSkipsOpenAndHidesHome(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local/state/nvim/swap")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	touch(t, dir, "%a%b.swp", 30*24*time.Hour) // 開かれている (動いている nvim の)
	touch(t, dir, "%a%c.swp", 30*24*time.Hour) // 落ちたセッションの残り
	touch(t, dir, "%a%d.swp", time.Hour)       // 新しい
	// lsof は symlink を解いた実体のパスを返す (渡した dir と文字列が一致しない)
	lsofOut := "p123\nfcwd\nn/private/real/state/nvim/swap/%a%b.swp\n"
	s := &scanner{o: Options{Home: home, Now: func() time.Time { return now }, Run: fakeRun(t, map[string]fakeOut{"lsof": {out: lsofOut}})}}
	c := s.nvimSwap(context.Background())
	if !strings.Contains(c.Value, "開かれていないもの 1 個") || c.Fix == "" {
		t.Errorf("開いている swap は数えない: %+v", c)
	}
	if strings.Contains(c.Value+c.Note+c.Fix, home) || !strings.Contains(c.Fix, "~/.local/state/nvim/swap") {
		t.Errorf("ホームのパスを出さない: %+v", c)
	}
	// lsof は見つけて出力しても rc=1 を返す (実測)
	rcOne := &scanner{o: Options{Home: home, Now: func() time.Time { return now }, Run: fakeRun(t, map[string]fakeOut{"lsof": {out: lsofOut, rc: 1}})}}
	if c := rcOne.nvimSwap(context.Background()); !strings.Contains(c.Value, "開かれていないもの 1 個") {
		t.Errorf("rc=1 で出力のある lsof を失敗と読んだ: %+v", c)
	}
	// 0 行は「開いていない」と「見えていない」を区別できない (-w で警告を消した lsof がこの形を返す)
	empty := &scanner{o: Options{Home: home, Now: func() time.Time { return now }, Run: fakeRun(t, map[string]fakeOut{"lsof": {rc: 1}})}}
	if c := empty.nvimSwap(context.Background()); c.Fix != "" {
		t.Errorf("lsof が 0 行なのに移す案内を出した: %+v", c)
	}
	failing := &scanner{o: Options{Home: home, Now: func() time.Time { return now }, Run: fakeRun(t, nil)}}
	if c := failing.nvimSwap(context.Background()); c.Fix != "" {
		t.Errorf("開いているかを確かめられないなら移す案内を出さない: %+v", c)
	}
}

// ゲートウェイが ICMP に応答しないルーターでも、外へ届いていれば net の観点は異常を出さない (net() からの配線ごと見る)。
func TestNetGatewayWithoutICMP(t *testing.T) {
	run := func(_ context.Context, name string, args ...string) (string, string, int, error) {
		switch filepath.Base(name) {
		case "route":
			return "   gateway: 192.168.1.1\n  interface: en0\n", "", 0, nil
		case "ping":
			if args[len(args)-1] == "192.168.1.1" {
				return "3 packets transmitted, 0 packets received, 100.0% packet loss\n", "", 2, nil
			}
			return "3 packets transmitted, 3 packets received, 0.0% packet loss\nround-trip min/avg/max/stddev = 9/10/11/1 ms\n", "", 0, nil
		}
		return "", "", -1, errors.New("fake: 登録なし")
	}
	s := &scanner{o: Options{Run: run, Now: func() time.Time { return now }}}
	if c := findCheck(t, s.net(context.Background()), "net.gateway"); c.Status != StatusInfo {
		t.Errorf("外へ届くならゲートウェイの無応答は情報: %+v", c)
	}
}

func TestCmdNoteHidesHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("home が無い")
	}
	for _, stderr := range []string{"can't opendir(" + home + ")", "can't stat(" + home + "/x)", "x " + home} {
		if got := cmdNote("lsof", 1, nil, stderr); strings.Contains(got, home) {
			t.Errorf("home が残った: %q", got)
		}
	}
	if got := cmdNote("lsof", 1, nil, "x "+home+"X/y"); !strings.Contains(got, home+"X") {
		t.Errorf("別のユーザーの home まで置き換えた: %q", got)
	}
}

// lsof に -w を渡さない: 権限で読めなかった警告が消え、見えなかったのを「無い」と読む (実機で rc=1・stderr 空・0 行になる)。
func TestLsofNeverSilencesWarnings(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, name string, args ...string) (string, string, int, error) {
		if filepath.Base(name) == "lsof" {
			calls = append(calls, args)
		}
		return "", "", 1, nil
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local/state/nvim/swap"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &scanner{o: Options{Run: run, Home: home, Now: func() time.Time { return now }}}
	s.listen(context.Background())
	s.nvimSwap(context.Background())
	if len(calls) != 2 {
		t.Fatalf("lsof の呼び出しが %d 回 (2 回のはず)", len(calls))
	}
	for _, a := range calls {
		if slices.Contains(a, "-w") {
			t.Errorf("lsof に -w を渡した: %v", a)
		}
	}
}
