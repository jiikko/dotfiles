package safefs_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"safefs"
	"safefs/sandbox"
)

// sb はこのパッケージのテストの検査点。操作は全部 sb.Guard を通し、登録した一時ディレクトリの外は実行前に拒否される。
var sb sandbox.Sandbox

func TestMain(m *testing.M) {
	code := m.Run()
	if v := sb.Violations(); len(v) > 0 {
		fmt.Fprintf(os.Stderr, "sandbox が拒否した操作が残っています (テストが一時ディレクトリの外を触ろうとした):\n  %s\n", strings.Join(v, "\n  "))
		code = 1
	}
	os.Exit(code)
}

func tempDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	sb.Allow(t, d)
	return d
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ident(t *testing.T, p string) (uint64, uint64) {
	t.Helper()
	d, i, err := safefs.Identity(p)
	if err != nil {
		t.Fatal(err)
	}
	return d, i
}

func TestRenameMovesWithinDir(t *testing.T) {
	d := tempDir(t)
	write(t, filepath.Join(d, "a"), "A")
	dev, ino := ident(t, filepath.Join(d, "a"))
	if err := safefs.Rename(d, "a", "b", dev, ino, sb.Guard); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(d, "b")); got != "A" {
		t.Errorf("b = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(d, "a")); !os.IsNotExist(err) {
		t.Errorf("a が残った: %v", err)
	}
}

// 既にある名前へは変えない (上書きで中身を失わない)。
func TestRenameDoesNotOverwrite(t *testing.T) {
	d := tempDir(t)
	write(t, filepath.Join(d, "a"), "A")
	write(t, filepath.Join(d, "b"), "B")
	dev, ino := ident(t, filepath.Join(d, "a"))
	err := safefs.Rename(d, "a", "b", dev, ino, sb.Guard)
	if !errors.Is(err, safefs.ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if read(t, filepath.Join(d, "b")) != "B" || read(t, filepath.Join(d, "a")) != "A" {
		t.Error("上書きした / 移した")
	}
}

// 空でないフォルダへの名前の変更も上書きしない (RENAME_EXCL は非空のフォルダでも EEXIST)。
func TestRenameDoesNotReplaceDir(t *testing.T) {
	d := tempDir(t)
	if err := os.MkdirAll(filepath.Join(d, "dst"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(d, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	dev, ino := ident(t, filepath.Join(d, "src"))
	if err := safefs.Rename(d, "src", "dst", dev, ino, sb.Guard); !errors.Is(err, safefs.ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
}

// 大文字小文字だけの変更は通る (区別しない APFS でも RENAME_EXCL が EEXIST にならない。ops.go の Rename の doc)。
func TestRenameCaseOnly(t *testing.T) {
	d := tempDir(t)
	write(t, filepath.Join(d, "readme"), "R")
	if _, err := os.Lstat(filepath.Join(d, "README")); err != nil {
		t.Skip("大文字小文字を区別する FS (この検査は区別しない APFS の振る舞いを見る)")
	}
	dev, ino := ident(t, filepath.Join(d, "readme"))
	if err := safefs.Rename(d, "readme", "README", dev, ino, sb.Guard); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "README" {
		t.Errorf("中身 = %v, want [README]", ents)
	}
	if read(t, filepath.Join(d, "README")) != "R" {
		t.Error("中身が変わった")
	}
}

// 決めたときと別の実体に差し替わっていたら変えない。
func TestRenameRefusesSwappedEntry(t *testing.T) {
	d := tempDir(t)
	write(t, filepath.Join(d, "a"), "A")
	dev, ino := ident(t, filepath.Join(d, "a"))
	if err := os.Remove(filepath.Join(d, "a")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d, "keep"), "K") // 消した a の inode が再利用されないよう、別の実体を挟む
	write(t, filepath.Join(d, "a"), "X")
	if d2, i2 := ident(t, filepath.Join(d, "a")); d2 == dev && i2 == ino {
		t.Skip("inode が再利用された")
	}
	if err := safefs.Rename(d, "a", "b", dev, ino, sb.Guard); err == nil {
		t.Fatal("差し替わった実体の名前を変えた")
	}
	if _, err := os.Lstat(filepath.Join(d, "b")); !os.IsNotExist(err) {
		t.Error("b ができた")
	}
}

func TestNamesStayInsideDir(t *testing.T) {
	d := tempDir(t)
	if err := os.Mkdir(filepath.Join(d, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d, "sub", "a"), "A")
	dev, ino := ident(t, filepath.Join(d, "sub", "a"))
	for _, bad := range []string{"", ".", "..", "../a", "x/y", "/abs", "nul\x00", strings.Repeat("n", 256)} {
		if err := safefs.Rename(filepath.Join(d, "sub"), "a", bad, dev, ino, sb.Guard); err == nil {
			t.Errorf("Rename to %q が通った", bad)
		}
		if err := safefs.Create(filepath.Join(d, "sub"), bad, false, sb.Guard); err == nil {
			t.Errorf("Create %q が通った", bad)
		}
	}
	ents, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Errorf("フォルダの外に何かできた: %v", ents)
	}
	if err := safefs.ValidName(strings.Repeat("n", 255)); err != nil {
		t.Errorf("255 バイトを断った: %v", err)
	}
}

func TestCreateFileAndDir(t *testing.T) {
	d := tempDir(t)
	if err := safefs.Create(d, "f", false, sb.Guard); err != nil {
		t.Fatal(err)
	}
	if err := safefs.Create(d, "g", true, sb.Guard); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(d, "f")); err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
		t.Errorf("f: %v %v", fi, err)
	}
	if fi, err := os.Lstat(filepath.Join(d, "g")); err != nil || !fi.IsDir() {
		t.Errorf("g: %v %v", fi, err)
	}
}

// 既にある項目は作り直さない (中身を空にしない)。symlink の先も作らない。
func TestCreateDoesNotOverwrite(t *testing.T) {
	d := tempDir(t)
	write(t, filepath.Join(d, "f"), "F")
	if err := os.Mkdir(filepath.Join(d, "g"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(d, "target"), filepath.Join(d, "link")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		isDir bool
	}{{"f", false}, {"f", true}, {"g", false}, {"g", true}, {"link", false}} {
		if err := safefs.Create(d, c.name, c.isDir, sb.Guard); !errors.Is(err, safefs.ErrExists) {
			t.Errorf("Create(%q, dir=%v) = %v, want ErrExists", c.name, c.isDir, err)
		}
	}
	if read(t, filepath.Join(d, "f")) != "F" {
		t.Error("f の中身が変わった")
	}
	if _, err := os.Lstat(filepath.Join(d, "target")); !os.IsNotExist(err) {
		t.Error("symlink の先を作った")
	}
}

// 検査点が拒否したら、syscall に届かない (何も変わらない)。nil の検査点も拒否として扱う。
func TestGuardRunsBeforeTheOperation(t *testing.T) {
	d := tempDir(t)
	write(t, filepath.Join(d, "a"), "A")
	dev, ino := ident(t, filepath.Join(d, "a"))
	var ops []string
	deny := func(op, path string) error { ops = append(ops, op); return errors.New("deny") }
	for _, g := range []safefs.Guard{deny, nil} {
		if err := safefs.Rename(d, "a", "b", dev, ino, g); err == nil {
			t.Error("Rename が拒否を無視した")
		}
		if err := safefs.Create(d, "c", false, g); err == nil {
			t.Error("Create が拒否を無視した")
		}
		if _, err := safefs.TrashMove(filepath.Join(d, "a"), filepath.Join(d, "Trash"), dev, ino, g); err == nil {
			t.Error("TrashMove が拒否を無視した")
		}
	}
	if len(ops) == 0 {
		t.Error("検査点が呼ばれていない")
	}
	ents, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "a" {
		t.Errorf("拒否したのに変わった: %v", ents)
	}
}

func TestTrashMoveNumbersDuplicates(t *testing.T) {
	d := tempDir(t)
	trash := filepath.Join(d, "Trash")
	if err := os.Mkdir(trash, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(trash, "x"), "OLD")
	write(t, filepath.Join(d, "x"), "NEW")
	dev, ino := ident(t, filepath.Join(d, "x"))
	dst, err := safefs.TrashMove(filepath.Join(d, "x"), trash, dev, ino, sb.Guard)
	if err != nil {
		t.Fatal(err)
	}
	if dst != filepath.Join(trash, "x 2") || read(t, dst) != "NEW" || read(t, filepath.Join(trash, "x")) != "OLD" {
		t.Errorf("dst = %s", dst)
	}
}

func TestProductionGuardRefusesUnderTest(t *testing.T) {
	if err := safefs.ProductionGuard("create", "/nowhere"); err == nil {
		t.Error("テストのバイナリの中で素通りした")
	}
}

// 同じ実体の別名 (hardlink) へも変えない (宛先の名前が既にある)。
func TestRenameRefusesHardlinkName(t *testing.T) {
	d := tempDir(t)
	write(t, filepath.Join(d, "a"), "A")
	if err := os.Link(filepath.Join(d, "a"), filepath.Join(d, "b")); err != nil {
		t.Fatal(err)
	}
	dev, ino := ident(t, filepath.Join(d, "a"))
	if err := safefs.Rename(d, "a", "b", dev, ino, sb.Guard); !errors.Is(err, safefs.ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if ents, _ := os.ReadDir(d); len(ents) != 2 {
		t.Errorf("中身 = %v (a と b が残ること)", ents)
	}
}

// TrashMove は移動元も検査点に通す (ゴミ箱の側だけ許されても、移動元が許されなければ動かさない)。
func TestTrashMoveChecksSourceToo(t *testing.T) {
	d := tempDir(t)
	write(t, filepath.Join(d, "a"), "A")
	dev, ino := ident(t, filepath.Join(d, "a"))
	onlyDest := func(op, path string) error {
		if op == "trash-dest" {
			return nil
		}
		return errors.New("deny " + op)
	}
	if _, err := safefs.TrashMove(filepath.Join(d, "a"), filepath.Join(d, "Trash"), dev, ino, onlyDest); err == nil {
		t.Fatal("移動元の検査を通らずに移した")
	}
	if read(t, filepath.Join(d, "a")) != "A" {
		t.Error("a が動いた")
	}
}
