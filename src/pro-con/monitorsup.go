package main

// dispatcher が見張り (pro-con monitor。issue 475) を子として起こし、落ちたら起こし直し、抜けるときに止める (起こし直しの仕組みは procsup)。
// 見張りが落ちても dispatcher と PG は止めない (見張りは知らせるだけ。落ち続けたら起こし直すのをやめて出来事にする)。
//
// 失敗モード:
//   - dispatcher が kill -9 で死ぬ → 見張りの stdin は procsup の生命線 (dispatcher が書く側を握るパイプ) なので、OS が閉じて見張りはすぐ抜ける (--until-stdin-closes)
//   - 前の見張りがまだ抜けていない間に次の dispatcher が起こす → lock が取れない見張りは exitLockHeld で抜ける。落ちたと数えず (procsup.Retry)、間を空けて起こし直す
//   - 止めている最中 (ctx の取り消し = SIGTERM / SIGHUP。同じプロセスグループの見張りも同じ信号で死ぬ) → 起こし直さず、死を数えない
// 🚨 lock のファイル (dispatcher.lock) を子へ渡さない (ExtraFiles に入れない。Go の開くファイルは CLOEXEC なので exec では引き継がれない)。
// 渡すと、dispatcher が死んだ後も見張りが dispatcher の lock を握り、次の dispatcher が起動できない

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"procsup"
)

// monitorCommand は見張りのコマンド (stdin は procsup の生命線が付ける)。
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

// monitorSpec は見張りの起こし直し方。say は出来事にする (procsup の goroutine から呼ばれる)。落ちた回数の窓は PG の crash の窓と同じ長さ。
func monitorSpec(say func(string), stdout, stderr io.Writer) procsup.Spec {
	return procsup.Spec{
		Command:  func() (*exec.Cmd, error) { return monitorCommand(stdout, stderr) },
		Lifeline: true,
		Classify: func(err error) procsup.Outcome {
			if procsup.ExitCode(err) == exitLockHeld {
				return procsup.Retry
			}
			return procsup.Crash // rc=0 も落ちたと同じ: 見張りは stdin が閉じるか信号でしか抜けない
		},
		OnEvent:     monitorEvents(say),
		RestartWait: 10 * time.Second, RetryWait: 30 * time.Second,
		CrashLimit: 3, CrashWindow: monitorCrashWindow, StopWait: 5 * time.Second,
	}
}

// monitorCrashWindow は見張りが落ちた回数を数える窓。
const monitorCrashWindow = 30 * time.Minute

// monitorEvents は procsup の出来事を見張りの出来事の文にする。
func monitorEvents(say func(string)) func(procsup.Event) {
	return func(e procsup.Event) {
		switch e.Kind {
		case procsup.EventRetrying:
			if !e.Repeat { // 前の見張りが抜けるまで続きうる。30 秒ごとに書かない
				say(fmt.Sprintf("見張りは前の見張りが lock を持っているので起きなかった。%s ごとに起こし直す", e.Wait))
			}
		case procsup.EventCrashed:
			say(fmt.Sprintf("見張りが抜けた (%s)。%s 後に起こし直す", exitText(e.Err), e.Wait))
		case procsup.EventGaveUp:
			say(fmt.Sprintf("見張りが %s の間に %d 回抜けたので、起こし直さない (最後: %s)。dispatcher と PG は動き続ける。dispatcher を起動し直すと、もう一度起こす", monitorCrashWindow, e.Crashes, exitText(e.Err)))
		case procsup.EventStartFailed:
			say("見張りを起こせない: " + e.Err.Error())
		}
	}
}

// superviseMonitor は見張りを裏で起こし続ける。返した関数は、見張りを止めて終わるまで待つ (ctx の取り消しでも止まる。何度呼んでもよい)。
func superviseMonitor(ctx context.Context, s procsup.Spec) (stop func()) {
	stopSup := procsup.Start(ctx, s)
	return func() { stopSup() }
}

// exitText は子の Wait の結果を出来事の文にする (nil は rc=0)。
func exitText(err error) string {
	if err == nil {
		return "rc=0"
	}
	return err.Error()
}
