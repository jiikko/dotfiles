// runtimeout — コマンドを時間の上限付きで実行し、時間が来たら子孫ごと止める (coreutils の timeout 相当)。
//
//	runtimeout [-f] [-k <猶予秒>] <秒> <コマンド> [引数…]
//
// rc: 子の rc (シグナルで死んだら 128+n) / 124 時間切れ / 125 道具の誤用 / 126 起動できない / 127 見つからない。
// 設計と置き換えた手組みの一覧は issue 640。
package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"proctree"
	"strconv"
	"syscall"
	"time"
)

const (
	rcTimeout    = 124
	rcUsage      = 125
	rcCannotExec = 126
	rcNotFound   = 127

	defaultGrace = 5 * time.Second
)

const usage = `usage: runtimeout [-f] [-k <猶予秒>] <秒> <コマンド> [引数…]
  <秒>  上限 (小数可)。0 は上限なし
  -f    子を呼び出し元のプロセスグループのまま起こす (端末の job control に残す。止めるのは ps でたどった木だけ)
  -k    止めるときに TERM から KILL までの猶予 (既定 5 秒)
rc: 子の rc / 124 時間切れ / 125 誤用 / 126 起動できない / 127 見つからない
`

type options struct {
	limit      time.Duration // 0 = 上限なし
	grace      time.Duration
	foreground bool // -f: 子を専用グループに置かない
	argv       []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	opt, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "runtimeout: %v\n%s", err, usage)
		return rcUsage
	}

	// 🚨 Notify は Start より前に張る。張る前に届いたシグナルは道具だけを殺し、子が残る。
	// Notify した INT / QUIT は、bash の非対話の `&` が SIG_IGN で渡してきても既定に戻って子へ渡る
	// (Go は fork した子で、自分が扱うシグナルを SIG_DFL に戻す。無視のままのものは無視のまま)。
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT)
	// TERM は常に捕まえる (Go の runtime は起動時に TERM が無視されていても自分のハンドラを張るので、子には既定で渡る)。
	signal.Notify(sigs, syscall.SIGTERM)
	// 🚨 起動時に無視されていた HUP は Notify しない (無視のまま子へ渡る)。Notify すると無視が外れ、
	// `nohup codex-fanout …` の run が HUP で止まる (codex-drive の 900 秒超の run はこの形で起動する)。
	// INT / QUIT は無視されていても Notify する: 非対話の bash の `&` は INT / QUIT を無視して子を起こすので、
	// 戻さないと検証コマンドの Ctrl-C や trap … INT が効かない (bin/mutate-verify の run_in_wt の注記)
	if !signal.Ignored(syscall.SIGHUP) {
		signal.Notify(sigs, syscall.SIGHUP)
	}

	cmd := exec.Command(opt.argv[0], opt.argv[1:]...)
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil // PATH の . / 空要素で見つかったものも起動する (coreutils の timeout と同じ)
	}
	// *os.File を渡すと Go はパイプを挟まず fd をそのまま子へ渡す (EOF・パイプの意味を変えない)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if !opt.foreground {
		// 子を専用のグループに置く。道具自身は呼び出し元のグループに残る
		// 🚨 端末の job control から外れる (tostop の端末で子の出力が SIGTTOU で止まる / Ctrl-Z で子が止まらない)。
		//    端末に出力しうる子を対話で走らせる呼び出し側は -f を使う (bin/mutate-verify)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "runtimeout: %v\n", err)
		return startErrorRC(err)
	}
	tgt := proctree.Target{Root: cmd.Process.Pid, Group: !opt.foreground}

	done := make(chan int, 1)
	go func() { done <- waitStatus(cmd.Wait()) }()

	var deadline <-chan time.Time
	if opt.limit > 0 {
		t := time.NewTimer(opt.limit)
		defer t.Stop()
		deadline = t.C
	}

	var graceUp <-chan time.Time
	var got syscall.Signal // 道具が受けたシグナル (最後のもの)
	for {
		select {
		case rc := <-done:
			return exitAs(rc, got)
		case <-deadline:
			// 子の終了と上限が同時に来たら終了を採る (select は準備できた case をランダムに選ぶ)
			select {
			case rc := <-done:
				return exitAs(rc, got)
			default:
			}
			tgt.Stop(opt.grace)
			<-done
			return rcTimeout
		case s := <-sigs:
			got, _ = s.(syscall.Signal)
			switch {
			case got == syscall.SIGTERM || got == syscall.SIGHUP:
				// 止める指示: 転送より先に凍らせて集める (転送してから待つと、途中の親が先に死んで
				// setsid した孫が init へ付け替わり、木から外れる)。止め終わるまで返らない
				// (この窓は数 ms で外から決定論的に作れないので、テストでは固定していない。mutate-verify の stop_tree と同じ)
				tgt.Stop(opt.grace)
				return exitAs(<-done, got)
			case opt.foreground:
				// -f の INT / QUIT は端末から子にも直接届いている。伝え直すと 2 回届くので伝えない
			default:
				tgt.Forward(got)
			}
			if graceUp == nil {
				graceUp = time.After(opt.grace)
			}
		case <-graceUp:
			// シグナルを伝えても猶予の間に終わらなかった: 時間切れと同じ止め方で止める
			tgt.Stop(opt.grace)
			return exitAs(<-done, got)
		}
	}
}

// exitAs は子の rc を返す。ただし道具がシグナルを受けて子もシグナルで死んだなら、道具も同じシグナルで死に直す。
// rc=128+n で返ると、呼び出し元の bash は「子が INT を処理した」と読んで次へ進む (bash の wait-and-cooperative-exit)。
// 呼び出し元が元々そのシグナルを無視していたなら Reset で無視に戻り、死なずに 128+n を返す
func exitAs(rc int, got syscall.Signal) int {
	if got == 0 || rc <= 128 {
		return rc
	}
	signal.Reset(got)
	_ = syscall.Kill(os.Getpid(), got)
	time.Sleep(100 * time.Millisecond) // 撃ったシグナルが届くまでの間 (届けばここへは戻らない)
	return rc
}

func parseArgs(args []string) (options, error) {
	opt := options{grace: defaultGrace}
	for len(args) > 0 {
		a := args[0]
		if a == "--" {
			args = args[1:]
			break
		}
		if a == "-f" {
			opt.foreground = true
			args = args[1:]
			continue
		}
		if a == "-k" {
			if len(args) < 2 {
				return opt, errors.New("-k に値が無い")
			}
			d, err := parseSeconds(args[1])
			if err != nil {
				return opt, fmt.Errorf("-k: %w", err)
			}
			opt.grace = d
			args = args[2:]
			continue
		}
		// 負の数は秒として不正なので、- で始まるものは全部フラグとして扱う
		// (未知のフラグで即終了するのは go_autobuild の温め `--__autobuild_warmup__` の要件)
		if len(a) > 1 && a[0] == '-' {
			return opt, fmt.Errorf("未知のフラグ: %s", a)
		}
		break
	}
	if len(args) < 2 {
		return opt, errors.New("<秒> と <コマンド> が要る")
	}
	d, err := parseSeconds(args[0])
	if err != nil {
		return opt, err
	}
	opt.limit = d
	opt.argv = args[1:]
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
