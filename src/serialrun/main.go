// serial-run — 重いコマンド (make test / xcodebuild 等) をマシン全体で順番待ちにし、同時には 1 本だけ走らせる。
//
//	serial-run [-l <lock名>] [--max-wait <秒>] -- <コマンド> [引数…]
//	serial-run status [-l <lock名>]
//	serial-run hook
//
// rc: 子の rc (シグナルで死んだら 128+n) / 125 道具の失敗 (誤用・置き場・待ちの上限) / 126 起動できない / 127 見つからない。
// 設計と排他の約束は src/serialrun/README.md。
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"proctree"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	rcTool       = 125
	rcCannotExec = 126
	rcNotFound   = 127

	defaultMaxWait = time.Hour
	// fallbackPoll は blocking の flock が何かで起こされなかったときの保険の見直しの間隔 (ユーザー指示 2026-10-22)。
	// 普段は前の札の flock で起きるので、この間隔では起きる必要が無い
	fallbackPoll   = 2 * time.Minute
	reportInterval = 30 * time.Second
	// recheckInterval は前の札が flock で待てないとき (持ち主は死んだが子が走っている・札を開けない) の見直しの間隔
	recheckInterval = 5 * time.Second
	longHold        = 30 * time.Minute
	stopGrace       = 5 * time.Second
	// intGrace は端末の外から INT / QUIT を受けてから、子が自分で終わるのを待つ間 (過ぎたら子孫ごと止める)。
	// 後始末に数秒かける子 (テストの片付け・ビルドの中断) を途中で殺さない長さにする
	intGrace = 30 * time.Second

	heldEnv = "SERIAL_RUN_HELD"
)

const usage = `usage: serial-run [-l <lock名>] [--max-wait <秒>] -- <コマンド> [引数…]
       serial-run status [-l <lock名>]
       serial-run hook   (Claude Code の PreToolUse(Bash) hook。重いコマンドの前に serial-run -- を差し込む)
  -l          lock の名前 (既定 default)
  --max-wait  待ちの上限 (秒。既定 3600。0 は上限なし)。超えたら実行せずに rc=125
rc: 子の rc / 125 道具の失敗 / 126 起動できない / 127 見つからない
`

// lock 名は小文字だけを通す (APFS は大小文字を区別しないので、`Default` と `default` が同じ置き場を別の名前で指し、入れ子の照合が食い違う)。
// 先頭の . も通さない (`.` / `..` は親の置き場の seq を上書きする)
var lockNameRe = regexp.MustCompile(`^[a-z0-9_-][a-z0-9._-]*$`)

type options struct {
	lock    string
	maxWait time.Duration
	status  bool
	argv    []string
}

// tunables は待ちの刻み。本番では既定値で、テストのビルドだけが縮める (seam_on.go)
type tunables struct {
	fallback time.Duration
	report   time.Duration
	recheck  time.Duration // 前の札が flock で待てない (孤児・判定不能) ときの見直しの間隔
	intGrace time.Duration // 端末の外からの INT / QUIT の後、子孫ごと止めるまでの猶予
	noBlock  bool          // blocking の待ちを外し、保険の見直しだけで進むか (保険が効くことを確かめるため)
	wakeLog  string        // 待ちのループが回るたびに 1 行足すファイル (起きた回数を数えるため)
}

func loadTunables() tunables {
	t := tunables{fallback: fallbackPoll, report: reportInterval, recheck: recheckInterval, intGrace: intGrace}
	applyTestSeams(&t)
	return t
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	// __ready は bin/serial-run がビルドの成否だけを確かめる呼び方 (何もせず rc=0)。ビルドの失敗を子の rc=1 と見分けるため
	if len(args) == 1 && args[0] == "__ready" {
		return 0
	}
	if len(args) == 1 && args[0] == "hook" {
		return runHook(os.Stdin, os.Stdout)
	}
	opt, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "serial-run: %v\n%s", err, usage)
		return rcTool
	}
	base, err := stateBase()
	if opt.status {
		return runStatus(base, err, opt.lock, os.Stdout, stderr)
	}
	if err == nil {
		var q queue
		if q, err = openQueue(filepath.Join(base, opt.lock)); err == nil {
			return runWithQueue(q, opt, stderr)
		}
	}
	return runUnqueued(opt.argv, err, stderr)
}

// runStatus は見るだけの入口。置き場を作らない (typo の lock 名で空の置き場を作り「無い」と答えない)
func runStatus(base string, baseErr error, lock string, out, stderr io.Writer) int {
	if baseErr != nil {
		fmt.Fprintf(stderr, "serial-run: 状態の置き場を使えない: %v\n", baseErr)
		return rcTool
	}
	q, err := openExistingQueue(filepath.Join(base, lock))
	if errors.Is(err, errNoQueue) {
		fmt.Fprintf(out, "lock %q は一度も使われていない (置き場 %s が無い)\n", lock, q.dir)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "serial-run: 状態の置き場を使えない: %v\n", err)
		return rcTool
	}
	return printStatus(q, out, stderr)
}

// runUnqueued は待ち列を使えない (置き場を作れない・採番できない) ときに、順番を待たずに子を走らせる。
// 直列化は重さを避けるための便宜で、使えないからといって包んだコマンド (hook が差し込んだ make test など) を走らせないのは
// 害が大きい (利用者には「テストが 125 で落ちた」と見える)。排他を保証しないことは stderr に出す
func runUnqueued(argv []string, why error, stderr io.Writer) int {
	fmt.Fprintf(stderr, "serial-run: 🚨 待ち列を使えないので、順番を待たずに実行する (他の重いコマンドと重なりうる): %v\n", why)
	return execDirect(argv, stderr)
}

func runWithQueue(q queue, opt options, stderr io.Writer) int {
	if q.heldByAncestor(os.Getenv(heldEnv)) || q.heldByAncestorProcess() {
		return execDirect(opt.argv, stderr)
	}
	return runQueued(q, opt, loadTunables(), stderr)
}

// stateBase は状態の置き場の親を返す。どの枝も絶対パスにする (相対だと cwd ごとに別の待ち行列ができる)。
// 相対の XDG_STATE_HOME は XDG の仕様どおり無視して既定に落とす
func stateBase() (string, error) {
	if d := os.Getenv("SERIAL_RUN_STATE_DIR"); d != "" {
		return filepath.Abs(d)
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "serial-run"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "serial-run"), nil
}

// heldToken は子へ渡す SERIAL_RUN_HELD の値 (置き場:番号:nonce)。書式の正本はこの関数と parseHeld の 2 つだけ
func heldToken(dir string, seq int64, nonce string) string {
	return fmt.Sprintf("%s:%d:%s", dir, seq, nonce)
}

func parseHeld(v string) (dir string, seq int64, nonce string, ok bool) {
	j := strings.LastIndex(v, ":")
	if j <= 0 {
		return "", 0, "", false
	}
	i := strings.LastIndex(v[:j], ":")
	if i <= 0 {
		return "", 0, "", false
	}
	n, err := strconv.ParseInt(v[i+1:j], 10, 64)
	if err != nil {
		return "", 0, "", false
	}
	return v[:i], n, v[j+1:], true
}

// heldByAncestor は、呼び出し元の serial-run がこの lock の実行権を今持っているかを SERIAL_RUN_HELD で判定する (入れ子)。
// 持っているのに並ぶと、親が子の終わりを待ち、子が親の終わりを待って止まる。
// その番号の札が生きていて (持ち主の serial-run だけが死んで子が残った孤児も含む)、nonce が一致し、前に札が無いときだけ持ち主とみなす
// (親が終わった後に生き残った孫・置き場が作り直されて番号が使い回された後の古い値では、普通に並ぶ)
func (q queue) heldByAncestor(v string) bool {
	dir, seq, nonce, ok := parseHeld(v)
	if !ok {
		return false
	}
	if real, err := filepath.EvalSymlinks(dir); err != nil || real != q.dir {
		return false
	}
	if st := stateOf(q.ticketPath(seq)); st != stateAlive && st != stateOrphan {
		return false
	}
	if info, ok := q.readInfo(seq); !ok || info.Nonce != nonce {
		return false
	}
	preds, err := q.predecessors(seq)
	return err == nil && len(preds) == 0
}

// heldByAncestorProcess は、環境変数が途中で落とされても (`env -u SERIAL_RUN_HELD`・`env -i HOME=…`) 入れ子を見分ける保険。
// 先頭の生きている札の持ち主 (serial-run) が自分の祖先なら、その serial-run の子孫として呼ばれている (待つと自分の親を待つ)
// 先頭が孤児 (持ち主の serial-run だけが死んで子が走っている) なら、その子が祖先かで見る
func (q queue) heldByAncestorProcess() bool {
	preds, err := q.lineup()
	if err != nil || len(preds) == 0 {
		return false
	}
	info, ok := q.readInfo(preds[0].seq)
	if !ok {
		return false
	}
	switch preds[0].state {
	case stateAlive:
		return info.Pid > 1 && ancestors()[info.Pid]
	case stateOrphan:
		return info.ChildPid > 1 && ancestors()[info.ChildPid]
	}
	return false
}

func execDirect(argv []string, stderr io.Writer) int {
	path, err := exec.LookPath(argv[0])
	if err != nil && !errors.Is(err, exec.ErrDot) {
		fmt.Fprintf(stderr, "serial-run: %v\n", err)
		return startErrorRC(err)
	}
	err = syscall.Exec(path, argv, os.Environ())
	// 子を起こす経路 (runChild) と同じ rc にする (見つからない 127 / 実行できない 126)
	fmt.Fprintf(stderr, "serial-run: %s: %v\n", path, err)
	return startErrorRC(err)
}

type waitResult int

const (
	waitTurn waitResult = iota
	waitTimeout
	waitSignal
	waitError
)

func runQueued(q queue, opt options, tn tunables, stderr io.Writer) int {
	// 🚨 Notify は札を出す前に張る。張る前に届いたシグナルは、札を消さずに道具を殺す (札はカーネルが flock を外すので
	// 次の待ち手が消すが、子を起こした後なら子が残る)
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM)
	// 起動時に無視されていた INT / QUIT / HUP は Notify しない (無視のまま子へ渡す)。serial-run を挟んでも挟まなくても子の受け方を
	// 変えない: 非対話の bash の `make test &` は INT / QUIT を無視して起こされ、`nohup` は HUP を無視させる。
	// 🚨 runtimeout は INT / QUIT の無視を意図して外す (src/runtimeout/main.go の注記。mutate-verify の Ctrl-C のため)。serial-run は
	// 包むだけの道具なので、外さない方を選んだ
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			signal.Notify(sigs, sig)
		}
	}

	// コマンドが無ければ、列を待ち切る前に 127 で抜ける (typo に気づくのが自分の番になってからでは遅い)。
	// / を含む名前は見ない (前に並んでいる make build が作るもの `./bin/x` を指せるように。自分の番で確かめる)
	if _, err := exec.LookPath(opt.argv[0]); !strings.Contains(opt.argv[0], "/") && err != nil && !errors.Is(err, exec.ErrDot) {
		fmt.Fprintf(stderr, "serial-run: %v\n", err)
		return startErrorRC(err)
	}

	var deadline <-chan time.Time
	if opt.maxWait > 0 {
		t := time.NewTimer(opt.maxWait)
		defer t.Stop()
		deadline = t.C
	}

	cwd, _ := os.Getwd()
	enq := time.Now()
	info := ticketInfo{Pid: os.Getpid(), Start: enq, Cwd: cwd, Cmd: displayCmd(opt.argv), Nonce: newNonce(), Pgid: syscall.Getpgrp(), V: ticketVersion}
	var abortSig syscall.Signal
	errMaxWait := errors.New("待ちの上限")
	hist := func(seq int64, waited, ran time.Duration, rc int, outcome string) {
		q.appendHistory(historyEntry{seq: seq, enq: enq, waited: waited, ran: ran, rc: rc, outcome: outcome, cwd: cwd, cmd: info.Cmd})
	}
	tk, err := q.enqueue(info, func() error {
		select {
		case s := <-sigs:
			abortSig, _ = s.(syscall.Signal)
			return fmt.Errorf("シグナル %v", abortSig)
		case <-deadline:
			return errMaxWait
		default:
			return nil
		}
	})
	switch {
	case err == nil:
	case abortSig != 0:
		hist(0, time.Since(enq), -1, 128+int(abortSig), "signal")
		return exitAs(128+int(abortSig), abortSig)
	case errors.Is(err, errMaxWait):
		fmt.Fprintf(stderr, "serial-run: 待ちの上限 (%s) を超えた。採番の lock が空かない。実行しない\n", opt.maxWait)
		hist(0, time.Since(enq), -1, rcTool, "timeout")
		return rcTool
	default:
		hist(0, time.Since(enq), -1, -1, "unqueued")
		return runUnqueued(opt.argv, fmt.Errorf("札を出せない: %w", err), stderr)
	}
	seq := tk.seq

	rf, err := os.OpenFile(q.runPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		tk.release(nil)
		hist(seq, time.Since(enq), -1, -1, "unqueued")
		return runUnqueued(opt.argv, fmt.Errorf("run.lock を開けない: %w", err), stderr)
	}

	w := waiter{q: q, seq: seq, tn: tn, stderr: stderr, start: enq}
	res, got := w.waitPredecessors(deadline, sigs)
	if res == waitTurn {
		res, got = w.acquireRun(rf, deadline, sigs)
	}
	switch res {
	case waitTimeout:
		note := w.holderNote()
		tk.release(rf)
		fmt.Fprintf(stderr, "serial-run: 待ちの上限 (%s) を超えた。実行しない%s\n", opt.maxWait, note)
		hist(seq, time.Since(enq), -1, rcTool, "timeout")
		return rcTool
	case waitError:
		tk.release(rf)
		hist(seq, time.Since(enq), -1, rcTool, "error")
		return rcTool
	case waitSignal:
		tk.release(rf)
		hist(seq, time.Since(enq), -1, 128+int(got), "signal")
		return exitAs(128+int(got), got)
	}
	waited := time.Since(enq)
	if waited >= time.Second {
		fmt.Fprintf(stderr, "serial-run: 実行 (待ち %s)\n", fmtDur(waited))
	}

	started := time.Now()
	rc, got := runChild(opt.argv, heldToken(q.dir, seq, info.Nonce), tn.intGrace, sigs, stderr, func(pid int) {
		if err := tk.setChild(pid, time.Now()); err != nil {
			fmt.Fprintf(stderr, "serial-run: 札に子の情報を書けない (この serial-run だけが殺されたとき、子の同一性を確かめにくくなる): %v\n", err)
		}
	})
	hist(seq, waited, time.Since(started), rc, "ran")
	tk.release(rf)
	return exitAs(rc, got)
}

type waiter struct {
	q      queue
	seq    int64
	tn     tunables
	stderr io.Writer
	start  time.Time

	lastPos int
	told    bool // 最初の待ちの行を出したか

}

func (w *waiter) logWake() {
	if w.tn.wakeLog == "" {
		return
	}
	if f, err := os.OpenFile(w.tn.wakeLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600); err == nil {
		_, _ = f.WriteString("w\n")
		_ = f.Close()
	}
}

// waitPredecessors は自分より前の札が無くなるまで待つ。
// 待ち方はイベント駆動: 自分の 1 つ前の札に blocking の flock をかけて眠り、持ち主が lock を外したら起きる
// (ユーザー指示 2026-10-22。ポーリングにしない)。新しく来た人は必ず大きい番号になるので、前の札は減る一方で、
// 1 つ前だけを見れば足りる。起きる理由が壊れていたとき (起こされない) の保険として、fallback ごとに列挙し直す。
func (w *waiter) waitPredecessors(deadline <-chan time.Time, sigs <-chan os.Signal) (waitResult, syscall.Signal) {
	fallback := time.NewTicker(w.tn.fallback)
	defer fallback.Stop()
	report := time.NewTicker(w.tn.report)
	defer report.Stop()

	var watching int64 = -1
	var woke, failed chan struct{}
	for {
		w.logWake()
		w.q.sweepStaleTmp()
		preds, err := w.q.predecessors(w.seq)
		if err == nil && len(preds) == 0 {
			return waitTurn, 0
		}
		// 1 つ前の札が flock で待てない (孤児・判定不能・一覧を読めない) ときは blocking で眠らず、短い間隔で見直す
		var recheck <-chan time.Time
		switch {
		case err != nil:
			watching, woke = -1, nil
			recheck = time.After(w.tn.recheck)
		case preds[len(preds)-1].state != stateAlive:
			w.reportPos(preds, false)
			watching, woke = -1, nil
			recheck = time.After(w.tn.recheck)
		default:
			w.reportPos(preds, false)
			if last := preds[len(preds)-1]; last.seq != watching {
				watching = last.seq
				woke, failed = make(chan struct{}), make(chan struct{})
				if !w.tn.noBlock {
					go blockUntilReleased(w.q.ticketPath(last.seq), woke, failed)
				}
			}
		}
	inner:
		for {
			select {
			case <-woke:
				watching = -1 // 起きたら同じ札でも眠り直す (起こした lock はもう放してある)
				break inner
			case <-failed:
				// 1 つ前の札で眠れなかった (開けない・flock の失敗): 短い間隔の見直しに切り替える
				failed, watching = nil, -1
				recheck = time.After(w.tn.recheck)
			case <-recheck:
				break inner
			case <-fallback.C:
				break inner
			case <-report.C:
				if p, err := w.q.predecessors(w.seq); err == nil {
					w.reportPos(p, true)
				}
			case <-deadline:
				return waitTimeout, 0
			case s := <-sigs:
				sig, _ := s.(syscall.Signal)
				return waitSignal, sig
			}
		}
	}
}

// acquireRun は run.lock を取る。前の札が無くなった後に取るので、普段はすぐ取れる
// (前の持ち主が札を消してから run.lock を放すまでの一瞬だけ待つ)
func (w *waiter) acquireRun(rf *os.File, deadline <-chan time.Time, sigs <-chan os.Signal) (waitResult, syscall.Signal) {
	got := make(chan error, 1)
	go blockLock(rf, got)
	report := time.NewTicker(w.tn.report)
	defer report.Stop()
	for {
		select {
		case err := <-got:
			if err != nil {
				fmt.Fprintf(w.stderr, "serial-run: run.lock の flock: %v\n", err)
				return waitError, 0
			}
			return waitTurn, 0
		case <-report.C:
			fmt.Fprintf(w.stderr, "serial-run: 実行権を待っている (%s)\n", fmtDur(time.Since(w.start)))
		case <-deadline:
			return waitTimeout, 0
		case s := <-sigs:
			sig, _ := s.(syscall.Signal)
			return waitSignal, sig
		}
	}
}

// reportPos は待ちの順番を stderr に出す。順番が変わったときと、定期 (periodic) のときだけ
func (w *waiter) reportPos(preds []pred, periodic bool) {
	pos := len(preds)
	if !periodic && pos == w.lastPos {
		return
	}
	w.lastPos = pos
	if pos == 0 {
		return
	}
	hint := ""
	if !w.told {
		// 最初の 1 行にだけ、外から様子を見る口を添える (背景に回った後の呼び出し側が、別の呼び出しで確かめられる)
		hint, w.told = " (状況は serial-run status)", true
	}
	fmt.Fprintf(w.stderr, "serial-run: 待ち %d 番目 (%s)%s%s\n", pos, fmtDur(time.Since(w.start)), w.describe(preds[0]), hint)
}

func (w *waiter) holderNote() string {
	preds, err := w.q.predecessors(w.seq)
	if err != nil || len(preds) == 0 {
		return ""
	}
	return w.describe(preds[0])
}

// describe は先頭 (走っているか、走る直前) の札の持ち主を 1 行で書く
func (w *waiter) describe(p pred) string {
	switch p.state {
	case stateUnknown:
		return fmt.Sprintf(" 前: 札 #%d を開けない (権限か fd の枯渇。生死を決められないので待ち続ける)", p.seq)
	case stateOrphan:
		info, _ := w.q.readInfo(p.seq)
		return fmt.Sprintf(" 前: #%d の serial-run は死んだが、子 pid %d (%s) がまだ走っている", p.seq, info.ChildPid, info.Cmd)
	}
	info, ok := w.q.readInfo(p.seq)
	if !ok {
		return ""
	}
	age, long := info.age(true) // describe は先頭の札にだけ使う
	s := fmt.Sprintf(" 前: %s の %s (%s)", info.Cwd, info.Cmd, age)
	if long {
		s += fmt.Sprintf(" 🚨 長く持っている: pid %d %s", info.Pid, statNote(info.Pid))
	}
	return s
}

func statNote(pid int) string {
	st, known := procStat(pid)
	if !known || st == "" {
		return "(ps で見えない)"
	}
	return "stat=" + st
}

func newNonce() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// runChild は子を起こして待つ。
// 🚨 子を別のプロセスグループに置かない。別に置くと、呼び出し元がグループへ SIGKILL を送ったとき serial-run だけが死んで
// lock が外れ、子は lock の外で走り続けて次の待ち手と 2 本同時に走る。同じグループなら一緒に死ぬ (端末の job control も子に効く)。
// 🚨 lock と札の fd は子へ渡さない (os.OpenFile は O_CLOEXEC で開く)。渡すと子が残した背景プロセスが lock を握ったまま残る
func runChild(argv []string, held string, grace time.Duration, sigs <-chan os.Signal, stderr io.Writer, started func(pid int)) (int, syscall.Signal) {
	cmd := exec.Command(argv[0], argv[1:]...)
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), heldEnv+"="+held)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "serial-run: %v\n", err)
		return startErrorRC(err), 0
	}
	started(cmd.Process.Pid)
	tgt := proctree.Target{Root: cmd.Process.Pid, Group: false}
	done := make(chan int, 1)
	go func() { done <- waitStatus(cmd.Wait()) }()
	var got syscall.Signal
	var graceUp <-chan time.Time
	for {
		select {
		case rc := <-done:
			return rc, got
		case <-graceUp:
			// 猶予が切れるのと子が終わるのが同時なら、終わった方を採る (回収済みの pid を撃たない)
			select {
			case rc := <-done:
				return rc, got
			default:
			}
			// 端末の外からの INT / QUIT の後、猶予のうちに子が終わらなかった: 子孫ごと止める (pid 宛ての INT でも止められるように)
			tgt.Stop(stopGrace)
			return <-done, got
		case s := <-sigs:
			got, _ = s.(syscall.Signal)
			if got == syscall.SIGTERM || got == syscall.SIGHUP {
				// 止める指示: 子孫ごと止めてから返る
				tgt.Stop(stopGrace)
				return <-done, got
			}
			// INT / QUIT は子へ伝え直さない。端末の Ctrl-C や、グループ宛ての INT (runtimeout・CI) は同じグループの子にも直接届いていて、
			// 伝え直すと 2 回届く (2 回目を強制終了と読むプログラムがある)。
			// 🚨 端末の前景に居るときは猶予で止めない。Ctrl-C は子にも届いていて、それを自分で扱って走り続ける子 (REPL・長い後始末) を
			// 猶予で殺すことになる。端末の外 (pid 宛て・端末を持たない Claude の Bash) の INT だけ、猶予の後に子孫ごと止める
			// (pid 宛てかグループ宛てかは Go からは見えないので、子が自分で終わるのを猶予の間は待つ。runtimeout と同じ考え方)
			if graceUp == nil && !inTerminalForeground() {
				graceUp = time.After(grace)
			}
		}
	}
}

// exitAs は子の rc を返す。道具がシグナルを受けて子もシグナルで死んだなら、道具も同じシグナルで死に直す
// (rc=128+n で返ると、呼び出し元の bash は「子がシグナルを処理した」と読んで次へ進む。runtimeout の exitAs と同じ)
// 🚨 QUIT では死に直さない。Go は SIGQUIT を既定に戻しても自前の扱い (goroutine のダンプを出して exit 2) をするので、rc が 2 に化ける
func exitAs(rc int, got syscall.Signal) int {
	if got == 0 || rc <= 128 || got == syscall.SIGQUIT {
		return rc
	}
	signal.Reset(got)
	_ = syscall.Kill(os.Getpid(), got)
	time.Sleep(100 * time.Millisecond) // 撃ったシグナルが届くまでの間 (届けばここへは戻らない)
	return rc
}

// age は表示する時間と「長く持っている」かを返す。走り出していれば子を起こしてから数える (待って先頭に上がった札を、走り出した直後から 🚨 にしない)。
// まだ走っていない札は札を出してから数え、🚨 は先頭のときだけ付ける。先頭は順番が来ればすぐ子を起こすので、走らないまま長いのは
// 持ち主が止まった (Ctrl-Z・SIGSTOP) か、run_start を書かない古い版が走らせている。待っている後ろの札には付けない
func (i ticketInfo) age(head bool) (label string, long bool) {
	if !i.RunStart.IsZero() {
		d := time.Since(i.RunStart)
		return "走り出してから " + fmtDur(d), d >= longHold
	}
	d := time.Since(i.Start)
	return "札を出してから " + fmtDur(d), head && d >= longHold
}

// printStatus は列を表示する。数えるのは待ち手と同じ入口 (lineup) で、孤児の子・開けない札も 1 本に数える
func printStatus(q queue, out, stderr io.Writer) int {
	preds, err := q.lineup()
	if err != nil {
		fmt.Fprintf(stderr, "serial-run: 札を読めない: %v\n", err)
		return rcTool
	}
	for i, p := range preds {
		label := "待ち"
		if i == 0 {
			label = "先頭"
		}
		if p.state == stateUnknown {
			fmt.Fprintf(out, "%s\t#%d\t(札を開けない。権限か fd の枯渇。生死を決められない)\n", label, p.seq)
			continue
		}
		info, ok := q.readInfo(p.seq)
		if !ok {
			fmt.Fprintf(out, "%s\t#%d\t(札の情報を読めない)\n", label, p.seq)
			continue
		}
		age, long := info.age(i == 0)
		line := fmt.Sprintf("%s\t#%d\t%s\tpid %d\t%s\t%s", label, p.seq, age, info.Pid, info.Cwd, info.Cmd)
		switch {
		case p.state == stateOrphan:
			line += fmt.Sprintf("\t(持ち主の serial-run は死んだ。子 pid %d がまだ走っている)", info.ChildPid)
		case long:
			line += "\t🚨 長く持っている " + statNote(info.Pid)
		}
		fmt.Fprintln(out, line)
	}
	if len(preds) == 0 {
		fmt.Fprintln(out, "走っているものも待っているものも無い")
	}
	// hook の最近の判断 (差し込んだ / 読み切れず見送った)。効いているかを後から確かめる口
	if b, err := os.ReadFile(q.hookLogPath()); err == nil {
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
		fmt.Fprintln(out, "hook の最近の判断:")
		for _, l := range lines {
			fmt.Fprintln(out, "  "+l)
		}
	}
	return 0
}

// displayCmd は表示用のコマンド行。hook が包んだ `zsh -c '<元のコマンド>'` は元のコマンドを見せる
func displayCmd(argv []string) string {
	s := strings.Join(argv, " ")
	if len(argv) == 3 && argv[1] == "-c" && (filepath.Base(argv[0]) == "zsh" || filepath.Base(argv[0]) == "bash" || filepath.Base(argv[0]) == "sh") {
		s = argv[2]
	}
	s = oneLine(s)
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	return fmt.Sprintf("%d 分 %02d 秒", int(d.Minutes()), int(d.Seconds())%60)
}

func parseArgs(args []string) (options, error) {
	opt := options{lock: "default", maxWait: defaultMaxWait}
	if len(args) > 0 && args[0] == "status" {
		opt.status = true
		args = args[1:]
	}
	for len(args) > 0 {
		a := args[0]
		switch a {
		case "--":
			args = args[1:]
			goto done
		case "-l", "--max-wait":
			if len(args) < 2 {
				return opt, fmt.Errorf("%s に値が無い", a)
			}
			if a == "-l" {
				if !lockNameRe.MatchString(args[1]) {
					return opt, fmt.Errorf("lock 名は英数字と . _ - だけ: %q", args[1])
				}
				opt.lock = args[1]
			} else {
				d, err := parseSeconds(args[1])
				if err != nil {
					return opt, fmt.Errorf("--max-wait: %w", err)
				}
				opt.maxWait = d
			}
			args = args[2:]
		default:
			// 未知のフラグで即終了するのは go_autobuild の温め `--__autobuild_warmup__` の要件
			return opt, fmt.Errorf("未知の引数: %s (コマンドの前に -- が要る)", a)
		}
	}
done:
	if opt.status {
		if len(args) > 0 {
			return opt, errors.New("status はコマンドを取らない")
		}
		return opt, nil
	}
	if len(args) == 0 {
		return opt, errors.New("-- の後にコマンドが要る")
	}
	opt.argv = args
	return opt, nil
}

func parseSeconds(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > math.MaxInt64/float64(time.Second) {
		return 0, fmt.Errorf("秒は 0 以上の数: %q", s)
	}
	return time.Duration(f * float64(time.Second)), nil
}

func startErrorRC(err error) int {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return rcNotFound
	}
	return rcCannotExec
}

// waitStatus は Wait の結果をシェルと同じ rc に直す (シグナルで死んだら 128+n)
func waitStatus(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	return rcCannotExec
}
