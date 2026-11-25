package check

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyjson"
)

// 締め切りの後に処理がロックを放すのを待っても、ホストの上限 (--print-config が書く値) の内に終わる。
func TestHookUnwindStaysWithinHostTimeout(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		for _, event := range hookEvents {
			if total := hookBudgets[event] + hookUnwindLimit(host, event) + hookFailLockLimit(host, event); total >= hookHostTimeout(host, event) {
				t.Errorf("%s %s: 締め切り %.2f + 待ち %.2f + fail のロックの待ち %.2f = %.2f 秒がホストの上限 %.0f 秒を超える", host, event,
					hookBudgets[event], hookUnwindLimit(host, event), hookFailLockLimit(host, event), total, hookHostTimeout(host, event))
			}
		}
	}
}

// 締め切りがロックを持った処理の途中に来ても、ロックを残さずに終わり (本家は SIGALRM の例外で with _ArmedLock を抜けて消す)、
// 処理していた登録の log に timeout を、hook-errors.log に締め切りの行を残す。出力は無く rc 0。
// 締め切りは実時間のタイマーで、race つきで回す (記録の突き合わせは race なしでしか回らず、goroutine とタイマーの枝を通らない)。
func TestHookDeadlineInsideLockReleasesLockAndLogsTimeout(t *testing.T) {
	state, runID, left := runDeadlineInsideLock(t, hookUnwindLimit)
	<-left
	locks, err := filepath.Glob(filepath.Join(state, "armed", "*.lock"))
	if err != nil || len(locks) != 0 {
		t.Fatalf("ロックが残った: %v %v", locks, err)
	}
	logged, err := os.ReadFile(filepath.Join(state, "hook-errors.log"))
	if err != nil || !strings.HasSuffix(string(logged), " PreToolUse TimeoutError yomiyasu_hook.py:897\n") {
		t.Fatalf("hook-errors.log = %q (%v)", logged, err)
	}
	record, err := ReadRecord(filepath.Join(state, "armed", runID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	items := record.Field("log").([]pyjson.Value)
	if last := items[len(items)-1].(*pyjson.Object); last.Field("event") != "PreToolUse" || last.Field("result") != "timeout" {
		t.Fatalf("登録の log の最後 = %v", last)
	}
}

// 締め切りの後に待つ秒数は hookUnwind から取る (ホストの上限から決めた値で待つ配線)。待ちを短くすると、処理がロックを放す前に返る。
func TestHookDeadlineWaitsOnlyTheUnwindLimit(t *testing.T) {
	state, _, left := runDeadlineInsideLock(t, func(string, string) float64 { return 0.05 })
	locks, err := filepath.Glob(filepath.Join(state, "armed", "*.lock"))
	<-left
	if err != nil || len(locks) != 1 {
		t.Fatalf("待ちを短くしても、処理がロックを放すまで待った (返った時点のロック %v %v)", locks, err)
	}
}

// runDeadlineInsideLock は、ロックを持った投稿の照合の途中で PreToolUse の締め切りを迎える hook を起こし、返るまで待つ。
// 処理は 1 件目 (関係しない文書) の頭で止まり、取り消しを受けてから 0.5 秒かけて抜ける (後片付けの遅い処理)。
// 取り消しを見ずに 2 件目へ進む退行は、2 件目の頭で入れない送り先に詰まり、ロックを持ったまま残る。
// left は差し込み口が抜けたときに閉じる (その後に処理がロックを放す)。
func runDeadlineInsideLock(t *testing.T, unwind func(host, event string) float64) (state, runID string, left chan struct{}) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"src.md": "今日は良い天気です。\n", "other.md": "関係しない文書です。\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state = filepath.Join(dir, ".yomiyasu")
	rt := DefaultRuntime()
	session, host := "s1", "claude-code"
	entry, err := StartRun(context.Background(), rt, state, "src.md", StartOptions{Session: &session, Host: &host, Cwd: dir, DefaultPlace: true})
	if err != nil {
		t.Fatal(err)
	}
	runID = entry.Field("run_id").(string)

	entered := make(chan struct{}, 1)
	left = make(chan struct{})
	hookInLock = func(ctx context.Context) {
		entered <- struct{}{}
		<-ctx.Done()
		time.Sleep(500 * time.Millisecond) // sleep-ok: other: 取り消しを受けてからロックを放すまでの遅さ (待ちではない)
		close(left)
	}
	saved := hookBudgets["PreToolUse"]
	hookBudgets["PreToolUse"] = 0.2
	hookUnwind = unwind
	t.Cleanup(func() {
		hookInLock = func(context.Context) {}
		hookBudgets["PreToolUse"] = saved
		hookUnwind = hookUnwindLimit
	})

	data, err := json.Marshal(map[string]any{"session_id": session, "hook_event_name": "PreToolUse", "cwd": dir, "tool_name": "Bash",
		"tool_input": map[string]any{"command": "gh pr create --body-file other.md; gh pr edit 1 --body-file src.md"}})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	env := cmdenv.Env{Args: []string{"--host", "claude-code"}, Stdin: bytes.NewReader(data), Stdout: &stdout, Stderr: &stderr, Cwd: dir,
		Environ: []string{"HOME=" + dir}}
	rc := HookMainWith(context.Background(), env, rt)
	select {
	case <-entered:
	default:
		t.Fatal("ロックを持った処理に入らなかった (締め切りがロックの外で来た)")
	}
	if rc != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("rc=%d stdout=%q stderr=%q", rc, stdout.String(), stderr.String())
	}
	return state, runID, left
}
