//go:build serialrun_testseam

package main

import (
	"os"
	"time"
)

// テストの差し込み口 (main_test.go の TestMain が -tags serialrun_testseam でビルドしたときだけ入る)

func applyTestSeams(t *tunables) {
	for name, dst := range map[string]*time.Duration{
		"SERIAL_RUN_TEST_RECHECK":   &t.recheck,
		"SERIAL_RUN_TEST_FALLBACK":  &t.fallback,
		"SERIAL_RUN_TEST_REPORT":    &t.report,
		"SERIAL_RUN_TEST_INT_GRACE": &t.intGrace,
	} {
		if d, err := time.ParseDuration(os.Getenv(name)); err == nil && d > 0 {
			*dst = d
		}
	}
	t.noBlock = os.Getenv("SERIAL_RUN_TEST_NO_BLOCK") == "1"
	t.wakeLog = os.Getenv("SERIAL_RUN_TEST_WAKE_LOG")
}

// pauseBeforeRename は札を公開する直前で、fifo に書かれるまで止まる (この間に来た人が順番を抜かさないことを見る)
func pauseBeforeRename() {
	p := os.Getenv("SERIAL_RUN_TEST_PAUSE_BEFORE_RENAME")
	if p == "" {
		return
	}
	if g, err := os.Open(p); err == nil {
		_, _ = g.Read(make([]byte, 1))
		_ = g.Close()
	}
}

// psBin は ps の置き場 (失敗する ps に差し替えて、判定不能の扱いを見る)
func psBin() string {
	if p := os.Getenv("SERIAL_RUN_TEST_PS"); p != "" {
		return p
	}
	return "/bin/ps"
}

// noteSeqRetry は採番の lock の再試行に入った印を書く (テストが「再試行の中で」シグナルを送るため)
func noteSeqRetry() {
	if p := os.Getenv("SERIAL_RUN_TEST_SEQ_RETRY_LOG"); p != "" {
		if f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600); err == nil {
			_, _ = f.WriteString("r\n")
			_ = f.Close()
		}
	}
}
