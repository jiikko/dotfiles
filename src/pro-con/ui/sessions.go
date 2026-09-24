package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"pro-con/agents"
	"pro-con/card"
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

// pgBlock は s で開く PG (consumer) の一覧。PG ごとに担当カード・状態・実行中のコマンド・経過を出し、claude agents で
// 取った session のうち PG と同じ session のものを紐づける (pid)。PG でない session (Desktop の対話 等) は件数だけ
// (全部並べると pro-con と関係の無い session に埋もれて、PG が今どうなっているかが読めない)。
func (m *Model) pgBlock() []string {
	w := m.width
	border := fg(51)
	cards := map[string]card.Card{}
	queued := 0
	for _, c := range m.snap.Cards {
		cards[c.ID] = c
		if c.State == card.Planned {
			queued++
		}
	}
	title := fmt.Sprintf("PG (consumer) %d/%d  PG の空き待ち %d 件", len(m.snap.Consumers), m.snap.Limit, queued)
	out := []string{boxTop(border, sgrBold+title+sgrReset, w)}
	if len(m.snap.Consumers) == 0 {
		out = append(out, boxLine(border, sgrDim+" 動いている PG は無い"+sgrReset, w))
	} else {
		head := sgrDim + fit(" PG", 11) + fit("状態", 18) + fit("カード", 36) + fit("実行中", 30) + fit("経過", 9) + "実体" + sgrReset
		out = append(out, boxLine(border, head, w))
	}
	matched := map[int]bool{}
	for _, pg := range m.snap.Consumers {
		c := cards[pg.CardID]
		status, col := pgStatus(c)
		run := "-"
		if e := c.Exec; e.Active() {
			run = e.Command
			if e.Resource != "" {
				run = e.Resource + ": " + run
			}
			run += " " + fmtDur(m.snap.Now.Sub(e.Since))
		}
		real := sgrDim + "模擬 (claude の session に無い)" + sgrReset
		for i, s := range m.sessions {
			if s.ID == pg.Session || s.SessionID == pg.Session {
				real, matched[i] = fmt.Sprintf("pid %d", s.PID), true
			}
		}
		row := fit(" "+pg.Session, 11) + col + fit(status, 18) + sgrFgReset + fit(c.ID+issueTag(c)+" "+c.Title, 36) +
			fit(run, 30) + fit(fmtDur(m.snap.Now.Sub(c.Since)), 9) + real
		out = append(out, boxLine(border, row, w))
	}
	interactive, background := 0, 0
	for i, s := range m.sessions {
		if matched[i] {
			continue
		}
		if s.Kind == "background" {
			background++
		} else {
			interactive++
		}
	}
	other := fmt.Sprintf(" ほかの Claude Code の session (PG ではない): 対話 %d / 裏 %d", interactive, background)
	switch {
	case m.sessErr != "":
		other += sgrRed + "  一覧を取れない: " + m.sessErr + sgrReset
	case !m.sessFetched:
		other = " ほかの Claude Code の session: 取得中…"
	}
	out = append(out, boxLine(border, sgrDim+other+sgrReset, w))
	return append(out, boxBottom(border, w))
}

// pgStatus は PG の状態の語と色 (担当カードから)。
func pgStatus(c card.Card) (string, string) {
	switch {
	case c.Stalled:
		return "🚨 停滞", sgrRed
	case c.Wait.Kind == card.WaitPermission:
		return "権限待ち", sgrYellow
	case c.Wait.Kind == card.WaitResource:
		return fmt.Sprintf("%s 待ち %d 番目", c.Wait.Resource, c.Wait.Position), sgrYellow
	case c.Wait.Kind == card.WaitQuota:
		return "枠待ち", sgrYellow
	case c.Exec.Active():
		return "▶ 実行中", fg(46)
	}
	return "作業中", fg(46)
}
