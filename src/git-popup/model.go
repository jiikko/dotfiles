package main

import tea "github.com/charmbracelet/bubbletea"

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
