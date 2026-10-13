package filer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goTo は `:` で入力欄を開いて text を入れて Enter する。
func goTo(t *testing.T, m *Model, text string) {
	t.Helper()
	m.HandleKey(":")
	if !m.OwnsKeys() || m.prompt.kind != promptGoto {
		t.Fatalf(": で移動の入力欄が開かない: %+v", m.prompt)
	}
	setLine(m, text)
	m.HandleKey("enter")
	if m.prompt.active {
		t.Fatal("Enter で入力欄が閉じない")
	}
}

// 相対パスは root から。途中のフォルダ (まだ読んでいない) を開いてカーソルを置く。
func TestGotoRelativeOpensAncestors(t *testing.T) {
	m := newTest(t)
	root := m.root
	goTo(t, m, "b/deep/x.txt")
	if m.root != root {
		t.Fatalf("root の中なのに root が変わった: %s", m.root.abs)
	}
	if want := filepath.Join(root.abs, "b", "deep", "x.txt"); m.cur.abs != want {
		t.Fatalf("カーソル = %s, want %s", m.cur.abs, want)
	}
	for a := m.cur.parent; a != nil; a = a.parent {
		if !a.expanded {
			t.Errorf("%s が開いていない", a.abs)
		}
	}
	if ns := m.TakeNotices(); len(ns) != 0 {
		t.Errorf("成功したのに知らせが出た: %+v", ns)
	}
	// dotfile を隠していても、カーソルへの経路として見える
	goTo(t, m, filepath.Join(root.abs, ".hidden"))
	if m.cur.raw != ".hidden" || !strings.Contains(names(m.kids(m.root)), ".hidden") {
		t.Fatalf("隠した dotfile へ移れない / 見えない: cur=%s kids=%s", m.cur.abs, names(m.kids(m.root)))
	}
}

// root を symlink 越しの別の綴り (macOS の /private/var) で書いても root の中として扱う (root を上げない)。
func TestGotoRealSpellingOfRootStaysInside(t *testing.T) {
	m := newTest(t)
	real, err := filepath.EvalSymlinks(m.root.abs)
	if err != nil {
		t.Fatal(err)
	}
	if real == m.root.abs {
		t.Skipf("一時ディレクトリが symlink 越しでない (%s)。macOS の /var → /private/var を前提にしたテスト", real)
	}
	root := m.root
	goTo(t, m, filepath.Join(real, "a", "two.go"))
	if m.root != root || m.cur.abs != filepath.Join(root.abs, "a", "two.go") {
		t.Fatalf("root = %s cur = %s (root を保ったまま a/two.go へ移ること)", m.root.abs, m.cur.abs)
	}
}

// root の外は共通の祖先まで root を上げる。開いていたフォルダは開いたまま。
func TestGotoOutsideRaisesRootToCommonAncestor(t *testing.T) {
	m := newTest(t)
	old := m.root
	cdTo(t, m, "a")
	m.HandleKey("space") // a を開く
	a := m.cur
	if !a.expanded {
		t.Fatal("a が開かない")
	}
	parent := filepath.Dir(old.abs)
	target := filepath.Join(parent, "other", "z.txt")
	mkfile(t, target, "z\n")
	goTo(t, m, target)
	if m.root.abs != parent {
		t.Fatalf("root = %s, want 共通の祖先 %s", m.root.abs, parent)
	}
	if m.cur.abs != target {
		t.Fatalf("カーソル = %s, want %s", m.cur.abs, target)
	}
	if !m.inTree(old) || !m.inTree(a) || !a.expanded {
		t.Errorf("旧 root と開いていた a が木に残っていない (inTree old=%v a=%v expanded=%v)", m.inTree(old), m.inTree(a), a.expanded)
	}
}

// 無いパスでは root もカーソルも動かさず、理由を知らせる。空欄の Enter と Esc は何もしない。
func TestGotoMissingOrEmptyKeepsPlace(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "c.txt")
	root, cur := m.root, m.cur
	goTo(t, m, filepath.Join(filepath.Dir(root.abs), "nope", "x.txt"))
	if m.root != root || m.cur != cur {
		t.Fatalf("無いパスで動いた: root=%s cur=%s", m.root.abs, m.cur.abs)
	}
	if n := lastNotice(t, m); n.OK || !strings.Contains(n.Text, "移動できません") {
		t.Errorf("知らせ = %+v", n)
	}
	goTo(t, m, "  ")
	m.HandleKey(":")
	setLine(m, "a/one.txt")
	m.HandleKey("esc")
	if m.prompt.active || m.root != root || m.cur != cur {
		t.Fatalf("空欄 / Esc で動いた: cur=%s", m.cur.abs)
	}
	if ns := m.TakeNotices(); len(ns) != 0 {
		t.Errorf("空欄 / Esc で知らせが出た: %+v", ns)
	}
}

// `~` は家のフォルダ ($HOME)。貼り付けた行末の改行は落とす。
func TestGotoTildeIsHome(t *testing.T) {
	m := newTest(t)
	t.Setenv("HOME", filepath.Join(m.root.abs, "b"))
	m.HandleKey(":")
	m.Paste("~/deep/x.txt\n")
	m.HandleKey("enter")
	if want := filepath.Join(m.root.abs, "b", "deep", "x.txt"); m.cur.abs != want {
		t.Fatalf("カーソル = %s, want %s", m.cur.abs, want)
	}
	if _, err := os.Lstat(m.cur.abs); err != nil {
		t.Fatal(err)
	}
}

// 在るが木に載らないパスでは、root を上げた後でも上げる前へ戻す (issue 746 の敵対レビュー: 戻さず / まで上がっていた)。
func TestGotoExistingButNotInTreeKeepsRoot(t *testing.T) {
	m := newTest(t)
	root, cur := m.root, m.cur
	rootRaw := root.raw
	parent := filepath.Dir(root.abs)
	elsewhere := t.TempDir()
	mkfile(t, filepath.Join(elsewhere, "f.txt"), "f\n")
	if err := os.Symlink(elsewhere, filepath.Join(parent, "ln")); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"root の外の symlink のフォルダ越し": filepath.Join(parent, "ln", "f.txt"),
		"root の中の symlink のフォルダ越し": filepath.Join(root.abs, "lnb", "deep", "x.txt"),
	}
	if err := os.Symlink(filepath.Join(root.abs, "b"), filepath.Join(root.abs, "lnb")); err != nil {
		t.Fatal(err)
	}
	if real, err := filepath.EvalSymlinks(root.abs); err == nil && real != root.abs {
		// 実の綴りで書いても、root の中の symlink は解かない (解くと b/deep/x.txt へ黙って飛んでいた)
		cases["root の中の symlink を実の綴りで"] = filepath.Join(real, "lnb", "deep", "x.txt")
		// root 自身・root の親を指す root の中の symlink も解かない (頭の一致を最長で取ると self / up が root に化けた)
		cases["root を指す root の中の symlink を実の綴りで"] = filepath.Join(real, "self", "a", "two.go")
		cases["root の親を指す root の中の symlink を実の綴りで"] = filepath.Join(real, "up", filepath.Base(root.abs), "c.txt")
	}
	for name, to := range map[string]string{"self": root.abs, "up": parent} {
		if err := os.Symlink(to, filepath.Join(root.abs, name)); err != nil {
			t.Fatal(err)
		}
	}
	if up := strings.ToUpper(filepath.Join(root.abs, "a", "two.go")); up != filepath.Join(root.abs, "a", "two.go") {
		if _, err := os.Lstat(up); err == nil { // 大文字小文字を区別しないファイルシステム (macOS の既定) でだけ在る
			cases["大文字小文字の違う綴り"] = up
		}
	}
	for name, p := range cases {
		goTo(t, m, p)
		if m.root != root || m.root.raw != rootRaw || m.root.parent != nil || m.cur != cur || !m.inTree(cur) {
			t.Errorf("%s: root=%s (raw=%q parent=%v) cur=%s (動かないこと)", name, m.root.abs, m.root.raw, m.root.parent != nil, m.cur.abs)
		}
		if n := lastNotice(t, m); n.OK || !strings.Contains(n.Text, "見つかりません") {
			t.Errorf("%s: 知らせ = %+v", name, n)
		}
		m.root, m.cur = root, cur // 次のケースを同じ場所から
	}
	if len(cases) < 6 {
		t.Logf("この環境で試せたのは %d ケース", len(cases))
	}
}

// root を上げる途中で上がれなくなったら、途中で止めずに上げる前へ戻す。
func TestGotoRerootFailingHalfwayRestoresRoot(t *testing.T) {
	isolateConfig(t)
	base := t.TempDir()
	noread := filepath.Join(base, "noread")
	mkfile(t, filepath.Join(noread, "inner", "deep", "f.txt"), "f\n")
	mkfile(t, filepath.Join(base, "other", "g.txt"), "g\n")
	m := newAt(t, filepath.Join(noread, "inner", "deep"))
	m.Resize(120, 40)
	settle(m)
	if err := os.Chmod(noread, 0o311); err != nil { // 辿れるが読めない: inner から noread へは上がれない
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(noread, 0o755) })
	root, cur := m.root, m.cur
	goTo(t, m, filepath.Join(base, "other", "g.txt"))
	if m.root != root || m.root.parent != nil || m.cur != cur {
		t.Fatalf("root = %s cur = %s (上げる前へ戻ること)", m.root.abs, m.cur.abs)
	}
	if n := lastNotice(t, m); n.OK || !strings.Contains(n.Text, "1 段上へ行けません") {
		t.Errorf("知らせ = %+v", n)
	}
}

// root を指す symlink そのもの (root の外) は root と見なさず、その symlink の項目へ移る (末尾は辿らない)。
func TestGotoSymlinkToRootIsTheLinkItself(t *testing.T) {
	m := newTest(t)
	old := m.root
	parent := filepath.Dir(old.abs)
	link := filepath.Join(parent, "toroot")
	if err := os.Symlink(old.abs, link); err != nil {
		t.Fatal(err)
	}
	goTo(t, m, link)
	if m.root.abs != parent || m.cur.abs != link || m.cur.dir {
		t.Fatalf("root = %s cur = %s dir=%v (symlink の項目へ移ること)", m.root.abs, m.cur.abs, m.cur.dir)
	}
}
