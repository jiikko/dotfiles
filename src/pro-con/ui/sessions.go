package ui

import "fmt"

// screensBlock は開いている画面の一覧 (設定画面のプロセスのタブの下段。issue 481)。持ち主の画面が 1 つだけなら出さない (ヘッダと同じ)。
// 見ているだけの画面 (--view) は画面の印を置かないので入らない。w は板の幅。
func (m *Model) screensBlock(w int) []string {
	ss := m.snap.Screens
	if len(ss) == 0 || (len(ss) == 1 && !ss[0].Join) {
		return nil
	}
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
