package chromecookie

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperEnv はテストバイナリを「後始末の被験体」として再実行するための目印。
const helperEnv = "CHROMECOOKIE_CLEANUP_HELPER"

// ②（シグナル経路）を**実際にシグナルを撃って**確かめる。
//
// 🚨 RunAllCleanups を直接呼ぶテストは、②の配線を 1 mm も守らない
// （どのシグナルを捕まえているかが検査されない）。ここではテストバイナリ自身を
// 子プロセスとして起こし、本物のシグナルを送って残骸の有無を見る。
//
// 対象シグナルは「人が実際に送れて、プロセスを終わらせるもの」:
//   - SIGINT  (Ctrl-C)
//   - SIGTERM (kill の既定)
//   - SIGHUP  (端末が閉じた)
//   - SIGQUIT (Ctrl-\)  ← 捕まえていないと復号済み cookie のコピーが残る
func TestSignalCleanupRemovesTempDirs(t *testing.T) {
	if os.Getenv(helperEnv) == "1" {
		runCleanupHelper()
		return
	}

	cases := []struct {
		name string
		sig  syscall.Signal
	}{
		{"SIGINT (Ctrl-C)", syscall.SIGINT},
		{"SIGTERM", syscall.SIGTERM},
		{"SIGHUP", syscall.SIGHUP},
		{"SIGQUIT (Ctrl-\\)", syscall.SIGQUIT},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 🚨 作業領域は HOME 基準なので、HOME を隔離しないと**実際の
			// ~/Library/Caches** にテストがコピーを作る（実測で踏んだ）。
			home := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestSignalCleanupRemovesTempDirs$")
			cmd.Env = append(os.Environ(), helperEnv+"=1", "HOME="+home)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()

			// 子が一時ディレクトリを作り終えるのを待つ（壁時計で待たない）。
			line := make(chan string, 1)
			go func() {
				sc := bufio.NewScanner(stdout)
				for sc.Scan() {
					if strings.HasPrefix(sc.Text(), "READY ") {
						line <- strings.TrimPrefix(sc.Text(), "READY ")
						return
					}
				}
				line <- ""
			}()
			var dir string
			select {
			case dir = <-line:
			case <-time.After(30 * time.Second):
				t.Fatal("子プロセスが準備完了を報告しない")
			}
			if dir == "" {
				t.Fatal("子プロセスが一時ディレクトリを作れなかった")
			}
			if _, err := os.Stat(filepath.Join(dir, "Cookies")); err != nil {
				t.Fatalf("前提: 一時コピーが存在するはず: %v", err)
			}

			if err := cmd.Process.Signal(c.sig); err != nil {
				t.Fatal(err)
			}
			waitErr := cmd.Wait()

			// シグナルで終了したことも見る（後始末だけして走り続けていないこと）。
			var exitErr *exec.ExitError
			if !errors.As(waitErr, &exitErr) {
				t.Fatalf("シグナルで終了していない: %v", waitErr)
			}
			if got, want := exitErr.ExitCode(), 128+int(c.sig); got != want {
				t.Errorf("終了コード: got %d, want %d（シェルの慣習 128+シグナル番号）", got, want)
			}

			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				// 🚨 ここが赤いとき、復号済みの cookie を含むコピーが作業領域に残っている。
				entries, _ := os.ReadDir(dir)
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Errorf("%v で終了したのに一時コピーが残っている: %s (%v)", c.sig, dir, names)
			}
		})
	}
}

// runCleanupHelper は子プロセス側。②を仕掛けてから一時コピーを作り、シグナルを待つ。
func runCleanupHelper() {
	testWS.InstallCleanupOnSignal()
	dir, _, err := testWS.NewTempDir()
	if err != nil {
		fmt.Println("READY ")
		return
	}
	// 本物と同じ形（復号済みの値を書いたファイル）を置く。
	_ = os.WriteFile(filepath.Join(dir, "Cookies"), []byte("xoxd-HELPER-NOT-A-REAL-SECRET"), 0o600)
	fmt.Println("READY " + dir)
	_ = os.Stdout.Sync()
	// sleep-ok: dummy: helper プロセスをシグナルで殺されるまで生かすだけ
	time.Sleep(60 * time.Second) // シグナルで殺される前提
}
