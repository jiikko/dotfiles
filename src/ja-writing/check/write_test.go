package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandbox は書き込みの関数に渡すパスがテストの一時フォルダの中かを、呼ぶ前に確かめる。外なら関数を呼ばずに止める
// (書き込み・rename・削除を本物のファイルシステムで呼ぶため、テストの書き損じで利用者のファイルに届かないように)。
type sandbox struct {
	t    *testing.T
	root string
}

func newSandbox(t *testing.T) sandbox {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return sandbox{t: t, root: root}
}

func (s sandbox) path(parts ...string) string {
	s.t.Helper()
	p := filepath.Join(append([]string{s.root}, parts...)...)
	if rel, err := filepath.Rel(s.root, p); err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		s.t.Fatalf("テストの一時フォルダの外を書こうとした: %s", p)
	}
	return p
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func temporaries(t *testing.T, folder string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(folder, temporaryHead+"*"))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestEnsureStateCreatesFolderWithIgnoreAndState(t *testing.T) {
	s := newSandbox(t)
	state := s.path("a", ".yomiyasu")
	if err := EnsureState(state, true); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, state); got != 0o700 {
		t.Errorf("状態フォルダの mode = %o, want 700", got)
	}
	if got := readText(t, filepath.Join(state, ".gitignore")); got != GitignoreText {
		t.Errorf(".gitignore = %q", got)
	}
	if got := readText(t, filepath.Join(state, "state.json")); got != "{\"format\": \"yomiyasu-state\", \"format_version\": 1}\n" {
		t.Errorf("state.json = %q", got)
	}
	for _, name := range []string{".gitignore", "state.json"} {
		if got := mode(t, filepath.Join(state, name)); got != 0o600 {
			t.Errorf("%s の mode = %o, want 600", name, got)
		}
	}
	if left := temporaries(t, state); len(left) != 0 {
		t.Errorf("一時ファイルが残った: %v", left)
	}
}

func TestEnsureStateAddsFilesOnlyToBareDefaultFolder(t *testing.T) {
	cases := []struct {
		name         string
		defaultPlace bool
		prepare      func(s sandbox, state string)
		wantIgnore   bool
		wantState    bool
	}{
		{"既定の場所で空", true, func(sandbox, string) {}, true, true},
		{"指定した場所", false, func(sandbox, string) {}, false, false},
		{".gitignore だけある", true, func(_ sandbox, state string) {
			os.WriteFile(filepath.Join(state, ".gitignore"), []byte("x"), 0o600)
		}, true, false},
		{"state.json だけある", true, func(_ sandbox, state string) {
			os.WriteFile(filepath.Join(state, "state.json"), []byte("x"), 0o600)
		}, false, true},
		{".gitignore が壊れたリンク", true, func(_ sandbox, state string) {
			os.Symlink("missing", filepath.Join(state, ".gitignore"))
		}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSandbox(t)
			state := s.path(".yomiyasu")
			if err := os.Mkdir(state, 0o755); err != nil {
				t.Fatal(err)
			}
			c.prepare(s, state)
			if err := EnsureState(state, c.defaultPlace); err != nil {
				t.Fatal(err)
			}
			if got := lexistsRegular(filepath.Join(state, ".gitignore")); got != c.wantIgnore {
				t.Errorf(".gitignore がある = %v, want %v", got, c.wantIgnore)
			}
			if got := lexistsRegular(filepath.Join(state, "state.json")); got != c.wantState {
				t.Errorf("state.json がある = %v, want %v", got, c.wantState)
			}
			if got := mode(t, state); got != 0o755 {
				t.Errorf("すでにあったフォルダの mode を変えた: %o", got)
			}
		})
	}
}

func lexistsRegular(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func TestEnsureStateLeavesLinkedFolderAlone(t *testing.T) {
	s := newSandbox(t)
	target := s.path("elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	state := s.path(".yomiyasu")
	if err := os.Symlink(target, state); err != nil {
		t.Fatal(err)
	}
	if err := EnsureState(state, true); err != nil {
		t.Fatal(err)
	}
	if lexists(filepath.Join(target, ".gitignore")) || lexists(filepath.Join(target, "state.json")) {
		t.Error("リンクの先のフォルダに書いた")
	}
}

func TestEnsureStateKeepsIgnoreWhenStateAlreadyPlaced(t *testing.T) {
	s := newSandbox(t)
	state := s.path(".yomiyasu")
	os.Mkdir(state, 0o700)
	if err := os.WriteFile(filepath.Join(state, "state.json"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeStateFiles(state); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, filepath.Join(state, "state.json")); got != "mine" {
		t.Errorf("すでにある state.json を書き換えた: %q", got)
	}
	if got := readText(t, filepath.Join(state, ".gitignore")); got != GitignoreText {
		t.Errorf(".gitignore = %q", got)
	}
}

func TestWriteStateFilesSkipsStateWhenIgnoreTaken(t *testing.T) {
	s := newSandbox(t)
	state := s.path(".yomiyasu")
	os.Mkdir(state, 0o700)
	if err := os.WriteFile(filepath.Join(state, ".gitignore"), []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeStateFiles(state); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, filepath.Join(state, ".gitignore")); got != "theirs" {
		t.Errorf("先に置かれた .gitignore を書き換えた: %q", got)
	}
	if lexists(filepath.Join(state, "state.json")) {
		t.Error(".gitignore を置けなかったのに state.json を置いた")
	}
}

func TestWriteAtomicReplacesAndCleansUp(t *testing.T) {
	s := newSandbox(t)
	path := s.path("record.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, path); got != "new" {
		t.Errorf("中身 = %q", got)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
	if left := temporaries(t, s.root); len(left) != 0 {
		t.Errorf("一時ファイルが残った: %v", left)
	}
}

func TestWriteAtomicFailureRemovesTemporaryAndNamesFolder(t *testing.T) {
	s := newSandbox(t)
	target := s.path("taken")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	err := WriteAtomic(target, []byte("data"))
	if err == nil {
		t.Fatal("フォルダの名前に書けてしまった")
	}
	if left := temporaries(t, s.root); len(left) != 0 {
		t.Errorf("失敗したのに一時ファイルが残った: %v", left)
	}
	if got, want := WriteProblem(s.path("state"), err), s.root+" に書けません: Is a directory"; got != want {
		t.Errorf("WriteProblem = %q, want %q", got, want)
	}
}

func TestWriteProblemForUnwritableFolder(t *testing.T) {
	s := newSandbox(t)
	folder := s.path("locked")
	if err := os.Mkdir(folder, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(folder, 0o700) })
	err := WriteAtomic(filepath.Join(folder, "record.json"), []byte("data"))
	if err == nil {
		t.Skip("書き込みを拒むフォルダを作れない (root で動いている)")
	}
	if got, want := WriteProblem(s.path("state"), err), folder+" に書けません: Permission denied"; got != want {
		t.Errorf("WriteProblem = %q, want %q", got, want)
	}
}

func TestWriteProblemWithoutFilenameNamesState(t *testing.T) {
	err := unpathed(&os.PathError{Op: "write", Path: "/x/.tmp-00", Err: os.ErrClosed})
	if got := WriteProblem("/s", err); !strings.HasPrefix(got, "/s に書けません: ") {
		t.Errorf("WriteProblem = %q", got)
	}
	if got, want := WriteProblem("/s", &namingError{Folder: "/f"}), "/f に書けません: 一時ファイルの名前を100回試して決められません"; got != want {
		t.Errorf("WriteProblem = %q, want %q", got, want)
	}
}

func TestPlaceNewFileDoesNotOverwrite(t *testing.T) {
	s := newSandbox(t)
	path := s.path("index")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	placed, err := placeNewFile(path, []byte("second"))
	if err != nil || placed {
		t.Fatalf("placeNewFile = %v, %v; want false, nil", placed, err)
	}
	if got := readText(t, path); got != "first" {
		t.Errorf("上書きした: %q", got)
	}
	if left := temporaries(t, s.root); len(left) != 0 {
		t.Errorf("一時ファイルが残った: %v", left)
	}
}

func TestEnsureDirAndTouch(t *testing.T) {
	s := newSandbox(t)
	path, err := ensureDir(s.root, "runs", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if path != s.path("runs", "r1") || mode(t, path) != 0o700 {
		t.Errorf("ensureDir = %s (mode %o)", path, mode(t, path))
	}
	if err := os.WriteFile(s.path("file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureDir(s.root, "file"); err == nil {
		t.Error("ファイルの名前をフォルダとして受けた")
	}
	if err := touch(path, "by-path"); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, filepath.Join(path, "by-path")); got != 0o600 {
		t.Errorf("touch の mode = %o", got)
	}
}
