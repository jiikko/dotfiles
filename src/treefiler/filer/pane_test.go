package filer

import (
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// ファイルの中身は裏で読む: 読み込みは、それを頼んだ Advance が戻ってから先へ進める (UI の goroutine で読むと、Advance が
// 読み込みの終わりを待つので、読み込みは Advance の戻りを見られない)。届いてから今のファイルの中身になる。
func TestPaneLoadsAsync(t *testing.T) {
	m := newTest(t)
	returned := make(chan struct{})
	var sawReturn atomic.Bool
	m.pane.loader = func(k paneKey) paneResult {
		select {
		case <-returned:
			sawReturn.Store(true)
		case <-time.After(5 * time.Second): // hang guard (同期で読む退行で、Advance が永久に戻らないのを止める)
		}
		return loadPane(k)
	}
	cdTo(t, m, "a/one.txt")
	m.Advance(m.lastAdv.Add(200 * time.Millisecond))
	close(returned)
	settlePane(t, m)
	if !sawReturn.Load() {
		t.Fatal("読み込みが、頼んだ Advance の戻りより先に終わった (UI の goroutine で読んでいる)")
	}
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

// blockLoader は m の欄の読み込みを release を呼ぶまで止める (後始末でも放す)。止めるのは 5 秒まで: UI の goroutine で読む
// 退行では、止めた読み込みを待つ Advance が戻らず、テストが赤ではなく止まって判定できなくなる。
func blockLoader(t *testing.T, m *Model) (release func()) {
	ch := make(chan struct{})
	m.pane.loader = func(k paneKey) paneResult {
		select {
		case <-ch:
		case <-time.After(5 * time.Second): // 上限 (hang guard)
		}
		return loadPane(k)
	}
	var once sync.Once
	release = func() { once.Do(func() { close(ch) }) }
	t.Cleanup(release)
	return release
}

// paneBodyColors は欄の本文 (見出しの下) の空白でないセルの色の集合。
func paneBodyColors(m *Model) map[rgb]bool {
	c := m.draw()
	set := map[rgb]bool{}
	for y := 1; y < m.canvasH(); y++ {
		for x := m.w - m.paneWidth() + 1; x < m.w; x++ {
			if p := c.at(x, y); p.s != " " && p.s != "" {
				set[p.fg] = true
			}
		}
	}
	return set
}

// 今のカーソルの中身が届くまでは、前の本文を薄い色で出す。届いたら薄い色は消える。
func TestPaneDimsUntilCurrentArrives(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	settlePane(t, m)
	dim := mix(cMuted, cBg, 0.4)
	if paneBodyColors(m)[dim] {
		t.Fatal("前提: 届いた本文が薄い")
	}
	release := blockLoader(t, m)
	cdTo(t, m, "a/two.go")
	m.Advance(m.lastAdv.Add(200 * time.Millisecond))
	if !paneBodyColors(m)[dim] {
		t.Fatal("読み込み中に前の本文が薄くならない (前のファイルの中身を今のものとして出している)")
	}
	release()
	settlePane(t, m)
	if paneBodyColors(m)[dim] {
		t.Fatal("届いた後も薄い")
	}
}

// 古い頼みの結果が遅れて届いても、先に届いた新しい結果を消さない。
func TestPaneOfferKeepsNewest(t *testing.T) {
	var p paneState
	p.offer(paneResult{gen: 2})
	p.offer(paneResult{gen: 1})
	if p.latest.gen != 2 {
		t.Fatalf("遅れて届いた世代 1 が世代 2 を上書きした")
	}
}

// フォルダの上では頼まない。待っている間にフォルダへ移ったら待つのをやめる (読み込みが返らなくても Busy が下りる)。
// 同じファイルへ戻ったら頼み直す。
func TestPaneFolderStopsWaiting(t *testing.T) {
	m := newTest(t)
	release := blockLoader(t, m)
	cdTo(t, m, "c.txt")
	m.Advance(m.lastAdv.Add(200 * time.Millisecond))
	if !m.paneLoading() {
		t.Fatal("前提: 頼んでいない")
	}
	gen := m.pane.gen
	cdTo(t, m, "b")
	m.Advance(m.lastAdv.Add(200 * time.Millisecond))
	if m.pane.gen != gen {
		t.Fatal("フォルダの上で頼んだ")
	}
	if m.paneLoading() {
		t.Fatal("フォルダへ移っても、返らない読み込みを待ち続ける")
	}
	// 同じファイルへ戻ったら頼み直す (待つのをやめたので、前の頼みの結果を取り込む Advance は来ない)
	cdTo(t, m, "c.txt")
	m.Advance(m.lastAdv.Add(200 * time.Millisecond))
	if m.pane.gen == gen || !m.paneLoading() {
		t.Fatal("同じファイルへ戻っても頼み直さない (前の本文が薄いまま残る)")
	}
	release()
}

// 裏の読み込みの panic はプロセスを落とさず、欄に理由を 1 行出す。
func TestPaneLoaderPanicIsNote(t *testing.T) {
	r := loadPaneSafe(func(paneKey) paneResult { panic("boom") }, paneKey{name: "x.go"}, 7)
	if r.gen != 7 || !strings.Contains(r.note, "内部エラー") {
		t.Fatalf("panic の結果: gen %d note %q", r.gen, r.note)
	}
}

// Markdown は整形して出す (見出しの # を外す)。
func TestLoadPaneRendersMarkdown(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.md")
	mustWrite(t, p, "# Title\n\nbody text\n")
	r := loadPane(paneKey{path: p, raw: "r.md", name: "r.md", w: 30, h: 10, wrap: true})
	got := termwidth.StripSGR(strings.Join(r.lines, "\n"))
	if !strings.Contains(got, "Title") || strings.Contains(got, "# Title") || !strings.Contains(got, "body text") {
		t.Fatalf("Markdown を整形していない:\n%s", got)
	}
}

// 幅を変えた直後 (Advance の前に描く。glogx は Resize の直後に描く) は、前の幅で作った本文を今の中身として出さない。
func TestPaneDimsAfterResizeBeforeAdvance(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	settlePane(t, m)
	m.Resize(m.w+20, m.h)
	if !paneBodyColors(m)[mix(cMuted, cBg, 0.4)] {
		t.Fatal("幅を変えた直後に、前の幅の本文を今の中身として出した")
	}
}
