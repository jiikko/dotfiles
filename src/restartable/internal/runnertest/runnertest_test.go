package runnertest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestShellQuoteSurvivesSh(t *testing.T) {
	for _, in := range []string{"plain", "with space", "it's", `a"b$c`, "", "a'b'c"} {
		out, err := exec.Command("/bin/sh", "-c", "printf %s "+ShellQuote(in)).Output()
		if err != nil || string(out) != in {
			t.Errorf("ShellQuote(%q) を sh に通した結果 = %q (err=%v)", in, out, err)
		}
	}
}

func TestEnvDuration(t *testing.T) {
	t.Setenv("RUNNERTEST_D", "250ms")
	if got := EnvDuration("RUNNERTEST_D", time.Second); got != 250*time.Millisecond {
		t.Errorf("読める値: %v", got)
	}
	t.Setenv("RUNNERTEST_D", "not-a-duration")
	if got := EnvDuration("RUNNERTEST_D", time.Second); got != time.Second {
		t.Errorf("読めない値は fallback: %v", got)
	}
	if got := EnvDuration("RUNNERTEST_UNSET_ZZZ", 3*time.Second); got != 3*time.Second {
		t.Errorf("無い値は fallback: %v", got)
	}
}

func TestShortTempDirIsUnderSlashTmpAndRemoved(t *testing.T) {
	var dir string
	t.Run("inner", func(t *testing.T) {
		dir = ShortTempDir(t, "rt-test-")
		if !strings.HasPrefix(dir, "/tmp/rt-test-") {
			t.Errorf("socket の path が長くならないよう /tmp の直下: %q", dir)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("作られていない: %v", err)
		}
	})
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("テストの後に消える: %v", err)
	}
}

// 子が出した出力を集め、条件が成り立つまで待ち、テストの後に子を止める。
func TestPTYCollectsOutputWaitsAndStopsChild(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "echo hello; exec /bin/sleep 30")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var p *PTY
	t.Run("lifetime", func(t *testing.T) {
		p = Attach(t, cmd, devNull(t), out, "/x/sock")
		p.WaitText(t, "hello", 3*time.Second, func(s string) bool { return strings.Contains(s, "hello") })
		n := 0
		p.WaitUntil(t, "3 回目", 3*time.Second, func() bool { n++; return n >= 3 })
		if p.Path != "/x/sock" || !strings.Contains(p.Text(), "hello") {
			t.Errorf("Path=%q Text=%q", p.Path, p.Text())
		}
	})
	select {
	case <-p.Done:
	default:
		t.Fatal("テストの後に子が止まっていない")
	}
}

func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
