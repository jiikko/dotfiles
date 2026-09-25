package main

// pro-con monitor — 見張り (issue 475。判定は package monitor)。dispatcher が子として起こして止める (monitorsup.go の superviseMonitor)。
// 手で起動してもよい (--once で 1 回だけ見る)。2 つ起動しない (dispatcher.LockMonitor)。読むだけで、見つけたことは受付の箱に置く。

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pro-con/dispatcher"
	"pro-con/monitor"
)

// monitorInterval は見張りの既定の間隔 (git の結果は HEAD の組が変わったときだけ出し直すので、git を呼ぶのは変わったときだけ)。
const monitorInterval = time.Minute

// monitorExitHeld は、別の見張りが lock を持っていたので何もせずに抜けたときの終了コード
// (起こした dispatcher は「落ちた」と数えず、間を空けて起こし直す。kill -9 された前の dispatcher の見張りがまだ抜けていない間)。
const monitorExitHeld = 3

// untilStdinClosesFlag は、stdin が閉じたら抜ける (内部用)。dispatcher が握るパイプの読む側を stdin に渡す:
// dispatcher がどう死んでも (kill -9 でも) OS がパイプを閉じるので、見張りはすぐ抜ける (macOS には PDEATHSIG が無い)。
const untilStdinClosesFlag = "until-stdin-closes"

func runMonitor(args []string, dir string, repos map[string]string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con monitor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	interval := fs.Duration("interval", monitorInterval, "見る間隔")
	once := fs.Bool("once", false, "1 回だけ見て終わる")
	untilClosed := fs.Bool(untilStdinClosesFlag, false, "stdin が閉じたら抜ける (内部用。起こした dispatcher が握るパイプを stdin に渡す)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *interval <= 0 {
		_, _ = fmt.Fprintln(stderr, "pro-con monitor: --interval は正の長さ")
		return 2
	}
	unlock, err := dispatcher.LockMonitor(dir)
	if errors.Is(err, dispatcher.ErrMonitorRunning) {
		_, _ = fmt.Fprintln(stderr, "pro-con monitor:", err)
		return monitorExitHeld
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con monitor:", err)
		return 1
	}
	defer unlock()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if *untilClosed {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin) // EOF = 起こした dispatcher が居なくなった
			_, _ = fmt.Fprintln(stderr, "pro-con monitor: 起こした dispatcher が居なくなったので抜ける")
			cancel()
		}()
	}
	m := &monitor.Monitor{Dir: dir, Repos: repos, Now: time.Now, Log: func(text string) { _, _ = fmt.Fprintln(stderr, "pro-con monitor:", text) }}
	return watch(ctx, m, *interval, *once, stdout, stderr)
}

// watch は m.Check を interval ごとに、ctx が終わるまで回す。
func watch(ctx context.Context, m *monitor.Monitor, interval time.Duration, once bool, stdout, stderr io.Writer) int {
	for {
		n, err := m.Check(ctx)
		if n > 0 {
			_, _ = fmt.Fprintf(stdout, "%s 見張り: %d 件を受付の箱に置いた\n", time.Now().Format("15:04:05"), n)
		}
		if err != nil && ctx.Err() == nil {
			_, _ = fmt.Fprintln(stderr, "pro-con monitor:", err) // 1 回の失敗では抜けない (次の回で見直す)
		}
		if once {
			if err != nil {
				return 1
			}
			return 0
		}
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return 0
		case <-t.C:
		}
	}
}
