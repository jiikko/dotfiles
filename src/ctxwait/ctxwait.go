// Package ctxwait は、取り消しで抜けられる待ち (時間の待ち・他のプロセスが持つ flock の待ち) を 1 か所に持つ。
package ctxwait

import (
	"context"
	"errors"
	"syscall"
	"time"
)

// Sleep は d だけ待つ。ctx が先に終われば ctx.Err() を返す。d が 0 以下なら待たずに ctx.Err() を返す。
func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Flock は fd に how (syscall.LOCK_EX か LOCK_SH) の flock を取る。他のプロセスが持っていれば wait(poll) で待ってから取り直す。
// wait が誤りを返したら (取り消し・中断)、その誤りをそのまま返す。EWOULDBLOCK 以外の flock の失敗は syscall.Errno のまま返す
// (呼び出し元は errors.As で wait の誤りと分けて包める)。
//
// 待ちを flock の外で行うのは、LOCK_NB を外した素の flock は ctx でも Ctrl-C でも抜けられないため。
// 待ちを関数で受けるのは、待ち方 (ctx・プロセスの中断の合図・テストの差し替えた時計) が呼び出し元ごとに違うため。
func Flock(fd, how int, poll time.Duration, wait func(time.Duration) error) error {
	for {
		err := syscall.Flock(fd, how|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		if err := wait(poll); err != nil {
			return err
		}
	}
}
