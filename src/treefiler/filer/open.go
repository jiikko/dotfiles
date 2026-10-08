package filer

import (
	"context"
	"sync"
	"time"

	"subproc"
)

// open.go は `o` (選んだ項目を外のアプリで開く。glogx の語彙のまま `open <path>`。spec §0.1)。
// 端末は明け渡さない (open は LaunchServices へ渡してすぐ戻る)。起動は裏の goroutine で行い、失敗は次の Advance で toast にする。

// openCommand は外のアプリで開く (テストで本物のアプリを開かないための差し替え点)。
var openCommand = func(ctx context.Context, path string) error {
	cmd := subproc.CommandContext(ctx, "open", path)
	cmd.Stdin = nil
	return cmd.Run()
}

const openTimeout = 10 * time.Second

type opener struct {
	mu      sync.Mutex
	running int
	errs    []string
}

// start は path を開く。name は toast に出す名前 (termsafe 済みの表示名)。
func (o *opener) start(path, name string) {
	o.mu.Lock()
	o.running++
	o.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		err := openCommand(ctx, path)
		o.mu.Lock()
		defer o.mu.Unlock()
		o.running--
		if err != nil {
			o.errs = append(o.errs, "開けません: "+name)
		}
	}()
}

// busy は起動中か、知らせていない失敗があるか (呼び出し側が失敗の toast を出す前に tick を止めないように)。
func (o *opener) busy() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running > 0 || len(o.errs) > 0
}

func (o *opener) take() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	e := o.errs
	o.errs = nil
	return e
}
