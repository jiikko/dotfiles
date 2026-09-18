package main

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// errIOTimeout は「期限内に返らなかった」= 判定不能。**「空いている」に倒さない**ため、
// 呼び出し側は errors.Is でこれを見分けて 091:398-399 の終了コードへ落とす。
var errIOTimeout = errors.New("I/O timeout")

// errAbandoned は「見捨てられたと気づいたので、副作用を残さずに降りた」。
// **呼び出し側はもう待っていない**ので誰も読まないが、`Acquire` の内部で
// 「busy でもエラーでもない第 3 の帰り方」を errBusy へ丸めないために要る。
var errAbandoned = errors.New("abandoned after I/O timeout")

// abandon は「呼び出し側はもう待っていない」を、見捨てられた goroutine 自身へ伝える
// (issue 362)。`withTimeout` が期限切れで諦めた瞬間に立ち、`fn` の側は**不可逆な操作の
// 直前**と**置いた直後**でこれを見る。
//
// 🚨 **nil を受けてよい**。包みの外から本体を直接呼ぶ経路 (テスト / 包みが要らない場所) は
// 見捨てられようが無いので、常に「見捨てられていない」を返す。
//
// 🚨 **窓は 0 にならない**。`fn` が期限ちょうどに完走した場合、select はどちらの case を
// 選ぶかを決めず、time.After 側が選ばれると mark はもう誰にも観測されない。
// 「ブロックしていた syscall 自身が遅かった」残りぶんも同じで、そこは TTL 頼みになる。
type abandon struct {
	flag atomic.Bool
}

// mark は「見捨てた」を立てる。`withTimeout` の期限切れの枝だけが呼ぶ。
func (a *abandon) mark() {
	if a != nil {
		a.flag.Store(true)
	}
}

// abandoned は既に見捨てられているかを返す。
func (a *abandon) abandoned() bool { return a != nil && a.flag.Load() }

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
func withTimeout[T any](d time.Duration, fn func(*abandon) (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	ab := &abandon{}
	ch := make(chan result, 1)
	go func() {
		v, err := fn(ab)
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.val, r.err
	case <-time.After(d):
		// 🚨 **エラーを返す前に立てる**。返してから立てると、呼び出し側が失敗を報告して
		// プロセスを畳み始めるまでのあいだ、見捨てられた goroutine は「まだ待たれている」と
		// 思ったまま不可逆な操作へ進める (issue 362 の窓をそのぶん広げる)。
		ab.mark()
		var zero T
		return zero, fmt.Errorf("%w: I/O が %v 以内に返らない (マウントが応答しない可能性): 判定不能", errIOTimeout, d)
	}
}

// AcquireTimed 以下は Locker の I/O を --io-timeout で包んだ入口。
// 本体 (Acquire / Renew / ...) を直接呼ぶのは、包みが要らないと分かっている場所だけ。

func (l *Locker) AcquireTimed(ttl time.Duration, label string) (*Meta, error) {
	return withTimeout(l.timeout, func(ab *abandon) (*Meta, error) { return l.acquire(ttl, label, ab) })
}

func (l *Locker) RenewTimed(token string) error {
	_, err := withTimeout(l.timeout, func(_ *abandon) (struct{}, error) { return struct{}{}, l.Renew(token) })
	return err
}

func (l *Locker) ReleaseTimed(token string) error {
	_, err := withTimeout(l.timeout, func(_ *abandon) (struct{}, error) { return struct{}{}, l.Release(token) })
	return err
}

// renewAsync は更新を **1 本だけ**起こし、結果の chan と期限の chan を返す。
//
// 🚨 `with` は lockman で唯一の長寿命モードなので、`RenewTimed` を毎 tick 呼ぶと
// 応答しないマウントでは **tick ごとに 1 goroutine + 1 ブロック中 syscall が積む**
// (`withTimeout` は見捨てた goroutine を回収できない)。だから `with` の更新だけは
// 「詰まっている間は新しく起こさない」制御を呼び出し側に持たせる形にした。
//
// 🚨 **期限が来ても更新をやめてはいけない。** やめるとマウントが復旧しても lease が
// 期限切れになり、他者が正当に引き継ぐ = 子が走ったまま二重実行になる
// (敵対レビュー 2026-09-15 が A-B で実測。`ticker.Stop()` で止めた版は他マシンの
// Acquire が成功し、止めない版は拒否された)。期限は**報告**のためだけに使う。
// inFlight は「まだ返っていない Renew の本数」。**起こす側で増やし、返った goroutine 自身が
// 減らす** — 呼び出し側は期限切れで chan を捨てるので、返り値からは終わりを観測できない。
func (l *Locker) renewAsync(token string, inFlight *atomic.Int64) (<-chan error, <-chan time.Time) {
	// 🚨 chan は**バッファ 1**。呼び出し側が期限切れで参照を捨てても、返ってきた
	// goroutine は送信でブロックせずに終われる (捨てても goroutine 漏れにならない)。
	ch := make(chan error, 1)
	inFlight.Add(1) // 🚨 go の**前**に増やす (起動までの窓で上限をすり抜けない)
	go func() {
		defer inFlight.Add(-1)
		ch <- l.Renew(token)
	}()
	return ch, time.After(l.timeout)
}

// ioTimeoutErr は期限切れのエラー。文面は withTimeout と揃える。
func (l *Locker) ioTimeoutErr() error {
	return fmt.Errorf("%w: I/O が %v 以内に返らない (マウントが応答しない可能性): 判定不能",
		errIOTimeout, l.timeout)
}

func (l *Locker) InspectTimed() (*State, error) {
	return withTimeout(l.timeout, func(_ *abandon) (*State, error) { return l.Inspect() })
}

func (l *Locker) BreakTimed() error {
	_, err := withTimeout(l.timeout, func(_ *abandon) (struct{}, error) { return struct{}{}, l.Break() })
	return err
}

// CleanupTimed は掃除を包む。**タイムアウトは致命にしない** — 掃除は正しさに関与しない
// 設計 (cleanup.go の 🚨) なので、失敗は件数と一緒に res.Errors へ入れて呼び出し側に見せる。
func (l *Locker) CleanupTimed(force bool) CleanupResult {
	// 🚨 **期限切れでも「どこまで進んだか」を返す** (issue 393)。旧版は `res` を丸ごと捨てて
	// `CleanupResult{Errors: ...}` を返しており、**実際には数千件を消しているのに
	// `removed=0`** と報告していた (実測 2026-09-18: 残骸 10,000 件で 3 回連続 `removed=0`、
	// 実数は毎回 2,400〜2,500 件)。`dispatch` のコメントは「部分的に進んでいるのか
	// 何も進んでいないのかは**失敗している場面でこそ知りたい**」と約束しているのに、
	// その唯一の場面で数字が嘘になっていた。
	// 🚨 `res` 自体はまだ見捨てた goroutine が書いているので**読めない** (データ競合)。
	// 読んでよいのは atomic な件数だけ。
	p := &cleanupProgress{}
	res, err := withTimeout(l.timeout, func(_ *abandon) (CleanupResult, error) {
		return l.Cleanup(force, p), nil
	})
	if err != nil {
		// 🚨 **件数だけでなく sweep 中のエラーも持ち帰る** (敵対レビュー 393 の P2-3)。
		// 旧版はここで `CleanupResult` を組み立て直してタイムアウト 1 本だけを入れており、
		// 「排水中」と「権限ドリフトで恒久的に詰まっている」を分ける情報を捨てていた。
		removed, errs := p.snapshot() // 読んだ後も増え続ける = 「少なくとも」の値
		return CleanupResult{
			Removed: removed,
			Partial: true,
			Errors:  append(errs, err.Error()),
		}
	}
	return res
}

// statDirTimed は **Locker を作る前**の I/O を包む。対象ディレクトリは共有そのものなので、
// ここが応答しないと `lockman check <share>` が無言で固まる (`--io-timeout` を渡していても)。
// 🚨 敵対レビュー 2026-09-15 が実測: 3s ブロックするマウントで
// `check --io-timeout 200ms` が exit=0 / 所要 3.001s だった。
func statDirTimed(path string, d time.Duration) (os.FileInfo, error) {
	return withTimeout(d, func(_ *abandon) (os.FileInfo, error) { return os.Stat(path) })
}

// readFileTimed / writeFileTimed は token-file の I/O。共有上に置かれうるので包む。
func readFileTimed(path string, d time.Duration) ([]byte, error) {
	return withTimeout(d, func(_ *abandon) ([]byte, error) { return os.ReadFile(path) })
}

func writeFileTimed(path string, b []byte, mode os.FileMode, d time.Duration) error {
	_, err := withTimeout(d, func(_ *abandon) (struct{}, error) { return struct{}{}, os.WriteFile(path, b, mode) })
	return err
}
