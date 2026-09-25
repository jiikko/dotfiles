package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"pro-con/dispatcher"
	"pro-con/live"
	"pro-con/presence"
	"pro-con/store"
)

// heldRoot は e2e モードの置き場 (socket のパスを短くするため /tmp の下) と、その状態の置き場。画面を 1 つ開いておく。
func heldRoot(t *testing.T) (root, dir string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "pchd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	dir = dispatcher.E2E{Root: root}.StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sc, err := presence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sc.Close)
	return root, dir
}

func runDispatcherT(t *testing.T, args ...string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if rc := runDispatcher(args, "", "", nil, pmConfig{}, &out, &errOut); rc != 0 {
		t.Fatalf("%v: rc=%d\n%s%s", args, rc, out.String(), errOut.String())
	}
}

func ticked(dir string) bool {
	s, _, _ := store.LoadDispatcherState(dir)
	return !s.Tick.IsZero()
}

// issue 459: 画面が開いている間に人が `pro-con dispatcher --stop` で止めたら、画面は dispatcher を起こし直さない
// (開いたときも keeper も)。画面が起こした dispatcher (--from-screen) も、印があれば回らずに抜ける (画面が印を見てから起こすまでの窓)。
// 手で dispatcher を起動すると印が外れ、画面はまた起こす。
func TestManualStopHoldsScreenKeeper(t *testing.T) {
	root, dir := heldRoot(t)
	runDispatcherT(t, "--stop", "--e2e", root)
	spawns := 0
	spawn := func(string) error { spawns++; return nil }
	if started, err := startDispatcherIfIdle(dir, spawn); started || err != nil || spawns != 0 {
		t.Fatalf("人が止めたのに画面の keeper が dispatcher を起こした: started=%v err=%v spawns=%d", started, err, spawns)
	}
	_, notes := wireLive(live.New(nil, t.TempDir(), dir), screenFlags{}, dir, spawn, func(context.Context) error { return nil })
	if spawns != 0 || !strings.Contains(strings.Join(notes, "\n"), "止めてある") {
		t.Fatalf("人が止めたのに開いた画面が起こした / 止めてあると知らせない: spawns=%d notes=%q", spawns, notes)
	}

	runDispatcherT(t, "--"+fromScreenFlag, "--once", "--e2e", root)
	if ticked(dir) || !store.Held(dir) {
		t.Fatalf("印があるのに画面が起こした dispatcher が回った / 印を外した: ticked=%v held=%v", ticked(dir), store.Held(dir))
	}

	runDispatcherT(t, "--once", "--e2e", root)
	if !ticked(dir) || store.Held(dir) {
		t.Fatalf("手で起動したのに回らない / 印を外さない: ticked=%v held=%v", ticked(dir), store.Held(dir))
	}
	if started, err := startDispatcherIfIdle(dir, spawn); !started || err != nil || spawns != 1 {
		t.Fatalf("印が外れたのに画面が起こさない: started=%v err=%v spawns=%d", started, err, spawns)
	}
}

// 画面の quit (最後の画面) で止めたときは印を置かない: 次に開いた画面は今までどおり起こす。
func TestScreenQuitStopDoesNotHold(t *testing.T) {
	root, dir := heldRoot(t)
	runDispatcherT(t, "--stop", "--"+fromScreenFlag, "--e2e", root)
	if store.Held(dir) {
		t.Fatal("画面の quit で止めたのに印を置いた")
	}
	spawns := 0
	if started, err := startDispatcherIfIdle(dir, func(string) error { spawns++; return nil }); !started || err != nil || spawns != 1 {
		t.Fatalf("印が無いのに画面が起こさない: started=%v err=%v spawns=%d", started, err, spawns)
	}
}
