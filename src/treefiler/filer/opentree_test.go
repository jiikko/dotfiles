package filer

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// layoutAll は仮想化せずに全ノードを配置する (正解役だけに使う)。
func (m *Model) layoutAll() map[*node]place { return m.layoutWith(false) }

// maxFixtureNodes はこの検査が扱う木の節の数の上限。fixture は数十節で、これを超えたら木が fixture の外 (実在の dir) へ広がっている。
// 正解役の配置は打鍵ごとに木全体を作り直すので、外の dir が大きい (開発機の $TMPDIR は 5 万件に達した) と終わらない。
const maxFixtureNodes = 300

// escapedFixture は木が fixture の外へ広がったか (節の数が上限を超えたか) と、その数。
func escapedFixture(m *Model) (int, bool) {
	n := len(m.layoutAll())
	return n, n > maxFixtureNodes
}

// capAscent は root にカーソルがあるときの h (root を 1 段上げる) を、root が limit (fixture の親) なら j に替える。
// fixture の親は t.TempDir が作る fixture だけを入れた dir までで、そこから上は実在の $TMPDIR になる。
// 1 段目 (fixture の親へ上がる) は通すので、root を上げる動きは検査に残る。
func capAscent(m *Model, limit, k string) string {
	if k == "h" && m.cur.parent == nil && m.root.path() == limit {
		return "j"
	}
	return k
}

// 列 (検索の対象) は、配置を作って y で並べた列と同じ項目・同じ順。開閉・dotfile・経路の上の dotfile・並びの設定を乱数で混ぜる。
// TestOpenTreeMatchesLayout は、検索の対象 (openTree) が、仮想化せずに配置した項目 (描く項目) とちょうど同じ集合であることを、
// 乱数の操作列 (開閉・移動・隠しファイル・並べ替え) の後ごとに確かめる。検索で届く項目と光る項目を揃える前提 (spec §6.2)。
func TestOpenTreeMatchesLayout(t *testing.T) {
	keys := []string{"j", "k", "J", "K", "g", "G", "h", "l", "l", "space", "tab", "c", "C", ".", "S"}
	for seed := range uint64(40) {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			root := fixture(t)
			for _, rel := range []string{"a/sub/x.txt", "a/sub/.dot/y.txt", "a/.h2/z.txt", "b/deep/more/w.txt", "b/e.txt", ".hd/q.txt", "c2/f.txt"} {
				mkfile(t, filepath.Join(root, rel), "x")
			}
			m := newAt(t, root)
			m.Resize(120, 40)
			limit := filepath.Dir(root)
			rng := rand.New(rand.NewPCG(seed, 723))
			var log []string
			for range 60 {
				k := keys[rng.IntN(len(keys))]
				if rng.IntN(5) == 0 {
					m.set.FoldersFirst = !m.set.FoldersFirst
					m.applySettings("folders_first")
					k = "folders_first"
				} else {
					k = capAscent(m, limit, k)
					m.HandleKey(k)
				}
				log = append(log, k)
				if n, escaped := escapedFixture(m); escaped {
					t.Fatalf("%v の後に木が fixture の外へ広がった: %d 節 (root=%s)。正解役は打鍵ごとに全体を配置し、外の dir が大きいと終わらない", log, n, m.root.path())
				}
				want := maps(m.layoutAll())
				got := m.openTree()
				if len(want) == 0 {
					t.Fatal("配置が空 (検査が何も見ていない)")
				}
				byPath := func(ns []*node) []string {
					out := make([]string, len(ns))
					for i, n := range ns {
						out[i] = n.abs
					}
					sort.Strings(out)
					return out
				}
				if w, g := byPath(want), byPath(got); !slices.Equal(w, g) {
					t.Fatalf("%v の後の開いた木と配置が食い違う:\n got %s\nwant %s", log, g, w)
				}
			}
		})
	}
}

// maps は配置の map の鍵を写す (layoutAll の map は次の配置で中身が変わる使い回しの入れ物)。
func maps(pos map[*node]place) []*node {
	ns := make([]*node, 0, len(pos))
	for n := range pos {
		ns = append(ns, n)
	}
	return ns
}

// root を何度 h で上げようとしても、fixture の親より上へは行かない (実在の $TMPDIR の大きさに左右されない)。
func TestColumnWalkNeverAscendsAboveFixtureParent(t *testing.T) {
	root := fixture(t)
	m := newAt(t, root)
	m.Resize(120, 40)
	limit := filepath.Dir(root)
	for i := range 8 {
		m.HandleKey(capAscent(m, limit, "h"))
		if got := m.root.path(); got != root && got != limit {
			t.Fatalf("h を %d 回押して root が %s まで上がった (fixture %s・その親 %s より上へ行かない)", i+1, got, root, limit)
		}
	}
	if m.root.path() != limit {
		t.Errorf("1 段は上がる (root を上げる動きを検査に残す): root=%s", m.root.path())
	}
	if n, escaped := escapedFixture(m); escaped {
		t.Errorf("fixture の親までなら小さい: %d 節", n)
	}
}

// 木が fixture の外の大きな dir へ広がったことを、節の数で見つける。
func TestEscapedFixtureDetectsALargeTree(t *testing.T) {
	small := newAt(t, fixture(t))
	if n, escaped := escapedFixture(small); escaped {
		t.Errorf("fixture は小さい: %d 節", n)
	}
	big := t.TempDir()
	for i := range maxFixtureNodes + 50 {
		mkfile(t, filepath.Join(big, fmt.Sprintf("f%04d.txt", i)), "x")
	}
	if n, escaped := escapedFixture(newAt(t, big)); !escaped {
		t.Errorf("%d 節の木を外へ広がったと見つけられない (%d)", maxFixtureNodes+50, n)
	}
}
