package filer

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// `!` のキー待ちで矢印キー (ESC [ D) を押しても、残りのバイトが戻った先へ漏れない。
// 包みのシェルが waitIfQuick の後に端末の入力に残ったものを読んで出す。🚨 端末でしか成り立たない (パイプでは stty が効かない) ので pty で回す
func TestKeyWaitSwallowsWholeEscapeSequence(t *testing.T) {
	probe := `/bin/sh -c "$1" treefiler /bin/sh "/bin/echo ran"; stty -icanon -echo min 0 time 3 2>/dev/null; printf 'LEFT<'; dd bs=16 count=1 2>/dev/null | od -An -c | tr -d ' \n'; printf '>END'`
	cmd := exec.Command("/bin/sh", "-c", probe, "probe", waitIfQuick)
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close() }()
	var out lockedBuffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&out, master); close(done) }() // pty の出力は EIO で終わる
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "何かキーを押すと") {
		if time.Now().After(deadline) {
			t.Fatalf("キー待ちの案内が出ない: %q", out.String())
		}
		time.Sleep(5 * time.Millisecond) // sleep-ok: tick: pty の出力に案内が出るのを条件で待つループの刻み
	}
	if _, err := master.WriteString("\x1b[D"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("包みのシェルが失敗: %v (%q)", err, out.String())
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("pty の出力が終わらない: %q", out.String())
	}
	got := out.String()
	i := strings.Index(got, "LEFT<")
	if i < 0 || !strings.Contains(got[i:], ">END") {
		t.Fatalf("残りの読み取りの印が無い: %q", got)
	}
	if left := got[i+len("LEFT<") : strings.Index(got[i:], ">END")+i]; left != "" {
		t.Fatalf("キー待ちの後に入力が残った (戻った先がキーとして受ける): %q", left)
	}
}

// lockedBuffer は読み書きを排他した bytes.Buffer (pty を写す goroutine と待ちのループが同時に触る)。
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }
