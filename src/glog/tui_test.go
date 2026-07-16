package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

func newTestBrowse(t *testing.T, n int, statuses map[string]CIState, toFetch []string) *browseModel {
	t.Helper()
	commits := make([]Commit, n)
	for i := range commits {
		sha := strings.Repeat(string(rune('a'+i)), 40)
		commits[i] = Commit{
			SHA: sha, ShortSHA: sha[:7], Subject: "subject", Author: "koji", AuthorEmail: "k@x",
			Date: "Thu Jul 16 19:12:47 2026 +0900", RelDate: "now", Message: "subject",
		}
	}
	m := newBrowseModel(commits, statuses, toFetch, Repo{Owner: "o", Name: "r"}, true,
		&Options{}, false, 80, 10)
	t.Cleanup(m.cancel)
	return m
}

func statusesFor(m *browseModel, state CIState) map[string]CIState {
	s := map[string]CIState{}
	for _, c := range m.commits {
		s[c.SHA] = state
	}
	return s
}

// withJobs は commit idx の details を job 2 件で埋めるテストヘルパー。
func withJobs(m *browseModel, idx int) {
	m.details[m.commits[idx].SHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: StateFailure, URL: ""},
	}
}

func TestBrowseCursorNavigation(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.handleKey("j")
	if m.cursor != 1 {
		t.Errorf("j 後の cursor = %d; want 1", m.cursor)
	}
	m.handleKey("k")
	m.handleKey("k") // 先頭で止まる
	if m.cursor != 0 {
		t.Errorf("k 連打後の cursor = %d; want 0", m.cursor)
	}
	// emacs 風の Ctrl-N / Ctrl-P でも移動できる
	m.handleKey("ctrl+n")
	if m.cursor != 1 {
		t.Errorf("ctrl+n 後の cursor = %d; want 1", m.cursor)
	}
	m.handleKey("ctrl+p")
	if m.cursor != 0 {
		t.Errorf("ctrl+p 後の cursor = %d; want 0", m.cursor)
	}
	m.handleKey("G")
	if m.cursor != 2 {
		t.Errorf("G 後の cursor = %d; want 2", m.cursor)
	}
	m.handleKey("g")
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("g 後の cursor/offset = %d/%d; want 0/0", m.cursor, m.offset)
	}
}

func TestBrowseCursorScrollsViewport(t *testing.T) {
	// medium 形式 1 コミット ≈ 6 行 × 5 件 > 高さ 10 なので、下へ移動すると offset が進む
	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	for range 4 {
		m.handleKey("j")
	}
	if m.offset == 0 {
		t.Errorf("末尾コミットへ移動しても offset が 0 のまま")
	}
}

func TestBrowsePanelOpenClose(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	if cmd := m.openPanel(); cmd != nil {
		t.Errorf("詳細取得済みなのに fetch Cmd が返った")
	}
	if m.panelSHA != m.commits[0].SHA {
		t.Fatalf("パネルが開いていない")
	}
	view := m.View()
	for _, want := range []string{"CI jobs:", "✓ build", "✗ lint"} {
		if !strings.Contains(view, want) {
			t.Errorf("パネルに %q が出ていない:\n%s", want, view)
		}
	}
	// h で閉じる
	m.handleKey("h")
	if m.panelSHA != "" {
		t.Errorf("h で閉じない")
	}
	// esc でも閉じる (アプリ終了にはならない)
	m.openPanel()
	m.handleKey("esc")
	if m.panelSHA != "" || m.done {
		t.Errorf("esc でパネルだけ閉じるべき: panelSHA=%q done=%v", m.panelSHA, m.done)
	}
}

func TestBrowsePanelEnterToggles(t *testing.T) {
	// Enter は popup の表示・非表示の toggle (ユーザー要望)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.handleKey("enter")
	if m.panelSHA == "" {
		t.Fatalf("Enter でパネルが開かない")
	}
	m.handleKey("enter")
	if m.panelSHA != "" || m.done {
		t.Errorf("Enter 2 回目でパネルが閉じない: panelSHA=%q done=%v", m.panelSHA, m.done)
	}
}

func TestBrowsePanelAnchoredAtCommit(t *testing.T) {
	// パネルは一律上部でなく、対象コミットのヘッダー行直下に出る (ユーザー要望)
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.height = 40 // 3 コミットが全部見える高さ
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 1)
	m.handleKey("j") // 2 番目のコミットへ
	m.openPanel()
	view := strings.Split(m.View(), "\n")
	headerIdx, panelIdx := -1, -1
	for i, line := range view {
		if strings.Contains(line, "commit "+m.commits[1].SHA) {
			headerIdx = i
		}
		if panelIdx == -1 && strings.Contains(line, "CI jobs:") {
			panelIdx = i
		}
	}
	if headerIdx == -1 || panelIdx == -1 {
		t.Fatalf("ヘッダー行 (%d) かパネル (%d) が見つからない:\n%s", headerIdx, panelIdx, m.View())
	}
	if panelIdx != headerIdx+1 {
		t.Errorf("パネル位置 = %d 行目; want ヘッダー直下 %d 行目:\n%s", panelIdx, headerIdx+1, m.View())
	}
}

func TestBrowsePanelClampedToViewport(t *testing.T) {
	// 対象コミットが画面下部でも、パネルはビューポート内へ収まる位置に出る
	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 4)
	m.handleKey("G") // 末尾コミットへ (ヘッダーはビューポート下端付近)
	m.openPanel()
	view := m.View()
	if !strings.Contains(view, "CI jobs:") || !strings.Contains(view, "✓ build") {
		t.Errorf("下端のコミットでパネルが見えていない:\n%s", view)
	}
	if got := strings.Count(view, "\n"); got+1 > m.pageSize()+1 {
		t.Errorf("パネルでビューポートが伸びた: %d 行", got+1)
	}
}

func TestBrowsePanelKeepsListHeight(t *testing.T) {
	// パネルはリストへ行を差し込まず上へ重ねるため、View の行数は開閉で変わらない
	// (高さのガタつき防止: ユーザー要望の回帰テスト)
	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	before := strings.Count(m.View(), "\n")
	m.openPanel()
	after := strings.Count(m.View(), "\n")
	if before != after {
		t.Errorf("パネル開閉で View の行数が変わった: %d → %d", before, after)
	}
}

func TestBrowsePanelJobCursorAndOpen(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	if m.panelCursor != -1 {
		t.Fatalf("開いた直後のフォーカスはタイトル行 (-1) のはず: %d", m.panelCursor)
	}
	var opened string
	orig := openInBrowser
	openInBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
	// j で job0 にフォーカスして o → ブラウザで開く (Enter は TUI 内の詳細に使う)
	m.handleKey("j")
	if m.panelCursor != 0 {
		t.Fatalf("j 後の panelCursor = %d; want 0", m.panelCursor)
	}
	_, cmd := m.handleKey("o")
	if cmd == nil {
		t.Fatalf("job フォーカス中の o で Cmd が返らない")
	}
	if msg := cmd(); msg.(openURLMsg).err != nil {
		t.Fatalf("openURLMsg.err = %v", msg.(openURLMsg).err)
	}
	if opened != "https://github.com/o/r/runs/1" {
		t.Errorf("開いた URL = %q", opened)
	}
	// j で job1 へ (末尾で止まる)。URL なし job は notice を出して開かない
	m.handleKey("j")
	m.handleKey("j")
	if m.panelCursor != 1 {
		t.Errorf("panelCursor = %d; want 1", m.panelCursor)
	}
	_, cmd = m.handleKey("o")
	if cmd != nil {
		t.Errorf("URL なし job で Cmd が返った")
	}
	if !strings.Contains(m.hintLine(), "URL がありません") {
		t.Errorf("notice が hint に出ていない: %q", m.hintLine())
	}
	// k でタイトル行まで戻れば Enter は「閉じる」に戻る
	m.handleKey("k")
	m.handleKey("k")
	if m.panelCursor != -1 {
		t.Fatalf("k で -1 に戻らない: %d", m.panelCursor)
	}
	m.handleKey("enter")
	if m.panelSHA != "" {
		t.Errorf("タイトル行フォーカスの Enter で閉じない")
	}
}

func TestBrowseEnterOpensDetailAndToggles(t *testing.T) {
	// job 行の Enter は TUI 内の詳細ポップアップの開閉 toggle (ブラウザは o)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	_, cmd := m.handleKey("enter")
	if cmd == nil || !m.detailOpen {
		t.Fatalf("job 行の Enter で詳細が開かない (cmd=%v detailOpen=%v)", cmd, m.detailOpen)
	}
	m.Update(jobDetailMsg{key: m.detailKey(), lines: []string{"line"}})
	m.handleKey("enter")
	if m.detailOpen {
		t.Errorf("詳細表示中の Enter で閉じない (toggle)")
	}
	if m.panelSHA == "" || m.panelCursor != 0 {
		t.Errorf("詳細を閉じた後 job フォーカスに戻らない: panelSHA=%q cursor=%d", m.panelSHA, m.panelCursor)
	}
}

func TestBrowsePanelTriggersDetailFetch(t *testing.T) {
	// キャッシュヒットで詳細が無い SHA のパネルはオンデマンド取得になる
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess // キャッシュ由来 (details なし)
	cmd := m.openPanel()
	if cmd == nil {
		t.Fatalf("詳細未取得なのに fetch Cmd が返らない")
	}
	if !m.detailsLoading[sha] {
		t.Errorf("detailsLoading が立っていない")
	}
	if !strings.Contains(m.View(), "取得中") {
		t.Errorf("取得中表示がない:\n%s", m.View())
	}
	// 取得完了メッセージで反映される
	m.Update(detailMsg{sha: sha,
		fetched: map[string]CIState{sha: StateSuccess},
		details: map[string][]CheckDetail{sha: {{Name: "build", State: StateSuccess}}}})
	if m.detailsLoading[sha] {
		t.Errorf("取得完了後も loading のまま")
	}
	if !strings.Contains(m.View(), "✓ build") {
		t.Errorf("取得した詳細がパネルに出ていない:\n%s", m.View())
	}
	if m.fetched[sha] != StateSuccess {
		t.Errorf("詳細取得の状態がキャッシュ保存対象 (fetched) に入っていない")
	}
}

func TestBrowsePanelDuringBatchFetchWaits(t *testing.T) {
	// 一括取得中にその対象 SHA のパネルを開いても、重複リクエストは打たず結果を待つ
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	if cmd := m.openPanel(); cmd != nil {
		t.Errorf("一括取得中の SHA に重複 fetch Cmd が返った")
	}
	if !m.detailsLoading[shas[0]] {
		t.Errorf("待機中の loading 表示が立っていない")
	}
	// 一括取得の完了で loading が解除され details が表示される
	m.Update(ciResultMsg{
		fetched: map[string]CIState{shas[0]: StateSuccess},
		details: map[string][]CheckDetail{shas[0]: {{Name: "build", State: StateSuccess}}},
	})
	if m.detailsLoading[shas[0]] {
		t.Errorf("一括取得完了後も loading のまま")
	}
	if !strings.Contains(m.View(), "✓ build") {
		t.Errorf("一括取得の詳細がパネルに出ていない:\n%s", m.View())
	}
}

func TestBrowseCIResultMergesAndStopsSpinner(t *testing.T) {
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	if !m.fetching {
		t.Fatalf("toFetch ありで fetching が立っていない")
	}
	m.Update(ciResultMsg{
		fetched: map[string]CIState{shas[0]: StateFailure},
		details: map[string][]CheckDetail{shas[0]: {{Name: "lint", State: StateFailure}}},
	})
	if m.fetching {
		t.Errorf("取得完了後も fetching のまま")
	}
	if m.statuses[shas[0]] != StateFailure || m.fetched[shas[0]] != StateFailure {
		t.Errorf("statuses/fetched に反映されていない: %+v", m.statuses)
	}
}

func TestBrowseQuitFillsUnknown(t *testing.T) {
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	_, cmd := m.handleKey("q")
	if cmd == nil {
		t.Fatalf("q で Quit が返らない")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("q の Cmd が tea.Quit でない: %v", msg)
	}
	if !m.done {
		t.Errorf("done が立っていない")
	}
	if m.statuses[shas[0]] != StateUnknown {
		t.Errorf("取得中断で unknown に落ちていない: %v", m.statuses[shas[0]])
	}
	if m.View() != "" {
		t.Errorf("done 後の View が空でない (TUI 領域が残る): %q", m.View())
	}
}

func TestBrowseBatchedRunesKeyMsg(t *testing.T) {
	// 高速連打で複数キーが 1 つの KeyMsg (Runes="hhq") にまとまっても 1 文字ずつ
	// 処理される (未対応だと q が無視されて終了できない: pty スモークで実測した回帰)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.openJobDetail()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hhq")})
	if !m.done {
		t.Fatalf("hhq のまとめ配送で終了しない (detailOpen=%v panelSHA=%q)", m.detailOpen, m.panelSHA)
	}
	if cmd == nil {
		t.Errorf("q の Quit Cmd が返らない")
	}
}

func TestBrowseQuitWorksWhilePanelOpen(t *testing.T) {
	// パネル表示中でも q はアプリ終了
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.openPanel()
	_, cmd := m.handleKey("q")
	if cmd == nil || !m.done {
		t.Errorf("パネル表示中の q で終了しない")
	}
}

func TestBrowseCIResultNegativeCachesUnknown(t *testing.T) {
	// API から結果が返らなかった SHA は unknown 表示 + 負キャッシュ対象 (fetched) に入る
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	m.Update(ciResultMsg{fetched: map[string]CIState{}})
	if m.statuses[shas[0]] != StateUnknown {
		t.Errorf("statuses = %v; want unknown", m.statuses[shas[0]])
	}
	if m.fetched[shas[0]] != StateUnknown {
		t.Errorf("unknown が負キャッシュ対象 (fetched) に入っていない")
	}
}

func TestBrowsePanelUnpushedNoFetch(t *testing.T) {
	// 未 push の SHA のパネルは GitHub へ問い合わせない
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateUnpushed
	if cmd := m.openPanel(); cmd != nil {
		t.Errorf("未 push SHA で fetch Cmd が返った")
	}
	if !strings.Contains(m.View(), "Check はありません") {
		t.Errorf("Check なし表示がない:\n%s", m.View())
	}
}

func TestBrowseOpenJobRejectsNonHTTP(t *testing.T) {
	// targetUrl は外部 CI が任意に設定できるため http(s) 以外は開かない
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	m.details[sha] = []CheckDetail{{Name: "evil", State: StateSuccess, URL: "file:///etc/passwd"}}
	m.openPanel()
	m.handleKey("j")
	called := false
	orig := openInBrowser
	openInBrowser = func(string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
	if _, cmd := m.handleKey("o"); cmd != nil {
		t.Errorf("file:// URL で Cmd が返った")
	}
	if called {
		t.Errorf("file:// URL がブラウザに渡された")
	}
	if !strings.Contains(m.hintLine(), "http(s) 以外") {
		t.Errorf("notice が出ていない: %q", m.hintLine())
	}
}

func TestBrowseJobDetailPopup(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j") // job0 へ
	// l で詳細ポップアップ (未取得なので fetch Cmd が返る)
	_, cmd := m.handleKey("l")
	if cmd == nil {
		t.Fatalf("l で詳細取得 Cmd が返らない")
	}
	if !m.detailOpen || !m.jobDetailBusy[m.detailKey()] {
		t.Fatalf("詳細が開いていない / busy でない")
	}
	if !strings.Contains(m.View(), "詳細を取得中") {
		t.Errorf("取得中表示がない:\n%s", m.View())
	}
	// 取得完了 → 末尾から表示
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("log line %d", i)
	}
	m.Update(jobDetailMsg{key: m.detailKey(), lines: lines})
	rows := m.visibleDetailRows()
	if m.detailOffset != 30-rows {
		t.Errorf("detailOffset = %d; want 末尾表示 %d", m.detailOffset, 30-rows)
	}
	if !strings.Contains(m.View(), "log line 29") {
		t.Errorf("末尾行が見えていない (低い端末でも末尾は見える):\n%s", m.View())
	}
	// k で上へスクロール、g で先頭
	m.handleKey("k")
	if m.detailOffset != 30-rows-1 {
		t.Errorf("k 後の offset = %d", m.detailOffset)
	}
	m.handleKey("g")
	if m.detailOffset != 0 {
		t.Errorf("g 後の offset = %d", m.detailOffset)
	}
	// h で job フォーカスへ戻る (パネルは開いたまま)
	m.handleKey("h")
	if m.detailOpen || m.panelSHA == "" || m.panelCursor != 0 {
		t.Errorf("h 後の状態: detailOpen=%v panelSHA=%q cursor=%d", m.detailOpen, m.panelSHA, m.panelCursor)
	}
	// 再度 l → キャッシュ済みなので fetch なしで即表示
	if _, cmd := m.handleKey("l"); cmd != nil {
		t.Errorf("キャッシュ済み詳細で再 fetch した")
	}
	if !m.detailOpen {
		t.Errorf("2 回目の l で開かない")
	}
}

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

func TestBrowseNonGitHubRepoPanel(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.hasRepo = false
	sha := m.commits[0].SHA
	m.statuses[sha] = StateNone
	if cmd := m.openPanel(); cmd != nil {
		t.Errorf("GitHub 以外の remote で fetch Cmd が返った")
	}
	if !strings.Contains(m.View(), "Check はありません") {
		t.Errorf("Check なし表示がない:\n%s", m.View())
	}
}

func TestBrowseLinesMemoized(t *testing.T) {
	// 行リストはカーソル移動やパネル開閉で再構築しない (メモ化)。-p の巨大 patch で
	// キー 1 打ごとに全行を組み直す計算量爆発を防ぐ
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	first := m.lines()
	m.handleKey("j")
	m.handleKey("ctrl+d")
	withJobs(m, 0)
	m.openPanel()
	if second := m.lines(); &first[0] != &second[0] {
		t.Errorf("カーソル移動・パネル開閉で行リストが再構築された")
	}
	// 状態を変える更新 (CI 結果のマージ) では再構築される
	sha := m.commits[0].SHA
	m.Update(ciResultMsg{fetched: map[string]CIState{sha: StateFailure}})
	rebuilt := m.lines()
	if &first[0] == &rebuilt[0] {
		t.Errorf("CI 結果反映後も古い行リストのまま")
	}
	if !strings.Contains(rebuilt[0].Text, "✗") {
		t.Errorf("再構築後の行に新しい状態が反映されていない: %q", rebuilt[0].Text)
	}
}

func TestBrowsePanelHomeKeyOnEmptyJobs(t *testing.T) {
	// job 0 件のパネルで g を押してもタイトル行 (-1) から動かず、Enter で閉じられる
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateNone
	m.details[sha] = []CheckDetail{}
	m.openPanel()
	m.handleKey("g")
	if m.panelCursor != -1 {
		t.Fatalf("空パネルで g がフォーカスを動かした: %d", m.panelCursor)
	}
	m.handleKey("enter")
	if m.panelSHA != "" {
		t.Errorf("空パネルが Enter で閉じない")
	}
}

func TestBuildPanelBoxWidths(t *testing.T) {
	lines := buildPanelBox(" title ", []string{"row", strings.Repeat("x", 200)}, 40, false)
	if len(lines) != 4 {
		t.Fatalf("枠 + 2 行のはずが %d 行", len(lines))
	}
	for _, l := range lines {
		if w := runewidth.StringWidth(stripANSI(l)); w != 40 {
			t.Errorf("パネル行の幅 = %d; want 40: %q", w, l)
		}
	}
}
