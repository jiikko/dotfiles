package main

import (
	"errors"
	"fmt"
	"time"
)

// errIOTimeout は「期限内に返らなかった」= 判定不能。**「空いている」に倒さない**ため、
// 呼び出し側は errors.Is でこれを見分けて 091:398-399 の終了コードへ落とす。
var errIOTimeout = errors.New("I/O timeout")

// I/O の包み。**`l.timeout` を読んでよいのはこのファイルだけ**にする。
//
// 🚨 以前は包みが `main.go` の `timed()` だけにあり、呼び出し側が包むかどうかを
// 選べる形だった。結果、`runWith` の Acquire / Renew / Release と `dispatch` の
// deferred Cleanup の 4 経路が**素通り**していた (issue 357)。`--io-timeout` の目的は
// 「スクリプトが無言で固まらない」なので、包み忘れが 1 つでもあると機構ごと無意味になる。
// 包みを Locker のメソッド側に寄せると、生の I/O を呼ぶ形がコード上に残らない。
//
// 🚨 **止まるのは「呼び出し側が待つ時間」までで、固まった goroutine は回収できない**
// (ブロック中の syscall は中断できない)。見捨てた goroutine が後から lock を置く形は
// 別問題として issue 362 が扱う。

// withTimeout は fn を別 goroutine で走らせ、期限を超えたら timeout エラーを返す。
//
// smbfs はサーバ不達で長時間ブロックする。スクリプトの中で無言のまま固まるのが最悪なので、
// 「固まった」を「空いている」と混同せず、判定不能として返せるようにする。
// 🚨 固まった goroutine は回収できない (ブロック中の syscall は中断できない)。
// プロセスの終了で解放される前提の使い捨て。
func withTimeout[T any](d time.Duration, fn func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.val, r.err
	case <-time.After(d):
		var zero T
		return zero, fmt.Errorf("%w: I/O が %v 以内に返らない (マウントが応答しない可能性): 判定不能", errIOTimeout, d)
	}
}

// AcquireTimed 以下は Locker の I/O を --io-timeout で包んだ入口。
// 本体 (Acquire / Renew / ...) を直接呼ぶのは、包みが要らないと分かっている場所だけ。

func (l *Locker) AcquireTimed(ttl time.Duration, label string) (*Meta, error) {
	return withTimeout(l.timeout, func() (*Meta, error) { return l.Acquire(ttl, label) })
}

func (l *Locker) RenewTimed(token string) error {
	_, err := withTimeout(l.timeout, func() (struct{}, error) { return struct{}{}, l.Renew(token) })
	return err
}

func (l *Locker) ReleaseTimed(token string) error {
	_, err := withTimeout(l.timeout, func() (struct{}, error) { return struct{}{}, l.Release(token) })
	return err
}

func (l *Locker) InspectTimed() (*State, error) {
	return withTimeout(l.timeout, func() (*State, error) { return l.Inspect() })
}

func (l *Locker) BreakTimed() error {
	_, err := withTimeout(l.timeout, func() (struct{}, error) { return struct{}{}, l.Break() })
	return err
}

// CleanupTimed は掃除を包む。**タイムアウトは致命にしない** — 掃除は正しさに関与しない
// 設計 (cleanup.go の 🚨) なので、失敗は件数と一緒に res.Errors へ入れて呼び出し側に見せる。
func (l *Locker) CleanupTimed(force bool) CleanupResult {
	res, err := withTimeout(l.timeout, func() (CleanupResult, error) { return l.Cleanup(force), nil })
	if err != nil {
		return CleanupResult{Errors: []string{err.Error()}}
	}
	return res
}
