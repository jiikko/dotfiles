package filer

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"testwait"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// paneText は右の欄 (縦線の右) の行。
func paneText(m *Model) []string {
	c := m.draw()
	x0 := m.w - m.paneWidth()
	var rows []string
	for y := range m.canvasH() {
		var b strings.Builder
		for x := x0 + 1; x < m.w; x++ {
			b.WriteString(c.at(x, y).s)
		}
		rows = append(rows, strings.TrimRight(b.String(), " "))
	}
	return rows
}

// settlePane は欄の読み込みが届くまで Advance を回す (裏の goroutine が置いた結果は Advance で取り込む)。
// 時刻は前の Advance から押しっぱなしとみなさない間を空けて進める (巻き戻すと押しっぱなしと判定されて頼まない)。
func settlePane(t *testing.T, m *Model) {
	t.Helper()
	at := m.lastAdv.Add(200 * time.Millisecond)
	testwait.Until(t, "プレビューの欄", func() bool {
		at = at.Add(time.Millisecond)
		m.Advance(at)
		return !m.paneLoading()
	})
}

// 欄は画面の右 3 割。木のカメラは残りの 7 割で、カーソルをその 38% に置く。狭い画面では欄を出さない。
func TestPaneSplitsScreen(t *testing.T) {
	m := newTest(t)
	m.Resize(100, 30)
	cdTo(t, m, "c.txt")
	settle(m)
	if m.paneWidth() != 30 || m.treeWidth() != 70 {
		t.Fatalf("欄 %d 桁・木 %d 桁 (30・70 のはず)", m.paneWidth(), m.treeWidth())
	}
	if x := int(math.Round(m.anims[m.cur].x.v - m.camX.v)); x != int(math.Round(70*0.38)) {
		t.Fatalf("カーソルの桁 %d (木の幅 70 の 38%% のはず)", x)
	}
	m.Resize(90, 30) // 27 桁 < paneMinW
	if m.paneWidth() != 0 || m.treeWidth() != 90 {
		t.Fatalf("狭い画面で欄 %d 桁・木 %d 桁 (欄を出さないはず)", m.paneWidth(), m.treeWidth())
	}
}

// p で欄を閉じると保存され、作り直した Model も閉じたまま起動する。もう一度 p で開く。
func TestPaneToggleIsSaved(t *testing.T) {
	m := newTest(t)
	if !m.set.Preview {
		t.Fatal("前提: 既定で欄が開いていない")
	}
	m.HandleKey("p")
	if m.set.Preview || m.paneWidth() != 0 {
		t.Fatal("p で欄が閉じない")
	}
	if m2 := newAt(t, m.root.abs); m2.set.Preview {
		t.Fatal("閉じたことが保存されていない (作り直した Model で開いている)")
	}
	m.HandleKey("p")
	if !m.set.Preview {
		t.Fatal("もう一度 p で欄が開かない")
	}
}

// ファイルの中身は裏で読む: 読み込みが止まっていても、カーソルを動かした Advance はすぐ戻る (UI の goroutine で読まない)。
// 届いてから今のファイルになる。
func TestPaneLoadsAsync(t *testing.T) {
	m := newTest(t)
	release := make(chan struct{})
	orig := paneLoader
	paneLoader = func(k paneKey) paneResult { <-release; return orig(k) }
	t.Cleanup(func() { paneLoader = orig })
	cdTo(t, m, "a/one.txt")
	done := make(chan struct{})
	go func() {
		m.Advance(m.lastAdv.Add(200 * time.Millisecond))
		close(done)
	}()
	testwait.Until(t, "読み込みが止まったままの Advance が戻る", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if !m.paneLoading() {
		t.Fatal("前提: 頼んでいない")
	}
	close(release)
	settlePane(t, m)
	got := strings.Join(paneText(m), "\n")
	for _, want := range []string{"one.txt", "1", "2", "3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("欄に %q が無い:\n%s", want, got)
		}
	}
}

// 前の頼みの結果 (世代が古い) は取り込まない。
func TestPaneDropsStaleResult(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	settlePane(t, m)
	shown := m.pane.shown
	m.pane.latest = &paneResult{gen: m.pane.gen - 1, key: paneKey{path: "/stale"}}
	m.stepPane()
	if m.pane.shown != shown {
		t.Fatal("古い世代の結果を取り込んだ")
	}
}

// 押しっぱなしの間は頼まない (離したら頼む)。
func TestPaneWaitsWhileHeld(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "c.txt")
	settlePane(t, m)
	gen := m.pane.gen
	at := fixedNow.Add(time.Second)
	m.Advance(at)
	for range 3 {
		m.HandleKey("j")
		at = at.Add(30 * time.Millisecond)
		m.Advance(at)
	}
	if !m.holding {
		t.Fatal("前提: 押しっぱなしになっていない")
	}
	if m.pane.gen > gen+1 { // 最初の 1 打 (押しっぱなしになる前) の分だけは頼んでよい
		t.Fatalf("押しっぱなしの間に %d 回頼んだ", m.pane.gen-gen)
	}
	asked := m.pane.gen
	at = at.Add(200 * time.Millisecond)
	m.Advance(at)
	if m.pane.gen != asked+1 || m.pane.asked.path != m.cur.path() {
		t.Fatalf("離した後に今のカーソル (%s) を頼んでいない", m.cur.name)
	}
}

// フォルダの上では空欄 (見出しも出さない)。開けないファイルは理由を出す。
func TestPaneFolderBlankAndRefused(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "b")
	settlePane(t, m)
	for i, r := range paneText(m) {
		if strings.TrimLeft(r, " ") != "" {
			t.Fatalf("フォルダの上で欄の %d 行目に %q", i, r)
		}
	}
	cdTo(t, m, "song.mp3")
	settlePane(t, m)
	if got := strings.Join(paneText(m), "\n"); !strings.Contains(got, "音声は表示できません") {
		t.Fatalf("音声の理由が出ない:\n%s", got)
	}
	cdTo(t, m, "bin.dat")
	settlePane(t, m)
	if got := strings.Join(paneText(m), "\n"); !strings.Contains(got, "バイナリは表示できません") {
		t.Fatalf("バイナリの理由が出ない:\n%s", got)
	}
}

// 本文は欄の高さで切り、長い行は欄の幅で折り返す。
func TestLoadPaneFitsBody(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "long.txt")
	mustWrite(t, p, strings.Repeat(strings.Repeat("x", 25)+"\n", 50))
	r := loadPane(paneKey{path: p, raw: "long.txt", name: "long.txt", w: 20, h: 10, wrap: true})
	if r.note != "" || len(r.lines) != 10 {
		t.Fatalf("行 %d (note %q)。高さ 10 で切るはず", len(r.lines), r.note)
	}
	for i, l := range r.lines {
		if w := termwidth.Of(termwidth.StripSGR(l)); w > 20-r.gw {
			t.Fatalf("%d 行目が %d 桁 (本文の幅 %d を超える)", i, w, 20-r.gw)
		}
	}
}
