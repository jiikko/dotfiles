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

	unpushed map[string]bool // @{upstream} に未 push のコミット SHA (色分け用)
}

func newLogModel(commits []Commit) *logModel {
	return &logModel{commits: commits, unpushed: loadUnpushed()}
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
		// 縦に拡大すると paneRows が増え最大オフセットが減るため詳細スクロール位置を再クランプ
		m.detailOffset = min(m.detailOffset, max(len(m.rightLines())-m.paneRows(), 0))
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
			m.unpushed = loadUnpushed() // push 成功で未 push 集合が変わる → 色分けを更新
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

// rightLines は右ペインの diff 行 (CI job は含めない。CI は View で上部固定オーバーレイ)。
// detail スクロールの範囲計算と View で共用。
func (m *logModel) rightLines() []string {
	return strings.Split(strings.TrimRight(m.preview, "\n"), "\n")
}

// handleDetailKey は Enter で入った詳細スクロールモードのキー処理。q/Esc/h/Enter で一覧へ戻る。
func (m *logModel) handleDetailKey(key string) (*logModel, tea.Cmd) {
	rows := m.paneRows()
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

// layout は現在の端末サイズのペイン寸法 (共通実装は layout.go)。
func (m *logModel) layout() paneLayout { return layoutFor(m.width, m.height) }
func (m *logModel) paneRows() int      { return m.layout().paneRows() }

func (m *logModel) ensureCursorVisible() {
	m.offset = clampOffset(m.cursor, m.offset, m.paneRows())
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

// buildListLines は左ペインのコミット一覧を rows 行ぶん組む (offset でスクロール)。
// 行頭: カーソル (accent の ▌) + CI マーク + 短SHA (未 push=橙 / push 済み=灰) + 件名。
func (m *logModel) buildListLines(w, rows int) []string {
	lines := make([]string, rows)
	for i := range lines {
		idx := m.offset + i
		if idx >= len(m.commits) {
			continue
		}
		c := m.commits[idx]
		cursor := "  "
		if idx == m.cursor {
			cursor = paintFg("current_accent", "▌") + " "
		}
		sha := c.ShortSHA
		if m.unpushed != nil { // push 状態が分かるときだけ色分け
			if m.unpushed[c.SHA] {
				sha = paintFg("marker_orange", c.ShortSHA) // 未 push = 橙で目立たせる
			} else {
				sha = paintFg("cold_gray", c.ShortSHA) // push 済み = 落ち着いた灰
			}
		}
		lines[i] = clip(cursor+m.ciMark(c.SHA)+" "+sha+" "+c.Subject, w)
	}
	return lines
}

// buildDetailLines は右ペイン (diff) を rows 行ぶん組む。CI job 結果は「上部固定オーバーレイ」
// として先頭数行に重ねる (挿入しないので取得完了で diff が押し下がらない = 高さ不変)。
// 詳細スクロール中 (detailOpen) は diff を隅々まで読めるようオーバーレイしない。
func (m *logModel) buildDetailLines(w, rows int) []string {
	diff := m.rightLines()
	if len(diff) == 1 && diff[0] == "" {
		diff = []string{ansiDim + "(no preview)" + ansiReset}
	}
	base := 0
	if m.detailOpen {
		base = m.detailOffset
	}
	lines := make([]string, rows)
	for i := range lines {
		if idx := base + i; idx < len(diff) {
			lines[i] = clip(diff[idx], w)
		}
	}
	if !m.detailOpen && m.ciJobs != "" {
		ci := strings.Split(strings.TrimRight(m.ciJobs, "\n"), "\n")
		for i := 0; i < len(ci) && i < rows; i++ {
			lines[i] = clip(ci[i], w)
		}
	}
	return lines
}

func (m *logModel) View() string {
	// WindowSizeMsg 到着前 (サイズ未確定) は描かない (残像=カーソル分身の防止)。
	if m.width < 1 || m.height < 1 {
		return ""
	}
	l := m.layout()
	if l.tooSmall() {
		return l.degradeView()
	}
	m.ensureCursorVisible()

	footer := "j/k 移動  Enter: 詳細  C-b push  q/Esc/C-g 閉じる"
	switch {
	case m.confirm:
		footer = "push しますか? [y/N]"
	case m.busy:
		footer = "pushing..."
	case m.detailOpen:
		footer = fmt.Sprintf("[詳細] j/k・Space/b・g/G スクロール  q/Esc/Enter 一覧へ  (%d)", m.detailOffset)
	case m.status != "":
		footer = m.status
	}
	return l.render(
		m.buildListLines(l.leftInnerW(), l.paneRows()),
		m.buildDetailLines(l.rightInnerW(), l.paneRows()),
		!m.detailOpen, // 一覧フォーカス時は左を accent (詳細中は右)
		footer)
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
