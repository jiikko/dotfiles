package doctorview_test

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/runner"
	"doctor/ssd"
	"doctor/svc"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
)

// doctorview.WaitDoctorCleanup は登録された走査が帰るまで戻らない。
// 🚨 判定は**時計ではなく順序**で見る (issue 211 / avoid-wall-clock-assertions)。
// 「走査が終わる前に Wait が戻ったか」をフラグの読み書き順で観測する。
func TestWaitDoctorCleanupWaitsForTrackedScan(t *testing.T) {
	drainDoctorCleanup(t)
	release := make(chan struct{})
	var finished bool
	var mu sync.Mutex

	doctorview.DoctorCleanup.Add()
	go func() {
		defer doctorview.DoctorCleanup.Done()
		<-release
		mu.Lock()
		finished = true
		mu.Unlock()
	}()

	waited := make(chan struct{})
	go func() { doctorview.WaitDoctorCleanup(); close(waited) }()

	select {
	case <-waited:
		t.Fatal("走査が終わる前に doctorview.WaitDoctorCleanup が戻った (看取っていない)")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("走査が終わったのに doctorview.WaitDoctorCleanup が戻らない")
	}
	mu.Lock()
	defer mu.Unlock()
	if !finished {
		t.Fatal("走査の完了前に Wait が抜けた")
	}
}

// start が起こす 3 つの走査 (disk / svc / brew) が **それぞれ** latch に載る。
//
// 🚨 判定を「子プロセスが死んだか」にしてはいけない。テストプロセスでは
// `exec.CommandContext` の watchdog goroutine が生きているので、**latch が無くても少し遅れて
// 殺される** = latch の有無を判別できない (実測 2026-09-03: 子の生死で見た版は
// 「disk を latch から外す」変異を素通しし、しかも数 ms のタイミングで flaky だった)。
// latch が守るのは「**走査が帰るまで Wait が戻らない**」という順序なので、それを直接見る。
//
// 🚨 3 つまとめて 1 回試す形でも足りない。1 経路だけ外した変異が緑になるので、
// 「その経路だけが時間のかかる子を持つ」fixture を経路ごとに作って回す。
func TestEachDoctorScanIsTrackedUntilItReturns(t *testing.T) {
	for _, tc := range []string{"disk", "svc", "brew", "ssd"} {
		t.Run(tc, func(t *testing.T) {
			drainDoctorCleanup(t)
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

			v := &doctorview.DoctorView{}
			diskRun, svcRun, brewRun, ssdRun := quiet, quiet, quiet, quiet
			diskCatalog := []disk.Entry{}
			switch tc {
			case "disk":
				diskRun = slow
				// 🚨 Run が呼ばれる Guard を選ぶ。素のエントリは du だけで Run を使わない (実測)
				diskCatalog = []disk.Entry{{
					ID: "test-entry", Label: "テスト用", Tier: 1,
					Guard: disk.GuardProcessAbsent, Processes: []string{"NoSuchApp"},
					Paths: []string{t.TempDir()},
				}}
			case "svc":
				svcRun = slow
			case "brew":
				brewRun = slow
			case "ssd":
				ssdRun = slow
			}
			v.DiskOpts = func() disk.Options { return disk.Options{Catalog: diskCatalog, Run: diskRun} }
			v.SvcOpts = func() svc.Options { return svc.Options{Dirs: nil, Run: svcRun} }
			v.BrewRun = brewRun
			v.DockerOpts = doctortest.NoDockerOptions // 本物の docker system df を叩かない (issue 419)
			v.SsdOpts = func() ssd.Options {
				return ssd.Options{Run: ssdRun, LookPath: func(string) (string, error) { return "/opt/homebrew/bin/smartctl", nil }}
			}

			cmd := v.Start(true)
			if cmd == nil {
				t.Fatal("start が Cmd を返さない")
			}
			// 🚨 `tea.Batch` は**中の Cmd を実行しない**。BatchMsg (Cmd の並び) を返すだけで、
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

			v.Stop() // = cancelAll 相当 (ctx を切るだけ。子の死は待たない)
			doctorview.WaitDoctorCleanup()

			mu.Lock()
			defer mu.Unlock()
			if !finished {
				t.Fatalf("%s: 走査が帰る前に doctorview.WaitDoctorCleanup が戻った (この経路が latch に載っていない)", tc)
			}
		})
	}
}

// 削除 (下見・本番) も latch に載り、stop() で ctx が切れる (issue 211 の敵対的レビュー P1)。
// 🚨 ここが抜けていると、いちばん危ない経路 (rm / trash / brew cleanup と、その後の
// インベントリ記録) が終了・再起動で watchdog ごと消えて走り続ける。
func TestDeleteIsTrackedAndCancelled(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		name := "本番"
		if dryRun {
			name = "下見"
		}
		t.Run(name, func(t *testing.T) {
			drainDoctorCleanup(t)
			ctxCh := make(chan context.Context, 1)
			var mu sync.Mutex
			var finished bool

			v := &doctorview.DoctorView{}
			v.DeleteFn = func(ctx context.Context, targets []disk.Result, opt disk.DeleteOptions) (disk.DeleteReport, error) {
				ctxCh <- ctx
				<-ctx.Done() // cancel されるまで帰らない (実際の rm / brew cleanup の代わり)
				mu.Lock()
				finished = true
				mu.Unlock()
				return disk.DeleteReport{}, ctx.Err()
			}

			cmd := v.StartDelete([]disk.Result{{}}, dryRun)
			if cmd == nil {
				t.Fatal("startDelete が Cmd を返さない")
			}
			batch, ok := cmd().(tea.BatchMsg)
			if !ok {
				t.Fatalf("startDelete が tea.Batch を返していない (%T)", cmd())
			}
			for _, c := range batch {
				if c != nil {
					go c()
				}
			}

			var ctx context.Context
			select {
			case ctx = <-ctxCh:
			case <-time.After(10 * time.Second):
				t.Fatal("判定不能: deleteFn が呼ばれない")
			}
			if ctx.Err() != nil {
				t.Fatal("判定不能: 削除の ctx が最初から切れている")
			}

			v.Stop() // = cancelAll 相当

			// 🚨 **stop() の直後に判定する**。「待っている間に切れたか」で見ると、別経路が遅れて
			//    cancel しても緑になり、`stop()` から削除の ctx を外す変異を素通しした
			//    (実測 2026-09-03。cancel は同期なので、戻った時点で切れているのが契約)
			if ctx.Err() == nil {
				t.Fatal("stop() で削除の ctx が切られていない (終了・再起動で rm / brew cleanup が走り続ける)")
			}

			doctorview.WaitDoctorCleanup()
			mu.Lock()
			defer mu.Unlock()
			if !finished {
				t.Fatal("削除が帰る前に doctorview.WaitDoctorCleanup が戻った (latch に載っていない)")
			}
		})
	}
}

// rescan (r) は前世代を止めてから世代を進める (issue 211 の敵対的レビュー P1)。
// 🚨 止めないと前世代の走査を誰も cancel できず、latch に載ったまま完走するので
// doctorview.WaitDoctorCleanup の上限が 2 秒ではなく数分になる (latch を入れる前は即終了だった =
// この修正が無いと 211 が hang を新設する)。
func TestRescanCancelsPreviousGeneration(t *testing.T) {
	drainDoctorCleanup(t)
	started := make(chan struct{})
	returned := make(chan struct{})
	var once sync.Once
	// 🚨 ctx を掴んで Err() を見る形は駄目だった: Run に渡るのは `runner.WithTimeout` の
	//    **派生 ctx** で、呼び出しが帰った時点で defer cancel により Done になる。
	//    「最初から切れている」と誤診する (実測 2026-09-03)。**cancel されるまで帰らない Run** を
	//    置いて、rescan で帰ってくるかを見る
	blocking := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		// 🚨 この closure は **世代ごとに呼ばれる** (rescan の 2 世代目も brew を回す)。
		//    素の close だと 2 回目で panic する (実測 -count=2)
		once.Do(func() { close(returned) })
		return "", "", 0, ctx.Err()
	}
	quiet := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		return "", "", 0, nil
	}

	v := &doctorview.DoctorView{}
	v.DiskOpts = func() disk.Options { return disk.Options{Catalog: []disk.Entry{}, Run: quiet} }
	v.SvcOpts = func() svc.Options { return svc.Options{Dirs: nil, Run: quiet} }
	v.BrewRun = blocking

	runCmd := func(c tea.Cmd) {
		if c == nil {
			return
		}
		if batch, ok := c().(tea.BatchMsg); ok {
			for _, x := range batch {
				if x != nil {
					go x()
				}
			}
		}
	}

	runCmd(v.Start(true))
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("判定不能: 1 世代目の Run が呼ばれない")
	}

	runCmd(v.Rescan()) // 2 世代目。start の冒頭で前世代を止めるはず

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("rescan が前世代を cancel していない (誰も止められない走査が latch に残り、doctorview.WaitDoctorCleanup が数分待つ)")
	}
	v.Stop() // 2 世代目も止める (ブロックした goroutine と latch のカウントを残さない)
	doctorview.WaitDoctorCleanup()
}

// drainDoctorCleanup は前のテストが残した latch のカウントを吸う。
// 🚨 `doctorview.DoctorCleanup` は **package 変数**なので glogx パッケージのテスト全体で共有される
// (敵対的レビュー 2026-09-03 のぼやき)。前のテストの走査が残っていると、こちらの Wait が
// それを待って誤判定する (実測 -count=2 で落ちた)。**この共有ゆえに `t.Parallel()` を足せない**。
func drainDoctorCleanup(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { <-doctorview.DoctorCleanup.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("判定不能: 前のテストの走査が残っている (latch が空にならない)")
	}
}
