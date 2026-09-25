package daemon

// テストの係 (426 の決定 5): PG が `pro-con card run <カード> -- <コマンド>` で頼んだコマンドを、daemon が PG の worktree で 1 本ずつ順に実行する
// (make test / 実機 E2E を PG ごとに走らせると、占有リソースを取り合う)。結果 (rc・ログ) を持たせて PG を再開する。
// 失敗したときだけ、安いモデル (haiku) の Claude がログの末尾を要約する (成功では token を使わない)。
// 実行は裏の goroutine で回し、Tick を止めない。daemon が実行の途中で落ちたら、次の daemon は「結果が無い」として PG を再開する。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// Runner はコマンドを dir で実行し、出力 (stdout と stderr) を logPath へ書いて終了コードを返す。
type Runner interface {
	Run(ctx context.Context, dir, command, logPath string) (rc int, err error)
}

// RunsDir は実行のログの置き場 (状態の置き場の下)。
const RunsDir = "runs"

// runTailLines は PG と要約に渡すログの末尾の行数。
const runTailLines = 60

// runJob は実行中の 1 本。
type runJob struct {
	cardID, command, logPath string
	start                    time.Time
	done                     chan runResult
	cancel                   context.CancelFunc // 実行を取り消す (終了のとき)
}

type runResult struct {
	rc      int
	err     error
	summary string // 失敗したときの要約 (要約できなければ空)
}

// tickRuns はテストの係の 1 回ぶん: 終わった実行の結果を渡し、次の実行を始め、待っているカードに順番を書く。
func (d *Daemon) tickRuns(ctx context.Context, now time.Time) ([]string, error) {
	var notes []string
	if d.active != nil {
		select {
		case r := <-d.active.done:
			n, err := d.finishRun(now, d.active, r)
			notes = append(notes, n)
			d.active = nil
			if err != nil {
				return notes, err
			}
		default:
		}
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return notes, err
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return notes, err
	}
	var queue []card.Card
	for _, c := range st.Cards {
		if c.Run == "" || c.State != card.Running {
			continue
		}
		if c.Exec.Active() && (d.active == nil || d.active.cardID != c.ID) {
			// 実行の途中で daemon が落ちた (この daemon は実行していない)。結果が無いことを渡して PG を再開する
			n, err := d.finishRun(now, &runJob{cardID: c.ID, command: c.Run, start: c.Exec.Since},
				runResult{rc: -1, err: errors.New("daemon が実行の途中で止まったので結果が無い。もう一度頼むこと")})
			notes = append(notes, n)
			if err != nil {
				return notes, err
			}
			continue
		}
		if !c.Exec.Active() {
			queue = append(queue, c)
		}
	}
	sort.SliceStable(queue, func(i, j int) bool { return queue[i].RunAt.Before(queue[j].RunAt) })
	if d.active == nil && len(queue) > 0 && d.Runner != nil {
		c := queue[0]
		queue = queue[1:]
		o, ok := owned(c, reg)
		if !ok || !strings.Contains(o.Cwd, worktreeMarker) {
			n, err := d.finishRun(now, &runJob{cardID: c.ID, command: c.Run, start: now},
				runResult{rc: -1, err: errors.New("PG の作業ディレクトリが記録に無いので実行できない")})
			notes = append(notes, n)
			if err != nil {
				return notes, err
			}
		} else {
			runCtx, cancel := context.WithCancel(ctx)
			job := &runJob{cardID: c.ID, command: c.Run, start: now, done: make(chan runResult, 1), cancel: cancel,
				logPath: filepath.Join(d.Dir, RunsDir, fmt.Sprintf("%s-%d.log", c.ID, now.Unix()))}
			if err := d.update(c.ID, func(cc *card.Card) {
				cc.Exec = card.Exec{Command: c.Run, Resource: store.RunResource, Since: now}
				cc.Wait = card.Wait{}
				cc.History = append(cc.History, card.Event{At: now, Text: "テストの係が実行を始めた: " + c.Run})
			}); err != nil {
				return notes, err
			}
			d.active = job
			go d.execute(runCtx, job, o.Cwd)
			notes = append(notes, fmt.Sprintf("%s のコマンドを実行する: %s", c.ID, c.Run))
		}
	}
	for i, c := range queue { // 待っているカードに順番を書く (1 始まり。実行中の 1 本の後ろ)
		pos := i + 1
		if c.Wait.Kind == card.WaitResource && c.Wait.Position == pos {
			continue
		}
		if err := d.update(c.ID, func(cc *card.Card) {
			cc.Wait = card.Wait{Kind: card.WaitResource, Resource: store.RunResource, Position: pos}
		}); err != nil {
			return notes, err
		}
	}
	return notes, nil
}

// cancelRun は実行中の 1 本を取り消し、終わるまで待つ (上限 wait)。終了のときに使う (daemon が抜けた後にコマンドを残さない)。
func (d *Daemon) cancelRun(wait time.Duration) {
	if d.active == nil {
		return
	}
	d.active.cancel()
	select {
	case <-d.active.done:
	case <-time.After(wait):
	}
	d.active = nil
}

// execute は裏で実行し、失敗なら要約して結果を返す。
func (d *Daemon) execute(ctx context.Context, job *runJob, dir string) {
	var r runResult
	if err := os.MkdirAll(filepath.Dir(job.logPath), 0o700); err != nil {
		r.rc, r.err = -1, err
		job.done <- r
		return
	}
	defer job.cancel()
	r.rc, r.err = d.Runner.Run(ctx, dir, job.command, job.logPath)
	if (r.rc != 0 || r.err != nil) && d.Summarize != nil {
		if s, err := d.Summarize(ctx, logTail(job.logPath)); err == nil {
			r.summary = strings.TrimSpace(s)
		}
	}
	job.done <- r
}

// finishRun は結果をカードに持たせ、分解済みへ戻す (dispatch が結果を渡して同じ session を再開する)。
func (d *Daemon) finishRun(now time.Time, job *runJob, r runResult) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "テストの係の結果: `%s` rc=%d (所要 %s)\n", job.command, r.rc, now.Sub(job.start).Round(time.Second))
	switch {
	case r.err != nil && r.rc == -1:
		fmt.Fprintf(&b, "実行できなかった: %v\n", r.err)
	case r.rc == 0 && r.err == nil:
		b.WriteString("成功\n")
	default:
		if r.err != nil {
			fmt.Fprintf(&b, "エラー: %v\n", r.err)
		}
		if r.summary != "" {
			b.WriteString("要約:\n" + r.summary + "\n")
		}
		if tail := logTail(job.logPath); tail != "" {
			b.WriteString("ログの末尾:\n" + tail + "\n")
		}
	}
	if job.logPath != "" {
		b.WriteString("全体のログ: " + job.logPath + "\n")
	}
	text := b.String()
	err := d.update(job.cardID, func(cc *card.Card) {
		if cc.State != card.Running || cc.Run == "" { // 止められた等で、もう結果を待っていない
			return
		}
		cc.Run, cc.RunAt, cc.Exec, cc.Wait = "", time.Time{}, card.Exec{}, card.Wait{}
		cc.State, cc.Since, cc.Resume = card.Planned, now, text
		cc.History = append(cc.History, card.Event{At: now, Text: fmt.Sprintf("テストの係: rc=%d (%s)。結果を渡して PG を再開する", r.rc, clipLine(job.command))})
	})
	return fmt.Sprintf("%s のコマンドが終わった: rc=%d", job.cardID, r.rc), err
}

func clipLine(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// logTail はログの末尾 runTailLines 行 (読めなければ空)。
func logTail(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > runTailLines {
		lines = lines[len(lines)-runTailLines:]
	}
	return strings.Join(lines, "\n")
}

// runTimeout は 1 本の実行の上限 (長いテストでも超えない長さ。超えたら止めて失敗にする)。
const runTimeout = time.Hour

// ExecRunner は本物のシェルで実行する。TMUX / TMUX_PANE は落とす (PG と同じ理由)。
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir, command, logPath string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	f, err := os.Create(logPath)
	if err != nil {
		return -1, err
	}
	defer func() { _ = f.Close() }()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", command)
	cmd.Dir = dir
	cmd.Env = withoutTmux(os.Environ())
	cmd.Stdout, cmd.Stderr = f, f
	// make test などは子プロセスを起こすので、取り消し・時間切れはプロセスグループごと止める (bash だけ止めると子が残る)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &ee) && ctx.Err() == nil:
		return ee.ExitCode(), nil
	case ctx.Err() != nil:
		return -1, fmt.Errorf("時間切れか中断 (%v)", ctx.Err())
	default:
		return -1, err
	}
}

// summarizeTimeout は要約の上限。
const summarizeTimeout = 3 * time.Minute

// HaikuSummarize は失敗したログの末尾を haiku に要約させる (426 の決定 5)。状態の置き場で動かす (repo の hook・規約を読ませない)。
func HaikuSummarize(dir string) func(ctx context.Context, tail string) (string, error) {
	return func(ctx context.Context, tail string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, summarizeTimeout)
		defer cancel()
		prompt := "次はテストかビルドのコマンドの失敗したログの末尾です。何が失敗したか (落ちたテスト名・エラーの場所と内容) を日本語で 5 行以内に要約してください。推測は書かないこと。\n\n" + tail
		cmd := exec.CommandContext(ctx, "claude", "-p", "--model", "haiku", "--setting-sources", "project,local")
		cmd.Dir = dir
		cmd.Env = withoutTmux(os.Environ())
		cmd.Stdin = strings.NewReader(prompt)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("claude -p: %w: %s", err, strings.TrimSpace(errOut.String()))
		}
		return out.String(), nil
	}
}
