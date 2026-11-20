//go:build !race

// 本物のプロセスを並べる検査 (run の子 (lint・diff) は race で作ったテストのバイナリになり、race の run では時間が 30 倍近くになるため race から外し、
// Makefile の test が race なしで別に回す)。

package check

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"ja-writing/internal/pyops"
)

// processResult は 1 つのプロセスの終了コードと出力。
type processResult struct {
	rc             int
	stdout, stderr string
}

// startProcesses は check を argv で n 個のプロセスとして同時に起こし、全部が終わるのを待つ (Wait はプロセスの終了を待つ。時間は測らない)。
// テストのバイナリを本番と同じ入口 (entry.Run) で走らせる。各プロセスは同じ作業フォルダで動く。
func startProcesses(t *testing.T, dir string, n int, argv ...string) []processResult {
	t.Helper()
	exe := testExecutable(t)
	environ := append(isolatedEnviron(t), childEnvName+"="+childReal)
	results := make([]processResult, n)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := range n {
		cmd := exec.Command(exe, append([]string{"check"}, argv...)...)
		cmd.Dir, cmd.Env = dir, environ
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			err := cmd.Run()
			rc := 0
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				rc = exit.ExitCode()
			} else if err != nil {
				rc = -1
			}
			results[i] = processResult{rc, stdout.String(), stderr.String()}
		}()
	}
	close(gate)
	wg.Wait()
	return results
}

func manageFixture(t *testing.T) string {
	t.Helper()
	dir := fixtureRoot(t)
	if err := os.Mkdir(dir+"/.git", 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"src.md": vSourceText, "final.md": vFinalText} {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// 同じ原文の start を別々のプロセスで同時に起こしても、原文のロックで直列になり、どれも登録を書き (rc 0)、最後の 1 つだけが active で、
// ほかは superseded で終わる (同時に 2 つの登録が active にならない)。ロックのファイルは残らない。
func TestConcurrentStartLeavesOneActiveRegistration(t *testing.T) {
	dir := manageFixture(t)
	const n = 6
	for i, r := range startProcesses(t, dir, n, "start", "--source", "src.md") {
		if r.rc != 0 {
			t.Fatalf("%d 番目の start: rc=%d stdout=%q stderr=%q", i, r.rc, r.stdout, r.stderr)
		}
	}
	entries, err := ArmedEntries(dir + "/.yomiyasu")
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]int{}
	for _, entry := range entries {
		status := pyops.Str(entry.Field("status"))
		if status == "ended" {
			status += ":" + pyops.Str(entry.Field("status_reason"))
		}
		statuses[status]++
	}
	if len(entries) != n || statuses["active"] != 1 || statuses["ended:superseded"] != n-1 {
		t.Fatalf("登録 %d 件の状態: %v", len(entries), statuses)
	}
	if locks, _ := filepath.Glob(dir + "/.yomiyasu/armed/*.lock"); len(locks) != 0 {
		t.Fatalf("ロックが残った: %v", locks)
	}
	hidden, _ := filepath.Glob(dir + "/.yomiyasu/armed/.source-*.lock")
	if len(hidden) != 0 {
		t.Fatalf("原文のロックが残った: %v", hidden)
	}
}

// run を 1 回までに決めた登録で、run を別々のプロセスで同時に起こすと、上限の判定と回の確保が登録のロックの中で直列になり、
// 検査を実行するのは 1 つだけで (rc 0)、ほかは上限で実行しない (rc 1。unverified)。記録は 1 回分だけ残り、空の予約は残らない。
func TestConcurrentManagedRunsRunOnce(t *testing.T) {
	dir := manageFixture(t)
	if r := startProcesses(t, dir, 1, "start", "--source", "src.md", "--run-id", "c1", "--max-runs", "1"); r[0].rc != 0 {
		t.Fatalf("start: %+v", r[0])
	}
	const n = 4
	codes := map[int]int{}
	for _, r := range startProcesses(t, dir, n, "run", "--source", "src.md", "--final", "final.md", "--json") {
		codes[r.rc]++
		if r.rc == 1 && !strings.Contains(r.stdout, `"status": "unverified"`) {
			t.Errorf("上限で止めた run の出力が unverified でない: %q", r.stdout)
		}
		if r.rc != 0 && r.rc != 1 {
			t.Errorf("rc=%d stdout=%q stderr=%q", r.rc, r.stdout, r.stderr)
		}
	}
	if codes[0] != 1 || codes[1] != n-1 {
		t.Fatalf("終了コードの数: %v (実行は 1 つ、ほかは上限のはず)", codes)
	}
	attempts, _ := filepath.Glob(dir + "/.yomiyasu/runs/c1/attempt-[0-9]*.json")
	attempts = slices.DeleteFunc(attempts, func(p string) bool { return !attemptFilePattern.MustMatchAt0(filepath.Base(p)) })
	if len(attempts) != 1 {
		t.Fatalf("回の記録: %v", attempts)
	}
	record, err := ReadRecord(attempts[0])
	if err != nil || record == nil {
		t.Fatalf("%s を読めない: %v", attempts[0], err)
	}
	if by := pyops.Index(pyops.Index(record, "host"), "armed_by"); !pyops.Equal(by, "armed/c1.json") {
		t.Fatalf("記録の host.armed_by = %v", by)
	}
	entries, err := ArmedEntries(dir + "/.yomiyasu")
	if err != nil || len(entries) != 1 {
		t.Fatalf("登録: %v %v", entries, err)
	}
	if got := fmt.Sprint(entries[0].Field("status"), " ", entries[0].Field("status_reason")); got != "unverified run_limit" {
		t.Fatalf("登録の状態 %s", got)
	}
	var unverified int
	for _, item := range pyops.Iter(entries[0].Field("log")) {
		if pyops.Equal(pyops.Index(item, "result"), "unverified:run_limit") {
			unverified++
		}
	}
	if unverified != n-1 {
		t.Fatalf("log の unverified:run_limit が %d 件 (%v)", unverified, entries[0].Field("log"))
	}
}
