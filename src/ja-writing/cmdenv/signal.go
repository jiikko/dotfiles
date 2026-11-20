package cmdenv

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// Signaled は、外から届いたシグナルで ctx が取り消されたことを表す cause。
// check は止められたシグナルの番号で終了コード (128+番号) と文を決めるので、signal.NotifyContext (cause を持たない) では足りない。
type Signaled struct{ Signal syscall.Signal }

func (s Signaled) Error() string {
	return "シグナル " + s.Signal.String() + " で止められました"
}

// NotifyContext は signal.NotifyContext と同じく、sigs のどれかが届くと取り消す ctx を返す。取り消しの cause は Signaled。
// stop を呼ぶまで、2 つ目以降のシグナルも受けたまま (既定の振る舞いに戻さない)。
func NotifyContext(parent context.Context, sigs ...os.Signal) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sigs...)
	done := make(chan struct{})
	go func() {
		select {
		case got := <-ch:
			if number, ok := got.(syscall.Signal); ok {
				cancel(Signaled{Signal: number})
			} else {
				cancel(errors.New(got.String()))
			}
		case <-done:
		case <-ctx.Done():
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			close(done)
			signal.Stop(ch)
			cancel(context.Canceled)
		})
	}
}

// SignalOf は ctx がシグナルで取り消されていれば、そのシグナルを返す。
func SignalOf(ctx context.Context) (syscall.Signal, bool) {
	var s Signaled
	if errors.As(context.Cause(ctx), &s) {
		return s.Signal, true
	}
	return 0, false
}
