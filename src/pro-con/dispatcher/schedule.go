package dispatcher

// 予定 (package schedule。issue 550): 決まった時刻に、表 (schedule.Jobs) の argv どおりに pro-con を子として起こす。
// 中で回す行 (schedule.Job.Internal。issue 497 の書庫の削除) は子を起こさず、この Tick の中で回して同じ記録に書く (runInternal)。
//
// 起こす前に始めた時刻を記録 (store.ScheduleFile) に書く (途中で落ちて起き直しても、同じ枠で 2 回回さない)。記録を書けない・読めないなら回さない。
// 子は裏の goroutine で待ち、結果は次の Tick で記録と出来事にする (出来事と記録の書き手を Tick の goroutine だけにする)。
// 🚨 子の stdout / stderr はファイルへ直接書かせる (パイプにすると、dispatcher が先に抜けたとき子が SIGPIPE で片付けの途中で死ぬ)。
// 子は自分のプロセスグループで動かす (dispatcher のグループへの信号で、git worktree remove の途中で殺さない)。

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"pro-con/eventlog"
	"pro-con/schedule"
	"pro-con/store"
)

// ScheduleRunner は予定の 1 行を起こして終わるまで待ち、終了コードと結果の 1 行 (無ければ失敗の理由) を返す。起こせなかったら rc -1。
type ScheduleRunner func(ctx context.Context, job schedule.Job) (rc int, note string)

// scheduleWaitLimit は、入れ替え (505) が予定の子の終わりを待つ上限。🚨 子は殺さない (worktree remove と branch の削除の間で殺すと、
// ブランチだけが残って二度と片付かない)。過ぎたら待たずに入れ替え、終わりは次の dispatcher が lock で知る (settleOrphan)。
const scheduleWaitLimit = 30 * time.Minute

// scheduleStuckAfter は、待てなかった予定の子が lock を持ち続けていたら error にするまでの長さ (worktree clean は実物で数十秒)。
const scheduleStuckAfter = 6 * time.Hour

// scheduleDone は終わった予定の結果 (裏の goroutine が置き、次の Tick が記録と出来事にする)。
type scheduleDone struct {
	job schedule.Job
	run store.ScheduleRun
}

// tickSchedule は終わった予定を記録し、回す時刻を過ぎた予定があれば 1 つだけ起こす (同時に走らせるのは 1 つまで)。
func (d *Dispatcher) tickSchedule(ctx context.Context, now time.Time) []eventlog.Event {
	if len(d.Scheduled) == 0 {
		return nil
	}
	d.schedErrsNow = map[string]bool{}
	defer func() { d.schedErrs = d.schedErrsNow }() // この Tick に出なかった理由は外す (一度直って同じ理由で再発したら、また出す)
	// 🚨 busy を先に見る: goroutine は結果を置いてから busy を下ろすので、下りていれば結果は必ず置かれている
	// (逆の順に読むと、2 手の間に終わった自分の予定を「待てなかった」と読む)
	// 子を待っている間 (busy) は、子の行を新しく起こさず、中で回す行は進める
	// (子が戻らなくても 1 週間の削除は止めない。以前の purge も子と関係なく回っていた = 子と同時に走るのは今までどおり。issue 497)
	var notes []eventlog.Event
	busy := d.schedBusy.Load()
	if busy {
		if now.Sub(d.schedStart) > scheduleStuckAfter { // 子が戻らない (git・claude rm が止まった等)。殺さずに知らせる
			notes = d.scheduleErr(fmt.Sprintf("予定の子が %s に起こしてから戻らない (終わるまで子の予定を回さない。ps で worktree clean を見る)", d.schedStart.Local().Format("01-02 15:04")))
		}
	} else if r := d.schedDone.Swap(nil); r != nil {
		notes = append(notes, d.finishScheduled(*r)...)
	}
	rec, err := store.LoadSchedule(d.Dir)
	if err != nil {
		return append(notes, d.scheduleErr("予定の記録を読めない (読めるまで予定を回さない): "+err.Error())...)
	}
	running := map[string]bool{} // 前の dispatcher が置き去りにした子がまだ lock を持っている予定 (新しく起こさない)
	for _, j := range d.Scheduled {
		// 自分が起こした予定の終わりは goroutine から受ける (結果を記録に書けなかった回を、lock で「待てなかった」に上書きしない)
		if r := rec[j.Name]; !r.Start.IsZero() && r.End.IsZero() && !r.Start.Equal(d.schedStart) {
			held, evs := d.settleOrphan(j, r, now)
			running[j.Name], notes = held, append(notes, evs...)
		}
	}
	if d.settingsBroken { // schedule off を読めないまま破壊的な予定を回さない (判定できないときは回さない側)
		return append(notes, d.scheduleErr("設定を読めないので予定を回さない (止めたかどうか分からない): "+d.settingsErr)...)
	}
	if d.settings.ScheduleOff { // 人が止めた (pro-con config set schedule off)。走り終えたものの記録は上で締める
		return notes
	}
	for _, j := range d.Scheduled {
		prev := rec[j.Name]
		if running[j.Name] || !j.Due(prev.Start, now) || busy && j.Internal == "" {
			continue
		}
		if j.Internal == "" && d.RunScheduled == nil { // 子を起こす口が無い (起動時に理由を出した。中で回す行だけは回す)
			continue
		}
		start := store.ScheduleRun{Start: now, RC: -1, Locked: prev.Locked}
		if err := store.SaveScheduleRun(d.Dir, j.Name, start); err != nil {
			return append(notes, d.scheduleErr("予定の記録を書けない (書けるまで予定を回さない): "+err.Error())...)
		}
		if j.Internal != "" { // 子を起こさず、この Tick の中で回す (始めた記録は上で書いた = 途中で落ちても同じ枠で 2 回回さない)
			rc, note, evs := d.runInternal(j, now)
			notes = append(notes, evs...)
			return append(notes, d.finishScheduled(scheduleDone{job: j, run: store.ScheduleRun{Start: now, End: d.Now(), RC: rc, Note: note}})...)
		}
		d.schedBusy.Store(true) // 🚨 goroutine を起こす前に立てる (Tick の後の入れ替えが、子を起こす前の隙に exec しない)
		d.schedStart = now
		go func() {
			rc, note := d.RunScheduled(context.WithoutCancel(ctx), j) // dispatcher を止めても子は止めない (片付けの途中で殺さない)
			run := store.ScheduleRun{Start: start.Start, End: d.Now(), RC: rc, Note: note}
			if rc == schedule.ExitLocked {
				run.Locked = start.Locked + 1
			}
			d.schedDone.Store(&scheduleDone{job: j, run: run})
			d.schedBusy.Store(false)
		}()
		return append(notes, ev(eventlog.KindSchedule, "", "", fmt.Sprintf("予定 (%s) の %s を起こした", j.When(), j.Command())))
	}
	return notes
}

// settleOrphan は、始めた記録だけがあって、この dispatcher が待っていない予定 (前の dispatcher が抜けた・入れ替わった) の終わりを締める。
// 予定のコマンドが持つ lock が空いていれば終わっている: 出力のファイルから結果の行を読んで記録する (rc は分からない = schedule.RCUnknown)。
// lock を持つ者がいれば、まだ走っている (人の手の実行でも同じ) ので held を返して次の Tick を待つ。長く持たれ続けたら error を出す。
// 既知の穴: 子を起こしてから子が lock を取るまでの間 (worktree clean は lock を設定の読み込みより前に取る) に dispatcher が落ちると、
// 走っている子を「終わった」と読む (記録の文が違うだけで、何も消さず、同じ枠で回し直しもしない)。
func (d *Dispatcher) settleOrphan(j schedule.Job, r store.ScheduleRun, now time.Time) (held bool, notes []eventlog.Event) {
	if j.Internal != "" { // 中で回す行は Tick の中で締めるので、終わりの記録が無いのは回している途中で dispatcher が落ちたとき
		r.End, r.RC, r.Locked = now, schedule.RCUnknown, 0
		r.Note = "終わりの記録が無い (dispatcher の中で回している途中で dispatcher が止まったか、終わりを記録に書けなかった。どこまで進んだかは出来事を見る。次の枠で回し直す)"
		if err := store.SaveScheduleRun(d.Dir, j.Name, r); err != nil {
			return false, d.scheduleErr("予定の結果を記録に書けない: " + err.Error())
		}
		return false, []eventlog.Event{ev(eventlog.KindError, "", "", fmt.Sprintf("予定の %s: %s", j.Command(), r.Note))}
	}
	if j.Lock == "" {
		return false, nil
	}
	l, err := lockAs(d.Dir, j.Lock, errScheduleRunning, j.Name)
	if errors.Is(err, errScheduleRunning) {
		if now.Sub(r.Start) > scheduleStuckAfter {
			return true, d.scheduleErr(fmt.Sprintf("予定の %s を %s に起こした後、%s を持つ実行が 6 時間以上居る (止まった子か人の手の実行。中の pid を見る)。lock が空くまで次を起こさない",
				j.Command(), r.Start.Local().Format("01-02 15:04"), filepath.Join(d.Dir, j.Lock)))
		}
		return true, nil
	}
	if err != nil {
		return true, d.scheduleErr("予定の lock を確かめられない (" + j.Name + "。確かめられるまで次を起こさない): " + err.Error())
	}
	l.Release()
	r.End, r.RC, r.Locked = now, schedule.RCUnknown, 0
	outPath := store.ScheduleOutPath(d.Dir, j.Name)
	if fi, err := os.Stat(outPath); err != nil || fi.ModTime().Before(r.Start.Truncate(time.Second)) { // 前の回の出力を今回の結果と読まない
		r.Note = "子を起こした記録が無い (始めた記録を書いた後、起こす前に dispatcher が止まった)"
	} else {
		r.Note = "dispatcher が終わりを待てなかった (rc は分からない): " + scheduleNote(outPath, store.ScheduleErrPath(d.Dir, j.Name), nil)
	}
	if err := store.SaveScheduleRun(d.Dir, j.Name, r); err != nil {
		return false, d.scheduleErr("予定の結果を記録に書けない: " + err.Error())
	}
	kind := eventlog.KindSchedule
	if r.Failed() {
		kind = eventlog.KindError
	}
	return false, []eventlog.Event{ev(kind, "", "", fmt.Sprintf("予定の %s: %s", j.Command(), r.Note))}
}

// runInternal は中で回す予定 (schedule.Job.Internal) を回し、記録に書く rc と結果の 1 行を返す。どの処理を呼ぶかは予定の名前で決まる。
func (d *Dispatcher) runInternal(j schedule.Job, now time.Time) (rc int, note string, notes []eventlog.Event) {
	if j.Name == schedule.CardPurge {
		return d.purge(now)
	}
	return 1, "dispatcher が知らない中の予定 (" + j.Name + "。schedule.Jobs と dispatcher の版がずれている)", nil
}

// errScheduleRunning は予定のコマンドがまだ lock を持っている (settleOrphan の中だけで使う)。
var errScheduleRunning = errors.New("予定のコマンドが走っている")

// scheduleBusy は入れ替えが待つべき予定があるか (始めてから scheduleWaitLimit まで。結果を記録する前も)。
func (d *Dispatcher) scheduleBusy(now time.Time) bool {
	return d.schedDone.Load() != nil || (d.schedBusy.Load() && now.Sub(d.schedStart) < scheduleWaitLimit)
}

// finishScheduled は終わった予定の結果を記録に書き、出来事にする (失敗の判定は画面と同じ store.ScheduleRun.Failed)。
func (d *Dispatcher) finishScheduled(r scheduleDone) []eventlog.Event {
	var notes []eventlog.Event
	if err := store.SaveScheduleRun(d.Dir, r.job.Name, r.run); err != nil {
		notes = append(notes, ev(eventlog.KindError, "", "", "予定の結果を記録に書けない: "+err.Error()))
	}
	cmd := r.job.Command()
	switch {
	case r.run.RC == schedule.ExitLocked && !r.run.Failed(): // 人の手の実行と重なった (向こうが片付けている)。この枠はこれで済ませる
		return append(notes, ev(eventlog.KindSchedule, "", "", fmt.Sprintf("予定の %s は別の実行と重なったので何もしなかった: %s", cmd, r.run.Note)))
	case r.run.RC == schedule.ExitLocked:
		return append(notes, ev(eventlog.KindError, "", "", fmt.Sprintf("予定の %s は %d 回続けて別の実行と重なった (止まったままの実行が lock を持っていないか): %s", cmd, r.run.Locked, r.run.Note)))
	case r.run.Failed():
		return append(notes, ev(eventlog.KindError, "", "", fmt.Sprintf("予定の %s が失敗した (rc=%d): %s", cmd, r.run.RC, r.run.Note)))
	}
	return append(notes, ev(eventlog.KindSchedule, "", "", fmt.Sprintf("予定の %s が終わった: %s", cmd, r.run.Note)))
}

// scheduleErr は予定を回せない理由を出来事にする (前の Tick にも出した理由は重ねない。1 つの Tick に理由が 2 つあっても、それぞれ 1 度だけ)。
func (d *Dispatcher) scheduleErr(why string) []eventlog.Event {
	d.schedErrsNow[why] = true
	if d.schedErrs[why] {
		return nil
	}
	return []eventlog.Event{ev(eventlog.KindError, "", "", why)}
}

// ExecScheduled は exe (動いている pro-con) を予定の Args で起こす (cwd は状態の置き場 dir = どの worktree の中でもない)。
// stdout / stderr は store.ScheduleOutPath / ScheduleErrPath へ毎回上書きする。
func ExecScheduled(exe, dir string) ScheduleRunner {
	return func(ctx context.Context, j schedule.Job) (int, string) {
		if self, err := os.Executable(); testing.Testing() && (err != nil || self == exe) { // テストの二進は argv を解さない (自分を起こすとテストを走らせ直す)
			return -1, "テストの二進で自分を予定として起こさない"
		}
		outPath, errPath := store.ScheduleOutPath(dir, j.Name), store.ScheduleErrPath(dir, j.Name)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
			return -1, "出力の置き場を作れない: " + err.Error()
		}
		stdout, err := os.Create(outPath)
		if err != nil {
			return -1, "出力のファイルを作れない: " + err.Error()
		}
		defer func() { _ = stdout.Close() }()
		stderr, err := os.Create(errPath)
		if err != nil {
			return -1, "出力のファイルを作れない: " + err.Error()
		}
		defer func() { _ = stderr.Close() }()
		cmd := exec.CommandContext(ctx, exe, j.Args...)
		cmd.Dir = dir
		cmd.Env = scheduleEnv(os.Environ())
		cmd.Stdout, cmd.Stderr = stdout, stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		runErr := cmd.Run()
		rc := -1
		if cmd.ProcessState != nil {
			rc = cmd.ProcessState.ExitCode() // 信号で死んだら -1
		}
		note := scheduleNote(outPath, errPath, runErr)
		if l := lastLine(errPath, ""); rc != 0 && l != "" && l != note { // 結果の行 (「失敗 0」等) だけでは失敗の理由が見えない
			note += " / " + l
		}
		return rc, note
	}
}

// scheduleNote は結果の 1 行 (stdout の最後の schedule.ResultPrefix の行)。無ければ stderr の最後の行か、起こせなかった理由。
func scheduleNote(outPath, errPath string, runErr error) string {
	if l := lastLine(outPath, schedule.ResultPrefix); l != "" {
		return l
	}
	if l := lastLine(errPath, ""); l != "" {
		return l
	}
	if runErr != nil {
		return runErr.Error()
	}
	return "結果の行が無い (" + outPath + ")"
}

// lastLine は path の、prefix で始まる最後の空でない行 (200 文字まで)。読めなければ空。
func lastLine(path, prefix string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	var last string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" && strings.HasPrefix(l, prefix) {
			last = l
		}
	}
	if r := []rune(last); len(r) > 200 {
		last = string(r[:200]) + "…"
	}
	return last
}

// scheduleEnv は予定の子に渡す環境 (tmux の変数と、入れ替えで引き継いだ lock の fd の番号を外す)。
func scheduleEnv(env []string) []string {
	out := withoutTmux(env)
	kept := out[:0]
	for _, e := range out {
		if !strings.HasPrefix(e, LockFDEnv+"=") {
			kept = append(kept, e)
		}
	}
	return kept
}
