package relay

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 500 { // 10 秒
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(what)
}

func fastInterval(t *testing.T) {
	old := minInterval
	minInterval = time.Millisecond
	t.Cleanup(func() { minInterval = old })
}

// 🚨 描画を待たせない: 書き出しが詰まっていても Put は戻る。詰まりが解けたら、途中の枚は捨てて最後の 1 枚を書く。
func TestPutDoesNotWaitForStuckWrite(t *testing.T) {
	fastInterval(t)
	w, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	stuck, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unstick := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unstick) // 失敗で抜けても、Close (defer) が詰まった書き出しを待って固まらない
	var writes atomic.Int32
	w.write = func(p string, data []byte) error {
		if writes.Add(1) == 1 {
			close(stuck)
			<-release // 1 枚目の書き出しを詰まらせる
		}
		return writeAtomic(p, data)
	}
	w.Put(Frame{ANSI: "frame-0"})
	<-stuck
	done := make(chan struct{})
	go func() {
		for i := 1; i <= 1000; i++ {
			w.Put(Frame{ANSI: fmt.Sprintf("frame-%d", i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		unstick()
		t.Fatal("書き出しが詰まっている間に Put が待った (画面の描画が止まる)")
	}
	unstick()
	path := filepath.Join(w.dir, w.ID()+".frame")
	waitFor(t, "詰まりが解けても最後の 1 枚が書かれない", func() bool {
		f, err := Read(path)
		return err == nil && f.ANSI == "frame-1000"
	})
	if n := writes.Load(); n > 3 { // 1 枚目 (詰まった) + 最後の 1 枚 (+ 間に取った 1 枚)。1000 枚を 1 枚ずつ書いていない
		t.Fatalf("途中の枚を捨てずに書いた: 書き出し %d 回", n)
	}
}

// 最新の 1 枚は色を落とした文字も持つ。置き場は自分だけが読める (0700 / 0600)。閉じたらファイルを消す。
func TestFrameFileAndClose(t *testing.T) {
	fastInterval(t)
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w.Put(Frame{ANSI: "\x1b[38;5;196m赤い\x1b[0m 文字", View: true, Width: 80, Height: 24})
	path := filepath.Join(dir, Dir, w.ID()+".frame")
	waitFor(t, "1 枚目が書かれない", func() bool { _, err := os.Stat(path); return err == nil })
	f, err := Read(path)
	if err != nil || f.Plain != "赤い 文字" || !f.View || f.ID != w.ID() || f.PID != os.Getpid() {
		t.Fatalf("1 枚: %+v %v", f, err)
	}
	for p, want := range map[string]os.FileMode{filepath.Join(dir, Dir): 0o700, path: 0o600, filepath.Join(dir, Dir, w.ID()+".lock"): 0o600} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != want {
			t.Fatalf("%s の権限が %v (期待 %v): %v", p, st.Mode().Perm(), want, err)
		}
	}
	ss, err := List(dir)
	if err != nil || len(ss) != 1 || !ss[0].Open || ss[0].Path != path {
		t.Fatalf("開いている画面が並ばない: %+v %v", ss, err)
	}
	w.Close()
	if ss, _ := List(dir); len(ss) != 0 {
		t.Fatalf("閉じた後も中継が残る: %+v", ss)
	}
	if es, _ := os.ReadDir(filepath.Join(dir, Dir)); len(es) != 0 { // List は印しか見ないので、1 枚のファイルの残りは中身で見る
		t.Fatalf("閉じた後も中継のファイルが残る: %v", es)
	}
	w.Put(Frame{ANSI: "閉じた後"}) // 閉じた後の Put は何もしない (落ちない)
}

// 落ちた画面の残り (flock が外れた印) は、開いている画面と見分ける。読む側は消さない。次に開く画面が残りだけを片付ける。
func TestLeftoversAreToldApartAndSweptByNextOpen(t *testing.T) {
	fastInterval(t)
	dir := t.TempDir()
	d := filepath.Join(dir, Dir)
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Put(Frame{ANSI: "開いている画面"})
	waitFor(t, "1 枚目が書かれない", func() bool { _, err := os.Stat(filepath.Join(d, w.ID()+".frame")); return err == nil })
	for _, n := range []string{"dead.lock", "dead.frame", "orphan.frame", "keep.txt"} { // 落ちた画面の残り (+ 印の無い 1 枚、関係ないファイル)
		if err := os.WriteFile(filepath.Join(d, n), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ss, err := List(dir)
	if err != nil || len(ss) != 2 {
		t.Fatalf("List: %+v %v", ss, err)
	}
	for _, s := range ss {
		if (s.ID == "dead") == s.Open {
			t.Fatalf("開いている / 閉じたの見分けが違う: %+v", s)
		}
	}
	if _, err := os.Stat(filepath.Join(d, "dead.lock")); err != nil {
		t.Fatal("読む側が閉じた画面の残りを消した")
	}
	w2, err := Open(dir) // 次に開く画面が片付ける
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	for _, n := range []string{"dead.lock", "dead.frame", "orphan.frame"} {
		if _, err := os.Stat(filepath.Join(d, n)); err == nil {
			t.Fatalf("落ちた画面の残り %s が片付かない", n)
		}
	}
	for _, n := range []string{w.ID() + ".lock", w.ID() + ".frame", "keep.txt"} {
		if _, err := os.Stat(filepath.Join(d, n)); err != nil {
			t.Fatalf("開いている画面のファイル / 関係ないファイル %s まで消した", n)
		}
	}
}

// 中身が変わらない描き直し (tick 等) は書かない。At は最後に中身が変わった時刻のまま。
func TestSameContentIsNotRewritten(t *testing.T) {
	fastInterval(t)
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var writes atomic.Int32
	w.write = func(p string, data []byte) error { writes.Add(1); return writeAtomic(p, data) }
	t0 := time.Now()
	w.Put(Frame{ANSI: "同じ", At: t0, State: map[string]string{"mode": "board"}})
	waitFor(t, "1 枚目が書かれない", func() bool { return writes.Load() == 1 })
	taken := func() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.latest == nil }
	for i := range 5 {
		// 1 枚ずつ書き出しの側が取るのを待つ (続けて置くと途中の枚が捨てられ、中身が同じでも書く退行が回数に出ない)
		w.Put(Frame{ANSI: "同じ", At: t0.Add(time.Duration(i+1) * time.Second), State: map[string]string{"mode": "board"}})
		waitFor(t, "書き出しの側が 1 枚を取らない", taken)
	}
	w.Put(Frame{ANSI: "変わった", At: t0.Add(time.Minute), State: map[string]string{"mode": "board"}})
	path := filepath.Join(dir, Dir, w.ID()+".frame")
	waitFor(t, "変わった 1 枚が書かれない", func() bool { f, err := Read(path); return err == nil && f.ANSI == "変わった" })
	if n := writes.Load(); n != 2 {
		t.Fatalf("中身が同じ描き直しも書いた: 書き出し %d 回 (期待 2)", n)
	}
}

// 書き出しに失敗した 1 枚は「書いた」ことにしない (同じ中身の描き直しで書き直す)。
func TestFailedWriteIsRetriedBySameContent(t *testing.T) {
	fastInterval(t)
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var calls atomic.Int32
	w.write = func(p string, data []byte) error {
		if calls.Add(1) == 1 {
			return os.ErrPermission // 1 回目だけ失敗させる (一時的な ENOSPC 等)
		}
		return writeAtomic(p, data)
	}
	taken := func() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.latest == nil }
	w.Put(Frame{ANSI: "B"})
	waitFor(t, "1 回目の書き出しが走らない", func() bool { return calls.Load() == 1 && taken() })
	w.Put(Frame{ANSI: "B"}) // 同じ中身の描き直し
	path := filepath.Join(dir, Dir, w.ID()+".frame")
	waitFor(t, "書き出しに失敗した 1 枚が、同じ中身の描き直しで書き直されない", func() bool { f, err := Read(path); return err == nil && f.ANSI == "B" })
}

// 古い一時ファイル (作ってから rename するまでに落ちた残り) は次に開く画面が片付ける。新しいもの (作っている最中) には触らない。
func TestSweepRemovesOnlyStaleTmp(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, Dir)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	old, fresh := filepath.Join(d, ".tmp-old"), filepath.Join(d, ".tmp-fresh")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * staleTmp)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := os.Stat(old); err == nil {
		t.Fatal("古い一時ファイルが片付かない")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("作っている最中かもしれない新しい一時ファイルを消した")
	}
}

// 普通のファイルでない印 (FIFO) は開かずに飛ばす (開くと止まる)。一覧はほかの画面を出す。
func TestListSkipsNonRegularLock(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := syscall.Mkfifo(filepath.Join(dir, Dir, "fifo.lock"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan []Screen, 1)
	go func() { ss, _ := List(dir); done <- ss }()
	select {
	case ss := <-done:
		if len(ss) != 1 || ss[0].ID != w.ID() || !ss[0].Open {
			t.Fatalf("一覧: %+v", ss)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("FIFO の印を開いて一覧が止まった")
	}
}
