package dispatcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"pro-con/eventlog"
	"pro-con/schedule"
	"pro-con/store"
)

var testJob = schedule.Job{Name: "job", Hour: 4, Min: 0, Args: []string{"worktree", "clean", "--yes"}, Lock: "job.lock"}

// schedFixture は予定 1 つを持つ dispatcher。runner は呼ばれた回数を数え、rc と note を返す。
func schedFixture(t *testing.T, rc int, note string) (*Dispatcher, *atomic.Int32, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.Local)
	calls := &atomic.Int32{}
	d := &Dispatcher{Dir: t.TempDir(), Now: func() time.Time { return now }, Scheduled: []schedule.Job{testJob},
		RunScheduled: func(_ context.Context, j schedule.Job) (int, string) {
			calls.Add(1)
			return rc, note
		}}
	return d, calls, &now
}

// waitSchedule は裏の goroutine が結果を置くまで待つ (上限つき)。
func waitSchedule(t *testing.T, d *Dispatcher) {
	t.Helper()
	for range 400 {
		if !d.schedBusy.Load() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("予定の goroutine が 2 秒たっても終わらない")
}

func kinds(evs []eventlog.Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Kind+": "+e.Reason)
	}
	return out
}

// 回す時刻を過ぎた予定を 1 回だけ起こし、始めた時刻を先に、終わりを次の Tick で記録する。同じ枠では起こし直さない。
func TestTickScheduleRunsOncePerSlot(t *testing.T) {
	d, calls, now := schedFixture(t, 0, "結果: worktree 消した 2")
	evs := d.tickSchedule(context.Background(), *now)
	if len(evs) != 1 || evs[0].Kind != eventlog.KindSchedule || !strings.Contains(evs[0].Reason, "pro-con worktree clean --yes を起こした") {
		t.Fatalf("起こした出来事 = %v", kinds(evs))
	}
	if rec, _ := store.LoadSchedule(d.Dir); !rec["job"].Start.Equal(*now) || !rec["job"].End.IsZero() {
		t.Errorf("起こす前の記録 = %+v", rec["job"])
	}
	waitSchedule(t, d)
	*now = now.Add(time.Minute)
	evs = d.tickSchedule(context.Background(), *now)
	if len(evs) != 1 || evs[0].Kind != eventlog.KindSchedule || !strings.Contains(evs[0].Reason, "結果: worktree 消した 2") {
		t.Fatalf("終わった出来事 = %v", kinds(evs))
	}
	rec, err := store.LoadSchedule(d.Dir)
	if err != nil || rec["job"].RC != 0 || rec["job"].End.IsZero() || rec["job"].Note != "結果: worktree 消した 2" {
		t.Errorf("終わった記録 = %+v (%v)", rec["job"], err)
	}
	*now = now.Add(time.Hour)
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 0 || calls.Load() != 1 {
		t.Errorf("同じ枠で起こし直した: calls=%d evs=%v", calls.Load(), kinds(evs))
	}
	*now = time.Date(2026, 9, 28, 4, 0, 0, 0, time.Local) // 次の枠
	d.tickSchedule(context.Background(), *now)
	waitSchedule(t, d)
	if calls.Load() != 2 {
		t.Errorf("次の枠で起こさない: calls=%d", calls.Load())
	}
}

// 走っている間は起こさず、入れ替えは待つ (上限まで)。結果を記録する前も待つ。
func TestScheduleBusyHoldsUpgrade(t *testing.T) {
	d, _, now := schedFixture(t, 0, "結果: x")
	release := make(chan struct{})
	d.RunScheduled = func(context.Context, schedule.Job) (int, string) { <-release; return 0, "結果: x" }
	d.tickSchedule(context.Background(), *now)
	if !d.scheduleBusy(*now) {
		t.Error("走っている間に入れ替えを待たない")
	}
	if d.scheduleBusy(now.Add(scheduleWaitLimit)) {
		t.Error("上限を過ぎても入れ替えを待つ (止まったままの子で入れ替えが永遠に止まる)")
	}
	close(release)
	waitSchedule(t, d)
	if !d.scheduleBusy(*now) {
		t.Error("結果を記録する前に入れ替えを許した (rc を記録できなくなる)")
	}
	d.tickSchedule(context.Background(), now.Add(time.Second))
	if d.scheduleBusy(*now) {
		t.Error("記録した後も入れ替えを待つ")
	}
}

// 失敗は error、別の実行と重なった (schedule.ExitLocked) は error にしない。
func TestFinishScheduledKinds(t *testing.T) {
	for _, c := range []struct {
		rc   int
		kind string
		word string
	}{{1, eventlog.KindError, "失敗した (rc=1)"}, {schedule.ExitLocked, eventlog.KindSchedule, "重なったので何もしなかった"}, {-1, eventlog.KindError, "失敗した (rc=-1)"}} {
		d, _, now := schedFixture(t, c.rc, "why")
		d.tickSchedule(context.Background(), *now)
		waitSchedule(t, d)
		evs := d.tickSchedule(context.Background(), *now)
		if len(evs) != 1 || evs[0].Kind != c.kind || !strings.Contains(evs[0].Reason, c.word) {
			t.Errorf("rc=%d: %v", c.rc, kinds(evs))
		}
	}
}

// 人が止めた (schedule off) なら起こさない。
func TestTickScheduleOff(t *testing.T) {
	d, calls, now := schedFixture(t, 0, "")
	d.settings.ScheduleOff = true
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 0 || calls.Load() != 0 {
		t.Errorf("off で起こした: %v", kinds(evs))
	}
}

// 記録を読めないなら回さない (読めない = 始めた記録が無い、と読むと Tick ごとに回す)。理由は 1 度だけ出す。
func TestTickScheduleUnreadableRecord(t *testing.T) {
	d, calls, now := schedFixture(t, 0, "")
	if err := os.WriteFile(filepath.Join(d.Dir, store.ScheduleFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	evs := d.tickSchedule(context.Background(), *now)
	if len(evs) != 1 || evs[0].Kind != eventlog.KindError || !strings.Contains(evs[0].Reason, "記録を読めない") || calls.Load() != 0 {
		t.Fatalf("壊れた記録で %v (calls=%d)", kinds(evs), calls.Load())
	}
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 0 {
		t.Errorf("同じ理由を重ねた: %v", kinds(evs))
	}
}

// 始めた記録を書けないなら起こさない (書かずに起こすと、終わった直後の Tick でまた起こす)。
func TestTickScheduleUnwritableRecord(t *testing.T) {
	d, calls, now := schedFixture(t, 0, "")
	if err := os.Chmod(d.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(d.Dir, 0o700) })
	evs := d.tickSchedule(context.Background(), *now)
	if calls.Load() != 0 || len(evs) != 1 || evs[0].Kind != eventlog.KindError {
		t.Errorf("書けないのに起こした: calls=%d %v", calls.Load(), kinds(evs))
	}
}

// 前の dispatcher が待てなかった予定 (始めた記録だけ): lock が空いていれば出力から結果を読んで締め、持たれていれば待つ。
// 出力が始めた時刻より古ければ、前の回の結果を今回と読まない。
func TestSettleOrphan(t *testing.T) {
	d, calls, now := schedFixture(t, 0, "")
	start := now.Add(-time.Hour)
	if err := store.SaveScheduleRun(d.Dir, "job", store.ScheduleRun{Start: start, RC: -1}); err != nil {
		t.Fatal(err)
	}
	out := store.ScheduleOutPath(d.Dir, "job")
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("消した pc-c-001: x\n"+schedule.ResultLine("worktree 消した 1", 0)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := lockAs(d.Dir, "job.lock", errScheduleRunning, "test")
	if err != nil {
		t.Fatal(err)
	}
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 0 {
		t.Errorf("lock を持たれているのに締めた: %v", kinds(evs))
	}
	held.Release()
	evs := d.tickSchedule(context.Background(), *now)
	rec, _ := store.LoadSchedule(d.Dir)
	if len(evs) != 1 || evs[0].Kind != eventlog.KindSchedule || rec["job"].End.IsZero() || !strings.Contains(rec["job"].Note, "結果: worktree 消した 1") {
		t.Errorf("締めていない: %v / %+v", kinds(evs), rec["job"])
	}
	if calls.Load() != 0 {
		t.Errorf("同じ枠の予定を起こし直した (calls=%d)", calls.Load())
	}

	old := start.Add(-time.Hour)
	if err := os.Chtimes(out, old, old); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveScheduleRun(d.Dir, "job", store.ScheduleRun{Start: start, RC: -1}); err != nil {
		t.Fatal(err)
	}
	evs = d.tickSchedule(context.Background(), *now)
	rec, _ = store.LoadSchedule(d.Dir)
	if strings.Contains(rec["job"].Note, "結果:") || !strings.Contains(rec["job"].Note, "起こす前に") || len(evs) != 1 || evs[0].Kind != eventlog.KindError {
		t.Errorf("前の回の出力を今回の結果と読んだ: %+v", rec["job"])
	}
}

// 本物の起こし方: argv どおり・cwd は状態の置き場・出力はファイルへ・結果は stdout の最後の「結果: 」の行 (無ければ stderr の最後の行)。
func TestExecScheduled(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(t.TempDir(), "fake-pro-con")
	script := `#!/bin/sh
printf '%s\n' "args=$*" "cwd=$(pwd -P)" "tmux=${TMUX-unset}" "fd=${` + LockFDEnv + `-unset}" "pgid=$(ps -o pgid= -p $$ | tr -d ' ')"
echo "結果: 古い行"
echo "結果: worktree 消した 3"
echo "warn" >&2
[ "$1" = fail ] && { echo "最後の理由" >&2; exit 7; }
exit 0
`
	if err := os.WriteFile(exe, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "/tmp/sock,1,0")
	t.Setenv(LockFDEnv, "9")
	run := ExecScheduled(exe, dir)
	rc, note := run(context.Background(), schedule.Job{Name: "j", Args: []string{"worktree", "clean", "--yes"}})
	if rc != 0 || note != "結果: worktree 消した 3" {
		t.Errorf("rc=%d note=%q", rc, note)
	}
	out, err := os.ReadFile(store.ScheduleOutPath(dir, "j"))
	if err != nil {
		t.Fatal(err)
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	// 自分のプロセスグループで動く (dispatcher のグループへの信号で、片付けの途中で殺さない)
	if strings.Contains(string(out), fmt.Sprintf("pgid=%d\n", syscall.Getpgrp())) || !strings.Contains(string(out), "pgid=") {
		t.Errorf("子が dispatcher と同じプロセスグループ: %s", out)
	}
	for _, want := range []string{"args=worktree clean --yes", "cwd=" + realDir, "tmux=unset", "fd=unset"} {
		if !slices.Contains(strings.Split(string(out), "\n"), want) {
			t.Errorf("stdout に %q が無い: %s", want, out)
		}
	}
	if b, _ := os.ReadFile(store.ScheduleErrPath(dir, "j")); string(b) != "warn\n" {
		t.Errorf("stderr = %q", b)
	}
	rc, note = run(context.Background(), schedule.Job{Name: "j", Args: []string{"fail"}})
	if rc != 7 || note != "結果: worktree 消した 3 / 最後の理由" {
		t.Errorf("失敗は結果の行に stderr の最後の行を足す: rc=%d note=%q", rc, note)
	}
}

// 結果の行が無ければ stderr の最後の行、それも無ければ起こせなかった理由。
func TestScheduleNoteFallback(t *testing.T) {
	dir := t.TempDir()
	out, errp := filepath.Join(dir, "o"), filepath.Join(dir, "e")
	_ = os.WriteFile(out, []byte("消した pc-c-001\n"), 0o600)
	_ = os.WriteFile(errp, []byte("pro-con worktree clean: 記録を読めない\n\n"), 0o600)
	if got := scheduleNote(out, errp, nil); got != "pro-con worktree clean: 記録を読めない" {
		t.Errorf("stderr の最後の行 = %q", got)
	}
	if got := scheduleNote(filepath.Join(dir, "x"), filepath.Join(dir, "y"), os.ErrPermission); got != os.ErrPermission.Error() {
		t.Errorf("起こせなかった理由 = %q", got)
	}
}

// テストの二進は自分を予定として起こさない (argv を解さず、テストを走らせ直す)。
func TestExecScheduledRefusesTestBinary(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if rc, note := ExecScheduled(self, t.TempDir())(context.Background(), testJob); rc != -1 || !strings.Contains(note, "テストの二進") {
		t.Errorf("rc=%d note=%q", rc, note)
	}
}

// 設定を読めないなら回さない (schedule off を読めないまま破壊的な予定を回さない)。
func TestTickScheduleSettingsBroken(t *testing.T) {
	d, calls, now := schedFixture(t, 0, "")
	if err := os.WriteFile(filepath.Join(d.Dir, store.SettingsFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.loadSettings()
	evs := d.tickSchedule(context.Background(), *now)
	if calls.Load() != 0 || len(evs) != 1 || evs[0].Kind != eventlog.KindError || !strings.Contains(evs[0].Reason, "設定を読めない") {
		t.Errorf("設定を読めないのに回した: calls=%d %v", calls.Load(), kinds(evs))
	}
}

// dispatcher を止めても (Tick の ctx を取り消しても) 予定の子には取り消しを渡さない。
func TestTickScheduleIgnoresCancel(t *testing.T) {
	d, _, now := schedFixture(t, 0, "")
	var got atomic.Value
	d.RunScheduled = func(ctx context.Context, _ schedule.Job) (int, string) { got.Store(ctx.Err() == nil); return 0, "" }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.tickSchedule(ctx, *now)
	waitSchedule(t, d)
	if ok, _ := got.Load().(bool); !ok {
		t.Error("取り消した ctx を予定の子に渡した (dispatcher を止めると片付けの途中で殺す)")
	}
}

// 前の dispatcher の子がまだ lock を持っている間は、次の枠が来ても新しく起こさない (出力を切り詰めない・記録を上書きしない)。
// 長く持たれ続けたら error を 1 度出す。
func TestTickScheduleWaitsForOrphan(t *testing.T) {
	d, calls, now := schedFixture(t, 0, "")
	start := time.Date(2026, 9, 26, 4, 0, 0, 0, time.Local)
	if err := store.SaveScheduleRun(d.Dir, "job", store.ScheduleRun{Start: start, RC: -1}); err != nil {
		t.Fatal(err)
	}
	held, err := lockAs(d.Dir, "job.lock", errScheduleRunning, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	*now = start.Add(time.Hour)
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 0 || calls.Load() != 0 {
		t.Errorf("持たれている間に何かした: calls=%d %v", calls.Load(), kinds(evs))
	}
	*now = time.Date(2026, 9, 27, 9, 0, 0, 0, time.Local) // 次の枠を過ぎ、6 時間以上持たれている
	evs := d.tickSchedule(context.Background(), *now)
	if calls.Load() != 0 || len(evs) != 1 || evs[0].Kind != eventlog.KindError || !strings.Contains(evs[0].Reason, "6 時間以上居る") {
		t.Errorf("持たれたままの子の上に起こした / 知らせない: calls=%d %v", calls.Load(), kinds(evs))
	}
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 0 {
		t.Errorf("同じ知らせを重ねた: %v", kinds(evs))
	}
	if rec, _ := store.LoadSchedule(d.Dir); !rec["job"].Start.Equal(start) {
		t.Errorf("置き去りの子の記録を上書きした: %+v", rec["job"])
	}
}

// 自分が起こした予定 (始めた時刻が一致) は lock で締めない (結果を記録に書けなかった回を「待てなかった」に上書きしない)。
func TestTickScheduleDoesNotSettleOwnRun(t *testing.T) {
	d, calls, now := schedFixture(t, 0, "")
	d.schedStart = *now
	if err := store.SaveScheduleRun(d.Dir, "job", store.ScheduleRun{Start: *now, RC: -1}); err != nil {
		t.Fatal(err)
	}
	if evs := d.tickSchedule(context.Background(), now.Add(time.Minute)); len(evs) != 0 || calls.Load() != 0 {
		t.Errorf("自分の予定を締めた / 起こし直した: %v", kinds(evs))
	}
	if rec, _ := store.LoadSchedule(d.Dir); !rec["job"].End.IsZero() {
		t.Errorf("自分の予定の記録を書き換えた: %+v", rec["job"])
	}
}

// 別の実行と重なった (ExitLocked) が 2 回続いたら失敗として出す (止まったままの実行が lock を握っていると、毎日「重なった」で片付けが起きない)。
func TestTickScheduleLockedStreak(t *testing.T) {
	d, _, now := schedFixture(t, schedule.ExitLocked, "別の pro-con worktree clean --yes が動いている")
	d.tickSchedule(context.Background(), *now)
	waitSchedule(t, d)
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 1 || evs[0].Kind != eventlog.KindSchedule {
		t.Fatalf("1 回目 = %v", kinds(evs))
	}
	*now = time.Date(2026, 9, 28, 4, 0, 0, 0, time.Local)
	d.tickSchedule(context.Background(), *now)
	waitSchedule(t, d)
	evs := d.tickSchedule(context.Background(), *now)
	rec, _ := store.LoadSchedule(d.Dir)
	if len(evs) != 1 || evs[0].Kind != eventlog.KindError || !strings.Contains(evs[0].Reason, "2 回続けて") || rec["job"].Locked != 2 || !rec["job"].Failed() {
		t.Errorf("2 回目 = %v / %+v", kinds(evs), rec["job"])
	}
}

// 待てなかった予定は、子が結果の行まで出していれば失敗にしない (出来事も error にしない)。
func TestSettleOrphanWithResultIsNotFailure(t *testing.T) {
	d, _, now := schedFixture(t, 0, "")
	start := now.Add(-time.Minute)
	if err := store.SaveScheduleRun(d.Dir, "job", store.ScheduleRun{Start: start, RC: -1}); err != nil {
		t.Fatal(err)
	}
	out := store.ScheduleOutPath(d.Dir, "job")
	_ = os.MkdirAll(filepath.Dir(out), 0o700)
	if err := os.WriteFile(out, []byte(schedule.ResultLine("worktree 消した 1", 0)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveScheduleRun(d.Dir, "job", store.ScheduleRun{Start: start, RC: -1, Locked: 1}); err != nil {
		t.Fatal(err)
	}
	evs := d.tickSchedule(context.Background(), *now)
	rec, _ := store.LoadSchedule(d.Dir)
	if len(evs) != 1 || evs[0].Kind != eventlog.KindSchedule || rec["job"].RC != schedule.RCUnknown || rec["job"].Failed() || rec["job"].Locked != 0 {
		t.Errorf("%v / %+v", kinds(evs), rec["job"])
	}
	// 子が失敗の結果の行を出していたら、待てなかった回でも失敗として出す
	if err := os.WriteFile(out, []byte(schedule.ResultLine("worktree 消した 0・失敗 2", 1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveScheduleRun(d.Dir, "job", store.ScheduleRun{Start: start, RC: -1}); err != nil {
		t.Fatal(err)
	}
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 1 || evs[0].Kind != eventlog.KindError {
		t.Errorf("失敗の結果の行を失敗にしない: %v", kinds(evs))
	}
}

// 自分が起こした子が戻らないまま 6 時間たったら知らせる (1 度だけ)。
func TestTickScheduleOwnChildStuck(t *testing.T) {
	d, _, now := schedFixture(t, 0, "")
	release := make(chan struct{})
	defer close(release)
	d.RunScheduled = func(context.Context, schedule.Job) (int, string) { <-release; return 0, "" }
	d.tickSchedule(context.Background(), *now)
	if evs := d.tickSchedule(context.Background(), now.Add(time.Hour)); len(evs) != 0 {
		t.Errorf("1 時間で知らせた: %v", kinds(evs))
	}
	evs := d.tickSchedule(context.Background(), now.Add(7*time.Hour))
	if len(evs) != 1 || evs[0].Kind != eventlog.KindError || !strings.Contains(evs[0].Reason, "戻らない") {
		t.Errorf("戻らない子を知らせない: %v", kinds(evs))
	}
	if evs := d.tickSchedule(context.Background(), now.Add(8*time.Hour)); len(evs) != 0 {
		t.Errorf("同じ知らせを重ねた: %v", kinds(evs))
	}
}

// 回せない理由は重ねないが、一度直ってから同じ理由で再発したら、また出す。
func TestScheduleErrRefiresAfterRecovery(t *testing.T) {
	d, _, now := schedFixture(t, 0, "")
	d.settings.ScheduleOff = true // 回さずに記録だけ読む
	broken := filepath.Join(d.Dir, store.ScheduleFile)
	write := func(b string) {
		if err := os.WriteFile(broken, []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("{broken")
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 1 {
		t.Fatalf("1 回目 = %v", kinds(evs))
	}
	write("{}")
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 0 {
		t.Fatalf("直った Tick = %v", kinds(evs))
	}
	write("{broken")
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 1 {
		t.Errorf("再発を知らせない: %v", kinds(evs))
	}
}

// 1 つの Tick に回せない理由が 2 つあっても、それぞれ 1 度だけ出す (前の Tick に出した理由は重ねない)。
func TestScheduleErrTwoReasons(t *testing.T) {
	d, _, now := schedFixture(t, 0, "")
	start := now.Add(-7 * time.Hour)
	if err := store.SaveScheduleRun(d.Dir, "job", store.ScheduleRun{Start: start, RC: -1}); err != nil {
		t.Fatal(err)
	}
	held, err := lockAs(d.Dir, "job.lock", errScheduleRunning, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if err := os.WriteFile(filepath.Join(d.Dir, store.SettingsFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.loadSettings()
	if evs := d.tickSchedule(context.Background(), *now); len(evs) != 2 {
		t.Fatalf("1 回目は理由 2 つ: %v", kinds(evs))
	}
	if evs := d.tickSchedule(context.Background(), now.Add(time.Second)); len(evs) != 0 {
		t.Errorf("2 回目に重ねた: %v", kinds(evs))
	}
}
