package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestBrowseCopyURL(t *testing.T) {
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(text string) error {
		copied = text
		return nil
	}
	t.Cleanup(func() { copyToClipboard = orig })
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	// コミット一覧では commit URL
	m.handleKey("y")
	wantCommit := "https://github.com/o/r/commit/" + m.commits[0].SHA
	if copied != wantCommit {
		t.Errorf("commit URL = %q; want %q", copied, wantCommit)
	}
	// job フォーカス中はその job の URL
	m.openPanel()
	m.handleKey("j")
	m.handleKey("y")
	if copied != "https://github.com/o/r/runs/1" {
		t.Errorf("job URL = %q", copied)
	}
	if !strings.Contains(m.hintLine(), "コピーしました") {
		t.Errorf("notice が出ていない: %q", m.hintLine())
	}
	// URL なし job (job1) は notice
	m.handleKey("j")
	m.handleKey("y")
	if !strings.Contains(m.hintLine(), "コピーできる URL がありません") {
		t.Errorf("URL なしの notice が出ていない: %q", m.hintLine())
	}
}

func TestBrowseBatchPRsFeedPCache(t *testing.T) {
	// 一括取得の PR は p キーのキャッシュとコミット行バッジの両方に合流する
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	m.Update(ciResultMsg{batch: CIBatch{
		Statuses: map[string]CIState{shas[0]: StateSuccess},
		Details:  map[string][]CheckDetail{},
		PRs:      map[string]*PRRef{shas[0]: {Number: 7, URL: "https://github.com/o/r/pull/7", State: "MERGED"}},
	}})
	var opened string
	orig := openInBrowser
	openInBrowser = func(u string) error {
		opened = u
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
	// 再取得なしで即 open
	_, cmd := m.handleKey("p")
	if cmd == nil || m.prBusy[shas[0]] {
		t.Fatalf("バッチ由来の PR キャッシュで即 open にならない")
	}
	cmd()
	if opened != "https://github.com/o/r/pull/7" {
		t.Errorf("URL = %q", opened)
	}
	// バッジも View に出る
	if !strings.Contains(m.View(), "#7") {
		t.Errorf("PR バッジが View に出ていない:\n%s", m.View())
	}
}

func TestBrowseOpenPR(t *testing.T) {
	var opened string
	orig := openInBrowser
	openInBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	// p → 取得 Cmd (実行はしない) と busy
	_, cmd := m.handleKey("p")
	if cmd == nil || !m.prBusy[sha] {
		t.Fatalf("p で PR 取得が始まらない (cmd=%v busy=%v)", cmd, m.prBusy[sha])
	}
	// 取得結果 (PR あり) → ブラウザで開く Cmd が返る
	_, cmd = m.Update(prMsg{sha: sha, pr: &PRRef{Number: 12, URL: "https://github.com/o/r/pull/12", State: "OPEN"}})
	if cmd == nil {
		t.Fatalf("prMsg (PR あり) で open Cmd が返らない")
	}
	cmd()
	if opened != "https://github.com/o/r/pull/12" {
		t.Errorf("開いた URL = %q", opened)
	}
	// キャッシュ済みなので 2 回目の p は再取得せず即 open
	_, cmd = m.handleKey("p")
	if cmd == nil || m.prBusy[sha] {
		t.Errorf("キャッシュ済み PR で即 open にならない")
	}
}

// detail/basis/jobDetail の一過性エラーは、後続の成功結果で hint 警告がクリアされる
// (set-only だとセッション中ずっと張り付いていた・レビュー C4)。
func TestBrowseGhErrClearedOnSuccess(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	// パネル対象の detail 取得が一過性エラー → 警告が出る
	m.panelSHA = sha
	m.Update(detailMsg{sha: sha, batch: emptyBatch(), ghErr: &GHError{Kind: GHOther, Detail: "boom"}})
	if m.ghErr == nil || !strings.Contains(m.hintLine(), "取得に失敗") {
		t.Fatalf("detail エラーで警告が出ない: ghErr=%v hint=%q", m.ghErr, m.hintLine())
	}
	// 成功 detail が届くと警告はクリアされる (set-only なら残ってしまう)
	m.Update(detailMsg{sha: sha, batch: emptyBatch(), ghErr: nil})
	if m.ghErr != nil {
		t.Fatalf("成功 detail 後も ghErr が残っている: %v", m.ghErr)
	}
	// basis / jobDetail も同様に成功でクリアする
	m.ghErr = &GHError{Kind: GHOther, Detail: "boom2"}
	m.Update(basisMsg{batch: emptyBatch(), ghErr: nil})
	if m.ghErr != nil {
		t.Errorf("成功 basis 後も ghErr が残っている: %v", m.ghErr)
	}
	m.ghErr = &GHError{Kind: GHOther, Detail: "boom3"}
	m.Update(jobDetailMsg{key: "k", lines: []string{"x"}, ghErr: nil})
	if m.ghErr != nil {
		t.Errorf("成功 jobDetail 後も ghErr が残っている: %v", m.ghErr)
	}
}

func TestBrowseOpenPRErrorNotCached(t *testing.T) {
	// 一時エラーは「PR なし」としてキャッシュしない (次の p で再試行できる)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	m.Update(prMsg{sha: sha, pr: nil, ghErr: &GHError{Kind: GHOther, Detail: "network down"}})
	if _, ok := m.prCache[sha]; ok {
		t.Fatalf("エラー結果がキャッシュされている")
	}
	if !strings.Contains(m.hintLine(), "PR の取得に失敗") {
		t.Errorf("エラー notice が出ない: %q", m.hintLine())
	}
	// 再度 p → 再取得が走る
	_, cmd := m.handleKey("p")
	if cmd == nil || !m.prBusy[sha] {
		t.Errorf("エラー後の p で再取得しない")
	}
}

func TestBrowseOpenPRNotFound(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	m.Update(prMsg{sha: sha, pr: nil})
	if !strings.Contains(m.hintLine(), "PR はありません") {
		t.Errorf("PR なしの notice が出ない: %q", m.hintLine())
	}
	// nil もキャッシュされ、再度 p を押しても API へ行かない
	_, cmd := m.handleKey("p")
	if cmd != nil || m.prBusy[sha] {
		t.Errorf("PR なしキャッシュ後に再取得した")
	}
}

func TestBrowseOpenPRUnpushed(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	if _, cmd := m.handleKey("p"); cmd != nil {
		t.Errorf("未 push コミットで PR 取得が走った")
	}
	if !strings.Contains(m.hintLine(), "未 push") {
		t.Errorf("未 push の notice が出ない: %q", m.hintLine())
	}
}

func TestBrowseDiffOpenScrollClose(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	diffLines := make([]string, 20)
	for i := range diffLines {
		diffLines[i] = fmt.Sprintf("line-%d", i)
	}
	calls := stubDiff(t, diffLines, nil)

	_, cmd := m.handleKey("d")
	if m.diffOv.sha != m.commits[0].SHA {
		t.Fatalf("diffSHA = %q; want カーソル位置のコミット", m.diffOv.sha)
	}
	if !m.diffOv.busy[m.diffOv.sha] {
		t.Error("取得中フラグが立っていない")
	}
	deliverDiffMsg(t, m, cmd)
	if len(*calls) != 1 || (*calls)[0] != m.commits[0].SHA {
		t.Fatalf("loadCommitDiff の呼び出し = %v", *calls)
	}
	if m.diffOv.busy[m.diffOv.sha] {
		t.Error("取得完了後も busy のまま")
	}
	view := m.View()
	if !strings.Contains(view, "line-0") || !strings.Contains(view, "diff:") {
		t.Fatalf("diff ポップアップが描画されていない:\n%s", view)
	}
	// スクロール: j で 1 行、G で末尾
	m.handleKey("j")
	if m.diffOv.offset != 1 {
		t.Errorf("j 後の offset = %d; want 1", m.diffOv.offset)
	}
	m.handleKey("G")
	if m.diffOv.offset != len(diffLines)-m.visibleDiffRows() {
		t.Errorf("G 後の offset = %d", m.diffOv.offset)
	}
	// q で閉じる (アプリは終了しない)
	m.handleKey("q")
	if m.diffOv.sha != "" || m.done {
		t.Errorf("q: diffSHA=%q done=%v; want 閉じるのみ", m.diffOv.sha, m.done)
	}
}

func TestBrowseDiffToggleAndCache(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	calls := stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	m.handleKey("d") // toggle 閉
	if m.diffOv.sha != "" {
		t.Fatal("d の再押下で閉じていない")
	}
	_, cmd2 := m.handleKey("d") // 再度開く → キャッシュヒットで再取得しない
	if cmd2 != nil {
		t.Error("キャッシュヒット時にも取得コマンドが返った")
	}
	if len(*calls) != 1 {
		t.Errorf("loadCommitDiff 呼び出し回数 = %d; want 1 (キャッシュ)", len(*calls))
	}
	if m.diffOv.sha == "" {
		t.Error("キャッシュヒット時に開いていない")
	}
}

func TestBrowseDiffFromPanelUsesPanelSHA(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	withJobs(m, 0)
	m.handleKey("enter") // panel を開く
	if m.panelSHA == "" {
		t.Fatal("panel が開いていない")
	}
	stubDiff(t, []string{"x"}, nil)
	m.handleKey("d")
	if m.diffOv.sha != m.commits[0].SHA {
		t.Errorf("diffSHA = %q; want panel のコミット", m.diffOv.sha)
	}
	if m.panelSHA != "" {
		t.Error("diff を開いたら panel は閉じる契約")
	}
}

func TestBrowseDiffErrorShowsNoticeAndCloses(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, nil, errors.New("boom"))
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	if m.diffOv.sha != "" {
		t.Error("取得失敗時にポップアップが開いたまま")
	}
	if !strings.Contains(m.notice, "diff の取得に失敗") {
		t.Errorf("notice = %q", m.notice)
	}
	if strings.Contains(m.hintLine(), "diff の取得に失敗") == false {
		t.Errorf("hint に notice が出ていない: %q", m.hintLine())
	}
}

// 回帰: diff ポップアップは pager 流儀 — Space/Enter はスクロールであり閉じない。
// 末尾に達したら最終行を表示したまま止まる (実機で Space 送り中に突然閉じた報告への修正)。
func TestBrowseDiffPagerKeysScrollNotClose(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	diffLines := make([]string, 30)
	for i := range diffLines {
		diffLines[i] = fmt.Sprintf("line-%d", i)
	}
	stubDiff(t, diffLines, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	maxOffset := len(diffLines) - m.visibleDiffRows()

	m.handleKey(" ")
	if m.diffOv.sha == "" {
		t.Fatal("Space で閉じた (半ページスクロールのはず)")
	}
	if m.diffOv.offset == 0 {
		t.Error("Space でスクロールしていない")
	}
	m.handleKey("enter")
	if m.diffOv.sha == "" {
		t.Fatal("Enter で閉じた (1 行スクロールのはず)")
	}
	// 末尾を大きく超えて送っても最終行位置で止まり、開いたまま
	for range 100 {
		m.handleKey(" ")
	}
	if m.diffOv.sha == "" {
		t.Fatal("末尾到達後のスクロールで閉じた")
	}
	if m.diffOv.offset != maxOffset {
		t.Errorf("末尾で offset = %d; want %d (最終行を表示し続ける)", m.diffOv.offset, maxOffset)
	}
	view := m.View()
	if !strings.Contains(view, diffLines[len(diffLines)-1]) {
		t.Error("末尾で最終行が描画されていない")
	}
	// 閉じるのは q
	m.handleKey("q")
	if m.diffOv.sha != "" || m.done {
		t.Errorf("q: diffSHA=%q done=%v", m.diffOv.sha, m.done)
	}
}

// diff が空 (変更なし) のときは "(diff はありません)" を出す (busy でもエラーでもない経路)。
func TestBrowseDiffEmptyShowsMessage(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, []string{}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	if m.diffOv.sha == "" {
		t.Fatal("空 diff でポップアップが閉じてしまった (エラーではないので開いたままが仕様)")
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "diff はありません") {
		t.Fatalf("空 diff の案内が出ていない:\n%s", v)
	}
}

// diff ポップアップ表示中の y はカーソル位置コミットの URL をコピーする。
func TestBrowseDiffCopyURLWhileOpen(t *testing.T) {
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { copyToClipboard = orig })
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	m.handleKey("y")
	want := "https://github.com/o/r/commit/" + m.commits[0].SHA
	if copied != want {
		t.Errorf("diff 表示中の y でコピーされた URL = %q; want %q", copied, want)
	}
	if m.diffOv.sha == "" {
		t.Error("y でポップアップが閉じた (コピーのみが仕様)")
	}
}

// 上スクロールは 0 で止まり、g で先頭・b (ctrl+u) で半ページ戻る。
func TestBrowseDiffScrollUpClampAndReset(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	diffLines := make([]string, 40)
	for i := range diffLines {
		diffLines[i] = fmt.Sprintf("line-%d", i)
	}
	stubDiff(t, diffLines, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)

	m.handleKey("G") // 末尾へ
	end := m.diffOv.offset
	if end == 0 {
		t.Fatal("G で末尾へ動いていない")
	}
	m.handleKey("b") // 半ページ戻る
	if m.diffOv.offset >= end || m.diffOv.offset < 0 {
		t.Errorf("b (半ページ上) 後の offset = %d; want 0<=x<%d", m.diffOv.offset, end)
	}
	m.handleKey("g") // 先頭
	if m.diffOv.offset != 0 {
		t.Errorf("g 後の offset = %d; want 0", m.diffOv.offset)
	}
	m.handleKey("k") // 先頭で k は 0 に張り付く
	if m.diffOv.offset != 0 {
		t.Errorf("先頭での k 後の offset = %d; want 0 (クランプ)", m.diffOv.offset)
	}
}

// esc でも diff ポップアップは閉じる (q/h/left/d と同じ閉じる系キー)。
func TestBrowseDiffEscCloses(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	m.handleKey("esc")
	if m.diffOv.sha != "" || m.done {
		t.Errorf("esc: diffSHA=%q done=%v; want 閉じるのみ", m.diffOv.sha, m.done)
	}
}

// 別コミットの古い diff 取得エラーが遅れて届いても、現在開いている diff は閉じない
// (diffMsg の sha 一致ガードの回帰。閉じるのは msg.sha == 現在表示中のときだけ)。
func TestBrowseDiffStaleErrorDoesNotCloseCurrent(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d") // commit[0] の diff を開く
	deliverDiffMsg(t, m, cmd)
	current := m.diffOv.sha
	if current == "" {
		t.Fatal("diff が開いていない")
	}
	// commit[1] 宛の古いエラーが遅れて到着 (直接 Update へ流す)
	m.Update(diffMsg{sha: m.commits[1].SHA, err: errors.New("stale boom")})
	if m.diffOv.sha != current {
		t.Errorf("別 SHA のエラーで現在の diff が閉じた: diffSHA=%q; want %q", m.diffOv.sha, current)
	}
}

func TestBrowseListOpenCommitURL(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	var opened []string
	orig := openInBrowser
	openInBrowser = func(url string) error {
		opened = append(opened, url)
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })

	m.handleKey("j") // 2 番目のコミットへ
	_, cmd := m.handleKey("o")
	if cmd == nil {
		t.Fatal("o で open コマンドが返らない")
	}
	cmd()
	want := "https://github.com/o/r/commit/" + m.commits[1].SHA
	if len(opened) != 1 || opened[0] != want {
		t.Errorf("開いた URL = %v; want %s", opened, want)
	}
}

func TestBrowseListOpenCommitURLNoRepo(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.hasRepo = false
	_, cmd := m.handleKey("o")
	if cmd != nil {
		t.Error("repo なしで open コマンドが返った")
	}
	if m.notice == "" {
		t.Error("repo なしの notice が出ていない")
	}
}

func TestBrowseCopyJobContextCached(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withFailedJob(m, 0, 7, StateFailure)
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { copyToClipboard = orig })
	m.openPanel()
	m.handleKey("j")
	// 詳細キャッシュ済み → 即コピー (追加 fetch なし)
	m.detailOv.cache[m.detailKey()] = []string{"✗ lint (13s)", "", "[failure] a.go:1", "  boom"}
	if _, cmd := m.handleKey("Y"); cmd != nil {
		t.Fatal("キャッシュ済みなのに fetch が走った")
	}
	for _, want := range []string{"## CI job: lint", "o/r@aaaaaaa", "https://github.com/o/r/runs/9", "[failure] a.go:1"} {
		if !strings.Contains(copied, want) {
			t.Fatalf("コピー内容に %q が無い:\n%s", want, copied)
		}
	}
	if strings.Contains(copied, "\x1b[") {
		t.Fatal("コピー内容に ANSI が残っている")
	}
	if !strings.Contains(m.notice, "コピーしました") {
		t.Fatalf("完了 notice が出ない: %q", m.notice)
	}
}

func TestBrowseCopyJobContextFetchesThenCopies(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withFailedJob(m, 0, 7, StateFailure)
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { copyToClipboard = orig })
	m.openPanel()
	m.handleKey("j")
	// 未取得 → 詳細ポップアップを開いて取得し、到着時にコピーされる
	_, cmd := m.handleKey("Y")
	if cmd == nil || !m.detailOv.visible() || m.copyOnDetail != m.detailKey() {
		t.Fatalf("Y で詳細取得+コピー予約に入らない: cmd=%v open=%v pending=%q",
			cmd != nil, m.detailOv.visible(), m.copyOnDetail)
	}
	m.Update(jobDetailMsg{key: m.detailKey(), lines: []string{"✗ step", "log line"}})
	if !strings.Contains(copied, "log line") || !strings.Contains(copied, "## CI job: lint") {
		t.Fatalf("到着時にコピーされない:\n%s", copied)
	}
	if m.copyOnDetail != "" {
		t.Fatal("コピー予約が消えない")
	}
	// 取得失敗 (ghErr) では予約破棄のみでコピーされない
	copied = ""
	m.detailOv.reset()
	m.openPanel()
	m.handleKey("j")
	m.handleKey("Y")
	m.Update(jobDetailMsg{key: m.detailKey(), ghErr: &GHError{Kind: GHOther, Detail: "boom"}})
	if copied != "" || m.copyOnDetail != "" {
		t.Fatalf("取得失敗でコピーされた / 予約が残った: copied=%q pending=%q", copied, m.copyOnDetail)
	}
	// closePanel で予約破棄 (閉じた後の到着でコピーしない)
	m.detailOv.reset()
	m.openPanel()
	m.handleKey("j")
	m.handleKey("Y")
	key := m.detailKey()
	m.closePanel()
	m.Update(jobDetailMsg{key: key, lines: []string{"late"}})
	if copied != "" {
		t.Fatal("パネルを閉じた後の到着でコピーされた")
	}
}

func TestBrowsePRStatusFlow(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	sha := m.commits[0].SHA
	// P で開いて取得に入る
	_, cmd := m.handleKey("P")
	if cmd == nil || !m.prStatusOv.visible() || !m.prStatusOv.fetching() {
		t.Fatalf("P で PR 取得に入らない: cmd=%v visible=%v fetching=%v", cmd != nil, m.prStatusOv.visible(), m.prStatusOv.fetching())
	}
	// 取得結果が描画される (state / レビュー / conflict / CI)
	m.Update(prStatusMsg{sha: sha, status: &PRStatus{
		PRRef: PRRef{Number: 12, URL: "https://github.com/o/r/pull/12", State: "OPEN"},
		Title: "new feature", ReviewDecision: "APPROVED", Mergeable: "CONFLICTING",
		BaseRefName: "master", HeadRefName: "f/new",
	}})
	m.details[sha] = []CheckDetail{{Name: "lint", State: StateFailure}}
	v := stripANSI(m.View())
	for _, want := range []string{"PR #12: new feature", "OPEN", "f/new → master", "APPROVED", "CONFLICTING", "CI: ✗", "1 job 失敗"} {
		if !strings.Contains(v, want) {
			t.Fatalf("PR ポップアップに %q が無い:\n%s", want, v)
		}
	}
	// 表示中はモーダル: o でブラウザ、q で閉じる
	var opened string
	origOpen := openInBrowser
	openInBrowser = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { openInBrowser = origOpen })
	_, cmd = m.handleKey("o")
	if cmd == nil {
		t.Fatal("o でブラウザ Cmd が返らない")
	}
	cmd()
	if opened != "https://github.com/o/r/pull/12" {
		t.Fatalf("開いた URL が違う: %q", opened)
	}
	m.handleKey("q")
	if m.prStatusOv.visible() {
		t.Fatal("q で閉じない")
	}
	// キャッシュ済みなので開き直しは fetch なし、P の再押下で toggle 閉
	if _, cmd := m.handleKey("P"); cmd != nil {
		t.Fatal("キャッシュ済みなのに再取得した")
	}
	if !m.prStatusOv.visible() {
		t.Fatal("開き直せない")
	}
	m.handleKey("P")
	if m.prStatusOv.visible() {
		t.Fatal("P の再押下で toggle 閉しない")
	}
}

func TestBrowsePRStatusGuardsAndErrors(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateUnpushed)
	// 未 push は取得しない
	if _, cmd := m.handleKey("P"); cmd != nil || m.prStatusOv.visible() {
		t.Fatalf("未 push で PR 取得に入った: %q", m.notice)
	}
	if !strings.Contains(m.notice, "未 push") {
		t.Fatalf("未 push notice が出ない: %q", m.notice)
	}
	// 取得エラーはキャッシュせず閉じ、notice を出す (次の P で再試行できる)
	m.statuses = statusesFor(m, StateSuccess)
	m.handleKey("P")
	sha := m.commits[0].SHA
	m.Update(prStatusMsg{sha: sha, ghErr: &GHError{Kind: GHOther, Detail: "boom"}})
	if m.prStatusOv.visible() || !strings.Contains(m.notice, "PR の取得に失敗") {
		t.Fatalf("エラーで閉じない / notice が出ない: visible=%v notice=%q", m.prStatusOv.visible(), m.notice)
	}
	if _, ok := m.prStatusOv.cache[sha]; ok {
		t.Fatal("エラーがキャッシュされた (PR なし誤答が固定される)")
	}
	// PR なしは nil キャッシュ + その旨の表示
	m.handleKey("P")
	m.Update(prStatusMsg{sha: sha, status: nil})
	if !strings.Contains(stripANSI(m.View()), "紐づく PR はありません") {
		t.Fatal("PR なしの表示が出ない")
	}
}

// 回帰 (レビュー確定 high): Y でコピー予約後にフォーカスが別 job へ動いたら、旧 job の詳細到着で
// コピーしない (別 job のヘッダに旧 job 本文を貼る silent 誤コピーの防止)。
func TestBrowseCopyJobContextFocusMovedDropsCopy(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	m.details[m.commits[0].SHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, CheckID: 1},
		{Name: "lint", State: StateFailure, CheckID: 7},
	}
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { copyToClipboard = orig })
	m.openPanel()
	m.handleKey("j") // job0 (build) にフォーカス
	key0 := m.detailKey()
	m.handleKey("Y") // 未取得 → 予約 + 詳細ポップアップ取得
	if m.copyOnDetail != key0 {
		t.Fatalf("コピー予約されない: %q", m.copyOnDetail)
	}
	m.handleKey("esc") // 詳細ポップアップだけ閉じる (パネルは残る)
	if m.panelSHA == "" {
		t.Fatal("esc でパネルまで閉じた")
	}
	m.handleKey("j") // job1 (lint) へフォーカス移動
	m.Update(jobDetailMsg{key: key0, lines: []string{"build log line"}})
	if copied != "" {
		t.Fatalf("フォーカス移動後に旧 job をコピーした: %q", copied)
	}
	if m.copyOnDetail != "" {
		t.Fatal("到着で予約が破棄されていない")
	}
}

// 回帰 (レビュー確定 medium security): コピー時に job 名/URL の端末制御シーケンス (OSC52 等) を
// 除去する。stripANSI 単体は OSC を残すため sanitizeDetailLine と併用している。
func TestBrowseCopyJobContextSanitizesHeader(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	m.details[m.commits[0].SHA] = []CheckDetail{
		{Name: "li\x1b[31mnt", State: StateFailure, CheckID: 7, URL: "https://x/\x1b]0;pwn\x07ok"},
	}
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { copyToClipboard = orig })
	m.openPanel()
	m.handleKey("j")
	m.detailOv.cache[m.detailKey()] = []string{"log"}
	m.handleKey("Y")
	if strings.ContainsRune(copied, '\x1b') || strings.ContainsRune(copied, '\x07') {
		t.Fatalf("コピー内容に端末制御シーケンスが残った: %q", copied)
	}
	if !strings.Contains(copied, "lint") || !strings.Contains(copied, "https://x/ok") {
		t.Fatalf("無害化で本来の文字まで欠けた: %q", copied)
	}
}

// 回帰 (レビュー確定 medium): close→reopen 連打で in-flight の PR 取得を二重発火しない。
func TestPRStatusOverlayNoDoubleFetch(t *testing.T) {
	o := newPRStatusOverlay()
	if !o.open("A") {
		t.Fatal("初回 open が fetch を要求しない")
	}
	o.close() // 応答前に閉じる (busy[A] は保持されるべき)
	if o.open("A") {
		t.Fatal("in-flight 中の reopen が二重 fetch を要求した")
	}
	o.receive("A", &PRStatus{PRRef: PRRef{Number: 1}}, nil) // 応答 → busy 解除 + cache
	o.close()
	if o.open("A") {
		t.Fatal("cache 済みなのに再 fetch を要求した")
	}
}

// 回帰 (レビュー確定 low): 別 sha へ移った後に届く PR 取得エラーで、表示中 (別 sha) に無関係な
// 失敗 notice を被せない。
func TestBrowsePRStatusStaleErrorNoNotice(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	shaA := m.commits[0].SHA
	shaB := m.commits[1].SHA
	m.handleKey("P") // A を開く (fetch A)
	m.handleKey("P") // A を閉じる (toggle)
	m.cursor = 1
	m.handleKey("P") // B を開く (fetch B)
	m.Update(prStatusMsg{sha: shaB, status: &PRStatus{PRRef: PRRef{Number: 2, State: "OPEN"}, Title: "b"}})
	m.notice = ""
	m.Update(prStatusMsg{sha: shaA, ghErr: &GHError{Kind: GHOther, Detail: "boom"}}) // A の遅延エラー
	if m.notice != "" {
		t.Fatalf("別 sha の遅延エラーで notice が出た: %q", m.notice)
	}
	if !m.prStatusOv.visible() {
		t.Fatal("別 sha の遅延エラーで B の表示が閉じられた")
	}
}
