package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"pro-con/agents"
)

// Claude Code の session の一覧 (claude agents --json)。3 秒ごとに裏で取り直す。前の取得が終わってから次を予約するので重ならない。
// 取れなかったときは「0 本」にせず、最後に取れた一覧を残したまま理由を出す。

const sessionsInterval = 3 * time.Second

type sessionsMsg struct {
	ss  []agents.Session
	err error
}

type sessionsTickMsg struct{}

func (m *Model) fetchSessions() tea.Cmd {
	list := m.listSessions
	return m.child(func() tea.Msg {
		ss, err := list(context.Background())
		return sessionsMsg{ss: ss, err: err}
	})
}

func (m *Model) onSessions(msg sessionsMsg) tea.Cmd {
	m.sessFetched = true
	if msg.err != nil {
		m.sessErr = msg.err.Error()
	} else {
		m.sessions, m.sessErr = msg.ss, ""
	}
	return tea.Tick(sessionsInterval, func(time.Time) tea.Msg { return sessionsTickMsg{} })
}

// sessionsSummary はゲージに出す 1 項目 (claude N 本 / うち裏 M)。
func (m *Model) sessionsSummary() string {
	switch {
	case !m.sessFetched:
		return "claude 取得中"
	case m.sessErr != "" && m.sessions == nil:
		return sgrRed + "claude の一覧を取れない" + sgrFgReset
	}
	bg := 0
	for _, s := range m.sessions {
		if s.Kind == "background" {
			bg++
		}
	}
	out := fmt.Sprintf("claude %d 本 (裏 %d)", len(m.sessions), bg)
	if m.sessErr != "" {
		out += sgrRed + " 更新失敗" + sgrFgReset
	}
	return out
}

// sessionsBlock は s で開く一覧のパネル。
func (m *Model) sessionsBlock() []string {
	w := m.width
	border := fg(51)
	out := []string{boxTop(border, sgrBold+fmt.Sprintf("Claude Code の session (%d)", len(m.sessions))+sgrReset, w)}
	if m.sessErr != "" {
		out = append(out, boxLine(border, " "+sgrRed+"一覧を取れない: "+m.sessErr+sgrReset+sgrDim+"  (下は最後に取れた一覧)"+sgrReset, w))
	}
	if !m.sessFetched {
		out = append(out, boxLine(border, sgrDim+" 取得中…"+sgrReset, w))
	}
	head := sgrDim + fit(" 種類", 7) + fit("状態", 30) + fit("名前", 26) + fit("経過", 10) + fit("pid", 8) + "場所" + sgrReset
	out = append(out, boxLine(border, head, w))
	home, _ := os.UserHomeDir()
	now := m.now()
	for _, s := range m.sessions {
		kind := "対話"
		if s.Kind == "background" {
			kind = "裏"
		}
		status := s.Status
		col := fg(250)
		switch s.Status {
		case "busy":
			col = fg(46)
		case "waiting":
			col = sgrYellow
			if s.WaitingFor != "" {
				status += ": " + s.WaitingFor
			}
		}
		cwd := s.Cwd
		if home != "" && strings.HasPrefix(cwd, home) {
			cwd = "~" + cwd[len(home):]
		}
		row := fit(" "+kind, 7) + col + fit(status, 30) + sgrFgReset + fit(s.Name, 26) + fit(fmtDur(now.Sub(s.Started())), 10) +
			fit(fmt.Sprint(s.PID), 8) + cwd
		out = append(out, boxLine(border, row, w))
	}
	return append(out, boxBottom(border, w))
}
