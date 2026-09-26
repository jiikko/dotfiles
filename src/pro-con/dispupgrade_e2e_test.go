package main

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

	"pro-con/dispatcher"
	"pro-con/eventlog"
	"pro-con/upgrade"
)

// 本物の pro-con のバイナリで、動いている dispatcher が新版へ自分で切り替わる (issue 505)。shim は偽物 (呼ばれたら新版を置く)、
// claude と tmux も偽物 (本物の session・tmux に触らない)。状態の置き場は一時の HOME / XDG_STATE_HOME の下。
// 確かめること: PID のまま引数も引き継いで新版に入れ替わる / 入れ替えの前から後まで 2 つ目の dispatcher は lock を取れない /
// 抜けた後は lock が外れる (入れ替えの前後に起こした子が握っていない)。
func TestDispatcherUpgradesItselfE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("pro-con を build する")
	}
	root, err := os.MkdirTemp("/tmp", "pcup") // socket のパスを短くする (heldRoot と同じ)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	src := filepath.Join(root, "src", "pro-con")
	fake := filepath.Join(root, "fakebin")
	for _, d := range []string{src, filepath.Join(root, "bin", "lib"), fake, filepath.Join(root, "home")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	exe, newExe := filepath.Join(src, upgrade.BinaryName), filepath.Join(root, "new-pro-con")
	if out, err := exec.Command("go", "build", "-buildvcs=false", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("pro-con を build できない: %v\n%s", err, out)
	}
	if out, err := exec.Command("cp", exe, newExe).CombinedOutput(); err != nil { // 新版 (別のファイル = 差し替えると inode が変わる)
		t.Fatalf("%v: %s", err, out)
	}
	files := map[string]string{
		filepath.Join(src, "go.mod"): "module pro-con\n",
		// 偽の shim: 最初に尋ねられたら新版を置いて「裏ビルドを起動した」、以後は「要らない」
		filepath.Join(root, "bin", "lib", "go_autobuild.zsh"): `go_autobuild_spawn_if_stale() {
  [[ -e "$1/.test-upgraded" ]] && return 1
  : > "$1/.test-upgraded"
  cp "$NEWBIN" "$1/$2.tmp" && mv "$1/$2.tmp" "$1/$2"
}
`,
		filepath.Join(fake, "claude"): "#!/bin/sh\ncase \"$1\" in --version) echo '9.9.9 (Claude Code)';; agents) echo '[]';; *) exit 1;; esac\n",
		filepath.Join(fake, "tmux"):   "#!/bin/sh\nexit 0\n",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"HOME=" + filepath.Join(root, "home"), "XDG_STATE_HOME=" + filepath.Join(root, "state"), "PATH=" + fake + ":/usr/bin:/bin", "NEWBIN=" + newExe, "LANG=ja_JP.UTF-8"}
	dir := filepath.Join(root, "state", "pro-con", "live") // liveDir を子の XDG_STATE_HOME で引いた場所
	logf, err := os.Create(filepath.Join(root, "dispatcher.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logf.Close() }()
	cmd := exec.Command(exe, "dispatcher", "--pm=off", "--integrator=off")
	cmd.Env, cmd.Stdout, cmd.Stderr = env, logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	logText := func() string { b, _ := os.ReadFile(logf.Name()); return string(b) }
	pid := strconv.Itoa(cmd.Process.Pid)
	deadline := time.Now().Add(60 * time.Second)
	for { // dispatcher が lock を取るまで (それより前は取れて当然)
		if b, _ := os.ReadFile(filepath.Join(dir, dispatcher.LockFile)); strings.TrimSpace(string(b)) == pid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dispatcher が lock を取らない:\n%s", logText())
		}
		time.Sleep(10 * time.Millisecond)
	}
	switched := func() bool {
		evs, _ := eventlog.Read(dir)
		for _, e := range evs {
			if e.Kind == eventlog.KindUpgrade && strings.Contains(e.Reason, "新版に切り替わった") {
				return true
			}
		}
		return false
	}
	for !switched() { // 入れ替わるまで、2 つ目の dispatcher が lock を取れないことを確かめ続ける
		select {
		case err := <-exited:
			t.Fatalf("dispatcher が抜けた (%v):\n%s", err, logText())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("新版に切り替わらない:\n%s", logText())
		}
		if unlock, err := dispatcher.Lock(dir); err == nil {
			unlock()
			t.Fatalf("2 つ目の dispatcher が lock を取れた:\n%s", logText())
		} else if !errors.Is(err, dispatcher.ErrRunning) {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	for _, want := range []string{"新版ができた", "新版へ切り替える", "PID " + pid + " のまま"} {
		if !strings.Contains(logText(), want) {
			t.Errorf("出来事に %q が無い:\n%s", want, logText())
		}
	}
	if out, _ := exec.Command("ps", "-o", "args=", "-p", pid).Output(); !strings.Contains(string(out), "dispatcher --pm=off --integrator=off") {
		t.Errorf("入れ替えで引数を引き継いでいない: %q", out)
	}
	second := exec.Command(exe, "dispatcher") // 入れ替わった後も lock は 1 つ目が持つ
	second.Env = env
	if out, err := second.CombinedOutput(); err == nil || !strings.Contains(string(out), "既に動いている") {
		t.Fatalf("入れ替わった後に 2 つ目の dispatcher が起動した: %v\n%s", err, out)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(30 * time.Second):
		t.Fatalf("SIGTERM で抜けない:\n%s", logText())
	}
	unlock, err := dispatcher.Lock(dir)
	if err != nil {
		t.Fatalf("dispatcher が抜けた後も lock が外れない (子が握っている?): %v\n%s", err, logText())
	}
	unlock()
}
