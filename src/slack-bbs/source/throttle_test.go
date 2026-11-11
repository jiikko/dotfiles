package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock は待たずに進む時計。sleep は時計を進めるだけ。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d > 0 {
		c.mu.Lock()
		c.t = c.t.Add(d)
		c.mu.Unlock()
	}
	return nil
}

// realPoll は flock の取り直しの待ち。偽の時計を進めない (進めると、取り直しの待ちで間が偶然に空き、状態の共有の退行が見えない)。
func realPoll(ctx context.Context, _ time.Duration) error {
	time.Sleep(200 * time.Microsecond) // sleep-ok: tick: flock の取り直しの刻み (待ちの実体は flock)
	return ctx.Err()
}

// recRunner は呼ばれた時刻とサブコマンドを覚え、同時に走った本数の最大を数える。errs は呼ばれた順に返す誤り。
type recRunner struct {
	now      func() time.Time
	mu       sync.Mutex
	starts   []time.Time
	cmds     []string
	errs     []error
	inflight atomic.Int32
	maxIn    atomic.Int32
	hold     time.Duration // 実時間で握る長さ (重なりを起こしやすくする)
}

func (r *recRunner) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	n := r.inflight.Add(1)
	defer r.inflight.Add(-1)
	for {
		m := r.maxIn.Load()
		if n <= m || r.maxIn.CompareAndSwap(m, n) {
			break
		}
	}
	r.mu.Lock()
	r.starts = append(r.starts, r.now())
	r.cmds = append(r.cmds, args[0])
	var err error
	if i := len(r.starts) - 1; i < len(r.errs) {
		err = r.errs[i]
	}
	r.mu.Unlock()
	if r.hold > 0 {
		time.Sleep(r.hold) // sleep-ok: window: 同時に走る呼び出しが重なる窓を作る入力 (待ちではない)
	}
	return []byte("[]"), []byte("案内\n"), err
}

func (r *recRunner) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.starts)
}

func throttleFor(r Runner, lockPath string, c *fakeClock) *Throttle {
	t := NewThrottle(r, lockPath, nil)
	t.g.now, t.g.sleep, t.g.poll = c.now, c.sleep, realPoll
	return t
}

var t0 = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

// limitErr は slack-cli が 429 で諦めたときの誤り (ExecRunner.Run が stderr を添えた形)。
func limitErr(wait string) error {
	return errors.New("slack history: exit status 1: レート制限（429）: conversations.history。" + wait + "待って再実行してください")
}

// 同じサブコマンドを続けて呼ぶときは、そのサブコマンドの段の間を空ける。サブコマンドが違えば floor だけ空ける。
func TestThrottleSpacesCallsByMethod(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now}
	th := throttleFor(r, "", c)
	for _, cmd := range []string{"history", "history", "search", "search", "whoami", "whoami"} {
		if _, _, err := th.Run(context.Background(), cmd); err != nil {
			t.Fatal(err)
		}
	}
	want := []time.Duration{gapTier3, gapFloor, gapTier2, gapFloor, gapDefault}
	for i, w := range want {
		if got := r.starts[i+1].Sub(r.starts[i]); got != w {
			t.Errorf("%s の前の間 = %v, want %v", r.cmds[i+1], got, w)
		}
	}
}

// 置き場が無く (lockPath が空) プロセスの中だけで並べるときも、読み直しを全チャンネルで同時に始めた呼び出しは重ならない。
func TestThrottleQueuesInProcess(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now, hold: 2 * time.Millisecond}
	th := throttleFor(r, "", c)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, _, err := th.Run(context.Background(), "history"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if m := r.maxIn.Load(); m != 1 {
		t.Errorf("同時に走った呼び出しの最大 = %d, want 1", m)
	}
}

// 列はプロセスをまたぐ: 同じファイルを共有する 2 つの Throttle (別のプロセスに当たる) からの呼び出しは重ならない。
func TestThrottleQueuesAcrossProcesses(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now, hold: 2 * time.Millisecond}
	path := filepath.Join(t.TempDir(), "slack-cli.lock")
	a, b := throttleFor(r, path, c), throttleFor(r, path, c)
	var wg sync.WaitGroup
	for i := range 12 {
		th := a
		if i%2 == 1 {
			th = b
		}
		wg.Go(func() {
			if _, _, err := th.Run(context.Background(), "history"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if m := r.maxIn.Load(); m != 1 {
		t.Errorf("同時に走った呼び出しの最大 = %d, want 1", m)
	}
}

// 間とレート制限の期限はファイルで共有する: 別のプロセスの最後の呼び出しの終わりから間を空け、別のプロセスが止めた期限まで待つ。
func TestThrottleSharesStateAcrossProcesses(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now, errs: []error{nil, nil, limitErr("120 秒")}}
	path := filepath.Join(t.TempDir(), "slack-cli.lock")
	a, b := throttleFor(r, path, c), throttleFor(r, path, c)
	for _, th := range []*Throttle{a, b} {
		if _, _, err := th.Run(context.Background(), "history"); err != nil {
			t.Fatal(err)
		}
	}
	if d := r.starts[1].Sub(r.starts[0]); d != gapTier3 {
		t.Errorf("別のプロセスの後の間 = %v, want %v", d, gapTier3)
	}
	if _, _, err := a.Run(context.Background(), "history"); err == nil {
		t.Fatal("前提: 120 秒の 429 は誤りを返す")
	}
	if _, _, err := b.Run(context.Background(), "whoami"); err != nil {
		t.Fatal(err)
	}
	if d := r.starts[3].Sub(r.starts[2]); d != 120*time.Second {
		t.Errorf("別のプロセスが止めた期限より前に呼んだ: 間 %v", d)
	}
}

// 続けて呼ぶプロセスがいても、他のプロセスも順番が回ってくる (間を空ける待ちの間は flock を持たない)。実時間で回し、時間でなく
// 「A が 40 回呼び終える前に B が呼べたか」で見る。間を空ける待ちの間も flock を持つと、A が flock を手放す隙はマイクロ秒しか無く、
// B は取り直し (lockPoll ごと) でほとんど入れない。
func TestThrottleDoesNotStarveOtherProcess(t *testing.T) {
	r := &recRunner{now: time.Now}
	path := filepath.Join(t.TempDir(), "slack-cli.lock")
	mk := func() *Throttle {
		th := NewThrottle(r, path, nil)
		th.g.gap = func(string) time.Duration { return 10 * time.Millisecond }
		th.g.floor = 5 * time.Millisecond
		return th
	}
	a, b := mk(), mk()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // hang guard
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 40 {
			if _, _, err := a.Run(ctx, "whoami"); err != nil {
				t.Error(err)
				return
			}
		}
	})
	time.Sleep(15 * time.Millisecond) // sleep-ok: window: A が続けて呼び始めてから B が並ぶ順を作る入力
	wg.Go(func() {
		if _, _, err := b.Run(ctx, "team"); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, c := range r.cmds {
		if c == "team" {
			if i == len(r.cmds)-1 {
				t.Errorf("B は A が 40 回呼び終えるまで呼べなかった (飢えた)")
			}
			return
		}
	}
	t.Error("B が呼ばれていない")
}

// 間の短いサブコマンド (history) を続けて呼ぶプロセスがいても、間の長いサブコマンド (search) を呼ぶ別のプロセスは追い越され続けない。
func TestThrottleDoesNotStarveSlowMethod(t *testing.T) {
	r := &recRunner{now: time.Now}
	path := filepath.Join(t.TempDir(), "slack-cli.lock")
	mk := func() *Throttle {
		th := NewThrottle(r, path, nil)
		th.g.gap = func(cmd string) time.Duration {
			if cmd == "search" {
				return 60 * time.Millisecond
			}
			return 25 * time.Millisecond
		}
		th.g.floor = 5 * time.Millisecond
		return th
	}
	a, b := mk(), mk()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // hang guard
	defer cancel()
	if _, _, err := b.Run(ctx, "search"); err != nil { // search の前回の終わりを作る
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 25 {
			if _, _, err := a.Run(ctx, "history"); err != nil {
				t.Error(err)
				return
			}
		}
	})
	time.Sleep(10 * time.Millisecond) // sleep-ok: window: A が続けて呼び始めてから B が並ぶ順を作る入力
	wg.Go(func() {
		if _, _, err := b.Run(ctx, "search"); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmds[len(r.cmds)-1] == "search" {
		t.Errorf("search は history が 25 回呼び終えるまで呼べなかった: %v", r.cmds)
	}
}

// レート制限に当たったら、slack-cli が書いた秒数だけ列ごと止めてから 1 回だけやり直し、待ったことを警告の行で返す。
// 次の呼び出しも、止めた期限まで待つ。続けて当たったら、それ以上やり直さずに誤りを返す。
func TestThrottleRetriesOnceOnRateLimit(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now, errs: []error{limitErr("30 秒"), nil, limitErr("30 秒"), limitErr("30 秒")}}
	th := throttleFor(r, "", c)
	var notify strings.Builder
	th.notify = &notify
	_, errb, err := th.Run(context.Background(), "history")
	if err != nil || len(r.starts) != 2 {
		t.Fatalf("やり直さない: err=%v 呼んだ回数 %d", err, len(r.starts))
	}
	if d := r.starts[1].Sub(r.starts[0]); d != 30*time.Second {
		t.Errorf("やり直す前の待ち = %v, want 30s", d)
	}
	if !strings.HasPrefix(string(errb), "警告: Slack のレート制限に当たったので、30 秒待って slack history をやり直した\n") || !strings.Contains(string(errb), "案内") {
		t.Errorf("待ったことが警告に無い (または slack-cli の stderr が消えた): %q", errb)
	}
	if !strings.Contains(notify.String(), "30 秒は Slack を呼ばない") {
		t.Errorf("端末へ止めることを知らせない: %q", notify.String())
	}
	if _, _, err := th.Run(context.Background(), "history"); err == nil || len(r.starts) != 4 {
		t.Errorf("続けて当たったのに 2 回より多くやり直した・誤りを返さない: err=%v 呼んだ回数 %d", err, len(r.starts))
	}
	if _, _, err := th.Run(context.Background(), "whoami"); err != nil {
		t.Fatal(err)
	}
	if d := r.starts[4].Sub(r.starts[3]); d != 30*time.Second {
		t.Errorf("止めた期限より前に次を呼んだ: 間 %v", d)
	}
}

// 60 秒より長い・秒数の分からない待ちを指定された 429 は、やり直さずに誤りを返し、列をその分止める (サーバーの指定より早く打たない)。
func TestThrottleDoesNotRetryLongRateLimit(t *testing.T) {
	for _, tc := range []struct {
		wait string
		want time.Duration
	}{{"120 秒", 120 * time.Second}, {`しばらく（Retry-After: "Wed, 21 Oct 2026 07:28:00 GMT"）`, rateLimitWait}, {"3600 秒", maxPause}} {
		c := &fakeClock{t: t0}
		r := &recRunner{now: c.now, errs: []error{limitErr(tc.wait)}}
		th := throttleFor(r, "", c)
		if _, _, err := th.Run(context.Background(), "history"); err == nil || r.calls() != 1 {
			t.Errorf("%s: やり直した・誤りを返さない: err=%v 呼んだ回数 %d", tc.wait, err, r.calls())
		}
		if _, _, err := th.Run(context.Background(), "whoami"); err != nil {
			t.Fatal(err)
		}
		if d := r.starts[1].Sub(r.starts[0]); d != tc.want {
			t.Errorf("%s: 列を止めた長さ = %v, want %v", tc.wait, d, tc.want)
		}
	}
}

// 誤りの判定は slack-cli の最終の誤り (「待って再実行してください」) の最後の 1 つで行う。slack-cli が自分で待ち直した行
// (「待って再試行します」) を 429 と読まない。
func TestRateLimited(t *testing.T) {
	retrying := "レート制限（429）: conversations.history。30 秒待って再試行します（1/3）\n"
	for _, tc := range []struct {
		msg          string
		wait         time.Duration
		retry, limit bool
	}{
		{"レート制限（429）: search.messages。5 秒待って再実行してください", 5 * time.Second, true, true},
		{retrying + "エラー: 応答が大きすぎます", 0, false, false},
		{retrying + "レート制限（429）: conversations.history。120 秒待って再実行してください", 120 * time.Second, false, true},
		{"レート制限（429）: x。しばらく待って再実行してください", rateLimitWait, false, true},
		{"レート制限（429）: x。5 秒待って再実行してください\nレート制限（429）: y。120 秒待って再実行してください", 120 * time.Second, false, true},
		{"channel_not_found", 0, false, false},
	} {
		w, retry, ok := rateLimited(errors.New(tc.msg))
		if w != tc.wait || retry != tc.retry || ok != tc.limit {
			t.Errorf("%q: %v %v %v, want %v %v %v", tc.msg, w, retry, ok, tc.wait, tc.retry, tc.limit)
		}
	}
}

// ファイルの時刻が先すぎれば (壊れた・時計を戻した) maxPause に詰めて書き直し、止まったままにしない。
func TestThrottleClampsFutureState(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now}
	path := filepath.Join(t.TempDir(), "slack-cli.lock")
	b, _ := json.Marshal(paceState{LastEnd: t0.AddDate(1, 0, 0), PausedUntil: t0.AddDate(1, 0, 0)})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := throttleFor(r, path, c).Run(context.Background(), "history"); err != nil {
		t.Fatal(err)
	}
	if d := r.starts[0].Sub(t0); d != maxPause {
		t.Errorf("先の時刻で待った長さ = %v, want %v", d, maxPause)
	}
}

// 列のファイルが使えないときは、プロセスの中だけで並べ、そのことは 1 回だけ警告する (呼び出しのたびに板の注記へ並べない)。
func TestThrottleWarnsOnceWhenLockUnusable(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now}
	th := throttleFor(r, t.TempDir(), c) // ディレクトリは開けない
	_, first, _ := th.Run(context.Background(), "history")
	_, second, _ := th.Run(context.Background(), "history")
	if !strings.Contains(string(first), "警告: Slack の呼び出しの列をプロセスの間で共有できない") || strings.Contains(string(second), "共有できない") {
		t.Errorf("警告が 1 回目だけでない: %q / %q", first, second)
	}
	if d := r.starts[1].Sub(r.starts[0]); d != gapTier3 {
		t.Errorf("ファイルが使えないとき、プロセスの中の間 = %v, want %v", d, gapTier3)
	}
}

// 警告を出した呼び出しが誤りで終わったら (Client は誤りのとき stderr を捨てる)、または警告を板に載せないサブコマンド (whoami) だったら、
// 次の呼び出しで警告を出し直す。
func TestThrottleRewarnsUntilShown(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now, errs: []error{nil, errors.New("channel_not_found")}}
	th := throttleFor(r, t.TempDir(), c)
	_, _, _ = th.Run(context.Background(), "whoami")
	_, _, _ = th.Run(context.Background(), "history")
	if _, errb, err := th.Run(context.Background(), "history"); err != nil || !strings.Contains(string(errb), "共有できない") {
		t.Errorf("届かなかった警告を出し直さない: %q %v", errb, err)
	}
}

// 列で待っている間に取り消されたら、呼ばずに戻る。
func TestThrottleStopsWaitingOnCancel(t *testing.T) {
	c := &fakeClock{t: t0}
	r := &recRunner{now: c.now}
	th := throttleFor(r, "", c)
	th.g.tok <- struct{}{} // 他の呼び出しが列の先頭にいる
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := th.Run(ctx, "history"); !errors.Is(err, context.Canceled) || len(r.starts) != 0 {
		t.Errorf("取り消しで戻らない: err=%v 呼んだ回数 %d", err, len(r.starts))
	}
}

// Quiet は中の slack-cli と知らせを静かにしても、列は同じものを使う (分けると同じプロセスの中で 2 本ずつ Slack へ届く)。
func TestQuietKeepsThrottleQueue(t *testing.T) {
	th := NewThrottle(ExecRunner{Bin: "slack", Tee: &strings.Builder{}}, "", &strings.Builder{})
	q, ok := Quiet(th).(*Throttle)
	if !ok || q.g != th.g || q.notify != nil {
		t.Fatalf("Quiet が列を分けた・知らせを残した: %#v", Quiet(th))
	}
	if er, ok := q.r.(ExecRunner); !ok || er.Tee != nil {
		t.Errorf("中の slack-cli が静かにならない: %#v", q.r)
	}
}
