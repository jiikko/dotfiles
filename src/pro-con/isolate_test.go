package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"pro-con/wake"
	"pro-con/wtclean"
)

// fakeDispatcherPidEnv が付いていると、テストの二進は dispatcher として起こされたときに偽物になる: 自分の pid をこのパスへ書き、
// パス + ".release" が置かれるまで居続けてから抜ける (本物の dispatcher と同じく常駐する形。spawn の検査が使う)。
const fakeDispatcherPidEnv = "PRO_CON_TEST_FAKE_DISPATCHER_PID"

// 🚨 socket の逃がし先を本物の /tmp/pro-con-<uid> にしない (t.TempDir の置き場は長く、逃がし先へ倒れる)。
func TestMain(m *testing.M) {
	// spawnSupervisor は os.Executable (= このテストの二進) を起こす。中継と supervisor は run を通して本物を走らせ、dispatcher は偽物にする
	// (🚨 本物の dispatcher を走らせない: 本物の状態の置き場で PG を起こす。supervisor は偽物の印があるときだけ走らせる)
	if len(os.Args) > 1 {
		switch p := os.Getenv(fakeDispatcherPidEnv); {
		case os.Args[1] == spawnDetachedCmd, os.Args[1] == superviseCmd && p != "":
			os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
		case os.Args[1] == "dispatcher" && p != "":
			os.Exit(runFakeDispatcher(p))
		}
	}
	// worktree clean のテストの fixture はこの下に作る。wtclean はテストの二進ではこの外を消す前に拒否する (issue 492)
	root, err := os.MkdirTemp("", "pro-con-wtclean")
	if err != nil {
		panic(err)
	}
	worktreeSandbox = root
	wtclean.SetTestSandbox(root)
	code := wake.RunIsolated(m.Run)
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// worktreeSandbox は worktree clean のテストの fixture の置き場 (TestMain が作る)。
var worktreeSandbox string

func runFakeDispatcher(pidPath string) int {
	tmp := pidPath + ".tmp" // 書きかけを読ませない
	if os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())), 0o600) != nil || os.Rename(tmp, filepath.Clean(pidPath)) != nil {
		return 1
	}
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); { // 上限はテストが落ちたときに残さないため
		if _, err := os.Stat(pidPath + ".release"); err == nil {
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 1
}
