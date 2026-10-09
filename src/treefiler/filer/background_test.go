package filer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// settleBackground は裏の走査と git の取得が終わるまで Advance を回す (hang guard 10 秒)。
func settleBackground(t *testing.T, m *Model) {
	t.Helper()
	// walker の時計を 1 周ごとに 1 秒進める (古い結果の数え直しの間隔を実時間で待たない)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	m.walker.mu.Lock()
	m.walker.now = func() time.Time { return time.Unix(0, clock.Load()) }
	m.walker.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	at := fixedNow
	for {
		at = at.Add(20 * time.Millisecond)
		clock.Add(int64(time.Second))
		m.Advance(at)
		if !m.Busy() {
			m.Advance(at.Add(20 * time.Millisecond)) // 最後の結果を取り込む
			if !m.Busy() {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("裏の処理が 10 秒で終わらない")
		}
		time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 裏の goroutine の完了を条件で待つループの刻み
	}
}

// フォルダの色は配下でいちばん新しい更新で決まる。dotfile を隠している間は dotfile を数えない (spec §5.1)。
func TestFolderHeatUsesNewestInsideAndHidesDotfiles(t *testing.T) {
	m := newTest(t)
	b := m.kids(m.root)[1] // b
	if b.raw != "b" {
		t.Fatalf("前提: %q", b.raw)
	}
	deep := filepath.Join(b.path(), "deep", "x.txt")
	recent := fixedNow.Add(-2 * time.Minute)
	if err := os.Chtimes(deep, recent, recent); err != nil {
		t.Fatal(err)
	}
	dot := filepath.Join(b.path(), ".newer")
	if err := os.WriteFile(dot, []byte("."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dot, fixedNow, fixedNow); err != nil {
		t.Fatal(err)
	}
	// フォルダ自身の mtime は 1 時間前 (b/deep を作った時刻より古くする)
	old := fixedNow.Add(-48 * time.Hour)
	for _, d := range []string{filepath.Join(b.path(), "deep"), b.path()} {
		if err := os.Chtimes(d, old, old); err != nil {
			t.Fatal(err)
		}
	}
	settleBackground(t, m)
	if got := m.heatAge(b); got < 119 || got > 121 {
		t.Fatalf("b の年齢 = %.0f 秒 (配下の x.txt の 120 秒になるはず。dotfile の 0 秒は数えない)", got)
	}
	m.showHidden = true
	if got := m.heatAge(b); got > 1 {
		t.Fatalf("dotfile を見せているのに dotfile の更新を数えない: %.0f 秒", got)
	}
}

func TestParsePorcelain(t *testing.T) {
	out := "## main...origin/main [ahead 1, behind 2]\x00" +
		" M src/a.go\x00" +
		"A  src/b.go\x00" +
		"?? docs/new.md\x00" +
		"UU conflict.txt\x00" +
		"R  renamed.go\x00old.go\x00" +
		"!! build/\x00" +
		"?? untracked-dir/\x00"
	s := parsePorcelain([]byte(out))
	s.top = "/r"
	if s.branch != "main ↑1 ↓2" {
		t.Fatalf("branch = %q", s.branch)
	}
	cases := map[string]struct {
		dir  bool
		want byte
	}{
		"/r/src/a.go":            {false, 'M'},
		"/r/src/b.go":            {false, '+'},
		"/r/docs/new.md":         {false, '?'},
		"/r/conflict.txt":        {false, '!'},
		"/r/renamed.go":          {false, '+'},
		"/r/old.go":              {false, gitNone}, // R の元のパスは状態に数えない
		"/r/src":                 {true, 'M'},      // 配下のいちばん重い状態
		"/r/build":               {true, gitIgn},
		"/r/build/x.o":           {false, gitIgn}, // ignored のフォルダの中
		"/r/untracked-dir/f.txt": {false, '?'},
		"/outside/a.go":          {false, gitNone},
	}
	for p, c := range cases {
		if got := s.state(p, c.dir); got != c.want {
			t.Errorf("state(%s) = %q, want %q", p, got, c.want)
		}
	}
	if s.state("/r", true) != gitNone {
		t.Error("repo の根そのものに状態を付けている")
	}
}

func TestBranchLabel(t *testing.T) {
	for in, want := range map[string]string{
		"main":                         "main",
		"main...origin/main":           "main",
		"main...origin/main [ahead 3]": "main ↑3",
		"No commits yet on dev":        "dev",
		"HEAD (no branch)":             "detached",
	} {
		if got := branchLabel(in); got != want {
			t.Errorf("branchLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// 本物の git で: 変更したファイルに印が付き、ステータスバーにブランチが出る。
func TestGitMarksFromRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	dir := fixture(t)
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "trunk")
	git("add", "a")
	git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
	if err := os.WriteFile(filepath.Join(dir, "a", "one.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(dir, Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	settleBackground(t, m)
	m.snapAll()
	cdTo(t, m, "a/one.txt")
	if m.gitState(m.cur) != 'M' {
		t.Fatalf("変更したファイルの状態 = %q", m.gitState(m.cur))
	}
	if m.gitState(m.cur.parent) != 'M' {
		t.Fatal("フォルダが配下の変更を持たない")
	}
	if !strings.Contains(m.draw().plain()[m.h-1], "trunk") {
		t.Fatal("ステータスバーにブランチが出ない")
	}
}

// 取り込んでいない結果がある間は Busy (呼び出し側が最後の結果を取り込む前に tick を止めないように)。
func TestBusyUntilResultsAreTaken(t *testing.T) {
	m := newTest(t)
	m.walker.request(m.root.path())
	deadline := time.Now().Add(10 * time.Second)
	for m.walker.busy() || m.git.busy() {
		if time.Now().After(deadline) {
			t.Fatal("走査が終わらない")
		}
		time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 裏の goroutine の完了を条件で待つループの刻み
	}
	if !m.Busy() {
		t.Fatal("取り込んでいない結果があるのに Busy が false")
	}
	m.takeBackground()
	m.walker.mu.Lock()
	m.walker.queue = nil // 見えているフォルダの依頼は今回の主張と無関係なので捨てる
	m.walker.mu.Unlock()
	for m.walker.busy() {
		time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 走りかけの 1 本の終わりを待つループの刻み
	}
	m.takeBackground()
	if m.Busy() {
		t.Fatal("取り込んだ後も Busy のまま (tick が止まらない)")
	}
}

func TestPorcelainWorktreeRenameAndRootSlash(t *testing.T) {
	s := parsePorcelain([]byte(" R new-name.txt\x00some-old-name.txt\x00?? x.txt\x00"))
	s.top = "/"
	if got := s.state("/new-name.txt", false); got != 'M' {
		t.Fatalf("worktree 側の rename の状態 = %q", got)
	}
	if _, ok := s.files["me-old-name.txt"]; ok || len(s.files) != 2 {
		t.Fatalf("元のパスを別の項目と読み違えた: %v", s.files)
	}
	if got := s.state("/x.txt", false); got != '?' {
		t.Fatalf("repo の根が / のとき状態が引けない: %q", got)
	}
}

// ライブ更新: 開いているフォルダにファイルが増えたら合図が届き、取り込むと木に出て、その項目から root へ光が昇る。
func TestWatchPicksUpNewFileAndLightsUp(t *testing.T) {
	m := newTest(t)
	ch := m.Changed()
	defer m.Close()
	m.Advance(fixedNow) // 見るフォルダ (開いている root) を渡す。基準は木が読み込んだ中身なので、最初の観測を待たずに作ってよい
	if err := os.WriteFile(filepath.Join(m.root.path(), "fresh.txt"), []byte("f\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(5 * time.Second): // hang guard
		t.Fatal("ファイルを作っても合図が届かない")
	}
	m.Advance(fixedNow.Add(time.Second))
	var fresh *node
	for _, k := range m.kids(m.root) {
		if k.raw == "fresh.txt" {
			fresh = k
		}
	}
	if fresh == nil {
		t.Fatal("新しいファイルが木に出ない")
	}
	if _, ok := m.ripples[fresh]; !ok {
		t.Fatal("新しいファイルが光らない")
	}
	if r, ok := m.ripples[m.root]; !ok || r.strength >= 1 {
		t.Fatalf("root へ光が昇らない / 弱まらない: %+v", r)
	}
	m.Close()
	select {
	case _, ok := <-ch:
		if ok {
			// 溜まっていた合図を読んだ。次は閉じている
			if _, ok := <-ch; ok {
				t.Fatal("Close の後もチャネルが閉じない")
			}
		}
	case <-time.After(5 * time.Second): // hang guard
		t.Fatal("Close の後もチャネルが閉じない (待っている呼び出し側が残る)")
	}
}

func TestDiffSig(t *testing.T) {
	t0 := fixedNow
	old := map[string]entrySig{"a": {t0, 1, false}, "b": {t0, 1, false}}
	cur := map[string]entrySig{"a": {t0.Add(time.Second), 1, false}, "c": {t0, 1, false}}
	c, ok := diffSig("/d", old, cur)
	if !ok || !c.listing || len(c.changed) != 2 {
		t.Fatalf("diffSig = %+v (a が変わり c が増え b が消えた)", c)
	}
	if _, ok := diffSig("/d", old, old); ok {
		t.Fatal("変わっていないのに変化と言う")
	}
}

// カーソルが動くとビーズが走り、次のキーで終点へ飛ぶ (打ち切られる)。
func TestBeadRunsOnMoveAndSnapsOnKey(t *testing.T) {
	m := newTest(t)
	m.HandleKey("j")
	m.Advance(fixedNow.Add(16 * time.Millisecond))
	m.Advance(fixedNow.Add(32 * time.Millisecond))
	if !m.beadOn {
		t.Fatal("カーソルが動いてもビーズが走らない")
	}
	m.HandleKey("k")
	if m.beadOn {
		t.Fatal("次のキーでビーズが打ち切られない")
	}
}

// 隠している dotfile の出入り・変化ではフォルダを光らせない (見えない変化で毎秒光るのを止める)。
func TestHiddenDotfileChangeDoesNotLightUp(t *testing.T) {
	m := newTest(t)
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{".hidden"}, removed: []string{".gone"}, listing: true})
	if len(m.ripples) != 0 {
		t.Fatalf("隠している dotfile の変化で光った: %d 件", len(m.ripples))
	}
	m.applyChange(dirChange{dir: m.root.path(), removed: []string{"gone.txt"}, listing: true})
	if _, ok := m.ripples[m.root]; !ok {
		t.Fatal("見える項目が消えてもフォルダが光らない")
	}
}

// 変化した項目から親への肘の線も、波紋の色に光る (spec §4.4 の肘)。名前だけ光るのではない。
func TestRippleLightsElbow(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	m.Advance(fixedNow)
	m.snapAll()
	one := m.cur
	before := m.draw()
	base := make([]rgb, len(before.cells))
	for i := range before.cells {
		base[i] = before.cells[i].fg
	}
	m.lightUp(one)
	m.now = func() time.Time { return fixedNow.Add(100 * time.Millisecond) }
	after := m.draw()
	lit := 0
	for i, cl := range after.cells {
		if strings.ContainsAny(cl.s, "═║╔╗╚╝╠╣╦╩╬─│") && dist(cl.fg, cRipple) < dist(base[i], cRipple)-1 {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("肘の線が光らない")
	}
}

func dist(a, b rgb) float64 {
	d := 0.0
	for i := range a {
		d += (a[i] - b[i]) * (a[i] - b[i])
	}
	return d
}

// setGit は root を根とする repo の状態を porcelain の出力から決め打ちする。
func setGit(m *Model, porcelain string) {
	s := parsePorcelain([]byte(porcelain))
	s.top, s.prefix = m.root.abs, withSep(m.root.abs)
	m.gitSnap = gitSet{repos: []gitSnapshot{s}}
}

// root が repo の外で、配下に repo が並ぶ (~/src のような) とき、開いたフォルダの repo ごとに印とブランチが出る (spec §5.3)。
func TestGitNestedRepos(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	root := t.TempDir()
	mkRepo := func(name, branch string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		git := func(args ...string) {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		git("init", "-q", "-b", branch)
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
		return dir
	}
	p1 := mkRepo("p1", "one")
	mkRepo("p2", "two")
	if err := os.WriteFile(filepath.Join(p1, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(root, Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	for _, name := range []string{"p1", "p2"} {
		n := m.findNode(filepath.Join(root, name))
		m.ensureLoaded(n)
		n.expanded = true
	}
	m.startGit(false) // 開いたフォルダの repo が増えたので、間隔を待たずに取る
	settleBackground(t, m)
	f := m.findNode(filepath.Join(p1, "f.txt"))
	if m.gitState(f) != 'M' {
		t.Fatalf("p1/f.txt の状態 = %q", m.gitState(f))
	}
	if m.gitState(f.parent) != 'M' {
		t.Fatalf("repo の根のフォルダ p1 の芽 = %q", m.gitState(f.parent))
	}
	if st := m.gitState(m.findNode(filepath.Join(root, "p2", "f.txt"))); st != gitNone {
		t.Fatalf("変更の無い p2/f.txt の状態 = %q", st)
	}
	m.setCur(m.findNode(filepath.Join(root, "p2", "f.txt")))
	m.snapAll()
	if bar := m.draw().plain()[m.h-1]; !strings.Contains(bar, "two") || strings.Contains(bar, "one") {
		t.Fatalf("カーソルの repo のブランチが出ない: %q", bar)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
}

// 外側の repo の中の入れ子の repo: 中のファイルは内側の repo で引き (深い repo が先)、変更の無い入れ子の repo の根は
// 外側から見た状態 (未追跡) のまま (開いた途端に印が消えない)。
func TestGitRepoInsideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("top.txt", "t\n")
	gitIn(t, root, "init", "-q", "-b", "outer")
	commitAll(t, root)
	for _, in := range []string{"dirty", "clean"} {
		mk(in+"/f.txt", "1\n")
		gitIn(t, filepath.Join(root, in), "init", "-q", "-b", in)
		commitAll(t, filepath.Join(root, in))
	}
	mk("dirty/f.txt", "changed\n")
	m, err := New(root, Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	for _, in := range []string{"dirty", "clean"} {
		n := m.findNode(filepath.Join(root, in))
		m.ensureLoaded(n)
		n.expanded = true
	}
	m.startGit(false)
	settleBackground(t, m)
	if st := m.gitState(m.findNode(filepath.Join(root, "dirty", "f.txt"))); st != 'M' {
		t.Fatalf("入れ子の repo の中の変更 = %q (外側の未追跡で引いていないか)", st)
	}
	if st := m.gitState(m.findNode(filepath.Join(root, "clean"))); st != '?' {
		t.Fatalf("変更の無い入れ子の repo の根 = %q (外側から見た未追跡のはず)", st)
	}
}

func TestGitSetPrefixBoundary(t *testing.T) {
	s := parsePorcelain([]byte(" M x\x00"))
	s.top = "/a/b"
	g := gitSet{repos: []gitSnapshot{s}}
	if g.state("/a/b/x", false) != 'M' {
		t.Fatal("前提: /a/b/x が引けない")
	}
	if st := g.state("/a/bc/x", false); st != gitNone {
		t.Fatalf("/a/bc/x を /a/b の repo で引いた: %q", st)
	}
	g.repos[0].branch = "b"
	if b := g.branchFor("/a/bc/x"); b != "" {
		t.Fatalf("/a/bc/x に /a/b のブランチを出した: %q", b)
	}
}

// フォルダで git init した後、読み直し (r) でも、ライブ更新で .git が現れたときでも、repo の根を数え直す。
func TestGitRepoTopRecountedAfterInit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	for _, via := range []string{"refresh", "live"} {
		m := newTest(t)
		a := m.findNode(filepath.Join(m.root.abs, "a"))
		m.ensureLoaded(a)
		a.expanded = true
		m.startGit(true)
		settleBackground(t, m)
		gitIn(t, a.path(), "init", "-q", "-b", "x")
		if via == "refresh" {
			m.Refresh()
		} else {
			m.applyChange(dirChange{dir: a.path(), changed: []string{".git"}, listing: true})
			m.startGit(true)
		}
		settleBackground(t, m)
		if st := m.gitState(m.findNode(filepath.Join(a.path(), "one.txt"))); st != '?' {
			t.Fatalf("%s: git init した後の a/one.txt = %q (repo の根を数え直していない)", via, st)
		}
	}
}

func TestShortSizeStaysFourWide(t *testing.T) {
	cases := map[int64]string{0: "0B", -5: "0B", 999: "999B", 1000: "1.0K", 10188: "9.9K", 10199: "10K", 10239: "10K",
		1023 * 1000: "999K", 1023500: "1.0M", 1048575: "1.0M", 9_990_000: "9.5M", 10_430_000: "9.9M", 10_440_000: "10M"}
	for n, want := range cases {
		if got := shortSize(n); got != want || widthOf(got) > 4 {
			t.Fatalf("shortSize(%d) = %q, want %q", n, got, want)
		}
	}
}

// 肘の光は親の行・子の行・縦の成分を持つセルだけ。途中の行の兄弟の枝 (横線だけのセル) は光らない。
func TestRippleElbowSkipsSiblingRows(t *testing.T) {
	m := newTest(t)
	m.Advance(fixedNow)
	m.snapAll()
	ks := m.kids(m.root)
	last := ks[len(ks)-1]
	before := m.draw()
	base := make([]rgb, len(before.cells))
	for i := range before.cells {
		base[i] = before.cells[i].fg
	}
	m.lightUp(last)
	m.now = func() time.Time { return fixedNow.Add(100 * time.Millisecond) }
	after := m.draw()
	for i, cl := range after.cells {
		if cl.s == "═" && after.cells[i].fg != base[i] {
			y := i / after.w
			if strings.Contains(after.plain()[y], last.name) || y == m.canvasH()/2 {
				continue // 光らせた子の行・親 (root) の行
			}
			t.Fatalf("%d 行目の兄弟の枝まで光った: %q", y, after.plain()[y])
		}
	}
}
