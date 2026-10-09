package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// filerDir は treefiler で開く小さな木 (pwd の代わりに t.Chdir する)。
func filerDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.mp3", "sub/c.txt"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newFilerBrowse(t *testing.T) *browseModel {
	t.Helper()
	t.Chdir(filerDir(t))
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.width, m.height = 100, 30
	return m
}

// F は一覧から開き、F で閉じて一覧へ戻る (spec §0.1)。diff の上からも開く (R / D と同じ判定位置)。
func TestFilerOpensWithFAndClosesWithF(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	if m.activeFullScreen() != fullScreenFiler {
		t.Fatalf("F で開かない: active=%v", m.activeFullScreen())
	}
	if !strings.Contains(strings.Join(m.filerV.lines(100, 29), "\n"), "a.txt") {
		t.Fatal("pwd の中身が描かれていない")
	}
	m.handleKey("F")
	if m.activeFullScreen() != fullScreenNone {
		t.Fatal("F で閉じない")
	}
	m.diffOv.sha = m.commits[0].SHA
	m.handleKey("F")
	if m.activeFullScreen() != fullScreenFiler {
		t.Fatal("diff の上から F で開かない")
	}
}

// q は glogx ごと終える (issues viewer に揃える。spec §0.1)。
func TestFilerQQuitsGlogx(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	if _, cmd := m.handleKey("q"); !isQuitCmd(cmd) {
		t.Fatal("ファイラーの木の上の q で glogx が終わらない")
	}
}

// C は claude update ではなくファイラーの「経路以外を全部畳む」へ届く (spec §0.1)。
func TestFilerTakesCFromUpdate(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	if m.updateKeyReachable("C") {
		t.Fatal("ファイラーを開いている間に C が claude update へ横取りされる")
	}
	if !m.updateKeyReachable("X") {
		t.Fatal("ファイラーの上で X (codex update) が効かない (spec §0.3 で効かせると決めた)")
	}
}

// 開けないファイルの知らせは glogx の toast に出る (失敗の赤)。
func TestFilerNoticeGoesToToast(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	m.handleKey("j") // a.txt → b.mp3
	m.handleKey("enter")
	if !strings.Contains(m.toast.Text(), "音声") || m.toast.OK() {
		t.Fatalf("音声の断りが失敗の toast に出ない: %q ok=%v", m.toast.Text(), m.toast.OK())
	}
}

// glogx の tick で動きが進み、止まったら tick のチェーンが終わる。開いた直後から tick が回ること
// (回らないと透明のまま次のキーまで何も見えない) も、ここで固定する。
func TestFilerAnimationAdvancesOnTickAndStops(t *testing.T) {
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	orig := timeNow
	timeNow = func() time.Time { clock = clock.Add(20 * time.Millisecond); return clock }
	t.Cleanup(func() { timeNow = orig })
	m := newFilerBrowse(t)
	m.handleKey("F")
	if !m.spinnerActive() {
		t.Fatal("開いた直後に tick が回らない (フェードインが進まず画面が透明のまま)")
	}
	for range 200 {
		if !m.spinnerActive() {
			break
		}
		m.Update(tickMsg{})
	}
	if m.filerV.animating() {
		t.Fatal("tick を 200 回回しても止まらない")
	}
	if !strings.Contains(strings.Join(m.filerV.lines(100, 29), "\n"), "a.txt") {
		t.Fatal("tick で動きが進んでいない (項目が見えない)")
	}
}

// 閉じて開き直すと、閉じている間に増えたファイルが出る (レビューの指摘 2026-10-08)。
func TestFilerReopenRefreshes(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	m.handleKey("F")
	if err := os.WriteFile("late.txt", []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.handleKey("F")
	settleFiler(m)
	if !strings.Contains(strings.Join(m.filerV.lines(100, 29), "\n"), "late.txt") {
		t.Fatal("開き直しても新しいファイルが出ない")
	}
}

// 画面の大きさが変わったら tick が回る (カメラの目標が変わるので)。U は利用枠を重ねる。
func TestFilerResizeTicksAndUToggles(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	m.filerV.resize(m.contentWidth(), m.pageSize())
	settleFiler(m)
	if m.filerV.animating() {
		t.Fatal("前提: 止まっていない")
	}
	m.ticking = false // 止まった後は tick のチェーンも終わっている (テストの中では tickMsg を流していないので手で戻す)
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40}); cmd == nil || !m.filerV.animating() {
		t.Fatal("大きさが変わっても tick が回らない")
	}
	before := m.usageOv.visible
	m.handleKey("U")
	if m.usageOv.visible == before {
		t.Fatal("ファイラーの上で U が利用枠を切り替えない")
	}
}

// settleFiler はファイラーの動きを止まるまで進める (時刻を 20ms ずつ進めて Advance を呼ぶ)。
func settleFiler(m *browseModel) {
	at := timeNow()
	for i := range 200 {
		m.filerV.advance(at.Add(time.Duration(i) * 20 * time.Millisecond))
	}
}

// 閉じるとライブ更新のポーリングが止まる (閉じた後も 1 秒ごとにディスクを読み続けない)。
func TestFilerCloseStopsWatching(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	if m.filerV.watchCmd() == nil {
		t.Fatal("開いているのにライブ更新を待たない")
	}
	ch := m.filerV.f.Changed()
	m.handleKey("F")
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("閉じた後にチャネルが閉じていない (合図が来た)")
		}
	case <-time.After(5 * time.Second): // hang guard
		t.Fatal("閉じてもポーリングが止まらない (チャネルが閉じない)")
	}
}

// 古いチャネルからの合図では待ち直さない (閉じる直前の合図で待ちが 2 本に増えて戻らない形を止める)。
func TestFilerRearmsOnlyForCurrentChannel(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	cur := m.filerV.f.WatchingChan()
	if m.filerV.rearm(filerChangedMsg{ch: cur, ok: true}) == nil {
		t.Fatal("今のチャネルの合図で待ち直さない")
	}
	old := make(chan struct{})
	if m.filerV.rearm(filerChangedMsg{ch: old, ok: true}) != nil {
		t.Fatal("古いチャネルの合図で待ち直した (待ちが 2 本になる)")
	}
}

// treefiler の検索欄の入力中は、端末のカーソルを入力欄に置く (IME の変換中の文字が欄に出るように)。
func TestFilerSearchPlacesCaret(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	m.zoom = appZoom{} // 起動の拡大の演出の途中はカーソルを置かない仕様なので、演出の無い状態で見る
	if m.View().Cursor != nil {
		t.Fatal("入力していないのにカーソルを置いた")
	}
	m.handleKey("/")
	if m.View().Cursor == nil { // View の中で描いて大きさを伝えてからカーソルを決める (実際のアプリと同じ順)
		t.Fatal("検索欄の入力中なのにカーソルを置かない")
	}
	if m.updateKeyReachable("X") {
		t.Fatal("検索欄の入力中に X が codex update に取られる")
	}
}

// s で端末を明け渡すコマンドを組み (作業ディレクトリと $f)、戻ると読み直す (シェルで作ったファイルが出る)。
func TestFilerShellExecAndRefresh(t *testing.T) {
	m := newFilerBrowse(t)
	cmds := stubEditorCapture(t)
	m.handleKey("F")
	_, cmd := m.handleKey("s")
	if len(*cmds) != 1 {
		t.Fatalf("s で端末を明け渡すコマンドを組まない (%d 本)", len(*cmds))
	}
	c := (*cmds)[0]
	wd, _ := os.Getwd()
	if realPath(t, c.Dir) != realPath(t, wd) || !strings.HasSuffix(c.Args[len(c.Args)-1], "-i") {
		t.Fatalf("シェルの作業ディレクトリ / 引数 = %q %v", c.Dir, c.Args)
	}
	found := false
	for _, e := range c.Env {
		if strings.HasPrefix(e, "f=") {
			found = true
		}
	}
	if !found {
		t.Fatal("$f を渡していない")
	}
	if err := os.WriteFile("made-in-shell.txt", []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Update(cmd()) // editorClosedMsg (シェルから戻った)
	settleFiler(m)
	if !strings.Contains(strings.Join(m.filerV.lines(100, 29), "\n"), "made-in-shell.txt") {
		t.Fatal("シェルから戻っても読み直さない")
	}
}

// 検索の入力中は F / U もそのまま文字として入り、同じ字の連打 (ss) も飲まれない (レビューの指摘 2026-10-09)。
func TestFilerSearchTakesFUAndRepeats(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	m.handleKey("/")
	for _, k := range []string{"F", "U", "s", "s"} {
		m.handleKey(k)
	}
	if m.activeFullScreen() != fullScreenFiler || m.usageOv.visible {
		t.Fatal("入力中の F / U で閉じた / 利用枠が開いた")
	}
	if got := m.filerV.f.SearchQuery(); got != "FUss" {
		t.Fatalf("検索語 = %q (F U と連打の s s がそのまま入るはず)", got)
	}
}

// キー一覧 (?) を開いている間の F / i / U は一覧を閉じるだけ (glogx が閉じる・横断する・利用枠を開くと、一覧が開いたまま残る。issue 694 の 6)。
func TestFilerHelpSwallowsCrossKeys(t *testing.T) {
	for _, k := range []string{"F", "i", "U"} { // C は一覧が閉じていても filer の木のキーなので、ここでは判別にならない
		m := newFilerBrowse(t)
		m.handleKey("F")
		m.handleKey("?")
		m.handleKey(k)
		if m.activeFullScreen() != fullScreenFiler || m.usageOv.visible || m.filerV.f.OwnsKeys() {
			t.Fatalf("キー一覧の中の %s: filer に留まって一覧だけ閉じるはず (全画面 %v・利用枠 %v・入力中 %v)",
				k, m.activeFullScreen(), m.usageOv.visible, m.filerV.f.OwnsKeys())
		}
	}
}

// シェルが 0 以外で終わっても「エディタが異常終了しました」を出さない (シェルは最後のコマンドの終了コードで抜けるのが普通)。
func TestFilerShellNonZeroExitIsQuiet(t *testing.T) {
	m := newFilerBrowse(t)
	stubEditorCapture(t)
	m.handleKey("F")
	m.handleKey("s")
	m.Update(editorClosedMsg{err: &exec.ExitError{}})
	if strings.Contains(m.toast.Text(), "エディタ") {
		t.Fatalf("シェルから戻ったのにエディタの文言: %q", m.toast.Text())
	}
}

// 貼り付けは filer の入力欄に入る (入力していないときは filer が捨て、一覧のキーとしても走らない)。
func TestFilerPasteGoesToSearch(t *testing.T) {
	m := newFilerBrowse(t)
	m.handleKey("F")
	m.Update(tea.PasteMsg{Content: "q"})
	if m.done || m.activeFullScreen() != fullScreenFiler {
		t.Fatal("入力していないときの貼り付けがキーとして走った")
	}
	m.handleKey("/")
	m.Update(tea.PasteMsg{Content: "sub"})
	if got := m.filerV.f.SearchQuery(); got != "sub" {
		t.Fatalf("検索語 = %q", got)
	}
}
