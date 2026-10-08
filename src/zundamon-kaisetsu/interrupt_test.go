package main

// 敵対的レビュー 2 周目で作り直した仕組み (中断・出力の置き換え・パスの解決・引数の分類) のテスト。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitUntil は条件が成り立つまで刻んで待つ。10 秒で成り立たなければ落とす (ハングの安全網。合否は条件で決める)。
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 1000 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond) // sleep-ok: tick: 条件を見ながら刻む待ちの helper の中の刻み
	}
	t.Fatalf("%s が 10 秒たっても成り立たない", what)
}

// withAppCtx は appCtx を取り消せるものに差し替える (テストの後に戻す)。
func withAppCtx(t *testing.T) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	old := appCtx
	appCtx = ctx
	t.Cleanup(func() { cancel(); appCtx = old })
	return cancel
}

// 中断すると、子 (ffmpeg) を止めてから一時ディレクトリを消す (子を止める前に消すと、書き続ける子に消し残される。2 周目 P2-1)。
func TestInterruptStopsChildThenRemovesTemp(t *testing.T) {
	sleepBin := "/bin/sleep"
	if !isFile(sleepBin) {
		t.Fatal("/bin/sleep が無い") // macOS / Linux には必ずある (skip すると検査が黙って消える)
	}
	cancel := withAppCtx(t)
	shims := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	// 偽の ffmpeg: 出力先 (最後の引数) に書き始めたことを印に残し、止められるまで待つ (実体は絶対パスで exec する)。
	// 止められない退行でも 30 秒で自分から終わり、孤児として残り続けない
	// sleep-ok: dummy: 止められる前提の偽の ffmpeg (実時間を待つのではなく、中断で kill される)
	shim := "#!/bin/sh\nfor a; do out=\"$a\"; done\necho x > \"$out\"\necho $$ > " + marker + "\nexec " + sleepBin + " 30\n"
	writeShim(t, shims, "ffmpeg", shim)
	prependPath(t, shims)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	env := testEnv(t)
	done := make(chan error, 1)
	go func() {
		done <- cmdBuild(env, filepath.Join("testdata", "build", "script.json"), filepath.Join(t.TempDir(), "out"), "html", 1, 64, true)
	}()
	waitUntil(t, "偽の ffmpeg の起動", func() bool { return isFile(marker) })
	cancel()
	var err error
	waitUntil(t, "cmdBuild の終了", func() bool {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	})
	if !errors.Is(err, errInterrupted) {
		t.Errorf("中断の誤りが返らない: %v", err)
	}
	// cmdBuild が戻った時点 (= 一時ディレクトリを消した後) で、子は止まっている (exec したので印の pid が子そのもの)
	b, _ := os.ReadFile(marker)
	pid, perr := strconv.Atoi(strings.TrimSpace(string(b)))
	if perr != nil {
		t.Fatalf("偽の ffmpeg の pid を読めない: %q", b)
	}
	if syscall.Kill(pid, 0) == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("cmdBuild が戻った後も子 (pid %d) が動いている (止める前に一時ディレクトリを消しにいく順になっていないか)", pid)
	}
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 0 {
		t.Errorf("一時ディレクトリが残った: %v", entries)
	}
}

func TestRunQuietStopsOnInterrupt(t *testing.T) {
	cancel := withAppCtx(t)
	marker := filepath.Join(t.TempDir(), "started")
	got := make(chan int, 1)
	go func() {
		// 子が起動したことを印に残してから眠る (起動前に取り消すと、走っている子を止める経路を通らない)
		// sleep-ok: dummy: 中断で kill される子 (止められない退行でも 30 秒で終わる)
		rc, _ := runQuiet(time.Minute, "/bin/sh", "-c", "touch "+marker+"; exec /bin/sleep 30")
		got <- rc
	}()
	waitUntil(t, "子の起動", func() bool { return isFile(marker) })
	cancel()
	var rc int
	waitUntil(t, "runQuiet の終了", func() bool {
		select {
		case rc = <-got:
			return true
		default:
			return false
		}
	})
	if rc != 130 {
		t.Errorf("rc=%d want 130 (中断)", rc)
	}
}

// 出力先が symlink ならリンク先に書き、既存のパーミッションを保ち、一時ファイルを残さない (2 周目 P3)。
func TestWriteOutputFollowsSymlinkAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.html")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "out.html")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeOutput(resolvePath(link), []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Errorf("リンク先に書かれていない: %q", b)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink が通常のファイルに置き換わった")
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Errorf("パーミッションが変わった: %v", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("一時ファイルが残った: %s", e.Name())
		}
	}
	a, _ := partPath(target)
	b, _ := partPath(target)
	if a == b {
		t.Error("同じ出力先の一時ファイルの名前が重なる (同時の build で取り合う)")
	}
}

// リンク先が存在しない (dangling) symlink も、Python の Path.resolve() と同じくリンク先へ進む。
func TestResolvePathDanglingSymlink(t *testing.T) {
	root := resolvePath(t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "real", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"dang": "nowhere/x", "half": filepath.Join(root, "real", "b", "c")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]string{
		root + "/dang":         root + "/nowhere/x",
		root + "/half":         root + "/real/b/c",
		root + "/half/../z":    root + "/real/b/z",
		root + "/real//b/":     root + "/real/b",
		root + "/nope/../real": root + "/real",
	}
	for in, want := range cases {
		if got := resolvePath(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestClassifyArgLikeArgparse(t *testing.T) {
	specs := []optSpec{
		{names: []string{"-o", "--output"}, dest: "output", takes: true},
		{names: []string{"--script"}, dest: "script", takes: true},
	}
	cases := []struct {
		args []string
		opts map[string]string
		pos  []string
	}{
		{[]string{"--output=my file", "s.json"}, map[string]string{"output": "my file"}, []string{"s.json"}},
		{[]string{"-omy file", "s.json"}, map[string]string{"output": "my file"}, []string{"s.json"}},
		{[]string{"--out=a b", "s.json"}, map[string]string{"output": "a b"}, []string{"s.json"}},
		{[]string{"--script=a b.json"}, map[string]string{"script": "a b.json"}, nil},
		{[]string{"-x y", "-5"}, map[string]string{}, []string{"-x y", "-5"}},
	}
	for _, c := range cases {
		p, _, err := parseArgs(c.args, specs, false)
		if err != nil {
			t.Errorf("%q: %v", c.args, err)
			continue
		}
		for k, v := range c.opts {
			if p.opts[k] != v {
				t.Errorf("%q: %s=%q want %q", c.args, k, p.opts[k], v)
			}
		}
		if !slices.Equal(p.pos, c.pos) {
			t.Errorf("%q: 位置引数 %q want %q", c.args, p.pos, c.pos)
		}
	}
	// 未知のオプションの誤りは最後に出すので、後ろの -h が勝つ
	if p, _, err := parseArgs([]string{"--bogus", "-h"}, specs, false); err != nil || !p.help {
		t.Errorf("--bogus -h: help=%v err=%v", p != nil && p.help, err)
	}
	if _, _, err := parseArgs([]string{"--bogus", "s.json"}, specs, false); err == nil || !strings.Contains(err.Error(), "unrecognized arguments: --bogus") {
		t.Errorf("未知のオプションを通した: %v", err)
	}
}

// 3 周目の指摘: 先頭の未知のオプション・--help の略記・先頭の --・負の数・kana の飛び飛びの位置引数。
func TestArgparseRound3(t *testing.T) {
	cases := []struct {
		args []string
		rc   int
	}{
		{[]string{"--enigne=http://x", "speakers"}, 2}, // サブコマンドより前の打ち間違いを黙って捨てない
		{[]string{"--dry-run", "synth", "s.json"}, 2},
		{[]string{"--he"}, 0},
		{[]string{"--", "nope"}, 2}, // -- の後ろをサブコマンドとして読む (nope は選べない値)
		{[]string{"kana", "a", "--who", "zundamon", "b"}, 2},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		if rc := run(c.args, &Env{Stdout: &out, Stderr: &errb}); rc != c.rc {
			t.Errorf("%q: rc=%d want %d (%s)", c.args, rc, c.rc, errb.String())
		}
	}
	var errb bytes.Buffer
	run([]string{"--", "nope"}, &Env{Stdout: &errb, Stderr: &errb})
	if !strings.Contains(errb.String(), "invalid choice: 'nope'") {
		t.Errorf("-- の後ろをサブコマンドとして読んでいない: %s", errb.String())
	}
	specs := []optSpec{{names: []string{"-o", "--output"}, dest: "output", takes: true}}
	for _, v := range []string{"-1e5", "-1_000", "-.5"} {
		if p, _, err := parseArgs([]string{"-o", v}, specs, false); err != nil || p.opts["output"] != v {
			t.Errorf("-o %s: 負の数を値に取らない (%v)", v, err)
		}
	}
}

// symlink のループと長い連鎖も Python の realpath と同じ結果にし、指数的に時間を食わない (3 周目 P3)。
func TestResolvePathLoopsAndChains(t *testing.T) {
	root := resolvePath(t.TempDir())
	mk := func(name, target string) {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	mk("self", "self/x")
	mk("a", "a/../a/../a")
	for i := range 45 {
		mk(fmt.Sprintf("chain%d", i), fmt.Sprintf("chain%d", i+1))
	}
	cases := map[string]string{
		root + "/self":   root + "/self/x",
		root + "/a":      root + "/a",
		root + "/chain0": root + "/chain45",
	}
	for in, want := range cases {
		if got := resolvePath(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

// 新しい出力は 0666 から umask を引いたもの。読み取り専用の既存の出力は置き換えない (3 周目 P3)。
func TestWriteOutputUmaskAndReadOnly(t *testing.T) {
	old := fileUmask
	fileUmask = 0o077
	t.Cleanup(func() { fileUmask = old })
	dir := t.TempDir()
	fresh := filepath.Join(dir, "new.html")
	if err := writeOutput(fresh, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0o600 {
		t.Errorf("umask 077 の新しい出力: %v (want 0600)", fi.Mode().Perm())
	}
	ro := filepath.Join(dir, "ro.html")
	if err := os.WriteFile(ro, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := writeOutput(ro, []byte("new")); err == nil {
		t.Error("読み取り専用の既存の出力を置き換えた")
	}
	if b, _ := os.ReadFile(ro); string(b) != "old" {
		t.Errorf("読み取り専用の出力が書き換わった: %q", b)
	}
}

// 合成のキャッシュは rename の意味 (Python 版と同じ): 読み取り専用の既存のファイルも置き換え、パーミッションは umask から決める (4 周目 P2-1)。
func TestCacheWriteReplacesReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.wav")
	if err := os.WriteFile(path, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new")); err != nil {
		t.Fatalf("読み取り専用の既存のキャッシュを置き換えられない: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Errorf("置き換わっていない: %q", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o666&^fileUmask {
		t.Errorf("パーミッション: %v (want %v)", fi.Mode().Perm(), 0o666&^fileUmask)
	}
}

func TestKanaDashDashAfterOptionIsExtra(t *testing.T) {
	for _, args := range [][]string{{"kana", "a", "--who", "zundamon", "--", "b"}, {"kana", "a", "b", "--who", "zundamon", "--", "c"}} {
		var errb bytes.Buffer
		if rc := run(args, &Env{Stdout: &errb, Stderr: &errb}); rc != 2 {
			t.Errorf("%q: rc=%d want 2 (%s)", args, rc, errb.String())
		}
	}
	specs := []optSpec{{names: []string{"--who"}, dest: "who", takes: true}}
	if p, _, err := parseArgs([]string{"--who", "x", "--", "a", "b"}, specs, false); err != nil || p.posGroups != 1 || p.firstGroup != 2 {
		t.Errorf("オプションの後の -- だけで始まる位置引数は 1 つのまとまり: %+v %v", p, err)
	}
}
