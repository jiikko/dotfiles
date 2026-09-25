package dispatcher

// dispatcher の排他 (2 つ起動しない。記録の書き手は 1 つだけ。426 の決定 1)。flock はプロセスが終われば OS が外すので、落ちても取り残されない。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"pro-con/store"
)

// LockFile はロックのファイル名 (状態の置き場の下)。画面は pid だけを読む (store.DispatcherGone)。
const LockFile = store.DispatcherLockFile

// ErrRunning は別の dispatcher が既に動いているとき。
var ErrRunning = errors.New("pro-con dispatcher は既に動いている")

// Lock は状態の置き場の dispatcher のロックを取る。取れなければ ErrRunning。返した関数で外す。
func Lock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, LockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, fmt.Errorf("dispatcher のロックを取れない: %w", err)
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid()) // 人が読む用 (どのプロセスが持っているか)
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
