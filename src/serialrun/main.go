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
	noBlock  bool          // blocking の待ちを外し、保険の見直しだけで進むか (保険が効くことを確かめるため)
	wakeLog  string        // 待ちのループが回るたびに 1 行足すファイル (起きた回数を数えるため)
}

func loadTunables() tunables {
	t := tunables{fallback: fallbackPoll, report: reportInterval, recheck: recheckInterval}
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
	if err != nil {
		fmt.Fprintf(stderr, "serial-run: 状態の置き場: %v\n", err)
		return rcTool
	}
	q, err := openQueue(filepath.Join(base, opt.lock))
	if err != nil {
		fmt.Fprintf(stderr, "serial-run: 状態の置き場を作れない: %v\n", err)
		return rcTool
	}
	if opt.status {
		printStatus(q, os.Stdout)
		return 0
	}
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
	preds, err := q.predecessors(math.MaxInt64)
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
	fmt.Fprintf(stderr, "serial-run: %v\n", err)
	return rcCannotExec
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
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	// 起動時に無視されていた HUP は Notify しない (無視のまま子へ渡す。runtimeout の注記と同じ: nohup の run を HUP で止めない)
	if !signal.Ignored(syscall.SIGHUP) {
		signal.Notify(sigs, syscall.SIGHUP)
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
	tf, seq, err := q.enqueue(info, func() error {
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
		return exitAs(128+int(abortSig), abortSig)
	case errors.Is(err, errMaxWait):
		fmt.Fprintf(stderr, "serial-run: 待ちの上限 (%s) を超えた。採番の lock が空かない。実行しない\n", opt.maxWait)
		return rcTool
	default:
		fmt.Fprintf(stderr, "serial-run: 札を出せない: %v\n", err)
		return rcTool
	}
	info.Seq = seq
	myPath := q.ticketPath(seq)
	leave := func() {
		_ = os.Remove(myPath)
		_ = tf.Close()
	}

	rf, err := os.OpenFile(q.runPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		leave()
		fmt.Fprintf(stderr, "serial-run: run.lock を開けない: %v\n", err)
		return rcTool
	}

	w := waiter{q: q, seq: seq, tn: tn, stderr: stderr, start: enq}
	res, got := w.waitPredecessors(deadline, sigs)
	if res == waitTurn {
		res, got = w.acquireRun(rf, deadline, sigs)
	}
	switch res {
	case waitTimeout:
		leave()
		_ = rf.Close()
		fmt.Fprintf(stderr, "serial-run: 待ちの上限 (%s) を超えた。実行しない%s\n", opt.maxWait, w.holderNote())
		return rcTool
	case waitError:
		leave()
		_ = rf.Close()
		return rcTool
	case waitSignal:
		leave()
		_ = rf.Close()
		return exitAs(128+int(got), got)
	}
	waited := time.Since(enq)
	if waited >= time.Second {
		fmt.Fprintf(stderr, "serial-run: 実行 (待ち %s)\n", fmtDur(waited))
	}

	started := time.Now()
	rc, got := runChild(opt.argv, heldToken(q.dir, seq, info.Nonce), sigs, stderr, func(pid int) {
		// 🚨 pid を先に書く。ps (開始時刻) を待ってから書くと、その間に serial-run だけが SIGKILL されたとき、子の情報が無い札として消され、
		// 走り続ける子の後ろで次の 1 本が走る
		info.ChildPid = pid
		err := rewriteInfo(tf, info)
		if err == nil {
			// ps は負荷で時間切れになりうるので数回試す。取れなくても子は走らせる (開始時刻の無い札は、札より後に起きた pid かで同一性を見る)
			for i := range 3 {
				if start, known := procStart(pid); known && start != "" {
					info.ChildStartUTC = start
					// 古い版との互換 (queue.go の ChildStartUTC の注記)。取れなくても今の版は困らない
					info.ChildStart, _ = psFieldEnv(pid, "lstart", false)
					err = rewriteInfo(tf, info)
					break
				}
				if i == 2 {
					err = errors.New("ps で子の開始時刻を取れない")
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		if err != nil {
			fmt.Fprintf(stderr, "serial-run: 札に子の情報を書けない (この serial-run だけが殺されたとき、子の同一性を確かめにくくなる): %v\n", err)
		}
	})
	q.appendHistory(seq, enq, waited, time.Since(started), rc, cwd, info.Cmd)
	// 🚨 札を消してから run.lock を放す。後ろの待ち手は札の flock で起きて run.lock を取りに来るので、逆にすると
	// 札が残ったまま run.lock が空き、起きた待ち手がまだ前に札があると読んで眠り直す (遅れるだけで壊れはしない)
	leave()
	_ = rf.Close()
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
	held := time.Since(info.Start)
	s := fmt.Sprintf(" 前: %s の %s (札を出してから %s)", info.Cwd, info.Cmd, fmtDur(held))
	if held >= longHold {
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
func runChild(argv []string, held string, sigs <-chan os.Signal, stderr io.Writer, started func(pid int)) (int, syscall.Signal) {
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
	for {
		select {
		case rc := <-done:
			return rc, got
		case s := <-sigs:
			got, _ = s.(syscall.Signal)
			if got == syscall.SIGTERM || got == syscall.SIGHUP {
				// 止める指示: 子孫ごと止めてから返る
				tgt.Stop(stopGrace)
				return <-done, got
			}
			// INT / QUIT は端末から同じグループの子にも届いている (伝え直すと 2 回届く)。子の終わりを待つ
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

func (q queue) appendHistory(seq int64, enq time.Time, waited, ran time.Duration, rc int, cwd, cmd string) {
	f, err := os.OpenFile(filepath.Join(q.dir, "history.log"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	// O_APPEND の 1 回の write で書く (並行する書き手と行が混ざらない)
	_, _ = fmt.Fprintf(f, "%d\t%s\twait=%.1fs\tran=%.1fs\trc=%d\t%s\t%s\n",
		seq, enq.Format(time.RFC3339), waited.Seconds(), ran.Seconds(), rc, cwd, oneLine(cmd))
}

func printStatus(q queue, out io.Writer) {
	seqs, err := q.tickets()
	if err != nil {
		fmt.Fprintf(out, "札を読めない: %v\n", err)
		return
	}
	n := 0
	for _, s := range seqs {
		info, ok := q.readInfo(s)
		if !ok {
			continue
		}
		f, err := os.Open(q.ticketPath(s))
		if err != nil {
			continue
		}
		dead := flock(f, syscall.LOCK_EX|syscall.LOCK_NB) == nil
		_ = f.Close()
		state := "待ち"
		switch {
		case dead:
			state = "(持ち主なし)"
		case n == 0:
			state = "先頭"
		}
		if !dead {
			n++
		}
		held := time.Since(info.Start)
		line := fmt.Sprintf("%s\t#%d\t%s\tpid %d\t%s\t%s", state, s, fmtDur(held), info.Pid, info.Cwd, info.Cmd)
		if !dead && held >= longHold {
			line += "\t🚨 長く持っている " + statNote(info.Pid)
		}
		fmt.Fprintln(out, line)
	}
	if n == 0 {
		fmt.Fprintln(out, "走っているものも待っているものも無い")
	}
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
