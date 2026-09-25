package dispatcher

// 🚨 信頼の前提: PG に `pro-con card` を許すことは、その PG の worktree で任意のシェルのコマンドを、PG の Claude Code の permission の外で
// (dispatcher の権限で) 走らせることを許すのと同じ。守っているのは「頼んだ場所がそのカードの PG の worktree であること」(RunCwd) だけ。
//
// テストの係 (426 の決定 5): PG が `pro-con card run <カード> -- <コマンド>` で頼んだコマンドを、dispatcher が PG の worktree で 1 本ずつ順に実行する
// (make test / 実機 E2E を PG ごとに走らせると、占有リソースを取り合う)。結果 (rc・ログ) を持たせて PG を再開する。
// 失敗したときだけ、安いモデル (haiku) の Claude がログの末尾を要約する (成功では token を使わない)。
// repo の lock (runLockDir) を lockman で取ってから走らせる。pro-con の外の session・人が同じ lock を取れば、それとも直列になる (471)。
// 実行は裏の goroutine で回し、Tick を止めない。dispatcher が実行の途中で落ちたら、次の dispatcher は「結果が無い」として PG を再開する。

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
)

// Runner はコマンドを dir で実行し、出力 (stdout と stderr) を logPath へ書いて終了コードを返す。runID は実行ごとの印 (残った実行を後で見つけるため)。
// repo の lock (runLockDir) を他が持っていてコマンドを始めなかったときは errRunLockBusy を返す (結果ではない。後で頼み直す)。
type Runner interface {
	Run(ctx context.Context, dir, command, logPath, runID string) (rc int, err error)
}

// RunsDir は実行のログの置き場 (状態の置き場の下)。
const RunsDir = "runs"

// runTailLines は PG と要約に渡すログの末尾の行数。
const runTailLines = 60

// errRunLockBusy は、repo の lock を pro-con の外 (他の session・人) が持っていて、コマンドを始めなかったこと。
var errRunLockBusy = errors.New("repo の lock を他が持っている")

// runLockRetry は lock を他が持っていたとき、次に取りにいくまでの間 (lockman with は待たずに抜けるので、dispatcher が間を空けて頼み直す)。
const runLockRetry = 30 * time.Second

// runBusyResource は lock を他が持っている間のカードの待ちに出すリソース名。
const runBusyResource = store.RunResource + " (pro-con の外が使用中)"

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
func (d *Dispatcher) tickRuns(ctx context.Context, now time.Time) ([]eventlog.Event, error) {
	var notes []eventlog.Event
	if d.active != nil { // 実行中のカードが作業中の列を離れた / 頼みが取り下げられたら、実行を取り消す (結果はもう誰も待っていない)
		st, err := store.Load(d.Dir)
		if err != nil {
			return nil, err
		}
		for _, c := range st.Cards {
			if c.ID == d.active.cardID && (c.State != card.Running || c.Run == "" || c.Deleting()) {
				d.cancelRun(10 * time.Second)
				notes = append(notes, ev(eventlog.KindRun, c.ID, c.Session, c.ID+": 結果を待つ PG が居なくなったので実行を取り消した"))
				break
			}
		}
	}
	if d.active != nil {
		select {
		case r := <-d.active.done:
			if errors.Is(r.err, errRunLockBusy) { // 始めていない: 結果にせず、間を空けて取りにいく (カードは列の先頭で待つ)
				n, err := d.deferRun(now, d.active, r)
				notes = append(notes, n...)
				d.active = nil
				if err != nil {
					return notes, err
				}
				break
			}
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
		if c.Deleting() { // 削除の依頼を受けた: 実行しない・結果で再開しない。前の dispatcher が残した実行は止めてから取り下げる
			if c.Exec.Active() {
				killStaleFn(c.Exec.RunID)
			}
			if err := d.update(c.ID, func(cc *card.Card) { cc.DropRun() }); err != nil {
				return notes, err
			}
			notes = append(notes, ev(eventlog.KindRun, c.ID, c.Session, c.ID+": 削除の依頼を受けたので、テストの係への頼みを取り下げた"))
			continue
		}
		if c.Exec.Active() && (d.active == nil || d.active.cardID != c.ID) {
			// 実行の途中で dispatcher が落ちた (この dispatcher は実行していない)。残ったコマンドを止めてから、結果が無いことを渡して PG を再開する
			// (止めずに頼み直させると、同じ worktree で 2 本が重なる)
			killStaleFn(c.Exec.RunID)
			n, err := d.finishRun(now, &runJob{cardID: c.ID, command: c.Run, start: c.Exec.Since},
				runResult{rc: -1, err: errors.New("dispatcher が実行の途中で止まったので結果が無い。もう一度頼むこと")})
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
	if d.blocked != nil && !slices.ContainsFunc(queue, d.blocked.is) { // 待たせていた頼みが取り下げられた: 次のカードを待たせない
		d.blocked = nil
	}
	if d.active == nil && len(queue) > 0 && d.Runner != nil && (d.blocked == nil || !now.Before(d.blocked.retryAt)) {
		c := queue[0]
		queue = queue[1:]
		o, ok := owned(c, reg)
		switch {
		case !ok || !strings.Contains(o.Cwd, worktreeMarker):
			n, err := d.finishRun(now, &runJob{cardID: c.ID, command: c.Run, start: now},
				runResult{rc: -1, err: errors.New("PG の作業ディレクトリが記録に無いので実行できない")})
			notes = append(notes, n)
			if err != nil {
				return notes, err
			}
		case !isUnder(c.RunCwd, o.Cwd):
			// 🚨 そのカードの PG の worktree から頼まれたものだけを実行する (別のカードの名前で、その worktree で走らせない)
			n, err := d.finishRun(now, &runJob{cardID: c.ID, command: c.Run, start: now},
				runResult{rc: -1, err: fmt.Errorf("頼んだ場所 (%s) がこのカードの PG の worktree ではないので実行しない", c.RunCwd)})
			notes = append(notes, n)
			if err != nil {
				return notes, err
			}
		default:
			runCtx, cancel := context.WithCancel(ctx)
			job := &runJob{cardID: c.ID, command: c.Run, start: now, done: make(chan runResult, 1), cancel: cancel,
				logPath: filepath.Join(d.Dir, RunsDir, fmt.Sprintf("%s-%d-%s.log", c.ID, now.Unix(), dirTag(d.Dir)))}
			if err := d.update(c.ID, func(cc *card.Card) {
				cc.Exec = card.Exec{Command: c.Run, Resource: store.RunResource, Since: now, RunID: filepath.Base(job.logPath)} // 始める前に印を記録する
				cc.Wait = card.Wait{}
				if !d.blocked.is(c) { // lock の取り直しでは足さない (30 秒ごとに履歴が伸びる)
					cc.History = append(cc.History, card.Event{At: now, Text: "テストの係が実行を始めた: " + c.Run})
				}
			}); err != nil {
				return notes, err
			}
			d.active = job
			go d.execute(runCtx, job, c.RunCwd) // 頼んだ場所 (worktree かその下) で実行する
			notes = append(notes, ev(eventlog.KindRun, c.ID, c.Session, fmt.Sprintf("%s のコマンドを実行する: %s", c.ID, c.Run)))
		}
	}
	for i, c := range queue { // 待っているカードに順番を書く (1 始まり。実行中の 1 本の後ろ)
		pos, res := i+1, store.RunResource
		if d.blocked.is(c) && d.active == nil {
			res = runBusyResource
		}
		if c.Wait.Kind == card.WaitResource && c.Wait.Position == pos && c.Wait.Resource == res {
			continue
		}
		if err := d.update(c.ID, func(cc *card.Card) {
			cc.Wait = card.Wait{Kind: card.WaitResource, Resource: res, Position: pos}
		}); err != nil {
			return notes, err
		}
	}
	return notes, nil
}

// runBlock は repo の lock を他が持っていて始められなかった頼み (dispatcher のメモリだけに持つ。落ちたら次の dispatcher が取り直しから始める)。
type runBlock struct {
	cardID, command string
	since           time.Time // 最初に始められなかった時刻 (runLockGiveUp を数える)
	retryAt         time.Time // これより前は次を始めない
}

// is は c の今の頼みが、待たせている頼みそのものか (取り下げて頼み直したものは別の頼み)。
func (b *runBlock) is(c card.Card) bool { return b != nil && b.cardID == c.ID && b.command == c.Run }

// runLockGiveUp は lock が空くのを待つ上限。超えたら結果 (実行できなかった) として PG を再開する
// (中身を読めない lock のように、人が動くまで空かない busy を黙って待ち続けない)。
const runLockGiveUp = time.Hour

// deferRun は lock を他が持っていて始めなかった 1 本を、始める前の形へ戻す (頼みは残し、runLockRetry の後に取りにいく)。
// 始めなかったのでログは結果にならない (消す。30 秒ごとに溜めない。lockman の言い分は r.err に入っていて、最初の 1 回を履歴に残す)。
func (d *Dispatcher) deferRun(now time.Time, job *runJob, r runResult) ([]eventlog.Event, error) {
	_ = os.Remove(job.logPath)
	first := !d.blocked.is(card.Card{ID: job.cardID, Run: job.command})
	if first {
		d.blocked = &runBlock{cardID: job.cardID, command: job.command, since: now}
	}
	if now.Sub(d.blocked.since) >= runLockGiveUp {
		n, err := d.finishRun(now, job, runResult{rc: -1, err: fmt.Errorf("repo の lock が %s たっても空かないので実行しない (%v)", runLockGiveUp, r.err)})
		return []eventlog.Event{n}, err
	}
	d.blocked.retryAt = now.Add(runLockRetry)
	err := d.update(job.cardID, func(cc *card.Card) {
		cc.Exec = card.Exec{}
		if first {
			cc.History = append(cc.History, card.Event{At: now, Text: fmt.Sprintf("repo の lock を pro-con の外が持っているので、空くのを待つ (%v)", r.err)})
		}
	})
	if !first {
		return nil, err
	}
	return []eventlog.Event{ev(eventlog.KindRun, job.cardID, "", job.cardID+": repo の lock を pro-con の外が持っているので、空くのを待つ")}, err
}

// CancelRun は実行中の 1 本 (テストの係) を取り消して終わるまで待ち、btw の答えを作っている 1 本も取り消す (dispatcher が抜ける前に呼ぶ)。
func (d *Dispatcher) CancelRun() {
	d.cancelRun(10 * time.Second)
	d.cancelBtw()
}

// cancelRun は実行中の 1 本を取り消し、終わるまで待つ (上限 wait)。終了のときに使う (dispatcher が抜けた後にコマンドを残さない)。
func (d *Dispatcher) cancelRun(wait time.Duration) {
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
func (d *Dispatcher) execute(ctx context.Context, job *runJob, dir string) {
	var r runResult
	if err := os.MkdirAll(filepath.Dir(job.logPath), 0o700); err != nil {
		r.rc, r.err = -1, err
		job.done <- r
		return
	}
	defer job.cancel()
	r.rc, r.err = d.Runner.Run(ctx, dir, job.command, job.logPath, filepath.Base(job.logPath))
	// 出力が無ければ要約させない (空のログを渡すと、要約の代わりに「ログを貼って」と返ってくる = 427 の段階 4 の本物の確認で実測)
	if tail := logTail(job.logPath); (r.rc != 0 || r.err != nil) && !errors.Is(r.err, errRunLockBusy) && d.Summarize != nil && strings.TrimSpace(tail) != "" {
		if s, err := d.Summarize(ctx, tail); err == nil {
			r.summary = strings.TrimSpace(s)
		}
	}
	job.done <- r
}

// finishRun は結果をカードに持たせ、分解済みへ戻す (dispatch が結果を渡して同じ session を再開する)。
func (d *Dispatcher) finishRun(now time.Time, job *runJob, r runResult) (eventlog.Event, error) {
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
		if tail := logTail(job.logPath); strings.TrimSpace(tail) != "" {
			b.WriteString("ログの末尾:\n" + tail + "\n")
		} else {
			b.WriteString("出力なし (stdout / stderr とも空)\n")
		}
	}
	if job.logPath != "" {
		b.WriteString("全体のログ: " + job.logPath + "\n")
	}
	text := b.String()
	if d.blocked != nil && d.blocked.cardID == job.cardID {
		d.blocked = nil
	}
	err := d.update(job.cardID, func(cc *card.Card) {
		if cc.State != card.Running || cc.Run == "" { // 止められた等で、もう結果を待っていない
			return
		}
		cc.DropRun()
		cc.State, cc.Since, cc.Resume = card.Planned, now, text
		cc.History = append(cc.History, card.Event{At: now, Text: fmt.Sprintf("テストの係: rc=%d (%s)。結果を渡して PG を再開する", r.rc, clipLine(job.command))})
	})
	return ev(eventlog.KindRun, job.cardID, "", fmt.Sprintf("%s のコマンドが終わった: rc=%d", job.cardID, r.rc)), err
}

func clipLine(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// logTailBytes はログの末尾を読む量 (巨大なログを丸ごと読まない)。
const logTailBytes = 64 << 10

// logTail はログの末尾 runTailLines 行 (読めなければ空)。
func logTail(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	off := max(st.Size()-logTailBytes, 0)
	data := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(data, off); err != nil {
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
type ExecRunner struct {
	// Lockman は lockman の実行ファイル (名前だけなら PATH から探す)。空でなければ repo の lock (runLockDir) を取ってからコマンドを走らせる
	// (pro-con の外の session・人も同じ lock を取れば、重い処理が repo ごとに直列になる。471)。空なら lock を取らない (e2e・テスト)
	Lockman string
}

func (r ExecRunner) Run(ctx context.Context, dir, command, logPath, runID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	f, err := os.Create(logPath)
	if err != nil {
		return -1, err
	}
	defer func() { _ = f.Close() }()
	cmd := runCommand(ctx, command, runID)
	pgidFile := "" // lock を取るときだけ使う (下の注記)
	if r.Lockman != "" {
		lockDir, err := runLockDir(ctx, dir)
		if err != nil {
			return -1, err
		}
		pgidFile = logPath + ".pgid"
		defer func() { _ = os.Remove(pgidFile) }()
		cmd = withRunLock(ctx, cmd, r.Lockman, lockDir, pgidFile, runID)
	}
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = f, f
	// make test などは子プロセスを起こすので、取り消し・時間切れはプロセスグループごと止める (bash だけ止めると子が残る)。
	// 🚨 lockman は子を別のグループに置く (lease を失ったときに止めるため) ので、lockman のグループを撃っても bash の子には届かない。
	// bash が自分の pgid を書いた pgidFile から、そのグループも撃つ
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	exited := make(chan struct{})
	cmd.Cancel = func() error {
		if pgid := readPgid(pgidFile); pgid > 1 {
			// 🚨 lockman 本体を SIGKILL すると lock を解放できず、TTL (30 分) まで repo の次の実行を止める。
			// SIGTERM を無視する子は、lockman ではなく bash のグループを後から SIGKILL する (子が死ねば lockman が解放して抜ける)
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			go func() {
				select {
				case <-exited:
				case <-time.After(runKillGrace):
					_ = syscall.Kill(-pgid, syscall.SIGKILL)
				}
			}()
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = runKillGrace
	if pgidFile != "" {
		cmd.WaitDelay = 3 * runKillGrace // 子を止めた後に lockman が解放する間 (これを過ぎると lockman も SIGKILL される)
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	// 終わった後も、SIGTERM を無視した子や bash が抜けた後に残った子を、プロセスグループごと止める (dispatcher の後に残さない)
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if pgid := readPgid(pgidFile); pgid > 1 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}()
	err = cmd.Wait()
	close(exited)
	started := readPgid(pgidFile) != 0
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &ee) && ctx.Err() == nil:
		switch rc := ee.ExitCode(); {
		case pgidFile == "":
			return rc, nil
		case !started && rc == lockmanWithBusy:
			// lockman の「他が保持中」は、bash が起動していない (pgid を書いていない) ときだけ。頼まれたコマンドが 121 で抜けたのと取り違えない
			return -1, fmt.Errorf("%w: %s", errRunLockBusy, strings.TrimSpace(logTail(logPath)))
		case !started: // bash が起動していないので、rc はコマンドのものではない
			return -1, fmt.Errorf("lockman が失敗した (rc=%d。コマンドは走っていない): %s", rc, strings.TrimSpace(logTail(logPath)))
		case rc == lockmanWithLost || rc == lockmanWithInvalid:
			// 子の rc が上書きされうる (122 = 走行中に lease を失った / 125 = 子の後の解放に失敗)。子自身がこの値で抜けたのとは区別できない
			return rc, fmt.Errorf("lockman の rc かもしれない (122 = 走行中に lock を失った / 125 = lock の解放に失敗。ログの lockman の行を見ること)")
		}
		return ee.ExitCode(), nil
	case ctx.Err() != nil:
		return -1, fmt.Errorf("時間切れか中断 (%w)", ctx.Err())
	default:
		return -1, err
	}
}

// `lockman with` の自分の終了コード (src/lockman/main.go の exitWith*): 121 = 他が保持中なので子を起こさなかった /
// 122 = 走行中に lease を失った / 125 = lockman のエラー (子の後の解放の失敗を含む)。
const (
	lockmanWithBusy    = 121
	lockmanWithLost    = 122
	lockmanWithInvalid = 125
)

// runKillGrace は取り消しの SIGTERM から SIGKILL までの猶予。
const runKillGrace = 5 * time.Second

// runLocksDir / runLockKind は repo の lock の置き場 (runLockDir)。種類 (テスト / xcodebuild / 実機) を分けるときは runLockKind の段を増やす。
const (
	runLocksDir = "pro-con-locks"
	runLockKind = "test"
)

// runLockDir は dir の repo の lock を置くディレクトリ: <git の共通ディレクトリ>/pro-con-locks/test (無ければ作る)。
// 共通ディレクトリ (`git rev-parse --git-common-dir`) は同じ repo のどの worktree から見ても同じなので、worktree をまたいで 1 つの lock になり、
// repo が違えば別の lock になる。.git の中なので working tree に lock の残骸を置かない。
// pro-con の外から同じ lock を取る形 (issue 471):
//
//	lockman with "$(git rev-parse --path-format=absolute --git-common-dir)/pro-con-locks/test" -- make test
func runLockDir(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("repo の lock の置き場を決められない (%s で git rev-parse --git-common-dir: %w)", dir, err)
	}
	d := filepath.Join(strings.TrimSpace(string(out)), runLocksDir, runLockKind)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", fmt.Errorf("repo の lock の置き場を作れない: %w", err)
	}
	return d, nil
}

// withRunLock は実行の bash (runCommand) を `lockman with <lockDir> -- <bash>` で包む。bash は自分の pgid を pgidFile へ書く (runScript)。
func withRunLock(ctx context.Context, bash *exec.Cmd, lockman, lockDir, pgidFile, runID string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, lockman, append([]string{"with", lockDir, "--label", "pro-con テストの係 " + runID, "--"}, bash.Args...)...)
	cmd.Env = append(bash.Env, runPgidEnv+"="+pgidFile)
	return cmd
}

// readPgid は pgidFile に bash が書いた pgid (無い・読めなければ 0)。
func readPgid(path string) int {
	if path == "" {
		return 0
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return n
}

// isUnder は child が root そのものかその下か (symlink を解決して比べる)。どちらかが空なら偽。
func isUnder(child, root string) bool {
	if child == "" || root == "" {
		return false
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	rel, err := filepath.Rel(resolve(root), resolve(child))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// dirTag は状態の置き場の短い印 (実行の印に混ぜて、別の置き場の dispatcher (e2e と本物) の実行と取り違えない)。
func dirTag(dir string) string { return fmt.Sprintf("%x", sha1.Sum([]byte(dir)))[:8] }

// runMarkerPrefix は実行の bash の $0 に載せる印の頭。
const runMarkerPrefix = "pro-con-run:"

// runScript は実行の bash が走らせる台本。頼まれたコマンドは環境変数 (runCommandEnv) で渡して eval する:
//   - 文字列を連結しない (末尾のバックスラッシュ・閉じていない here-doc で、足した行と繋がって意味が変わった = 427 の敵対的レビューで実測)
//   - eval (組み込み) を通すので、bash は単純なコマンドに exec で置き換わらない (置き換わると $0 の印が ps から消える。
//     /bin/bash 3.2 で実測。TestKillStaleByRunMarker が単純なコマンドで固定している)
//
// 終了コードは頼まれたコマンドのもの。ただし次の 2 つは直接の `bash -c` と違う (eval を通すため):
//   - シグナルで死んだときは bash が 128+n で返す (exec されていた前の形では -1 だった)
//   - 構文エラーは rc=1 (直接の `bash -c` は 2)。エラー文の頭も `eval:` になる
//
// 🚨 制限: 頼まれたコマンド自身が `exec` を含むと (例 `cd x && exec make test`)、bash が置き換わって印が消え、dispatcher が落ちた後に止められない。
// setsid / Setpgid で自分のグループを作った子孫も、グループごと撃つ方式では止められない
//
// runPgidEnv があれば (lock を取るとき。withRunLock)、先に自分の pid (= lockman が作ったグループの pgid) をそこへ書き、頼まれたコマンドには渡さない
const runScript = "[ -z \"${" + runPgidEnv + "-}\" ] || echo $$ >\"$" + runPgidEnv + "\"; unset " + runPgidEnv + "; eval \"$" + runCommandEnv + "\""

// runCommandEnv は頼まれたコマンドを渡す環境変数 / runPgidEnv は実行の bash が自分の pgid を書くファイル。
const (
	runCommandEnv = "PRO_CON_RUN_COMMAND"
	runPgidEnv    = "PRO_CON_RUN_PGID_FILE"
)

// runCommand は実行の bash を組む。印は $0 (`pro-con-run:<runID>`)。
func runCommand(ctx context.Context, command, runID string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", runScript, runMarkerPrefix+runID)
	cmd.Env = append(withoutTmux(os.Environ()), runCommandEnv+"="+command)
	return cmd
}

// killStaleFn は killStale (テストで差し替えて、呼ばれたかを見る)。
var killStaleFn = killStale

// killStale は、前の dispatcher が残した実行 (印が runID のもの) のプロセスグループを止める。印で探すので、pid の使い回しで別のものを撃たない。
func killStale(runID string) {
	if runID == "" {
		return
	}
	out, err := exec.Command("ps", "-A", "-ww", "-o", "pid=,pgid=,command=").Output()
	if err != nil {
		return
	}
	marker := " " + runMarkerPrefix + runID
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		// 撃つのは、dispatcher が起こした形 (`/bin/bash -c <台本> <印>`) のグループの先頭だけ。印を引数の最後に置いた
		// 他のプロセス (pgrep -f <印> 等) のグループを撃たない
		if len(f) < 4 || f[2] != "/bin/bash" || f[3] != "-c" || f[0] != f[1] || !strings.HasSuffix(strings.TrimSpace(line), marker) {
			continue
		}
		if pgid, err := strconv.Atoi(f[1]); err == nil && pgid > 1 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}
}

// summarizeTimeout は要約の上限。
const summarizeTimeout = 3 * time.Minute

// HaikuSummarize は失敗したログの末尾を haiku に要約させる (426 の決定 5)。状態の置き場で動かす (repo の hook・規約を読ませない)。
func HaikuSummarize(dir string) func(ctx context.Context, tail string) (string, error) {
	return func(ctx context.Context, tail string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, summarizeTimeout)
		defer cancel()
		return haiku(ctx, dir, "次はテストかビルドのコマンドの失敗したログの末尾です。何が失敗したか (落ちたテスト名・エラーの場所と内容) を日本語で 5 行以内に要約してください。ログに書かれていないことは書かず、質問もしないこと。\n\n"+tail)
	}
}

// HaikuAsk は btw の答えを haiku に作らせる (btw.go。上限は呼ぶ側が付ける)。
func HaikuAsk(dir string) func(ctx context.Context, prompt string) (string, error) {
	return func(ctx context.Context, prompt string) (string, error) { return haiku(ctx, dir, prompt) }
}

// haiku は prompt を安いモデルの claude -p に渡す。状態の置き場で動かす (repo の hook・規約を読ませない)。
func haiku(ctx context.Context, dir, prompt string) (string, error) {
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
