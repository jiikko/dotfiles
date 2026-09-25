package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pro-con/dispatcher"
	"pro-con/eventlog"
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
	go func() { rc <- runDispatcher([]string{"--e2e", root}, "", "", nil, pmConfig{}, &out, &errOut) }()
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
	// 適用した出来事は events.jsonl に残る (pro-con log が読む。issue 444)
	waitFor("適用した出来事を events.jsonl に書かない", func() bool {
		evs, _ := eventlog.Read(dir)
		for _, e := range evs {
			if e.Kind == eventlog.KindApply && e.Card == "C-001" && !e.At.IsZero() {
				return true
			}
		}
		return false
	})
	// 画面の出来事は、画面が置いた時刻のまま events.jsonl に入る (dispatcher が居ない間に置いたものを後で書いても、時刻がずれない。issue 445)
	opened := time.Now().Add(-time.Hour).Truncate(time.Second)
	if _, err := store.Submit(dir, store.Request{Kind: store.KindEvent, Note: "画面 (pid 1): 開いた", At: opened}); err != nil {
		t.Fatal(err)
	}
	waitFor("画面の出来事を置いた時刻のまま書かない", func() bool {
		evs, _ := eventlog.Read(dir)
		for _, e := range evs {
			if e.Kind == eventlog.KindScreen && e.At.Equal(opened) {
				return true
			}
		}
		return false
	})
	if st, err := os.Stat(filepath.Join(dir, eventlog.File)); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("events.jsonl の権限: %v %v", st, err)
	}
	sctx, scancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer scancel()
	if running, err := dispatcher.RequestStop(sctx, dir, 30*time.Second); !running || err != nil {
		t.Fatalf("止める頼みが届かない: running=%v %v", running, err)
	}
	if r := <-rc; r != 0 {
		t.Fatalf("rc=%d\n%s", r, errOut.String())
	}
}

// dispatcher の --pm が設定の pm に勝つ。書き間違いは誤りにする (on と読むと止めたつもりの PM が起動する)。
func TestResolvePM(t *testing.T) {
	for _, tc := range []struct {
		flag, cfg string
		off       bool
		why       string
	}{
		{"", "", false, ""},
		{"", "on", false, ""},
		{"", "off", true, `設定 pm = "off"`},
		{"off", "", true, "--pm=off"},
		{"off", "on", true, "--pm=off"},
		{"on", "off", false, ""},
	} {
		off, why, err := resolvePM(tc.flag, tc.cfg)
		if err != nil || off != tc.off || why != tc.why {
			t.Errorf("--pm=%q 設定 %q → off=%v why=%q err=%v (想定 off=%v why=%q)", tc.flag, tc.cfg, off, why, err, tc.off, tc.why)
		}
	}
	if _, _, err := resolvePM("of", ""); err == nil {
		t.Error("--pm の書き間違いを誤りにしていない")
	}
}

// PM を起こさない設定で起動した dispatcher は、そのことを出来事 (events.jsonl) と dispatcher のログに 1 行ずつ出す。on なら出さない。
func TestRunDispatcherAnnouncesPMOff(t *testing.T) {
	for _, tc := range []struct {
		args []string
		cfg  string
		want string
	}{
		{[]string{"--pm=off"}, "", "PM を起こさない (--pm=off)"},
		{nil, "off", `PM を起こさない (設定 pm = "off")`},
		{[]string{"--pm=on"}, "off", ""},
	} {
		root, err := os.MkdirTemp("/tmp", "pcpm")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
		var out, errOut bytes.Buffer
		if rc := runDispatcher(append([]string{"--e2e", root, "--once"}, tc.args...), "", "", nil, pmConfig{Mode: tc.cfg}, &out, &errOut); rc != 0 {
			t.Fatalf("%v: rc=%d stderr=%s", tc.args, rc, errOut.String())
		}
		evs, err := eventlog.Read(dispatcher.E2E{Root: root}.StateDir())
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range evs {
			if strings.Contains(e.Reason, "PM を起こさない") {
				n++
				if !strings.Contains(e.Reason, tc.want) || tc.want == "" {
					t.Errorf("%v 設定 %q: 出来事の文が違う: %q", tc.args, tc.cfg, e.Reason)
				}
			}
		}
		logged := strings.Count(out.String(), "PM を起こさない")
		if tc.want != "" && (n != 1 || logged != 1) || tc.want == "" && (n != 0 || logged != 0) {
			t.Errorf("%v 設定 %q: 出来事 %d 件・ログ %d 行 (想定 %v)\n%s", tc.args, tc.cfg, n, logged, tc.want != "", out.String())
		}
	}
}
