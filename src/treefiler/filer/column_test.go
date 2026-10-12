package filer

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// columnByLayout は issue 723 の前の column (木全体を配置して、同じ深さの項目を y で並べる)。新しい column の正解役。
func columnByLayout(m *Model, of *node) []*node {
	pos := m.layoutAll()
	d := of.depth()
	var col []*node
	for n := range pos {
		if n.depth() == d {
			col = append(col, n)
		}
	}
	sort.Slice(col, func(i, j int) bool { return pos[col[i]].y < pos[col[j]].y })
	return col
}

// 列 (検索の対象) は、配置を作って y で並べた列と同じ項目・同じ順。開閉・dotfile・経路の上の dotfile・並びの設定を乱数で混ぜる。
func TestColumnMatchesLayout(t *testing.T) {
	keys := []string{"j", "k", "J", "K", "g", "G", "h", "l", "l", "space", "tab", "c", "C", ".", "S"}
	for seed := range uint64(40) {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			root := fixture(t)
			for _, rel := range []string{"a/sub/x.txt", "a/sub/.dot/y.txt", "a/.h2/z.txt", "b/deep/more/w.txt", "b/e.txt", ".hd/q.txt", "c2/f.txt"} {
				p := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, p, "x")
			}
			m := newAt(t, root)
			m.Resize(120, 40)
			rng := rand.New(rand.NewPCG(seed, 723))
			var log []string
			for range 60 {
				k := keys[rng.IntN(len(keys))]
				if rng.IntN(5) == 0 {
					m.set.FoldersFirst = !m.set.FoldersFirst
					m.applySettings("folders_first")
					k = "folders_first"
				} else {
					m.HandleKey(k)
				}
				log = append(log, k)
				checked := 0
				for _, n := range maps(m.layoutAll()) {
					want, got := columnByLayout(m, n), m.column(n)
					if !slices.Equal(want, got) {
						t.Fatalf("%v の後の %s の列:\n got %s\nwant %s", log, n.abs, names(got), names(want))
					}
					checked++
				}
				if checked == 0 {
					t.Fatal("配置が空 (検査が何も見ていない)")
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
