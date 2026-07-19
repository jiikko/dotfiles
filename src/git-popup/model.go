package main

import tea "github.com/charmbracelet/bubbletea"

// rootModel は logModel の薄い wrapper。かつては C-l で changes(staging) 画面と
// トグルしていたが、ユーザー判断 (2026-07-19) で log 専用に縮小した (stage/commit は
// 別の手段でやる)。将来 view を増やすならここに mode を復活させる。
type rootModel struct {
	log *logModel
}

func newRootModel(commits []Commit) *rootModel {
	return &rootModel{log: newLogModel(commits)}
}

func (m *rootModel) Init() tea.Cmd { return m.log.Init() }

func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.log.Update(msg)
	m.log = updated
	return m, cmd
}

func (m *rootModel) View() string { return m.log.View() }
