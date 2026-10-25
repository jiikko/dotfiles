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

// 見える項目が 100 件以上なら、開く前に確認の板 (件数と対象の名前) を出す。n と Enter は取り消して知らせ、y で開く。99 件までは聞かずに開く。
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
	if m.ask.name != big.name { // 名前は木にも出ているので、画面の文字列では板に載ったかを見分けられない
		t.Errorf("確認の板の対象の名前 = %q (want %q)", m.ask.name, big.name)
	}
	m.TakeNotices()
	for _, k := range []string{"n", "enter"} { // Enter は l の別名。移動中に板が出ても開かない
		m.HandleKey(k)
		settle(m)
		if m.ask != nil || big.expanded {
			t.Fatalf("%s で取り消したのに開いた / 板が残る: ask=%v expanded=%v", k, m.ask != nil, big.expanded)
		}
		if ns := m.TakeNotices(); len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Text, "取り消しました") {
			t.Fatalf("%s の取り消しを知らせない: %+v", k, ns)
		}
		m.setCur(big)
		m.HandleKey(">")
		waitExplode(t, m)
	}

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
	m.TakeNotices()
	m.HandleKey("<")
	if ns := m.TakeNotices(); len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Text, "中断しました") {
		t.Fatalf("< で explode を止めたのに知らせない: %+v", ns)
	}
	waitExplode(t, m)
	if b.expanded {
		t.Fatal("< の後に explode が b を開き直した")
	}
}

// 板を待つ間に対象をゴミ箱へ移したら、板を出さずに捨てる (無いフォルダについて聞かない・「開いた」と知らせない)。
func TestExplodeDropsTargetGoneWhileWaiting(t *testing.T) {
	m, big := newBig(t, 120)
	allowOps(t, m)

	m.HandleKey(">")
	m.HandleKey("d")
	waitExplodeDone(t, m)
	m.HandleKey("y") // big をゴミ箱へ
	if m.findNode(big.abs) != nil {
		t.Fatal("前提: big がゴミ箱へ移っていない")
	}
	m.TakeNotices()
	m.Advance(fixedNow)
	if m.ask != nil || m.exploding != nil {
		t.Fatalf("木から無くなったフォルダの explode を捨てない: ask=%v exploding=%v", m.ask != nil, m.exploding != nil)
	}
	for _, n := range m.TakeNotices() {
		if strings.Contains(n.Text, "開いた") {
			t.Fatalf("無いフォルダについて開いたと知らせた: %q", n.Text)
		}
	}
}

// Z は root 以外を全部畳み (カーソルまでの経路も)、カーソルを経路の root 直下の項目へ移す。
func TestCollapseAllResetsToRootLevel(t *testing.T) {
	m := newTest(t)
	a := m.findNode(m.root.abs + "/a")
	m.setCur(a)
	m.HandleKey("space")
	b := m.findNode(m.root.abs + "/b")
	m.setCur(b)
	m.HandleKey(">")
	waitExplode(t, m)
	deep := m.findNode(b.abs + "/deep")
	if !a.expanded || !b.expanded || deep == nil || !deep.expanded {
		t.Fatal("前提: a と b と b/deep が開いていない")
	}
	m.setCur(deep)
	m.HandleKey("Z")
	if a.expanded || b.expanded || deep.expanded {
		t.Fatalf("Z で畳まれない: a=%v b=%v deep=%v", a.expanded, b.expanded, deep.expanded)
	}
	if !m.root.expanded {
		t.Fatal("Z で root まで畳んだ")
	}
	if m.cur != b {
		t.Fatalf("Z の後のカーソル = %s, want b (経路の root 直下)", m.cur.raw)
	}
}

// 走っている explode は Z で止まり、終わった後に開き直さない。
func TestCollapseAllStopsExplode(t *testing.T) {
	m := newTest(t)
	b := m.findNode(m.root.abs + "/b")
	m.setCur(b)
	m.HandleKey(">")
	m.TakeNotices()
	m.HandleKey("Z")
	if ns := m.TakeNotices(); len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Text, "中断しました") {
		t.Fatalf("Z で explode を止めたのに知らせない: %+v", ns)
	}
	waitExplode(t, m)
	if b.expanded {
		t.Fatal("Z の後に explode が b を開き直した")
	}
}

// カーソルが root に居るとき (ライブ更新で経路が消えた) の Z は、root 直下の先頭へ移す (root に残すと選べる項目が無い)。
func TestCollapseAllFromRootGoesToFirstKid(t *testing.T) {
	m := newTest(t)
	m.setCur(m.root)
	m.HandleKey("Z")
	if ks := m.kids(m.root); m.cur != ks[0] {
		t.Fatalf("root の上の Z のカーソル = %s, want %s", m.cur.raw, ks[0].raw)
	}
}
