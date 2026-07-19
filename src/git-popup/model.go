package main

import tea "github.com/charmbracelet/bubbletea"

type rootModel struct {
	log     *logModel
	changes *changesModel
	mode    rootMode
	width   int
	height  int
}

type rootMode int

const (
	modeLog rootMode = iota
	modeChanges
)

func newRootModel(commits []Commit) *rootModel {
	return &rootModel{log: newLogModel(commits), mode: modeLog}
}

func (m *rootModel) Init() tea.Cmd { return m.log.Init() }

// input は現在のフォーカス先が commit メッセージ入力中かを返す。入力中は C-l の mode
// トグルを抑止し、入力中のメッセージを失わないようにする (C-l は changes 側へ委譲され、
// rune でないため無視される)。
func (m *rootModel) input() inputMode {
	if m.mode == modeChanges && m.changes != nil {
		return m.changes.input
	}
	return inputNone
}

func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ウィンドウサイズは root で覚えておき、mode 切替で生成し直すモデルへ引き継ぐ
	// (WindowSizeMsg は起動時に 1 度だけ来るので、後から作る changesModel には届かない)。
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+l" && m.input() == inputNone {
		if m.mode == modeLog {
			m.mode = modeChanges
			m.changes = newChangesModel()
			m.changes.width, m.changes.height = m.width, m.height
			return m, m.changes.statusCmd()
		}
		m.mode = modeLog
		m.log.width, m.log.height = m.width, m.height // changes 中に resize されていても復帰時に一致させる
		return m, nil
	}
	if m.mode == modeLog {
		updated, cmd := m.log.Update(msg)
		m.log = updated
		return m, cmd
	}
	updated, cmd := m.changes.Update(msg)
	m.changes = updated
	return m, cmd
}

func (m *rootModel) View() string {
	if m.mode == modeChanges {
		return m.changes.View()
	}
	return m.log.View()
}
