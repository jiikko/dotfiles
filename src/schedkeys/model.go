// 予約入力ウィザードの状態機械。画面は 3 つ (menu / form / pick) で、決めた結果を result に残して
// 終了する。tmux や job ファイルには一切触れない (呼び出し側の scripts/tmux_schedule_keys.sh が行う):
// この分離があるので、破壊的な操作 (取消) の確認と実行はシェル側のテスト済み経路に残る。
package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type screen int

const (
	screenMenu screen = iota
	screenForm
	screenPick
)

// result は out ファイルに書く 1 行。action は "new" / "cancel" / "" (中止)。
type result struct {
	action string
	at     time.Time // new のときの発火時刻
	text   string    // new のときの送る文字列
	id     string    // cancel のときの予約 id
}

type model struct {
	screen  screen
	label   string // 送り先 pane の表示名
	now     time.Time
	jobs    []job
	menuIdx int
	form    formState
	pickIdx int
	res     result
	quit    bool
	width   int
}

func newModel(label string, now time.Time, jobs []job) *model {
	return &model{label: label, now: now, jobs: jobs, form: newForm(), width: 72}
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// handleKey は画面ごとのキー処理。終了するときだけ tea.Quit を返す。
func (m *model) handleKey(k tea.KeyPressMsg) tea.Cmd {
	key := k.String()
	if key == "ctrl+c" {
		m.res = result{}
		m.quit = true
		return tea.Quit
	}
	switch m.screen {
	case screenMenu:
		return m.keyMenu(key)
	case screenForm:
		return m.keyForm(key, k.Text)
	case screenPick:
		return m.keyPick(key)
	}
	return nil
}

func (m *model) keyMenu(key string) tea.Cmd {
	items := m.menuItems()
	switch key {
	case "esc", "q":
		m.quit = true
		return tea.Quit
	case "j", "down", "tab":
		m.menuIdx = (m.menuIdx + 1) % len(items)
	case "k", "up", "shift+tab":
		m.menuIdx = (m.menuIdx - 1 + len(items)) % len(items)
	case "enter":
		if m.menuIdx == 0 {
			m.screen = screenForm
			return nil
		}
		if len(m.jobs) == 0 {
			return nil
		}
		m.screen = screenPick
	}
	return nil
}

func (m *model) menuItems() []string {
	return []string{"新規予約", fmt.Sprintf("予約一覧・取消 (%d 件)", len(m.jobs))}
}

func (m *model) keyForm(key, text string) tea.Cmd {
	switch key {
	case "esc":
		m.screen = screenMenu
		return nil
	case "enter":
		at, txt, err := m.form.submit(m.now)
		if err != "" {
			m.form.err = err
			return nil
		}
		m.res = result{action: "new", at: at, text: txt}
		m.quit = true
		return tea.Quit
	}
	m.form.handleKey(key, text, m.now)
	return nil
}

func (m *model) keyPick(key string) tea.Cmd {
	switch key {
	case "esc", "q":
		m.screen = screenMenu
	case "j", "down":
		if m.pickIdx < len(m.jobs)-1 {
			m.pickIdx++
		}
	case "k", "up":
		if m.pickIdx > 0 {
			m.pickIdx--
		}
	case "enter":
		if len(m.jobs) == 0 {
			return nil
		}
		// 取消の確認と実行はシェル側 (gum confirm --default=false → cancel_job)。ここは選ぶだけ
		m.res = result{action: "cancel", id: m.jobs[m.pickIdx].id}
		m.quit = true
		return tea.Quit
	}
	return nil
}

func (m *model) View() tea.View {
	var v tea.View
	var body string
	var cur *tea.Cursor
	switch m.screen {
	case screenMenu:
		body = m.viewMenu()
	case screenForm:
		body, cur = m.form.view(m.label, m.now, m.width)
	case screenPick:
		body = m.viewPick()
	}
	v.SetContent(body)
	v.Cursor = cur // form のときだけ本物のカーソルを置く (IME の未確定文字がそこに出る)
	return v
}

func (m *model) viewMenu() string {
	var b strings.Builder
	b.WriteString(dim("予約入力  対象: ") + m.label + "\n\n")
	for i, it := range m.menuItems() {
		if i == 1 && len(m.jobs) == 0 {
			it = dim(it)
		}
		b.WriteString(row(i == m.menuIdx, it) + "\n")
	}
	b.WriteString("\n" + dim("j/k 移動   Enter 決定   Esc 閉じる"))
	return b.String()
}

func (m *model) viewPick() string {
	var b strings.Builder
	b.WriteString(dim("予約一覧") + "\n\n")
	remW, labelW := 0, 0
	for _, j := range m.jobs {
		remW = max(remW, ansi.StringWidth(formatRemaining(j.at.Sub(m.now))))
		labelW = max(labelW, ansi.StringWidth(j.label))
	}
	for i, j := range m.jobs {
		line := fmt.Sprintf("%s  %s  %s",
			pad(formatRemaining(j.at.Sub(m.now)), remW),
			pad(j.label, labelW),
			j.text)
		b.WriteString(row(i == m.pickIdx, line) + "\n")
	}
	b.WriteString("\n" + dim("j/k 移動   Enter 取消 (確認あり)   Esc 戻る"))
	return b.String()
}

// row は選択行を反転で描く (色は端末のテーマに任せる)。
func row(selected bool, s string) string {
	if selected {
		return "\x1b[7m> " + s + "\x1b[0m"
	}
	return "  " + s
}

func dim(s string) string { return "\x1b[2m" + s + "\x1b[0m" }

// pad は表示幅 (東アジア文字 = 2 セル) で右詰めする。byte 数で詰めると日本語で崩れる。
func pad(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
