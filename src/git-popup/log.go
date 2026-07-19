package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

type previewMsg struct {
	sha  string // どのコミットの preview か (遅延到着で別選択を上書きしないための照合キー)
	text string
	err  error
}

// ciJobsMsg は選択コミットの CI job 一覧 (別 goroutine で取得)。diff (previewMsg) とは
// 独立に非同期取得し、先に出た方から描画する (CI 取得の ~1s で diff を待たせない)。
type ciJobsMsg struct {
	sha  string
	text string
}

type pushMsg struct{ err error }
type ciResultMsg struct{ states map[string]CIState }

type logModel struct {
	commits []Commit
	ci      map[string]CIState
	cursor  int
	offset  int
	preview string
	ciJobs  string // 選択コミットの CI job ブロック (preview の上に重ねる)
	status  string
	width   int
	height  int
	done    bool
	confirm bool
	busy    bool

	detailOpen   bool // Enter で選択コミットの詳細 (右ペイン) にフォーカスしスクロールするモード
	detailOffset int  // 詳細スクロール位置 (右ペインの先頭行 index)
}

func newLogModel(commits []Commit) *logModel {
	return &logModel{commits: commits}
}

func (m *logModel) Init() tea.Cmd {
	return tea.Batch(m.previewCmd(), m.ciJobsCmd(), m.ciCmd())
}

// previewCmd は diff (git show) だけを取得する (速い)。CI job は ciJobsCmd で別途。
func (m *logModel) previewCmd() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	return func() tea.Msg {
		text, err := loadPreview(sha)
		return previewMsg{sha: sha, text: text, err: err}
	}
}

// ciJobsCmd は選択コミットの CI job 一覧を取得する (gh・~1s)。diff とは独立の goroutine で
// 走らせ、先に出た方から描画する (CI 取得で diff を待たせない)。
func (m *logModel) ciJobsCmd() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	return func() tea.Msg {
		return ciJobsMsg{sha: sha, text: loadCIJobsPreview(sha)}
	}
}

// currentSHA はカーソル位置コミットの SHA ("" = 一覧が空)。preview 照合に使う。
func (m *logModel) currentSHA() string {
	if len(m.commits) == 0 {
		return ""
	}
	return m.commits[m.cursor].SHA
}

func (m *logModel) ciCmd() tea.Cmd {
	commits := append([]Commit(nil), m.commits...)
	return func() tea.Msg { return ciResultMsg{states: loadCI(commits)} }
}

func (m *logModel) Update(msg tea.Msg) (*logModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureCursorVisible()
	case previewMsg:
		if msg.sha != m.currentSHA() {
			break // 遅延到着した別コミットの preview は捨てる
		}
		if msg.err != nil {
			m.status = "preview error: " + msg.err.Error()
		} else {
			m.preview = msg.text
		}
	case ciJobsMsg:
		if msg.sha == m.currentSHA() { // 遅延到着した別コミットの CI は捨てる
			m.ciJobs = msg.text
		}
	case ciResultMsg:
		m.ci = msg.states
	case pushMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "push failed: " + msg.err.Error()
		} else {
			m.status = "push completed"
		}
	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m *logModel) handleKey(key string) (*logModel, tea.Cmd) {
	if m.confirm {
		switch strings.ToLower(key) {
		case "y":
			m.confirm = false
			m.busy = true
			return m, func() tea.Msg { return pushMsg{err: push()} }
		case "n", "esc", "ctrl+c":
			m.confirm = false
			m.status = ""
		}
		return m, nil
	}
	if m.detailOpen { // 詳細スクロールモード中は右ペインのスクロールに専念 (下記)
		return m.handleDetailKey(key)
	}
	if key == "q" || key == "esc" || key == "ctrl+c" || key == "ctrl+g" {
		m.done = true
		return m, tea.Quit
	}
	if m.busy { // push 実行中は終了以外のキーを無視する
		return m, nil
	}
	if key == "ctrl+b" {
		m.confirm = true
		m.status = ""
		return m, nil
	}
	if len(m.commits) == 0 {
		return m, nil
	}
	if key == "enter" { // 選択コミットの詳細 (右ペイン) をスクロールするモードへ
		m.detailOpen = true
		m.detailOffset = 0
		return m, nil
	}
	old := m.cursor
	switch key {
	case "j", "down":
		m.cursor = min(m.cursor+1, len(m.commits)-1)
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	}
	if m.cursor != old {
		m.ensureCursorVisible()
		m.preview = "loading preview..."
		m.ciJobs = "" // 前コミットの CI ブロックを消し、diff と CI を独立に取り直す
		return m, tea.Batch(m.previewCmd(), m.ciJobsCmd())
	}
	return m, nil
}

// rightLines は右ペインの全行 (CI job ブロック + diff)。detail スクロールと View で共用。
func (m *logModel) rightLines() []string {
	return strings.Split(strings.TrimRight(m.ciJobs+m.preview, "\n"), "\n")
}

// handleDetailKey は Enter で入った詳細スクロールモードのキー処理。q/Esc/h/Enter で一覧へ戻る。
func (m *logModel) handleDetailKey(key string) (*logModel, tea.Cmd) {
	rows := m.visibleRows()
	maxOff := max(len(m.rightLines())-rows, 0)
	switch key {
	case "q", "esc", "h", "enter", "left":
		m.detailOpen = false
		m.detailOffset = 0
	case "j", "down", "ctrl+n":
		m.detailOffset = min(m.detailOffset+1, maxOff)
	case "k", "up", "ctrl+p":
		m.detailOffset = max(m.detailOffset-1, 0)
	case " ", "ctrl+d", "pgdown", "f":
		m.detailOffset = min(m.detailOffset+max(rows/2, 1), maxOff)
	case "b", "ctrl+u", "pgup":
		m.detailOffset = max(m.detailOffset-max(rows/2, 1), 0)
	case "g", "home":
		m.detailOffset = 0
	case "G", "end":
		m.detailOffset = maxOff
	}
	return m, nil
}

func (m *logModel) visibleRows() int { return max(m.height-2, 1) }

func (m *logModel) ensureCursorVisible() {
	rows := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *logModel) ciMark(sha string) string {
	if m.ci == nil {
		return ansiDim + "·" + ansiReset // 取得中プレースホルダ
	}
	switch m.ci[sha] {
	case CISuccess:
		return paintFg("active_green", "✓")
	case CIFailure:
		return paintFg("error_red", "✗")
	case CIPending:
		return paintFg("marker_orange", "●")
	default:
		return " "
	}
}

func (m *logModel) View() string {
	// WindowSizeMsg 到着前 (サイズ未確定) は描かない。既定サイズで大きく描くと、実サイズが
	// 判明した次フレームとの差分で端末に残像 (カーソル行の "分身") が出る。
	if m.width < 1 || m.height < 1 {
		return ""
	}
	leftWidth := max(m.width*45/100, 20)
	rightWidth := max(m.width-leftWidth-1, 10)
	rows := m.visibleRows()
	m.ensureCursorVisible()
	left := make([]string, rows)
	for i := range left {
		commitIndex := m.offset + i
		if commitIndex >= len(m.commits) {
			left[i] = ""
			continue
		}
		commit := m.commits[commitIndex]
		line := fmt.Sprintf("  %s %s %s", m.ciMark(commit.SHA), commit.ShortSHA, commit.Subject)
		if commitIndex == m.cursor {
			line = fmt.Sprintf("> %s %s %s", m.ciMark(commit.SHA), commit.ShortSHA, commit.Subject)
		}
		left[i] = clip(line, leftWidth)
	}
	// CI job ブロック (先に来たら上に) + diff。どちらも未着なら (no preview)。
	right := m.rightLines()
	if len(right) == 1 && right[0] == "" {
		right = []string{"(no preview)"}
	}
	if m.detailOpen { // 詳細スクロール: 右ペインを detailOffset だけ送る
		if m.detailOffset < len(right) {
			right = right[m.detailOffset:]
		} else {
			right = nil
		}
	}
	lines := make([]string, rows)
	for i := range lines {
		r := ""
		if i < len(right) {
			r = clip(right[i], rightWidth)
		}
		lines[i] = pad(left[i], leftWidth) + "│" + pad(r, rightWidth)
	}
	header := "git-popup  log"
	footer := "j/k move  Enter: 詳細スクロール  C-b push  q/Esc/C-g quit"
	if m.confirm {
		footer = "push しますか? [y/N]"
	} else if m.busy {
		footer = "pushing..."
	} else if m.detailOpen {
		footer = fmt.Sprintf("[詳細] j/k・Space/b スクロール  g/G 先頭/末尾  q/Esc/Enter 一覧へ  (%d)", m.detailOffset)
	} else if m.status != "" {
		footer = m.status
	}
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer
}

// ansiRe は SGR (色) エスケープ列。表示幅計算・truncate から除外するために使う。
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// rw は East Asian Ambiguous (✓ ● ○ · 罫線 等) を幅 1 として扱う条件。StringWidth と
// RuneWidth を同一条件に揃えないと clip の見積り (RuneWidth) と displayWidth (StringWidth)
// がズレて 1 桁分の切り過ぎ/揃わずが出る。端末側も ✓/● を 1 桁で描くためこれで一致する。
var rw = &runewidth.Condition{EastAsianWidth: false}

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func displayWidth(s string) int { return rw.StringWidth(stripANSI(s)) }

// clip は ANSI (色) を保持したまま「見える幅」で切り詰める。素の runewidth を色付き行に
// 当てると、エスケープ列の分だけ幅を過大評価して早く切れ、さらにエスケープ途中で切ると
// 端末の色が壊れる (git show --color や CI マークで発生)。エスケープはそのまま通し、
// 可視幅だけ数える。切り詰めたら末尾に … と reset を足して色残りを断つ。
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	var b strings.Builder
	vis := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			if loc := ansiRe.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				b.WriteString(s[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		cw := rw.RuneWidth(r)
		if vis+cw > width-1 { // 末尾の … の 1 桁を確保
			break
		}
		b.WriteString(s[i : i+size])
		vis += cw
		i += size
	}
	b.WriteString("…\x1b[0m")
	return b.String()
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(width-displayWidth(s), 0))
}
