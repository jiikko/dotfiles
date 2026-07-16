package main

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// Bubble Tea は「非同期レンダリング可能な CLI ランタイム」として使う (issue の設計) が、
// less 風の対話ブラウズ (カーソル移動 + CI job 表示) をユーザー要望で追加した
// (元 issue では非目標だった対話 UI を 2026-07-16 に明示指示で解禁)。
// Alt Screen へは切り替えず、インラインのビューポート描画で行う。終了時は View を空に
// して TUI 領域を消し、呼び出し元が最終結果を静的出力してターミナル履歴に残す。
// goroutine (fetch Cmd) は stdout へ直接書かず、結果を必ず tea.Msg として返す。
//
// CI job 一覧はリストへ行を差し込まず、ビューポート上部へ重ねるパネル (ポップアップ)
// で表示する。展開方式だと開閉のたびに後続行がずれて高さがガタつくため (ユーザー要望)。

const (
	fetchTimeout    = 10 * time.Second
	spinnerInterval = 80 * time.Millisecond
	// maxPanelJobs は job パネルに一度に表示する行数。超過分はパネル内でスクロールする。
	maxPanelJobs = 10
)

type ciResultMsg struct {
	fetched map[string]CIState
	details map[string][]CheckDetail
	ghErr   *GHError
}

// detailMsg はパネル表示時のオンデマンド取得 (キャッシュヒットで詳細が無い SHA) の結果。
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
	toFetch        []string
	repo           Repo
	hasRepo        bool
	ghErr          *GHError
	decor          *DecorColors
	oneline        bool
	colored        bool
	frame          int
	width          int
	height         int
	cursor         int    // コミット index
	offset         int    // ビューポート先頭の行 index
	panelSHA       string // job パネルを表示中のコミット SHA ("" = パネルなし)
	panelCursor    int    // パネル内で選択中の job index (-1 = タイトル行にフォーカス)
	notice         string // hint 行に出す一時メッセージ (次のキーで消える)
	fetching       bool
	done           bool
	fetch          tea.Cmd
	cancel         context.CancelFunc

	// lines() のメモ化。行リストの再構築は O(出力全行数) で、-p の巨大 patch では
	// キー 1 打ごとに数万行を組み直すことになるためキャッシュする。行内容を変えうる
	// 更新 (statuses/details のマージ・スピナーフレーム・幅変更) だけが無効化する。
	// カーソル移動・パネル開閉は View の窓側で重ねるだけなので無効化不要
	linesCache []Line
	linesValid bool
}

func newBrowseModel(commits []Commit, statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, opts *Options, colored bool, width, height int) *browseModel {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	m := &browseModel{
		commits:        commits,
		statuses:       statuses,
		fetched:        map[string]CIState{},
		details:        map[string][]CheckDetail{},
		detailsLoading: map[string]bool{},
		toFetch:        toFetch,
		repo:           repo,
		hasRepo:        hasRepo,
		panelCursor:    -1,
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
		m.invalidateLines() // 幅で折り返し行数が変わる
		m.ensureCursorVisible()
		return m, nil
	case tickMsg:
		if !m.spinnerActive() {
			return m, nil
		}
		m.frame++
		m.invalidateLines() // 取得中コミットのスピナーフレームが進む
		return m, tick()
	case ciResultMsg:
		m.invalidateLines()
		m.ghErr = msg.ghErr
		if msg.fetched != nil {
			maps.Copy(m.fetched, msg.fetched)
			maps.Copy(m.statuses, msg.fetched)
		}
		if msg.details != nil {
			maps.Copy(m.details, msg.details)
		}
		// 結果が得られなかった SHA は unknown として表示し、30 秒の負キャッシュにも
		// 載せる (fetched へ入れる = 終了時に SaveCache される)。q での中断 (fillUnknown)
		// と違い、こちらは API の実際の返答に基づく確定
		for _, sha := range m.toFetch {
			if _, ok := m.statuses[sha]; !ok {
				m.statuses[sha] = StateUnknown
				m.fetched[sha] = StateUnknown
			}
		}
		m.fetching = false
		// 一括取得待ちでパネルを開いていた SHA の loading を解除する (結果が来なかった
		// SHA も含めて解除。details 不在は「(CI job 情報なし)」表示に落ちる)
		for _, sha := range m.toFetch {
			delete(m.detailsLoading, sha)
		}
		return m, nil
	case detailMsg:
		m.invalidateLines()
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
	if key == "q" || key == "ctrl+c" {
		m.cancel()
		if m.fetching {
			m.fillUnknown()
		}
		m.done = true
		return m, tea.Quit
	}
	if m.panelSHA != "" {
		return m.handlePanelKey(key)
	}
	switch key {
	case "esc":
		m.cancel()
		if m.fetching {
			m.fillUnknown()
		}
		m.done = true
		return m, tea.Quit
	case "j", "down", "ctrl+n":
		m.cursor = clampIdx(m.cursor+1, len(m.commits))
		m.ensureCursorVisible()
	case "k", "up", "ctrl+p":
		m.cursor = clampIdx(m.cursor-1, len(m.commits))
		m.ensureCursorVisible()
	case "g", "home":
		m.cursor = 0
		m.offset = 0
	case "G", "end":
		m.cursor = clampIdx(len(m.commits)-1, len(m.commits))
		m.ensureCursorVisible()
	case "ctrl+d", "pgdown":
		m.offset = m.clampOffset(m.offset + m.pageSize()/2)
	case "ctrl+u", "pgup":
		m.offset = m.clampOffset(m.offset - m.pageSize()/2)
	case "enter", " ", "l", "right", "tab":
		return m, m.openPanel()
	}
	return m, nil
}

// handlePanelKey は job パネル表示中のキー操作。j/k はパネル内のフォーカス移動になる。
// フォーカスの初期位置はタイトル行 (-1) で、この状態の Enter は「閉じる」= Enter 連打で
// 開閉 toggle が成立する。j で job へフォーカスを降ろした後の Enter はその job を
// ブラウザで開く (両方ユーザー要望)。
func (m *browseModel) handlePanelKey(key string) (tea.Model, tea.Cmd) {
	jobs := m.details[m.panelSHA]
	switch key {
	case "esc", "h", "left":
		m.closePanel()
	case "enter":
		if m.panelCursor < 0 {
			m.closePanel()
			return m, nil
		}
		return m, m.openJob()
	case "j", "down", "ctrl+n":
		if m.panelCursor+1 < len(jobs) {
			m.panelCursor++
		}
	case "k", "up", "ctrl+p":
		m.panelCursor = max(m.panelCursor-1, -1)
	case "g", "home":
		// job 0 件でフォーカスを 0 にすると、存在しない job にフォーカスが移って
		// タイトル行 (-1) へ戻れなくなる (Enter で閉じられない) ため空ではしない
		if len(jobs) > 0 {
			m.panelCursor = 0
		}
	case "G", "end":
		if len(jobs) > 0 {
			m.panelCursor = len(jobs) - 1
		}
	case " ", "o":
		return m, m.openJob()
	}
	return m, nil
}

// openPanel はカーソル位置のコミットの CI job パネルを開く。詳細が未取得
// (キャッシュヒットで一括取得に含まれなかった SHA) なら、その SHA だけ追加取得する。
func (m *browseModel) openPanel() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	m.panelSHA = sha
	m.panelCursor = -1 // タイトル行フォーカスから開始 (この状態の Enter = 閉じる)
	if _, ok := m.details[sha]; ok || m.detailsLoading[sha] {
		return nil
	}
	if !m.hasRepo || m.statuses[sha] == StateUnpushed {
		// remote が GitHub でない / 未 push の SHA は取得先が無い
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

func (m *browseModel) closePanel() {
	m.panelSHA = ""
	m.panelCursor = -1
}

// openJob はパネルで選択中の job の詳細ページをブラウザで開く。
func (m *browseModel) openJob() tea.Cmd {
	jobs := m.details[m.panelSHA]
	if m.panelCursor < 0 || m.panelCursor >= len(jobs) {
		return nil
	}
	url := jobs[m.panelCursor].URL
	if url == "" {
		m.notice = "この job には詳細ページの URL がありません"
		return nil
	}
	// StatusContext の targetUrl は外部 CI が任意に設定できる値。file:// 等で
	// ローカルのハンドラを起動させないよう http(s) だけを開く
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		m.notice = "http(s) 以外の URL は開きません"
		return nil
	}
	return func() tea.Msg {
		return openURLMsg{err: openInBrowser(url)}
	}
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

func (m *browseModel) spinner() string {
	return spinnerFrames[m.frame%len(spinnerFrames)]
}

// renderOpts はリスト部分の描画パラメータ。job 一覧はパネルで重ねるため、
// インライン展開 (Expanded) は使わない (行構成を不変に保つ)。
func (m *browseModel) renderOpts() RenderOpts {
	return RenderOpts{
		Oneline: m.oneline,
		Colored: m.colored,
		Spinner: m.spinner(),
		Width:   m.width,
		Decor:   m.decor,
	}
}

func (m *browseModel) lines() []Line {
	if !m.linesValid {
		m.linesCache = RenderLines(m.commits, m.statuses, m.renderOpts())
		m.linesValid = true
	}
	return m.linesCache
}

func (m *browseModel) invalidateLines() {
	m.linesValid = false
}

// pageSize はビューポートの行数 (最下段のヒント行を除く)。
func (m *browseModel) pageSize() int {
	return max(m.height-1, 1)
}

func (m *browseModel) clampOffset(offset int) int {
	maxOffset := max(len(m.lines())-m.pageSize(), 0)
	return min(max(offset, 0), maxOffset)
}

// ensureCursorVisible はカーソル対象コミットのヘッダー行がビューポート内に入るよう
// offset を調整する。
func (m *browseModel) ensureCursorVisible() {
	lines := m.lines()
	header := 0
	for i, l := range lines {
		if l.Header && l.CommitIdx == m.cursor {
			header = i
			break
		}
	}
	page := m.pageSize()
	if header < m.offset {
		m.offset = header
	}
	if header >= m.offset+page {
		m.offset = header - page + 1
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
	window := make([]string, 0, page)
	for i := offset; i < end; i++ {
		text := lines[i].Text
		if lines[i].Header && lines[i].CommitIdx == m.cursor {
			text = cursorMark(m.colored) + text
		} else {
			text = "  " + text
		}
		window = append(window, clipToWidth(text, m.width))
	}
	// job パネルは対象コミットのヘッダー行直下へ「重ねる」(リスト行を置き換える)。
	// リストの行構成自体は変えないので、開閉で後続行がずれない。
	// 下に収まらない場合はビューポート内へ収まる位置まで引き上げる
	if panel := m.panelLines(); len(panel) > 0 {
		start := m.panelAnchor(lines, offset) + 1
		start = min(start, max(page-len(panel), 0))
		start = max(start, 0)
		for i, p := range panel {
			pos := start + i
			if pos < len(window) {
				window[pos] = p
			} else if len(window) < page {
				window = append(window, p)
			}
		}
	}
	var b strings.Builder
	for _, w := range window {
		b.WriteString(w)
		b.WriteString("\n")
	}
	b.WriteString(m.hintLine())
	return b.String()
}

// panelAnchor はパネル対象コミットのヘッダー行のウィンドウ内位置を返す
// (ウィンドウ外へスクロールしている場合は先頭 -1 = パネルは最上部に出る)。
func (m *browseModel) panelAnchor(lines []Line, offset int) int {
	for i, l := range lines {
		if l.Header && l.CommitIdx < len(m.commits) && m.commits[l.CommitIdx].SHA == m.panelSHA {
			return i - offset
		}
	}
	return -1
}

// panelLines は job パネルの描画行 (枠付き)。パネル非表示なら nil。
func (m *browseModel) panelLines() []string {
	if m.panelSHA == "" {
		return nil
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	var commit *Commit
	for i := range m.commits {
		if m.commits[i].SHA == m.panelSHA {
			commit = &m.commits[i]
			break
		}
	}
	if commit == nil {
		return nil
	}
	jobs, haveDetails := m.details[m.panelSHA]
	var rows []string
	switch {
	case m.detailsLoading[m.panelSHA]:
		rows = []string{paint(m.spinner()+" CI job を取得中...", ansiDim, m.colored)}
	case !haveDetails:
		rows = []string{paint("(CI job 情報なし)", ansiDim, m.colored)}
	case len(jobs) == 0:
		rows = []string{paint("(Check はありません)", ansiDim, m.colored)}
	default:
		// panelCursor が見える範囲の job を切り出す (maxPanelJobs でスクロール)
		start := 0
		if m.panelCursor >= maxPanelJobs {
			start = m.panelCursor - maxPanelJobs + 1
		}
		endJob := min(start+maxPanelJobs, len(jobs))
		for i := start; i < endJob; i++ {
			mark := "  "
			if i == m.panelCursor {
				mark = cursorMark(m.colored)
			}
			rows = append(rows, mark+StatusGlyph(jobs[i].State, m.colored, "")+" "+jobs[i].Name)
		}
	}
	title := fmt.Sprintf(" CI jobs: %s %s ", commit.ShortSHA, commit.Subject)
	switch {
	case len(jobs) > 0 && m.panelCursor >= 0:
		title = fmt.Sprintf(" CI jobs: %s (%d/%d) %s ", commit.ShortSHA, m.panelCursor+1, len(jobs), commit.Subject)
	case len(jobs) > 0:
		title = fmt.Sprintf(" CI jobs: %s (%d 件) %s ", commit.ShortSHA, len(jobs), commit.Subject)
	}
	return buildPanelBox(title, rows, width, m.colored)
}

// buildPanelBox は枠線付きのパネルを組み立てる。行の実効幅は ANSI を除いて計算する。
func buildPanelBox(title string, rows []string, width int, colored bool) []string {
	if width < 10 {
		width = 10
	}
	inner := width - 4 // "│ " + " │"
	lines := make([]string, 0, len(rows)+2)
	title = runewidth.Truncate(title, width-2, "…")
	top := "┌" + title + strings.Repeat("─", max(width-2-runewidth.StringWidth(title), 0)) + "┐"
	lines = append(lines, paint(top, ansiDim, colored))
	for _, row := range rows {
		content := clipToWidth(row, inner)
		pad := max(inner-runewidth.StringWidth(stripANSI(content)), 0)
		lines = append(lines, paint("│ ", ansiDim, colored)+content+strings.Repeat(" ", pad)+paint(" │", ansiDim, colored))
	}
	lines = append(lines, paint("└"+strings.Repeat("─", width-2)+"┘", ansiDim, colored))
	return lines
}

func cursorMark(colored bool) string {
	return paint("❯ ", ansiBold, colored)
}

func (m *browseModel) hintLine() string {
	hint := "j/k: 移動  Enter: CI job  q: 終了"
	if m.panelSHA != "" {
		if m.panelCursor >= 0 {
			hint = "j/k: job 移動  Enter: ブラウザで開く  h/Esc: 閉じる  q: 終了"
		} else {
			hint = "j: job を選択  Enter/h/Esc: 閉じる  q: 終了"
		}
	}
	if m.fetching {
		hint = m.spinner() + " CI 状態を取得中...  " + hint
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
