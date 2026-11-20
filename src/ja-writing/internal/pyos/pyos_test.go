package pyos_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"ja-writing/internal/pyfs"
	"ja-writing/internal/pyos"
)

// dcff は本家の str の '\udcff' (OS から受けた 0xff を os.fsdecode した字) を WTF-8 で書いたもの。
var dcff = string(pyfs.AppendSurrogate(nil, 0xDCFF))

// 誤りの Path は、OS に渡したバイト (0xff) でなく渡された字にする。誤りの文に出す字が本家の str (標準エラーで \udcff になる) と同じになる。
func TestErrorsKeepGivenSpelling(t *testing.T) {
	dir := t.TempDir()
	given := filepath.Join(dir, "z"+dcff)
	checks := map[string]error{}
	_, checks["Stat"] = pyos.Stat(given)
	_, checks["Lstat"] = pyos.Lstat(given)
	_, checks["Open"] = pyos.Open(given)
	_, checks["ReadFile"] = pyos.ReadFile(given)
	_, checks["ReadDir"] = pyos.ReadDir(given)
	_, checks["Readlink"] = pyos.Readlink(given)
	checks["Chmod"] = pyos.Chmod(given, 0o600)
	checks["Remove"] = pyos.Remove(given)
	checks["Mkdir"] = pyos.Mkdir(given, 0o700)
	for name, err := range checks {
		var pe *fs.PathError
		if !errors.As(err, &pe) || pe.Path != given {
			t.Errorf("%s: %#v (Path は渡した字 %q のはず)", name, err, given)
		}
	}
	// 本家と同じ errno: 0xff の名前は APFS に無い (ENOENT) し、作れない (EILSEQ)。python3.15 の os.stat・os.mkdir(b"z\xff") で実測
	if !errors.Is(checks["Stat"], fs.ErrNotExist) || !errors.Is(checks["Mkdir"], syscall.EILSEQ) {
		t.Errorf("errno が本家と違う: stat %v、mkdir %v", checks["Stat"], checks["Mkdir"])
	}
	err := pyos.Link(filepath.Join(dir, "none"), given)
	var le *os.LinkError
	if !errors.As(err, &le) || le.Old != filepath.Join(dir, "none") || le.New != given {
		t.Errorf("Link: %#v", err)
	}
}

// OS から受けた名前 (リンク先) は os.fsdecode した字にする。python3.15 の os.readlink は b"z\xff" のリンク先を 'z\udcff' で返す。
func TestReadlinkDecodesLikeFsdecode(t *testing.T) {
	link := filepath.Join(t.TempDir(), "l")
	if err := os.Symlink("z\xff", link); err != nil {
		t.Fatal(err)
	}
	got, err := pyos.Readlink(link)
	if err != nil || got != "z"+dcff {
		t.Fatalf("% x %v、本家は 'z\\udcff'", got, err)
	}
}
