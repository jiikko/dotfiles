package filer

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// newBig は root/big/sub に n 個のファイルを置き、カーソルを big に置く。> で見える項目は sub と n 個のファイルで n+1 個。
func newBig(t *testing.T, n int) (*Model, *node) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "big", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		mustWrite(t, filepath.Join(sub, "f"+strconv.Itoa(i)+".txt"), "")
	}
	m := newAt(t, root)
	m.Resize(120, 40)
	settle(m)
	big := m.findNode(filepath.Join(root, "big"))
	m.setCur(big)
	return m, big
}

// waitExplodeDone は裏の走査が終わるまで待つ (取り込みはしない。Advance を呼ぶまで m.exploding は残る)。
func waitExplodeDone(t *testing.T, m *Model) {
	t.Helper()
	j := m.exploding
	if j == nil {
		t.Fatal("explode が始まっていない")
	}
	waitFor(t, "explode の走査", j.finished)
}

// 見える項目が 100 件以上なら、開く前に確認の板を出す。n で取り消すと何も開かず、y で開く。99 件までは聞かずに開く。
func TestExplodeAsksBeforeOpeningHundredItems(t *testing.T) {
	m, big := newBig(t, 98) // 99 件
	m.HandleKey(">")
	waitExplode(t, m)
	if m.ask != nil || !big.expanded {
		t.Fatalf("99 件で確認の板が出た / 開かない: ask=%v expanded=%v", m.ask != nil, big.expanded)
	}

	m, big = newBig(t, 99) // 100 件
	m.HandleKey(">")
	waitExplode(t, m)
	if m.ask == nil || !m.OwnsKeys() {
		t.Fatal("100 件で確認の板が出ない")
	}
	if big.expanded {
		t.Fatal("確認の前に開いた")
	}
	if !strings.Contains(strings.Join(m.View(), "\n"), "100 個の項目") {
		t.Error("確認の板に件数が描かれていない")
	}
	m.HandleKey("n")
	settle(m)
	if m.ask != nil || big.expanded {
		t.Fatalf("n で取り消したのに開いた / 板が残る: ask=%v expanded=%v", m.ask != nil, big.expanded)
	}

	m.HandleKey(">")
	waitExplode(t, m)
	m.HandleKey("y")
	sub := m.findNode(filepath.Join(big.abs, "sub"))
	if !big.expanded || sub == nil || !sub.expanded {
		t.Fatal("y で開かない")
	}
}

// 他の板 (d の確認) を開いている間に走査が終わっても、その板を上書きしない。閉じてから聞く。
func TestExplodeAskWaitsForOpenBoard(t *testing.T) {
	m, big := newBig(t, 120)
	m.HandleKey(">")
	m.HandleKey("d") // big をゴミ箱へ移す確認の板
	waitExplodeDone(t, m)
	m.Advance(fixedNow)
	if m.ask == nil || m.ask.verb != "ゴミ箱へ移す" {
		t.Fatalf("d の確認の板が explode の板に置き換わった: %+v", m.ask)
	}
	m.HandleKey("n")
	m.Advance(fixedNow)
	if m.ask == nil || m.ask.verb != "開く" {
		t.Fatal("d の板を閉じた後に explode の確認が出ない")
	}
	if big.expanded {
		t.Fatal("確認の前に開いた")
	}
}

// < はカーソルのフォルダと配下を全部畳み、配下の開閉を覚えない。他の枝は触らない。ファイルの上では親が対象。
func TestCollapseBelowClosesSubtree(t *testing.T) {
	m := newTest(t)
	a := m.findNode(m.root.abs + "/a")
	m.setCur(a)
	m.HandleKey("space")
	b := m.findNode(m.root.abs + "/b")
	m.setCur(b)
	m.HandleKey(">")
	waitExplode(t, m)
	deep := m.findNode(b.abs + "/deep")
	if !b.expanded || deep == nil || !deep.expanded || !a.expanded {
		t.Fatal("前提: b と b/deep と a が開いていない")
	}
	m.HandleKey("<")
	if b.expanded || deep.expanded {
		t.Fatalf("< で配下が畳まれない: b=%v deep=%v", b.expanded, deep.expanded)
	}
	if !a.expanded {
		t.Fatal("< で別の枝まで畳んだ")
	}

	cdTo(t, m, "a/one.txt")
	m.HandleKey("<")
	if m.cur != a || a.expanded {
		t.Fatalf("ファイルの上の < で親が畳まれない / カーソルが親へ移らない: cur=%s expanded=%v", m.cur.raw, a.expanded)
	}
}

// 走っている explode の対象を < で畳んだら、explode は止まり、終わった後に開き直さない。
func TestCollapseBelowStopsExplode(t *testing.T) {
	m := newTest(t)
	b := m.findNode(m.root.abs + "/b")
	m.setCur(b)
	m.HandleKey(">")
	m.HandleKey("<")
	waitExplode(t, m)
	if b.expanded {
		t.Fatal("< の後に explode が b を開き直した")
	}
}
