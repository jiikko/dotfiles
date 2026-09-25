package main

// pro-con dispatcher — 本物のモードの dispatcher を常駐させる (issue 427 の段階 3c-3)。3 秒ごとに dispatcher.Tick し、何をしたかを時刻つきで出す。
// 2 つ起動しない (dispatcher.Lock)。🚨 本物の claude で PG を起動するので、週の利用枠を使う。

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
	"pro-con/dispatcher"
	"pro-con/live"
)

const dispatcherInterval = 3 * time.Second

// projects は transcript の置き場 (~/.claude/projects。PG が落ちて自動で再開したかを読む)。
func runDispatcher(args []string, dir, projects string, repos map[string]string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con dispatcher", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", 2, "同時に動かす PG の上限 (415 の決定事項: 2 から始める)")
	once := fs.Bool("once", false, "1 回だけ回して終わる")
	stopAll := fs.Bool("stop", false, "動いている dispatcher と、pro-con が起動した PG を止める (次に dispatcher を起動したら続きから再開する)")
	e2eRoot := fs.String("e2e", "", "e2e モードの置き場 (PG は台本どおりに動く偽物。claude を起動しない。pro-con e2e が使う)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *limit < 1 {
		_, _ = fmt.Fprintln(stderr, "pro-con dispatcher: --limit は 1 以上")
		return 2
	}
	var e2e *dispatcher.E2E
	if *e2eRoot != "" {
		e := dispatcher.E2E{Root: *e2eRoot}
		e2e, dir, repos = &e, e.StateDir(), map[string]string{dispatcher.E2ERepo: e.RepoDir()}
		if err := os.MkdirAll(e.RepoDir(), 0o700); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", err)
			return 1
		}
	}
	if *stopAll {
		if err := stopDispatcher(context.Background(), dir, projects, repos, e2e, stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con dispatcher --stop:", err)
			return 1
		}
		return 0
	}
	unlock, err := dispatcher.Lock(dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", err)
		return 1
	}
	defer unlock()
	_ = dispatcher.StopRequested(dir) // 前の --stop が dispatcher の居ない間に置いた印は捨てる (起動した途端に止まらないように)
	d := newDispatcherFor(dir, projects, repos, *limit, e2e)
	defer d.CancelRun()                                                                                    // どの出口 (Tick のエラー・SIGTERM) でも、テストの係の実行を残して抜けない
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP) // SIGHUP: 端末・tmux のペインを閉じた (既定の動作で死ぬと実行を残す)
	defer stop()
	if e2e == nil { // e2e モードは本物の tmux の件数・macOS の通知に触らない
		d.Publish, d.Notify = dispatcher.TmuxPublish(ctx), dispatcher.MacNotify(ctx)
		defer func() { _ = dispatcher.TmuxPublish(context.Background())("") }() // 止まるときに件数を消す (古い件数を出し続けない)
	}
	for {
		if dispatcher.StopRequested(dir) {
			notes, err := d.Shutdown(ctx)
			for _, n := range notes {
				_, _ = fmt.Fprintf(stdout, "%s %s\n", time.Now().Format("15:04:05"), n)
			}
			dispatcher.WriteStopResult(dir, err)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con dispatcher: 止める途中で:", err)
				return 1
			}
			return 0
		}
		notes, err := d.Tick(ctx)
		for _, n := range notes {
			_, _ = fmt.Fprintf(stdout, "%s %s\n", time.Now().Format("15:04:05"), n)
		}
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", err)
			return 1
		}
		if *once {
			return 0
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(dispatcherInterval):
		}
	}
}

// stopTimeout は、動いている dispatcher が止め終えるまで待つ上限。Shutdown の待ち (約 46 秒 + 一覧の取り直し) と実行中の 1 Tick より長く。
const stopTimeout = 120 * time.Second

// stopDispatcher は dispatcher と PG を止める。dispatcher が動いていれば止めるよう頼んで待ち、動いていなければ自分で dispatcher の役を取って止める。
func stopDispatcher(ctx context.Context, dir, projects string, repos map[string]string, e2e *dispatcher.E2E, stdout io.Writer) error {
	running, err := dispatcher.RequestStop(ctx, dir, stopTimeout)
	if running || err != nil {
		return err
	}
	unlock, err := dispatcher.Lock(dir)
	if err != nil {
		return err // 確かめた直後に別の dispatcher が起動した
	}
	defer unlock()
	_ = dispatcher.StopRequested(dir)
	d := newDispatcherFor(dir, projects, repos, 1, e2e)
	notes, err := d.Shutdown(ctx)
	for _, n := range notes {
		_, _ = fmt.Fprintln(stdout, n)
	}
	dispatcher.WriteStopResult(dir, err) // 自分で止めている間に次の --stop が来たら、その --stop はこれを読む
	return err
}

// newDispatcherFor は dispatcher を組む。e2e が nil なら本物 (claude を起動する)、あれば偽の PG と偽の一覧 (claude を起動しない)。
func newDispatcherFor(dir, projects string, repos map[string]string, limit int, e2e *dispatcher.E2E) *dispatcher.Dispatcher {
	if e2e != nil {
		return &dispatcher.Dispatcher{Dir: dir, Limit: limit, Repos: repos, Launch: e2e.Launcher(), List: e2e.List, Now: time.Now,
			Runner: dispatcher.ExecRunner{}, FakePM: e2e.FakePM} // テストの係は本物のシェル (偽の worktree で走る)。失敗の要約 (haiku) はしない
	}
	return &dispatcher.Dispatcher{Dir: dir, Limit: limit, Repos: repos, Launch: dispatcher.ExecLauncher{},
		Runner: dispatcher.ExecRunner{}, Summarize: dispatcher.HaikuSummarize(dir),
		List: func(ctx context.Context) ([]agents.Session, error) { return agents.List(ctx, agents.ExecRunner) }, Now: time.Now,
		Transcript: func(sessionID string) (live.Transcript, error) {
			p, err := live.FindTranscript(projects, sessionID)
			if err != nil {
				return live.Transcript{}, err
			}
			return live.ReadTail(p)
		}}
}
