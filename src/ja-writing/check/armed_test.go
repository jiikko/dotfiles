package check

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
)

// armedState は登録 1 つ (実行 ID a1) を start_run で書いた状態フォルダ。
func armedState(t *testing.T) (string, Runtime) {
	t.Helper()
	dir := fixtureRoot(t)
	if err := os.WriteFile(dir+"/src.md", []byte(vSourceText), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := DefaultRuntime()
	id := "a1"
	if _, err := StartRun(context.Background(), rt, dir+"/.yomiyasu", "src.md", StartOptions{RunID: &id, Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	return dir + "/.yomiyasu", rt
}

// change の後に締め切り (budget) を過ぎていれば、書かずに TimeoutError("armed_budget") を返し、ロックを外す (本家の armed_update)。
// 記録の driver は時計を止めるので、この枝は記録に入らない。
func TestArmedUpdateRefusesToWriteAfterBudget(t *testing.T) {
	state, rt := armedState(t)
	before, err := os.ReadFile(state + "/armed/a1.json")
	if err != nil {
		t.Fatal(err)
	}
	clock := 1000.0
	rt.Monotonic = func() float64 { return clock }
	budget := 1001.0
	_, err = ArmedUpdate(context.Background(), rt, state, "a1", func(entry *pyjson.Object) (bool, error) {
		entry.Set("notified", true)
		clock = 1001 // change の間に締め切りに届く
		return true, nil
	}, &budget)
	var e *pyops.Exception
	if !errors.As(err, &e) || e.Type != "TimeoutError" || e.Msg != "armed_budget" {
		t.Fatalf("err = %v", err)
	}
	after, err := os.ReadFile(state + "/armed/a1.json")
	if err != nil || string(after) != string(before) {
		t.Fatalf("締め切りの後に登録を書いた: %v", err)
	}
	if _, err := os.Lstat(state + "/armed/a1.lock"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("ロックが残った: %v", err)
	}
}

// change が誤りを返しても、ロックは外す (本家の with の __exit__)。
func TestArmedUpdateReleasesLockOnChangeError(t *testing.T) {
	state, rt := armedState(t)
	want := errors.New("boom")
	if _, err := ArmedUpdate(context.Background(), rt, state, "a1", func(*pyjson.Object) (bool, error) { return false, want }, nil); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Lstat(state + "/armed/a1.lock"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("ロックが残った: %v", err)
	}
}

// 使用中のロックを待っている間に止められたら (シグナル)、締め切りを待たずに errStopped を返し、ほかのプロセスのロックは消さない。
func TestArmedLockWaitStopsOnSignal(t *testing.T) {
	state, rt := armedState(t)
	if err := os.WriteFile(state+"/armed/a1.lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cmdenv.Signaled{Signal: syscall.SIGTERM})
	_, err := ArmedUpdate(ctx, rt, state, "a1", func(*pyjson.Object) (bool, error) { return true, nil }, nil)
	var stop errStopped
	if !errors.As(err, &stop) || stop.signal != syscall.SIGTERM {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Lstat(state + "/armed/a1.lock"); err != nil {
		t.Fatalf("ほかのプロセスのロックを消した: %v", err)
	}
}
