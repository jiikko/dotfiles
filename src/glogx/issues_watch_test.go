package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// watchTree は issues ディレクトリを 1 つ持つ木を作り (root, issue のパス) を返す。
func watchTree(t *testing.T, body string) (root, path string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "001-feat-x.md")
	writeIssue(t, path, body, time.Now().Add(-time.Hour))
	return root, path
}

// writeIssue は本文を書いて mtime を固定する (指紋の比較を時計に依存させない)。
func writeIssue(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// openedWatchView は「viewer を開いてスキャン結果を受け取り、見張りの基準も取れた」状態を作る。
func openedWatchView(t *testing.T, root, path string) *issuesView {
	t.Helper()
	v := newIssuesView()
	if cmd := v.toggle(root); cmd == nil {
		t.Fatal("toggle が Cmd を返さない")
	}
	v.receive(scanOf(t, root))
	// 最初の観測は基準取り (読み直さない)
	if cmd := v.handleWatch(v.observe()); cmd == nil {
		t.Fatal("基準取りの後に次の観測が予約されない")
	}
	if v.watch.seen == "" {
		t.Fatal("基準の指紋が取れていない")
	}
	return &v
}

// observe は watchCmd と同じ指紋を同期で計算する (tea.Tick を待たずにテストで駆動するため)。
// 対象集合は production と同じ watchTargets を通す (ここで再実装すると本番とずれる)。
func (v *issuesView) observe() issuesWatchMsg {
	return issuesWatchMsg{fp: issuesFingerprint(v.watchTargets())}
}

// scanOf は実ファイルを走査した結果を返す。⚠️ 中身を再実装せず production の scanIssues を
// 呼ぶ: 指紋の取り方がずれると「テストでは基準が揃うのに本番ではずれる」形で穴を見逃す。
func scanOf(t *testing.T, root string) issuesScanMsg {
	t.Helper()
	msg := scanIssues(root)
	if len(msg.dirs) == 0 {
		t.Fatalf("issues ディレクトリが見つからない: %s", root)
	}
	return msg
}

func TestIssuesWatchReloadsAfterExternalEdit(t *testing.T) {
	// ⚠️ 変化を見つけた最初の周期では読まない (書きかけを掴まないため)。安定を確かめた次の
	// 周期で reloadAfterEdit を返す。
	root, path := watchTree(t, "# 001 feat: 編集前\n")
	v := openedWatchView(t, root, path)
	if got := v.rows[0].Display(); got != "feat: 編集前" {
		t.Fatalf("前提が崩れた: 一覧のタイトルが %q", got)
	}

	writeIssue(t, path, "# 001 feat: 編集後\n\n- [x] やった\n", time.Now())
	changed := v.observe()
	if cmd := v.handleWatch(changed); cmd == nil {
		t.Fatal("変化を見つけた周期でも次の観測は予約する")
	}
	if v.watch.pending == "" {
		t.Fatal("変化が安定待ちに入っていない")
	}
	cmd := v.handleWatch(changed) // 同じ指紋 = 安定した
	if cmd == nil {
		t.Fatal("安定した変化で取り直しの Cmd が返らない")
	}
	v.receive(drainScanMsg(t, cmd))
	if got := v.rows[0].Display(); got != "feat: 編集後" {
		t.Fatalf("外部の編集が一覧に反映されていない: %q", got)
	}
	if got := v.rows[0].Progress(); got != "1/1" {
		t.Fatalf("チェックボックスの進捗が追従していない: %q", got)
	}
	// 取り直した直後の基準は「スキャンが読んだ時点の指紋」= 次の観測と一致する。自分の取り直しを
	// 外部の変化と誤検出しないことを、基準を空にする (= 次の観測を無条件に基準化する) のではなく
	// 一致で示す。空にすると、読んだ時刻と基準を取る時刻の差に入った編集を取りこぼす。
	if v.watch.pending != "" {
		t.Fatalf("取り直し後に安定待ちが残っている: %q", v.watch.pending)
	}
	if v.watch.seen != v.observe().fp {
		t.Fatal("取り直し後の基準がスキャン時点の指紋と一致しない")
	}
	v.handleWatch(v.observe())
	if v.scanning {
		t.Fatal("自分の取り直しを外部の変化と誤検出して再スキャンした")
	}
}

func TestIssuesWatchReloadsOpenBody(t *testing.T) {
	// 本文を開いたまま書き換えられたら本文も差し替わり、スクロール位置は保つ。
	root, path := watchTree(t, "# 001 feat: x\n\n本文の初版。\n")
	v := openedWatchView(t, root, path)
	v.handleKey("enter", 20)
	v.drawer.finish() // 引き出しの演出は着地させる (アニメ中は反映を保留する = 別テストで見る)
	v.lines(issuesRenderOpts{width: 80, page: 20})
	if v.open == nil {
		t.Fatal("本文モードに入れていない")
	}

	writeIssue(t, path, "# 001 feat: x\n\n本文の第 2 版。\n", time.Now())
	changed := v.observe()
	v.handleWatch(changed)
	cmd := v.handleWatch(changed)
	if cmd == nil {
		t.Fatal("本文の変化で取り直しの Cmd が返らない")
	}
	v.receive(drainScanMsg(t, cmd))
	out := strings.Join(v.lines(issuesRenderOpts{width: 80, page: 20}), "\n")
	if !strings.Contains(out, "第 2 版") {
		t.Fatalf("開いている本文が差し替わっていない:\n%s", out)
	}
}

func TestIssuesWatchIgnoresUnstableFile(t *testing.T) {
	// 書きかけ (周期ごとに指紋が変わる) の間は読まない。
	root, path := watchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	for i := range 3 {
		writeIssue(t, path, strings.Repeat("a", i+1), time.Now().Add(time.Duration(i)*time.Second))
		if cmd := v.handleWatch(v.observe()); cmd == nil {
			t.Fatal("次の観測が予約されない")
		}
		if v.watch.seen == "" {
			t.Fatal("基準が失われた (取り直しが走っている)")
		}
	}
}

func TestIssuesWatchDefersWhileURLPickerOpen(t *testing.T) {
	// ピッカーは本文の URL 集合を握っているので、下から差し替えると選択が別 URL に化ける。
	root, path := watchTree(t, "# 001 feat: x\n\nhttps://example.com/a\n")
	v := openedWatchView(t, root, path)
	v.urlPick.open([]string{"https://example.com/a"})

	writeIssue(t, path, "# 001 feat: x\n\nhttps://example.com/b\n", time.Now())
	changed := v.observe()
	v.handleWatch(changed)
	if cmd := v.handleWatch(changed); cmd == nil {
		t.Fatal("次の観測が予約されない")
	}
	if v.watch.pending == "" {
		t.Fatal("ピッカーを開いている間は反映を保留する (pending が消えた = 反映した)")
	}
	// 閉じたら次の周期で反映する
	v.urlPick = urlPicker{}
	cmd := v.handleWatch(changed)
	if cmd == nil {
		t.Fatal("ピッカーを閉じた後も反映されない")
	}
	if v.watch.pending != "" {
		t.Fatalf("反映後も保留が残っている: %q", v.watch.pending)
	}
}

func TestIssuesWatchDefersWhileDrawerAnimating(t *testing.T) {
	// 引き出しの開閉アニメ中は着地まで待つ (レイアウトが動いている最中に本文を差し替えない)。
	root, path := watchTree(t, "# 001 feat: x\n\n初版。\n")
	v := openedWatchView(t, root, path)
	v.handleKey("enter", 20) // 引き出しが開くアニメが始まる

	writeIssue(t, path, "# 001 feat: x\n\n第 2 版。\n", time.Now())
	changed := v.observe()
	v.handleWatch(changed)
	if cmd := v.handleWatch(changed); cmd == nil {
		t.Fatal("次の観測が予約されない")
	}
	if v.watch.pending == "" {
		t.Fatal("アニメ中は反映を保留する (pending が消えた = 反映した)")
	}
	v.drawer.finish() // 着地したら次の周期で反映する
	if cmd := v.handleWatch(changed); cmd == nil {
		t.Fatal("着地後も反映されない")
	}
	if v.watch.pending != "" {
		t.Fatalf("反映後も保留が残っている: %q", v.watch.pending)
	}
}

func TestIssuesWatchStopsWhenClosed(t *testing.T) {
	root, path := watchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	v.close()
	if cmd := v.handleWatch(v.observe()); cmd != nil {
		t.Fatal("閉じた後も見張りのチェーンが続いている (tick が残る)")
	}
	if v.watch.w != nil || v.watch.seen != "" || v.watch.evArmed || v.watch.pollArmed {
		t.Fatalf("閉じたのに見張りの状態が残っている (watcher の fd も残る): %+v", v.watch)
	}
	// 閉じている間は張り直しもしない
	if cmd := v.watchCmd(); cmd != nil {
		t.Fatal("閉じている viewer で watchCmd が Cmd を返した")
	}
}

func TestIssuesWatchIsSingleFlight(t *testing.T) {
	// i の連打・復元と重なっても見張りのチェーンを 2 本にしない (maybeTick と同じ規律)。
	root, path := watchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if cmd := v.watchCmd(); cmd != nil {
		t.Fatal("既に張っているのに 2 本目のチェーンを作った")
	}
	v.handleWatch(v.observe()) // 1 拍消費すれば張り直せる
	if !v.watch.pollArmed {
		t.Fatal("消費後に張り直していない")
	}
}

// ⚠️ 回帰防止 (リーク監査 2026-08-01): ポーリング由来の観測でイベント待ちの札まで降ろすと、
// まだ w.Events でブロックしている goroutine が生きているのに 2 本目が張られ、観測 1 回につき
// 1 本ずつ積み上がる。平常時のポーリングは 30s 周期なので、viewer を開きっぱなしにするだけで
// 増え続ける (回収は viewer を閉じたときだけ)。
//
// 札の状態では検出できない: handleWatch は最後に watchCmd で張り直すので、両方降ろす実装でも
// 事後の札は同じ true になる。実際に goroutine を走らせ、watcher を閉じたときに返ってくる
// closed の数 = 生きているイベント待ちの本数を数える。
func TestIssuesWatchPollDoesNotStackEventChains(t *testing.T) {
	root, path := watchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if v.watch.w == nil {
		t.Skip("この環境では fsnotify を作れない (ポーリングへ縮退する経路)")
	}
	msgs := make(chan tea.Msg, 64)
	v.watch.evArmed = false // 既に張られている札をテストが引き取り、実体の goroutine を作る
	runWatchChains(t, v.eventCmd(), msgs)

	for range 3 {
		runWatchChains(t, v.handleWatch(v.observe()), msgs) // ポーリング由来 (fromEvent=false)
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
func liveEventChains(v *issuesView, msgs <-chan tea.Msg) int {
	v.stopWatch()
	n := 0
	for {
		select {
		case msg := <-msgs:
			if m, ok := msg.(issuesWatchMsg); ok && m.closed {
				n++
			}
		case <-time.After(300 * time.Millisecond):
			return n
		}
	}
}

// 結合: viewer を開くと見張りのチェーンが張られ、閉じると止まる。
func TestIssuesViewerWatchWiredIntoUpdate(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	root, _ := watchTree(t, "# 001 feat: x\n")
	m.handleKey("i")
	m.issuesOv.cwd = root
	m.Update(scanOf(t, root))
	m.issuesOv.finishAnim()
	if !m.issuesOv.watch.pollArmed {
		t.Fatal("viewer を開いても見張りが張られていない")
	}
	_, cmd := m.Update(m.issuesOv.observe())
	if cmd == nil {
		t.Fatal("Update が見張りのチェーンを継続しない")
	}
	m.handleKey("i") // 閉じる
	if _, cmd := m.Update(m.issuesOv.observe()); cmd != nil {
		t.Fatal("閉じた後も見張りが続いている")
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
	root, path := watchTree(t, "# 001 feat: 編集前\n")
	v := newIssuesView()
	if cmd := v.toggle(root); cmd == nil {
		t.Fatal("toggle が Cmd を返さない")
	}
	v.receive(scanOf(t, root)) // スキャンはここまでの内容を読んだ

	writeIssue(t, path, "# 001 feat: 編集後\n", time.Now()) // 基準取りより前に外部が書く

	// 以降どれだけ観測しても指紋は「編集後」で一定なので、基準取りで吸収されると二度と気づけない。
	for range 3 {
		v.handleWatch(v.observe())
	}
	if !v.scanning {
		t.Fatal("スキャンと基準取りの間に入った編集を取りこぼした (一覧が編集前のまま固まる)")
	}
}

func TestIssuesWatchReloadsFromRealEvent(t *testing.T) {
	// イベント経路の結合: 実際に fsnotify のイベントで起こされ、指紋で判定して取り直しへ進む。
	// ⚠️ 指紋が正本 (イベントは Create/Rename/Write と嘘をつくので、起こす役だけ)。
	root, path := watchTree(t, "# 001 feat: 編集前\n")
	v := openedWatchView(t, root, path)
	if v.watch.w == nil {
		t.Skip("この環境では fsnotify を作れない (ポーリングへ縮退する経路)")
	}
	v.watch.evArmed = false // 既に張られているチェーンをテストが引き取る
	cmd := v.eventCmd()
	if cmd == nil {
		t.Fatal("イベント待ちの Cmd が作れない")
	}
	writeIssue(t, path, "# 001 feat: 編集後\n", time.Now())

	msg, ok := waitMsg(t, cmd, 5*time.Second).(issuesWatchMsg)
	if !ok || msg.closed {
		t.Fatalf("イベントで観測が返らない: %+v", msg)
	}
	if msg.fp == v.watch.seen {
		t.Fatal("イベント経路の指紋が変化を捉えていない")
	}
	v.handleWatch(msg)                                  // 1 回目: 安定待ち
	reload := v.handleWatch(issuesWatchMsg{fp: msg.fp}) // 2 回目: 安定 → 取り直し
	if reload == nil {
		t.Fatal("イベント由来の変化で取り直しが走らない")
	}
	v.receive(drainScanMsg(t, reload))
	if got := v.rows[0].Display(); got != "feat: 編集後" {
		t.Fatalf("イベント経由の編集が反映されていない: %q", got)
	}
}

func TestIssuesWatchClosingReleasesWatcher(t *testing.T) {
	// 閉じたら watcher を Close する (fd を残さない)。ブロックしていたイベント待ちの Cmd は
	// チャネルが閉じることで終わり、closed の観測として戻る。
	root, path := watchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if v.watch.w == nil {
		t.Skip("この環境では fsnotify を作れない")
	}
	v.watch.evArmed = false // 既に張られているチェーンをテストが引き取る
	cmd := v.eventCmd()
	if cmd == nil {
		t.Fatal("イベント待ちの Cmd が作れない")
	}
	v.close() // watcher を閉じる = ブロック中の Cmd が解ける

	msg, ok := waitMsg(t, cmd, 5*time.Second).(issuesWatchMsg)
	if !ok || !msg.closed {
		t.Fatalf("閉じたのに closed の観測が返らない: %+v", msg)
	}
	if next := v.handleWatch(msg); next != nil {
		t.Fatal("閉じた後もチェーンが続いている")
	}
}

func TestIssuesWatchFallsBackToPollingWhenWatcherDies(t *testing.T) {
	// watcher が死んでも (fd 回収・NFS 等) 無音にはしない: ポーリングだけで続ける。
	root, path := watchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if v.watch.w == nil {
		t.Skip("この環境では fsnotify を作れない")
	}
	if got := v.pollInterval(); got != issuesWatchIdlePoll {
		t.Fatalf("イベントがある間の保険は低頻度でよい: %v", got)
	}
	v.handleWatch(issuesWatchMsg{closed: true})
	if v.watch.w != nil {
		t.Fatal("死んだ watcher を掴んだままにしている")
	}
	if !v.watch.pollArmed {
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
	select {
	case msg := <-ch:
		return msg
	case <-time.After(wait):
		t.Fatal("Cmd が返らない (イベントを待ったまま固まっている)")
		return nil
	}
}

func TestIssuesWatchIgnoresStaleGeneration(t *testing.T) {
	// ⚠️ 回帰防止: 閉じてすぐ開き直すと、閉じる前に張ったチェーンの closed が後から届く。世代で
	// 弾かないと、それが開き直して作った新しい watcher を閉じてしまい、以降イベントが来なくなる
	// (無音ではないがポーリングだけへ静かに縮退する)。
	root, path := watchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	if v.watch.w == nil {
		t.Skip("この環境では fsnotify を作れない")
	}
	stale := issuesWatchMsg{closed: true, gen: v.watch.gen} // 閉じる前のチェーンが持っている世代

	v.close()
	v.shown = true
	v.receive(scanOf(t, root)) // 開き直し (新しい watcher)
	if v.watch.w == nil {
		t.Fatal("開き直しで watcher が作られていない")
	}
	if v.handleWatch(stale) != nil {
		t.Fatal("古い世代の観測でチェーンを張り直した")
	}
	if v.watch.w == nil {
		t.Fatal("古い世代の closed が新しい watcher を閉じた")
	}
}
