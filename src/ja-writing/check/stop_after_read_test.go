package check

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"ja-writing/cmdenv"
)

// 読み終わった瞬間に届いたシグナル (読みは結果か誤りを返して終わったが、ctx はもう取り消されている) で、check が何も出さずに止まることを見る。
// 本家はハンドラが read の中で例外を投げるので、読みの結果も誤りの文も出さない。
// 本物の ctx では「読みが終わる」と「取り消し」の順を決められない (select がどちらを選ぶかは決まらない) ので、Done を閉じずに Err だけを
// 返す ctx (flipCtx) を使い、読みが終わる瞬間に取り消す。untilStopped の select は読みの終わりを選び、その後に ctx を見るかだけが答えを決める。

// flipCtx は Done を閉じないが、flip の後は Err が Canceled を返す ctx。
type flipCtx struct{ on atomic.Bool }

func (c *flipCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *flipCtx) Done() <-chan struct{}       { return nil }
func (c *flipCtx) Value(any) any               { return nil }
func (c *flipCtx) Err() error {
	if c.on.Load() {
		return context.Canceled
	}
	return nil
}

// flipReader は data を返し終えた次の Read で ctx を取り消し (flip が true のとき)、err か EOF を返す。
type flipReader struct {
	ctx  *flipCtx
	flip bool
	data []byte
	err  error
}

func (r *flipReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	if r.flip {
		r.ctx.on.Store(true)
	}
	if r.err != nil {
		return 0, r.err
	}
	return 0, io.EOF
}

// runFlip は標準入力が data (の後に err) の check を動かす。flip なら読み終わる瞬間に止める。状態フォルダは dir/state。
func runFlip(t *testing.T, flip bool, data []byte, readErr error, args ...string) (dir string, out runOutcome) {
	t.Helper()
	dir = fixtureRoot(t)
	ctx := &flipCtx{}
	var stdout, stderr bytes.Buffer
	env := cmdenv.Env{
		Args: append(args, "--state-dir", dir+"/state"), Stdin: &flipReader{ctx: ctx, flip: flip, data: data, err: readErr},
		Stdout: &stdout, Stderr: &stderr, Cwd: dir, Environ: []string{"HOME=" + dir},
	}
	rc := MainWith(ctx, env, childRuntime(t))
	return dir, runOutcome{rc, stdout.String(), stderr.String()}
}

const stoppedByInterrupt = "yomiyasu_check.py: 止められました（SIGINT）。動いていた検査を止めて終わります。\n"

func TestCheckPrintsNothingWhenStoppedAsInputEnds(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		readErr error
		args    []string
		// unstopped は止めないときに出るもの (この case が結果か誤りの文を出す枝を通ることの確かめ)
		unstopped func(runOutcome) bool
	}{
		{"verify の結果", []byte("これは最終稿です。\n"), nil, []string{"verify", "--final", "-"},
			func(o runOutcome) bool { return strings.Contains(o.stdout, "no_record via=verify") }},
		{"verify の読みの誤り", nil, errors.New("読めない"), []string{"verify", "--final", "-"},
			func(o runOutcome) bool { return o.rc != 0 && strings.Contains(o.stderr, "読めない") }},
		{"run --stdin の空の入力", nil, nil, []string{"run", "--stdin"},
			func(o runOutcome) bool { return o.rc != 0 && o.stderr != "" && o.stdout == "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, out := runFlip(t, false, c.data, c.readErr, c.args...); !c.unstopped(out) {
				t.Fatalf("止めないときに結果か誤りの文を出していない rc=%d stdout=%q stderr=%q", out.rc, out.stdout, out.stderr)
			}
			dir, out := runFlip(t, true, c.data, c.readErr, c.args...)
			if out.rc != 128+int(syscall.SIGINT) || out.stdout != "" || out.stderr != stoppedByInterrupt {
				t.Fatalf("rc=%d stdout=%q stderr=%q", out.rc, out.stdout, out.stderr)
			}
			if _, err := os.Lstat(dir + "/state"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("止められた check が状態フォルダを作った: %v", err)
			}
		})
	}
}

// read が終わった後に取り消されていれば、read の panic も投げ直さずに止める (本家は read の中で例外を投げ、read の誤りは出ない)。
func TestUntilStoppedPrefersStopOverReadOutcome(t *testing.T) {
	for _, panics := range []bool{false, true} {
		ctx := &flipCtx{}
		var stop errStopped
		err := untilStopped(ctx, func() {
			ctx.on.Store(true)
			if panics {
				panic("読みの誤り")
			}
		})
		if !errors.As(err, &stop) || stop.signal != syscall.SIGINT {
			t.Fatalf("panics=%v: untilStopped = %v", panics, err)
		}
	}
}
