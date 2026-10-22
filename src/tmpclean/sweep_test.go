package tmpclean

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// sweepFixture は sweep のテストの fixture。作成時刻 (birthtime) は過去へずらせないので、「今」を実時刻にし、
// 古さ (mtime) だけを実時刻から days 日前にずらす。Swift のツールの起動時刻は etime で dir の作成の前後に置く。
func sweepFixture(t *testing.T) (*fixture, time.Time) {
	t.Helper()
	f := newFixture(t)
	now := time.Now().Add(time.Minute) // 作成の直後でも etime の 1 秒の見積もりより後になるように
	f.d.now = func() time.Time { return now }
	return f, now
}

// tdir は tmpdir の直下に name の dir を作り、keep が非 nil ならその中身で .keep-directory を置き、全部の mtime を days 日前にする。
func tdir(t *testing.T, f *fixture, now time.Time, name string, keep *string, days int) string {
	t.Helper()
	p := filepath.Join(f.tmpdir, name)
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if keep != nil {
		if err := os.WriteFile(filepath.Join(p, keepFile), []byte(*keep), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	setAge(t, p, now, days)
	return p
}

func setAge(t *testing.T, p string, now time.Time, days int) {
	t.Helper()
	ts := now.Add(-time.Duration(days) * 24 * time.Hour)
	_ = filepath.Walk(p, func(q string, _ os.FileInfo, err error) error {
		if err == nil && q != p {
			_ = os.Chtimes(q, ts, ts)
		}
		return nil
	})
	if err := os.Chtimes(p, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func strp(s string) *string { return &s }

func TestSweepRemovesOnlyEmptySwiftTempDirs(t *testing.T) {
	f, now := sweepFixture(t)
	empty := tdir(t, f, now, "TemporaryDirectory.aaaaaa", nil, 30)
	keepOnly := tdir(t, f, now, "TemporaryDirectory.bbbbbb", strp(""), 30)
	keepFull := tdir(t, f, now, "TemporaryDirectory.cccccc", strp("x"), 30)
	withFile := tdir(t, f, now, "TemporaryDirectory.dddddd", strp(""), 30)
	if err := os.WriteFile(filepath.Join(withFile, "inputs-1.LinkFileList"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	setAge(t, withFile, now, 30)
	fresh := tdir(t, f, now, "TemporaryDirectory.eeeeee", strp(""), 1)
	opened := tdir(t, f, now, "TemporaryDirectory.ffffff", strp(""), 30)
	f.sys.open = []string{filepath.Join(opened, keepFile)}
	fifo := tdir(t, f, now, "TemporaryDirectory.gggggg", nil, 30)
	if err := syscall.Mkfifo(filepath.Join(fifo, "s"), 0o600); err != nil {
		t.Fatal(err)
	}
	setAge(t, fifo, now, 30)
	otherName := tdir(t, f, now, "TemporaryDirectory.hhhhhhh", nil, 30) // 7 字
	otherPrefix := tdir(t, f, now, "tmp.aaaaaa", nil, 30)
	if err := os.Symlink(f.trash, filepath.Join(f.tmpdir, "TemporaryDirectory.iiiiii")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.tmpdir, "TemporaryDirectory.jjjjjj"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	rc, out, errOut := f.runCLI("sweep")
	if rc != exitOK {
		t.Fatalf("rc=%d\n%s\n%s", rc, out, errOut)
	}
	for _, p := range []string{empty, keepOnly} {
		if exists(p) {
			t.Errorf("消えていない: %s", p)
		}
	}
	for _, p := range []string{keepFull, withFile, fresh, opened, fifo, otherName, otherPrefix,
		filepath.Join(f.tmpdir, "TemporaryDirectory.iiiiii"), filepath.Join(f.tmpdir, "TemporaryDirectory.jjjjjj")} {
		if !exists(p) {
			t.Errorf("消えた: %s", p)
		}
	}
	if !exists(filepath.Join(keepFull, keepFile)) || !exists(filepath.Join(withFile, "inputs-1.LinkFileList")) {
		t.Error("中身が消えた")
	}
	for _, want := range []string{"removed: 2 / failed: 0", "kept (中身がある): 2", "kept (新しい): 1", "kept (開かれている): 1",
		"kept (socket / FIFO を含む): 1", "kept (フォルダでない): 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い\n%s", want, out)
		}
	}
	if len(f.sys.calls) == 0 || entriesIn(t, f.trash) != 0 {
		t.Error("ゴミ箱に何かを置いた")
	}
}

// 動いている Swift のツールの起動より後に作られたものは、古くても消さない (そのツールが後で使いうる)。起動より前に作られたものは消す。
func TestSweepKeepsDirsBornAfterOldestSwiftTool(t *testing.T) {
	for _, c := range []struct {
		name    string
		etime   string
		removed bool
	}{
		{"tool started before the dir", "40-00:00:00", false}, // tdir の mtime (30 日前) が作成時刻も 30 日前へ引き戻す (macOS)
		{"tool started after the dir", "00:01", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, now := sweepFixture(t)
			p := tdir(t, f, now, "TemporaryDirectory.aaaaaa", strp(""), 30)
			f.sys.procs = append(f.sys.procs,
				fproc{pid: 600, ppid: 1, command: "/Applications/Xcode.app/Contents/Developer/usr/bin/sourcekit-lsp", etime: c.etime},
				fproc{pid: 601, ppid: 1, command: "/usr/bin/swift build", etime: "00:01"})
			rc, out, errOut := f.runCLI("sweep")
			if rc != exitOK {
				t.Fatalf("rc=%d\n%s\n%s", rc, out, errOut)
			}
			if exists(p) == c.removed {
				t.Errorf("removed=%v を期待\n%s", c.removed, out)
			}
			if !strings.Contains(out, "limit: pid 600") {
				t.Errorf("一番古いツールが limit に出ない\n%s", out)
			}
			if !c.removed && !strings.Contains(out, "kept (動いている Swift のツールの起動より後に作られた): 1") {
				t.Errorf("残した理由が出ない\n%s", out)
			}
		})
	}
}

// 何日も動く dev ループの make は、Xcode.app の中にあっても範囲を決めない。範囲はその次に古い Swift のツールで決まる。
func TestSweepIgnoresLongRunningMake(t *testing.T) {
	f, now := sweepFixture(t)
	p := tdir(t, f, now, "TemporaryDirectory.aaaaaa", strp(""), 30)
	f.sys.procs = append(f.sys.procs,
		fproc{pid: 600, ppid: 1, command: "/Applications/Xcode.app/Contents/Developer/usr/bin/make dev-fg-loop", etime: "40-00:00:00"},
		fproc{pid: 601, ppid: 1, command: "/usr/bin/swift build", etime: "00:01"})
	rc, out, errOut := f.runCLI("sweep")
	if rc != exitOK {
		t.Fatalf("rc=%d\n%s\n%s", rc, out, errOut)
	}
	if exists(p) {
		t.Errorf("make の起動より後に作られたものが残った\n%s", out)
	}
	if !strings.Contains(out, "limit: pid 601") {
		t.Errorf("範囲を決めたのが swift build でない\n%s", out)
	}
}

// ps・lsof が読めないときは何も消さずに止める (読めないのを「居ない」「開かれていない」と読まない)。
func TestSweepStopsWhenUndecidable(t *testing.T) {
	for _, c := range []struct {
		name   string
		break_ func(f *fixture)
	}{
		{"ps fails", func(f *fixture) { f.sys.psEtime = func() (string, int) { return "", 1 } }},
		{"ps empty", func(f *fixture) { f.sys.psEtime = func() (string, int) { return "", 0 } }},
		{"ps etime garbage", func(f *fixture) { f.sys.psEtime = func() (string, int) { return "  1 ??? /sbin/launchd\n", 0 } }},
		{"lsof short", func(f *fixture) { f.sys.lsofAll = func() (string, int) { return "p1\nn/x\n", 0 } }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, now := sweepFixture(t)
			p := tdir(t, f, now, "TemporaryDirectory.aaaaaa", strp(""), 30)
			c.break_(f)
			rc, out, errOut := f.runCLI("sweep")
			if rc != exitStopped || !exists(p) {
				t.Fatalf("rc=%d exists=%v\n%s\n%s", rc, exists(p), out, errOut)
			}
		})
	}
}

func TestSweepRejectsBadDays(t *testing.T) {
	f, now := sweepFixture(t)
	p := tdir(t, f, now, "TemporaryDirectory.aaaaaa", nil, 30)
	if rc, _, _ := f.runCLI("sweep", "-days", "0"); rc != exitStopped || !exists(p) {
		t.Fatalf("rc=%d exists=%v", rc, exists(p))
	}
}

func TestSweepGuard(t *testing.T) {
	root := "/private/var/folders/xx/yy/T"
	g := sweepGuard(root, func(string, string) error { return nil })
	ok := [][2]string{
		{"rmdir", root + "/TemporaryDirectory.aB3dE9"},
		{"unlink", root + "/TemporaryDirectory.aB3dE9/.keep-directory"},
	}
	ng := [][2]string{
		{"rmdir", root},
		{"rmdir", root + "/tmp.aB3dE9"},
		{"rmdir", root + "/TemporaryDirectory.aB3dE9/x"},
		{"rmdir", "/private/var/folders/xx/yy/TemporaryDirectory.aB3dE9"},
		{"unlink", root + "/TemporaryDirectory.aB3dE9/main.swift"},
		{"unlink", root + "/tmp.aB3dE9/.keep-directory"},
		{"unlink", root + "/.keep-directory"},
		{"trash", root + "/TemporaryDirectory.aB3dE9"},
		{"rmdir", root + "/TemporaryDirectory.aB3dE9/../tmp.x"},
	}
	for _, c := range ok {
		if err := g(c[0], c[1]); err != nil {
			t.Errorf("%v が断られた: %v", c, err)
		}
	}
	for _, c := range ng {
		if err := g(c[0], c[1]); err == nil {
			t.Errorf("%v が通った", c)
		}
	}
	if err := sweepGuard(root, nil)("rmdir", root+"/TemporaryDirectory.aB3dE9"); err == nil {
		t.Error("inner が nil で通った")
	}
}

func TestParseEtime(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"00:01":       time.Second,
		"59:59":       59*time.Minute + 59*time.Second,
		"01:00:00":    time.Hour,
		"34-09:36:08": 34*24*time.Hour + 9*time.Hour + 36*time.Minute + 8*time.Second,
	} {
		if got, ok := parseEtime(in); !ok || got != want {
			t.Errorf("parseEtime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "1", "a:b", "1:2:3:4", "-01:00", "1--01:00", "01:-1", "01:+1", "1-", ":"} {
		if _, ok := parseEtime(in); ok {
			t.Errorf("parseEtime(%q) が通った", in)
		}
	}
}

func TestSwiftToolCommand(t *testing.T) {
	yes := []string{
		"/Applications/Xcode.app/Contents/MacOS/Xcode",
		"/Applications/Xcode-beta.app/Contents/Developer/usr/bin/xcodebuild -scheme X test",
		"/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin/sourcekit-lsp",
		"/Applications/Xcode.app/Contents/SharedFrameworks/SwiftBuild.framework/Versions/A/PlugIns/SWBBuildService.bundle/Contents/MacOS/SWBBuildService",
		"/usr/bin/swift build",
		"swift-frontend -frontend -c x.swift",
		"/Library/Developer/CommandLineTools/usr/bin/swift-package resolve",
		"/Users/me/.swiftly/toolchains/x/usr/bin/swiftc -c a.swift",
		"/Library/Developer/Toolchains/swift-6.xctoolchain/usr/bin/sourcekit-lsp",
		"sourcekit-lsp",
		"/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild -project /src/git -scheme make",
		"/Applications/Xcode 16.app/Contents/Developer/usr/bin/make dev-fg-loop",
	}
	no := []string{
		"/Applications/Xcode.app/Contents/Developer/usr/bin/make dev-fg-loop",
		"/Applications/Xcode-beta.app/Contents/Developer/usr/bin/gnumake -C x",
		"/Library/Developer/CommandLineTools/usr/bin/git fetch",
		"/Library/Developer/CommandLineTools/usr/bin/git-upload-pack /x",
		"make -C /Library/Developer/Toolchains/x",
		"/Users/me/.local/bin/claude",
		"/bin/zsh -c tmpclean",
		"/Applications/SwiftBar.app/Contents/MacOS/SwiftBar",
		"/usr/bin/grep swiftly",
		"/sbin/launchd",
	}
	for _, c := range yes {
		if !isSwiftTool(c) {
			t.Errorf("拾わない: %s", c)
		}
	}
	for _, c := range no {
		if isSwiftTool(c) {
			t.Errorf("拾った: %s", c)
		}
	}
}

func entriesIn(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

// 一覧を読めないのを「消すものが無い」(rc=0) にしない。
func TestSweepStopsWhenTmpdirUnreadable(t *testing.T) {
	f, now := sweepFixture(t)
	p := tdir(t, f, now, "TemporaryDirectory.aaaaaa", nil, 30)
	if err := os.Chmod(f.tmpdir, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.tmpdir, 0o700) })
	rc, out, errOut := f.runCLI("sweep")
	if rc != exitStopped || !strings.Contains(errOut, "一覧を読めない") {
		t.Fatalf("rc=%d\n%s\n%s", rc, out, errOut)
	}
	_ = os.Chmod(f.tmpdir, 0o700)
	if !exists(p) {
		t.Error("消えた")
	}
}

// 作成時刻が 0 (mtime を 1970 年にすると macOS は作成時刻も引き戻す) のものを、「どの起動よりも前」と読んで消さない。
func TestSweepKeepsZeroBirthtime(t *testing.T) {
	f, now := sweepFixture(t)
	p := tdir(t, f, now, "TemporaryDirectory.aaaaaa", nil, 30)
	if err := os.Chtimes(p, time.Unix(0, 0), time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	f.sys.procs = append(f.sys.procs, fproc{pid: 600, ppid: 1, command: "/usr/bin/sourcekit-lsp", etime: "40-00:00:00"})
	rc, out, errOut := f.runCLI("sweep")
	if rc != exitOK || !exists(p) || !strings.Contains(out, "kept (作成時刻を読めない): 1") {
		t.Fatalf("rc=%d exists=%v\n%s\n%s", rc, exists(p), out, errOut)
	}
}

// 消せない属性 (uchg など) の付いたものは、失敗 (rc=1) にせず残す。
func TestSweepKeepsUnmovable(t *testing.T) {
	f, now := sweepFixture(t)
	p := tdir(t, f, now, "TemporaryDirectory.aaaaaa", nil, 30)
	if err := unix.Chflags(p, ufImmutable); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Chflags(p, 0) })
	rc, out, errOut := f.runCLI("sweep")
	if rc != exitOK || !exists(p) || !strings.Contains(out, "kept (消せない属性 (immutable / SIP)): 1") {
		t.Fatalf("rc=%d exists=%v\n%s\n%s", rc, exists(p), out, errOut)
	}
}
