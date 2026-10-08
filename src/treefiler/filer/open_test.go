package filer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOpen は openCommand を差し替え、開こうとしたパスを記録する。
func fakeOpen(t *testing.T, err error) func() []string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	orig := openCommand
	openCommand = func(_ context.Context, path string) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, path)
		return err
	}
	t.Cleanup(func() { openCommand = orig })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

func settleOpen(t *testing.T, m *Model) []Notice {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for m.opener.busy() {
		m.Advance(fixedNow)
		if time.Now().After(deadline) {
			t.Fatal("open が終わらない")
		}
		time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 裏の goroutine の完了を条件で待つループの刻み
	}
	return m.TakeNotices()
}

func TestOpenKeyOpensCursor(t *testing.T) {
	got := fakeOpen(t, nil)
	m := newTest(t)
	cdTo(t, m, "c.txt")
	m.HandleKey("o")
	if ns := settleOpen(t, m); len(ns) != 0 {
		t.Fatalf("成功なのに知らせ: %+v", ns)
	}
	if g := got(); len(g) != 1 || g[0] != filepath.Join(m.root.abs, "c.txt") {
		t.Fatalf("開いたパス = %v", g)
	}
}

// open が走っている間は Busy (呼び出し側が失敗の toast を出す前に tick を止めないように)。
func TestOpenKeepsBusyWhileRunning(t *testing.T) {
	release := make(chan struct{})
	orig := openCommand
	openCommand = func(context.Context, string) error { <-release; return errors.New("boom") }
	t.Cleanup(func() { openCommand = orig })
	m := newTest(t)
	settleBackground(t, m)
	if m.Busy() {
		t.Fatal("前提: 裏の処理が落ち着いていない")
	}
	m.HandleKey("o")
	if !m.Busy() {
		t.Fatal("open の起動中に Busy が false")
	}
	close(release)
	if ns := settleOpen(t, m); len(ns) != 1 {
		t.Fatalf("知らせ = %+v", ns)
	}
	if m.Busy() {
		t.Fatal("知らせを出した後も Busy")
	}
}

func TestOpenFailureBecomesToast(t *testing.T) {
	fakeOpen(t, errors.New("boom"))
	m := newTest(t)
	cdTo(t, m, "c.txt")
	m.HandleKey("o")
	ns := settleOpen(t, m)
	if len(ns) != 1 || ns[0].OK || !strings.Contains(ns[0].Text, "c.txt") {
		t.Fatalf("失敗の知らせ = %+v", ns)
	}
}

func TestOpenKeyInTileOpensTileFile(t *testing.T) {
	got := fakeOpen(t, nil)
	m := newTest(t)
	cdTo(t, m, "c.txt")
	m.HandleKey("enter")
	if m.frontTile() == nil {
		t.Fatal("タイルが開かない")
	}
	m.HandleKey("o")
	settleOpen(t, m)
	if g := got(); len(g) != 1 || !strings.HasSuffix(g[0], "c.txt") {
		t.Fatalf("開いたパス = %v", g)
	}
	if m.frontTile() == nil || m.frontTile().closing {
		t.Fatal("o でタイルが閉じた")
	}
}

// r は読み直す: 外で足したファイルが見え、開いていたフォルダは開いたまま。
func TestReloadKey(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	if err := os.WriteFile(filepath.Join(m.root.abs, "a", "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.HandleKey("r")
	a := m.findNode(filepath.Join(m.root.abs, "a"))
	if a == nil || !a.expanded {
		t.Fatal("r で a が閉じた")
	}
	if m.findNode(filepath.Join(m.root.abs, "a", "new.txt")) == nil {
		t.Fatal("r で足したファイルが見えない")
	}
}
