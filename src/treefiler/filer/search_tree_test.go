package filer

import (
	"math"
	"path/filepath"
	"slices"
	"testing"
)

// child は dir の見せる子のうち名前が raw のもの。
func child(t *testing.T, m *Model, dir *node, raw string) *node {
	t.Helper()
	for _, k := range m.kids(dir) {
		if k.raw == raw {
			return k
		}
	}
	t.Fatalf("%s の下に %s が無い (%s)", dir.raw, raw, names(m.kids(dir)))
	return nil
}

// 検索の対象は開いている木の全体: カーソルの列 (root の直下) から、開いたフォルダ a の中 (1 つ深い列) の一致へ飛ぶ。n も列をまたぐ
func TestSearchReachesMatchInOpenedFolderOfAnotherColumn(t *testing.T) {
	m := newTest(t)
	a := child(t, m, m.root, "a")
	m.setCur(a)
	m.HandleKey("l") // a を開いて中へ
	m.HandleKey("h") // root の直下の列へ戻る (a は開いたまま)
	settle(m)
	if m.cur != a || !a.expanded {
		t.Fatalf("前提: a の上に居て a が開いている (cur=%s expanded=%v)", m.cur.raw, a.expanded)
	}
	m.HandleKey("/")
	typeText(m, "three")
	if m.cur.raw != "three.md" {
		t.Fatalf("開いたフォルダの中の一致へ飛ばない: cur=%s", m.cur.raw)
	}
	m.HandleKey("enter")
	m.setCur(a)
	m.HandleKey("n")
	if m.cur.raw != "three.md" {
		t.Fatalf("n が開いたフォルダの中の一致へ行かない: cur=%s", m.cur.raw)
	}
}

// 開いていないフォルダの中は対象にしない (見えない項目へ飛ばない・木を開かない)。一度開いて閉じたフォルダ (子は読み込み済み) で見る
func TestSearchSkipsInsideClosedFolders(t *testing.T) {
	m := newTest(t)
	b := child(t, m, m.root, "b")
	m.setCur(b)
	m.HandleKey("l") // b を開いて子を読み込む
	m.HandleKey("h")
	m.HandleKey("space") // 閉じる
	settle(m)
	if b.expanded || len(b.kids) == 0 {
		t.Fatalf("前提: b は閉じていて、子は読み込み済み (expanded=%v kids=%d)", b.expanded, len(b.kids))
	}
	origin := m.cur
	m.HandleKey("/")
	typeText(m, "deep")
	if m.cur != origin || b.expanded || len(m.search.matches) != 0 {
		t.Fatalf("閉じたフォルダの中に一致した: cur=%s b.expanded=%v matches=%s", m.cur.raw, b.expanded, names(m.search.matches))
	}
}

// 同じ段の一致は、起点から読み順で次に来るものを先に (起点が一致すれば起点)。上から数えると、下の方で始めた検索が遠くの上へ飛ぶ
func TestSearchSameRankStartsFromOrigin(t *testing.T) {
	m := newTest(t)
	f10 := child(t, m, m.root, "file10.txt")
	m.setCur(f10)
	m.HandleKey("/")
	typeText(m, "file")
	if m.cur != f10 {
		t.Fatalf("同じ段 (前方一致) の起点に留まらない: cur=%s matches=%s", m.cur.raw, names(m.search.matches))
	}
	m.HandleKey("tab")
	if m.cur.raw != "file2.txt" {
		t.Fatalf("Tab で読み順の次 (末尾を越えて先頭から) の file2 へ行かない: cur=%s", m.cur.raw)
	}
}

// ハイライトは一致の集合 (Tab で回る項目) と同じ項目だけ: より良い一致があるとき、文字が順に含まれるだけの項目は光らせない
func TestSearchHighlightsOnlyReachableMatches(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	for _, rel := range []string{"alpha.txt", "xaxl.txt", "zz.txt"} {
		mkfile(t, filepath.Join(root, rel), "x")
	}
	m := newAt(t, root)
	m.Resize(120, 40)
	settle(m)
	m.setCur(child(t, m, m.root, "zz.txt"))
	settle(m)
	m.HandleKey("/")
	typeText(m, "al")
	settle(m)
	lit := func(raw string) bool {
		n := child(t, m, m.root, raw)
		c := m.draw()
		a := m.anims[n]
		ox, oy := int(math.Round(m.camX.v)), int(math.Round(m.camY.v))
		x, y := int(math.Round(a.x.v))-ox, int(math.Round(a.y.v))-oy
		for k := range n.labelCells() {
			if p := c.at(x+k, y); p != nil && p.bg == cMatchBg {
				return true
			}
		}
		return false
	}
	if !lit("alpha.txt") {
		t.Fatal("前方一致の alpha が光らない (検査が何も見ていない)")
	}
	if lit("xaxl.txt") {
		t.Fatal("文字が順に含まれるだけの xaxl が光った (Tab では届かない)")
	}
}

// openTree は読み順 (親 → 開いた子 → 次の兄弟)。並びが崩れると、同じ段の一致を起点から回す順と n / N の次が変わる
func TestOpenTreeIsReadingOrder(t *testing.T) {
	m := newTest(t)
	a := child(t, m, m.root, "a")
	m.setCur(a)
	m.HandleKey("l")
	settle(m)
	var got []string
	for _, n := range m.openTree() {
		got = append(got, n.raw)
	}
	var want []string
	want = append(want, m.root.raw)
	for _, k := range m.kids(m.root) {
		want = append(want, k.raw)
		if k == a {
			for _, g := range m.kids(a) {
				want = append(want, g.raw)
			}
		}
	}
	if len(m.kids(a)) == 0 || !slices.Equal(got, want) {
		t.Fatalf("読み順でない:\n got %v\nwant %v", got, want)
	}
}

// Tab で、検索中に見えなくなった一致を外したら、ハイライトの集合からも外す (光っているのに届かない項目を残さない)
func TestStepMatchDropsHiddenMatchFromHighlight(t *testing.T) {
	m := newTest(t)
	a := child(t, m, m.root, "a")
	m.setCur(a)
	m.HandleKey("l")
	m.HandleKey("h")
	settle(m)
	m.HandleKey("/")
	typeText(m, "o") // a/one.txt・a/two.go と、root の直下の項目にも一致する
	one := child(t, m, a, "one.txt")
	if !m.search.hit[one] {
		t.Fatal("前提: a/one.txt が一致に入っている")
	}
	a.expanded = false // 検索中に a が畳まれた (ライブ更新・外の操作の代わり)
	m.HandleKey("tab")
	if m.search.hit[one] || slices.Contains(m.search.matches, one) {
		t.Fatalf("畳まれた a の中の一致が残った: hit=%v matches=%s", m.search.hit[one], names(m.search.matches))
	}
}
