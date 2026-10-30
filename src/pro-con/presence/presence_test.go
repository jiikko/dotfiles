package presence

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"pro-con/flockmark"

	"testwait"
)

func open(t *testing.T, dir string) *Screen {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func mustCount(t *testing.T, dir string) int {
	t.Helper()
	n, err := Count(dir)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// 開いている画面を数える (自分を含む)。閉じると伝えた画面は、ほかに開いている数を受ける。
func TestCountAndLeave(t *testing.T) {
	dir := t.TempDir()
	if n := mustCount(t, dir); n != 0 {
		t.Fatalf("何も開いていないのに %d", n)
	}
	a, b := open(t, dir), open(t, dir)
	open(t, dir)
	if n := mustCount(t, dir); n != 3 {
		t.Fatalf("3 つ開いているのに %d", n)
	}
	if n, err := a.Leave(); err != nil || n.Owners != 2 {
		t.Fatalf("ほかに 2 つ開いているのに %d (%v)", n, err)
	}
	if n := mustCount(t, dir); n != 2 {
		t.Fatalf("閉じた画面を数えた: %d", n)
	}
	if n, err := b.Leave(); err != nil || n.Owners != 1 {
		t.Fatalf("ほかに 1 つ開いているのに %d (%v)", n, err)
	}
}

// 同時に閉じても、ちょうど 1 つだけが 0 (最後) を受ける。
func TestSimultaneousLeaveExactlyOneLast(t *testing.T) {
	for range 20 {
		dir := t.TempDir()
		var ss []*Screen
		for range 4 {
			ss = append(ss, open(t, dir))
		}
		res := make(chan int, len(ss))
		var wg sync.WaitGroup
		for _, s := range ss {
			wg.Add(1)
			go func() { defer wg.Done(); n, _ := s.Leave(); res <- n.Owners }()
		}
		wg.Wait()
		close(res)
		zeros := 0
		for n := range res {
			if n == 0 {
				zeros++
			}
		}
		if zeros != 1 {
			t.Fatalf("最後を受けた画面が %d 個 (ちょうど 1 つのはず)", zeros)
		}
	}
}

// 落ちた画面 (flock が外れて印だけ残った) は数えず、印を消す。
func TestCrashedScreenNotCounted(t *testing.T) {
	dir := t.TempDir()
	open(t, dir)
	stale := filepath.Join(dir, Dir, "deadbeef.lock") // 落ちた画面の形: 印はあるが誰も flock を持っていない
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if n := mustCount(t, dir); n != 1 {
		t.Fatalf("落ちた画面を数えた: %d", n)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("落ちた画面の印を消さない")
	}
}

// 印を置くのと flock は 1 手 (ほかの画面が数えている最中に開いても、数えられない画面・置けない画面にならない)。
func TestOpenIsAtomicAgainstConcurrentCount(t *testing.T) {
	dir := t.TempDir()
	open(t, dir) // 数える側の画面
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = Count(dir)
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	for i := range 3000 {
		s, err := Open(dir)
		if err != nil {
			t.Fatalf("%d 回目: 数えている最中に印を置けない: %v", i, err)
		}
		if _, err := os.Stat(s.path); err != nil {
			t.Fatalf("%d 回目: 開いた直後の画面の印が消された (数えられない画面になる)", i)
		}
		s.Close()
	}
}

// 持ち主と join を分けて数える。持ち主の Leave は、join が残っていても持ち主が 0 なら最後を受ける。
func TestTallyByMode(t *testing.T) {
	dir := t.TempDir()
	owner := open(t, dir)
	j, err := OpenAs(dir, Info{Mode: Join, Label: "review", TTY: "/dev/ttys009"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(j.Close)
	ss, err := List(dir)
	if err != nil || len(ss) != 2 {
		t.Fatalf("2 つ開いているのに %v (%v)", ss, err)
	}
	var got Info
	for _, s := range ss {
		if s.ID == j.Info().ID {
			got = s
		}
	}
	if got.Mode != Join || got.Label != "review" || got.TTY != "/dev/ttys009" || got.PID != os.Getpid() || got.Opened.IsZero() {
		t.Fatalf("join の画面の見分けを読めない: %+v", got)
	}
	if n, err := Owners(dir); err != nil || n != 1 {
		t.Fatalf("持ち主は 1 つなのに %d (%v)", n, err)
	}
	left, err := owner.Leave()
	if err != nil || left != (Tally{Owners: 0, Joins: 1}) {
		t.Fatalf("持ち主が閉じたら ほかは join 1 つ (持ち主 0) のはず: %+v (%v)", left, err)
	}
}

// 中身を読めない印 (前の版の画面は空のファイルを置く) は、flock が持たれていれば持ち主として数える。
func TestUnreadableInfoCountsAsOwner(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if err := os.WriteFile(s.path, []byte("{壊れた"), 0o600); err != nil { // flock は s が持ったまま
		t.Fatal(err)
	}
	ss, err := List(dir)
	if err != nil || len(ss) != 1 || ss[0].Mode != Owner || ss[0].ID != s.Info().ID {
		t.Fatalf("中身の壊れた生きている印を持ち主として数えない: %+v (%v)", ss, err)
	}
}

// 普通のファイルでない印 (FIFO 等) は flock できない。1 つあっても、ほかの画面は数えられる。
func TestScanSkipsNonRegularLock(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if err := syscall.Mkfifo(filepath.Join(dir, Dir, "fifo.lock"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var ss []Info
	var err error
	go func() { ss, err = List(dir); close(done) }()
	testwait.Recv(t, "FIFO の印で数え上げが止まった", done, testwait.Within(10*time.Second))
	if err != nil || len(ss) != 1 || ss[0].ID != s.Info().ID {
		t.Fatalf("FIFO の印があると数えられない: %+v (%v)", ss, err)
	}
	if left, err := s.Leave(); err != nil || left != (Tally{}) {
		t.Fatalf("FIFO の印があると最後の持ち主を判定できない: %+v (%v)", left, err)
	}
}

// 作りかけの印 (.tmp-<id>) は数えない。古くて誰も持っていないものだけ片付け、作っている最中 (新しい) と持たれているものには触らない。
func TestScanCleansOnlyStaleUnheldTmp(t *testing.T) {
	dir := t.TempDir()
	open(t, dir)
	d := filepath.Join(dir, Dir)
	old := time.Now().Add(-2 * flockmark.StaleTmp)
	mk := func(name string, stale bool) string {
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if stale {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	dead, fresh, held := mk(".tmp-dead", true), mk(".tmp-fresh", false), mk(".tmp-held", true)
	f, err := os.OpenFile(held, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if n := mustCount(t, dir); n != 1 {
		t.Fatalf("作りかけの印を数えた: %d", n)
	}
	if _, err := os.Stat(dead); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("古くて誰も持っていない作りかけが残った: %v", err)
	}
	for _, p := range []string{fresh, held} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("消してはいけない作りかけ %s が消えた: %v", filepath.Base(p), err)
		}
	}
}
