package main

// dispatcher が見張り (pro-con monitor。issue 475) を子として起こし、落ちたら起こし直し、抜けるときに止める。
// 見張りが落ちても dispatcher と PG は止めない (見張りは知らせるだけ。落ち続けたら起こし直すのをやめて出来事にする)。
//
// 失敗モード:
//   - dispatcher が kill -9 で死ぬ → 見張りの stdin は dispatcher が握るパイプなので、OS が閉じて見張りはすぐ抜ける (--until-stdin-closes)
//   - 前の見張りがまだ抜けていない間に次の dispatcher が起こす → lock が取れない見張りは monitorExitHeld で抜ける。落ちたと数えず、間を空けて起こし直す
//   - 止めている最中 (ctx の取り消し = SIGTERM / SIGHUP。同じプロセスグループの見張りも同じ信号で死ぬ) → 起こし直さず、死を数えない
// 🚨 lock のファイル (dispatcher.lock) を子へ渡さない (ExtraFiles に入れない。Go の開くファイルは CLOEXEC なので exec では引き継がれない)。
// 渡すと、dispatcher が死んだ後も見張りが dispatcher の lock を握り、次の dispatcher が起動できない

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"time"
)

// monitorCommand は見張りのコマンド (stdin は superviseMonitor が付ける)。
// 🚨 runDispatcher を e2e / --once 以外で呼ぶテストを足すなら、これを差し替える (既定のまま go test から呼ぶと、テストのバイナリを monitor の引数で起こす)。
// 今の runDispatcher のテストはすべて --e2e か --once なので見張りを起こさない。
var monitorCommand = func(stdout, stderr io.Writer) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "monitor", "--"+untilStdinClosesFlag)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd, nil
}

// monitorSup は見張りの起こし直し方。
type monitorSup struct {
	command     func() (*exec.Cmd, error)
	say         func(text string) // 出来事にする (並行に呼ばれてよいこと)
	now         func() time.Time
	restartWait time.Duration // 落ちてから起こし直すまで
	heldWait    time.Duration // 前の見張りが lock を持っていたときに、起こし直すまで
	crashLimit  int           // crashWindow の間に落ちてよい回数 (超えたら起こし直さない)
	crashWindow time.Duration
	stopWait    time.Duration // 止めるとき、SIGTERM から kill までの待ち
}

// defaultMonitorSup は本物の dispatcher の起こし直し方。落ちた回数の窓は PG の crash の窓と同じ長さ。
func defaultMonitorSup(say func(string), stdout, stderr io.Writer) monitorSup {
	return monitorSup{
		command: func() (*exec.Cmd, error) { return monitorCommand(stdout, stderr) },
		say:     say, now: time.Now,
		restartWait: 10 * time.Second, heldWait: 30 * time.Second,
		crashLimit: 3, crashWindow: 30 * time.Minute, stopWait: 5 * time.Second,
	}
}

// superviseMonitor は見張りを裏で起こし続ける。返した関数は、見張りを止めて終わるまで待つ (ctx の取り消しでも止まる)。
func superviseMonitor(ctx context.Context, s monitorSup) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var crashes []time.Time
		heldSaid := false
		for ctx.Err() == nil {
			err, held, ok := s.runOnce(ctx)
			if !ok || ctx.Err() != nil { // 起こせなかった (出来事にした) / 止める途中に抜けた
				return
			}
			if held {
				if !heldSaid { // 前の見張りが抜けるまで続きうる。30 秒ごとに書かない
					heldSaid = true
					s.say(fmt.Sprintf("見張りは前の見張りが lock を持っているので起きなかった。%s ごとに起こし直す", s.heldWait))
				}
				sleepCtx(ctx, s.heldWait)
				continue
			}
			heldSaid = false
			now := s.now()
			crashes = append(slices.DeleteFunc(crashes, func(at time.Time) bool { return now.Sub(at) > s.crashWindow }), now)
			if len(crashes) > s.crashLimit {
				s.say(fmt.Sprintf("見張りが %s の間に %d 回抜けたので、起こし直さない (最後: %v)。dispatcher と PG は動き続ける。dispatcher を起動し直すと、もう一度起こす", s.crashWindow, len(crashes), err))
				return
			}
			s.say(fmt.Sprintf("見張りが抜けた (%v)。%s 後に起こし直す", err, s.restartWait))
			sleepCtx(ctx, s.restartWait)
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// runOnce は見張りを 1 回起こして、抜けるか ctx が終わるまで待つ。held は lock を他が持っていて抜けた、ok が偽なら起こせなかった
// (実行ファイルが無い等。起こし直しても直らないので、出来事にして起こし直さない)。
func (s monitorSup) runOnce(ctx context.Context) (err error, held, ok bool) {
	cmd, err := s.command()
	if err != nil {
		s.say("見張りを起こせない: " + err.Error())
		return err, false, false
	}
	r, w, err := os.Pipe()
	if err != nil {
		s.say("見張りを起こせない (パイプを作れない): " + err.Error())
		return err, false, false
	}
	cmd.Stdin = r
	if err := cmd.Start(); err != nil {
		_ = r.Close()
		_ = w.Close()
		s.say("見張りを起こせない: " + err.Error())
		return err, false, false
	}
	_ = r.Close() // 読む側は子だけが持つ (親が持つと、書く側を閉じても EOF にならない)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = w.Close() // EOF で抜ける
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(s.stopWait):
			_ = cmd.Process.Kill()
			<-exited
		}
		return nil, false, true
	case err = <-exited:
		_ = w.Close()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == monitorExitHeld {
		return err, true, true
	}
	if err == nil {
		err = errors.New("rc=0") // 見張りは stdin が閉じるか信号でしか抜けない。自分で抜けたのは落ちたのと同じに扱う
	}
	return err, false, true
}

// sleepCtx は d だけ待つ (ctx が終われば先に戻る)。
func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
