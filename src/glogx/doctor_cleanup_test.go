package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
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
	drainDoctorCleanup(t)
	release := make(chan struct{})
	var finished bool
	var mu sync.Mutex

	doctorCleanup.add()
	go func() {
		defer doctorCleanup.done()
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

// 再起動経路と終了経路の**配線**を固定する (issue 211)。
//
// ⚠️ `syscall.Exec` を通る経路は単体テストで走らせられないので、ソースの構造で固定する。
// **文字列検索では駄目**: `waitDoctorCleanup()` をコメントに変える変異が素通りした
// (敵対的レビュー 2026-09-03 の P2)。go/ast で「その関数の呼び出し」だけを見る。
func TestRestartAndExitPathsWaitForDoctorCleanup(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("main.go を parse できない: %v", err)
	}

	// --- 再起動経路: `if model.restartRequested` の**ブロックの中だけ**を見る ---
	// ⚠️ ファイル全体から waitDoctorCleanup の呼び出しを探すと、**終了経路の defer を拾って**
	//    再起動経路の呼び出しが消えても緑になる (変異検証 2026-09-03 で実際に素通りした)。
	//    ブロックを特定してからその中を見る
	var block *ast.BlockStmt
	ast.Inspect(f, func(n ast.Node) bool {
		ifst, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if sel, ok := ifst.Cond.(*ast.SelectorExpr); ok && sel.Sel.Name == "restartRequested" {
			block = ifst.Body
			return false
		}
		return true
	})
	if block == nil {
		t.Fatal("`if model.restartRequested` のブロックが見つからない (main.go の構造が変わった)")
	}

	var order []string
	ast.Inspect(block, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if name := callName(c); name != "" {
				order = append(order, name)
			}
		}
		return true
	})
	idx := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}
	iCancel, iWait, iExec := idx("browse.cancelAll"), idx("waitDoctorCleanup"), idx("restartSelf")
	if iExec < 0 {
		t.Fatalf("restart ブロックに restartSelf() が無い (呼び出し: %v)", order)
	}
	if iWait < 0 {
		t.Fatalf("restart ブロックで waitDoctorCleanup() を呼んでいない (doctor の子孫が exec を越えて残る。呼び出し: %v)", order)
	}
	if iCancel < 0 {
		t.Fatalf("restart ブロックに browse.cancelAll() が無い (呼び出し: %v)", order)
	}
	if iCancel >= iWait || iWait >= iExec {
		t.Fatalf("順序が違う (cancelAll → waitDoctorCleanup → restartSelf であること): %v", order)
	}

	// --- 終了経路: defer は LIFO なので「cancelAll を後に積む」= cancel が先に走る ---
	var dWait, dCancel token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		switch callName(d.Call) {
		case "waitDoctorCleanup":
			dWait = d.Pos()
		case "browse.cancelAll":
			dCancel = d.Pos()
		}
		return true
	})
	if dWait == 0 {
		t.Fatal("defer waitDoctorCleanup() が無い (シグナル終了で doctor の子孫が残る)")
	}
	if dCancel == 0 {
		t.Fatal("defer browse.cancelAll() が無い (main.go の構造が変わった)")
	}
	if dWait >= dCancel {
		t.Fatal("defer の順序が逆 (LIFO なので cancelAll を後に積む。cancel より先に待つと走査が終わるまで固まる)")
	}
}

// callName は呼び出し先の名前を "pkg.Fn" / "recv.Fn" / "Fn" の形で返す。
func callName(c *ast.CallExpr) string {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	}
	return ""
}

// 削除 (下見・本番) も latch に載り、stop() で ctx が切れる (issue 211 の敵対的レビュー P1)。
// ⚠️ ここが抜けていると、いちばん危ない経路 (rm / trash / brew cleanup と、その後の
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

			v := &doctorView{}
			v.deleteFn = func(ctx context.Context, targets []disk.Result, opt disk.DeleteOptions) (disk.DeleteReport, error) {
				ctxCh <- ctx
				<-ctx.Done() // cancel されるまで帰らない (実際の rm / brew cleanup の代わり)
				mu.Lock()
				finished = true
				mu.Unlock()
				return disk.DeleteReport{}, ctx.Err()
			}

			cmd := v.startDelete([]disk.Result{{}}, dryRun)
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

			v.stop() // = cancelAll 相当

			// ⚠️ **stop() の直後に判定する**。「待っている間に切れたか」で見ると、別経路が遅れて
			//    cancel しても緑になり、`stop()` から削除の ctx を外す変異を素通しした
			//    (実測 2026-09-03。cancel は同期なので、戻った時点で切れているのが契約)
			if ctx.Err() == nil {
				t.Fatal("stop() で削除の ctx が切られていない (終了・再起動で rm / brew cleanup が走り続ける)")
			}

			waitDoctorCleanup()
			mu.Lock()
			defer mu.Unlock()
			if !finished {
				t.Fatal("削除が帰る前に waitDoctorCleanup が戻った (latch に載っていない)")
			}
		})
	}
}

// rescan (r) は前世代を止めてから世代を進める (issue 211 の敵対的レビュー P1)。
// ⚠️ 止めないと前世代の走査を誰も cancel できず、latch に載ったまま完走するので
// waitDoctorCleanup の上限が 2 秒ではなく数分になる (latch を入れる前は即終了だった =
// この修正が無いと 211 が hang を新設する)。
func TestRescanCancelsPreviousGeneration(t *testing.T) {
	drainDoctorCleanup(t)
	started := make(chan struct{})
	returned := make(chan struct{})
	var once sync.Once
	// ⚠️ ctx を掴んで Err() を見る形は駄目だった: Run に渡るのは `runner.WithTimeout` の
	//    **派生 ctx** で、呼び出しが帰った時点で defer cancel により Done になる。
	//    「最初から切れている」と誤診する (実測 2026-09-03)。**cancel されるまで帰らない Run** を
	//    置いて、rescan で帰ってくるかを見る
	blocking := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		// ⚠️ この closure は **世代ごとに呼ばれる** (rescan の 2 世代目も brew を回す)。
		//    素の close だと 2 回目で panic する (実測 -count=2)
		once.Do(func() { close(returned) })
		return "", "", 0, ctx.Err()
	}
	quiet := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		return "", "", 0, nil
	}

	v := &doctorView{}
	v.diskOpts = func() disk.Options { return disk.Options{Catalog: []disk.Entry{}, Run: quiet} }
	v.svcOpts = func() svc.Options { return svc.Options{Dirs: nil, Run: quiet} }
	v.brewRun = blocking

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

	runCmd(v.start(true))
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("判定不能: 1 世代目の Run が呼ばれない")
	}

	runCmd(v.rescan()) // 2 世代目。start の冒頭で前世代を止めるはず

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("rescan が前世代を cancel していない (誰も止められない走査が latch に残り、waitDoctorCleanup が数分待つ)")
	}
	v.stop() // 2 世代目も止める (ブロックした goroutine と latch のカウントを残さない)
	waitDoctorCleanup()
}

// drainDoctorCleanup は前のテストが残した latch のカウントを吸う。
// ⚠️ `doctorCleanup` は **package 変数**なので glogx パッケージのテスト全体で共有される
// (敵対的レビュー 2026-09-03 のぼやき)。前のテストの走査が残っていると、こちらの Wait が
// それを待って誤判定する (実測 -count=2 で落ちた)。**この共有ゆえに `t.Parallel()` を足せない**。
func drainDoctorCleanup(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { <-doctorCleanup.wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("判定不能: 前のテストの走査が残っている (latch が空にならない)")
	}
}

// 走査中に行が増えても、選択は同じエントリに留まる (issue 210)。
//
// ⚠️ **カーソルより上に挿入する** fixture を使う。disk 行は Size 降順に並べ替えて描くので、
// 大きい結果が後から届くと既存の行の上に入る。下に挿入する形では index が偶然一致して
// 素通りする (issue 210 のテスト観点)。
func TestCursorStaysOnSameRowWhenRowsGrowAbove(t *testing.T) {
	v := &doctorView{expanded: map[string]bool{}, selected: map[string]bool{}, inspected: map[string]bool{}}
	small := disk.Result{Entry: disk.Entry{ID: "small", Label: "小", Tier: 1}, Size: 100, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/small", Size: 100}}}
	mid := disk.Result{Entry: disk.Entry{ID: "mid", Label: "中", Tier: 1}, Size: 200, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/mid", Size: 200}}}
	huge := disk.Result{Entry: disk.Entry{ID: "huge", Label: "大", Tier: 1}, Size: 999999, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/huge", Size: 999999}}}

	o := doctorRenderOpts{page: 40, width: 100}
	v.diskResults = []disk.Result{small, mid}
	v.lines(o) // 1 度描いて rows を組む

	// 一番上の選べる行から 1 つ下へ動く (= 2 番目のエントリを選ぶ)
	v.moveCursor(1)
	before := v.rows[v.cursor].key
	if before == "" {
		t.Fatal("判定不能: 選択行に key が無い")
	}

	// 走査中に「もっと大きい結果」が届く = 並べ替えでカーソルより上に入る
	v.diskResults = append(v.diskResults, huge)
	v.lines(o)

	after := v.rows[v.cursor].key
	if after != before {
		t.Fatalf("走査中に選択が別の行へ移った: before=%s after=%s (y / Y が別エントリをコピーする)", before, after)
	}
}

// 選んでいた行が消えたら近傍へ寄せ、**寄せたことを画面に出す** (issue 210 の敵対レビュー P1)。
//
// ⚠️ フィールド (`pendingToast`) を見るだけでは足りない。表示するのは tui.go の
// `case doctorToast:` = handleKey の戻り値経路だけで、`restoreCursor` は View から呼ばれる。
// **browseModel 経由でトーストが出ることまで**見ないと、配線の穴を守れない (実測: 初版は
// フィールド代入しか見ておらず、本番では一度も出ない状態で緑だった)。
func TestCursorFallbackIsToldThroughBrowseModel(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	v := m.doctorOv
	v.shown = true
	a := disk.Result{Entry: disk.Entry{ID: "a", Label: "A", Tier: 1}, Size: 300, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/a", Size: 300}}}
	b := disk.Result{Entry: disk.Entry{ID: "b", Label: "B", Tier: 1}, Size: 200, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/b", Size: 200}}}

	o := doctorRenderOpts{page: 40, width: 100}
	v.diskResults = []disk.Result{a, b}
	v.lines(o)
	v.moveCursor(1)
	gone := v.rows[v.cursor].key

	// 再走査で b が落ちた → 描画で寄せる
	v.diskResults = []disk.Result{a}
	v.lines(o)
	if v.rows[v.cursor].key == gone {
		t.Fatal("消えた行を指したまま")
	}

	// 次のキー操作で**画面に**出る
	m.toast.text = ""
	if act := v.handleKey("j", 20); act != doctorToast {
		t.Fatalf("寄せた後の最初のキーが doctorToast を返さない (act=%v)", act)
	}
	m.toast.show(v.pendingToast, false) // tui.go の case doctorToast: と同じ扱い
	if !strings.Contains(m.toast.text, "近くの行へ移りました") {
		t.Fatalf("寄せたことが画面に出ない (toast=%q)", m.toast.text)
	}
	// 一度出したら再発しない (毎キー出ると邪魔)
	if act := v.handleKey("j", 20); act == doctorToast {
		t.Fatal("同じ寄せで 2 回目もトーストを返した")
	}
}

// G (末尾へ) が次の描画で巻き戻らない (issue 210 の敵対レビュー P1-1。私が入れた回帰)。
//
// ⚠️ `moveCursor(0)` を「移動」として使う経路 (G) が key を覚えないと、`restoreCursor` が
// 古い key の行へ cursor を戻す。実測された症状: after G cursor=6 / cursorKey="disk:b" →
// repaint で cursor=4 へ巻き戻る。
func TestCursorEndKeySurvivesRepaint(t *testing.T) {
	v := &doctorView{expanded: map[string]bool{}, selected: map[string]bool{}, inspected: map[string]bool{}}
	mk := func(id string, size int64) disk.Result {
		return disk.Result{Entry: disk.Entry{ID: id, Label: id, Tier: 1}, Size: size, Status: disk.StatusOK,
			Items: []disk.Item{{Path: "/tmp/" + id, Size: size}}}
	}
	o := doctorRenderOpts{page: 40, width: 100}
	v.diskResults = []disk.Result{mk("a", 300), mk("b", 200), mk("c", 100)}
	v.lines(o)

	// G 相当: 末尾へ飛んで寄せる
	v.cursor = len(v.rows) - 1
	v.moveCursor(0)
	atEnd := v.rows[v.cursor].key
	if atEnd == "" {
		t.Fatal("判定不能: 末尾の選べる行に key が無い")
	}

	v.lines(o) // 次の描画
	if got := v.rows[v.cursor].key; got != atEnd {
		t.Fatalf("G の後の描画で巻き戻った: at end=%s after repaint=%s", atEnd, got)
	}
}

// 選べる行が 0 件のフレームを挟んでも key を捨てない (issue 210 の敵対レビュー P2)。
// 捨てると index 保持へ退行する。
func TestCursorKeySurvivesFrameWithoutSelectableRows(t *testing.T) {
	v := &doctorView{expanded: map[string]bool{}, selected: map[string]bool{}, inspected: map[string]bool{}}
	a := disk.Result{Entry: disk.Entry{ID: "a", Label: "A", Tier: 1}, Size: 300, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/a", Size: 300}}}
	b := disk.Result{Entry: disk.Entry{ID: "b", Label: "B", Tier: 1}, Size: 200, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/b", Size: 200}}}
	o := doctorRenderOpts{page: 40, width: 100}
	v.diskResults = []disk.Result{a, b}
	v.lines(o)
	v.moveCursor(1)
	want := v.cursorKey
	if want == "" {
		t.Fatal("判定不能: key を覚えていない")
	}

	// 選べる行が 1 つも無いフレーム (結果が消えた瞬間)
	v.diskResults = nil
	v.lines(o)

	// 結果が戻ってきたら元の行へ復帰する。
	// ⚠️ **戻すときに「上へ増える」形にする**。同じ 2 件で戻すと、key を捨てる退行でも
	//    index が偶然一致して緑になる (変異検証 2026-09-03 で実際に素通りした)
	huge := disk.Result{Entry: disk.Entry{ID: "huge", Label: "H", Tier: 1}, Size: 999999, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/huge", Size: 999999}}}
	v.diskResults = []disk.Result{a, b, huge}
	v.lines(o)
	if v.rows[v.cursor].key != want {
		t.Fatalf("空フレームを挟んだら別の行へ移った: want=%s got=%s", want, v.rows[v.cursor].key)
	}
}
