package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"pro-con/dispatcher"
)

// 見張りの行は、monitor.lock の pid が生きていてコマンド行が monitor のときだけ「動いている」(pid の使い回しで別のプロセスを見張りと出さない)。
func TestPSShowsMonitor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dispatcher.MonitorLockFile), []byte("77\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	row := func(procs map[int]string) Proc {
		t.Helper()
		rows, _ := collectProcs(dir, time.Now(), procs)
		for _, p := range rows {
			if p.Role == "見張り" {
				return p
			}
		}
		t.Fatalf("見張りの行が無い: %+v", rows)
		return Proc{}
	}
	if p := row(map[int]string{77: "/bin/pro-con monitor --until-stdin-closes"}); p.PID != 77 || p.State != "動いている" {
		t.Fatalf("動いている見張りを出さない: %+v", p)
	}
	if p := row(map[int]string{77: "/usr/bin/vim notes"}); p.PID != 0 || p.State != "止まっている" {
		t.Fatalf("pid を使い回した別のプロセスを見張りと出した: %+v", p)
	}
}
