package dispatcher

// dispatcher の排他 (2 つ起動しない。記録の書き手は 1 つだけ。426 の決定 1)。flock はプロセスが終われば OS が外すので、落ちても取り残されない。
//
// 新版への入れ替え (issue 505。syscall.Exec) では lock を**外さずに**新しいプロセス像へ引き継ぐ (HeldLock.Exec / AdoptLock)。
// 外して取り直す形にすると、その隙に画面の keeper が起こした 2 つ目の dispatcher が lock を取る。
// flock は開いたファイル (open file description) に付くので、fd を exec に持ち越せば PID と一緒に lock も続く。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"

	"pro-con/store"
)

// LockFile はロックのファイル名 (状態の置き場の下)。画面は pid だけを読む (store.DispatcherGone)。
// MonitorLockFile は見張り (pro-con monitor。issue 475) のロック。
const (
	LockFile        = store.DispatcherLockFile
	MonitorLockFile = "monitor.lock"
)

// LockFDEnv は入れ替え (HeldLock.Exec) が新しいプロセス像へ lock の fd の番号を渡す環境変数。
const LockFDEnv = "PRO_CON_DISPATCHER_LOCK_FD"

// ErrRunning は別の dispatcher が既に動いているとき。ErrMonitorRunning は別の見張りが既に動いているとき。
var (
	ErrRunning        = errors.New("pro-con dispatcher は既に動いている")
	ErrMonitorRunning = errors.New("pro-con monitor は既に動いている")
)

// Lock は状態の置き場の dispatcher のロックを取る。取れなければ ErrRunning。返した関数で外す。
func Lock(dir string) (func(), error) {
	l, err := TakeLock(dir)
	if err != nil {
		return nil, err
	}
	return l.Release, nil
}

// LockMonitor は状態の置き場の見張りのロックを取る (見張りを 2 つ動かさない)。取れなければ ErrMonitorRunning。返した関数で外す。
func LockMonitor(dir string) (func(), error) {
	l, err := lockAs(dir, MonitorLockFile, ErrMonitorRunning, "monitor")
	if err != nil {
		return nil, err
	}
	return l.Release, nil
}

// HeldLock は持っているロック。
type HeldLock struct{ f *os.File }

// TakeLock は Lock と同じく dispatcher のロックを取り、入れ替えで引き継げる形で返す。
func TakeLock(dir string) (*HeldLock, error) { return lockAs(dir, LockFile, ErrRunning, "dispatcher") }

// Release はロックを外す。
func (l *HeldLock) Release() {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}

// Exec はロックを持ったまま execFn (syscall.Exec) で新しいプロセス像へ入れ替える。env には LockFDEnv を足して渡す。
// 成功すると戻らない。戻ってきたら失敗で、ロックは元どおり持っている (fd も exec で閉じる形に戻す)。
// 🚨 fd の CLOEXEC を外している間に他の goroutine が子を起こすと、子が lock を握ったまま残る (dispatcher が死んでも次が起動できない)。
// syscall.ForkLock を書き手で持って、その間の fork (os/exec) を止める。
func (l *HeldLock) Exec(argv0 string, argv, env []string, execFn func(string, []string, []string) error) error {
	fd := int(l.f.Fd())
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0); err != nil {
		return fmt.Errorf("lock を引き継げる形にできない: %w", err)
	}
	err := execFn(argv0, argv, append(append([]string(nil), env...), LockFDEnv+"="+strconv.Itoa(fd)))
	syscall.CloseOnExec(fd)
	if err == nil {
		err = errors.New("exec が戻ってきた")
	}
	return err
}

// GuardInheritedLock は入れ替えで引き継いだ lock の fd (LockFDEnv) を、exec で閉じる形に戻す (AdoptLock で受け取るまでに子を起こしても渡さない)。
// main の頭で呼ぶ: 引き継いだ fd は exec を越えるために CLOEXEC を外してあるので、このままだと最初に起こした子 (claude --version 等) が
// lock を握ったまま残り、dispatcher が死んでも次が起動できない。環境変数は AdoptLock が読むので残す。
func GuardInheritedLock() {
	if n, err := strconv.Atoi(os.Getenv(LockFDEnv)); err == nil && n >= 3 {
		syscall.CloseOnExec(n)
	}
}

// AdoptLock は入れ替えの前のプロセス像が渡した lock (LockFDEnv) を受け取る。環境変数は読んだら消す (子へ漏らさない)。
// 渡されていなければ (nil, nil)。渡された fd が dir の lock のファイルでない・lock を確かめられないときはエラー
// (呼び出し側は TakeLock で取り直す: 別の dispatcher が持っていれば ErrRunning で抜ける)。
func AdoptLock(dir string) (*HeldLock, error) {
	v, ok := os.LookupEnv(LockFDEnv)
	_ = os.Unsetenv(LockFDEnv)
	if !ok {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 3 {
		return nil, fmt.Errorf("引き継いだ lock の fd を読めない (%q)", v)
	}
	path := filepath.Join(dir, LockFile)
	var got unix.Stat_t
	if err := unix.Fstat(n, &got); err != nil {
		return nil, fmt.Errorf("引き継いだ lock の fd %d を見られない: %w", n, err)
	}
	syscall.CloseOnExec(n) // この後に起こす子へは渡さない
	var want unix.Stat_t
	// 🚨 別のファイルの fd は閉じない (何の fd か分からないものを閉じると、持ち主の読み書きを壊す)
	if err := unix.Stat(path, &want); err != nil || got.Dev != want.Dev || got.Ino != want.Ino {
		return nil, fmt.Errorf("引き継いだ fd %d は %s ではない", n, path)
	}
	f := os.NewFile(uintptr(n), path)
	// 同じ開いたファイルが持つ flock は取り直しても通る (持っていなかったなら、ここで取る。他が持っていれば取れない)
	if err := syscall.Flock(n, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, fmt.Errorf("引き継いだ lock を確かめられない: %w", err)
	}
	return &HeldLock{f: f}, nil
}

// lockAs は状態の置き場の name のロックを取る。他が持っていれば held。
func lockAs(dir, name string, held error, who string) (*HeldLock, error) {
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
	return &HeldLock{f: f}, nil
}
