package filer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// gateLoad は m が打鍵で頼む dir の読み込みを、返した関数を呼ぶまで返さないようにする (裏で読んでいる最中を決定的に作る)。
// LoadWait は 0 (待たない) にする。後片付けで読み込みの goroutine の終わりを待つ。
// 🚨 パッケージ変数の readBulk は差し替えない: 裏の走査 (walk) の goroutine も readEntries 経由で読み、差し替えと競合する。
func gateLoad(t *testing.T, m *Model, dirs ...string) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	oldWait := LoadWait
	m.loadRead = func(d string) ([]dirEntry, error) {
		if slices.Contains(dirs, d) {
			<-gate
		}
		return readEntries(d)
	}
	LoadWait = 0
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(func() {
		release()
		loadWG.Wait()
		LoadWait = oldWait
	})
	return release
}

// arrive は裏の読み込みを届かせて Advance で取り込む。
func arrive(m *Model, release func()) {
	release()
	loadWG.Wait()
	m.Advance(fixedNow)
}

func statusLine(m *Model) string {
	v := m.View()
	return v[len(v)-1]
}

// 間に合わない読み込みは打鍵を止めず、届いたら続きで潜る。待つ間は動きとして扱い (16ms の tick で取り込む)、最下行に出す。
func TestDescendLoadsOffUI(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	m.HandleKey("l")
	if m.cur != a || a.loaded {
		t.Fatalf("読み終わる前に潜った / 読み込み済みになった: cur=%s loaded=%v", m.cur.raw, a.loaded)
	}
	m.Advance(fixedNow)
	if !m.Animating() || !m.Busy() {
		t.Fatalf("読み込みを待つ間に Animating=%v Busy=%v (tick が止まると結果を取り込めない)", m.Animating(), m.Busy())
	}
	if !strings.Contains(statusLine(m), "reading a") {
		t.Fatalf("最下行に読み込み中が出ない: %q", statusLine(m))
	}
	arrive(m, release)
	if !a.loaded || m.cur.parent != a || !a.expanded {
		t.Fatalf("届いた後に潜らない: loaded=%v cur=%s expanded=%v", a.loaded, m.cur.raw, a.expanded)
	}
	if strings.Contains(statusLine(m), "reading") || len(m.loads) != 0 {
		t.Fatalf("届いた後も読み込み中のまま: loads=%d %q", len(m.loads), statusLine(m))
	}
}

// Space の続きは開く。
func TestToggleLoadsOffUI(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	m.HandleKey("space")
	if a.expanded {
		t.Fatal("読み終わる前に開いた")
	}
	arrive(m, release)
	if !a.loaded || !a.expanded || m.cur != a {
		t.Fatalf("届いた後に開かない (カーソルは動かさない): loaded=%v expanded=%v cur=%s", a.loaded, a.expanded, m.cur.raw)
	}
}

// 続きを待っている間に来たキーは、読み込みと続きを済ませてから効く (先打ち: 同期で読んでいた頃と同じ順と意味)。
func TestNextKeyRunsAfterLoad(t *testing.T) {
	for _, c := range []struct {
		name, dir string
		keys      []string
		check     func(t *testing.T, m *Model, dir *node)
	}{
		{"l j は潜った先で動く", "a", []string{"l", "j"}, func(t *testing.T, m *Model, a *node) {
			if m.cur.parent != a || m.cur != m.kids(a)[1] {
				t.Fatalf("cur = %s (a の 2 番目の子にいない)", m.cur.raw)
			}
		}},
		{"l l は 2 段潜る", "b", []string{"l", "l"}, func(t *testing.T, m *Model, b *node) {
			if m.cur.parent == nil || m.cur.parent.parent != b {
				t.Fatalf("cur = %s (b/deep の中にいない)", m.cur.raw)
			}
		}},
		{"Space Space は開いて閉じる", "a", []string{"space", "space"}, func(t *testing.T, m *Model, a *node) {
			if a.expanded || m.cur != a {
				t.Fatalf("expanded=%v cur=%s (2 打目で閉じない)", a.expanded, m.cur.raw)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newTest(t)
			d := m.findNode(filepath.Join(m.root.abs, c.dir))
			m.setCur(d)
			release := gateLoad(t, m, d.abs)
			m.HandleKey(c.keys[0])
			if d.loaded {
				t.Fatal("前提の破れ: 1 打目で読み終わった")
			}
			release() // 2 打目は届くのを待ってから効く
			for _, k := range c.keys[1:] {
				m.HandleKey(k)
			}
			loadWG.Wait() // 2 打目が頼んだ読み込み (l l の deep) も取り込む
			m.Advance(fixedNow)
			c.check(t, m, d)
		})
	}
}

// 続きを待っている読み込みが loadFlushWait のうちに届かなければ (止まったマウント)、続きは捨ててキーを今の状態に効かせる。
// j k でカーソルを戻す: 戻った後に届いても、捨てた続きで潜らない (カーソルの守りだけに頼らない)。
func TestNextKeyGivesUpOnStuckLoad(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	old := loadFlushWait
	loadFlushWait = 0
	t.Cleanup(func() { loadFlushWait = old })
	m.HandleKey("l")
	m.HandleKey("j")
	if m.cur == a || m.cur.parent == a {
		t.Fatalf("止まった読み込みで j が効かない / 潜った: cur=%s", m.cur.raw)
	}
	m.HandleKey("k")
	if m.cur != a {
		t.Fatalf("前提の破れ: k で a に戻らない (cur=%s)", m.cur.raw)
	}
	cur := m.cur
	arrive(m, release)
	if !a.loaded || m.cur != cur || a.expanded {
		t.Fatalf("届いた後: loaded=%v cur=%s expanded=%v (捨てた続きで潜った)", a.loaded, m.cur.raw, a.expanded)
	}
}

// 一度待って届かなかった読み込みは、次のキーでもう待たない (止まったマウントで l を押し続けるたびに止まらない)。
func TestStuckLoadIsWaitedOnce(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	gateLoad(t, m, a.abs)
	waits := 0
	old := loadWaiter
	loadWaiter = func(<-chan loadResult, time.Duration) (loadResult, bool) { waits++; return loadResult{}, false } // 待ちの上限が切れたことにする
	t.Cleanup(func() { loadWaiter = old })
	m.HandleKey("l")
	m.HandleKey("j") // 1 回待って諦める
	m.HandleKey("k")
	m.HandleKey("l") // 同じ読み込みに続きを張り直す
	m.HandleKey("j") // もう待たない
	if waits != 1 {
		t.Fatalf("止まった読み込みを %d 回待った (1 回で諦める)", waits)
	}
	if len(m.loads) != 1 {
		t.Fatalf("読み込みの記録が %d 本 (止まった 1 本を頼み直していない)", len(m.loads))
	}
}

// 続きを済ませる待ちは、頼んでからの合計で loadFlushWait まで (キーごとに数えない)。
func TestFlushWaitCountsFromRequest(t *testing.T) {
	isolateConfig(t)
	now := fixedNow
	m, err := New(fixture(t), Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	settle(m)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	gateLoad(t, m, a.abs)
	var got []time.Duration
	old := loadWaiter
	loadWaiter = func(_ <-chan loadResult, d time.Duration) (loadResult, bool) {
		got = append(got, d)
		return loadResult{}, false
	}
	t.Cleanup(func() { loadWaiter = old })
	m.HandleKey("l")
	now = now.Add(loadFlushWait - 100*time.Millisecond)
	m.HandleKey("j")
	if len(got) != 1 || got[0] != 100*time.Millisecond {
		t.Fatalf("頼んでから %v 後のキーの待ち = %v, want [100ms]", loadFlushWait-100*time.Millisecond, got)
	}
}

// 諦めた読み込みでも、次のキーの時点で届いていれば続きを済ませる (tick より先にキーが来た窓で、潜らずに隣へ動かない)。
func TestFlushTakesArrivedResultAfterGivingUp(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	old := loadWaiter
	loadWaiter = func(<-chan loadResult, time.Duration) (loadResult, bool) { return loadResult{}, false }
	t.Cleanup(func() { loadWaiter = old })
	m.HandleKey("l")
	m.HandleKey("j") // 諦める
	m.HandleKey("k")
	m.HandleKey("l") // 続きを張り直す
	release()
	loadWG.Wait() // 届いたが Advance (tick) はまだ
	m.HandleKey("j")
	if m.cur.parent != a {
		t.Fatalf("届いていた結果を見ずに続きを捨てた: cur=%s (a の中で j が効くはず)", m.cur.raw)
	}
}

// 頼んでから loadFlushWait を過ぎていても、次のキーの時点で届いていれば続きを済ませる (合計の上限切れの枝でも覗く)。
func TestFlushTakesArrivedResultAfterLimit(t *testing.T) {
	isolateConfig(t)
	now := fixedNow
	m, err := New(fixture(t), Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	settle(m)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	m.HandleKey("l")
	now = now.Add(2 * loadFlushWait)
	release()
	loadWG.Wait() // 届いたが Advance (tick) はまだ
	m.HandleKey("j")
	if m.cur.parent != a {
		t.Fatalf("上限を過ぎて届いていた結果を見ずに続きを捨てた: cur=%s (a の中で j が効くはず)", m.cur.raw)
	}
}

// ctrl+c は読み込みを待たずに終わる。
func TestQuitDoesNotWaitForLoad(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	gateLoad(t, m, a.abs)
	waits := 0
	old := loadWaiter
	loadWaiter = func(<-chan loadResult, time.Duration) (loadResult, bool) { waits++; return loadResult{}, false }
	t.Cleanup(func() { loadWaiter = old })
	m.HandleKey("l")
	if r := m.HandleKey("ctrl+c"); r != Quit || waits != 0 {
		t.Fatalf("ctrl+c: result=%v waits=%d (待たずに終わる)", r, waits)
	}
}

// キーを通らずにカーソルが動いたら (ライブ更新でフォルダが消えて親へ移る等)、届いても潜らない。
func TestContinuationNeedsCursorOnFolder(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	m.HandleKey("l")
	other := m.root // 子のある読み込み済みのフォルダ (ここで潜ると cur が動くので、守りを外すと見える)
	m.setCur(other)
	arrive(m, release)
	if !a.loaded || m.cur != other || a.expanded {
		t.Fatalf("カーソルが離れたのに潜った / 開いた: cur=%s expanded=%v", m.cur.raw, a.expanded)
	}
}

// 読めなかったら「読めません」を出す。待つ間に木から外れたフォルダの失敗は出さない。
func TestLoadFailureNoticeOnlyInTree(t *testing.T) {
	for _, detach := range []bool{false, true} {
		t.Run(map[bool]string{false: "木の中", true: "外れた"}[detach], func(t *testing.T) {
			m := newTest(t)
			a := m.findNode(filepath.Join(m.root.abs, "a"))
			m.setCur(a)
			gate := make(chan struct{})
			oldWait := LoadWait
			LoadWait = 0
			m.loadRead = func(string) ([]dirEntry, error) { <-gate; return nil, os.ErrPermission }
			t.Cleanup(func() { loadWG.Wait(); LoadWait = oldWait })
			m.HandleKey("l")
			m.TakeNotices()
			if detach {
				m.root.kids = slices.DeleteFunc(m.root.kids, func(k *node) bool { return k == a })
				m.root.kidsVer++
				m.setCur(m.root.kids[0])
			}
			close(gate)
			loadWG.Wait()
			m.Advance(fixedNow)
			got := false
			for _, n := range m.TakeNotices() {
				got = got || strings.Contains(n.Text, "読めません")
			}
			if got == detach {
				t.Fatalf("読めません の知らせ = %v (木から外れた = %v)", got, detach)
			}
		})
	}
}

// 同じフォルダへもう一度打つと続きを覚え直す (頼み直しはしない)。
func TestRepeatedKeyKeepsOneLoad(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	old := loadFlushWait
	loadFlushWait = 0 // 2 打目で待たない (待つと読み終わってから効くので、読み込み中への 2 打目にならない)
	t.Cleanup(func() { loadFlushWait = old })
	m.HandleKey("space")
	m.HandleKey("l")
	if len(m.loads) != 1 {
		t.Fatalf("同じフォルダを %d 本読んでいる", len(m.loads))
	}
	arrive(m, release)
	if m.cur.parent != a {
		t.Fatalf("後の打鍵 (l) の続きで潜らない: cur=%s", m.cur.raw)
	}
}

// 待つ間に同期で読まれたら (a・>・: など)、後から届いた結果は捨てる (同期で読んだ子を差し替えない)。
func TestSyncLoadWinsOverLateResult(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	m.HandleKey("l")
	m.ensureLoaded(a) // 同期の読み込みは止める読み手を通らない
	kids := a.kids
	arrive(m, release)
	if len(kids) == 0 || &a.kids[0] != &kids[0] {
		t.Fatal("同期で読んだ子を、後から届いた結果で差し替えた")
	}
	if len(m.loads) != 0 {
		t.Fatal("捨てた結果の記録が残っている")
	}
}

// 待つ間に並べ方が変わったら、取り込むときに今の並べ方で並べ直す。
func TestLateResultUsesCurrentOrder(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	m.HandleKey("l")
	saved := order
	t.Cleanup(func() { order = saved })
	order.reverse = !order.reverse
	arrive(m, release)
	got, want := names(a.kids), "two.go,three.md,one.txt"
	if !order.reverse {
		want = "one.txt,three.md,two.go"
	}
	if got != want {
		t.Fatalf("kids = %s, want %s (頼んだ時点の並べ方のまま)", got, want)
	}
}

// Close は続きだけを消す (glogx の F は同じ Model を開き直す)。開き直した後の Advance で取り込み、勝手に潜らない。
func TestCloseDropsContinuationButTakesResult(t *testing.T) {
	m := newTest(t)
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	m.setCur(a)
	release := gateLoad(t, m, a.abs)
	m.HandleKey("l")
	m.Close()
	arrive(m, release)
	if !a.loaded || m.cur != a || len(m.loads) != 0 {
		t.Fatalf("閉じた後: loaded=%v cur=%s loads=%d", a.loaded, m.cur.raw, len(m.loads))
	}
	m.HandleKey("l")
	if m.cur.parent != a {
		t.Fatalf("開き直した後に l で潜れない: cur=%s", m.cur.raw)
	}
}

// 裏で読む本数は loadMax まで。超えたら同期で読む (応答しないマウントで goroutine を積み上げない)。
func TestLoadsAreBounded(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	for _, d := range []string{"x", "y", "z"} {
		mkfile(t, filepath.Join(root, d, "f.txt"), "")
	}
	m := newAt(t, root)
	m.Resize(120, 40)
	settle(m)
	gateLoad(t, m, filepath.Join(root, "x"), filepath.Join(root, "y")) // z は止めない (同期で読めることを見る)
	old := loadFlushWait
	loadFlushWait = 0 // 次の打鍵で、止めてある前の読み込みを待たない
	t.Cleanup(func() { loadFlushWait = old })
	for _, n := range []string{"x", "y"} {
		m.setCur(m.findNode(filepath.Join(root, n)))
		m.HandleKey("space")
	}
	if len(m.loads) != loadMax {
		t.Fatalf("裏で読んでいるのが %d 本 (want %d)", len(m.loads), loadMax)
	}
	z := m.findNode(filepath.Join(root, "z"))
	m.setCur(z)
	m.HandleKey("space")
	if !z.loaded || !z.expanded || len(m.loads) != loadMax {
		t.Fatalf("上限を超えた 3 本目を同期で読まない: loaded=%v expanded=%v loads=%d", z.loaded, z.expanded, len(m.loads))
	}
}

// 頼んでから loadHot を過ぎたら Animating を下ろす (止まったマウントで速い tick を回し続けない)。Busy は残す (遅い周期で取り込む)。
func TestLoadStopsHotAfterLoadHot(t *testing.T) {
	isolateConfig(t)
	root := fixture(t)
	now := fixedNow
	m, err := New(root, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	settle(m)
	a := m.findNode(filepath.Join(root, "a"))
	m.setCur(a)
	gateLoad(t, m, a.abs)
	m.HandleKey("l")
	for i := 0; m.Animating(); i++ {
		if i > 200 {
			t.Fatal("動きが終わらない")
		}
		now = now.Add(16 * time.Millisecond)
		m.Advance(now)
		if now.Sub(fixedNow) < loadHot && !m.Animating() {
			t.Fatalf("頼んでから %v で Animating が下りた (loadHot %v の間は取り込みの tick を回す)", now.Sub(fixedNow), loadHot)
		}
	}
	if now.Sub(fixedNow) < loadHot {
		t.Fatalf("loadHot より前 (%v) に Animating が下りた", now.Sub(fixedNow))
	}
	if !m.Busy() {
		t.Fatal("読み込みを待っているのに Busy が下りた (遅い周期でも取り込めない)")
	}
}

// root を上げた後も、旧 root は名前で並ぶ (raw と一緒に名前順の小文字の名前も書き換える)。
func TestRerootKeepsNameOrderAfterResort(t *testing.T) {
	isolateConfig(t)
	parent := t.TempDir()
	for _, d := range []string{"aaa", "mid", "zzz"} {
		mkfile(t, filepath.Join(parent, d, "f.txt"), "")
	}
	m := newAt(t, filepath.Join(parent, "mid"))
	m.Resize(120, 40)
	settle(m)
	m.rerootUp()
	m.resort(m.root)
	if got := names(m.root.kids); got != "aaa,mid,zzz" {
		t.Fatalf("root を上げて並べ直した後 = %s, want aaa,mid,zzz", got)
	}
}
