package speedtest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mac-lan-manager/sshdest"
)

var dest = sshdest.Dest{User: "koji", IP: "192.168.100.12", HostKeyAlias: "m2macmini.local"}

// fakeClock は Now を呼ぶたびに進め方を選べる時計。After は手で閉じるチャネルを返す。
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	stops []chan time.Time
}

func (c *fakeClock) clock() Clock {
	return Clock{
		Now: func() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now },
		After: func(time.Duration) <-chan time.Time {
			c.mu.Lock()
			defer c.mu.Unlock()
			ch := make(chan time.Time, 1)
			c.stops = append(c.stops, ch)
			return ch
		},
	}
}

func (c *fakeClock) stopsSnapshot() []chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.stops)
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fire は最後に渡した After を鳴らす (鳴らし済みなら何もしない)。
func (c *fakeClock) fire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case c.stops[len(c.stops)-1] <- c.now:
	default:
	}
}

type fakeProc struct {
	stdin  io.WriteCloser
	stdout io.Reader
	wait   func() error
}

func (p *fakeProc) Stdin() io.WriteCloser { return p.stdin }
func (p *fakeProc) Stdout() io.Reader     { return p.stdout }
func (p *fakeProc) Wait() error           { return p.wait() }

// countingWriter は書かれた量を数え、onWrite で時計を進める (書くたびに時間が経つ)。閉じた後の Write は失敗する (本物のパイプと同じ)。
type countingWriter struct {
	mu      sync.Mutex
	n       int64
	onWrite func(n int64)
	closed  bool
}

func (w *countingWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	w.n += int64(len(b))
	n := w.n
	w.mu.Unlock()
	w.onWrite(n)
	return len(b), nil
}
func (w *countingWriter) Close() error   { w.mu.Lock(); defer w.mu.Unlock(); w.closed = true; return nil }
func (w *countingWriter) isClosed() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.closed }

// 上りは相手の wc が数えた量と、書き始めから相手が数え終えるまでの時間で決める (こちらが書けた量ではない)。
func TestMeasureUpUsesRemoteCount(t *testing.T) {
	c := &fakeClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	var argvs [][]string
	start := func(ctx context.Context, argv []string, withStdin bool) (Proc, error) {
		argvs = append(argvs, argv)
		switch argv[len(argv)-1] {
		case "true":
			return &fakeProc{stdout: strings.NewReader(""), wait: func() error { return nil }}, nil
		case "exec wc -c":
			w := &countingWriter{}
			w.onWrite = func(n int64) {
				if n == 4*chunk { // 4 回書いたら時間切れ (時計は 1 度だけ進める)
					c.advance(time.Second)
					c.fire()
				}
			}
			return &fakeProc{stdin: w, stdout: strings.NewReader("  1250000\n"), wait: func() error {
				if !w.isClosed() {
					return errors.New("stdin を閉じずに待った")
				}
				c.advance(time.Second) // 溜まっていた分が相手に届いて数え終わるまでの時間も測定に入る
				return nil
			}}, nil
		default: // 下り: 1 回目 (起点) + 2 回 × 625000 バイトを 1 秒で
			c.advance(3 * time.Second) // 接続と相手の起動の時間 (起点に入れない)
			ctx2, cancel := context.WithCancel(ctx)
			s := &stream{chunks: [][]byte{make([]byte, 100), make([]byte, 625000), make([]byte, 625000)}, stop: ctx.Done(), stopErr: ctx.Err, onDrained: func() {
				c.advance(time.Second)
				c.fire()
			}, done: cancel}
			return &fakeProc{stdout: s, wait: func() error { <-ctx2.Done(); return nil }}, nil
		}
	}
	r := measureWithin(t, context.Background(), start, c.clock())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.UpBps != 5e6 {
		t.Errorf("上り = %v, want 5 Mbps (相手が数えた 1250000 バイト / 書き始めから数え終わりまでの 2 秒)", r.UpBps)
	}
	if r.DownBps != 10e6 {
		t.Errorf("下り = %v, want 10 Mbps (最初の 1 回を除いた 1250000 バイト / 1 秒)", r.DownBps)
	}
	if len(argvs) != 3 || !strings.Contains(strings.Join(argvs[2], " "), "head -c 8000000000 /dev/zero") {
		t.Errorf("起こした ssh = %q", argvs)
	}
	for _, a := range argvs {
		if !strings.Contains(strings.Join(a, " "), "BatchMode=yes") {
			t.Errorf("BatchMode でない ssh (パスワードを聞いて止まる): %q", a)
		}
	}
}

// 鍵で入れない相手は測る前に断る (上り・下りを起こさない)。
func TestMeasureStopsWhenKeyDenied(t *testing.T) {
	c := &fakeClock{}
	n := 0
	start := func(_ context.Context, argv []string, _ bool) (Proc, error) {
		n++
		return &fakeProc{stdout: strings.NewReader(""), wait: func() error {
			return errors.New("exit status 255: koji@192.168.100.12: Permission denied (publickey,password,keyboard-interactive).")
		}}, nil
	}
	r := measureWithin(t, context.Background(), start, c.clock())
	if !errors.Is(r.Err, ErrNoKey) || n != 1 {
		t.Errorf("Err = %v / 起こした数 = %d, want ErrNoKey / 1", r.Err, n)
	}
}

// 下りで何も届かずに切れたら値を出さない (0 Mbps と言わない)。
func TestMeasureDownWithoutDataIsError(t *testing.T) {
	c := &fakeClock{}
	start := func(_ context.Context, argv []string, withStdin bool) (Proc, error) {
		switch argv[len(argv)-1] {
		case "true":
			return &fakeProc{stdout: strings.NewReader(""), wait: func() error { return nil }}, nil
		case "exec wc -c":
			w := &countingWriter{onWrite: func(int64) { c.advance(time.Second); c.fire() }}
			return &fakeProc{stdin: w, stdout: strings.NewReader("10\n"), wait: func() error { return nil }}, nil
		}
		return &fakeProc{stdout: bytes.NewReader(nil), wait: func() error { return errors.New("exit status 1") }}, nil
	}
	r := measureWithin(t, context.Background(), start, c.clock())
	if r.Err == nil || !strings.Contains(r.Err.Error(), "下り") {
		t.Errorf("Err = %v, want 下りの失敗", r.Err)
	}
}

func TestCapped100M(t *testing.T) {
	for _, c := range []struct {
		up, down float64
		want     bool
	}{{94e6, 93e6, true}, {79e6, 50e6, false}, {100e6, 90e6, false}, {300e6, 92e6, false}, {50e6, 85e6, true}} {
		if got := Capped100M(Result{UpBps: c.up, DownBps: c.down}); got != c.want {
			t.Errorf("Capped100M(%v, %v) = %v, want %v", c.up, c.down, got, c.want)
		}
	}
	if Capped100M(Result{UpBps: 94e6, Err: errors.New("x")}) {
		t.Error("失敗した結果を頭打ちと言った")
	}
}

func TestFormat(t *testing.T) {
	for in, want := range map[float64]string{94.4e6: "94 Mbps", 1.25e9: "1.2 Gbps", 2e9: "2.0 Gbps", 0.3e6: "0.30 Mbps", 999.7e6: "1.0 Gbps"} {
		if got := Format(in); got != want {
			t.Errorf("Format(%v) = %q, want %q", in, got, want)
		}
	}
}

// 上りで相手が読まず Write が詰まっても、時間が来たら stdin を閉じて止める (SpeedTimeout まで待たない)。
func TestMeasureUpStopsWhenWriteBlocks(t *testing.T) {
	c := &fakeClock{}
	unblock := make(chan struct{})
	start := func(_ context.Context, argv []string, _ bool) (Proc, error) {
		switch argv[len(argv)-1] {
		case "true":
			return &fakeProc{stdout: strings.NewReader(""), wait: func() error { return nil }}, nil
		case "exec wc -c":
			pr, pw := io.Pipe() // 誰も読まない: 1 回目の Write から詰まる
			go func() { <-unblock; _ = pr.Close() }()
			go func() {
				for len(c.stopsSnapshot()) < 1 { // After が呼ばれて t0 を取った後に時計を進める (先に進めると測った時間が 0 になった)
					runtime.Gosched()
				}
				c.advance(time.Second)
				c.fire()
			}()
			return &fakeProc{stdin: pw, stdout: strings.NewReader("0\n"), wait: func() error { close(unblock); return nil }}, nil
		}
		return &fakeProc{stdout: bytes.NewReader(nil), wait: func() error { return nil }}, nil
	}
	done := make(chan Result, 1)
	go func() { done <- Measure(context.Background(), dest, start, c.clock(), Duration) }()
	select {
	case r := <-done:
		if r.Err == nil || !strings.Contains(r.Err.Error(), "下り") {
			t.Errorf("Err = %v (上りは止まって 0 を数え、下りで何も届かずに失敗するはず)", r.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("詰まった Write で止まったまま (時間が来ても stdin を閉じていない)")
	}
}

// 下りが時間より前に EOF で終わったら、そこまでの値を測れた値として出さない。ssh の失敗の文も残す。
func TestMeasureDownCutBeforeTimeIsError(t *testing.T) {
	for _, c := range []struct {
		name    string
		waitErr error
		want    string
	}{
		{"ssh fails", errors.New("exit status 127"), "exit status 127"},
		{"clean exit", nil, "時間より前"},
	} {
		t.Run(c.name, func(t *testing.T) {
			clk := &fakeClock{}
			start := func(_ context.Context, argv []string, _ bool) (Proc, error) {
				switch argv[len(argv)-1] {
				case "true":
					return &fakeProc{stdout: strings.NewReader(""), wait: func() error { return nil }}, nil
				case "exec wc -c":
					w := &countingWriter{onWrite: func(int64) { clk.advance(time.Second); clk.fire() }}
					return &fakeProc{stdin: w, stdout: strings.NewReader("10\n"), wait: func() error { return nil }}, nil
				}
				return &fakeProc{stdout: bytes.NewReader(make([]byte, 2*chunk)), wait: func() error { return c.waitErr }}, nil
			}
			r := measureWithin(t, context.Background(), start, clk.clock())
			if r.Err == nil || !strings.Contains(r.Err.Error(), "途中で切れた") || !strings.Contains(r.Err.Error(), c.want) {
				t.Errorf("Result = %+v, want 途中で切れた (%s)", r, c.want)
			}
		})
	}
}

// 下りの途中で取り消しても ssh を回収する (Wait を呼ぶ)。
func TestMeasureDownCancelReapsProcess(t *testing.T) {
	c := &fakeClock{}
	ctx, cancelAll := context.WithCancel(context.Background())
	waited := make(chan struct{})
	start := func(pctx context.Context, argv []string, _ bool) (Proc, error) {
		switch argv[len(argv)-1] {
		case "true":
			return &fakeProc{stdout: strings.NewReader(""), wait: func() error { return nil }}, nil
		case "exec wc -c":
			w := &countingWriter{onWrite: func(int64) { c.advance(time.Second); c.fire() }}
			return &fakeProc{stdin: w, stdout: strings.NewReader("10\n"), wait: func() error { return nil }}, nil
		}
		pr, pw := io.Pipe()
		go func() {
			_, _ = pw.Write(make([]byte, 10))
			cancelAll() // 下りの途中で取り消す (画面を閉じた)
			<-pctx.Done()
			pw.CloseWithError(pctx.Err())
		}()
		return &fakeProc{stdout: pr, wait: func() error { close(waited); return nil }}, nil
	}
	r := measureWithin(t, ctx, start, c.clock())
	if !errors.Is(r.Err, context.Canceled) {
		t.Errorf("Err = %v, want 取り消し", r.Err)
	}
	select {
	case <-waited:
	default:
		t.Error("取り消した下りの ssh を Wait していない (ゾンビが残る)")
	}
}

// measureWithin は Measure を上限つきで走らせる (退行で止まったら赤にする。止まった Measure の goroutine は置いていく)。
func measureWithin(t *testing.T, ctx context.Context, start Start, clk Clock) Result {
	t.Helper()
	done := make(chan Result, 1)
	go func() { done <- Measure(ctx, dest, start, clk, Duration) }()
	select {
	case r := <-done:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("Measure が 10 秒で戻らない")
		return Result{}
	}
}

// stream は下りの相手を演じる読み口。chunks を 1 回の Read に 1 つずつ渡し、渡し切った後の次の Read で onDrained を呼んで
// 止められるまで待つ。測る側の読み手は前の塊を本体に渡してから次を読むので、onDrained の時点で全部が数えられている
// (書き終えた直後に時計を鳴らすと、最後の塊を本体に渡す前に鳴って取りこぼすことがあった)。
type stream struct {
	chunks    [][]byte
	stop      <-chan struct{} // 測る側が ssh を止めた (ctx.Done)
	stopErr   func() error
	onDrained func()
	done      func()
	drained   bool
}

func (s *stream) Read(b []byte) (int, error) {
	if len(s.chunks) > 0 {
		n := copy(b, s.chunks[0])
		if n == len(s.chunks[0]) {
			s.chunks = s.chunks[1:]
		} else {
			s.chunks[0] = s.chunks[0][n:]
		}
		return n, nil
	}
	if !s.drained {
		s.drained = true
		s.onDrained()
	}
	<-s.stop
	s.done()
	return 0, s.stopErr()
}
