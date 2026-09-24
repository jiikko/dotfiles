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
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *limit < 1 {
		_, _ = fmt.Fprintln(stderr, "pro-con daemon: --limit は 1 以上")
		return 2
	}
	unlock, err := daemon.Lock(dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con daemon:", err)
		return 1
	}
	defer unlock()
	d := &daemon.Daemon{Dir: dir, Limit: *limit, Repos: repos, Launch: daemon.ExecLauncher{},
		List: func(ctx context.Context) ([]agents.Session, error) { return agents.List(ctx, agents.ExecRunner) }, Now: time.Now,
		Transcript: func(sessionID string) (live.Transcript, error) {
			p, err := live.FindTranscript(projects, sessionID)
			if err != nil {
				return live.Transcript{}, err
			}
			return live.ReadTail(p)
		}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d.Publish, d.Notify = daemon.TmuxPublish(ctx), daemon.MacNotify(ctx)
	defer func() { _ = daemon.TmuxPublish(context.Background())("") }() // 止まるときに件数を消す (古い件数を出し続けない)
	for {
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
