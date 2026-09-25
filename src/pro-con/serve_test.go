package main

import (
	"context"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"bytes"
	"path/filepath"
	"pro-con/agents"
	"pro-con/dispatcher"
	"pro-con/live"
	"pro-con/presence"
	"pro-con/store"
	"pro-con/wake"
	"strings"
	"sync"
)

// dispatcher は interval を待たずに、依頼を置いた側の Poke ですぐ次の Tick を回す (interval は 1 時間にして、起きるのは Poke だけにする)。
func TestServeWakesOnPoke(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pcsv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	srv, err := wake.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	var ticks atomic.Int32 // Tick ごとに一覧を 1 回取る
	d := &dispatcher.Dispatcher{Dir: dir, Limit: 1, Now: time.Now,
		List: func(context.Context) ([]agents.Session, error) { ticks.Add(1); return nil, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int)
	go func() {
		done <- serve(ctx, d, dir, srv.Wakes(), serveOpts{interval: time.Hour}, io.Discard)
	}()
	wait := func(n int32) {
		t.Helper()
		for range 400 {
			if ticks.Load() >= n {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("Tick が %d 回にならない (%d 回)", n, ticks.Load())
	}
	wait(1)
	if err := wake.Poke(dir); err != nil {
		t.Fatal(err)
	}
	wait(2)
	cancel()
	if rc := <-done; rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
}

// 画面が起こした dispatcher (alone) は、開いている画面がある間は回り続け、1 つも無い状態が続いたら PG を止めて抜ける
// (最後の画面が quit を通らずに消えても PG を残さない)。止めた結果も書く。
func TestServeExitsWithoutScreens(t *testing.T) {
	dir := t.TempDir()
	scr, err := presence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ticks atomic.Int32
	d := &dispatcher.Dispatcher{Dir: dir, Limit: 1, Now: time.Now, Sleep: func(time.Duration) {},
		List: func(context.Context) ([]agents.Session, error) { ticks.Add(1); return nil, nil }}
	done := make(chan int, 1)
	go func() {
		done <- serve(context.Background(), d, dir, nil, serveOpts{interval: 10 * time.Millisecond, alone: 100 * time.Millisecond}, io.Discard)
	}()
	for ticks.Load() < 30 { // 画面が開いている間は、alone の何倍も回っても抜けない
		select {
		case rc := <-done:
			t.Fatalf("画面が開いているのに抜けた (rc=%d, %d 回目)", rc, ticks.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	scr.Close() // 落ちた画面と同じ (Leave しない)
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("rc=%d", rc)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("画面が無くなっても抜けない")
	}
	if b, err := os.ReadFile(filepath.Join(dir, dispatcher.StopResultFile)); err != nil || string(b) != "ok\n" {
		t.Fatalf("止めた結果を書かない: %q %v", b, err)
	}
}

// 画面が起こす dispatcher には「画面が起こした」と「画面が無くなったら抜ける」を付ける (手で起動した dispatcher には付かない)。
func TestSpawnedDispatcherExitsWithoutScreens(t *testing.T) {
	cmd := dispatcherCmd("/bin/pro-con", []string{"--e2e", "/x"})
	if got := strings.Join(cmd.Args[1:], " "); got != "dispatcher --from-screen --exit-without-screens 1m --e2e /x" {
		t.Fatalf("args=%q", got)
	}
}

// 画面の quit の停止は「画面が止めた」と付ける (人が止めた印を置かない。issue 459)。
func TestScreenStopCmdIsFromScreen(t *testing.T) {
	if got := strings.Join(stopCmd("/bin/pro-con", []string{"--e2e", "/x"}).Args[1:], " "); got != "dispatcher --stop --from-screen --e2e /x" {
		t.Fatalf("args=%q", got)
	}
}

// 猶予は「最後の画面が消えてから」数える (起動してからではない)。時計を差し替えて決める。
func TestServeAloneGraceCountsFromLastScreen(t *testing.T) {
	dir := t.TempDir()
	scr, err := presence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	clock := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }
	var ticks atomic.Int32
	d := &dispatcher.Dispatcher{Dir: dir, Limit: 1, Now: time.Now, Sleep: func(time.Duration) {},
		List: func(context.Context) ([]agents.Session, error) { ticks.Add(1); return nil, nil }}
	done := make(chan int, 1)
	go func() {
		done <- serve(context.Background(), d, dir, nil, serveOpts{interval: 5 * time.Millisecond, alone: time.Minute, now: now}, io.Discard)
	}()
	waitTicks := func(n int32) {
		t.Helper()
		start := ticks.Load()
		for ticks.Load() < start+n {
			select {
			case rc := <-done:
				t.Fatalf("早く抜けた (rc=%d)", rc)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	waitTicks(3)
	advance(time.Hour) // 画面が開いたまま長く経った
	waitTicks(3)
	scr.Close()
	waitTicks(3) // 画面が消えた直後: 猶予はここから (起動から数えると、すぐ抜ける)
	advance(time.Minute)
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("rc=%d", rc)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("猶予が過ぎても抜けない")
	}
}

// stopFlaky は止めた回数を数えるだけの PG の偽物 (止まったかは ListAll の偽物が決める)。
type stopFlaky struct {
	mu    sync.Mutex
	stops int
}

func (f *stopFlaky) Start(context.Context, string, string, string) (string, error) { return "", nil }
func (f *stopFlaky) Resume(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (f *stopFlaky) Stop(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}

// 止めきれなければ抜けずに止め直し、止まったら抜けて結果を ok で書き直す。止めている間に画面が開いたら、止めるのをやめて続ける。
// ただし人が止めた印 (issue 459) があれば、画面が開いていても止めきる (画面は印がある間 dispatcher を起こさないので、続ける者が居ない)。
func TestStopUntilDoneRetriesAndYieldsToScreen(t *testing.T) {
	old := stopRetryEvery
	stopRetryEvery = time.Millisecond
	t.Cleanup(func() { stopRetryEvery = old })
	run := func(t *testing.T, aliveLists int, openScreen, held bool) (bool, *dispatcher.Dispatcher, string) {
		dir := t.TempDir()
		if held {
			if err := store.Hold(dir, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if err := live.Register(filepath.Join(dir, live.RegistryFile), live.Owned{SessionID: "S1", ID: "pg1", PID: 42, CardID: "C-001"}); err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		lists := 0
		d := &dispatcher.Dispatcher{Dir: dir, Limit: 1, Now: time.Now, Sleep: func(time.Duration) {}, Launch: &stopFlaky{},
			List: func(context.Context) ([]agents.Session, error) { return nil, nil },
			ListAll: func(context.Context) ([]agents.Session, error) {
				mu.Lock()
				defer mu.Unlock()
				lists++
				state, pid := "working", 42
				if lists > aliveLists {
					state, pid = agents.StateStopped, 0 // 本物と同じく、止めると pid が無くなる
				}
				return []agents.Session{{ID: "pg1", SessionID: "S1", PID: pid, Kind: "background", State: state}}, nil
			}}
		if openScreen {
			sc, err := presence.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sc.Close)
		}
		ok := stopUntilDone(context.Background(), d, dir, nil, serveOpts{}, io.Discard)
		b, _ := os.ReadFile(filepath.Join(dir, dispatcher.StopResultFile))
		return ok, d, string(b)
	}
	// 1 回目の Shutdown の確かめ (一覧を ensurePolls+2 = 17 回取る) の間は止まらず、2 回目の途中で止まる
	if ok, _, res := run(t, 20, false, false); !ok || res != "ok\n" {
		t.Fatalf("止まるまで止め直さない / 結果を ok で書き直さない: ok=%v res=%q", ok, res)
	}
	type result struct {
		ok  bool
		res string
	}
	got := make(chan result, 1)
	if ok, _, res := run(t, 20, true, true); !ok || res != "ok\n" {
		t.Fatalf("人が止めたのに、画面が開いたので止めるのをやめた: ok=%v res=%q", ok, res)
	}
	go func() { ok, _, res := run(t, 1<<30, true, false); got <- result{ok, res} }()
	select {
	case r := <-got:
		if r.ok || r.res == "ok\n" || r.res == "" {
			t.Fatalf("画面が開いたのに止めるのをやめない / 止めたと書いた / 最初の失敗を書かない: ok=%v res=%q", r.ok, r.res)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("画面が開いても止めるのをやめない (戻らない)")
	}
}

// 画面が起こした dispatcher (alone) は、取り消されても (SIGTERM / SIGHUP) 画面が無ければ PG を止めてから抜ける。止めるのは取り消されて
// いない ctx で行う (取り消された ctx のままでは claude の呼び出しが即座に失敗する。偽物も ctx を見る)。画面が開いていれば止めない
// (画面が dispatcher を起こし直し、PG はそのまま続く)。
func TestServeStopsOnSignalWhenSpawnedByScreen(t *testing.T) {
	for _, screenOpen := range []bool{false, true} {
		if stopped, rc := serveUntilSignal(t, screenOpen); stopped == screenOpen || rc != 0 {
			t.Fatalf("画面が開いている=%v で、止めた=%v rc=%d", screenOpen, stopped, rc)
		}
	}
}

func serveUntilSignal(t *testing.T, screenOpen bool) (bool, int) {
	t.Helper()
	dir := t.TempDir()
	if screenOpen {
		sc, err := presence.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer sc.Close()
	}
	if err := live.Register(filepath.Join(dir, live.RegistryFile), live.Owned{SessionID: "S1", ID: "pg1", PID: 42, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	var ticks atomic.Int32
	var stopped atomic.Bool
	l := &ctxLauncher{stopped: &stopped}
	d := &dispatcher.Dispatcher{Dir: dir, Limit: 1, Now: time.Now, Sleep: func(time.Duration) {}, Launch: l,
		List: func(ctx context.Context) ([]agents.Session, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			ticks.Add(1)
			return nil, nil
		},
		ListAll: func(ctx context.Context) ([]agents.Session, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			state, pid := "working", 42
			if stopped.Load() {
				state, pid = agents.StateStopped, 0 // 本物と同じく、止めると pid が無くなる
			}
			return []agents.Session{{ID: "pg1", SessionID: "S1", PID: pid, Kind: "background", State: state}}, nil
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- serve(ctx, d, dir, nil, serveOpts{interval: time.Hour, alone: time.Hour}, io.Discard)
	}()
	for ticks.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	rc := <-done
	if b, err := os.ReadFile(filepath.Join(dir, dispatcher.StopResultFile)); stopped.Load() && (err != nil || string(b) != "ok\n") {
		t.Fatalf("止めた結果を ok で書かない: %q %v", b, err)
	}
	return stopped.Load(), rc
}

// ctxLauncher は ctx が取り消されていたら止められない (本物の exec.CommandContext と同じ) PG の偽物。
type ctxLauncher struct{ stopped *atomic.Bool }

func (l *ctxLauncher) Start(context.Context, string, string, string) (string, error) { return "", nil }
func (l *ctxLauncher) Resume(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (l *ctxLauncher) Stop(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.stopped.Store(true)
	return nil
}

// 止めていた dispatcher が結果を書く前に落ちたら、--stop が止める役を引き継いで止める (e2e モード: 偽の PG)。
func TestStopTakesOverWhenStopperDies(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "pctk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	e := dispatcher.E2E{Root: root}
	dir := e.StateDir()
	if err := os.MkdirAll(e.RepoDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	id, err := e.Launcher().Start(context.Background(), e.RepoDir(), "pc-c-001", "")
	if err != nil {
		t.Fatal(err)
	}
	ss, _ := e.List(context.Background())
	if err := live.Register(filepath.Join(dir, live.RegistryFile), live.Owned{SessionID: ss[0].SessionID, ID: id, PID: ss[0].PID, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	unlock, err := dispatcher.Lock(dir) // 止めている dispatcher の形
	if err != nil {
		t.Fatal(err)
	}
	go func() { // 止める頼みが届いたら、結果を書かずに落ちる
		for range 400 {
			if _, err := os.Stat(filepath.Join(dir, dispatcher.StopRequestFile)); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		unlock()
	}()
	var out bytes.Buffer
	if err := stopDispatcher(context.Background(), dir, "", map[string]string{dispatcher.E2ERepo: e.RepoDir()}, "", &e, &out); err != nil {
		t.Fatalf("引き継いで止めない: %v\n%s", err, out.String())
	}
	all, _ := e.ListAll(context.Background())
	if len(all) != 1 || !all[0].Stopped() {
		t.Fatalf("偽の PG が止まっていない: %+v\n%s", all, out.String())
	}
}

// 取り消された (SIGTERM) とき、画面も同時に閉じる途中なら (ログアウト・pkill)、画面が消えるのを待ってから止める。
func TestServeSignalWaitsForClosingScreens(t *testing.T) {
	old := signalGrace
	signalGrace = 5 * time.Second
	t.Cleanup(func() { signalGrace = old })
	dir := t.TempDir()
	sc, err := presence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Register(filepath.Join(dir, live.RegistryFile), live.Owned{SessionID: "S1", ID: "pg1", PID: 42, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	var ticks atomic.Int32
	var stopped atomic.Bool
	d := &dispatcher.Dispatcher{Dir: dir, Limit: 1, Now: time.Now, Sleep: func(time.Duration) {}, Launch: &ctxLauncher{stopped: &stopped},
		List: func(context.Context) ([]agents.Session, error) { ticks.Add(1); return nil, nil },
		ListAll: func(context.Context) ([]agents.Session, error) {
			state, pid := "working", 42
			if stopped.Load() {
				state, pid = agents.StateStopped, 0 // 本物と同じく、止めると pid が無くなる
			}
			return []agents.Session{{ID: "pg1", SessionID: "S1", PID: pid, Kind: "background", State: state}}, nil
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- serve(ctx, d, dir, nil, serveOpts{interval: time.Hour, alone: time.Hour}, io.Discard)
	}()
	for ticks.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()                          // dispatcher と画面に同時に届いた
	time.Sleep(50 * time.Millisecond) // 画面が閉じるのは dispatcher が見た後 (待ちの窓を作るための入力。判定には使わない)
	sc.Close()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("抜けない")
	}
	if !stopped.Load() {
		t.Fatal("画面が閉じる途中だったのに、止めずに抜けた (PG が残る)")
	}
}

// 画面を数えられない (置き場を読めない) ときは、画面が無いとみなして猶予の後に止める (止める側に倒す。止める判定と同じ向き)。
func TestServeAloneTreatsUncountableAsNoScreens(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, presence.Dir)
	if err := os.MkdirAll(sd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sd, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sd, 0o700) })
	if _, err := presence.Count(dir); err == nil {
		t.Skip("前提: 読めない置き場を作れない (root で走っている?)")
	}
	d := &dispatcher.Dispatcher{Dir: dir, Limit: 1, Now: time.Now, Sleep: func(time.Duration) {},
		List: func(context.Context) ([]agents.Session, error) { return nil, nil }}
	done := make(chan int, 1)
	go func() {
		done <- serve(context.Background(), d, dir, nil, serveOpts{interval: 5 * time.Millisecond, alone: 20 * time.Millisecond}, io.Discard)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("画面を数えられないまま、止めずに回り続けた")
	}
}

// 取り消されて (SIGTERM) 止めようとしたが止めきれないまま抜けるときは、rc=1 で抜ける (止めたふりをしない)。
func TestServeSignalStopFailureExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	if err := live.Register(filepath.Join(dir, live.RegistryFile), live.Owned{SessionID: "S1", ID: "pg1", PID: 42, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	var ticks atomic.Int32
	d := &dispatcher.Dispatcher{Dir: dir, Limit: 1, Now: time.Now, Sleep: func(time.Duration) {}, Launch: &stopFlaky{},
		List: func(context.Context) ([]agents.Session, error) { ticks.Add(1); return nil, nil },
		ListAll: func(context.Context) ([]agents.Session, error) { // 止めても止まらない
			return []agents.Session{{ID: "pg1", SessionID: "S1", PID: 42, Kind: "background", State: "working"}}, nil
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- serve(ctx, d, dir, nil, serveOpts{interval: time.Hour, alone: time.Hour}, io.Discard)
	}()
	for ticks.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case rc := <-done:
		if rc != 1 {
			t.Fatalf("止めきれないまま抜けたのに rc=%d", rc)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("抜けない")
	}
}
