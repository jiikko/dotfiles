package repo

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"subproc"
)

func TestMain(m *testing.M) {
	if err := subproc.UnsetGitLocationEnv(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := subproc.GitCommand(context.Background(), append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture は追跡・untracked・ignore 済み・symlink・消えたファイルを持つ repo を作る。
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	run(t, root, "init", "-q")
	write(t, root, "macOS/Sources/A.swift", "func a() {}\n")
	write(t, root, "macOS/Sources/Gone.swift", "func g() {}\n")
	write(t, root, "Shared/B.swift", "func b() {}\n")
	write(t, root, "Shared/notes.txt", "x\n")
	write(t, root, ".gitignore", "ignored/\n")
	run(t, root, "add", ".")
	run(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	write(t, root, "macOS/Sources/New.swift", "func n() {}\n")                         // untracked
	write(t, root, "ignored/C.swift", "func c() {}\n")                                 // ignore 済み
	if err := os.Remove(filepath.Join(root, "macOS/Sources/Gone.swift")); err != nil { // 追跡しているが消えた
		t.Fatal(err)
	}
	if err := os.Symlink("../../Shared/B.swift", filepath.Join(root, "macOS/Sources/Link.swift")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestListRootAndSubdir(t *testing.T) {
	root := fixture(t)
	sc, err := Resolve([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	got, err := List(sc, []string{".swift"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Shared/B.swift", "macOS/Sources/A.swift", "macOS/Sources/New.swift"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("root: got %v, want %v (untracked を拾い、ignore 済み・symlink・消えたファイル・拡張子違いを拾わない)", got, want)
	}

	sc, err = Resolve([]string{filepath.Join(root, "macOS")})
	if err != nil {
		t.Fatal(err)
	}
	got, err = List(sc, []string{".swift"})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"macOS/Sources/A.swift", "macOS/Sources/New.swift"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("subdir: got %v, want %v", got, want)
	}
}

// 大文字小文字の違う起点 (macOS の既定のファイルシステムでは cd できる) を、git が知る綴りに直す。
// 字面のまま pathspec に渡すと 0 件になる。
func TestResolveNormalizesCase(t *testing.T) {
	root := fixture(t)
	lower := filepath.Join(root, "macos")
	if _, err := os.Stat(lower); err != nil {
		t.Skip("大文字小文字を区別するファイルシステム: ", err)
	}
	sc, err := Resolve([]string{lower})
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Prefixes) != 1 || !strings.HasPrefix(sc.Prefixes[0], "macOS/") {
		t.Fatalf("prefix = %v, 期待 macOS/", sc.Prefixes)
	}
	got, err := List(sc, []string{".swift"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, 期待 2 件", got)
	}
}

func TestCleanEnvDropsGitLocation(t *testing.T) {
	got := cleanEnv([]string{"GIT_DIR=/x", "HOME=/h", "GIT_WORK_TREE=/y", "PATH=/p"})
	if !reflect.DeepEqual(got, []string{"HOME=/h", "PATH=/p"}) {
		t.Errorf("got %v", got)
	}
}

// base の commit を読みかけで止めるときは、git を止めてから待つ。読み残したまま待つと、後ろの大きな blob で pipe を埋めた git が書けずに止まり、
// GitOpTimeout (30 秒) まで戻らない。直した版は即座に返る。10 秒は時間切れより十分短い見張りで、合否を所要で決めるのではなく「止まらない」ことを見る。
func TestCommitReadAllStopsGitOnError(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q")
	big := strings.Repeat("let x = 1\n", 60000) // 600KB: pipe の容量を大きく超える
	for _, f := range []string{"A.swift", "B.swift", "C.swift", "D.swift"} {
		write(t, root, f, f+big)
	}
	run(t, root, "add", ".")
	run(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c")
	h, err := CommitHash(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	c := &Commit{Scope: Scope{Root: root, Prefixes: []string{""}}, Exts: []string{".swift"}, Hash: h}
	files, err := c.Files()
	if err != nil || len(files) != 4 {
		t.Fatalf("Files: %v %v", files, err)
	}
	c.blobs[files[0]] = strings.Repeat("0", 40) // 無い object: cat-file は `missing` を返し、後ろの 3 つは読まれずに残る
	done := make(chan error, 1)
	go func() {
		_, err := c.ReadAll(files)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "blob として返さない") {
			t.Errorf("無い blob で失敗を期待: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("読みかけで止めた ReadAll が戻らない (git の終了を待ち続けている)")
	}
}
