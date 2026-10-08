package filer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// settleBackground は裏の走査と git の取得が終わるまで Advance を回す (hang guard 10 秒)。
func settleBackground(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	at := fixedNow
	for {
		at = at.Add(20 * time.Millisecond)
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
