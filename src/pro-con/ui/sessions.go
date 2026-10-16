package ui

import (
	"fmt"
	"time"

	"pro-con/card"
)

// screensBlock は開いている画面の一覧 (設定画面のプロセスのタブの下段。issue 481)。持ち主の画面が 1 つだけなら出さない (ヘッダと同じ)。
// 見ているだけの画面 (--view) は画面の印を置かないので入らない。w は板の幅。
func (m *Model) screensBlock(w int) []string {
	ss := m.snap.Screens
	if len(ss) == 0 || (len(ss) == 1 && !ss[0].Join) {
		return nil
	}
	border := fg(colInfo)
	owners, joins := m.snap.ScreenTally()
	out := []string{boxTop(border, sgrBold+fmt.Sprintf("開いている画面 %d (持ち主 %d・join %d)", len(ss), owners, joins)+sgrReset, w)}
	out = append(out, boxLine(border, sgrDim+fit(" 画面", 10)+fit("モード", 22)+fit("開いた", openedWidth)+fit("端末", 16)+"実体"+sgrReset, w))
	for _, s := range ss {
		mode := screenMode(s)
		if s.Self {
			mode += " (この画面)"
		}
		tty := s.TTY
		if tty == "" {
			tty = "-"
		}
		row := fit(" "+s.ID, 10) + fit(mode, 22) + fit(openedText(s.Opened, m.snap.Now), openedWidth) +
			fit(tty, 16) + fmt.Sprintf("pid %d", s.PID)
		out = append(out, boxLine(border, row, w))
	}
	return append(out, boxBottom(border, w))
}

// openedWidth は「開いた」の欄の幅。openedText の最長 (「15:04 (999日前)」の 16 桁) に区切りの 2 桁。
const openedWidth = 18

// openedText は「開いた」の欄の中身。経過は 1 時間以上を時間・日に丸める (この欄だけ。案C の「1時間5分」の形は欄に収まらず切れていた。
// 2026-10-16 に見本からユーザーが選んだ案 B。issue 780)。端数は時刻の側で読める。
func openedText(opened, now time.Time) string {
	d := now.Sub(opened)
	age := card.Age(d)
	switch {
	case d >= 24*time.Hour:
		age = fmt.Sprintf("%d日", int(d/(24*time.Hour)))
	case d >= time.Hour:
		age = fmt.Sprintf("%d時間", int(d/time.Hour))
	}
	return opened.Local().Format("15:04") + " (" + age + "前)"
}
