package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"pro-con/wake"
)

// fakeDispatcherPidEnv は、テストの二進が dispatcher として起こされたときに自分の pid を書く先 (spawn の検査が使う)。
const fakeDispatcherPidEnv = "PRO_CON_TEST_FAKE_DISPATCHER_PID"

// 🚨 socket の逃がし先を本物の /tmp/pro-con-<uid> にしない (t.TempDir の置き場は長く、逃がし先へ倒れる)。
func TestMain(m *testing.M) {
	// spawnDispatcher は os.Executable (= このテストの二進) を起こす。中継は本物を走らせ、dispatcher は偽物にする
	// (🚨 本物の dispatcher を走らせない: 本物の状態の置き場で PG を起こす)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case spawnDetachedCmd:
			os.Exit(spawnDetached(os.Args[2:], os.Stderr))
		case "dispatcher":
			if p := os.Getenv(fakeDispatcherPidEnv); p != "" {
				tmp := p + ".tmp" // 書きかけを読ませない
				if os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())), 0o600) == nil {
					_ = os.Rename(tmp, filepath.Clean(p))
				}
			}
			os.Exit(0) // すぐ抜ける (落ち続けて keeper が起こし直す形)
		}
	}
	os.Exit(wake.RunIsolated(m.Run))
}
