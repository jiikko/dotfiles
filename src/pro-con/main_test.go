package main

import (
	"errors"
	"os"
	"path/filepath"
	"pro-con/daemon"
	"strings"
	"testing"
	"time"

	"pro-con/backend"
	"pro-con/fake"
	"pro-con/ui"
)

// exec に失敗したら旧版のまま続けるので、書いた状態のファイルは残さない。前に引き継いだ状態のファイルも役目を終えて消える。
func TestSwitchFailureLeavesNoStateFiles(t *testing.T) {
	orig := execFn
	t.Cleanup(func() { execFn = orig })
	var passed string
	execFn = func(_ string, _ []string, env []string) error {
		for _, kv := range env {
			if len(kv) > len("PRO_CON_RESUME=") && kv[:len("PRO_CON_RESUME=")] == "PRO_CON_RESUME=" {
				passed = kv[len("PRO_CON_RESUME="):]
			}
		}
		if _, err := os.Stat(passed); err != nil {
			t.Fatalf("exec の時点で状態のファイルが無い: %v", err)
		}
		return errors.New("exec できない")
	}
	dir := t.TempDir()
	prev := filepath.Join(dir, "resume-old.json")
	if err := os.WriteFile(prev, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	sim := fake.New(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	m := ui.New(sim, []backend.Repo{})
	root := t.TempDir()
	src := filepath.Join(root, "src", "pro-con")
	_ = os.MkdirAll(src, 0o755)
	_ = os.MkdirAll(filepath.Join(root, "bin", "lib"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "go.mod"), []byte("module pro-con\n"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "pro-con"), []byte("x"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "bin", "lib", "go_autobuild.zsh"), []byte("#"), 0o644)
	if err := m.EnableUpgrade(filepath.Join(src, "pro-con"), nil); err != nil {
		t.Fatal(err)
	}
	// 引き継いだファイルがあるとき: そこへ上書きして渡し、exec に失敗しても消さない (旧版のまま続けるときの状態のファイル)
	if p, err := switchToNew(m, sim, nil, dir, prev); err == nil || p != prev || passed != prev {
		t.Fatalf("引き継いだファイルへ上書きして渡すはず: path=%s passed=%s err=%v", p, passed, err)
	}
	if _, err := os.Stat(prev); err != nil {
		t.Fatal("引き継いだファイルを消した")
	}
	// 引き継いでいないとき: 新しく作って渡し、exec に失敗したら消す
	_ = os.Remove(prev)
	if p, err := switchToNew(m, sim, nil, dir, ""); err == nil || p != "" {
		t.Fatalf("失敗したのにパスを返した: %s %v", p, err)
	}
	if passed == "" || passed == prev {
		t.Fatal("新しいファイルを渡していない")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("状態のファイルが残った: %v", entries)
	}
}

// 引き継いだ状態のパスは取ったら環境変数から消す (子プロセスへ漏らさない)。
func TestTakeResumeEnvUnsets(t *testing.T) {
	t.Setenv("PRO_CON_RESUME", "/s/resume.json")
	if got := takeResumeEnv(); got != "/s/resume.json" {
		t.Fatalf("パスが違う: %q", got)
	}
	if v, ok := os.LookupEnv("PRO_CON_RESUME"); ok {
		t.Fatalf("環境変数が残った: %q", v)
	}
}

// ライブアップグレードで新版に渡す引数は --mock を付け直す (付け忘れると、模擬で使っていたのに本物で起動し直す)。
func TestExecArgsKeepsMock(t *testing.T) {
	if got := strings.Join(execArgs(true, nil), " "); got != "--mock" {
		t.Fatalf("模擬の引数: %q", got)
	}
	if got := execArgs(false, nil); len(got) != 0 {
		t.Fatalf("本物の引数に余計なものが付いた: %q", got)
	}
}

// 画面を開いたとき、daemon が動いていなければ起動し、動いていれば起動しない。
func TestStartDaemonIfIdle(t *testing.T) {
	dir := t.TempDir()
	spawned := 0
	spawn := func(string) error { spawned++; return nil }
	if started, err := startDaemonIfIdle(dir, spawn); !started || err != nil || spawned != 1 {
		t.Fatalf("daemon が居ないのに起動しない: started=%v err=%v spawned=%d", started, err, spawned)
	}
	unlock, err := daemon.Lock(dir) // daemon が動いている形
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if started, err := startDaemonIfIdle(dir, spawn); started || err != nil || spawned != 1 {
		t.Fatalf("daemon が動いているのに起動した: started=%v err=%v spawned=%d", started, err, spawned)
	}
}
