package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	view := m.View()
	if !strings.Contains(view, "❯") && !strings.Contains(view, "commit "+m.commits[4].SHA) {
		t.Errorf("カーソル行がビューポートに入っていない:\n%s", view)
	}
}

func TestBrowseExpandUsesFetchedDetails(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateFailure
	m.details[sha] = []CheckDetail{{Name: "lint", State: StateFailure}}
	if cmd := m.toggleExpand(); cmd != nil {
		t.Errorf("詳細取得済みなのに fetch Cmd が返った")
	}
	if !m.expanded[sha] {
		t.Errorf("展開されていない")
	}
	if !strings.Contains(m.View(), "✗ lint") {
		t.Errorf("展開行が View に出ていない:\n%s", m.View())
	}
	// もう一度で折りたたみ
	m.toggleExpand()
	if m.expanded[sha] {
		t.Errorf("折りたたまれていない")
	}
}

func TestBrowseExpandTriggersDetailFetch(t *testing.T) {
	// キャッシュヒットで詳細が無い SHA の展開はオンデマンド取得になる
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess // キャッシュ由来 (details なし)
	cmd := m.toggleExpand()
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
		t.Errorf("取得した詳細が View に出ていない:\n%s", m.View())
	}
	if m.fetched[sha] != StateSuccess {
		t.Errorf("詳細取得の状態がキャッシュ保存対象 (fetched) に入っていない")
	}
}

// expandWithJobs は commit idx を job 2 件付きで展開済みにするテストヘルパー。
func expandWithJobs(m *browseModel, idx int) {
	sha := m.commits[idx].SHA
	m.expanded[sha] = true
	m.details[sha] = []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: StateFailure, URL: ""},
	}
}

func TestBrowseTreeNavigation(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	expandWithJobs(m, 0)
	// commit0 → job0 → job1 → commit1 と降りる
	m.handleKey("j")
	if m.cursor != 0 || m.cursorJob != 0 {
		t.Fatalf("j 1回目 = (%d,%d); want (0,0)", m.cursor, m.cursorJob)
	}
	m.handleKey("j")
	if m.cursor != 0 || m.cursorJob != 1 {
		t.Fatalf("j 2回目 = (%d,%d); want (0,1)", m.cursor, m.cursorJob)
	}
	m.handleKey("j")
	if m.cursor != 1 || m.cursorJob != -1 {
		t.Fatalf("j 3回目 = (%d,%d); want (1,-1)", m.cursor, m.cursorJob)
	}
	// 逆順で戻ると commit1 → job1 → job0 → commit0
	m.handleKey("k")
	if m.cursor != 0 || m.cursorJob != 1 {
		t.Fatalf("k 1回目 = (%d,%d); want (0,1)", m.cursor, m.cursorJob)
	}
	m.handleKey("k")
	m.handleKey("k")
	if m.cursor != 0 || m.cursorJob != -1 {
		t.Fatalf("k 3回目 = (%d,%d); want (0,-1)", m.cursor, m.cursorJob)
	}
}

func TestBrowseDescendAndCollapse(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateFailure
	m.details[sha] = []CheckDetail{{Name: "lint", State: StateFailure}}
	// l: 折りたたみ中 → 展開
	m.handleKey("l")
	if !m.expanded[sha] {
		t.Fatalf("l で展開されない")
	}
	// l: 展開済み → 最初の job へ降りる
	m.handleKey("l")
	if m.cursorJob != 0 {
		t.Fatalf("l 2回目で job に降りない: cursorJob=%d", m.cursorJob)
	}
	// h: job から親コミットへ戻ってツリーを閉じる
	m.handleKey("h")
	if m.cursorJob != -1 || m.expanded[sha] {
		t.Fatalf("h で閉じない: cursorJob=%d expanded=%v", m.cursorJob, m.expanded[sha])
	}
}

func TestBrowseOpenJobInBrowser(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	expandWithJobs(m, 0)
	var opened string
	orig := openInBrowser
	openInBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
	// job0 (URL あり) で Enter → ブラウザで開く
	m.handleKey("j")
	_, cmd := m.handleKey("enter")
	if cmd == nil {
		t.Fatalf("job 上の Enter で Cmd が返らない")
	}
	if msg := cmd(); msg.(openURLMsg).err != nil {
		t.Fatalf("openURLMsg.err = %v", msg.(openURLMsg).err)
	}
	if opened != "https://github.com/o/r/runs/1" {
		t.Errorf("開いた URL = %q", opened)
	}
	// job1 (URL なし) は notice を出して開かない
	m.handleKey("j")
	_, cmd = m.handleKey("enter")
	if cmd != nil {
		t.Errorf("URL なし job で Cmd が返った")
	}
	if !strings.Contains(m.hintLine(), "URL がありません") {
		t.Errorf("notice が hint に出ていない: %q", m.hintLine())
	}
}

func TestBrowseCursorJobClampedAfterCollapse(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	expandWithJobs(m, 0)
	m.handleKey("j") // job0 へ
	sha := m.commits[0].SHA
	delete(m.expanded, sha) // 外部要因で閉じた状態を再現
	m.ensureCursorVisible()
	if m.cursorJob != -1 {
		t.Errorf("閉じた後も cursorJob が残っている: %d", m.cursorJob)
	}
}

func TestBrowseExpandDuringBatchFetchWaits(t *testing.T) {
	// 一括取得中にその対象 SHA を展開しても、重複リクエストは打たず結果を待つ
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	if cmd := m.toggleExpand(); cmd != nil {
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
		t.Errorf("一括取得の詳細が展開表示に出ていない:\n%s", m.View())
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

func TestBrowseNonGitHubRepoExpand(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.hasRepo = false
	sha := m.commits[0].SHA
	m.statuses[sha] = StateNone
	if cmd := m.toggleExpand(); cmd != nil {
		t.Errorf("GitHub 以外の remote で fetch Cmd が返った")
	}
	if !strings.Contains(m.View(), "Check はありません") {
		t.Errorf("Check なし表示がない:\n%s", m.View())
	}
}

func TestBrowseUnknownStateExpandRetries(t *testing.T) {
	// unknown (前回取得失敗) の展開は再取得を試みる
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateUnknown
	if cmd := m.toggleExpand(); cmd == nil {
		t.Errorf("unknown 状態の展開で再取得が走らない")
	}
	if !m.detailsLoading[sha] {
		t.Errorf("detailsLoading が立っていない")
	}
}
