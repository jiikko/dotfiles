package tmpclean

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"safefs/sandbox"
)

// sb はこのパッケージのテスト全体の検査点。根とゴミ箱の使い捨て dir だけを Allow し、TestMain の最後に違反を見る
// (違反 = テストが根の外を動かそうとした = ハーネスか実装が壊れている)。
var sb sandbox.Sandbox

func TestMain(m *testing.M) {
	code := m.Run()
	if v := sb.Violations(); len(v) > 0 {
		fmt.Fprintf(os.Stderr, "sandbox が拒否した操作があった:\n  %s\n", strings.Join(v, "\n  "))
		code = 1
	}
	os.Exit(code)
}

var testNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

const (
	selfSID  = "aaaaaaaa-0000-0000-0000-000000000001"
	deadSID  = "bbbbbbbb-0000-0000-0000-000000000002"
	deadSID2 = "bbbbbbbb-0000-0000-0000-000000000003"
	selfPID  = 300
)

// fproc は fake のプロセス表の 1 行。
type fproc struct {
	pid, ppid int
	command   string // ps -o command (argv)
	ucomm     string
	env       string // ps eww が command の後ろに出す環境変数
	cwd       string // "" = lsof で cwd が取れない
	pgrep     bool   // pgrep -a -x claude に当たる (argv[0] の名前が claude)
	etime     string // ps -o etime ("" = 01:00)
}

// fakeSys は ps / pgrep / lsof の fake。テストは procs・open・openNow を書き換えて状況を作る。
type fakeSys struct {
	procs   []fproc
	open    []string             // 全体の lsof に出すパス (100 件に満たない分は無関係なパスで埋める)
	openNow map[string][3]any    // 1 件の lsof の応答 {stdout, stderr, rc}。無ければ「該当なし」(rc=1・空)
	lsofAll func() (string, int) // 全体の lsof を差し替える (失敗を作る)
	psEtime func() (string, int) // ps -Ao pid=,etime=,command= を差し替える (失敗を作る)
	onPS    func(n int)          // ps -Ao pid=,ppid=,command= が呼ばれた回数 (1 始まり) を受ける。途中で状況を変える
	nPS     int
	calls   []string
}

func (f *fakeSys) run(_ context.Context, name string, args ...string) (string, string, int, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	var b strings.Builder
	switch {
	case call == "ps -Ao pid=,ppid=,command=":
		f.nPS++
		if f.onPS != nil {
			f.onPS(f.nPS)
		}
		for _, p := range f.procs {
			fmt.Fprintf(&b, "%5d %5d %s\n", p.pid, p.ppid, p.command)
		}
		return b.String(), "", 0, nil
	case call == "ps -Ao pid=,etime=,command=":
		if f.psEtime != nil {
			out, rc := f.psEtime()
			return out, "", rc, nil
		}
		for _, p := range f.procs {
			et := p.etime
			if et == "" {
				et = "01:00"
			}
			fmt.Fprintf(&b, "%5d %11s %s\n", p.pid, et, p.command)
		}
		return b.String(), "", 0, nil
	case call == "ps -Ao pid=,comm=":
		for _, p := range f.procs {
			fmt.Fprintf(&b, "%5d %s\n", p.pid, strings.Fields(p.command + " x")[0])
		}
		return b.String(), "", 0, nil
	case call == "ps -Ao pid=,ucomm=":
		for _, p := range f.procs {
			fmt.Fprintf(&b, "%5d %s\n", p.pid, p.ucomm)
		}
		return b.String(), "", 0, nil
	case call == "pgrep -a -x claude":
		for _, p := range f.procs {
			if p.pgrep {
				fmt.Fprintf(&b, "%d\n", p.pid)
			}
		}
		if b.Len() == 0 {
			return "", "", 1, nil
		}
		return b.String(), "", 0, nil
	case call == "ps eww -Ao pid=,command=":
		for _, p := range f.procs {
			fmt.Fprintf(&b, "%5d %s %s\n", p.pid, p.command, p.env)
		}
		return b.String(), "", 0, nil
	case strings.HasPrefix(call, "lsof -a -d cwd -p "):
		pid, _ := strconv.Atoi(args[4])
		for _, p := range f.procs {
			if p.pid == pid && p.cwd != "" {
				return fmt.Sprintf("p%d\nfcwd\nn%s\n", pid, p.cwd), "", 0, nil
			}
		}
		return "", "", 1, nil
	case strings.HasPrefix(call, "ps -p "):
		pid, _ := strconv.Atoi(args[1])
		for _, p := range f.procs {
			if p.pid == pid {
				return fmt.Sprintf("%d\n", pid), "", 0, nil
			}
		}
		return "", "", 1, nil
	case call == "lsof +c 0 -b -w -Fn":
		if f.lsofAll != nil {
			out, rc := f.lsofAll()
			return out, "", rc, nil
		}
		for _, p := range f.open {
			fmt.Fprintf(&b, "p1\nn%s\n", p)
		}
		for i := range 120 {
			fmt.Fprintf(&b, "p1\nn/usr/lib/dummy%d.dylib\n", i)
		}
		return b.String(), "", 0, nil
	case strings.HasPrefix(call, "lsof -w -Fn "): // 本物は +D で開いているものを見つけても rc=1 で stdout に出す
		path := args[len(args)-1]
		if r, ok := f.openNow[path]; ok {
			return r[0].(string), r[1].(string), r[2].(int), nil
		}
		return "", "", 1, nil
	}
	return "", "unknown command in fake: " + call, 127, nil
}

// fixture は使い捨ての根 (scratchpad・TMPDIR・ゴミ箱・plan の置き場) と fake。自分の claude (pid 100、cwd /self/proj) と
// 自分の scratchpad (-self-proj/<selfSID>) を最初から持つ。
type fixture struct {
	t                           *testing.T
	session, tmpdir, trash, out string
	sys                         *fakeSys
	d                           deps
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		t:       t,
		session: filepath.Join(root, "claude-501"),
		tmpdir:  filepath.Join(root, "T"),
		trash:   filepath.Join(root, "Trash"),
		out:     filepath.Join(root, "out"),
	}
	for _, dir := range []string{f.session, f.tmpdir, f.trash, f.out} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sb.Allow(t, root)
	f.sys = &fakeSys{
		procs: []fproc{
			{pid: 100, ppid: 1, command: "/Users/me/.local/bin/claude", ucomm: "2.1.306", cwd: "/self/proj", pgrep: true},
			{pid: 200, ppid: 100, command: "/bin/zsh -c tmpclean", ucomm: "zsh", env: "CLAUDE_CODE_SESSION_ID=" + selfSID, cwd: "/self/proj"},
			{pid: selfPID, ppid: 200, command: "tmpclean plan", ucomm: "tmpclean"},
		},
		openNow: map[string][3]any{},
	}
	f.d = deps{
		run:         f.sys.run,
		sessionRoot: f.session,
		tmpDirEnv:   f.tmpdir,
		trashRoot:   f.trash,
		selfSID:     selfSID,
		selfPID:     selfPID,
		now:         func() time.Time { return testNow },
		inner:       sb.Guard,
	}
	f.mkdir(filepath.Join(f.session, "-self-proj", selfSID, "scratchpad"))
	return f
}

// mkdir は dir を作り、中にファイルを 1 つ置く。
func (f *fixture) mkdir(dir string) string {
	f.t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return dir
}

// age は path 以下の全部の更新時刻を testNow の days 日前にする (作った後に呼ぶ。子を作ると親の mtime が動くので)。
func (f *fixture) age(path string, days int) {
	f.t.Helper()
	ts := testNow.Add(-time.Duration(days) * 24 * time.Hour)
	var paths []string
	_ = filepath.Walk(path, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(paths))) // 子から先に (親の mtime を最後に決める)
	for _, p := range paths {
		if err := os.Chtimes(p, ts, ts); err != nil {
			f.t.Fatal(err)
		}
	}
}

// deadSession は claude の居ないプロジェクトに、days 日前の古いセッション dir を作る。
func (f *fixture) deadSession(project, sid string, days int) string {
	f.t.Helper()
	p := f.mkdir(filepath.Join(f.session, project, sid, "scratchpad"))
	_ = p
	dir := filepath.Join(f.session, project, sid)
	f.age(filepath.Join(f.session, project), days)
	return dir
}

func (f *fixture) oldTmp(name string, days int) string {
	f.t.Helper()
	p := f.mkdir(filepath.Join(f.tmpdir, name))
	f.age(p, days)
	return p
}

// runCLI は run を呼び、終了コードと stdout / stderr を返す。
func (f *fixture) runCLI(args ...string) (int, string, string) {
	f.t.Helper()
	var out, errb strings.Builder
	rc := run(context.Background(), f.d, args, &out, &errb)
	return rc, out.String(), errb.String()
}

// plan は plan を作り、ID と plan ファイルのパスを返す (rc が 0 でなければ落とす)。
func (f *fixture) plan(kinds string) (string, string) {
	f.t.Helper()
	rc, out, errOut := f.runCLI("plan", "-kinds", kinds, "-out", f.out)
	if rc != exitOK {
		f.t.Fatalf("plan rc=%d\nstdout:\n%s\nstderr:\n%s", rc, out, errOut)
	}
	var id, path string
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "id: "); ok {
			id = v
		}
		if v, ok := strings.CutPrefix(line, "plan: "); ok {
			path = v
		}
	}
	if id == "" || path == "" {
		f.t.Fatalf("plan の出力に id / plan の行が無い:\n%s", out)
	}
	return id, path
}

func (f *fixture) apply(id, path string) (int, string, string) {
	f.t.Helper()
	return f.runCLI("apply", "-plan", path, "-approve", id)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// trashed は p がゴミ箱の今回の移し先の配下に kind/[project/]名前 の形で在るか。
func (f *fixture) trashed(kind, rel string) bool {
	f.t.Helper()
	matches, _ := filepath.Glob(filepath.Join(f.trash, "tmpclean-*", kind, rel))
	return len(matches) == 1
}
