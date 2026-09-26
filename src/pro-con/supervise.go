package main

// pro-con supervise — 画面が起こすワンショットの supervisor (issue 506。内部用)。dispatcher を子として起こし、落ちたら間を空けて
// 起こし直し、落ち続けたら諦めて PG を止める。dispatcher が自分の判断で抜けたら (rc=0) 一緒に抜ける (常駐しない = foreman の形)。
// 起こし直しの仕組みは process_supervisor、止める条件 (人が止めた印・画面の quit / --stop・持ち主の画面) はここに置く。
//
// 子の終わり方:
//   - rc=0: dispatcher が止める判断をした (止める印 = 最後の持ち主の画面の quit / --stop・画面が無い状態が続いた・信号・人が止めた印) → 一緒に抜ける
//   - rc=exitLockHeld: 別の dispatcher が lock を持っている (kill -9 された前の supervisor の dispatcher・手で起動した dispatcher・--stop が止めている最中)
//     → 落ちたと数えない。起こし直す前の確かめで lock がまだ持たれていれば、その dispatcher に任せて抜ける (下)
//   - それ以外 (Tick の失敗・panic・kill -9): 落ちた → 間を空けて起こし直す。supCrashWindow の間に supCrashLimit を超えたら諦める
//
// 起こし直す前に見る止める条件 (dispatcher が居ない間は dispatcher が決められないので、ここで決める):
//   - 人が止めた印 (store.Held) → 起こさずに抜ける (PG は --stop が止める)
//   - 落ちた後に止め終えた (stop-result が落ちた時刻より新しい = 起こし直しの待ちの間に最後の持ち主の画面が quit した) → 起こさずに抜ける
//   - 別の dispatcher が lock を持っている → 起こさずに抜ける (その dispatcher が抜けたら画面の keeper が supervisor を起こす)。
//     🚨 起こし直し続けない: dispatcher は lock を取る前に claude --version を引くので、10 秒ごとに起こすと手で起動した dispatcher が動く間ずっと続く
//   - 持ち主の画面が ownerGrace の間ずっと無い → PG を止めて抜ける (画面が起こした dispatcher が画面の無いときに抜ける形と同じ。join は起こさない = issue 481)。
//     一瞬で決めない: 画面の ctrl+r (exec) の間は、presence の flock が外れて持ち主が 0 に見える
//
// 起こせなかった (実行ファイルが消えた・fork の失敗) ときは、人が止めた印を置かずに抜ける (印は置き場で共有なので、別のバイナリで開いた
// 画面の keeper まで止める。次の keeper が起こし直す = 前の形と同じ)。
//
// 落ち続けて諦めたとき: 人が止めた印を置いてから PG を止める (起こし直し続けて枠を使わない。印があるので画面の keeper も起こさない。
// 画面の c か、手で pro-con dispatcher を起動すると外れる)。
//
// 🚨 kill -9 で supervisor が死んだときに dispatcher と PG が残るのは受け入れる (ユーザーの決定)。dispatcher は自分の決まり
// (画面が無い状態が 1 分続いたら抜ける) で動き続け、次の画面の keeper は dispatcher が抜けた後に supervisor を起こす。
// 🚨 supervisor 自身は新版へ入れ替わらない (dispatcher は 505 で自分で exec する。同じ PID なので supervisor からは同じ子のまま)。
// 起こし直す dispatcher は os.Executable のパスなので、shim が差し替えた新しいビルドになる。

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"pro-con/dispatcher"
	"pro-con/store"
	supervisor "process_supervisor"
)

// superviseCmd は supervisor の内部用のサブコマンド (spawnSupervisor が spawnDetached を挟んで起こす)。
const superviseCmd = "supervise"

// dispatcherSupervisor は dispatcher の見張り方。
type dispatcherSupervisor struct {
	dir     string
	command func() (*exec.Cmd, error)   // dispatcher を 1 回ぶん作る
	stop    func() error                // PG を止める (dispatcher --stop --from-screen。人が止めた印は置かない)
	out     io.Writer                   // 時刻つきの 1 行 (dispatcher.log)
	submit  func(r store.Request) error // 出来事を受付の箱に置く (次の dispatcher が events.jsonl に書く)
	now     func() time.Time

	ownerGrace                                    time.Duration // 持ち主の画面が無いと決めるまで待つ (ctrl+r の隙間を無いと数えない)
	restartWait, retryWait, crashWindow, stopWait time.Duration
	crashLimit                                    int
}

// supervisor の既定の起こし直し方。落ちてからの間は画面の keeper の起こし直し (10 秒) と同じ。
// 止めるときの待ちは長く取る: SIGTERM を受けた dispatcher は、画面が無ければ PG を止めてから抜ける (claude stop を PG ごとに呼ぶ)
const (
	supRestartWait = 10 * time.Second
	supRetryWait   = 10 * time.Second
	supCrashLimit  = 5
	supCrashWindow = 10 * time.Minute
	supStopWait    = 3 * time.Minute
)

func runSupervise(args []string, dir string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con supervise", flag.ContinueOnError)
	fs.SetOutput(stderr)
	e2eRoot := fs.String("e2e", "", "e2e モードの置き場 (dispatcher にもそのまま渡す)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var extra []string // dispatcher に渡す引数
	if *e2eRoot != "" {
		e := dispatcher.E2E{Root: *e2eRoot}
		dir, extra = e.StateDir(), []string{"--e2e", *e2eRoot}
	}
	unlock, err := dispatcher.LockSupervisor(dir)
	if errors.Is(err, dispatcher.ErrSupervisorRunning) {
		return 0 // 別の画面が先に起こした
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con supervise:", err)
		return 1
	}
	defer unlock()
	exe, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con supervise:", err)
		return 1
	}
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stopSignals()
	s := dispatcherSupervisor{
		dir: dir,
		command: func() (*exec.Cmd, error) {
			cmd := dispatcherCmd(exe, extra)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr         // dispatcher.log (spawnSupervisor が渡した)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // 前と同じく dispatcher は自分のプロセスグループ (見張り・テストの係はその中)
			return cmd, nil
		},
		stop: func() error {
			cmd := stopCmd(exe, extra)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			return cmd.Run()
		},
		out:    stdout,
		submit: func(r store.Request) error { _, err := store.Submit(dir, r); return err },
		now:    time.Now,

		ownerGrace: signalGrace, restartWait: supRestartWait, retryWait: supRetryWait, crashLimit: supCrashLimit, crashWindow: supCrashWindow, stopWait: supStopWait,
	}
	s.say(fmt.Sprintf("supervisor が dispatcher を起こす (pid %d)", os.Getpid()))
	s.run(ctx)
	return 0
}

// run は dispatcher を見張り、見張りを終えたら後始末 (諦めた・持ち主の画面が無いなら PG を止める) をして戻る。
func (s dispatcherSupervisor) run(ctx context.Context) supervisor.Result {
	since := s.now() // 最後に落ちた時刻 (まだ落ちていなければ起動した時刻)。これより後に止め終えていたら起こし直さない
	var halt string  // 起こし直さなかった理由
	stopPGs := false // 起こし直さずに抜けるとき、PG を止めるか
	res := supervisor.Run(ctx, supervisor.Spec{
		Command: s.command,
		Classify: func(err error) supervisor.Outcome {
			switch supervisor.ExitCode(err) {
			case 0:
				return supervisor.Done
			case exitLockHeld:
				return supervisor.Retry
			}
			since = s.now()
			return supervisor.Crash
		},
		Continue: func() bool {
			halt, stopPGs = s.haltReason(since)
			return halt == ""
		},
		OnEvent:     s.event,
		RestartWait: s.restartWait, RetryWait: s.retryWait,
		CrashLimit: s.crashLimit, CrashWindow: s.crashWindow, StopWait: s.stopWait,
		Now: s.now,
	})
	switch res.Reason {
	case supervisor.ReasonStartFailed:
		s.say("supervisor も抜ける (人が止めた印は置かない。画面が開いていれば、その keeper が起こし直す)")
	case supervisor.ReasonGaveUp:
		if err := store.Hold(s.dir, s.now()); err != nil {
			s.say("人が止めた印を置けない (開いている画面が supervisor を起こし直しうる): " + err.Error())
		}
		s.stopPGs("dispatcher を戻せないので、人が止めた印を置いて PG を止める (画面の c か、手で pro-con dispatcher を起動すると外れる)")
	case supervisor.ReasonHalted:
		if stopPGs {
			s.stopPGs(halt + "。PG を止めて抜ける")
		} else {
			s.say(halt + "。supervisor も抜ける")
		}
	case supervisor.ReasonDone:
		s.say(fmt.Sprintf("dispatcher が抜けた (%s) ので、supervisor も抜ける", exitText(res.Err)))
	case supervisor.ReasonStopped:
		s.say(fmt.Sprintf("止める合図を受けたので、dispatcher を止めて抜ける (dispatcher は %s)", exitText(res.Err)))
	}
	return res
}

// haltReason は、dispatcher を起こし直さない理由 (空なら起こし直す) と、そのとき PG を止めるか。
func (s dispatcherSupervisor) haltReason(since time.Time) (string, bool) {
	if store.Held(s.dir) {
		return "人が止めた印 (dispatcher --stop) があるので、dispatcher を起こし直さない", false
	}
	if st, err := os.Stat(filepath.Join(s.dir, dispatcher.StopResultFile)); err == nil && st.ModTime().After(since) {
		return "dispatcher が抜けた後に止め終えた (画面の quit か dispatcher --stop) ので、dispatcher を起こし直さない", false
	}
	if unlock, err := dispatcher.Lock(s.dir); errors.Is(err, dispatcher.ErrRunning) {
		return "別の dispatcher が動いているので、それに任せて dispatcher を起こさない (抜けたら画面が supervisor を起こし直す)", false
	} else if err == nil {
		unlock()
	}
	if !ownersAppear(s.dir, s.ownerGrace) {
		return "開いている持ち主の画面が無いので、dispatcher を起こし直さない", true
	}
	return "", false
}

// ownersAppear は、grace の間に 1 度でも持ち主の画面が開いているのを見たか (見たらすぐ真を返す)。
func ownersAppear(dir string, grace time.Duration) bool {
	deadline := time.Now().Add(grace)
	for {
		if ownersOpen(dir) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stopPGs は出来事にしてから PG を止める (止めきれなくても抜ける。残りは dispatcher.log と stop-result)。
// 止めるのは別のプロセス (dispatcher --stop) なので、supervisor が信号で抜ける途中でも続く。
func (s dispatcherSupervisor) stopPGs(why string) {
	s.say(why)
	if err := s.stop(); err != nil {
		s.say("PG を止めきれなかった: " + err.Error())
	}
}

// event は process_supervisor の出来事を supervisor の出来事の文にする。
func (s dispatcherSupervisor) event(e supervisor.Event) {
	switch e.Kind {
	case supervisor.EventCrashed:
		s.say(fmt.Sprintf("dispatcher が落ちた (%s。%s の間に %d 回目)。%s 後に起こし直す", exitText(e.Err), s.crashWindow, e.Crashes, e.Wait))
	case supervisor.EventRetrying: // 起こし直す前の確かめ (haltReason) が、lock がまだ持たれていれば理由を書いて抜ける
	case supervisor.EventGaveUp:
		s.say(fmt.Sprintf("dispatcher が %s の間に %d 回落ちたので、起こし直さない (最後: %s)", s.crashWindow, e.Crashes, exitText(e.Err)))
	case supervisor.EventStartFailed:
		s.say("dispatcher を起こせない: " + e.Err.Error())
	}
}

// say は出来事を dispatcher.log へ時刻つきで出し、受付の箱に置く (events.jsonl の書き手は dispatcher だけ。次の dispatcher が書く)。
func (s dispatcherSupervisor) say(text string) {
	now := s.now()
	_, _ = fmt.Fprintf(s.out, "%s supervisor: %s\n", now.Format("15:04:05"), text)
	if err := s.submit(store.Request{Kind: store.KindSupervisor, Note: text, At: now}); err != nil {
		_, _ = fmt.Fprintf(s.out, "%s supervisor: 出来事を受付の箱に置けない: %v\n", now.Format("15:04:05"), err)
	}
}
