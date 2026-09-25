package main

// pro-con daemon — 本物のモードの dispatcher を常駐させる (issue 427 の段階 3c-3)。3 秒ごとに daemon.Tick し、何をしたかを時刻つきで出す。
// 2 つ起動しない (daemon.Lock)。🚨 本物の claude で PG を起動するので、週の利用枠を使う。

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pro-con/agents"
	"pro-con/daemon"
	"pro-con/live"
)

const daemonInterval = 3 * time.Second

// projects は transcript の置き場 (~/.claude/projects。PG が落ちて自動で再開したかを読む)。
func runDaemon(args []string, dir, projects string, repos map[string]string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", 2, "同時に動かす PG の上限 (415 の決定事項: 2 から始める)")
	once := fs.Bool("once", false, "1 回だけ回して終わる")
	stopAll := fs.Bool("stop", false, "動いている daemon と、pro-con が起動した PG を止める (次に daemon を起動したら続きから再開する)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *limit < 1 {
		_, _ = fmt.Fprintln(stderr, "pro-con daemon: --limit は 1 以上")
		return 2
	}
	if *stopAll {
		if err := stopDaemon(context.Background(), dir, projects, repos, stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con daemon --stop:", err)
			return 1
		}
		return 0
	}
	unlock, err := daemon.Lock(dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con daemon:", err)
		return 1
	}
	defer unlock()
	_ = daemon.StopRequested(dir) // 前の --stop が daemon の居ない間に置いた印は捨てる (起動した途端に止まらないように)
	d := newExecDaemon(dir, projects, repos, *limit)
	defer d.CancelRun() // どの出口 (Tick のエラー・SIGTERM) でも、テストの係の実行を残して抜けない
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d.Publish, d.Notify = daemon.TmuxPublish(ctx), daemon.MacNotify(ctx)
	defer func() { _ = daemon.TmuxPublish(context.Background())("") }() // 止まるときに件数を消す (古い件数を出し続けない)
	for {
		if daemon.StopRequested(dir) {
			notes, err := d.Shutdown(ctx)
			for _, n := range notes {
				_, _ = fmt.Fprintf(stdout, "%s %s\n", time.Now().Format("15:04:05"), n)
			}
			daemon.WriteStopResult(dir, err)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con daemon: 止める途中で:", err)
				return 1
			}
			return 0
		}
		notes, err := d.Tick(ctx)
		for _, n := range notes {
			_, _ = fmt.Fprintf(stdout, "%s %s\n", time.Now().Format("15:04:05"), n)
		}
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con daemon:", err)
			return 1
		}
		if *once {
			return 0
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(daemonInterval):
		}
	}
}

// stopTimeout は、動いている daemon が止め終えるまで待つ上限。Shutdown の待ち (約 46 秒 + 一覧の取り直し) と実行中の 1 Tick より長く。
const stopTimeout = 120 * time.Second

// stopDaemon は daemon と PG を止める。daemon が動いていれば止めるよう頼んで待ち、動いていなければ自分で daemon の役を取って止める。
func stopDaemon(ctx context.Context, dir, projects string, repos map[string]string, stdout io.Writer) error {
	running, err := daemon.RequestStop(ctx, dir, stopTimeout)
	if running || err != nil {
		return err
	}
	unlock, err := daemon.Lock(dir)
	if err != nil {
		return err // 確かめた直後に別の daemon が起動した
	}
	defer unlock()
	_ = daemon.StopRequested(dir)
	d := newExecDaemon(dir, projects, repos, 1)
	notes, err := d.Shutdown(ctx)
	for _, n := range notes {
		_, _ = fmt.Fprintln(stdout, n)
	}
	daemon.WriteStopResult(dir, err) // 自分で止めている間に次の --stop が来たら、その --stop はこれを読む
	return err
}

func newExecDaemon(dir, projects string, repos map[string]string, limit int) *daemon.Daemon {
	return &daemon.Daemon{Dir: dir, Limit: limit, Repos: repos, Launch: daemon.ExecLauncher{},
		Runner: daemon.ExecRunner{}, Summarize: daemon.HaikuSummarize(dir),
		List: func(ctx context.Context) ([]agents.Session, error) { return agents.List(ctx, agents.ExecRunner) }, Now: time.Now,
		Transcript: func(sessionID string) (live.Transcript, error) {
			p, err := live.FindTranscript(projects, sessionID)
			if err != nil {
				return live.Transcript{}, err
			}
			return live.ReadTail(p)
		}}
}
