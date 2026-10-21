package safefs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
	"safefs"
)

func TestRemoveEmptyDirRemovesEmptyAndKeepOnly(t *testing.T) {
	d := tempDir(t)
	if err := os.Mkdir(filepath.Join(d, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(d, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d, "keep", ".keep-directory"), "")
	for _, n := range []string{"empty", "keep"} {
		if err := safefs.RemoveEmptyDir(d, n, ident(t, filepath.Join(d, n)), ".keep-directory", sb.Guard); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if _, err := os.Lstat(filepath.Join(d, n)); !os.IsNotExist(err) {
			t.Errorf("%s が残った: %v", n, err)
		}
	}
}

// 空でないもの・keep が空でない・keep がフォルダ / symlink / hardlink の片割れ・keep を許していない、は何も消さない。
func TestRemoveEmptyDirRefusesAnythingWithContent(t *testing.T) {
	d := tempDir(t)
	other := filepath.Join(d, "other")
	write(t, other, "X")
	cases := []struct {
		name  string
		setup func(dir string)
		keep  string
	}{
		{"file", func(dir string) { write(t, filepath.Join(dir, "a.o"), "") }, ".keep-directory"},
		{"keep-and-file", func(dir string) {
			write(t, filepath.Join(dir, ".keep-directory"), "")
			write(t, filepath.Join(dir, "inputs-1.LinkFileList"), "x")
		}, ".keep-directory"},
		{"keep-not-empty", func(dir string) { write(t, filepath.Join(dir, ".keep-directory"), "x") }, ".keep-directory"},
		{"keep-is-dir", func(dir string) { _ = os.Mkdir(filepath.Join(dir, ".keep-directory"), 0o700) }, ".keep-directory"},
		{"keep-is-symlink", func(dir string) { _ = os.Symlink(other, filepath.Join(dir, ".keep-directory")) }, ".keep-directory"},
		{"keep-is-hardlink", func(dir string) {
			empty := filepath.Join(d, "emptyfile")
			write(t, empty, "")
			_ = os.Link(empty, filepath.Join(dir, ".keep-directory"))
		}, ".keep-directory"},
		{"keep-not-allowed", func(dir string) { write(t, filepath.Join(dir, ".keep-directory"), "") }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(d, c.name)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			c.setup(dir)
			before, _ := os.ReadDir(dir)
			err := safefs.RemoveEmptyDir(d, c.name, ident(t, dir), c.keep, sb.Guard)
			if !errors.Is(err, safefs.ErrNotEmpty) {
				t.Fatalf("err = %v, want ErrNotEmpty", err)
			}
			after, err := os.ReadDir(dir)
			if err != nil || len(after) != len(before) {
				t.Errorf("中身が変わった: before=%d after=%d err=%v", len(before), len(after), err)
			}
		})
	}
	if got := read(t, other); got != "X" {
		t.Errorf("symlink の先が変わった: %q", got)
	}
}

// 決めた実体と違う (差し替わった)・識別できない・symlink・ファイル、は消さない。
func TestRemoveEmptyDirRefusesSwappedAndNonDirs(t *testing.T) {
	d := tempDir(t)
	a, b := filepath.Join(d, "a"), filepath.Join(d, "b")
	for _, p := range []string{a, b} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := safefs.RemoveEmptyDir(d, "a", ident(t, b), "", sb.Guard); !errors.Is(err, safefs.ErrSwapped) {
		t.Errorf("別の実体の id: err = %v", err)
	}
	if err := safefs.RemoveEmptyDir(d, "a", safefs.ID{}, "", sb.Guard); !errors.Is(err, safefs.ErrUnidentified) {
		t.Errorf("ゼロの id: err = %v", err)
	}
	if err := os.Symlink(b, filepath.Join(d, "link")); err != nil {
		t.Fatal(err)
	}
	if err := safefs.RemoveEmptyDir(d, "link", ident(t, filepath.Join(d, "link")), "", sb.Guard); err == nil {
		t.Error("symlink を消した")
	}
	write(t, filepath.Join(d, "f"), "")
	if err := safefs.RemoveEmptyDir(d, "f", ident(t, filepath.Join(d, "f")), "", sb.Guard); err == nil {
		t.Error("ファイルを消した")
	}
	for _, p := range []string{a, b, filepath.Join(d, "link"), filepath.Join(d, "f")} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s が消えた: %v", p, err)
		}
	}
}

// Guard が断ったら keep も消さない (検査は syscall の前)。
func TestRemoveEmptyDirGuardRunsFirst(t *testing.T) {
	d := tempDir(t)
	dir := filepath.Join(d, "x")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".keep-directory"), "")
	var ops []string
	deny := func(op, path string) error { ops = append(ops, op); return errors.New("deny") }
	if err := safefs.RemoveEmptyDir(d, "x", ident(t, dir), ".keep-directory", deny); err == nil {
		t.Fatal("Guard が断ったのに成功した")
	}
	if _, err := os.Lstat(filepath.Join(dir, ".keep-directory")); err != nil {
		t.Errorf("Guard が断ったのに keep が消えた: %v", err)
	}
	if len(ops) != 1 || ops[0] != "unlink" {
		t.Errorf("ops = %v", ops)
	}
	if err := safefs.RemoveEmptyDir(d, "x", ident(t, dir), ".keep-directory", nil); err == nil {
		t.Error("nil の Guard で成功した")
	}
	if _, err := os.Lstat(dir); err != nil {
		t.Errorf("消えた: %v", err)
	}
}

// 長さ 0 でも拡張属性で中身を持つ keep と、許していない拡張属性の付いたフォルダは消さない。
func TestRemoveEmptyDirRefusesXattrs(t *testing.T) {
	d := tempDir(t)
	k := filepath.Join(d, "k")
	if err := os.Mkdir(k, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(k, ".keep-directory"), "")
	if err := unix.Setxattr(filepath.Join(k, ".keep-directory"), "user.secret", []byte("important"), 0); err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(d, "g")
	if err := os.Mkdir(g, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(g, "user.secret", []byte("important"), 0); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{k, g} {
		if err := safefs.RemoveEmptyDir(d, filepath.Base(p), ident(t, p), ".keep-directory", sb.Guard); !errors.Is(err, safefs.ErrNotEmpty) {
			t.Errorf("%s: err = %v, want ErrNotEmpty", p, err)
		}
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s が消えた", p)
		}
	}
	if _, err := os.Lstat(filepath.Join(k, ".keep-directory")); err != nil {
		t.Error("xattr の付いた keep が消えた")
	}
}
