package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pro-con/dispatcher"
	"pro-con/presence"
	"pro-con/store"
	supervisor "process_supervisor"
)

// supTest は supervisor を、sh の台本を dispatcher の代わりにして回す。scripts[i] が i 回目の起動 (足りなければ最後を繰り返す)。
type supTest struct {
	mu     sync.Mutex
	starts int
	stops  int
	said   []string
	out    bytes.Buffer
}

func (r *supTest) sup(dir string, scripts ...string) dispatcherSupervisor {
	return dispatcherSupervisor{
		dir: dir,
		command: func() (*exec.Cmd, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			script := scripts[min(r.starts, len(scripts)-1)]
			r.starts++
			return exec.Command("sh", "-c", script), nil
		},
		stop: func() error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.stops++
			return nil
		},
		out: &r.out,
		submit: func(req store.Request) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			if req.Kind != store.KindSupervisor {
				return nil
			}
			r.said = append(r.said, req.Note)
			return nil
		},
		now:        time.Now,
		ownerGrace: time.Millisecond, restartWait: time.Millisecond, retryWait: time.Millisecond, crashLimit: 2, crashWindow: time.Hour, stopWait: 5 * time.Second,
	}
}

func (r *supTest) saidText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.said, "\n")
}

// openOwner は持ち主の画面が開いている形にする。
func openOwner(t *testing.T, dir string) {
	t.Helper()
	scr, err := presence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scr.Close)
}

// dispatcher が自分の判断で抜けたら (rc=0)、起こし直さずに一緒に抜ける (ワンショット)。
func TestSupervisorExitsWithDispatcher(t *testing.T) {
	dir := t.TempDir()
	openOwner(t, dir)
	r := &supTest{}
	res := r.sup(dir, "exit 0").run(t.Context())
	if res.Reason != supervisor.ReasonDone || r.starts != 1 || r.stops != 0 || r.saidText() != "" {
		t.Fatalf("rc=0 の dispatcher を起こし直した / PG を止めた: %+v starts=%d stops=%d said=%q", res, r.starts, r.stops, r.saidText())
	}
}

// 落ちた dispatcher は、持ち主の画面が開いていれば起こし直す。
func TestSupervisorRestartsCrashedDispatcher(t *testing.T) {
	dir := t.TempDir()
	openOwner(t, dir)
	r := &supTest{}
	res := r.sup(dir, "exit 1", "exit 0").run(t.Context())
	if res.Reason != supervisor.ReasonDone || r.starts != 2 || r.stops != 0 || !strings.Contains(r.saidText(), "dispatcher が落ちた (exit status 1") {
		t.Fatalf("落ちた dispatcher を起こし直さない: %+v starts=%d stops=%d said=%q", res, r.starts, r.stops, r.saidText())
	}
}

// 落ち続けたら諦める: 人が止めた印を置いて (画面の keeper も起こさない)、PG を止める。
func TestSupervisorGivesUpAndHolds(t *testing.T) {
	dir := t.TempDir()
	openOwner(t, dir)
	r := &supTest{}
	res := r.sup(dir, "exit 1").run(t.Context())
	if res.Reason != supervisor.ReasonGaveUp || r.starts != 3 || r.stops != 1 || !store.Held(dir) {
		t.Fatalf("落ち続けても諦めない / 諦めて印を置かない・PG を止めない: %+v starts=%d stops=%d held=%v", res, r.starts, r.stops, store.Held(dir))
	}
	if said := r.saidText(); !strings.Contains(said, "3 回落ちた") || !strings.Contains(said, "人が止めた印を置いて PG を止める") {
		t.Fatalf("諦めたことを出来事にしない: %q", said)
	}
	spawns := 0
	if started, _ := startDispatcherIfIdle(dir, func(string) error { spawns++; return nil }); started {
		t.Fatal("諦めた後に画面の keeper が supervisor を起こす (落ち続けるのを繰り返す)")
	}
}

// 起こし直す前に人が止めた印があれば、起こさずに抜ける (PG は --stop が止めるので、supervisor は止めない)。
func TestSupervisorHaltsOnHeld(t *testing.T) {
	dir := t.TempDir()
	openOwner(t, dir)
	r := &supTest{}
	s := r.sup(dir, "exit 1")
	inner := s.command
	s.command = func() (*exec.Cmd, error) {
		if err := store.Hold(dir, time.Now()); err != nil { // 動いている間に人が --stop した形
			t.Error(err)
		}
		return inner()
	}
	res := s.run(t.Context())
	if res.Reason != supervisor.ReasonHalted || r.starts != 1 || r.stops != 0 || !strings.Contains(r.saidText(), "人が止めた印") {
		t.Fatalf("人が止めたのに起こし直した / PG を止め直した: %+v starts=%d stops=%d said=%q", res, r.starts, r.stops, r.saidText())
	}
}

// 落ちた後に止め終えていたら (起こし直しの待ちの間に最後の持ち主の画面が quit した)、起こさずに抜ける。
// 落ちる前の結果 (supervisor の起動より前・起動の後に止めかけて続けた) では止めない。
func TestSupervisorHaltsWhenStoppedAfterCrash(t *testing.T) {
	for name, c := range map[string]struct {
		resultAge time.Duration // 止めた結果を書いたのは今からどれだけ前か
		halt      bool
	}{
		"落ちた後":              {0, true},
		"起動の後・落ちる前":         {time.Hour, false},
		"supervisor の起動より前": {3 * time.Hour, false},
	} {
		dir := t.TempDir()
		openOwner(t, dir)
		result := filepath.Join(dir, dispatcher.StopResultFile)
		if err := os.WriteFile(result, []byte("ok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-c.resultAge)
		if err := os.Chtimes(result, at, at); err != nil {
			t.Fatal(err)
		}
		r := &supTest{}
		s := r.sup(dir, "exit 1", "exit 0")
		calls := 0
		s.now = func() time.Time { // 起動は 2 時間前、落ちたのは 30 分前
			calls++
			if calls == 1 {
				return time.Now().Add(-2 * time.Hour)
			}
			return time.Now().Add(-30 * time.Minute)
		}
		res := s.run(t.Context())
		if c.halt && (res.Reason != supervisor.ReasonHalted || r.starts != 1 || r.stops != 0) {
			t.Fatalf("%s: 落ちた後に止め終えたのに起こし直した: %+v starts=%d stops=%d", name, res, r.starts, r.stops)
		}
		if !c.halt && (res.Reason != supervisor.ReasonDone || r.starts != 2) {
			t.Fatalf("%s: 落ちる前の止めた結果を見て起こし直さない: %+v starts=%d said=%q", name, res, r.starts, r.saidText())
		}
	}
}

// 持ち主の画面が無ければ起こし直さず、PG を止めて抜ける (join の画面は起こさない = issue 481。人が止めた印は置かない)。
func TestSupervisorStopsPGsWithoutOwners(t *testing.T) {
	dir := t.TempDir()
	scr, err := presence.OpenAs(dir, presence.Info{Mode: presence.Join})
	if err != nil {
		t.Fatal(err)
	}
	defer scr.Close()
	r := &supTest{}
	res := r.sup(dir, "exit 1").run(t.Context())
	if res.Reason != supervisor.ReasonHalted || r.starts != 1 || r.stops != 1 || store.Held(dir) || !strings.Contains(r.saidText(), "持ち主の画面が無い") {
		t.Fatalf("持ち主の画面が無いのに起こし直した / PG を止めない / 印を置いた: %+v starts=%d stops=%d held=%v said=%q", res, r.starts, r.stops, store.Held(dir), r.saidText())
	}
}

// dispatcher が exitLockHeld で抜けたのは落ちたと数えない (lock が空いていれば起こし直して引き継ぐ)。
func TestSupervisorRetriesWhenLockFreed(t *testing.T) {
	dir := t.TempDir()
	openOwner(t, dir)
	r := &supTest{}
	res := r.sup(dir, "exit 3", "exit 3", "exit 3", "exit 3", "exit 0").run(t.Context())
	if res.Reason != supervisor.ReasonDone || r.starts != 5 || strings.Contains(r.saidText(), "落ちた") {
		t.Fatalf("lock を持たれていたのを落ちたと数えた: %+v starts=%d said=%q", res, r.starts, r.saidText())
	}
}

// 起こし直す前に別の dispatcher が lock を持っていれば、起こし直し続けずにそれに任せて抜ける (PG は止めない。
// dispatcher は lock の前に claude --version を引くので、10 秒ごとに起こすと手で起動した dispatcher が動く間ずっと続く)。
func TestSupervisorLeavesToAnotherDispatcher(t *testing.T) {
	dir := t.TempDir()
	openOwner(t, dir)
	unlock, err := dispatcher.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	r := &supTest{}
	res := r.sup(dir, "exit 3").run(t.Context())
	if res.Reason != supervisor.ReasonHalted || r.starts != 1 || r.stops != 0 || !strings.Contains(r.saidText(), "別の dispatcher が動いている") {
		t.Fatalf("別の dispatcher が居るのに起こし直した / PG を止めた: %+v starts=%d stops=%d said=%q", res, r.starts, r.stops, r.saidText())
	}
}

// 持ち主の画面が一瞬 0 に見えても (ctrl+r の exec の隙間)、猶予の間に戻れば PG を止めずに起こし直す。
func TestSupervisorWaitsForOwnerDuringUpgrade(t *testing.T) {
	dir := t.TempDir()
	r := &supTest{}
	s := r.sup(dir, "exit 1", "exit 0")
	s.ownerGrace = 5 * time.Second
	opened := make(chan *presence.Screen, 1)
	go func() {
		time.Sleep(200 * time.Millisecond) // 入れ替わった画面が印を置き直す
		scr, _ := presence.Open(dir)
		opened <- scr
	}()
	res := s.run(t.Context())
	if scr := <-opened; scr != nil {
		defer scr.Close()
	}
	if res.Reason != supervisor.ReasonDone || r.starts != 2 || r.stops != 0 {
		t.Fatalf("ctrl+r の隙間で持ち主が居ないと決めた: %+v starts=%d stops=%d said=%q", res, r.starts, r.stops, r.saidText())
	}
}

// 起こせなかった (実行ファイルが消えた) ときは、人が止めた印を置かず PG も止めない (印は置き場で共有 = 別のバイナリの画面の keeper まで止める)。
func TestSupervisorStartFailureDoesNotHold(t *testing.T) {
	dir := t.TempDir()
	openOwner(t, dir)
	r := &supTest{}
	s := r.sup(dir, "")
	s.command = func() (*exec.Cmd, error) { return exec.Command("/nonexistent/pro-con"), nil }
	res := s.run(t.Context())
	if res.Reason != supervisor.ReasonStartFailed || store.Held(dir) || r.stops != 0 || !strings.Contains(r.saidText(), "dispatcher を起こせない") {
		t.Fatalf("起こせないだけで印を置いた / PG を止めた: %+v held=%v stops=%d said=%q", res, store.Held(dir), r.stops, r.saidText())
	}
}

// 止める合図 (SIGTERM 等 = ctx の取り消し) では dispatcher に信号を送って待ち、PG は止めない (dispatcher が自分の決まりで止める)。
func TestSupervisorStopSignalsDispatcher(t *testing.T) {
	dir := t.TempDir()
	openOwner(t, dir)
	mark := filepath.Join(dir, "term")
	ready := filepath.Join(dir, "ready")
	r := &supTest{}
	s := r.sup(dir, `trap 'touch "`+mark+`"; exit 0' TERM; touch "`+ready+`"; while :; do sleep 0.01; done`)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan supervisor.Result, 1)
	go func() { done <- s.run(ctx) }()
	eventually(t, "dispatcher が立たない", func() bool { _, err := os.Stat(ready); return err == nil })
	cancel()
	res := <-done
	if _, err := os.Stat(mark); err != nil || res.Reason != supervisor.ReasonStopped || r.stops != 0 || r.starts != 1 {
		t.Fatalf("止める合図を dispatcher に届けない / PG を止めた: %+v stops=%d starts=%d err=%v", res, r.stops, r.starts, err)
	}
}

// 別の dispatcher が lock を持っていたら、dispatcher は exitLockHeld で抜ける (supervisor が落ちたと数えない印)。
func TestRunDispatcherExitsHeldWhenLocked(t *testing.T) {
	root, dir := heldRoot(t)
	unlock, err := dispatcher.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	var out, errOut bytes.Buffer
	if rc := runDispatcher([]string{"--once", "--e2e", root}, "", "", nil, pmConfig{}, &out, &errOut); rc != exitLockHeld {
		t.Fatalf("lock を持たれているのに rc=%d: %s", rc, errOut.String())
	}
}

// supervisor は 2 つ立たない: 別の supervisor が lock を持っていたら、dispatcher を起こさずにすぐ抜ける。
func TestRunSuperviseExitsWhenAnotherRuns(t *testing.T) {
	dir := t.TempDir()
	unlock, err := dispatcher.LockSupervisor(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	var out, errOut bytes.Buffer
	if rc := runSupervise(nil, dir, &out, &errOut); rc != 0 || out.Len() != 0 {
		t.Fatalf("別の supervisor が居るのに動いた: rc=%d out=%q err=%q", rc, out.String(), errOut.String())
	}
}

// supervisor の知らせは受付の箱を通って、dispatcher が supervisor の出来事として書く (記録は変えない)。
func TestSupervisorNoteBecomesEvent(t *testing.T) {
	dir := t.TempDir()
	if _, err := store.Submit(dir, store.Request{Kind: store.KindSupervisor, Note: "dispatcher が落ちた"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(dir, store.Request{Kind: store.KindSupervisor, Note: " "}); err != nil {
		t.Fatal(err)
	}
	res, err := store.Apply(dir, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Err != "" || res[0].Note != "dispatcher が落ちた" || res[1].Err == "" {
		t.Fatalf("supervisor の知らせを適用できない / 空の知らせを受けた: %+v", res)
	}
}
