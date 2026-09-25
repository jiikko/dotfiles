package ui

import (
	"fmt"

	"pro-con/card"
)

// pgBlock は s で開く PG (consumer) の一覧。PG ごとに担当カード・状態・実行中のコマンド・経過・実体の pid を出す。
// 🚨 画面は claude agents を自分で読まない。PG の情報は backend の Snapshot だけから出す (本物のモードは pro-con が起動した
// session だけを扱うので、pro-con の外の session は名前も本数も出さない。2026-09-24 のユーザーの方針)。
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
		real := sgrDim + "模擬" + sgrReset
		if pg.PID > 0 {
			real = fmt.Sprintf("pid %d", pg.PID)
		}
		row := fit(" "+pg.Session, 11) + col + fit(status, 18) + sgrFgReset + fit(c.ID+issueTag(c)+" "+c.Title, 36) +
			fit(run, 30) + fit(fmtDur(m.snap.Now.Sub(c.Since)), 9) + real
		out = append(out, boxLine(border, row, w))
	}
	out = append(out, boxBottom(border, w))
	return append(out, m.screensBlock()...)
}

// screensBlock は開いている画面の一覧 (s の板の下段。issue 481)。持ち主の画面が 1 つだけなら出さない (ヘッダと同じ)。
// 見ているだけの画面 (--view) は画面の印を置かないので入らない。
func (m *Model) screensBlock() []string {
	ss := m.snap.Screens
	if len(ss) == 0 || (len(ss) == 1 && !ss[0].Join) {
		return nil
	}
	w := m.width
	border := fg(51)
	owners, joins := m.snap.ScreenTally()
	out := []string{boxTop(border, sgrBold+fmt.Sprintf("開いている画面 %d (持ち主 %d・join %d)", len(ss), owners, joins)+sgrReset, w)}
	out = append(out, boxLine(border, sgrDim+fit(" 画面", 10)+fit("モード", 22)+fit("開いた", 16)+fit("端末", 16)+"実体"+sgrReset, w))
	for _, s := range ss {
		mode := screenMode(s)
		if s.Self {
			mode += " (この画面)"
		}
		tty := s.TTY
		if tty == "" {
			tty = "-"
		}
		row := fit(" "+s.ID, 10) + fit(mode, 22) + fit(s.Opened.Local().Format("15:04")+" ("+fmtDur(m.snap.Now.Sub(s.Opened))+"前)", 16) +
			fit(tty, 16) + fmt.Sprintf("pid %d", s.PID)
		out = append(out, boxLine(border, row, w))
	}
	return append(out, boxBottom(border, w))
}

// pgStatus は PG の状態の語と色 (担当カードから)。
func pgStatus(c card.Card) (string, string) {
	switch {
	case c.Stalled:
		return "🚨 停滞", sgrRed
	case c.Wait.Kind == card.WaitPermission:
		return "権限待ち", sgrYellow
	case c.Wait.Kind == card.WaitCrashed:
		return "🚨 落ちて止めた", sgrRed
	case c.Wait.Kind == card.WaitResource:
		return fmt.Sprintf("%s 待ち %d 番目", c.Wait.Resource, c.Wait.Position), sgrYellow
	case c.Wait.Kind == card.WaitQuota:
		return "枠待ち", sgrYellow
	case c.Exec.Active():
		return "▶ 実行中", fg(46)
	}
	return "作業中", fg(46)
}
