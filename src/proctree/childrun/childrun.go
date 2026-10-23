// Package childrun は、子を 1 本起こして待つ道具 (runtimeout・serialrun) の共通部:
// 秒の引数・起動の失敗の rc・子の終わり方の rc・道具の死に直し。
package childrun

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// シェルと同じ rc (起動できない / 見つからない)
const (
	RCCannotExec = 126
	RCNotFound   = 127
)

// ParseSeconds は秒 (小数可) を読む。0 は「上限なし」の意味で使われるので、0 より大きいのに 1ns 未満になる値は 1ns に切り上げる
// (切り捨てると `--max-wait 1e-10` が上限なしに化ける)
func ParseSeconds(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > math.MaxInt64/float64(time.Second) {
		return 0, fmt.Errorf("秒は 0 以上の数: %q", s)
	}
	d := time.Duration(f * float64(time.Second))
	if f > 0 && d == 0 {
		d = time.Nanosecond
	}
	return d, nil
}

// StartErrorRC は Start の失敗をシェルと同じ rc に直す (見つからない 127 / 実行できない 126)
func StartErrorRC(err error) int {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return RCNotFound
	}
	return RCCannotExec
}

// WaitStatus は Wait の結果をシェルと同じ rc に直す (シグナルで死んだら 128+n)
func WaitStatus(err error) int {
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
	return RCCannotExec
}

// ExitAs は子の rc を返す。ただし道具がシグナル got を受けて子もシグナルで死んだ (rc > 128) なら、道具も同じシグナルで死に直す。
// rc=128+n で返ると、呼び出し元の bash は「子がシグナルを処理した」と読んで次へ進む (bash の wait-and-cooperative-exit)。
// 呼び出し元が元々そのシグナルを無視していたなら Reset で無視に戻り、死なずに 128+n を返す。
// 🚨 QUIT では死に直さない。Go は SIGQUIT を既定に戻しても自前の扱い (goroutine のダンプを出して exit 2) をするので、rc が 2 に化ける
func ExitAs(rc int, got syscall.Signal) int {
	if got == 0 || rc <= 128 || got == syscall.SIGQUIT {
		return rc
	}
	signal.Reset(got)
	_ = syscall.Kill(os.Getpid(), got)
	time.Sleep(100 * time.Millisecond) // 撃ったシグナルが届くまでの間 (届けばここへは戻らない)
	return rc
}
