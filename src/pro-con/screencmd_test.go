package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pro-con/dispatcher"
	"pro-con/relay"
)

func screenCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	rc := runScreen(args, t.TempDir(), time.Now, &o, &e)
	return rc, o.String(), e.String()
}

// syncBuf は goroutine から書かれて、テストが途中で読むバッファ。
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// openRelay は e2e の置き場に中継を置いて 1 枚描き、書き出されるまで待つ。
func openRelay(t *testing.T, root string, fr relay.Frame) *relay.Writer {
	t.Helper()
	w, err := relay.Open(dispatcher.E2E{Root: root}.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	fr.At = time.Now()
	w.Put(fr)
	path := filepath.Join(dispatcher.E2E{Root: root}.StateDir(), relay.Dir, w.ID()+".frame")
	waitUntil(t, "中継の 1 枚が書かれない", func() bool {
		f, err := relay.Read(path)
		return err == nil && f.ANSI == fr.ANSI
	})
	return w
}

// 開いている画面が 1 つなら、その画面を色を落として出す (--ansi は色つき、--json は機械が読む形)。
func TestScreenShowsTheOpenScreen(t *testing.T) {
	root := t.TempDir()
	openRelay(t, root, relay.Frame{ANSI: "\x1b[38;5;51mproducer-consumer\x1b[0m ゲージ", Width: 100, Height: 30, State: map[string]string{"mode": "board"}})
	rc, out, errOut := screenCmd(t, "--e2e", root)
	if rc != 0 || !strings.Contains(out, "producer-consumer ゲージ") || strings.Contains(out, "\x1b[") || !strings.Contains(out, "普通の画面") {
		t.Fatalf("screen: rc=%d out=%q err=%q", rc, out, errOut)
	}
	if _, out, _ := screenCmd(t, "--e2e", root, "--ansi"); !strings.Contains(out, "\x1b[38;5;51m") {
		t.Fatalf("--ansi で色が出ない: %q", out)
	}
	rc, out, _ = screenCmd(t, "--e2e", root, "--json")
	var f relay.Frame
	if rc != 0 || json.Unmarshal([]byte(out), &f) != nil || f.Plain != "producer-consumer ゲージ" || f.ANSI != "" || f.State["mode"] != "board" {
		t.Fatalf("--json: rc=%d %q", rc, out)
	}
}

// 複数の画面が開いていれば一覧を出し、--screen で選ぶ。閉じた画面の残りは --all のときだけ並べる。画面が無ければ rc=1。
func TestScreenListsAndPicks(t *testing.T) {
	root := t.TempDir()
	if rc, _, errOut := screenCmd(t, "--e2e", root); rc != 1 || !strings.Contains(errOut, "開いている画面が無い") {
		t.Fatalf("画面が無い: rc=%d err=%q", rc, errOut)
	}
	a := openRelay(t, root, relay.Frame{ANSI: "画面 A"})
	b := openRelay(t, root, relay.Frame{ANSI: "画面 B", View: true})
	rc, out, _ := screenCmd(t, "--e2e", root)
	if rc != 0 || !strings.Contains(out, "画面が 2 個ある") || !strings.Contains(out, a.ID()) || !strings.Contains(out, b.ID()) || !strings.Contains(out, "見ているだけ") {
		t.Fatalf("一覧: rc=%d %q", rc, out)
	}
	if _, out, _ := screenCmd(t, "--e2e", root, "--screen", b.ID()); !strings.Contains(out, "画面 B") || strings.Contains(out, "画面 A") {
		t.Fatalf("--screen で選べない: %q", out)
	}
	// 落ちた画面の残り (flock の外れた印) を作る
	d := filepath.Join(dispatcher.E2E{Root: root}.StateDir(), relay.Dir)
	if err := os.WriteFile(filepath.Join(d, "dead.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := screenCmd(t, "--e2e", root); strings.Contains(out, "dead") {
		t.Fatalf("閉じた画面の残りを並べた: %q", out)
	}
	if _, out, _ := screenCmd(t, "--e2e", root, "--all"); !strings.Contains(out, "dead") || !strings.Contains(out, "閉じた画面の残り") {
		t.Fatalf("--all で閉じた画面の残りが出ない: %q", out)
	}
	if rc, _, _ := screenCmd(t, "--e2e", root, "--screen", "nosuch"); rc != 1 {
		t.Fatalf("無い画面: rc=%d", rc)
	}
}

// --follow は描き直されるたびに出し、画面が閉じたら終わる。
func TestScreenFollow(t *testing.T) {
	old := screenPoll
	screenPoll = 10 * time.Millisecond
	t.Cleanup(func() { screenPoll = old })
	root := t.TempDir()
	w := openRelay(t, root, relay.Frame{ANSI: "1 枚目"})
	var o, e syncBuf
	done := make(chan int, 1)
	go func() {
		done <- runScreen([]string{"--e2e", root, "--follow", "--timeout", "30s"}, t.TempDir(), time.Now, &o, &e)
	}()
	waitUntil(t, "--follow が 1 枚目を出さない", func() bool { return strings.Contains(o.String(), "1 枚目") })
	w.Put(relay.Frame{ANSI: "2 枚目", At: time.Now().Add(time.Second)})
	// 描き直した 1 枚を follow が出してから閉じる (出す前に閉じると、読み直す前にファイルが消える)
	waitUntil(t, "--follow が描き直した 2 枚目を出さない", func() bool { return strings.Contains(o.String(), "2 枚目") })
	w.Close()
	select {
	case rc := <-done:
		if rc != 0 || !strings.Contains(o.String(), "2 枚目") || !strings.Contains(e.String(), "閉じた") {
			t.Fatalf("--follow: rc=%d out=%q err=%q", rc, o.String(), e.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("画面が閉じても --follow が終わらない")
	}
}

// 🚨 pro-con screen は読むだけ (441 の守ること 1 / 445): 状態の置き場を 1 バイトも変えない (中継のファイルの中身・mtime・数も)。
func TestScreenDoesNotWrite(t *testing.T) {
	old := screenPoll
	screenPoll = 10 * time.Millisecond
	t.Cleanup(func() { screenPoll = old })
	root := t.TempDir()
	a := openRelay(t, root, relay.Frame{ANSI: "画面 A"})
	openRelay(t, root, relay.Frame{ANSI: "画面 B"})
	d := filepath.Join(dispatcher.E2E{Root: root}.StateDir(), relay.Dir)
	if err := os.WriteFile(filepath.Join(d, "dead.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	for _, args := range [][]string{
		{}, {"--all"}, {"--json"}, {"--all", "--json"}, {"--screen", a.ID()}, {"--screen", a.ID(), "--ansi", "--json"},
		{"--screen", "dead"}, {"--screen", a.ID(), "--follow", "--timeout", "100ms"},
	} {
		screenCmd(t, append([]string{"--e2e", root}, args...)...)
	}
	after := snapshotTree(t, root)
	if len(before) != len(after) {
		t.Fatalf("置き場のファイルが増減した: 前 %d / 後 %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("pro-con screen が %s を変えた:\n前 %s\n後 %s", p, v, after[p])
		}
	}
}

// --follow は 1 枚目がまだでも待つ (画面を開いた直後に始めても、すぐ諦めない)。上限で終わったらそう言う。
func TestScreenFollowWaitsForFirstFrame(t *testing.T) {
	old := screenPoll
	screenPoll = 10 * time.Millisecond
	t.Cleanup(func() { screenPoll = old })
	root := t.TempDir()
	w, err := relay.Open(dispatcher.E2E{Root: root}.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var o, e syncBuf
	done := make(chan int, 1)
	go func() {
		done <- runScreen([]string{"--e2e", root, "--follow", "--timeout", "2s"}, t.TempDir(), time.Now, &o, &e)
	}()
	w.Put(relay.Frame{ANSI: "遅れて描いた 1 枚", At: time.Now()})
	select {
	case rc := <-done:
		if rc != 0 || !strings.Contains(o.String(), "遅れて描いた 1 枚") || !strings.Contains(e.String(), "上限") {
			t.Fatalf("--follow (1 枚目を待つ): rc=%d out=%q err=%q", rc, o.String(), e.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("--follow が上限で終わらない")
	}
}
