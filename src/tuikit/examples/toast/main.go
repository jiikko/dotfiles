// toast は tuikit の toast (右下に数秒だけ出る通知のスタック) を 1 画面で見せるデモ。
// 動かし方と gif の撮り方は tuikit/README.md の「デモ」。
//
//	go run ./examples/toast
//
// キー: s 成功の通知 / f 失敗の通知 / i 進行中の通知 (次の通知が来ると退く) / q 終了
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuikit/layout"
	"tuikit/sgr"
	"tuikit/termwidth"
	"tuikit/toast"
)

const tickInterval = 16 * time.Millisecond

type tickMsg struct{}

type model struct {
	width, height int
	toasts        toast.Stack
	ticking       bool
	n             int // 何枚目の通知か (文を変えるため)
}

func (m *model) Init() tea.Cmd { return nil }

// tick は通知がスライドしている間だけ回す (静止中は toast.Msg の退場タイマーを待つだけで、tick は要らない)。
func (m *model) tick() tea.Cmd {
	if m.ticking || !m.toasts.Animating() {
		return nil
	}
	m.ticking = true
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.ticking = false
		var cmds []tea.Cmd
		for _, t := range m.toasts.Advance() { // 入場を終えた枚の退場タイマー (toast は張らないので、ここで Tick にする)
			msg := t.Msg
			cmds = append(cmds, tea.Tick(t.After, func(time.Time) tea.Msg { return msg }))
		}
		return m, tea.Batch(append(cmds, m.tick())...) // 進めた後で、まだスライド中なら次の tick
	case toast.Msg:
		m.toasts.StartLeaving(msg)
		return m, m.tick()
	case tea.KeyPressMsg:
		m.n++
		switch msg.String() {
		case "s":
			m.toasts.Show(fmt.Sprintf("pushed origin/main (%d)", m.n), true)
		case "f":
			m.toasts.Show(fmt.Sprintf("push rejected: non-fast-forward (%d)", m.n), false)
		case "i":
			m.toasts.ShowInfo(fmt.Sprintf("fetching origin... (%d)", m.n))
		case "l":
			m.toasts.Show(fmt.Sprintf("push rejected: remote rejected the update because the branch is protected and requires review (%d)", m.n), false)
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, m.tick()
	}
	return m, nil
}

func (m *model) View() tea.View {
	w, h := max(m.width, 20), max(m.height, 8)
	lines := make([]string, h)
	lines[0] = sgr.Bold + " tuikit/toast" + sgr.Reset
	help := []string{"", " s  成功の通知 (緑)", " f  失敗の通知 (赤)", " i  進行中の通知 (次の通知で退く)", " l  長い通知 (窓の幅で折り返す)", " q  終了", "",
		sgr.Dim + " 新しい通知は上に積まれ、古い通知は下から抜ける (最大 3 枚)" + sgr.Reset}
	copy(lines[1:], help)
	box := m.toasts.BoxLines(true, h-1, w)
	overlayBottomRight(lines, box, w)
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

// overlayBottomRight は box を window の右下に重ねる (左の背景は残す)。glogx の overlayBoxRight と同じ形 (デモ用に小さく持つ)。
func overlayBottomRight(window, box []string, width int) {
	base := max(len(window)-len(box), 0)
	for i, row := range box {
		pos := base + i
		if pos >= len(window) {
			break
		}
		left := termwidth.Cut(window[pos], max(width-termwidth.Of(row), 0))
		pad := strings.Repeat(" ", max(width-termwidth.Of(row)-termwidth.Of(left), 0))
		window[pos] = left + sgr.Reset + pad + row
	}
}

func main() {
	m := &model{toasts: toast.Stack{Shadow: layout.ShadowNearBlack}}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "toast:", err)
		os.Exit(1)
	}
}
