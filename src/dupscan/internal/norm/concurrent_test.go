package norm

import (
	"sync"
	"testing"

	"dupscan/internal/tree"
)

// Normalize は作業領域 (旗の表・書き出し buffer) を sync.Pool で使い回す。goroutine が並行に呼んでも、hash は 1 本ずつ呼んだときと同じ。
// 返却前に clear しない・pool に戻した表を別の呼び出しが使う、といった退行は、ここ (go test -race を含む) で落ちる。
func TestNormalizeIsDeterministicUnderConcurrency(t *testing.T) {
	mk := func(i int) *tree.Unit {
		// 単位ごとに形を変える (表に残った別の単位の旗が、次の単位の判定に漏れると hash が変わる)
		kids := []*tree.Node{{Kind: "go.ident", Role: tree.RoleName, Text: "x", Expr: true}}
		for range i % 7 {
			kids = append(kids, &tree.Node{Kind: "go.lit", Role: tree.RoleLiteral, LitKind: "num", Text: "1", Expr: true})
		}
		return &tree.Unit{Name: "f", Params: []string{"x"}, Body: &tree.Node{Kind: "go.block", Children: kids}, ReturnType: "int"}
	}
	want := make([]map[Level]string, 7)
	for i := range want {
		want[i] = Normalize(mk(i)).Hash
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 1500 {
				got := Normalize(mk(i)).Hash
				for lv, h := range want[i%7] {
					if got[lv] != h {
						t.Errorf("形 %d の %v の hash が並行で変わった", i%7, lv)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

// pool から取り出した作業領域は空である。返却時に clear しないと、前の単位の旗が残り、同じアドレスのノードを
// 持つ次の単位の判定 (refs / acts) が汚染される (ノードのアドレスで引く表なので、GC 後の再利用で起こりうる)。
func TestScratchIsEmptyWhenTakenFromThePool(t *testing.T) {
	node := &tree.Node{Kind: "go.ident"}
	s := scratchPool.Get().(*scratch)
	s.refs[node] = true
	s.acts[node] = true
	s.release()
	// pool は goroutine ごとの私有領域から返すので、同じ goroutine で続けて取れば同じ scratch が返る (Put/Get の組)
	got := scratchPool.Get().(*scratch)
	defer got.release()
	if got == s && (len(got.refs) != 0 || len(got.acts) != 0) {
		t.Errorf("返却した作業領域の旗の表が空でない: refs=%d acts=%d (前の単位の旗が次の単位へ漏れる)", len(got.refs), len(got.acts))
	}
	if got != s {
		t.Skip("pool が別の作業領域を返した (この呼び出しでは検査できない)")
	}
}

// 巨大に育った表は pool に戻さない (小さい単位の走査の間ずっと抱えない)。
func TestScratchBiggerThanTheLimitIsNotReturned(t *testing.T) {
	s := &scratch{refs: map[*tree.Node]bool{}, acts: map[*tree.Node]bool{}}
	for i := 0; i <= scratchKeepMax; i++ {
		s.refs[&tree.Node{}] = true
	}
	s.release()
	if len(s.refs) != scratchKeepMax+1 {
		t.Errorf("上限を超えた表を clear して pool に戻した (巨大な表を抱える対策が効かない): len=%d", len(s.refs))
	}
}

// 巨大な文字列リテラルで育った引用の buffer は、pool に戻す前に捨てる。表は小さいままなので、表の要素数の判定では見逃す。
func TestScratchDropsAnOversizedQuoteBuffer(t *testing.T) {
	s := newScratchForTest()
	s.quoted = make([]byte, 0, quotedKeepMax+1)
	s.release()
	if cap(s.quoted) > quotedKeepMax {
		t.Errorf("上限を超えた引用 buffer (cap=%d) を、捨てずに pool に戻した", cap(s.quoted))
	}
	// 上限以下の buffer は使い回す (毎回作り直さない)
	s2 := newScratchForTest()
	s2.quoted = make([]byte, 0, 128)
	s2.release()
	if cap(s2.quoted) != 128 {
		t.Errorf("上限以下の引用 buffer を捨てた: cap=%d", cap(s2.quoted))
	}
}

// release が書き込み先を外すので、out も持つ実物と同じ作り (pool の New と同じ) で組む。
func newScratchForTest() *scratch {
	return scratchPool.New().(*scratch)
}
