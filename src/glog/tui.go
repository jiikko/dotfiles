package main

import (
	"context"
	"maps"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Bubble Tea は対話 UI のためではなく「非同期レンダリング可能な CLI ランタイム」として使う
// (issue の設計)。Alt Screen へは切り替えず、インライン描画で最終表示をターミナル履歴に残す。
// goroutine (fetch Cmd) は stdout へ直接書かず、結果を必ず tea.Msg として返す。

const (
	fetchTimeout    = 10 * time.Second
	spinnerInterval = 80 * time.Millisecond
)

type ciResultMsg struct {
	fetched map[string]CIState
	ghErr   *GHError
}

type tickMsg struct{}

// renderFunc は「現在の状態 → 画面全体の文字列」。log / --cached の両モードを同じ
// TUI ループで扱うための差し替え点。
type renderFunc func(statuses map[string]CIState, width int, spinner string) string

type tuiModel struct {
	render   renderFunc
	statuses map[string]CIState // 表示用 (キャッシュ + 取得結果のマージ)
	fetched  map[string]CIState // API から取得した分 (終了後のキャッシュ保存用)
	toFetch  []string
	ghErr    *GHError
	frame    int
	width    int
	done     bool
	fetch    tea.Cmd
	cancel   context.CancelFunc
}

func newTUIModel(render renderFunc, statuses map[string]CIState, toFetch []string, repo Repo, width int) *tuiModel {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	m := &tuiModel{
		render:   render,
		statuses: statuses,
		toFetch:  toFetch,
		width:    width,
		cancel:   cancel,
	}
	m.fetch = func() tea.Msg {
		defer cancel()
		fetched, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, toFetch)
		return ciResultMsg{fetched: fetched, ghErr: ghErr}
	}
	return m
}

func (m *tuiModel) Init() tea.Cmd {
	return tea.Batch(m.fetch, tick())
}

func tick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tickMsg:
		if m.done {
			return m, nil
		}
		m.frame++
		return m, tick()
	case ciResultMsg:
		m.ghErr = msg.ghErr
		if msg.fetched != nil {
			m.fetched = msg.fetched
			maps.Copy(m.statuses, msg.fetched)
		}
		m.fillUnknown()
		m.done = true
		return m, tea.Quit
	case tea.KeyMsg:
		// キー操作は原則 Ctrl-C だけ (issue の設計)
		if msg.String() == "ctrl+c" {
			m.cancel()
			m.fillUnknown()
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// fillUnknown は結果が得られなかった SHA を「取得中」のまま残さず unknown へ落とす。
func (m *tuiModel) fillUnknown() {
	for _, sha := range m.toFetch {
		if _, ok := m.statuses[sha]; !ok {
			m.statuses[sha] = StateUnknown
		}
	}
}

func (m *tuiModel) View() string {
	return m.render(m.statuses, m.width, spinnerFrames[m.frame%len(spinnerFrames)]) + "\n"
}

// RunTUI はインライン TUI を実行し、最終状態のモデルを返す。
// 最終フレーム (CI 状態確定後の表示) はそのままターミナル履歴に残る。
func RunTUI(m *tuiModel) (*tuiModel, error) {
	p := tea.NewProgram(m) // WithAltScreen は使わない
	final, err := p.Run()
	if err != nil {
		return m, err
	}
	if fm, ok := final.(*tuiModel); ok {
		return fm, nil
	}
	return m, nil
}
