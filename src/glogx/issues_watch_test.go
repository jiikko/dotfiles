package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"glogx/issues"
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

// scanOf は実ファイルを走査した結果 (scanCmd と同じ中身) を返す。
func scanOf(t *testing.T, root string) issuesScanMsg {
	t.Helper()
	dirs := issues.FindDirs(root)
	if len(dirs) == 0 {
		t.Fatalf("issues ディレクトリが見つからない: %s", root)
	}
	found, warnings := issues.Scan(dirs)
	for _, iss := range found {
		_ = iss.LoadMeta()
	}
	return issuesScanMsg{root: root, dirs: dirs, issues: found, warnings: warnings}
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
	// 取り直した直後は基準を引き直す (自分の scan を外部の変化と誤検出しない)
	if v.watch.seen != "" || v.watch.pending != "" {
		t.Fatalf("取り直し後に見張りの基準が残っている: seen=%q pending=%q", v.watch.seen, v.watch.pending)
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
	if v.watch != (issuesWatch{}) {
		t.Fatalf("閉じたのに見張りの状態が残っている: %+v", v.watch)
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
	if !v.watch.armed {
		t.Fatal("消費後に張り直していない")
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
	if !m.issuesOv.watch.armed {
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
