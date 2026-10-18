package filer

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
	"weak"
)

// leak_test.go は、消したフォルダの部分木が Model から外れて GC に回収されることを固定する。
// 回収は weak pointer で見る (掴んでいる経路が 1 つでも残ると nil にならない)。

func mkEmptyFiles(t *testing.T, dir string, n int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		f, err := os.Create(filepath.Join(dir, "f"+strconv.Itoa(i)))
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
}

func runFrames(m *Model, now time.Time, frames int) time.Time {
	for range frames {
		now = now.Add(16 * time.Millisecond)
		m.Advance(now)
		m.View()
	}
	return now
}

// collected は GC を回して、weak pointer の先が回収されたか (回数で打ち切る。時間では待たない)。
func collected(ws ...weak.Pointer[node]) bool {
	for range 10 {
		runtime.GC()
		all := true
		for _, w := range ws {
			if w.Value() != nil {
				all = false
			}
		}
		if all {
			return true
		}
	}
	return false
}

// openBig は dir を読んで開き、カーソルを子の i 番目に置き、動きを終わらせる。
func openBig(t *testing.T, m *Model, dir string, i int) *node {
	t.Helper()
	n := m.lookup(dir, (*node).load)
	if n == nil {
		t.Fatalf("%s が木に無い", dir)
	}
	n.load()
	n.expanded = true
	m.setCur(n.kids[i])
	return n
}

// 小さな親 (仮想化しない) の下の、仮想化していた大きな子フォルダを消す: 親の覚えの removed・詰めた order の後ろ・使わなかった
// 仮想化のブロックのどれも、消えた部分木を掴まない。
func TestRemovedSubtreeUnderSmallParentIsCollected(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	x := filepath.Join(root, "X")
	sub := filepath.Join(x, "sub")
	mkEmptyFiles(t, sub, 400)
	mustWrite(t, filepath.Join(x, "keep"), "k")
	m := newAt(t, root)
	m.virtOverride = 64
	m.Resize(120, 40)
	m.lookup(x, (*node).load).expanded = true
	sn := openBig(t, m, sub, 200)
	now := runFrames(m, fixedNow, 80)
	if len(m.virt) == 0 {
		t.Fatal("前提: sub を仮想化していない")
	}
	ws := []weak.Pointer[node]{weak.Make(sn), weak.Make(sn.kids[0]), weak.Make(sn.kids[200]), weak.Make(sn.kids[399])}
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	m.Refresh()
	runFrames(m, now, 120)
	if !collected(ws...) {
		t.Fatal("消した sub の部分木が回収されない (Model が掴んでいる)")
	}
	runtime.KeepAlive(m) // 🚨 m が先に回収されると、掴んでいても weak pointer は nil になる (検査が素通りする)
}

// 仮想化していた巨大なフォルダそのものを消す (カーソルは遠くの子に居た)。
func TestRemovedBigFolderIsCollected(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	big := filepath.Join(root, "big")
	mkEmptyFiles(t, big, 400)
	mustWrite(t, filepath.Join(root, "other"), "o")
	m := newAt(t, root)
	m.virtOverride = 64
	m.Resize(120, 40)
	bn := openBig(t, m, big, 300)
	now := runFrames(m, fixedNow, 120)
	ws := []weak.Pointer[node]{weak.Make(bn), weak.Make(bn.kids[300]), weak.Make(bn.kids[10])}
	m.setCur(m.findNode(filepath.Join(root, "other")))
	if err := os.RemoveAll(big); err != nil {
		t.Fatal(err)
	}
	m.Refresh()
	runFrames(m, now, 120)
	if !collected(ws...) {
		t.Fatal("消した big の部分木が回収されない (Model が掴んでいる)")
	}
	runtime.KeepAlive(m) // 🚨 m が先に回収されると、掴んでいても weak pointer は nil になる (検査が素通りする)
}

// 名前の違うフォルダを作っては消すのを繰り返しても、走査の結果 (recs と walker の記録) は木の中のフォルダの数を超えて伸びない。
func TestWalkResultsDropRemovedDirs(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	m := newAt(t, root)
	m.Resize(100, 30)
	now := fixedNow
	for i := range 20 {
		d := filepath.Join(root, "tmp"+strconv.Itoa(i))
		mkEmptyFiles(t, d, 1)
		m.Refresh()
		waitWalked(t, m, &now, d)
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
		m.Refresh()
		now = runFrames(m, now, 5)
	}
	m.walker.mu.Lock()
	nr, nl := len(m.walker.results), len(m.walker.last)
	m.walker.mu.Unlock()
	if len(m.recs) > 1 || nr > 1 || nl > 1 { // 残ってよいのは root 自身だけ
		t.Fatalf("消したフォルダの記録が残る: recs=%d walker.results=%d walker.last=%d (%v)", len(m.recs), nr, nl, m.recs)
	}
}

// ライブ更新 (force なしの forget) で作っては消しても、walker の記録 (last を含む) は伸びない。
func TestWalkResultsDropRemovedDirsOnLiveUpdate(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	m := newAt(t, root)
	m.Resize(100, 30)
	now := fixedNow
	for i := range 20 {
		name := "tmp" + strconv.Itoa(i)
		d := filepath.Join(root, name)
		mkEmptyFiles(t, d, 1)
		m.applyChange(dirChange{dir: root, changed: []string{name}, listing: true})
		waitWalked(t, m, &now, d)
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
		m.applyChange(dirChange{dir: root, removed: []string{name}, listing: true})
		now = runFrames(m, now, 5)
	}
	m.walker.mu.Lock()
	nr, nl := len(m.walker.results), len(m.walker.last)
	m.walker.mu.Unlock()
	if len(m.recs) > 1 || nr > 1 || nl > 1 {
		t.Fatalf("消したフォルダの記録が残る: recs=%d walker.results=%d walker.last=%d", len(m.recs), nr, nl)
	}
}

// 走査の結果は配下の全部のフォルダを持つ。node の無い未展開の子孫の結果は、読み直しでも捨てない (捨てると開くたびに数え直す)。
func TestWalkResultsKeepUnexpandedDescendants(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	mkEmptyFiles(t, deep, 1)
	m := newAt(t, root)
	m.Resize(100, 30)
	now := fixedNow
	waitWalked(t, m, &now, deep)
	if n := m.findNode(filepath.Join(root, "a", "b")); n != nil && n.loaded {
		t.Fatal("前提: b を読み込んでいる (未展開の子孫にならない)")
	}
	m.Refresh()
	m.applyChange(dirChange{dir: root, changed: []string{"x"}, listing: true})
	if _, ok := m.recs[deep]; !ok {
		t.Fatal("未展開の子孫 a/b/c の走査の結果を捨てた")
	}
}

// 検索で巨大なフォルダの中の一致へ移って閉じた後、そのフォルダを消しても、検索の一致と起点は部分木を掴まない。
func TestRemovedFolderIsCollectedAfterSearch(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	big := filepath.Join(root, "big")
	mkEmptyFiles(t, big, 400)
	mustWrite(t, filepath.Join(root, "other"), "o")
	m := newAt(t, root)
	m.virtOverride = 64
	m.Resize(120, 40)
	bn := openBig(t, m, big, 0)
	now := runFrames(m, fixedNow, 60)
	m.HandleKey("/")
	typeText(m, "f39")
	if m.cur.parent != bn {
		t.Fatalf("前提: big の中の一致へ移っていない (%s)", m.cur.raw)
	}
	m.HandleKey("enter")
	ws := []weak.Pointer[node]{weak.Make(bn), weak.Make(m.cur)}
	m.setCur(m.findNode(filepath.Join(root, "other")))
	if err := os.RemoveAll(big); err != nil {
		t.Fatal(err)
	}
	m.Refresh()
	runFrames(m, now, 120)
	if !collected(ws...) {
		t.Fatal("検索を閉じた後、消した big の部分木が回収されない")
	}
	runtime.KeepAlive(m)
}

// 巨大なフォルダの中のファイルをタイルで開いて閉じた後、そのフォルダを消しても、閉じたタイルは部分木を掴まない。
func TestRemovedFolderIsCollectedAfterTileClosed(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	big := filepath.Join(root, "big")
	mkEmptyFiles(t, big, 400)
	mustWrite(t, filepath.Join(root, "other"), "o")
	m := newAt(t, root)
	m.virtOverride = 64
	m.Resize(120, 40)
	bn := openBig(t, m, big, 5)
	now := runFrames(m, fixedNow, 60)
	m.HandleKey("enter") // f5 をタイルで開く
	now = runFrames(m, now, 30)
	if m.frontTile() == nil {
		t.Fatal("前提: タイルが開かない")
	}
	m.HandleKey("q") // タイルを閉じる
	now = runFrames(m, now, 60)
	if len(m.tiles) != 0 {
		t.Fatalf("前提: タイルが閉じ切っていない (%d)", len(m.tiles))
	}
	ws := []weak.Pointer[node]{weak.Make(bn), weak.Make(bn.kids[5])}
	m.setCur(m.findNode(filepath.Join(root, "other")))
	if err := os.RemoveAll(big); err != nil {
		t.Fatal(err)
	}
	m.Refresh()
	runFrames(m, now, 120)
	if !collected(ws...) {
		t.Fatal("タイルを閉じた後、消した big の部分木が回収されない")
	}
	runtime.KeepAlive(m)
}

// 消えたかの判定は、1 回の中でフォルダごとに名前の表を 1 回だけ作る (キーごとに子を舐めると 2 乗になり、子 4 万件で UI が秒単位で固まった)。
func TestGoneCheckBuildsIndexOncePerFolder(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	m := newAt(t, root)
	for i := range 2000 {
		d := filepath.Join(root, "d"+strconv.Itoa(i))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m.Refresh()
	for i := range 2000 {
		m.recs[filepath.Join(root, "d"+strconv.Itoa(i))] = walkResult{}
	}
	// 表を作るのが root の 1 回なら確保は 12 回、キーごとに作り直すと 28002 回 (2026-10-12 の実測。閾値 100 は両側から桁で離れている)
	allocs := testing.AllocsPerRun(3, func() { m.dropGoneRecs(root) })
	t.Logf("1 回の判定の確保 = %.0f", allocs)
	if allocs > 100 || len(m.recs) != 2000 {
		t.Fatalf("1 回の判定の確保 = %.0f (キーごとに表を作り直している) / 残った記録 = %d (want 2000)", allocs, len(m.recs))
	}
}
