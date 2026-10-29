package filer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newOps は fixture の Model と、sandbox に登録したゴミ箱を作る (操作は全部 sandbox の検査点を通る。main_test.go)。
func newOps(t *testing.T) (*Model, string) {
	t.Helper()
	m := newTest(t)
	return m, allowOps(t, m)
}

// allowOps は m の root と使い捨てのゴミ箱へのファイル操作を sandbox に許し、ゴミ箱の場所を返す。
func allowOps(t *testing.T, m *Model) string {
	t.Helper()
	sb.Allow(t, m.root.abs)
	trash := filepath.Join(t.TempDir(), "Trash")
	sb.Allow(t, filepath.Dir(trash))
	old := trashDir
	trashDir = func() string { return trash }
	t.Cleanup(func() { trashDir = old })
	return trash
}

func setLine(m *Model, s string) {
	m.prompt.line.Reset()
	typeText(m, s)
}

func lastNotice(t *testing.T, m *Model) Notice {
	t.Helper()
	ns := m.TakeNotices()
	if len(ns) == 0 {
		t.Fatal("知らせが無い")
	}
	return ns[len(ns)-1]
}

func TestCreateFileAndFolderMovesCursor(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("a")
	if !m.OwnsKeys() || m.prompt.kind != promptCreate || m.prompt.target.raw != "a" {
		t.Fatalf("a で a/ に作る入力欄が開かない: %+v", m.prompt)
	}
	setLine(m, "new.txt")
	m.HandleKey("enter")
	if fi, err := os.Lstat(filepath.Join(m.root.abs, "a", "new.txt")); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("new.txt ができていない: %v", err)
	}
	if m.cur.abs != filepath.Join(m.root.abs, "a", "new.txt") {
		t.Errorf("カーソル = %s (作った項目へ移ること)", m.cur.abs)
	}
	m.HandleKey("a")
	setLine(m, "sub/")
	m.HandleKey("enter")
	if fi, err := os.Lstat(filepath.Join(m.root.abs, "a", "sub")); err != nil || !fi.IsDir() {
		t.Fatalf("sub/ がフォルダでできていない: %v", err)
	}
	if !m.cur.dir || m.cur.raw != "sub" {
		t.Errorf("カーソル = %s", m.cur.abs)
	}
	// フォルダの上の a はその中に作る (まだ読んでいないフォルダでも)
	cdTo(t, m, "b")
	m.HandleKey("a")
	setLine(m, "inb")
	m.HandleKey("enter")
	if _, err := os.Lstat(filepath.Join(m.root.abs, "b", "inb")); err != nil || m.cur.abs != filepath.Join(m.root.abs, "b", "inb") {
		t.Fatalf("b の中に作られない / カーソルが移らない: %v %s", err, m.cur.abs)
	}
}

// 既にある名前は作り直さない・同じフォルダの外へ出る名前は断る。
func TestCreateRefusesExistingAndEscapingNames(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "a/one.txt")
	for _, name := range []string{"two.go", "two.go/", "../escape", "x/y", ".", "..", "", "/", "x//"} {
		m.HandleKey("a")
		setLine(m, name)
		m.HandleKey("enter")
		if n := lastNotice(t, m); n.OK {
			t.Errorf("%q を作った: %q", name, n.Text)
		}
	}
	if readBody(t, filepath.Join(m.root.abs, "a", "two.go")) != "package two\n" {
		t.Error("既にある two.go の中身が変わった")
	}
	for _, p := range []string{filepath.Join(m.root.abs, "escape"), filepath.Join(m.root.abs, "a", "x")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s ができた", p)
		}
	}
}

func TestRenameWithinFolder(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("m")
	if m.prompt.kind != promptRename || m.prompt.line.String() != "one.txt" {
		t.Fatalf("m で今の名前を入れた入力欄が開かない: %q", m.prompt.line.String())
	}
	if m.HandleKey("up"); m.prompt.line.String() != "one.txt" {
		t.Error("名前の入力で `!` の履歴が出た")
	}
	setLine(m, "uno.txt")
	m.HandleKey("enter")
	if readBody(t, filepath.Join(m.root.abs, "a", "uno.txt")) != "1\n2\n3\n" {
		t.Fatal("uno.txt に変わっていない")
	}
	if _, err := os.Lstat(filepath.Join(m.root.abs, "a", "one.txt")); !os.IsNotExist(err) {
		t.Error("one.txt が残った")
	}
	if m.cur.abs != filepath.Join(m.root.abs, "a", "uno.txt") {
		t.Errorf("カーソル = %s (変えた項目へ移ること)", m.cur.abs)
	}
}

// 既にある名前へは変えない・同じフォルダの外へは出さない。
func TestRenameRefusesOverwriteAndEscape(t *testing.T) {
	m, _ := newOps(t)
	for _, to := range []string{"two.go", "../escape", "x/y", "..", ""} {
		cdTo(t, m, "a/one.txt")
		m.HandleKey("m")
		setLine(m, to)
		m.HandleKey("enter")
		if n := lastNotice(t, m); n.OK {
			t.Errorf("%q へ変えた: %q", to, n.Text)
		}
	}
	if readBody(t, filepath.Join(m.root.abs, "a", "two.go")) != "package two\n" || readBody(t, filepath.Join(m.root.abs, "a", "one.txt")) != "1\n2\n3\n" {
		t.Error("上書きした / 動かした")
	}
	if _, err := os.Lstat(filepath.Join(m.root.abs, "escape")); !os.IsNotExist(err) {
		t.Error("フォルダの外へ動いた")
	}
}

// 名前に制御文字があると、入力欄には落とした形が入る。そのまま Enter しても名前は変えない。
func TestRenameUnchangedPrefillKeepsRawName(t *testing.T) {
	m, _ := newOps(t)
	raw := "name\x1b[31m.md"
	cdTo(t, m, raw)
	m.HandleKey("m")
	if m.prompt.line.String() == raw {
		t.Skip("入力欄が制御文字を落とさない (この検査の前提)")
	}
	m.HandleKey("enter")
	if _, err := os.Lstat(filepath.Join(m.root.abs, raw)); err != nil {
		t.Fatalf("開いたまま Enter で名前が変わった: %v", err)
	}
}

// 入力欄を開いた後に別の実体へ差し替わったら、名前を変えない。
func TestRenameRefusesSwappedEntry(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "c.txt")
	m.HandleKey("m")
	p := filepath.Join(m.root.abs, "c.txt")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(m.root.abs, "pin"), "x") // 消した inode の再利用を避ける
	mustWrite(t, p, "swapped")
	setLine(m, "d.txt")
	m.HandleKey("enter")
	if n := lastNotice(t, m); n.OK {
		t.Fatal("差し替わった実体の名前を変えた")
	}
	if readBody(t, p) != "swapped" {
		t.Error("差し替えた c.txt が動いた")
	}
}

func TestTrashAsksThenMovesAndMovesCursorToNeighbor(t *testing.T) {
	m, trash := newOps(t)
	cdTo(t, m, "c.txt")
	next := m.kids(m.root)[m.cur.vidx+1].abs
	m.HandleKey("d")
	if !m.OwnsKeys() || m.ask == nil {
		t.Fatal("d で確認の板が開かない")
	}
	if !strings.Contains(strings.Join(m.View(), "\n"), "ゴミ箱へ移す") {
		t.Error("確認の板が描かれていない")
	}
	m.HandleKey("n")
	if m.ask != nil || m.OwnsKeys() {
		t.Fatal("n で板が閉じない")
	}
	if _, err := os.Lstat(filepath.Join(m.root.abs, "c.txt")); err != nil {
		t.Fatal("取り消したのに移した")
	}
	m.HandleKey("d")
	m.HandleKey("y")
	if readBody(t, filepath.Join(trash, "c.txt")) != "c\n" {
		t.Fatal("ゴミ箱に c.txt が無い")
	}
	if _, err := os.Lstat(filepath.Join(m.root.abs, "c.txt")); !os.IsNotExist(err) {
		t.Fatal("c.txt が残った")
	}
	if m.cur.abs != next {
		t.Errorf("カーソル = %s, want %s (下の隣へ移ること)", m.cur.abs, next)
	}
	if n := lastNotice(t, m); !n.OK {
		t.Errorf("知らせ = %+v", n)
	}
}

// 末尾の項目を移したら上の隣へ。フォルダは中身ごと移る。
func TestTrashLastMovesCursorUpAndTakesFolderWhole(t *testing.T) {
	m, trash := newOps(t)
	cdTo(t, m, "b/deep")
	m.HandleKey("d")
	m.HandleKey("enter")
	if readBody(t, filepath.Join(trash, "deep", "x.txt")) != "x\n" {
		t.Fatal("フォルダが中身ごと移っていない")
	}
	if m.cur != m.findNode(filepath.Join(m.root.abs, "b")) {
		t.Errorf("唯一の子を移したらカーソルは親へ: %s", m.cur.abs)
	}
	ks := m.kids(m.root)
	cdTo(t, m, ks[len(ks)-1].raw)
	prev := ks[len(ks)-2].abs
	m.HandleKey("d")
	m.HandleKey("y")
	if m.cur.abs != prev {
		t.Errorf("末尾を移したらカーソルは上の隣へ: %s, want %s", m.cur.abs, prev)
	}
}

// 確認の板を開いた後に別の実体へ差し替わったら移さない。
func TestTrashRefusesSwappedEntry(t *testing.T) {
	m, trash := newOps(t)
	cdTo(t, m, "c.txt")
	m.HandleKey("d")
	p := filepath.Join(m.root.abs, "c.txt")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(m.root.abs, "pin"), "x")
	mustWrite(t, p, "swapped")
	m.HandleKey("y")
	if n := lastNotice(t, m); n.OK {
		t.Fatal("差し替わった実体を移した")
	}
	if readBody(t, p) != "swapped" {
		t.Error("差し替えた c.txt が動いた")
	}
	if _, err := os.Lstat(filepath.Join(trash, "c.txt")); !os.IsNotExist(err) {
		t.Error("ゴミ箱へ入った")
	}
}

func TestRootIsNotRenamedOrTrashed(t *testing.T) {
	m, trash := newOps(t)
	m.setCur(m.root)
	for _, k := range []string{"m", "d"} {
		m.HandleKey(k)
		if m.OwnsKeys() {
			t.Errorf("root の上の %s で層が開いた", k)
			m.HandleKey("esc")
		}
		if n := lastNotice(t, m); n.OK {
			t.Errorf("%s: %q", k, n.Text)
		}
	}
	if _, err := os.Lstat(trash); !os.IsNotExist(err) {
		t.Error("ゴミ箱に何か入った")
	}
}

// ctrl+c は入力欄と確認の板では取り消し (アプリを終えない)。
func TestCtrlCCancelsFileOpLayers(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "c.txt")
	for _, k := range []string{"a", "m", "d"} {
		m.HandleKey(k)
		if r := m.HandleKey("ctrl+c"); r == Quit || m.OwnsKeys() {
			t.Errorf("%s の層で ctrl+c = %v / 開いたまま", k, r)
		}
	}
	if _, err := os.Lstat(filepath.Join(m.root.abs, "c.txt")); err != nil {
		t.Error("取り消したのに移した")
	}
}

func TestPasteGoesToNameInput(t *testing.T) {
	m, _ := newOps(t)
	cdTo(t, m, "c.txt")
	m.HandleKey("a")
	m.Paste("pasted.txt")
	if m.prompt.line.String() != "pasted.txt" {
		t.Fatalf("貼り付けが入力欄に入らない: %q", m.prompt.line.String())
	}
	if _, _, ok := m.CaretPos(); !ok {
		t.Error("名前の入力中にキャレットが無い")
	}
}

// 木を読んだ後で途中のフォルダが外を指す symlink に差し替わっても、古い項目越しに root の外を触らない
// (再現: a を退けて外への symlink を置くと、a/one.txt の d / m / a が外のファイルに効いた)。
func TestOpsDoNotFollowSwappedSymlinkFolder(t *testing.T) {
	for _, k := range []string{"d", "m", "a"} {
		m, trash := newOps(t)
		outside := t.TempDir()
		sb.Allow(t, outside) // 外も登録して、効いてしまったら砂場ではなくこの検査で見る
		mustWrite(t, filepath.Join(outside, "one.txt"), "OUTSIDE")
		cdTo(t, m, "a/one.txt")
		a := filepath.Join(m.root.abs, "a")
		if err := os.Rename(a, a+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, a); err != nil {
			t.Fatal(err)
		}
		m.HandleKey(k)
		if m.OwnsKeys() {
			switch k {
			case "d":
				m.HandleKey("y")
			case "m":
				setLine(m, "renamed")
				m.HandleKey("enter")
			case "a":
				setLine(m, "newfile")
				m.HandleKey("enter")
			}
		}
		ents, err := os.ReadDir(outside)
		if err != nil {
			t.Fatal(err)
		}
		if len(ents) != 1 || ents[0].Name() != "one.txt" || readBody(t, filepath.Join(outside, "one.txt")) != "OUTSIDE" {
			t.Errorf("%s: root の外が変わった: %v", k, ents)
		}
		if _, err := os.Lstat(trash); err == nil {
			if es, _ := os.ReadDir(trash); len(es) > 0 {
				t.Errorf("%s: ゴミ箱に入った: %v", k, es)
			}
		}
	}
}

// ファイル操作の検査点は砂場に差し替わっていて、登録していない場所を実行前に拒否して記録する (main_test.go の配線。差し替え忘れ・
// 「何でも許す」への書き換えでは他のテストが緑のままだった)。
func TestFileOpGuardIsSandboxed(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "unregistered", "x") // 登録しない (一時ディレクトリの中なので、拒否されなくても本物は触らない)
	if err := fsGuard("create", outside); err == nil {
		t.Fatal("fsGuard が登録していない場所を通した (砂場に差し替わっていない)")
	}
	v := sb.Violations() // 記録を取り出して空にする (残すと TestMain がスイートを落とす)
	if len(v) != 1 || !strings.Contains(v[0], "create") {
		t.Fatalf("拒否が記録されていない: %v", v)
	}
}
