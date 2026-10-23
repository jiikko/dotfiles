package childrun

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestParseSeconds(t *testing.T) {
	for in, want := range map[string]time.Duration{"0": 0, "1": time.Second, "0.5": 500 * time.Millisecond, "60": time.Minute, "1e-10": time.Nanosecond} {
		got, err := ParseSeconds(in)
		if err != nil || got != want {
			t.Errorf("%q: got %v,%v want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-1", "NaN", "Inf", "1e30", "x"} {
		if _, err := ParseSeconds(in); err == nil {
			t.Errorf("%q を受け付けた", in)
		}
	}
}

func TestStartErrorRC(t *testing.T) {
	if rc := StartErrorRC(fmt.Errorf("x: %w", exec.ErrNotFound)); rc != RCNotFound {
		t.Fatalf("見つからない: rc=%d", rc)
	}
	if rc := StartErrorRC(errors.New("permission denied")); rc != RCCannotExec {
		t.Fatalf("実行できない: rc=%d", rc)
	}
}

func TestWaitStatus(t *testing.T) {
	if rc := WaitStatus(nil); rc != 0 {
		t.Fatalf("成功: rc=%d", rc)
	}
	if rc := WaitStatus(exec.Command("/bin/sh", "-c", "exit 3").Run()); rc != 3 {
		t.Fatalf("exit 3: rc=%d", rc)
	}
	if rc := WaitStatus(exec.Command("/bin/sh", "-c", "kill -TERM $$").Run()); rc != 128+int(syscall.SIGTERM) {
		t.Fatalf("TERM で死んだ子: rc=%d", rc)
	}
}

// 死に直さない場合 (シグナルを受けていない・子が普通に終わった・QUIT) は rc をそのまま返す。死に直す場合は呼び出し元のテストが見る
func TestExitAsReturnsRCWithoutReraise(t *testing.T) {
	for _, c := range []struct {
		rc  int
		got syscall.Signal
	}{{0, 0}, {131, 0}, {3, syscall.SIGTERM}, {128 + int(syscall.SIGQUIT), syscall.SIGQUIT}} {
		if rc := ExitAs(c.rc, c.got); rc != c.rc {
			t.Errorf("ExitAs(%d, %v) = %d", c.rc, c.got, rc)
		}
	}
}
