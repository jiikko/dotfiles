package main

// pro-con supervise — 画面が起こすワンショットの supervisor (issue 506。内部用)。dispatcher を子として起こし、落ちたら間を空けて
// 起こし直し、落ち続けたら諦めて PG を止める。dispatcher が自分の判断で抜けたら (rc=0) 一緒に抜ける (常駐しない = foreman の形)。
// 起こし直しの仕組みは procsup、止める条件 (人が止めた印・画面の quit / --stop・持ち主の画面) はここに置く。
//
// 子の終わり方:
//   - rc=0: dispatcher が止める判断をした (止める印 = 最後の持ち主の画面の quit / --stop・画面が無い状態が続いた・信号・人が止めた印) → 一緒に抜ける
//   - rc=exitLockHeld: 別の dispatcher が lock を持っている (kill -9 された前の supervisor の dispatcher・手で起動した dispatcher) → 数えずに間を空けて起こし直す
//   - それ以外 (Tick の失敗・panic・kill -9): 落ちた → 間を空けて起こし直す。supCrashWindow の間に supCrashLimit を超えたら諦める
//
// 起こし直す前に見る止める条件 (dispatcher が居ない間は dispatcher が決められないので、ここで決める):
//   - 人が止めた印 (store.Held) → 起こさずに抜ける (PG は --stop が止める)
//   - 落ちた後に止め終えた (stop-result が落ちた時刻より新しい = 起こし直しの待ちの間に最後の持ち主の画面が quit した) → 起こさずに抜ける
//   - 持ち主の画面が無い → PG を止めて抜ける (画面が起こした dispatcher が画面の無いときに抜ける形と同じ。join は起こさない = issue 481)
//
// 諦めたとき: 人が止めた印を置いてから PG を止める (起こし直し続けて枠を使わない。印があるので画面の keeper も起こさない。
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
	"procsup"
)

// superviseCmd は supervisor の内部用のサブコマンド (spawnSupervisor が spawnDetached を挟んで起こす)。
const superviseCmd = "supervise"

// supervisor は dispatcher の見張り方。
type supervisor struct {
	dir     string
	command func() (*exec.Cmd, error)   // dispatcher を 1 回ぶん作る
	stop    func() error                // PG を止める (dispatcher --stop --from-screen。人が止めた印は置かない)
	out     io.Writer                   // 時刻つきの 1 行 (dispatcher.log)
	submit  func(r store.Request) error // 出来事を受付の箱に置く (次の dispatcher が events.jsonl に書く)
	now     func() time.Time

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
	s := supervisor{
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

		restartWait: supRestartWait, retryWait: supRetryWait, crashLimit: supCrashLimit, crashWindow: supCrashWindow, stopWait: supStopWait,
	}
	s.say(fmt.Sprintf("supervisor が dispatcher を起こす (pid %d)", os.Getpid()))
	s.run(ctx)
	return 0
}

// run は dispatcher を見張り、見張りを終えたら後始末 (諦めた・持ち主の画面が無いなら PG を止める) をして戻る。
func (s supervisor) run(ctx context.Context) procsup.Result {
	since := s.now() // 最後に落ちた時刻 (まだ落ちていなければ起動した時刻)。これより後に止め終えていたら起こし直さない
	var halt string  // 起こし直さなかった理由
	stopPGs := false // 起こし直さずに抜けるとき、PG を止めるか
	res := procsup.Run(ctx, procsup.Spec{
		Command: s.command,
		Classify: func(err error) procsup.Outcome {
			switch procsup.ExitCode(err) {
			case 0:
				return procsup.Done
			case exitLockHeld:
				return procsup.Retry
			}
			since = s.now()
			return procsup.Crash
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
	case procsup.ReasonGaveUp, procsup.ReasonStartFailed:
		if err := store.Hold(s.dir, s.now()); err != nil {
			s.say("人が止めた印を置けない (開いている画面が supervisor を起こし直しうる): " + err.Error())
		}
		s.stopPGs("dispatcher を戻せないので、人が止めた印を置いて PG を止める (画面の c か、手で pro-con dispatcher を起動すると外れる)")
	case procsup.ReasonHalted:
		if stopPGs {
			s.stopPGs(halt + "。PG を止めて抜ける")
		} else {
			s.say(halt + "。supervisor も抜ける")
		}
	case procsup.ReasonDone, procsup.ReasonStopped:
		_, _ = fmt.Fprintf(s.out, "%s supervisor: dispatcher が抜けた (%s) ので、supervisor も抜ける\n", s.now().Format("15:04:05"), exitText(res.Err))
	}
	return res
}

// haltReason は、dispatcher を起こし直さない理由 (空なら起こし直す) と、そのとき PG を止めるか。
func (s supervisor) haltReason(since time.Time) (string, bool) {
	if store.Held(s.dir) {
		return "人が止めた印 (dispatcher --stop) があるので、dispatcher を起こし直さない", false
	}
	if st, err := os.Stat(filepath.Join(s.dir, dispatcher.StopResultFile)); err == nil && st.ModTime().After(since) {
		return "dispatcher が抜けた後に止め終えた (画面の quit か dispatcher --stop) ので、dispatcher を起こし直さない", false
	}
	if !ownersOpen(s.dir) {
		return "開いている持ち主の画面が無いので、dispatcher を起こし直さない", true
	}
	return "", false
}

// stopPGs は出来事にしてから PG を止める (止めきれなくても抜ける。残りは dispatcher.log と stop-result)。
// 止めるのは別のプロセス (dispatcher --stop) なので、supervisor が信号で抜ける途中でも続く。
func (s supervisor) stopPGs(why string) {
	s.say(why)
	if err := s.stop(); err != nil {
		s.say("PG を止めきれなかった: " + err.Error())
	}
}

// event は procsup の出来事を supervisor の出来事の文にする。
func (s supervisor) event(e procsup.Event) {
	switch e.Kind {
	case procsup.EventCrashed:
		s.say(fmt.Sprintf("dispatcher が落ちた (%s。%s の間に %d 回目)。%s 後に起こし直す", exitText(e.Err), s.crashWindow, e.Crashes, e.Wait))
	case procsup.EventRetrying:
		if !e.Repeat {
			s.say(fmt.Sprintf("別の dispatcher が動いているので、%s ごとに起こし直す (その dispatcher が抜けたら引き継ぐ)", e.Wait))
		}
	case procsup.EventGaveUp:
		s.say(fmt.Sprintf("dispatcher が %s の間に %d 回落ちたので、起こし直さない (最後: %s)", s.crashWindow, e.Crashes, exitText(e.Err)))
	case procsup.EventStartFailed:
		s.say("dispatcher を起こせない: " + e.Err.Error())
	}
}

// say は出来事を dispatcher.log へ時刻つきで出し、受付の箱に置く (events.jsonl の書き手は dispatcher だけ。次の dispatcher が書く)。
func (s supervisor) say(text string) {
	now := s.now()
	_, _ = fmt.Fprintf(s.out, "%s supervisor: %s\n", now.Format("15:04:05"), text)
	if err := s.submit(store.Request{Kind: store.KindSupervisor, Note: text, At: now}); err != nil {
		_, _ = fmt.Fprintf(s.out, "%s supervisor: 出来事を受付の箱に置けない: %v\n", now.Format("15:04:05"), err)
	}
}
