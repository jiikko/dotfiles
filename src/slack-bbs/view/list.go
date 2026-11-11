package view

import (
	"fmt"
	"slices"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/board"
	"slack-bbs/render"
)

// row は左の一覧の 1 行。日付の見出しか、話題 (page.Topics の添字) のどちらか。
type row struct {
	topic int       // 話題の添字。見出しは -1
	day   time.Time // 見出しの日 (その日の 0 時。未分類の見出しは零値)
}

func (r row) header() bool { return r.topic < 0 }

// buildRows は話題を最後の発言の日 (新しい日が上) で組み、日ごとに見出しを入れる。同じ日の中は板の並び
// (要対応 → 必読 → 新しい順。board.Assemble) を保つ。未分類は日をまたいで集めた話題なので、日の見出しの下に入れず、最後に別の見出しで出す。
func buildRows(ts []board.Topic, loc *time.Location) []row {
	idx := make([]int, 0, len(ts))
	var other []int
	for i, t := range ts {
		if t.Unclassified {
			other = append(other, i)
			continue
		}
		idx = append(idx, i)
	}
	day := func(i int) time.Time { return dayOf(board.TsTime(ts[i].Last()), loc) }
	slices.SortStableFunc(idx, func(a, b int) int { return day(b).Compare(day(a)) })
	var rows []row
	for k, i := range idx {
		if d := day(i); k == 0 || !d.Equal(day(idx[k-1])) {
			rows = append(rows, row{topic: -1, day: d})
		}
		rows = append(rows, row{topic: i})
	}
	if len(other) > 0 {
		rows = append(rows, row{topic: -1})
		for _, i := range other {
			rows = append(rows, row{topic: i})
		}
	}
	return rows
}

func dayOf(t time.Time, loc *time.Location) time.Time {
	y, mo, d := t.In(loc).Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, loc)
}

var weekdays = [...]string{"日", "月", "火", "水", "木", "金", "土"}

// dayLabel は見出しの文 (「11/10 (月) 今日」)。今日・昨日は描くときの now で決める (日をまたいで開いたままでも合う)。
func dayLabel(r row, now time.Time) string {
	if r.day.IsZero() {
		return "未分類"
	}
	s := fmt.Sprintf("%s (%s)", r.day.Format("01/02"), weekdays[r.day.Weekday()])
	switch today := dayOf(now, r.day.Location()); {
	case r.day.Equal(today):
		s += " 今日"
	case r.day.Equal(today.AddDate(0, 0, -1)):
		s += " 昨日"
	}
	return s
}

func (m *Model) rows() []row { return m.chans[m.cur].rows }

// fitList はカーソルと窓を一覧に収め、見出しに乗ったカーソルを話題へ逃がす (dir の向きを先に探す)。
func (m *Model) fitList(dir int) {
	rows := m.rows()
	m.list.Fit(len(rows), m.listRows())
	if len(rows) == 0 || !rows[m.list.Cursor].header() {
		return
	}
	for _, d := range []int{dir, -dir} {
		for i := m.list.Cursor + d; i >= 0 && i < len(rows); i += d {
			if !rows[i].header() {
				m.list.Cursor = i
				// 窓は動かさない (上へ逃がしたときに見出しが窓に残る)。収まらなくなったときだけ Fit が直す
				m.list.Fit(len(rows), m.listRows())
				return
			}
		}
	}
}

// leftLines は話題の一覧のカード (落ち影込み)。行の頭は 1 桁目が選んだ行の印 (桃色の ▌)、2 桁目が未読の印 (桃色の ●)。
// 選んだ行は地も塗るが、色の無い端末 (NO_COLOR) では地が消えるので、印の桁でも選んだ行を見分けさせる。
func (m *Model) leftLines() []string {
	ts := m.topics()
	rows := m.rows()
	m.fitList(1)
	c := m.chans[m.cur]
	w := m.leftWidth() - cardChrome(cardPadX(m.w))
	var out []string
	end := min(m.list.Offset+m.listRows(), len(rows))
	for i := m.list.Offset; i < end; i++ {
		r := rows[i]
		if r.header() {
			out = append(out, accent(termwidth.Truncate(dayLabel(r, m.now()), w, "…")))
			continue
		}
		t := ts[r.topic]
		cur, dot := " ", " "
		if i == m.list.Cursor {
			cur = pink("▌")
		}
		unread := topicUnread(t, c.read, c.mentions)
		if unread {
			dot = pink("●")
		}
		count := fmt.Sprintf(" %d", render.Count(t))
		text := firstMark(t) + " " + termsafe.PlainLine(t.Title)
		room := max(w-3-termwidth.Of(count), 1)
		text = termwidth.Truncate(text, room, "…")
		pad := termwidth.PadSpaces(room - termwidth.Of(text))
		if unread || i == m.list.Cursor {
			text = boldText(text)
		}
		line := cur + dot + " " + text + pad + dim(count)
		if i == m.list.Cursor {
			bg := cSelIdle
			if !m.body {
				bg = cSel
			}
			line = paintRow(line, w, bg, surfList)
		}
		out = append(out, line)
	}
	edge := edgeListOn
	if m.body {
		edge = edgeOff
	}
	return shadow(card(out, m.leftWidth(), m.cardRows(), cardPadX(m.w), surfList, edge))
}

// firstMark は一覧に出す印 1 つ (要対応 → 必読 → 状態の順で最初に当てはまるもの)。どの印も表示幅は 2。
func firstMark(t board.Topic) string {
	switch {
	case t.Has(board.KindAction):
		return "🔴"
	case t.Has(board.KindMustRead):
		return "📌"
	case t.Unclassified:
		return "⚪"
	case t.State == board.StateDecided:
		return "🟢"
	case t.State == board.StateOpen:
		return "🟡"
	}
	return "⚪"
}
