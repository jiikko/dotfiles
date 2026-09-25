package presence

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
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
	if n, err := a.Leave(); err != nil || n != 2 {
		t.Fatalf("ほかに 2 つ開いているのに %d (%v)", n, err)
	}
	if n := mustCount(t, dir); n != 2 {
		t.Fatalf("閉じた画面を数えた: %d", n)
	}
	if n, err := b.Leave(); err != nil || n != 1 {
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
			go func() { defer wg.Done(); n, _ := s.Leave(); res <- n }()
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
