package check

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

func requireText(t *testing.T, path, want, what string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("%s: %s = %q, want %q", what, filepath.Base(path), data, want)
	}
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
	requireText(t, filepath.Join(state, ".gitignore"), GitignoreText, ".gitignore")
	requireText(t, filepath.Join(state, "state.json"), "{\"format\": \"yomiyasu-state\", \"format_version\": 1}\n", "作った状態フォルダ")
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
		prepare      func(state string) error
		wantIgnore   bool
		wantState    bool
	}{
		{"既定の場所で空", true, func(string) error { return nil }, true, true},
		{"指定した場所", false, func(string) error { return nil }, false, false},
		{".gitignore だけある", true, func(state string) error {
			return os.WriteFile(filepath.Join(state, ".gitignore"), []byte("x"), 0o600)
		}, true, false},
		{"state.json だけある", true, func(state string) error {
			return os.WriteFile(filepath.Join(state, "state.json"), []byte("x"), 0o600)
		}, false, true},
		{".gitignore が壊れたリンク", true, func(state string) error {
			return os.Symlink("missing", filepath.Join(state, ".gitignore"))
		}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSandbox(t)
			state := s.path(".yomiyasu")
			if err := os.Mkdir(state, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := c.prepare(state); err != nil {
				t.Fatal(err)
			}
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
	if fileInfo(filepath.Join(target, ".gitignore"), false) != nil || fileInfo(filepath.Join(target, "state.json"), false) != nil {
		t.Error("リンクの先のフォルダに書いた")
	}
}

func TestEnsureStateKeepsIgnoreWhenStateAlreadyPlaced(t *testing.T) {
	s := newSandbox(t)
	state := s.path(".yomiyasu")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "state.json"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeStateFiles(state); err != nil {
		t.Fatal(err)
	}
	requireText(t, filepath.Join(state, "state.json"), "mine", "すでにある state.json を書き換えた")
	requireText(t, filepath.Join(state, ".gitignore"), GitignoreText, ".gitignore")
}

func TestWriteStateFilesSkipsStateWhenIgnoreTaken(t *testing.T) {
	s := newSandbox(t)
	state := s.path(".yomiyasu")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, ".gitignore"), []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeStateFiles(state); err != nil {
		t.Fatal(err)
	}
	requireText(t, filepath.Join(state, ".gitignore"), "theirs", "先に置かれた .gitignore を書き換えた")
	if fileInfo(filepath.Join(state, "state.json"), false) != nil {
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
	requireText(t, path, "new", "入れ替えた後")
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
	t.Cleanup(func() {
		if err := os.Chmod(folder, 0o700); err != nil {
			t.Error(err)
		}
	})
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
	requireText(t, path, "first", "すでにあるファイル")
	if left := temporaries(t, s.root); len(left) != 0 {
		t.Errorf("一時ファイルが残った: %v", left)
	}
}

// withRandom は一時ファイルの名前の乱数を差し替え、テストの終わりに戻す。
func withRandom(t *testing.T, read func([]byte) (int, error)) {
	t.Helper()
	saved := randomRead
	randomRead = read
	t.Cleanup(func() { randomRead = saved })
}

// withLink は置くときの link を差し替え、テストの終わりに戻す。
func withLink(t *testing.T, link func(string, string) error) {
	t.Helper()
	saved := linkFile
	linkFile = link
	t.Cleanup(func() { linkFile = saved })
}

func TestEnsureStateRemovesNewFolderWhenIgnoreCannotBeWritten(t *testing.T) {
	s := newSandbox(t)
	state := s.path("a", ".yomiyasu")
	failure := errors.New("乱数を読めない")
	withRandom(t, func([]byte) (int, error) { return 0, failure })
	if err := EnsureState(state, true); !errors.Is(err, failure) {
		t.Fatalf("EnsureState = %v, want %v", err, failure)
	}
	if _, err := os.Lstat(state); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf(".gitignore を置けなかった状態フォルダが残った (lstat: %v)", err)
	}
	if !isDir(s.path("a")) {
		t.Error("状態フォルダの親まで消した")
	}
}

func TestEnsureStateKeepsFolderWithIgnoreWhenStateJSONCannotBeWritten(t *testing.T) {
	s := newSandbox(t)
	state := s.path(".yomiyasu")
	failure := errors.New("乱数を読めない")
	reads := 0
	withRandom(t, func(b []byte) (int, error) {
		reads++
		if reads > 1 {
			return 0, failure
		}
		return rand.Read(b)
	})
	if err := EnsureState(state, true); !errors.Is(err, failure) {
		t.Fatalf("EnsureState = %v, want %v", err, failure)
	}
	requireText(t, filepath.Join(state, ".gitignore"), GitignoreText, "置けた .gitignore")
	if fileInfo(filepath.Join(state, "state.json"), false) != nil {
		t.Error("書けなかった state.json がある")
	}
	if left := temporaries(t, state); len(left) != 0 {
		t.Errorf("一時ファイルが残った: %v", left)
	}
}

func TestPlaceNewFileWithoutHardLinks(t *testing.T) {
	withLink(t, func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} })
	s := newSandbox(t)
	path := s.path("index")
	placed, err := placeNewFile(path, []byte("first"))
	if err != nil || !placed {
		t.Fatalf("placeNewFile = %v, %v; want true, nil", placed, err)
	}
	requireText(t, path, "first", "O_EXCL で置いたファイル")
	if got := mode(t, path); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
	if placed, err := placeNewFile(path, []byte("second")); err != nil || placed {
		t.Fatalf("2 回目の placeNewFile = %v, %v; want false, nil", placed, err)
	}
	requireText(t, path, "first", "すでにあるファイル")
	dangling := s.path("dangling")
	if err := os.Symlink("missing", dangling); err != nil {
		t.Fatal(err)
	}
	if placed, err := placeNewFile(dangling, []byte("x")); err != nil || placed {
		t.Fatalf("壊れたリンクの名前に placeNewFile = %v, %v; want false, nil", placed, err)
	}
	if _, err := os.Stat(s.path("missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("壊れたリンクの先に書いた (stat: %v)", err)
	}
	if left := temporaries(t, s.root); len(left) != 0 {
		t.Errorf("一時ファイルが残った: %v", left)
	}
}

func TestWriteAtomicGivesUpWhenTemporaryNamesKeepColliding(t *testing.T) {
	s := newSandbox(t)
	if err := os.WriteFile(s.path(temporaryHead+"0000000000000000"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	withRandom(t, func(b []byte) (int, error) {
		reads++
		clear(b)
		return len(b), nil
	})
	path := s.path("record.json")
	err := WriteAtomic(path, []byte("data"))
	var naming *namingError
	if !errors.As(err, &naming) || naming.Folder != s.root {
		t.Fatalf("WriteAtomic = %v, want 名前を決められない誤り (%s)", err, s.root)
	}
	if reads != temporaryTries {
		t.Errorf("名前を %d 回試した、want %d", reads, temporaryTries)
	}
	if got, want := WriteProblem(s.path("state"), err), s.root+" に書けません: 一時ファイルの名前を100回試して決められません"; got != want {
		t.Errorf("WriteProblem = %q, want %q", got, want)
	}
	if fileInfo(path, false) != nil {
		t.Error("名前を決められなかったのに書いた")
	}
}
