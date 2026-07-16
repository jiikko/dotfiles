package main

import (
	"context"
	"maps"
	"os/exec"
	"runtime"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Bubble Tea は「非同期レンダリング可能な CLI ランタイム」として使う (issue の設計) が、
// less 風の対話ブラウズ (カーソル移動 + CI job 展開) をユーザー要望で追加した
// (元 issue では非目標だった対話 UI を 2026-07-16 に明示指示で解禁)。
// Alt Screen へは切り替えず、インラインのビューポート描画で行う。終了時は View を空に
// して TUI 領域を消し、呼び出し元が最終結果を静的出力してターミナル履歴に残す。
// goroutine (fetch Cmd) は stdout へ直接書かず、結果を必ず tea.Msg として返す。

const (
	fetchTimeout    = 10 * time.Second
	spinnerInterval = 80 * time.Millisecond
)

type ciResultMsg struct {
	fetched map[string]CIState
	details map[string][]CheckDetail
	ghErr   *GHError
}

// detailMsg は展開時のオンデマンド取得 (キャッシュヒットで詳細が無い SHA) の結果。
type detailMsg struct {
	sha     string
	fetched map[string]CIState
	details map[string][]CheckDetail
	ghErr   *GHError
}

type tickMsg struct{}

// openURLMsg は job 詳細ページをブラウザで開いた結果。
type openURLMsg struct{ err error }

// openInBrowser はテストで実ブラウザを開かないための差し替え点。
var openInBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}

type browseModel struct {
	commits        []Commit
	statuses       map[string]CIState // 表示用 (キャッシュ + 取得結果のマージ)
	fetched        map[string]CIState // API から取得した分 (終了後のキャッシュ保存用)
	details        map[string][]CheckDetail
	detailsLoading map[string]bool
	expanded       map[string]bool
	toFetch        []string
	repo           Repo
	hasRepo        bool
	ghErr          *GHError
	oneline        bool
	colored        bool
	frame          int
	width          int
	height         int
	cursor         int    // コミット index
	cursorJob      int    // 展開中 job の index (-1 = コミット行にカーソルがある)
	offset         int    // ビューポート先頭の行 index
	notice         string // hint 行に出す一時メッセージ (次のキーで消える)
	fetching       bool
	done           bool
	fetch          tea.Cmd
	cancel         context.CancelFunc
}

func newBrowseModel(commits []Commit, statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, opts *Options, colored bool, width, height int) *browseModel {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	m := &browseModel{
		commits:        commits,
		statuses:       statuses,
		fetched:        map[string]CIState{},
		details:        map[string][]CheckDetail{},
		detailsLoading: map[string]bool{},
		expanded:       map[string]bool{},
		toFetch:        toFetch,
		repo:           repo,
		hasRepo:        hasRepo,
		cursorJob:      -1,
		oneline:        opts.Oneline,
		colored:        colored,
		width:          width,
		height:         height,
		fetching:       len(toFetch) > 0,
		cancel:         cancel,
	}
	if m.fetching {
		m.fetch = func() tea.Msg {
			defer cancel()
			fetched, details, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, toFetch)
			return ciResultMsg{fetched: fetched, details: details, ghErr: ghErr}
		}
	}
	return m
}

func (m *browseModel) Init() tea.Cmd {
	if m.fetching {
		return tea.Batch(m.fetch, tick())
	}
	return nil
}

func tick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		// 終了確定後に届く残メッセージは無視する (q での取得中断が
		// 「context canceled」警告として出るのを防ぐ)
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureCursorVisible()
		return m, nil
	case tickMsg:
		if !m.spinnerActive() {
			return m, nil
		}
		m.frame++
		return m, tick()
	case ciResultMsg:
		m.ghErr = msg.ghErr
		if msg.fetched != nil {
			maps.Copy(m.fetched, msg.fetched)
			maps.Copy(m.statuses, msg.fetched)
		}
		if msg.details != nil {
			maps.Copy(m.details, msg.details)
		}
		m.fillUnknown()
		m.fetching = false
		// 一括取得待ちで展開されていた SHA の loading を解除する (結果が来なかった
		// SHA も含めて解除。details 不在は「(CI job 情報なし)」表示に落ちる)
		for _, sha := range m.toFetch {
			delete(m.detailsLoading, sha)
		}
		m.ensureCursorVisible()
		return m, nil
	case detailMsg:
		delete(m.detailsLoading, msg.sha)
		if msg.ghErr != nil {
			m.ghErr = msg.ghErr
		}
		if msg.fetched != nil {
			maps.Copy(m.fetched, msg.fetched)
			maps.Copy(m.statuses, msg.fetched)
		}
		if msg.details != nil {
			maps.Copy(m.details, msg.details)
		}
		m.ensureCursorVisible()
		return m, nil
	case openURLMsg:
		if msg.err != nil {
			m.notice = "ブラウザを開けませんでした: " + firstLine(msg.err.Error())
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m *browseModel) handleKey(key string) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch key {
	case "q", "ctrl+c", "esc":
		m.cancel()
		if m.fetching {
			m.fillUnknown()
		}
		m.done = true
		return m, tea.Quit
	case "j", "down", "ctrl+n":
		m.moveDown()
	case "k", "up", "ctrl+p":
		m.moveUp()
	case "g", "home":
		m.cursor = 0
		m.cursorJob = -1
		m.offset = 0
	case "G", "end":
		m.cursor = clampIdx(len(m.commits)-1, len(m.commits))
		m.cursorJob = -1
		m.ensureCursorVisible()
	case "ctrl+d", "pgdown":
		m.offset = m.clampOffset(m.offset + m.pageSize()/2)
	case "ctrl+u", "pgup":
		m.offset = m.clampOffset(m.offset - m.pageSize()/2)
	case "enter", " ":
		if m.cursorJob >= 0 {
			return m, m.openJob()
		}
		return m, m.toggleExpand()
	case "l", "right", "tab":
		return m, m.descendOrExpand()
	case "h", "left":
		m.collapse()
	}
	return m, nil
}

// moveDown / moveUp はコミット行と展開中の job 行を 1 本のツリーとして辿る。
func (m *browseModel) moveDown() {
	if m.cursorJob+1 < m.visibleJobs(m.cursor) {
		m.cursorJob++
	} else if m.cursor+1 < len(m.commits) {
		m.cursor++
		m.cursorJob = -1
	}
	m.ensureCursorVisible()
}

func (m *browseModel) moveUp() {
	if m.cursorJob >= 0 {
		m.cursorJob--
	} else if m.cursor > 0 {
		m.cursor--
		m.cursorJob = m.visibleJobs(m.cursor) - 1 // 展開なしなら -1 = コミット行
	}
	m.ensureCursorVisible()
}

// visibleJobs は指定コミットの下に表示されている選択可能な job 行数。
func (m *browseModel) visibleJobs(idx int) int {
	if idx < 0 || idx >= len(m.commits) {
		return 0
	}
	sha := m.commits[idx].SHA
	if !m.expanded[sha] || m.detailsLoading[sha] {
		return 0
	}
	return len(m.details[sha])
}

// descendOrExpand (l/→): 折りたたみ中なら展開し、展開済みなら最初の job へ降りる。
func (m *browseModel) descendOrExpand() tea.Cmd {
	if len(m.commits) == 0 || m.cursorJob >= 0 {
		return nil
	}
	if !m.expanded[m.commits[m.cursor].SHA] {
		return m.toggleExpand()
	}
	if m.visibleJobs(m.cursor) > 0 {
		m.cursorJob = 0
		m.ensureCursorVisible()
	}
	return nil
}

// collapse (h/←): job 行からは親コミットへ戻ってツリーを閉じる。コミット行では閉じるだけ。
func (m *browseModel) collapse() {
	if len(m.commits) == 0 {
		return
	}
	sha := m.commits[m.cursor].SHA
	m.cursorJob = -1
	delete(m.expanded, sha)
	m.ensureCursorVisible()
}

// openJob はカーソル位置の job の詳細ページをブラウザで開く。
func (m *browseModel) openJob() tea.Cmd {
	details := m.details[m.commits[m.cursor].SHA]
	if m.cursorJob < 0 || m.cursorJob >= len(details) {
		return nil
	}
	url := details[m.cursorJob].URL
	if url == "" {
		m.notice = "この job には詳細ページの URL がありません"
		return nil
	}
	return func() tea.Msg {
		return openURLMsg{err: openInBrowser(url)}
	}
}

// toggleExpand はカーソル位置のコミットの CI job 一覧を開閉する。詳細が未取得
// (キャッシュヒットで一括取得に含まれなかった SHA) なら、その SHA だけ追加取得する。
func (m *browseModel) toggleExpand() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	if m.expanded[sha] {
		delete(m.expanded, sha)
		m.ensureCursorVisible()
		return nil
	}
	m.expanded[sha] = true
	defer m.ensureCursorVisible()
	if _, ok := m.details[sha]; ok || m.detailsLoading[sha] {
		return nil
	}
	if m.statuses[sha] == StateNone && !m.hasRepo {
		// remote が GitHub でない場合は取得先が無い
		m.details[sha] = []CheckDetail{}
		return nil
	}
	// 進行中の一括取得に含まれる SHA は、その結果 (details 込み) を待つ。
	// ここで別リクエストを打つと同一 SHA への GraphQL が並行し、完了順で
	// statuses/details が上書きされる (codex レビュー指摘)
	if m.fetching && slices.Contains(m.toFetch, sha) {
		m.detailsLoading[sha] = true
		return nil
	}
	m.detailsLoading[sha] = true
	repo := m.repo
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		fetched, details, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, []string{sha})
		return detailMsg{sha: sha, fetched: fetched, details: details, ghErr: ghErr}
	}
	return tea.Batch(cmd, tick())
}

// fillUnknown は結果が得られなかった SHA を「取得中」のまま残さず unknown へ落とす。
func (m *browseModel) fillUnknown() {
	for _, sha := range m.toFetch {
		if _, ok := m.statuses[sha]; !ok {
			m.statuses[sha] = StateUnknown
		}
	}
}

func (m *browseModel) spinnerActive() bool {
	return m.fetching || len(m.detailsLoading) > 0
}

func (m *browseModel) renderOpts() RenderOpts {
	return RenderOpts{
		Oneline:        m.oneline,
		Colored:        m.colored,
		Spinner:        spinnerFrames[m.frame%len(spinnerFrames)],
		Expanded:       m.expanded,
		Details:        m.details,
		DetailsLoading: m.detailsLoading,
	}
}

func (m *browseModel) lines() []Line {
	return RenderLines(m.commits, m.statuses, m.renderOpts())
}

// pageSize はビューポートの行数 (最下段のヒント行を除く)。
func (m *browseModel) pageSize() int {
	return max(m.height-1, 1)
}

func (m *browseModel) clampOffset(offset int) int {
	maxOffset := max(len(m.lines())-m.pageSize(), 0)
	return min(max(offset, 0), maxOffset)
}

// cursorOn はこの行にカーソルが乗っているか。
func (m *browseModel) cursorOn(l Line) bool {
	if l.CommitIdx != m.cursor {
		return false
	}
	if m.cursorJob < 0 {
		return l.Header
	}
	return l.JobNum == m.cursorJob+1
}

// ensureCursorVisible はカーソル行がビューポート内に入るよう offset を調整する。
// 展開の開閉で job 数が変わったときの cursorJob の範囲外もここで矯正する。
func (m *browseModel) ensureCursorVisible() {
	if m.cursorJob >= m.visibleJobs(m.cursor) {
		m.cursorJob = -1
	}
	lines := m.lines()
	target := 0
	for i, l := range lines {
		if m.cursorOn(l) {
			target = i
			break
		}
	}
	page := m.pageSize()
	if target < m.offset {
		m.offset = target
	}
	if target >= m.offset+page {
		m.offset = target - page + 1
	}
	m.offset = min(max(m.offset, 0), max(len(lines)-page, 0))
}

func (m *browseModel) View() string {
	if m.done {
		// TUI 領域を消し、呼び出し元の静的出力 (ターミナル履歴に残る方) に置き換える
		return ""
	}
	lines := m.lines()
	page := m.pageSize()
	offset := min(m.offset, max(len(lines)-page, 0))
	end := min(offset+page, len(lines))
	var b []byte
	for i := offset; i < end; i++ {
		text := lines[i].Text
		if m.cursorOn(lines[i]) {
			text = cursorMark(m.colored) + text
		} else {
			text = "  " + text
		}
		b = append(b, clipToWidth(text, m.width)...)
		b = append(b, '\n')
	}
	b = append(b, m.hintLine()...)
	return string(b)
}

func cursorMark(colored bool) string {
	return paint("❯ ", ansiBold, colored)
}

func (m *browseModel) hintLine() string {
	hint := "j/k: 移動  Enter: CI job  h/l: 閉じる/開く  q: 終了"
	if m.cursorJob >= 0 {
		hint = "Enter: ブラウザで開く  h: 戻る  j/k: 移動  q: 終了"
	}
	if m.fetching {
		hint = spinnerFrames[m.frame%len(spinnerFrames)] + " CI 状態を取得中...  " + hint
	}
	if m.notice != "" {
		hint = "⚠ " + m.notice + "  " + hint
	}
	if m.ghErr != nil {
		hint = "⚠ " + firstLine(m.ghErr.Warning()) + "  " + hint
	}
	return clipToWidth(paint(hint, ansiDim, m.colored), m.width)
}

func clampIdx(i, total int) int {
	if total <= 0 {
		return 0
	}
	return min(max(i, 0), total-1)
}

// RunBrowse はインライン TUI を実行し、最終状態のモデルを返す。
func RunBrowse(m *browseModel) (*browseModel, error) {
	p := tea.NewProgram(m) // WithAltScreen は使わない (インライン描画)
	final, err := p.Run()
	if err != nil {
		return m, err
	}
	if fm, ok := final.(*browseModel); ok {
		return fm, nil
	}
	return m, nil
}
