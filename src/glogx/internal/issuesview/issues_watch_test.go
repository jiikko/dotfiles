package issuesview

import (
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/testenv"
	"glogx/issues"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"

	"testwait"
)

// openedWatchView は「viewer を開いてスキャン結果を受け取り、見張りの基準も取れた」状態を作る。
func openedWatchView(t *testing.T, root, path string) *IssuesView {
	t.Helper()
	v := newTestIssuesView()
	if cmd := v.Toggle(root); cmd == nil {
		t.Fatal("toggle が Cmd を返さない")
	}
	v.Receive(scanOf(t, root))
	// 最初の観測は基準取り (読み直さない)
	if cmd := v.HandleWatch(observeIssues(&v)); cmd == nil {
		t.Fatal("基準取りの後に次の観測が予約されない")
	}
	if v.watch.seen == "" {
		t.Fatal("基準の指紋が取れていない")
	}
	return &v
}

func TestIssuesWatchReloadsAfterExternalEdit(t *testing.T) {
	// 🚨 変化を見つけた最初の周期では読まない (書きかけを掴まないため)。安定を確かめた次の
	// 周期で reloadAfterEdit を返す。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: 編集前\n")
	v := openedWatchView(t, root, path)
	if got := v.rows[0].Display(); got != "feat: 編集前" {
		t.Fatalf("前提が崩れた: 一覧のタイトルが %q", got)
	}

	issuesviewtest.WriteIssue(t, path, "# 001 feat: 編集後\n", time.Now())
	changed := observeIssues(v)
	if cmd := v.HandleWatch(changed); cmd == nil {
		t.Fatal("変化を見つけた周期でも次の観測は予約する")
	}
	if v.watch.pending == "" {
		t.Fatal("変化が安定待ちに入っていない")
	}
	cmd := v.HandleWatch(changed) // 同じ指紋 = 安定した
	if cmd == nil {
		t.Fatal("安定した変化で取り直しの Cmd が返らない")
	}
	v.Receive(drainScanMsg(t, cmd))
	if got := v.rows[0].Display(); got != "feat: 編集後" {
		t.Fatalf("外部の編集が一覧に反映されていない: %q", got)
	}
	// 🚨 チェックボックスの進捗は観測点にしない。一覧は進捗を出さない
	// (Issue は本文を最後まで読まない。issues/parse.go の LoadMeta の doc)。
	// タイトルの差し替えで「メタデータが取り直された」は示せている
	// 取り直した直後の基準は「スキャンが読んだ時点の指紋」= 次の観測と一致する。自分の取り直しを
	// 外部の変化と誤検出しないことを、基準を空にする (= 次の観測を無条件に基準化する) のではなく
	// 一致で示す。空にすると、読んだ時刻と基準を取る時刻の差に入った編集を取りこぼす。
	if v.watch.pending != "" {
		t.Fatalf("取り直し後に安定待ちが残っている: %q", v.watch.pending)
	}
	if v.watch.seen != observeIssues(v).fp {
		t.Fatal("取り直し後の基準がスキャン時点の指紋と一致しない")
	}
	v.HandleWatch(observeIssues(v))
	if v.scanning {
		t.Fatal("自分の取り直しを外部の変化と誤検出して再スキャンした")
	}
}

func TestIssuesWatchDirsIncludeEpicAndEmptyGroups(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	cloud := filepath.Join(dir, issues.EpicDirName, "cloud")
	empty := filepath.Join(dir, issues.EpicDirName, "empty")
	cloudNext := filepath.Join(cloud, issues.NextDirName)
	if err := os.MkdirAll(cloud, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cloudNext, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	v := newTestIssuesView()
	v.dirs = []string{dir}
	dirs, _ := v.watchTargets()
	for _, want := range []string{dir, filepath.Join(dir, issues.EpicDirName), cloud, cloudNext, empty} {
		if !slices.Contains(dirs, want) {
			t.Fatalf("%q が watch 対象にない: %v", want, dirs)
		}
	}
	fp := issuesFingerprint(dirs, nil)
	if !strings.Contains(fp, filepath.Join(dir, issues.EpicDirName)+":entries:") ||
		!strings.Contains(fp, "cloud/\x00") || !strings.Contains(fp, "empty/\x00") {
		t.Fatalf("epic/ の entry list が指紋に含まれていない: %q", fp)
	}
	if err := os.WriteFile(filepath.Join(empty, "001-feat-first.md"), []byte("# 001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := issuesFingerprint(dirs, nil); got == fp {
		t.Fatal("空 group への最初の md 作成で指紋が変わらない")
	}
}

func TestIssuesWatchDirsUsesActualCaseInsensitiveEpicEntry(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	group := filepath.Join(dir, "EPIC", "cloud")
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatal(err)
	}

	v := newTestIssuesView()
	v.dirs = []string{dir}
	dirs, _ := v.watchTargets()
	for _, want := range []string{dir, filepath.Join(dir, "EPIC"), group} {
		if !slices.Contains(dirs, want) {
			t.Fatalf("大文字の Epic entry %q が watch 対象にない: %v", want, dirs)
		}
	}
	if slices.Contains(dirs, filepath.Join(dir, issues.EpicDirName)) {
		t.Fatalf("存在しない lowercase の Epic path を watch 対象にした: %v", dirs)
	}
	fp := issuesFingerprint(dirs, nil)
	if !strings.Contains(fp, filepath.Join(dir, "EPIC")+":entries:") {
		t.Fatalf("大文字の Epic entry list が指紋に含まれない: %q", fp)
	}
}

func TestIssuesWatchReloadsOpenBody(t *testing.T) {
	// 本文を開いたまま書き換えられたら本文も差し替わり、スクロール位置は保つ。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n\n本文の初版。\n")
	v := openedWatchView(t, root, path)
	v.HandleKey("enter", vp(20))
	v.drawer.finish() // 引き出しの演出は着地させる (アニメ中は反映を保留する = 別テストで見る)
	v.Lines(IssuesRenderOpts{Width: 80, Page: 20})
	if v.open == nil {
		t.Fatal("本文モードに入れていない")
	}

	issuesviewtest.WriteIssue(t, path, "# 001 feat: x\n\n本文の第 2 版。\n", time.Now())
	changed := observeIssues(v)
	v.HandleWatch(changed)
	cmd := v.HandleWatch(changed)
	if cmd == nil {
		t.Fatal("本文の変化で取り直しの Cmd が返らない")
	}
	v.Receive(drainScanMsg(t, cmd))
	out := strings.Join(v.Lines(IssuesRenderOpts{Width: 80, Page: 20}), "\n")
	if !strings.Contains(out, "第 2 版") {
		t.Fatalf("開いている本文が差し替わっていない:\n%s", out)
	}
}

func TestIssuesWatchIgnoresUnstableFile(t *testing.T) {
	// 書きかけ (周期ごとに指紋が変わる) の間は読まない。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	seen := v.watch.seen
	for i := range 3 {
		issuesviewtest.WriteIssue(t, path, strings.Repeat("a", i+1), time.Now().Add(time.Duration(i)*time.Second))
		if cmd := v.HandleWatch(observeIssues(v)); cmd == nil {
			t.Fatal("次の観測が予約されない")
		}
		// 🚨 「読まなかった」を直接見る: 基準 (seen) が変わらず、変化は安定待ち (pending) に留まる。
		// seen が空でないことだけを見ていた頃は、安定待ちを外して毎回すぐ読む変異でも緑だった
		if v.watch.seen != seen {
			t.Fatalf("%d 回目: 書きかけの指紋を基準に採った (取り直しが走った)", i+1)
		}
		if v.watch.pending == "" || v.scanning {
			t.Fatalf("%d 回目: 安定待ちに入っていない (pending=%q scanning=%v)", i+1, v.watch.pending, v.scanning)
		}
	}
}

func TestIssuesWatchDefersWhileURLPickerOpen(t *testing.T) {
	// ピッカーは本文の URL 集合を握っているので、下から差し替えると選択が別 URL に化ける。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n\nhttps://example.com/a\n")
	v := openedWatchView(t, root, path)
	v.urlPick.open([]string{"https://example.com/a"})

	issuesviewtest.WriteIssue(t, path, "# 001 feat: x\n\nhttps://example.com/b\n", time.Now())
	changed := observeIssues(v)
	v.HandleWatch(changed)
	if cmd := v.HandleWatch(changed); cmd == nil {
		t.Fatal("次の観測が予約されない")
	}
	if v.watch.pending == "" {
		t.Fatal("ピッカーを開いている間は反映を保留する (pending が消えた = 反映した)")
	}
	// 閉じたら次の周期で反映する
	v.urlPick = urlPicker{}
	cmd := v.HandleWatch(changed)
	if cmd == nil {
		t.Fatal("ピッカーを閉じた後も反映されない")
	}
	if v.watch.pending != "" {
		t.Fatalf("反映後も保留が残っている: %q", v.watch.pending)
	}
}

func TestIssuesWatchDefersWhileDrawerAnimating(t *testing.T) {
	// 引き出しの開閉アニメ中は着地まで待つ (レイアウトが動いている最中に本文を差し替えない)。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n\n初版。\n")
	v := openedWatchView(t, root, path)
	v.HandleKey("enter", vp(20)) // 引き出しが開くアニメが始まる

	issuesviewtest.WriteIssue(t, path, "# 001 feat: x\n\n第 2 版。\n", time.Now())
	changed := observeIssues(v)
	v.HandleWatch(changed)
	if cmd := v.HandleWatch(changed); cmd == nil {
		t.Fatal("次の観測が予約されない")
	}
	if v.watch.pending == "" {
		t.Fatal("アニメ中は反映を保留する (pending が消えた = 反映した)")
	}
	v.drawer.finish() // 着地したら次の周期で反映する
	if cmd := v.HandleWatch(changed); cmd == nil {
		t.Fatal("着地後も反映されない")
	}
	if v.watch.pending != "" {
		t.Fatalf("反映後も保留が残っている: %q", v.watch.pending)
	}
}

func TestIssuesWatchStopsWhenClosed(t *testing.T) {
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	v.close()
	if cmd := v.HandleWatch(observeIssues(v)); cmd != nil {
		t.Fatal("閉じた後も見張りのチェーンが続いている (tick が残る)")
	}
	if v.watch.W != nil || v.watch.seen != "" || v.watch.EvArmed || v.watch.PollArmed {
		t.Fatalf("閉じたのに見張りの状態が残っている (watcher の fd も残る): %+v", v.watch)
	}
	// 閉じている間は張り直しもしない
	if cmd := v.watchCmd(); cmd != nil {
		t.Fatal("閉じている viewer で watchCmd が Cmd を返した")
	}
}

func TestIssuesWatchIsSingleFlight(t *testing.T) {
	// i の連打・復元と重なっても見張りのチェーンを 2 本にしない (maybeTick と同じ規律)。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if cmd := v.watchCmd(); cmd != nil {
		t.Fatal("既に張っているのに 2 本目のチェーンを作った")
	}
	v.HandleWatch(observeIssues(v)) // 1 拍消費すれば張り直せる
	if !v.watch.PollArmed {
		t.Fatal("消費後に張り直していない")
	}
}

// 🚨 回帰防止 (リーク監査 2026-08-01): ポーリング由来の観測でイベント待ちの札まで降ろすと、
// まだ w.Events でブロックしている goroutine が生きているのに 2 本目が張られ、観測 1 回につき
// 1 本ずつ積み上がる。平常時のポーリングは 30s 周期なので、viewer を開きっぱなしにするだけで
// 増え続ける (回収は viewer を閉じたときだけ)。
//
// 札の状態では検出できない: handleWatch は最後に watchCmd で張り直すので、両方降ろす実装でも
// 事後の札は同じ true になる。実際に goroutine を走らせ、watcher を閉じたときに返ってくる
// closed の数 = 生きているイベント待ちの本数を数える。
func TestIssuesWatchPollDoesNotStackEventChains(t *testing.T) {
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if v.watch.W == nil {
		testenv.SkipEnv(t, "この環境では fsnotify を作れない (ポーリングへ縮退する経路)")
	}
	msgs := make(chan tea.Msg, 64)
	v.watch.EvArmed = false // 既に張られている札をテストが引き取り、実体の goroutine を作る
	runWatchChains(t, v.eventCmd(), msgs)

	for range 3 {
		runWatchChains(t, v.HandleWatch(observeIssues(v)), msgs) // ポーリング由来 (fromEvent=false)
	}
	if n := liveEventChains(v, msgs); n != 1 {
		t.Fatalf("イベント待ちの goroutine が %d 本生きている (1 本のはず): 観測のたびに積み上がっている", n)
	}
}

// runWatchChains は watchCmd が返す Cmd を bubbletea と同じように走らせる (tea.Batch は
// BatchMsg で複数の Cmd を返すので展開する)。結果の Msg は msgs へ流す。
func runWatchChains(t *testing.T, cmd tea.Cmd, msgs chan<- tea.Msg) {
	t.Helper()
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			msgs <- msg // tea.Batch は 1 本だけなら畳んでそのまま返す
			return
		}
		for _, c := range batch {
			if c != nil {
				go func(c tea.Cmd) { msgs <- c() }(c)
			}
		}
	}()
}

// liveEventChains は生きているイベント待ちの本数を数える。watcher を閉じるとブロックしている
// goroutine がそれぞれ closed をちょうど 1 通返すので、静まるまで数えれば本数が出る
// (保険のポーリングは 30s 先の tea.Tick なのでこの窓には混ざらない)。
func liveEventChains(v *IssuesView, msgs <-chan tea.Msg) int {
	v.StopWatch()
	n := 0
	for {
		select {
		case msg := <-msgs:
			if m, ok := msg.(IssuesWatchMsg); ok && m.closed {
				n++
			}
		case <-time.After(300 * time.Millisecond):
			return n
		}
	}
}

// スキャンが読んだ内容と見張りの基準がずれると、その間に入った編集を永久に取りこぼす。
//
// 基準を「最初の観測」で取ると、スキャン (内容を読んだ時刻) と基準取り (最大 1 周期あと) の間の
// 編集が基準に焼き込まれ、以降は「変化なし」に見える。取りこぼしは次の編集が来るまで解消せず、
// viewer は編集前の内容を出し続ける = この機能が塞ぎに来た「viewer が確信を持って嘘をつく」状態。
//
// Claude Code のように数秒おきに書くツールでは、取り直し直後の書き込みが普通に起きる。
func TestIssuesWatchCatchesEditRacingTheBaseline(t *testing.T) {
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: 編集前\n")
	v := newTestIssuesView()
	if cmd := v.Toggle(root); cmd == nil {
		t.Fatal("toggle が Cmd を返さない")
	}
	v.Receive(scanOf(t, root)) // スキャンはここまでの内容を読んだ

	issuesviewtest.WriteIssue(t, path, "# 001 feat: 編集後\n", time.Now()) // 基準取りより前に外部が書く

	// 以降どれだけ観測しても指紋は「編集後」で一定なので、基準取りで吸収されると二度と気づけない。
	for range 3 {
		v.HandleWatch(observeIssues(&v))
	}
	if !v.scanning {
		t.Fatal("スキャンと基準取りの間に入った編集を取りこぼした (一覧が編集前のまま固まる)")
	}
}

func TestIssuesWatchReloadsFromRealEvent(t *testing.T) {
	// イベント経路の結合: 実際に fsnotify のイベントで起こされ、指紋で判定して取り直しへ進む。
	// 🚨 指紋が正本 (イベントは Create/Rename/Write と嘘をつくので、起こす役だけ)。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: 編集前\n")
	v := openedWatchView(t, root, path)
	if v.watch.W == nil {
		testenv.SkipEnv(t, "この環境では fsnotify を作れない (ポーリングへ縮退する経路)")
	}
	v.watch.EvArmed = false // 既に張られているチェーンをテストが引き取る
	cmd := v.eventCmd()
	if cmd == nil {
		t.Fatal("イベント待ちの Cmd が作れない")
	}
	issuesviewtest.WriteIssue(t, path, "# 001 feat: 編集後\n", time.Now())

	msg, ok := waitMsg(t, cmd, 5*time.Second).(IssuesWatchMsg)
	if !ok || msg.closed {
		t.Fatalf("イベントで観測が返らない: %+v", msg)
	}
	if msg.fp == v.watch.seen {
		t.Fatal("イベント経路の指紋が変化を捉えていない")
	}
	v.HandleWatch(msg)                                  // 1 回目: 安定待ち
	reload := v.HandleWatch(IssuesWatchMsg{fp: msg.fp}) // 2 回目: 安定 → 取り直し
	if reload == nil {
		t.Fatal("イベント由来の変化で取り直しが走らない")
	}
	v.Receive(drainScanMsg(t, reload))
	if got := v.rows[0].Display(); got != "feat: 編集後" {
		t.Fatalf("イベント経由の編集が反映されていない: %q", got)
	}
}

func TestIssuesWatchClosingReleasesWatcher(t *testing.T) {
	// 閉じたら watcher を Close する (fd を残さない)。ブロックしていたイベント待ちの Cmd は
	// チャネルが閉じることで終わり、closed の観測として戻る。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if v.watch.W == nil {
		testenv.SkipEnv(t, "この環境では fsnotify を作れない")
	}
	v.watch.EvArmed = false // 既に張られているチェーンをテストが引き取る
	cmd := v.eventCmd()
	if cmd == nil {
		t.Fatal("イベント待ちの Cmd が作れない")
	}
	v.close() // watcher を閉じる = ブロック中の Cmd が解ける

	msg, ok := waitMsg(t, cmd, 5*time.Second).(IssuesWatchMsg)
	if !ok || !msg.closed {
		t.Fatalf("閉じたのに closed の観測が返らない: %+v", msg)
	}
	if next := v.HandleWatch(msg); next != nil {
		t.Fatal("閉じた後もチェーンが続いている")
	}
}

func TestIssuesWatchFallsBackToPollingWhenWatcherDies(t *testing.T) {
	// watcher が死んでも (fd 回収・NFS 等) 無音にはしない: ポーリングだけで続ける。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if v.watch.W == nil {
		testenv.SkipEnv(t, "この環境では fsnotify を作れない")
	}
	if got := v.pollInterval(); got != issuesWatchIdlePoll {
		t.Fatalf("イベントがある間の保険は低頻度でよい: %v", got)
	}
	v.HandleWatch(IssuesWatchMsg{closed: true})
	if v.watch.W != nil {
		t.Fatal("死んだ watcher を掴んだままにしている")
	}
	if !v.watch.PollArmed {
		t.Fatal("イベントが死んだのにポーリングも止まっている (無音になる)")
	}
	if got := v.pollInterval(); got != issuesWatchBlindPoll {
		t.Fatalf("イベントが来ない状態の周期になっていない: %v", got)
	}
}

// waitMsg は Cmd を goroutine で実行して結果を待つ (ブロックする Cmd をテストから駆動する)。
func waitMsg(t *testing.T, cmd tea.Cmd, wait time.Duration) tea.Msg {
	t.Helper()
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	msg := testwait.Recv(t, "Cmd が返らない (イベントを待ったまま固まっている)", ch, testwait.Within(wait))
	return msg
}

func TestIssuesWatchIgnoresStaleGeneration(t *testing.T) {
	// 🚨 回帰防止: 閉じてすぐ開き直すと、閉じる前に張ったチェーンの closed が後から届く。世代で
	// 弾かないと、それが開き直して作った新しい watcher を閉じてしまい、以降イベントが来なくなる
	// (無音ではないがポーリングだけへ静かに縮退する)。
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if v.watch.W == nil {
		testenv.SkipEnv(t, "この環境では fsnotify を作れない")
	}
	stale := IssuesWatchMsg{closed: true, gen: v.watch.Gen} // 閉じる前のチェーンが持っている世代

	v.close()
	v.shown = true
	v.Receive(scanOf(t, root)) // 開き直し (新しい watcher)
	if v.watch.W == nil {
		t.Fatal("開き直しで watcher が作られていない")
	}
	if v.HandleWatch(stale) != nil {
		t.Fatal("古い世代の観測でチェーンを張り直した")
	}
	if v.watch.W == nil {
		t.Fatal("古い世代の closed が新しい watcher を閉じた")
	}
}

// 回帰 (2026-08-21 実測): 消えて戻ったディレクトリを再び watch する。「Add 済み」を
// watched マップで覚えて skip すると、fsnotify の watch がディレクトリ削除で失われた後も
// 印だけが残り、**同一 viewer セッション中は二度と Add されない** (実 repo の git switch で
// issues/done が消えて戻ると、以後 done/ 内の変更が恒久的に無音。手動の取り直し 3 回でも復帰せず)。
//
// 🚨 この実 fsnotify 版は**再発の検出力が弱い**: ディレクトリを消しても fsnotify が watch を
// 落とすまでに間があり、WatchList() が消えた done/ をまだ載せている状態で assert が通りうる
// (タイミング依存。実測 2026-08-21)。再 Add の不変条件の正本は issues_watch_seam_test.go
// の TestIssuesWatchReAddsRecreatedDirWithoutFsnotify で、こちらは「実 fsnotify と結合しても
// 落ちない」ことの確認として残している。
func TestIssuesWatchReAddsRecreatedDir(t *testing.T) {
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	done := filepath.Join(root, "issues", "done")
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	// 🚨 空ディレクトリは watch 対象にならない (FindDirs は markdown を含むものだけ拾う)
	issuesviewtest.WriteIssue(t, filepath.Join(done, "000-feat-old.md"), "# 000 feat: old\n", time.Now().Add(-time.Hour))
	v := openedWatchView(t, root, path)
	if !slices.Contains(v.watchDirs(), done) {
		t.Fatalf("前提: done/ が watch 対象に入っていない: %v", v.watchDirs())
	}

	// ディレクトリが消えて戻る (git switch 相当)。watch は失われる
	if err := os.RemoveAll(done); err != nil {
		t.Fatal(err)
	}
	v.startWatch() // 消えている間の取り直し (Add は失敗する)
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	v.startWatch() // 戻った後の取り直し: ここで再び Add されなければならない

	// 🚨 watcher が無い環境と、配線が壊れた場合を区別する。fsnotify.NewWatcher が通らない環境では watch.w が nil になり、
	// 指紋ポーリングの経路だけで通ってしまう (この検査を素で書くと nil 参照で panic する)。
	// 「環境が対応していない」は SkipEnv (CI では FAIL) にし、「作れるのに張っていない」は配線の退行なので FAIL にする
	// (検査できなかったことを緑にしない)。
	if v.watch.W == nil {
		probe, err := fsnotify.NewWatcher()
		if err != nil {
			testenv.SkipEnv(t, "この環境では fsnotify watcher を作れない (%v)。実 fsnotify との結合は未検証 (不変条件は issues_watch_seam_test.go が固定)", err)
		}
		_ = probe.Close()
		t.Fatal("watcher は作れるのに startWatch が張っていない (配線の退行)")
	}
	// 再 Add されたかを watcher の登録一覧で直接見る (イベント配送のタイミングに依存させない)
	if !slices.Contains(v.watch.W.WatchList(), done) {
		t.Fatalf("消えて戻った done/ が再 Add されていない: %v", v.watch.W.WatchList())
	}
}

// global の next/ は目印 (symlink) だけが置かれ、issue の Path を持つものが無いので
// filepath.Dir(iss.Path) では watch に入らない。他セッション・git pull による付け外しを
// 反映するために明示的に見張る。
func TestIssuesWatchDirsIncludeGlobalNextDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001-feat-a.md"), []byte("# 001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, issues.NextDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	all, _ := issues.Scan([]string{dir})
	got := issuesWatchDirs([]string{dir}, all)
	if !slices.Contains(got, filepath.Join(dir, issues.NextDirName)) {
		t.Fatalf("global next/ が watch 対象に無い: %v", got)
	}
}

// 空の global 状態ディレクトリ (waiting/ pending/ done/ と別綴りの hold/ …) も見張る。issue の
// パスからしか足さないと、空の waiting/ へ新規作成した最初の md が watch にも指紋にも入らず、
// `r` を押すまで一覧に出なかった。予約外の名前 (notes/) は従来どおり見張らない
// (spec の「予約された名前だけが空のうちから見張られる」)。
func TestIssuesWatchDirsIncludeEmptyGlobalStatusDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001-feat-a.md"), []byte("# 001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Done は大文字: 走査 (scanDir) は大文字小文字を問わずに状態と読むので、見張りも同じに扱う
	for _, sub := range []string{"waiting", "pending", "Done", "hold", "notes"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := issues.Scan([]string{dir})
	got := issuesWatchDirs([]string{dir}, all)
	for _, sub := range []string{"waiting", "pending", "Done", "hold"} {
		if !slices.Contains(got, filepath.Join(dir, sub)) {
			t.Errorf("空の %s/ が watch 対象に無い: %v", sub, got)
		}
	}
	if slices.Contains(got, filepath.Join(dir, "notes")) {
		t.Errorf("予約外の notes/ を空のうちから見張っている: %v", got)
	}
	fp := issuesFingerprint(got, issuesWatchPaths(all))
	if err := os.WriteFile(filepath.Join(dir, "waiting", "002-feat-b.md"), []byte("# 002\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if issuesFingerprint(got, issuesWatchPaths(all)) == fp {
		t.Fatal("空の waiting/ への最初の md 作成で指紋が変わらない")
	}
}

// 走査の途中 (scanIssues の中) に入った編集も取りこぼさない。基準 (指紋) が読んだ内容より
// 新しい側に寄ると、以降の観測が「変化なし」になって一覧が古いまま残る。
//   - 本文の編集は「読み終えた直後」に起こす (loadAllMeta の差し込み口。読む位置を指紋の後ろから前へ
//     戻す退行では、差し込み口も一緒に指紋の前へ動くので、編集が基準に焼き込まれて赤になる)
//   - 追加・移動は「Scan の最中」(Scan が列挙を終えた後・戻る前) に起こす。基準のディレクトリの指紋を
//     Scan の後で取る退行では、追加が基準に焼き込まれて赤になる
func TestIssuesWatchCatchesEditDuringScan(t *testing.T) {
	cases := []struct {
		name       string
		duringScan bool // true = Scan の最中 / false = 読み終えた直後
		edit       func(t *testing.T, path string)
	}{
		{"読み終えた直後に本文を編集", false, func(t *testing.T, path string) {
			issuesviewtest.WriteIssue(t, path, "# 001 feat: 編集後\n", time.Now())
		}},
		{"走査の最中に issue を追加", true, func(t *testing.T, path string) {
			issuesviewtest.WriteIssue(t, filepath.Join(filepath.Dir(path), "002-feat-y.md"), "# 002 feat: 新規\n", time.Now())
		}},
		{"走査の最中に状態ディレクトリを作って移す", true, func(t *testing.T, path string) {
			done := filepath.Join(filepath.Dir(path), "done")
			if err := os.Mkdir(done, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path, filepath.Join(done, filepath.Base(path))); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, path := issuesviewtest.WatchTree(t, "# 001 feat: 編集前\n")
			v := newTestIssuesView()
			if cmd := v.Toggle(root); cmd == nil {
				t.Fatal("toggle が Cmd を返さない")
			}
			fired := false
			oldStage, oldScan := scanIssuesStage, issuesScanDirs
			t.Cleanup(func() { scanIssuesStage, issuesScanDirs = oldStage, oldScan })
			if c.duringScan {
				issuesScanDirs = func(dirs []string) ([]*issues.Issue, []string) {
					found, warnings := oldScan(dirs)
					if !fired {
						fired = true
						c.edit(t, path)
					}
					return found, warnings
				}
			} else {
				scanIssuesStage = func(stage string) {
					if stage == "read" && !fired {
						fired = true
						c.edit(t, path)
					}
				}
			}
			v.Receive(scanOf(t, root))
			scanIssuesStage, issuesScanDirs = oldStage, oldScan
			if !fired {
				t.Fatal("差し込み口が呼ばれない")
			}
			for range 3 {
				v.HandleWatch(observeIssues(&v))
			}
			if !v.scanning {
				t.Fatal("走査の途中に入った変化を取りこぼした (一覧が古いまま残る)")
			}
		})
	}
}
