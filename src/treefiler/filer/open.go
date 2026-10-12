package filer

import (
	"context"
	"sync"
	"time"

	"subproc"
)

// open.go は `o` (選んだ項目を外のアプリで開く。glogx の語彙のまま `open <path>`。spec §0.1) と `O` (Finder で選んだ状態で見せる。`open -R <path>`)。
// 端末は明け渡さない (open は LaunchServices へ渡してすぐ戻る)。起動は裏の goroutine で行い、失敗は次の Advance で toast にする。

// openCommand は open(1) を args で起こす (テストで本物のアプリを開かないための差し替え点)。
var openCommand = func(ctx context.Context, args []string) error {
	cmd := subproc.CommandContext(ctx, "open", args...)
	cmd.Stdin = nil
	return cmd.Run()
}

const openTimeout = 10 * time.Second

type opener struct {
	mu      sync.Mutex
	running int
	errs    []string
}

// open は path を外のアプリで開き、reveal は Finder で選んだ状態で見せる。name は toast に出す名前 (termsafe 済みの表示名)。
func (o *opener) open(path, name string) { o.start([]string{path}, "開けません: "+name) }
func (o *opener) reveal(path, name string) {
	o.start([]string{"-R", path}, "Finder で表示できません: "+name)
}

// start は open(1) を裏で起こし、失敗したら fail を次の Advance で toast にする。
func (o *opener) start(args []string, fail string) {
	o.mu.Lock()
	o.running++
	o.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		err := openCommand(ctx, args)
		o.mu.Lock()
		defer o.mu.Unlock()
		o.running--
		if err != nil {
			o.errs = append(o.errs, fail)
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
