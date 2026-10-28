// Package runnertest は restartable のテストが runner を実プロセスで起こして control socket 越しに確かめるときの共有 helper。
// runner / ui / control / main の各テストが同じ写しを持っていたものを置く (テストからだけ import する)。
package runnertest

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ShortTempDir は /tmp の直下に作る使い捨ての dir (テストの後に消す)。$TMPDIR は macOS で長く、
// unix socket の path の上限 (104 byte) を超えうるので、socket を置く dir はここに作る。
func ShortTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// EnvDuration は環境変数 name を時間として読む。無い・読めないなら fallback (遅い環境で待ちの上限を伸ばす用)。
func EnvDuration(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

// ShellQuote は sh で 1 語になる単引用符の形にする。
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// PTY は端末 (pty) の下で動かしている runner の子プロセス。出力を溜め、テストの後に止める。
type PTY struct {
	Cmd    *exec.Cmd
	Master *os.File
	Path   string        // control socket の path
	Done   chan struct{} // 子が終わったら閉じる
	Err    error         // Done が閉じた後の cmd.Wait の結果

	mu  sync.Mutex
	out bytes.Buffer
}

// Attach は起こした cmd を見張る。子の終了を Done / Err に、output から読んだ内容を Text に集め、
// テストの後に子を止める (まず SIGTERM、4 秒で終わらなければ Kill) と master を閉じる。
// output は pty の master か、子の標準出力に繋いだ pipe の読み側 (pipe を閉じるのは呼び出し側)。
func Attach(t *testing.T, cmd *exec.Cmd, master *os.File, output io.Reader, path string) *PTY {
	t.Helper()
	p := &PTY{Cmd: cmd, Master: master, Path: path, Done: make(chan struct{})}
	go func() {
		defer close(p.Done)
		p.Err = cmd.Wait()
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := output.Read(buf)
			if n > 0 {
				p.mu.Lock()
				_, _ = p.out.Write(buf[:n])
				p.mu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		select {
		case <-p.Done:
		default:
			_ = p.Cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-p.Done:
			case <-time.After(4 * time.Second):
				_ = p.Cmd.Process.Kill()
				<-p.Done
			}
		}
		_ = p.Master.Close()
	})
	return p
}

// Text はここまでに出た出力。
func (p *PTY) Text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.String()
}

// WaitUntil は cond が真になるのを 5ms 刻みで待つ。timeout で諦めるか、子が先に終わったらテストを止める (出力つき)。
func (p *PTY) WaitUntil(t *testing.T, description string, timeout time.Duration, cond func() bool) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		if cond() {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("timed out waiting for %s; PTY output: %q", description, p.Text())
		case <-p.Done:
			t.Fatalf("runner exited before %s: %v; PTY output: %q", description, p.Err, p.Text())
		}
	}
}

// WaitText は出力が predicate を満たすのを待つ (WaitUntil と同じ止め方)。
func (p *PTY) WaitText(t *testing.T, description string, timeout time.Duration, predicate func(string) bool) {
	t.Helper()
	p.WaitUntil(t, description, timeout, func() bool { return predicate(p.Text()) })
}
