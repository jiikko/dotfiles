package filer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// issue 691 の回帰テスト (2026-10-09 の監査で再現した不具合)。

// 続きのバイト (0x80〜0xBF) だけが改行なしに続く不正な UTF-8 でも、読み込みが進む (固まらない)。
func TestFeedInvalidContinuationDoesNotHang(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.txt")
	body := append([]byte("A"), make([]byte, 40000)...)
	for i := 1; i < len(body); i++ {
		body[i] = 0x80
	}
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := openText(p)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		src.ensure(50)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second): // hang guard (実時間の合否ではない。止まらないときだけ落とす)
		t.Fatal("ensure が戻らない (切れ目 0 で空の行を足し続けている)")
	}
	if !src.eof || len(src.lines) < 2 {
		t.Fatalf("読み切れていない: eof=%v lines=%d", src.eof, len(src.lines))
	}
}

// root が / でも、パスから node を引ける (root.abs + "/" が // にならない)。
func TestRootSlashFindsNodes(t *testing.T) {
	m, err := New("/", Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	if n := m.findNode("/usr"); n == nil {
		t.Fatal("root が / のとき findNode(/usr) が nil")
	}
	if n := m.loadPath("/usr/bin"); n == nil || !n.dir {
		t.Fatal("root が / のとき loadPath(/usr/bin) が nil")
	}
	if !related("/", "/usr/bin") {
		t.Fatal("/ が /usr/bin の祖先と見なされない (walker の forget)")
	}
}

// /usr のように親が / のフォルダから、- で / へ上がれる。/ ではそれ以上上がらず知らせる。
func TestRerootUpToSlash(t *testing.T) {
	m, err := New("/usr", Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	m.HandleKey("-")
	if m.root.path() != "/" {
		t.Fatalf("/usr から上がった root = %q", m.root.path())
	}
	m.TakeNotices()
	m.HandleKey("-")
	if ns := m.TakeNotices(); len(ns) != 1 || ns[0].OK {
		t.Fatalf("/ でさらに上がったときの知らせ = %+v", ns)
	}
}

// リンクの解決は、どのフォルダのファイルから読んだかで決まる (同じ文字列でもキャッシュを使い回さない)。
func TestLinkCacheKeyedByBase(t *testing.T) {
	m := newTest(t)
	for _, rel := range []string{"a/util.go", "b/util.go"} {
		if err := os.WriteFile(filepath.Join(m.root.abs, rel), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := resolveLink("util.go", m.root.abs, filepath.Join(m.root.abs, "a"), m.linkCache)
	b := resolveLink("util.go", m.root.abs, filepath.Join(m.root.abs, "b"), m.linkCache)
	if a != filepath.Join(m.root.abs, "a", "util.go") || b != filepath.Join(m.root.abs, "b", "util.go") {
		t.Fatalf("a=%s b=%s", a, b)
	}
	// 無かったファイルを後から作ると、読み直しの後はリンクになる (負のキャッシュを捨てる)
	if p := resolveLink("later.txt", m.root.abs, m.root.abs, m.linkCache); p != "" {
		t.Fatalf("前提: まだ無い later.txt = %q", p)
	}
	if err := os.WriteFile(filepath.Join(m.root.abs, "later.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Refresh()
	if p := resolveLink("later.txt", m.root.abs, m.root.abs, m.linkCache); p == "" {
		t.Fatal("読み直した後も、無かった頃の解決を使った")
	}
}

// 閉じたら基準と溜まった変化を捨てる (開き直したときに閉じていた間の変化を偽の光で出さない)。
func TestWatcherCloseForgetsBaseline(t *testing.T) {
	w := newWatcher()
	w.start()
	w.setDirs([]string{"/x"}, func(string) map[string]entrySig { return map[string]entrySig{"f": {}} })
	w.mu.Lock()
	w.pending = append(w.pending, dirChange{dir: "/x"})
	w.mu.Unlock()
	w.close()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.seen) != 0 || len(w.pending) != 0 {
		t.Fatalf("閉じた後に基準 %d・変化 %d が残った", len(w.seen), len(w.pending))
	}
}

// 周の途中に setDirs が足した基準を、周の終わりに消さない。
func TestWatcherKeepsBaselineAddedMidCycle(t *testing.T) {
	w := newWatcher()
	stop := make(chan struct{})
	w.want = []string{"/old"}
	w.seen = map[string]map[string]entrySig{"/old": {}}
	// 周の始めに want = [/old] を写した後、UI が /new を開いた
	w.setDirs([]string{"/old", "/new"}, func(string) map[string]entrySig { return map[string]entrySig{"f": {}} })
	if !w.commit(stop, map[string]map[string]entrySig{"/old": {}}, nil) {
		t.Fatal("前提: 止めていないのに書き込まなかった")
	}
	if _, ok := w.seen["/new"]; !ok {
		t.Fatal("周の途中に足した基準を消した (開いた直後の変化を取りこぼす)")
	}
}

// walker: 古くなっても値は残し (熱の色を消さない)、走査中に古くなった結果は古い印のまま書く。
func TestWalkerForgetKeepsValuesAndMarksStale(t *testing.T) {
	w := newWalker()
	w.results["/r"] = walkResult{bytes: 5, complete: true}
	w.results["/r/a"] = walkResult{bytes: 3, complete: true}
	w.results["/other"] = walkResult{bytes: 9, complete: true}
	w.last["/r"] = time.Now()
	w.forget("/r/a", false)
	if r := w.results["/r"]; !r.stale || r.bytes != 5 {
		t.Fatalf("祖先 /r = %+v (値を残して古い印)", r)
	}
	if w.results["/other"].stale {
		t.Fatal("関係の無い /other まで古い印")
	}
	d, _, ok := w.take(0)
	if !ok || !d["/r"].stale || d["/r"].bytes != 5 {
		t.Fatalf("取り込み側に古い印と値が渡らない: %+v", d)
	}
	w.request("/r") // 直前に数えたので、間を空ける
	if len(w.queue) != 0 {
		t.Fatal("数え直しの間隔を待たずに積んだ")
	}
	w.forget("/r", true) // 読み直し (r) は待たない
	w.mu.Lock()
	w.running = true // 走査を起こさずに積む
	w.mu.Unlock()
	w.request("/r")
	if len(w.queue) != 1 {
		t.Fatal("読み直しの後に数え直さない")
	}
}

func TestWalkerResultScannedDuringForgetStaysStale(t *testing.T) {
	dir := t.TempDir()
	w := newWalker()
	w.mu.Lock()
	w.running = true
	mark := len(w.forgotLog)
	w.mu.Unlock()
	w.forget(dir, false) // 走査の途中に中が変わった
	found := map[string]walkResult{}
	budget := walkCap
	walkDir(dir, &budget, found)
	w.store(found, w.forgotLog[mark:], time.Now())
	if !w.results[dir].stale {
		t.Fatal("走査中に古くなった結果を新しいものとして書いた (数え直されない)")
	}
}

// 板の外 (. キー) の保存の失敗と、設定の悪い行は toast で知らせる。
func TestSettingsErrorsAreNotified(t *testing.T) {
	m := newTest(t)
	bad := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(bad, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TREEFILER_CONFIG_DIR", filepath.Join(bad, "sub")) // ファイルの下には作れない
	m.TakeNotices()
	m.HandleKey(".")
	if ns := m.TakeNotices(); len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Text, "保存できません") {
		t.Fatalf("板の外の保存の失敗が出ない: %+v", ns)
	}
	cfg := t.TempDir()
	t.Setenv("TREEFILER_CONFIG_DIR", cfg)
	if err := os.WriteFile(filepath.Join(cfg, "treefiler.toml"), []byte("nope = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m2, err := New(m.root.abs, Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	if ns := m2.TakeNotices(); len(ns) == 0 || !strings.Contains(ns[0].Text, "読めない行") {
		t.Fatalf("設定の悪い行が出ない: %+v", ns)
	}
}

// タイルの読み込みが途中で失敗したら、末尾と見分けて知らせる。
func TestTileReadErrorIsShown(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/long.txt", strings.Repeat("x\n", 50000))
	tl := openTileAt(t, m, "a/long.txt")
	if err := os.Remove(filepath.Join(m.root.abs, "a", "long.txt")); err != nil {
		t.Fatal(err)
	}
	m.HandleKey("G")
	if !tl.src.readErr {
		t.Fatal("消えたファイルの続きを読めなかったのに、末尾と同じ扱い")
	}
	m.Advance(fixedNow)
	m.snapAll()
	if !strings.Contains(strings.Join(m.draw().plain(), "\n"), "途中で読めなくなりました") {
		t.Fatal("タイルに出ない")
	}
}

func TestBranchLabelEmptyDoesNotPanic(t *testing.T) {
	if got := branchLabel(""); got != "" {
		t.Fatalf("branchLabel(\"\") = %q", got)
	}
}

// 古くなって数え直しを待っている間は Busy (tick を止めない)。期限が来たら数え直して古い印が消える。
func TestStaleRecountKeepsBusyUntilDone(t *testing.T) {
	m := newTest(t)
	settleBackground(t, m)
	m.walker.forget(m.root.path(), false) // ライブ更新と同じ (読み直しではない)
	m.Advance(fixedNow)
	if !m.Busy() {
		t.Fatal("数え直しを待っているのに Busy が false (tick が止まり、古い値のまま残る)")
	}
	settleBackground(t, m) // walker の時計を進めながら、数え直しが終わるまで回す
	if m.recs[m.root.path()].stale {
		t.Fatal("待ちが終わったのに数え直していない")
	}
}

// 走査の途中に読み直し (r) が来ても、数え直しの間隔を待たせない。
func TestForcedForgetDuringScanDoesNotDelay(t *testing.T) {
	dir := t.TempDir()
	w := newWalker()
	w.mu.Lock()
	w.running = true
	mark := len(w.forgotLog)
	w.mu.Unlock()
	w.forget(dir, true)
	found := map[string]walkResult{}
	budget := walkCap
	walkDir(dir, &budget, found)
	w.mu.Lock()
	w.store(found, w.forgotLog[mark:], time.Now())
	w.mu.Unlock()
	w.request(dir)
	if len(w.queue) != 1 {
		t.Fatal("走査中の r の後に数え直しを待たせた")
	}
}

// 読んでいる途中の失敗 (ファイルがフォルダに差し替わった) も、末尾と見分ける。
func TestTileReadErrorMidRead(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/long.txt", strings.Repeat("x\n", 50000))
	tl := openTileAt(t, m, "a/long.txt")
	p := filepath.Join(m.root.abs, "a", "long.txt")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0o755); err != nil { // Open は通り、Read が失敗する
		t.Fatal(err)
	}
	m.HandleKey("G")
	if !tl.src.readErr {
		t.Fatal("読んでいる途中の失敗を末尾と同じ扱いにした")
	}
}

// 検索中にライブ更新で消えた一致は、Tab で移る先から外す。
func TestSearchSkipsVanishedMatch(t *testing.T) {
	m := newTest(t)
	m.HandleKey("/")
	for _, r := range "file" {
		m.HandleInput(string(r), string(r))
	}
	if len(m.search.matches) < 2 {
		t.Fatalf("前提: file の一致が 2 つ以上 (%d)", len(m.search.matches))
	}
	gone := m.search.matches[1]
	if err := os.Remove(gone.path()); err != nil {
		t.Fatal(err)
	}
	m.root.reload()
	m.HandleInput("tab", "")
	if m.cur == gone || !m.inTree(m.cur) {
		t.Fatalf("消えた一致へ移った: %s", m.cur.name)
	}
}
