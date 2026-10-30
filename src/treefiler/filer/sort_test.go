package filer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSortOrderLess(t *testing.T) {
	t0 := time.Unix(1_800_000_000, 0)
	f := func(name string, age time.Duration, size int64) *node {
		return &node{raw: name, lower: strings.ToLower(name), mtime: t0.Add(-age), size: size}
	}
	d := func(name string, age time.Duration) *node {
		return &node{raw: name, lower: strings.ToLower(name), dir: true, mtime: t0.Add(-age), size: 4096}
	}
	old, mid, newf := f("b-old", 3*time.Hour, 900), f("c-mid", 2*time.Hour, 10), f("a-new", time.Hour, 50)
	dirOld, dirNew := d("z-dir", 5*time.Hour), d("y-dir", 0)
	cases := []struct {
		name string
		o    sortOrder
		in   []*node
		want string
	}{
		{"name", sortOrder{by: "name"}, []*node{old, mid, newf}, "a-new,b-old,c-mid"},
		{"modified は新しい順", sortOrder{by: "modified"}, []*node{old, mid, newf}, "a-new,c-mid,b-old"},
		{"size は大きい順", sortOrder{by: "size"}, []*node{old, mid, newf}, "b-old,a-new,c-mid"},
		{"size ではフォルダは 0 (名前順で下)", sortOrder{by: "size"}, []*node{dirOld, mid, dirNew}, "c-mid,y-dir,z-dir"},
		{"modified ではフォルダも自身の mtime", sortOrder{by: "modified"}, []*node{dirOld, old, dirNew}, "y-dir,b-old,z-dir"},
		{"folders first は Sort by の外", sortOrder{by: "modified", foldersFirst: true}, []*node{newf, dirOld, dirNew}, "y-dir,z-dir,a-new"},
		{"reverse は向きだけ逆", sortOrder{by: "modified", reverse: true}, []*node{old, mid, newf}, "b-old,c-mid,a-new"},
		{"reverse でも folders first は上", sortOrder{by: "size", reverse: true, foldersFirst: true}, []*node{old, dirNew, newf}, "y-dir,a-new,b-old"},
		{"同じ値は名前", sortOrder{by: "size"}, []*node{f("b", 0, 5), f("a", 0, 5)}, "a,b"},
	}
	for _, c := range cases {
		ks := append([]*node(nil), c.in...)
		sortKids(ks, c.o)
		if got := names(ks); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// stampA は a/ の 3 つの mtime をずらす (fixture は全部同じ時刻)。新しい順: two.go, three.md, one.txt。大きさの順: three.md (54), two.go (12), one.txt (6)。
func stampA(t *testing.T, m *Model) {
	t.Helper()
	for name, age := range map[string]time.Duration{"two.go": time.Minute, "three.md": time.Hour, "one.txt": 2 * time.Hour} {
		at := fixedNow.Add(-age)
		if err := os.Chtimes(filepath.Join(m.root.abs, "a", name), at, at); err != nil {
			t.Fatal(err)
		}
	}
	m.Refresh()
}

func aKids(t *testing.T, m *Model) string {
	t.Helper()
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	if a == nil || !a.loaded {
		t.Fatal("a が読まれていない")
	}
	return names(a.kids)
}

// S は Sort by を name → modified → size → name と巡回し、その場で並べ直す。
func TestSortKeyCycles(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	stampA(t, m)
	want := []struct{ by, kids string }{
		{"modified", "two.go,three.md,one.txt"},
		{"size", "three.md,two.go,one.txt"},
		{"name", "one.txt,three.md,two.go"},
	}
	for _, w := range want {
		m.HandleKey("S")
		if m.set.SortBy != w.by || aKids(t, m) != w.kids {
			t.Fatalf("S の後 Sort by = %s / a の並び = %s, want %s / %s", m.set.SortBy, aKids(t, m), w.by, w.kids)
		}
		if ns := m.TakeNotices(); len(ns) != 1 || !ns[0].OK || ns[0].Text != "並び: "+sortByLabel[w.by] {
			t.Fatalf("S の後の知らせ = %+v", ns)
		}
	}
}

// Sort by と Reverse は保存され、作り直した Model で効く。
func TestSortSettingsPersist(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	stampA(t, m)
	m.set.SortBy, m.set.Reverse = "modified", true
	m.applySettings("sort_by")
	m.applySettings("reverse")
	if m.saveErr != "" {
		t.Fatal(m.saveErr)
	}
	m2 := newAt(t, m.root.abs)
	cdTo(t, m2, "a/one.txt")
	if m2.set.SortBy != "modified" || !m2.set.Reverse || aKids(t, m2) != "one.txt,three.md,two.go" {
		t.Fatalf("作り直した Model: Sort by = %s / Reverse = %v / a の並び = %s", m2.set.SortBy, m2.set.Reverse, aKids(t, m2))
	}
}

// modified で並べている間に外でファイルが更新されたら、読み直しで並びが変わる (読み込みと同じ並べ方を通る)。
func TestSortModifiedFollowsReload(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	stampA(t, m)
	m.set.SortBy = "modified"
	m.applySettings("sort_by")
	at := fixedNow
	if err := os.Chtimes(filepath.Join(m.root.abs, "a", "one.txt"), at, at); err != nil {
		t.Fatal(err)
	}
	m.Refresh()
	if got := aKids(t, m); got != "one.txt,two.go,three.md" {
		t.Fatalf("読み直した後の並び = %s", got)
	}
	if m.cur.raw != "one.txt" {
		t.Fatalf("カーソルが項目に付いていかない: %s", m.cur.raw)
	}
}

// 等価性のテスト (virt_test.go) の fixture は、modified / size の並びが名前順と違う (揃っていると並び替えの操作が検査にならない)。
func TestVirtFixtureSortOrdersDiffer(t *testing.T) {
	vp := newVirtPair(t)
	big := vp.ref.findNode(filepath.Join(vp.dir, "big"))
	if big == nil || !big.loaded {
		t.Fatal("big が読まれていない")
	}
	byName := names(big.kids)
	for _, want := range []string{"modified", "size"} {
		vp.ref.HandleKey("S")
		if vp.ref.set.SortBy != want {
			t.Fatalf("S の後 = %s, want %s", vp.ref.set.SortBy, want)
		}
		if names(big.kids) == byName {
			t.Fatalf("%s の並びが名前順と同じ (fixture の mtime / 大きさがずれていない)", want)
		}
	}
}

// 設定の板で Reverse を切り替えると、同じ Model のまま並べ直す (作り直した Model は読み込みで並べるので、それだけでは並べ直しを検査しない)。
func TestSortReverseResortsInPlace(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	stampA(t, m)
	m.set.SortBy = "modified"
	m.applySettings("sort_by")
	m.set.Reverse = true
	m.applySettings("reverse")
	if got := aKids(t, m); got != "one.txt,three.md,two.go" {
		t.Fatalf("Reverse の後の並び = %s", got)
	}
}

// root を 1 段上げた後も、旧 root の項目は今の mtime で並ぶ (新しい親は読み直した値で並べ、旧 root の node へ差し替える)。
func TestSortAfterRerootUsesFreshMtime(t *testing.T) {
	isolateConfig(t)
	p := t.TempDir()
	a, b := filepath.Join(p, "a"), filepath.Join(p, "b")
	mustWriteAll(t, filepath.Join(a, "x.txt"))
	mustWriteAll(t, filepath.Join(b, "y.txt"))
	for d, age := range map[string]time.Duration{a: 3 * time.Hour, b: 2 * time.Hour} {
		at := fixedNow.Add(-age)
		if err := os.Chtimes(d, at, at); err != nil {
			t.Fatal(err)
		}
	}
	m := newAt(t, a)
	m.set.Live = false
	m.applySettings("live")
	at := fixedNow.Add(-time.Hour) // 開いた後で a を b より新しくする
	if err := os.Chtimes(a, at, at); err != nil {
		t.Fatal(err)
	}
	m.set.SortBy = "modified"
	m.applySettings("sort_by")
	m.HandleKey("-")
	pn := m.findNode(p)
	if pn == nil || names(pn.kids) != "a,b" {
		t.Fatalf("1 段上げた直後の並び = %v", pn)
	}
	m.set.Reverse = !m.set.Reverse
	m.applySettings("reverse")
	m.set.Reverse = !m.set.Reverse
	m.applySettings("reverse")
	if got := names(pn.kids); got != "a,b" {
		t.Fatalf("並べ直した後の並び = %s (旧 root の node が開いたときの mtime のまま)", got)
	}
}

// 保存できなかったときは ✗ の知らせだけを出し、「並び: …」の成功の知らせは出さない。
func TestSortKeySaveFailureShowsOnlyFailure(t *testing.T) {
	m := newTest(t)
	bad := filepath.Join(t.TempDir(), "file")
	mustWrite(t, bad, "x")
	t.Setenv("TREEFILER_CONFIG_DIR", filepath.Join(bad, "sub")) // ファイルの下には作れない
	m.HandleKey("S")
	ns := m.TakeNotices()
	if len(ns) != 1 || ns[0].OK {
		t.Fatalf("知らせ = %+v", ns)
	}
}

// 名前順は大文字小文字を無視する (読み込んだノード = newNodeAttr の作り方で並べる)。
func TestNameOrderIgnoresCase(t *testing.T) {
	var kids []*node
	for _, n := range []string{"C10", "B", "c9", "a", "b"} {
		kids = append(kids, newNodeAttr(n, entryAttr{}, nil))
	}
	sortKids(kids, sortOrder{natural: true})
	var got []string
	for _, k := range kids {
		got = append(got, k.raw)
	}
	if s := strings.Join(got, ","); s != "a,B,b,c9,C10" {
		t.Fatalf("got %s, want a,B,b,c9,C10 (大文字小文字を無視・数字は数として・同じなら元の名前)", s)
	}
}
