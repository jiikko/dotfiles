package ui

// 設定画面のログのタブ (issue 512)。プロセスの出来事 (起動・落ちた・止めた・起こし直した・入れ替わった・取り込んだ) を、pro-con log と同じ
// 出どころ (events.jsonl を eventlog.Follower で読む) から時刻順に並べる。見た目は 2026-09-26 にユーザーが見本から選んだ A (syslog 風):
// 新しいほど下・時刻・役・カード・出来事の 1 行・繰り返しは行末に「… ×N 回、最後 hh:mm (最初 hh:mm)」。c でカードの出来事も混ぜる。
// 🚨 描くたびに読まない: 開いたとき・ログのタブへ移ったとき・backend の変化の知らせ (Notifier。知らせない backend は画面の tick) で、
// 裏で足された分だけ読む。畳んだ行は読んだとき・c で切り替えたときにだけ組み直す。

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"termsafe"
	"tuikit/listnav"
	"tuikit/termwidth"

	"pro-con/backend"
	"pro-con/eventlog"
)

// eventLog は設定画面のログのタブの状態。
type eventLog struct {
	events    []eventlog.Event  // 読んだ出来事 (ファイルの順)
	rows      []eventlog.Folded // 畳んだ行 (events と showCards から組む)
	err       error
	read      bool // 1 度でも読んだ
	loading   bool
	showCards bool // カードの出来事も出す (c)
	follow    bool // 選んでいる行を最新 (一番下) に付ける。開いたとき・一番下へ送ったとき
}

type eventsMsg struct {
	evs []eventlog.Event
	err error
}

func (m *Model) eventLogReader() backend.EventLog {
	r, _ := m.be.(backend.EventLog)
	return r
}

// fetchEvents は足された出来事を裏で読む (読んでいる最中・読めない backend なら nil)。
func (m *Model) fetchEvents() tea.Cmd {
	r := m.eventLogReader()
	if r == nil || m.set.log.loading {
		return nil
	}
	m.set.log.loading = true
	return func() tea.Msg {
		evs, err := r.Events()
		return eventsMsg{evs: evs, err: err}
	}
}

// fetchEventsIfShown は、ログのタブが見えている間だけ読む (変化の知らせ・tick から呼ぶ)。
func (m *Model) fetchEventsIfShown() tea.Cmd {
	if !m.set.open || m.set.tab != tabLog {
		return nil
	}
	return m.fetchEvents()
}

func (m *Model) onEvents(msg eventsMsg) {
	l := &m.set.log
	l.loading, l.read, l.err = false, true, msg.err
	if len(msg.evs) > 0 {
		l.events = append(l.events, msg.evs...)
		m.refoldEvents()
	}
}

// refoldEvents は畳んだ行を組み直す。最新に付けているなら一番下を選び、そうでなければ前に選んでいた行の時刻に近い行を選ぶ
// (行の番号は組み直すと動く: c で出す出来事が変わる・受付の箱を経た出来事が途中に差し込まれる・畳みがつながる)。
func (m *Model) refoldEvents() {
	l := &m.set.log
	var anchor time.Time // 選んでいた行の最後の時刻
	if m.set.cursor < len(l.rows) {
		anchor = l.rows[m.set.cursor].Last.At
	}
	role := eventlog.Role
	if l.showCards {
		role = eventlog.AnyRole
	}
	l.rows = eventlog.Fold(l.events, role)
	if m.set.tab != tabLog {
		return
	}
	last := max(len(l.rows)-1, 0)
	if l.follow || anchor.IsZero() {
		m.set.cursor = last
		return
	}
	m.set.cursor = 0 // 時刻が anchor 以前の最後の行 (どれも後なら先頭)
	for i, f := range l.rows {
		if f.Last.At.After(anchor) {
			break
		}
		m.set.cursor = i
	}
}

// toggleCardEvents は c: カードの出来事を混ぜる / プロセスの出来事だけにする (見ていた位置は変えない)。
func (m *Model) toggleCardEvents() {
	m.set.log.showCards = !m.set.log.showCards
	m.refoldEvents()
}

// logRoleColors は役の色 (見本 A と同じ)。
var logRoleColors = map[string]int{
	eventlog.RoleDispatcher: 51,
	eventlog.RoleSupervisor: 141,
	eventlog.RoleMonitor:    117,
	eventlog.RolePM:         214,
	eventlog.RoleIntegrator: 208,
	eventlog.RolePG:         46,
}

// logReasonColor は出来事の文の色 (落ちた・できない = 危険の赤 / 疑い・停滞 = 要対応の黄。ほかは色を付けない)。
func logReasonColor(e eventlog.Event) string {
	switch e.Kind {
	case eventlog.KindRecover: // 「判定できないので待つ: なし」のように、無事でも「できない」を含む
		return ""
	case eventlog.KindCrash, eventlog.KindError:
		return fg(196)
	case eventlog.KindSuspect, eventlog.KindWatchdog:
		return fg(214)
	}
	for _, w := range []string{"落ちた", "できない", "止めきれない", "起こせない", "抜けた ("} {
		if strings.Contains(e.Reason, w) {
			return fg(196)
		}
	}
	if strings.Contains(e.Reason, "疑い") {
		return fg(214)
	}
	return ""
}

// logHalfPage はログのタブの半ページの送り (ctrl+d / ctrl+u。ほかのタブは短いので端まで送る)。
const logHalfPage = 10

// ログのタブの列の幅 (時刻 = "01-02 15:04:05" + 空白 / 役 / カード)。
const (
	logTimeCells = 15
	logRoleCells = 12
	logCardCells = 8
)

// logLines はログのタブ (rows 行に収める)。枠の見出しと列の名前は残し、選んでいる行が入るよう出来事の行だけを送る。
func (m *Model) logLines(w, rows int) []string {
	border := fg(51)
	l := &m.set.log
	what, verb := "プロセスの出来事", "c でカードの出来事も出す"
	if l.showCards {
		what, verb = "出来事 (カードの出来事も)", "c でプロセスの出来事だけにする"
	}
	title := sgrBold + what + sgrReset
	if l.read {
		title += sgrDim + fmt.Sprintf("  %d 行 (畳む前 %d 件) · 新しいほど下 · %s", len(l.rows), foldedCount(l.rows), verb) + sgrReset
	}
	out := []string{boxTop(border, title, w)}
	switch {
	case m.eventLogReader() == nil:
		out = append(out, boxLine(border, sgrDim+" この画面では読めない"+sgrReset, w))
	case !l.read:
		out = append(out, boxLine(border, sgrDim+" 読んでいる…"+sgrReset, w))
	default:
		if l.err != nil {
			out = append(out, boxLine(border, sgrYellow+" 読めない: "+termsafe.PlainLine(l.err.Error())+sgrFgReset, w))
		}
		if len(l.rows) == 0 {
			out = append(out, boxLine(border, sgrDim+" まだ出来事が無い"+sgrReset, w))
			break
		}
		out = append(out, boxLine(border, sgrDim+fit(" 時刻", 1+logTimeCells)+fit("役", logRoleCells)+fit("カード", logCardCells)+"出来事"+sgrReset, w))
		shown := max(rows-len(out)-1, 1) // 下の枠の 1 行
		m.set.offset = listnav.WindowOffset(m.set.offset, m.set.cursor, len(l.rows), shown)
		end := min(len(l.rows), m.set.offset+shown)
		for i := min(m.set.offset, end); i < end; i++ {
			out = append(out, boxLine(border, m.logRow(l.rows[i], i == m.set.cursor, w), w))
		}
	}
	return append(out, boxBottom(border, w))
}

// logRow はログの 1 行。出来事の文は残りの幅で切る (畳んだ回数は切らずに行末に残す)。
func (m *Model) logRow(f eventlog.Folded, selected bool, w int) string {
	lead := " "
	if selected {
		lead = sgrCyan + "▌" + sgrFgReset
	}
	tail := ""
	if f.N > 1 {
		tail = sgrDim + fmt.Sprintf("  … ×%d 回、最後 %s (最初 %s)", f.N, f.Last.At.Local().Format("15:04"), f.First.Local().Format("15:04")) + sgrReset
	}
	role := f.Role
	if c, ok := logRoleColors[role]; ok {
		role = fg(c) + fit(role, logRoleCells) + sgrFgReset
	} else {
		role = fit(role, logRoleCells)
	}
	reason := termsafe.PlainLine(f.Last.Reason)
	if c := logReasonColor(f.Last); c != "" {
		reason = c + reason + sgrFgReset
	}
	rest := max(w-2-1-logTimeCells-logRoleCells-logCardCells-termwidth.Of(tail), 10)
	row := fit(f.Last.At.Local().Format("01-02 15:04:05"), logTimeCells) + role + fit(logCard(f), logCardCells) + fit(reason, rest) + tail
	if selected { // 選んでいる行に下線 (途中の色の戻し \x1b[0m で下線も消えるので、戻すたびに引き直す)
		row = sgrUnderline + strings.ReplaceAll(row, sgrReset, sgrReset+sgrUnderline) + sgrNoUnderline
	}
	return lead + row
}

// logText は 1 行をクリップボードへ写す文 (色なし。時刻・役・カード・出来事・畳んだ回数)。
func logText(f eventlog.Folded) string {
	parts := []string{f.Last.At.Local().Format("01-02 15:04:05"), f.Role}
	if c := logCard(f); c != "" {
		parts = append(parts, c)
	}
	parts = append(parts, termsafe.PlainLine(f.Last.Reason))
	s := strings.Join(parts, "  ")
	if f.N > 1 {
		s += fmt.Sprintf("  (×%d 回、最後 %s・最初 %s)", f.N, f.Last.At.Local().Format("15:04"), f.First.Local().Format("15:04"))
	}
	return s
}

// yankLog は選んでいるログの行をクリップボードへ写す。body なら出来事の文だけ (y = 行 / Y = 本文)。
func (m *Model) yankLog(body bool) {
	l := &m.set.log
	if m.set.cursor < 0 || m.set.cursor >= len(l.rows) {
		m.refuse("コピーするログの行が無い")
		return
	}
	f := l.rows[m.set.cursor]
	text, what := logText(f), "ログの行"
	if body {
		text, what = termsafe.PlainLine(f.Last.Reason), "ログの本文"
	}
	if err := m.copy(text); err != nil {
		m.fail("コピーに失敗した: " + err.Error())
		return
	}
	m.done(what + "をクリップボードへコピーした")
}

// logCard はカードの列 (PM・取り込みの係のカードの欄 = PM / INT は役の列で分かるので出さない)。
func logCard(f eventlog.Folded) string {
	if f.Role == eventlog.RolePM || f.Role == eventlog.RoleIntegrator {
		return "-"
	}
	return orDash(f.Last.Card)
}

func foldedCount(rows []eventlog.Folded) int {
	n := 0
	for _, f := range rows {
		n += f.N
	}
	return n
}
