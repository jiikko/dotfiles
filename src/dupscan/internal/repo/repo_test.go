package repo

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
