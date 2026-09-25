package main

import (
	"bytes"
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"pro-con/dispatcher"
	"pro-con/store"
	"pro-con/wake"
)

// 本物の起動の配線 (runDispatcher): 開いた socket で、箱に置かれたら待たずに回り、適用したら画面へ知らせる。
// 止める頼みもすぐ届く。待ちの間隔は 1 時間に延ばし、回るのは起こされたときだけにする (e2e モード。claude を起動しない)。
func TestRunDispatcherWiresSocket(t *testing.T) {
	old := dispatcherInterval
	dispatcherInterval = time.Hour
	t.Cleanup(func() { dispatcherInterval = old })
	root, err := os.MkdirTemp("/tmp", "pcrd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	dir := dispatcher.E2E{Root: root}.StateDir()
	var out, errOut bytes.Buffer
	rc := make(chan int, 1)
	go func() { rc <- runDispatcher([]string{"--e2e", root}, "", "", nil, &out, &errOut) }()
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for range 400 {
			if cond() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("%s\nstdout:\n%s\nstderr:\n%s", what, out.String(), errOut.String())
	}
	state := func() store.DispatcherState { s, _, _ := store.LoadDispatcherState(dir); return s }
	waitFor("最初の Tick が回らない", func() bool { return !state().Tick.IsZero() })
	var changes atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := wake.NewSubscriber(dir, func() { changes.Add(1) })
	go sub.Run(ctx)
	first := state().Tick
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "t", Repo: dispatcher.E2ERepo}); err != nil {
		t.Fatal(err)
	}
	waitFor("箱に置いても待ちを切り上げない", func() bool { return state().Tick.After(first) })
	waitFor("適用しても画面へ知らせない", func() bool { return changes.Load() > 0 })
	sctx, scancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer scancel()
	if running, err := dispatcher.RequestStop(sctx, dir, 30*time.Second); !running || err != nil {
		t.Fatalf("止める頼みが届かない: running=%v %v", running, err)
	}
	if r := <-rc; r != 0 {
		t.Fatalf("rc=%d\n%s", r, errOut.String())
	}
}
