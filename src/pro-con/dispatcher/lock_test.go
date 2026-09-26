package dispatcher

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// lockHelperEnv は TestLockExecHelper を入れ替えの役で走らせる印 (値は段: old / new)。lockHelperDir は状態の置き場。
const (
	lockHelperEnv = "PRO_CON_TEST_LOCK_HELPER"
	lockHelperDir = "PRO_CON_TEST_LOCK_DIR"
)

// TestLockExecHelper は TestLockSurvivesExec が起こす入れ替えの役 (印が無ければ何もしない)。
// old: lock を取って、自分 (このテストのバイナリ) へ HeldLock.Exec で入れ替える。
// new: 立ち上がりの遅い新版を真似て少し待ってから AdoptLock で受け取り、子を 1 つ起こし、release のファイルが置かれるまで lock を持つ。
func TestLockExecHelper(t *testing.T) {
	stage, dir := os.Getenv(lockHelperEnv), os.Getenv(lockHelperDir)
	if stage == "" {
		t.Skip("TestLockSurvivesExec が起こす役")
	}
	mark := func(name, v string) { _ = os.WriteFile(filepath.Join(dir, name), []byte(v), 0o600) }
	switch stage {
	case "old":
		l, err := TakeLock(dir)
		if err != nil {
			mark("failed", "old: "+err.Error())
			os.Exit(1)
		}
		mark("old-pid", strconv.Itoa(os.Getpid()))
		var env []string // 段の印を書き換える (同じ名前が 2 つあると先の値が読まれる)
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, lockHelperEnv+"=") {
				env = append(env, kv)
			}
		}
		env = append(env, lockHelperEnv+"=new")
		err = l.Exec(os.Args[0], []string{os.Args[0], "-test.run=^TestLockExecHelper$"}, env, syscall.Exec)
		mark("failed", "exec: "+err.Error())
		os.Exit(1)
	case "new":
		GuardInheritedLock()
		time.Sleep(300 * time.Millisecond) // 入れ替えの隙 (新版の立ち上がり) を広げる: この間に lock が外れていれば、テストが取れてしまう
		l, err := AdoptLock(dir)
		if l == nil {
			mark("failed", "adopt: "+errors.Join(err, errors.New("lock を受け取れない")).Error())
			os.Exit(1)
		}
		child := exec.Command("sleep", "30") // 受け取った後に起こす子は lock を握らない (CLOEXEC に戻っている)
		if err := child.Start(); err != nil {
			mark("failed", "child: "+err.Error())
			os.Exit(1)
		}
		mark("child-pid", strconv.Itoa(child.Process.Pid))
		mark("new-pid", strconv.Itoa(os.Getpid()))
		for {
			if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
				os.Exit(0) // Release せずに抜ける (OS が外す)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// 🚨 dispatcher が新版へ入れ替わる (syscall.Exec) 間、lock は一瞬も外れない: 入れ替えの前から後まで、2 つ目の dispatcher は lock を取れない。
// PID は変わらない。入れ替わった後に起こした子は lock を握らず、dispatcher が抜ければ次が取れる (issue 505)。
func TestLockSurvivesExec(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockExecHelper$")
	cmd.Env = append(os.Environ(), lockHelperEnv+"=old", lockHelperDir+"="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		if b, err := os.ReadFile(filepath.Join(dir, "child-pid")); err == nil {
			if pid, _ := strconv.Atoi(string(b)); pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}
	has := func(name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }
	deadline := time.Now().Add(30 * time.Second)
	for !has("old-pid") { // old が lock を取るまで (それより前は取れて当然)
		if has("failed") || time.Now().After(deadline) {
			t.Fatalf("入れ替えの前の役が lock を取らない: %s", read("failed"))
		}
		time.Sleep(time.Millisecond)
	}
	tries := 0
	for !has("new-pid") { // 入れ替えの前から、受け取り終えるまで取り続けてみる
		if has("failed") || time.Now().After(deadline) {
			t.Fatalf("入れ替わった役が lock を受け取らない: %s", read("failed"))
		}
		unlock, err := Lock(dir)
		if err == nil {
			unlock()
			t.Fatalf("入れ替えの隙に 2 つ目の dispatcher が lock を取れた (%d 回目)", tries+1)
		}
		if !errors.Is(err, ErrRunning) {
			t.Fatal(err)
		}
		tries++
		time.Sleep(time.Millisecond)
	}
	if tries < 50 { // 隙 (300ms) の間に試せていなければ、このテストは何も確かめていない
		t.Fatalf("入れ替えの間に %d 回しか試せていない", tries)
	}
	if o, n := read("old-pid"), read("new-pid"); o != n {
		t.Fatalf("入れ替えで PID が変わった: %s → %s", o, n)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrRunning) {
		t.Fatalf("入れ替わった dispatcher が lock を持っていない: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("入れ替わった役が失敗で抜けた: %v (%s)", err, read("failed"))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("入れ替わった役が抜けない")
	}
	unlock, err := Lock(dir) // 子 (sleep) はまだ生きている。握っていれば取れない
	if err != nil {
		t.Fatalf("dispatcher が抜けた後も lock が外れない (入れ替わった後の子が握っている?): %v", err)
	}
	unlock()
}

// 入れ替え (exec) が失敗して戻ってきたら、lock は持ったまま、fd は exec で閉じる形 (子へ渡らない) に戻る。
func TestLockExecFailureKeepsLockPrivate(t *testing.T) {
	dir := t.TempDir()
	l, err := TakeLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	var gotEnv []string
	err = l.Exec("/nonexistent", nil, []string{"A=1"}, func(_ string, _ []string, env []string) error {
		gotEnv = env
		if flags, err := unix.FcntlInt(l.f.Fd(), unix.F_GETFD, 0); err != nil || flags&unix.FD_CLOEXEC != 0 {
			t.Errorf("exec の間に fd が exec で閉じる形のまま (flags=%d err=%v)", flags, err)
		}
		return errors.New("exec できない")
	})
	if err == nil || !strings.Contains(err.Error(), "exec できない") {
		t.Fatalf("exec の失敗を返さない: %v", err)
	}
	if want := LockFDEnv + "=" + strconv.Itoa(int(l.f.Fd())); len(gotEnv) != 2 || gotEnv[1] != want {
		t.Fatalf("新しいプロセス像へ fd を渡していない: %v", gotEnv)
	}
	if flags, err := unix.FcntlInt(l.f.Fd(), unix.F_GETFD, 0); err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("失敗の後に fd が exec で閉じる形に戻っていない (flags=%d err=%v)", flags, err)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrRunning) {
		t.Fatalf("失敗の後に lock が外れた: %v", err)
	}
}

// 引き継いだと言われた fd が lock のファイルでなければ受け取らず、その fd は閉じない (何の fd か分からないものを壊さない)。
func TestAdoptLockRejectsOtherFile(t *testing.T) {
	dir := t.TempDir()
	other, err := os.CreateTemp(dir, "other")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	t.Setenv(LockFDEnv, strconv.Itoa(int(other.Fd())))
	l, err := AdoptLock(dir)
	if l != nil || err == nil {
		t.Fatalf("lock でない fd を受け取った: %v", err)
	}
	if _, ok := os.LookupEnv(LockFDEnv); ok {
		t.Fatal("環境変数を消していない (子へ漏れる)")
	}
	if _, err := other.WriteString("x"); err != nil {
		t.Fatalf("lock でない fd を閉じた: %v", err)
	}
	if got, err := AdoptLock(dir); got != nil || err != nil { // 渡されていなければ (nil, nil)
		t.Fatalf("渡されていないのに %v / %v", got, err)
	}
}
