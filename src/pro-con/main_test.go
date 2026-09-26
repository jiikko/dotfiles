package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"pro-con/dispatcher"
	"strconv"
	"strings"
	"testing"
	"time"

	"context"
	"pro-con/backend"
	"pro-con/fake"
	"pro-con/live"
	"pro-con/store"
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

// ライブアップグレードで新版に渡す引数は --mock / --e2e <置き場> を付け直す (付け忘れると、模擬や e2e で使っていたのに本物で起動し直す)。
func TestParseModeKeepsModeArgs(t *testing.T) {
	if mock, _, mode, rest, err := parseMode([]string{"--mock", "x"}); !mock || strings.Join(mode, " ") != "--mock" || strings.Join(rest, " ") != "x" || err != nil {
		t.Fatalf("模擬の引数: mock=%v mode=%q rest=%q err=%v", mock, mode, rest, err)
	}
	_, e2e, mode, _, err := parseMode([]string{"--e2e", "/tmp/e2e-root"})
	if e2e == nil || e2e.Root != "/tmp/e2e-root" || strings.Join(mode, " ") != "--e2e /tmp/e2e-root" || err != nil {
		t.Fatalf("e2e の引数: %+v mode=%q err=%v", e2e, mode, err)
	}
	if _, _, _, _, err := parseMode([]string{"--e2e"}); err == nil {
		t.Fatal("置き場の無い --e2e を受けた")
	}
	if _, e2e, mode, _, _ := parseMode(nil); e2e != nil || len(mode) != 0 {
		t.Fatalf("本物の引数に余計なものが付いた: %q", mode)
	}
}

// 画面を開いたとき、dispatcher も supervisor も動いていなければ (supervisor を) 起動し、どちらかが動いていれば起動しない
// (dispatcher を起こし直すのは supervisor だけ = keeper と二重に起こさない。issue 506)。
func TestStartDispatcherIfIdle(t *testing.T) {
	dir := t.TempDir()
	spawned := 0
	spawn := func(string) error { spawned++; return nil }
	if started, err := startDispatcherIfIdle(dir, spawn); !started || err != nil || spawned != 1 {
		t.Fatalf("dispatcher が居ないのに起動しない: started=%v err=%v spawned=%d", started, err, spawned)
	}
	for name, lock := range map[string]func(string) (func(), error){"dispatcher": dispatcher.Lock, "supervisor": dispatcher.LockSupervisor} {
		unlock, err := lock(dir) // 動いている形
		if err != nil {
			t.Fatal(err)
		}
		if started, err := startDispatcherIfIdle(dir, spawn); started || err != nil || spawned != 1 {
			t.Fatalf("%s が動いているのに起動した: started=%v err=%v spawned=%d", name, started, err, spawned)
		}
		unlock()
	}
	if started, err := startDispatcherIfIdle(dir, spawn); !started || err != nil || spawned != 2 {
		t.Fatalf("lock を外しても起動しない (確かめた lock を外し忘れた): started=%v err=%v spawned=%d", started, err, spawned)
	}
}

// --e2e / --mock はサブコマンドの前に付けられない (付くと、サブコマンドが本物の置き場で動いて本物の dispatcher と PG を止める / 起こす)。
func TestModeFlagBeforeSubcommandIsRejected(t *testing.T) {
	for _, args := range [][]string{{"--e2e", t.TempDir(), "dispatcher", "--stop"}, {"--mock", "card", "add", "--title", "x"}, {"--e2e", t.TempDir(), "dispatcher"}} {
		var out, errOut bytes.Buffer
		if rc := run(args, strings.NewReader(""), &out, &errOut); rc != 2 || !strings.Contains(errOut.String(), "サブコマンド") {
			t.Fatalf("%v を受けた: rc=%d stderr=%q", args, rc, errOut.String())
		}
	}
}

// --view は画面の起動にだけ付ける (サブコマンドの前・模擬とは組まない)。
func TestViewFlagRejectedWithSubcommandOrMock(t *testing.T) {
	for _, args := range [][]string{{"--view", "dispatcher", "--stop"}, {"--view", "--mock"}} {
		var out, errOut bytes.Buffer
		if rc := run(args, strings.NewReader(""), &out, &errOut); rc != 2 {
			t.Fatalf("%v: rc=%d (2 のはず) %s", args, rc, errOut.String())
		}
	}
}

// 画面の backend のつなぎ方: --view は止める口も起こす口もつながず、dispatcher を起こさない (quit で PG を止めない)。
// 普通の画面は止める口をつなぎ、dispatcher が居なければ起こす。
func TestWireLiveViewStopsAndStartsNothing(t *testing.T) {
	for _, view := range []bool{true, false} {
		dir := t.TempDir()
		spawns, stops := 0, 0
		be, _ := wireLive(live.New(nil, t.TempDir(), dir), screenFlags{view: view}, dir,
			func(string) error { spawns++; return nil }, func(context.Context) error { stops++; return nil })
		_, stopper := be.(backend.Stopper)
		_, readOnly := be.(backend.ReadOnly)
		if view && (stopper || !readOnly || spawns != 0) {
			t.Fatalf("--view: 止める口=%v 読み取りだけ=%v dispatcher を起こした=%d (止める口なし・読み取りだけ・起こさない のはず)", stopper, readOnly, spawns)
		}
		if !view && (!stopper || readOnly || spawns != 1) {
			t.Fatalf("普通の画面: 止める口=%v 読み取りだけ=%v dispatcher を起こした=%d", stopper, readOnly, spawns)
		}
	}
}

// 画面が起こした supervisor と dispatcher は画面の子にしない: spawn は dispatcher が居る間に戻り、dispatcher の親は supervisor
// (画面ではない) で、抜けても画面の子にゾンビで残らない (keeper が起こし直すたびに溜まる。goroutine で Wait する形は ctrl+r の exec で漏れる。issue 477)。
// dispatcher が自分の判断で抜けたら (rc=0) supervisor も抜ける (ワンショット。issue 506)。
func TestSpawnedDispatcherIsNotLeftAsZombie(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // supervisor は置き場を家から決める (本物の置き場の supervisor の lock を取らない)
	dir := liveDir(t.TempDir())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "dispatcher.pid")
	release := func() { _ = os.WriteFile(pidFile+".release", nil, 0o600) }
	t.Cleanup(release) // 途中で落ちても偽の dispatcher を残さない
	t.Setenv(fakeDispatcherPidEnv, pidFile)
	done := make(chan error, 1)
	go func() { done <- spawnSupervisor(dir, nil) }()
	deadline := time.Now().Add(10 * time.Second) // 上限だけ (通る形は条件が揃った時点で進む)
	pid := 0
	for pid == 0 {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(string(b))
		}
		if pid == 0 && time.Now().After(deadline) {
			t.Fatal("偽の dispatcher が起動しない")
		}
		select {
		case err := <-done: // 中継は dispatcher が pid を書く前に抜けてよい。失敗 (中継が起動しない等) だけここで落とす
			if err != nil {
				t.Fatal(err)
			}
			done <- err
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	select { // 偽の dispatcher は release まで居続ける。その間に戻らなければ、画面は常駐する dispatcher を待って固まる
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Until(deadline)):
		t.Fatal("spawnSupervisor が dispatcher の生きている間に戻らない (中継が supervisor を待っている)")
	}
	me := strconv.Itoa(os.Getpid())
	sup := ""
	if b, err := os.ReadFile(filepath.Join(dir, dispatcher.SupervisorLockFile)); err == nil {
		sup = strings.TrimSpace(string(b))
	}
	if out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output(); err != nil {
		t.Fatalf("偽の dispatcher (pid %d) が release の前に居なくなった: %v", pid, err)
	} else if ppid := strings.TrimSpace(string(out)); ppid == me || ppid != sup {
		t.Fatalf("dispatcher (pid %d) の親が supervisor (pid %q) でない: ppid=%s (画面は %s)", pid, sup, ppid, me)
	}
	if out, err := exec.Command("ps", "-o", "ppid=", "-p", sup).Output(); err != nil || strings.TrimSpace(string(out)) == me {
		t.Fatalf("supervisor (pid %s) が居ない / 親が画面のまま: %q %v", sup, out, err)
	}
	release()
	for exec.Command("kill", "-0", sup).Run() == nil { // dispatcher が rc=0 で抜けたら supervisor も抜ける
		if time.Now().After(deadline) {
			t.Fatalf("dispatcher が抜けたのに supervisor (pid %s) が残った", sup)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for {
		out, err := exec.Command("ps", "-o", "ppid=,stat=", "-p", strconv.Itoa(pid)).Output()
		if err != nil { // 居ない = 刈り取られた
			return
		}
		f := strings.Fields(string(out))
		if len(f) == 2 && f[0] == me && strings.HasPrefix(f[1], "Z") {
			t.Fatalf("抜けた dispatcher (pid %d) が画面の子のゾンビで残った: ppid=%s stat=%s", pid, f[0], f[1])
		}
		if time.Now().After(deadline) {
			t.Fatalf("抜けた dispatcher (pid %d) が刈り取られない: %q", pid, strings.TrimSpace(string(out)))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// --join は --view / --mock と組まない (読むだけのつもりが書ける形を作らない)。サブコマンドの前にも付けない。--as には名前が要る (issue 481)。
func TestJoinFlagMisuseIsUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"--join", "--view"}, {"--view", "--join"}, {"--join", "--mock"}, {"--join", "dispatcher", "--stop"},
		{"--as"}, {"--join", "--as", "--e2e", "x"}, {"--view", "--as", "x"},
	} {
		var out, errOut bytes.Buffer
		if rc := run(args, strings.NewReader(""), &out, &errOut); rc != 2 {
			t.Fatalf("%v: rc=%d (2 のはず) %s", args, rc, errOut.String())
		}
	}
	f, rest, err := parseScreen([]string{"--as", "review", "--join", "--e2e", "/x"})
	if err != nil || !f.join || f.label != "review" || strings.Join(rest, " ") != "--e2e /x" || strings.Join(f.args, " ") != "--as review --join" {
		t.Fatalf("--join --as を読めない: %+v rest=%q err=%v", f, rest, err)
	}
}

// --join の画面は書く口を持つが、dispatcher を起こさない。居ない・人が止めてあるときは理由を出す (起動したときから join しか無い形)。
func TestWireLiveJoinWakesNothing(t *testing.T) {
	for _, held := range []bool{false, true} {
		dir := t.TempDir()
		if held {
			if err := store.Hold(dir, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		spawns := 0
		be, notes := wireLive(live.New(nil, t.TempDir(), dir), screenFlags{join: true}, dir,
			func(string) error { spawns++; return nil }, func(context.Context) error { return nil })
		_, joined := be.(backend.Joiner)
		_, readOnly := be.(backend.ReadOnly)
		all := strings.Join(notes, "\n")
		want := "dispatcher が動いていない。起こすのは持ち主の画面か pro-con dispatcher"
		if held {
			want = "join の画面からは外せない"
		}
		if !joined || readOnly || spawns != 0 || !strings.Contains(all, want) {
			t.Fatalf("held=%v: join=%v 読み取りだけ=%v 起こした=%d notes=%q", held, joined, readOnly, spawns, all)
		}
	}
}
