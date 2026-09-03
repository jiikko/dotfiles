package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/runner"
	"doctor/svc"
)

// waitDoctorCleanup は登録された走査が帰るまで戻らない。
// ⚠️ 判定は**時計ではなく順序**で見る (issue 211 / avoid-wall-clock-assertions)。
// 「走査が終わる前に Wait が戻ったか」をフラグの読み書き順で観測する。
func TestWaitDoctorCleanupWaitsForTrackedScan(t *testing.T) {
	release := make(chan struct{})
	var finished bool
	var mu sync.Mutex

	doctorCleanup.Add(1)
	go func() {
		defer doctorCleanup.Done()
		<-release
		mu.Lock()
		finished = true
		mu.Unlock()
	}()

	waited := make(chan struct{})
	go func() { waitDoctorCleanup(); close(waited) }()

	select {
	case <-waited:
		t.Fatal("走査が終わる前に waitDoctorCleanup が戻った (看取っていない)")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("走査が終わったのに waitDoctorCleanup が戻らない")
	}
	mu.Lock()
	defer mu.Unlock()
	if !finished {
		t.Fatal("走査の完了前に Wait が抜けた")
	}
}

// start が起こす 3 つの走査 (disk / svc / brew) が **それぞれ** latch に載る。
//
// ⚠️ 判定を「子プロセスが死んだか」にしてはいけない。テストプロセスでは
// `exec.CommandContext` の watchdog goroutine が生きているので、**latch が無くても少し遅れて
// 殺される** = latch の有無を判別できない (実測 2026-09-03: 子の生死で見た版は
// 「disk を latch から外す」変異を素通しし、しかも数 ms のタイミングで flaky だった)。
// latch が守るのは「**走査が帰るまで Wait が戻らない**」という順序なので、それを直接見る。
//
// ⚠️ 3 つまとめて 1 回試す形でも足りない。1 経路だけ外した変異が緑になるので、
// 「その経路だけが時間のかかる子を持つ」fixture を経路ごとに作って回す。
func TestEachDoctorScanIsTrackedUntilItReturns(t *testing.T) {
	for _, tc := range []string{"disk", "svc", "brew"} {
		t.Run(tc, func(t *testing.T) {
			var mu sync.Mutex
			var finished bool
			started := make(chan struct{}, 1)

			// この経路だけ「cancel されるまで帰らない子」を起こす。帰った事実を記録する
			slow := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
				select {
				case started <- struct{}{}:
				default:
				}
				out, errs, rc, err := runner.Exec(ctx, "sh", "-c", "exec sleep 30")
				mu.Lock()
				finished = true
				mu.Unlock()
				return out, errs, rc, err
			}
			quiet := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
				return "", "", 0, nil
			}

			v := &doctorView{}
			diskRun, svcRun, brewRun := quiet, quiet, quiet
			diskCatalog := []disk.Entry{}
			switch tc {
			case "disk":
				diskRun = slow
				// ⚠️ Run が呼ばれる Guard を選ぶ。素のエントリは du だけで Run を使わない (実測)
				diskCatalog = []disk.Entry{{
					ID: "test-entry", Label: "テスト用", Tier: 1,
					Guard: disk.GuardProcessAbsent, Processes: []string{"NoSuchApp"},
					Paths: []string{t.TempDir()},
				}}
			case "svc":
				svcRun = slow
			case "brew":
				brewRun = slow
			}
			v.diskOpts = func() disk.Options { return disk.Options{Catalog: diskCatalog, Run: diskRun} }
			v.svcOpts = func() svc.Options { return svc.Options{Dirs: nil, Run: svcRun} }
			v.brewRun = brewRun

			cmd := v.start(true)
			if cmd == nil {
				t.Fatal("start が Cmd を返さない")
			}
			// ⚠️ `tea.Batch` は**中の Cmd を実行しない**。BatchMsg (Cmd の並び) を返すだけで、
			//    実行は runtime が行う。`go cmd()` で済ませると closure が 1 つも走らない
			batch, ok := cmd().(tea.BatchMsg)
			if !ok {
				t.Fatalf("start が tea.Batch を返していない (%T)", cmd())
			}
			for _, c := range batch {
				if c != nil {
					go c()
				}
			}

			// 走査が始まった証拠を待つ (始まっていなければ判定不能)
			select {
			case <-started:
			case <-time.After(15 * time.Second):
				t.Fatalf("判定不能: %s の Run が呼ばれない (fixture が経路を通っていない)", tc)
			}

			v.stop() // = cancelAll 相当 (ctx を切るだけ。子の死は待たない)
			waitDoctorCleanup()

			mu.Lock()
			defer mu.Unlock()
			if !finished {
				t.Fatalf("%s: 走査が帰る前に waitDoctorCleanup が戻った (この経路が latch に載っていない)", tc)
			}
		})
	}
}

// 再起動経路の**配線**を静的に固定する (issue 211)。
//
// ⚠️ `syscall.Exec` を通る経路は単体テストで走らせられないので、latch を待つ関数があることと
// 「cancelAll と restartSelf の間で呼んでいること」を**ソースの順序**で固定する。
// 関数だけ用意して呼び忘れると、latch は緑のまま実害 (brew の子孫が走り続ける) が残る
// (_claude/rules/verify-execution-not-just-exit-code.md: 配線されているかを別に見る)。
func TestRestartPathWaitsForDoctorCleanup(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("main.go を読めない: %v", err)
	}
	src := string(b)

	iCancel := strings.Index(src, "browse.cancelAll()\n\t\t// ")
	if iCancel < 0 {
		// 直後にコメントが無い書き方でも拾えるように、restart ブロック内の cancelAll を探す
		iRestart := strings.Index(src, "if model.restartRequested {")
		if iRestart < 0 {
			t.Fatal("restart ブロックが見つからない (main.go の構造が変わった)")
		}
		iCancel = strings.Index(src[iRestart:], "browse.cancelAll()")
		if iCancel < 0 {
			t.Fatal("restart ブロックに browse.cancelAll() が無い")
		}
		iCancel += iRestart
	}
	rest := src[iCancel:]
	iWait := strings.Index(rest, "waitDoctorCleanup()")
	iExec := strings.Index(rest, "restartSelf()")
	if iWait < 0 {
		t.Fatal("restart 経路で waitDoctorCleanup() を呼んでいない (doctor の子孫が exec を越えて残る)")
	}
	if iExec < 0 {
		t.Fatal("restart 経路に restartSelf() が無い (main.go の構造が変わった)")
	}
	if iWait > iExec {
		t.Fatal("waitDoctorCleanup() が restartSelf() より後にある (exec でプロセス像が消えるので待てない)")
	}
}
