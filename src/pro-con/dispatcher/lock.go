package dispatcher

// dispatcher の排他 (2 つ起動しない。記録の書き手は 1 つだけ。426 の決定 1)。flock はプロセスが終われば OS が外すので、落ちても取り残されない。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockFile はロックのファイル名 (状態の置き場の下)。MonitorLockFile は見張り (pro-con monitor。issue 475) のロック。
const (
	LockFile        = "dispatcher.lock"
	MonitorLockFile = "monitor.lock"
)

// ErrRunning は別の dispatcher が既に動いているとき。ErrMonitorRunning は別の見張りが既に動いているとき。
var (
	ErrRunning        = errors.New("pro-con dispatcher は既に動いている")
	ErrMonitorRunning = errors.New("pro-con monitor は既に動いている")
)

// Lock は状態の置き場の dispatcher のロックを取る。取れなければ ErrRunning。返した関数で外す。
func Lock(dir string) (func(), error) { return lockAs(dir, LockFile, ErrRunning, "dispatcher") }

// LockMonitor は状態の置き場の見張りのロックを取る (見張りを 2 つ動かさない)。取れなければ ErrMonitorRunning。返した関数で外す。
func LockMonitor(dir string) (func(), error) {
	return lockAs(dir, MonitorLockFile, ErrMonitorRunning, "monitor")
}

// lockAs は状態の置き場の name のロックを取る。他が持っていれば held。
func lockAs(dir, name string, held error, who string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, held
		}
		return nil, fmt.Errorf("%s のロックを取れない: %w", who, err)
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid()) // 人が読む用 (どのプロセスが持っているか)
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
