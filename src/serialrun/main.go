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
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"proctree"
	"proctree/childrun"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	rcTool       = 125
	rcCannotExec = childrun.RCCannotExec
	rcNotFound   = childrun.RCNotFound

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

// defaultLock はマシン全体の列の lock 名 (SERIAL_RUN_SCOPE=machine・git の repo の外)。-l を省いたときの既定は repo ごとの列 (scope.go)
const defaultLock = "default"

// usage の既定値は定数から作る (文面に複写すると定数を変えたときに食い違う)
var usage = fmt.Sprintf(`usage: serial-run [-l <lock名>] [--max-wait <秒>] -- <コマンド> [引数…]
       serial-run status [-l <lock名>]
       serial-run hook   (Claude Code の PreToolUse(Bash) hook。重いコマンドの前に serial-run -- を差し込む)
  -l          lock の名前 (既定は cwd の git の repo ごとの列。SERIAL_RUN_SCOPE=machine か repo の外ならマシン全体の %s)
  --max-wait  待ちの上限 (秒。既定 %d。0 は上限なし)。超えたら実行せずに rc=%d
rc: 子の rc / %d 道具の失敗 / %d 起動できない / %d 見つからない
hook を止める: Claude Code の環境に SERIAL_RUN_HOOK_DISABLE=1
`, defaultLock, int(defaultMaxWait.Seconds()), rcTool, rcTool, rcCannotExec, rcNotFound)

// notRunHint は待ちの上限で抜けたときの、次に取れる手
const notRunHint = "コマンドは実行していない。もう一度打つと列の最後尾に並び直す。--max-wait 0 なら上限なしで待つ"

// errHelp は -h / --help (usage を stdout に出して rc=0)
var errHelp = errors.New("help")

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
	if errors.Is(err, errHelp) {
		fmt.Fprint(os.Stdout, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "serial-run: %v\n%s", err, usage)
		return rcTool
	}
	if opt.lock == "" {
		cwd, _ := os.Getwd()
		lock, lerr := lockFor(cwd)
		if lerr != nil {
			// 範囲の値の誤りで包んだコマンドを走らせないのは害が大きい (runUnqueued と同じ考え方)。マシン全体の列に並ぶ
			fmt.Fprintf(stderr, "serial-run: 🚨 %v。マシン全体の列に並ぶ\n", lerr)
		}
		opt.lock = lock
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
	fmt.Fprintf(out, "lock: %s\n", lock)
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
	if q.heldByAncestor(os.Getenv(heldEnv)) || q.heldByAncestorProcess() || heldInOtherLock(filepath.Dir(q.dir)) {
		return execDirect(opt.argv, stderr)
	}
	return runQueued(q, opt, loadTunables(), stderr)
}

// heldInOtherLock は、別の列 (別の repo の列・default・-l で名指しした列) の serial-run の中で走っているか。中なら 2 本目の列は取らずに素通しにする。
// 🚨 列を持ったまま別の列を待たない (1 つのプロセスの木が持つ列は高々 1 本)。別の repo へ cd して打った make test が A の列を持ったまま
// B の列を待つと、逆向き (B の中から A) と組んで互いに待ち合い、両方が待ちの上限まで止まる。中の処理は外側の列の 1 本として走る。
// -l で名指しした列も同じ扱い: 名指しの列と範囲の列を逆の順で入れ子にした 2 本も互いに待ち合うので、入れ子の中では名指しの列にも並ばない
// (その資源の直列は、入れ子の外から打った分にだけ効く)。
// 祖先で見る枝は、持ち主が生きている (flock を持つ) 先頭だけを数える。開始時刻で確かめられない孤児の札は、子の pid の使い回しで
// 関係の無い祖先 (シェル・Claude Code) と一致しうるので、全部の列の素通しの根拠にしない
func heldInOtherLock(base string) bool {
	if v := os.Getenv(heldEnv); v != "" {
		if dir, _, _, ok := parseHeld(v); ok {
			if q, err := openExistingQueue(dir); err == nil && q.heldByAncestor(v) {
				return true
			}
		}
	}
	ents, err := os.ReadDir(base)
	if err != nil {
		return false
	}
	var anc map[int]bool // 祖先の一覧は ps を段の数だけ呼ぶので、要るときに 1 回だけ取る
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		q, err := openExistingQueue(filepath.Join(base, e.Name()))
		if err != nil {
			continue
		}
		preds, err := q.lineup()
		if err != nil || len(preds) == 0 || preds[0].state != stateAlive {
			continue
		}
		info, ok := q.readInfo(preds[0].seq)
		if !ok || info.Pid <= 1 {
			continue
		}
		if anc == nil {
			anc = ancestors()
		}
		if anc[info.Pid] {
			return true
		}
	}
	return false
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
		return childrun.StartErrorRC(err)
	}
	err = syscall.Exec(path, argv, os.Environ())
	// 子を起こす経路 (runChild) と同じ rc にする (見つからない 127 / 実行できない 126)
	fmt.Fprintf(stderr, "serial-run: %s: %v\n", path, err)
	return childrun.StartErrorRC(err)
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
		return childrun.StartErrorRC(err)
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
		fmt.Fprintf(stderr, "serial-run: 待ちをやめた (シグナル %v を受けた。コマンドは実行していない)\n", abortSig)
		hist(0, time.Since(enq), -1, 128+int(abortSig), "signal")
		return childrun.ExitAs(128+int(abortSig), abortSig)
	case errors.Is(err, errMaxWait):
		fmt.Fprintf(stderr, "serial-run: 待ちの上限 (%s) を超えた。採番の lock が空かない。%s\n", fmtDur(opt.maxWait), notRunHint)
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
	// 待ちで抜けるときは run.lock を閉じない (release(nil))。run.lock の待ち (acquireRun) で抜けると、別の goroutine が同じ fd で
	// blocking の flock に入ったままで、os.File の Close はその呼び出しが返るまで待つので、serial-run が終わらなくなる。
	// 札は消し、run.lock の fd はプロセスの終了でカーネルが閉じる (flock も取れていないので外すものは無い)
	switch res {
	case waitTimeout:
		note := w.holderNote()
		tk.release(nil)
		fmt.Fprintf(stderr, "serial-run: 待ちの上限 (%s) を超えた。%s%s\n", fmtDur(opt.maxWait), notRunHint, note)
		hist(seq, time.Since(enq), -1, rcTool, "timeout")
		return rcTool
	case waitError:
		tk.release(nil)
		hist(seq, time.Since(enq), -1, rcTool, "error")
		return rcTool
	case waitSignal:
		tk.release(nil)
		fmt.Fprintf(stderr, "serial-run: 待ちをやめた (シグナル %v を受けた。コマンドは実行していない)\n", got)
		hist(seq, time.Since(enq), -1, 128+int(got), "signal")
		return childrun.ExitAs(128+int(got), got)
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
	return childrun.ExitAs(rc, got)
}

type waiter struct {
	q      queue
	seq    int64
	tn     tunables
	stderr io.Writer
	start  time.Time

	lastPos int
	lastKey string // 先頭の説明つきの行を最後に出したときの順番・先頭の札・状態・長く持っているか
	told    bool   // 最初の待ちの行を出したか

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
		// 最初の 1 行にだけ、外から様子を見る口と止め方を添える (背景に回った後の呼び出し側が、別の呼び出しで確かめられる)
		hint, w.told = " (状況は serial-run status。待ちをやめるなら Ctrl-C。コマンドは実行されない)", true
	}
	// 周期の行は短くする: 先頭の説明を出すのは、順番・先頭の札・その状態 (走っている / 孤児 / 開けない)・長く持っているかのどれかが
	// 変わったときだけ (1 時間待つと 120 行になり、毎回全文だと tool の結果が数十 KB になる)。経過の秒は毎回変わるので比べない
	desc, long := w.describe(preds[0])
	key := fmt.Sprintf("%d/%d/%d/%v", pos, preds[0].seq, preds[0].state, long)
	switch {
	case !periodic || key != w.lastKey:
		w.lastKey = key
	case long:
		desc = " 🚨 先頭が長く持っている (詳しくは serial-run status)"
	default:
		desc = ""
	}
	fmt.Fprintf(w.stderr, "serial-run: 待ち %d 番目 (%s)%s%s\n", pos, fmtDur(time.Since(w.start)), desc, hint)
}

func (w *waiter) holderNote() string {
	preds, err := w.q.predecessors(w.seq)
	if err != nil || len(preds) == 0 {
		return ""
	}
	desc, _ := w.describe(preds[0])
	return desc
}

// describe は先頭 (走っているか、走る直前) の札の持ち主を 1 行で書き、長く持っているかを返す
// (長く持っているかを文面から読まない。cwd やコマンド行に 🚨 が入りうる)
func (w *waiter) describe(p pred) (string, bool) {
	switch p.state {
	case stateUnknown:
		return fmt.Sprintf(" 前: 札 #%d を開けない (権限か fd の枯渇。生死を決められないので待ち続ける)", p.seq), false
	case stateOrphan:
		info, _ := w.q.readInfo(p.seq)
		return fmt.Sprintf(" 前: #%d の serial-run は死んだが、子 pid %d (%s) がまだ走っている", p.seq, info.ChildPid, printable(info.Cmd)), false
	}
	info, ok := w.q.readInfo(p.seq)
	if !ok {
		return "", false
	}
	// describe は先頭の札にだけ使う
	s := " 前: " + holderSummary(info, true)
	n := longHoldNote(info, true)
	if n != "" {
		s += " " + n
	}
	return s, n != ""
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
		return childrun.StartErrorRC(err), 0
	}
	started(cmd.Process.Pid)
	tgt := proctree.Target{Root: cmd.Process.Pid, Group: false}
	done := make(chan int, 1)
	go func() { done <- childrun.WaitStatus(cmd.Wait()) }()
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

// printStatus は列を表示する。数えるのは待ち手と同じ入口 (lineup) で、孤児の子・開けない札も 1 本に数える
func printStatus(q queue, out, stderr io.Writer) int {
	preds, err := q.lineup()
	if err != nil {
		fmt.Fprintf(stderr, "serial-run: 札を読めない: %v\n", err)
		return rcTool
	}
	// 列は 状態 (全角 2 字)・番号・pid の固定の 3 つを先に置き、長さの揃わない説明は最後にまとめる
	// (全角と半角を同じ列に混ぜない。経過・cwd・コマンドの長さはまちまちなので桁を揃える列にしない)
	if len(preds) > 0 {
		fmt.Fprintln(out, "状態\t番号\tpid\t持ち主")
	}
	for i, p := range preds {
		head := i == 0
		label := "待ち"
		if head {
			label = "先頭"
		}
		if p.state == stateUnknown {
			fmt.Fprintf(out, "%s\t#%d\t-\t(札を開けない。権限か fd の枯渇。生死を決められない)\n", label, p.seq)
			continue
		}
		info, ok := q.readInfo(p.seq)
		if !ok {
			fmt.Fprintf(out, "%s\t#%d\t-\t(札の情報を読めない)\n", label, p.seq)
			continue
		}
		line := fmt.Sprintf("%s\t#%d\t%d\t%s", label, p.seq, info.Pid, holderSummary(info, head))
		if p.state == stateOrphan {
			line += fmt.Sprintf(" (持ち主の serial-run は死んだ。子 pid %d がまだ走っている)", info.ChildPid)
		} else if n := longHoldNote(info, head); n != "" {
			line += " " + n
		}
		fmt.Fprintln(out, line)
	}
	if len(preds) == 0 {
		fmt.Fprintln(out, "走っているものも待っているものも無い")
	}
	// hook の最近の判断 (差し込んだ / 読み切れず見送った)。効いているかを後から確かめる口
	if b, err := os.ReadFile(hookLogPath(filepath.Dir(q.dir))); err == nil {
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
		fmt.Fprintln(out, "hook の最近の判断:")
		for _, l := range lines {
			// 古い版や他の書き手が書いた行も、制御文字のまま端末へ出さない。区切りのタブは残す (欄ごとに落とす)
			cols := strings.Split(l, "\t")
			for i := range cols {
				cols[i] = printable(cols[i])
			}
			fmt.Fprintln(out, "  "+strings.Join(cols, "\t"))
		}
	}
	return 0
}

func parseArgs(args []string) (options, error) {
	opt := options{maxWait: defaultMaxWait}
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
		case "-h", "--help":
			return opt, errHelp
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
				d, err := childrun.ParseSeconds(args[1])
				if err != nil {
					return opt, fmt.Errorf("--max-wait: %w", err)
				}
				opt.maxWait = d
			}
			args = args[2:]
		default:
			// 未知のフラグで即終了するのは go_autobuild の温め `--__autobuild_warmup__` の要件
			if strings.HasPrefix(a, "-") {
				return opt, fmt.Errorf("未知のフラグ: %s (コマンドは -- の後に書く)", a)
			}
			return opt, fmt.Errorf("コマンドは -- の後に書く: %s", a)
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
