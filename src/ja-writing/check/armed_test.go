package check

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/jiikko/dotfiles/src/testwait"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyfs"
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
// 止めるのは待ちに入った後 (ロックを 1 度取れずに待ち直す所まで進んだのを、時計を読んだ回数で確かめてから)。
func TestArmedLockWaitStopsOnSignal(t *testing.T) {
	state, rt := armedState(t)
	if err := os.WriteFile(state+"/armed/a1.lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int64
	clock := rt.Monotonic
	rt.Monotonic = func() float64 {
		reads.Add(1)
		return clock()
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ArmedUpdate(ctx, rt, state, "a1", func(*pyjson.Object) (bool, error) { return true, nil }, nil)
		done <- err
	}()
	// 締め切りを決める 1 回・1 周目の確かめ・取れなかった後の待ちの長さで 3 回 (3 回目はロックを開けなかった後にしか読まない)
	testwait.Until(t, "ロックの待ちに入る", func() bool { return reads.Load() >= 3 })
	cancel(cmdenv.Signaled{Signal: syscall.SIGTERM})
	err := testwait.Recv(t, "ArmedUpdate が戻る", done)
	var stop errStopped
	if !errors.As(err, &stop) || stop.signal != syscall.SIGTERM {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Lstat(state + "/armed/a1.lock"); err != nil {
		t.Fatalf("ほかのプロセスのロックを消した: %v", err)
	}
}

// endedByOther は、ほかのプロセスが a1 の登録を終え (理由 other)、持っていたロックを外した形にする。書いた登録のバイト列を返す。
func endedByOther(t *testing.T, state string) []byte {
	t.Helper()
	data, err := os.ReadFile(state + "/armed/a1.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := pyjson.LoadsBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	entry := value.(*pyjson.Object)
	entry.Set("status", "ended")
	entry.Set("status_reason", "other")
	item := pyjson.NewObject()
	item.Set("at", "x")
	item.Set("event", "other")
	item.Set("result", "ended:other")
	item.Set("ms", int64(0))
	entry.Set("log", append(pyops.Index(entry, "log").([]pyjson.Value), item))
	text, err := pyjson.Dumps(entry, pyjson.Indent1())
	if err != nil {
		t.Fatal(err)
	}
	written := []byte(text + "\n")
	if err := os.WriteFile(state+"/armed/a1.json", written, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(state + "/armed/a1.lock"); err != nil {
		t.Fatal(err)
	}
	return written
}

// end と start は登録の一覧を読んでから、1 つずつロックを取って読み直す。一覧を読んだ後、ロックを待つ間にほかのプロセスが登録を終えていたら、
// 読み直した登録はもう ended なので書かない (本家の finish・end_old が False を返す。end の数には入る)。
// 本家の 3.1.0 で同じ手順 (外からロックを持ち、待たせている間に ended に書き換える) を測った結果と同じ: 登録は他のプロセスが書いたまま
// (理由 other・log の最後は ended:other)、end は「1件」で rc 0、start は新しい登録を書いて rc 0。
// 書き換えるのは、check がロックの締め切りを決める時計を読んだ時 (一覧を読んだ後。end はそれより前に時計を読まず、start は新しい実行 ID の
// ロックを持ってから a1 のロックの締め切りを読む)。
func TestManageLeavesRegistrationEndedWhileWaitingForLock(t *testing.T) {
	for name, args := range map[string][]string{
		"end":   {"end", "--run-id", "a1"},
		"start": {"start", "--source", "src.md", "--run-id", "a2"},
	} {
		t.Run(name, func(t *testing.T) {
			state, rt := armedState(t)
			dir := strings.TrimSuffix(state, "/.yomiyasu")
			if err := os.WriteFile(state+"/armed/a1.lock", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			var written []byte
			clock := rt.Monotonic
			inside := state + "/armed/a1.lock" // end は一覧を読んだ後の最初の時計で書き換える
			if name == "start" {
				inside = state + "/armed/a2.lock"
			}
			rt.Monotonic = func() float64 {
				if written == nil && exists(t, inside) && (name == "end" || exists(t, state+"/armed/a1.lock")) {
					written = endedByOther(t, state)
				}
				return clock()
			}
			var stdout, stderr bytes.Buffer
			env := cmdenv.Env{Args: append(args, "--state-dir", state), Stdout: &stdout, Stderr: &stderr, Cwd: dir, Environ: isolatedEnviron(t)}
			rc := MainWith(context.Background(), env, rt)
			if rc != 0 || written == nil {
				t.Fatalf("rc=%d 書き換えた=%v stdout=%q stderr=%q", rc, written != nil, stdout.String(), stderr.String())
			}
			if name == "end" && !strings.Contains(stdout.String(), "登録を終えました（1件）") {
				t.Fatalf("stdout=%q", stdout.String())
			}
			after, err := os.ReadFile(state + "/armed/a1.json")
			if err != nil || !bytes.Equal(after, written) {
				t.Fatalf("ほかのプロセスが終えた登録を書き直した:\n%s", after)
			}
		})
	}
}

// exists は path に何かがあるか (無いことを確かめられないときは止める)。
func exists(t *testing.T, path string) bool {
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, syscall.ENOENT) {
		t.Fatal(err)
	}
	return err == nil
}

// change が登録に 4300 桁を超える整数を入れたら、json.dumps と同じ ValueError で書かない (本家の _armed_write。Python 3.15.0 の実測の文)。
// 記録の driver は JSON の計画を読むときに同じ上限で止まるので、この値は記録に入れられない。
func TestArmedUpdateRefusesHugeIntegerLikeJSONDumps(t *testing.T) {
	state, rt := armedState(t)
	before, err := os.ReadFile(state + "/armed/a1.json")
	if err != nil {
		t.Fatal(err)
	}
	huge := new(big.Int).Exp(big.NewInt(10), big.NewInt(5000), nil)
	_, err = ArmedUpdate(context.Background(), rt, state, "a1", func(entry *pyjson.Object) (bool, error) {
		entry.Set("notified", pyops.IntValue(huge))
		return true, nil
	}, nil)
	var e *pyops.Exception
	want := "Exceeds the limit (4300 digits) for integer string conversion; use sys.set_int_max_str_digits() to increase the limit"
	if !errors.As(err, &e) || e.Type != "ValueError" || e.Msg != want {
		t.Fatalf("err = %#v", err)
	}
	if after, err := os.ReadFile(state + "/armed/a1.json"); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("登録を書いた: %v", err)
	}
}

// 場所の鍵は本家の _sha256(text.encode("utf-8", "surrogateescape"))。期待値は Python 3.15.0 の hashlib の実測。
func TestTextKeyEncodesLikeSurrogateescape(t *testing.T) {
	s := func(rs ...rune) string {
		var b []byte
		for _, r := range rs {
			b = pyfs.AppendSurrogate(b, r)
		}
		return string(b)
	}
	for text, want := range map[string]string{
		"/a/" + s(0xDCFF) + ".md":         "b843a8d45079c15c52d9fb8e8604aa0a9f5e1614ed71d284d7bc871fc033d78b",
		"/a/" + s(0xDCED, 0xDCB3, 0xDCBF): "f4152e44b9aa64ce2efad314a5c5e2a3e2943d7c9ffecf6563a546955cf6c4d5",
		"あ" + s(0xDC80):                   "73bd7c8fc7194456c57208018c0a681bb1565a94d90634f229bf5c804aee9fe5",
	} {
		if got := textKey(text); got != want {
			t.Errorf("textKey(%q) = %s、本家は %s", text, got, want)
		}
	}
	var err error
	func() {
		defer pyops.Catch(&err)
		textKey("/a/" + s(0xD800) + "x")
	}()
	var e *pyops.Exception
	want := "'utf-8' codec can't encode character '\\ud800' in position 3: surrogates not allowed"
	if !errors.As(err, &e) || e.Type != "UnicodeEncodeError" || e.Msg != want {
		t.Fatalf("err = %#v", err)
	}
}

// Monotonic の起点はプロセスで 1 つ (本家の time.monotonic)。後で作った DefaultRuntime も、プロセスが動いてからの時間を返す
// (Runtime ごとに 0 から数え直すと、hook が別の Runtime で作った締め切りが先へずれる)。
func TestDefaultRuntimeMonotonicSharesOrigin(t *testing.T) {
	first := DefaultRuntime()
	start := first.Monotonic()
	// 後で作る Runtime との間に、測れる差を置く (先の Runtime の時計が 10 ミリ秒進むまで待つ)
	testwait.Until(t, "時計が進む", func() bool { return first.Monotonic() >= start+0.01 })
	later := DefaultRuntime()
	if a, b := first.Monotonic(), later.Monotonic(); b < a {
		t.Fatalf("後で作った Runtime の時計 %v が、先に作った Runtime の時計 %v より遅れている", b, a)
	}
}
