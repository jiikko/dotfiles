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
	// owner は話題を持つタブの番号 + 1。0 は今のタブ (チャンネルのタブの行)。新着のタブの行はタブをまたぐので持ち主を持つ
	// (番号 + 1 にして、零値を「今のタブ」にする: チャンネルのタブの行は購読の並べ替えをまたいで覚えるので、番号を持たせない)
	owner int
	label string // 見出しの文 (新着のタブの「未読」「既読」)。空なら日の見出し
}

func (r row) header() bool { return r.topic < 0 }

// buildRows は話題を最後の発言の日 (新しい日が上) で組み、日ごとに見出しを入れる。同じ日の中は板の並び
// (要対応 → 必読 → 新しい順。board.Assemble) を保つ。未分類は日をまたいで集めた話題なので、日の見出しの下に入れず、最後に別の見出しで出す。
// skip が真の話題 (隠した話題) は出さない。
func buildRows(ts []board.Topic, loc *time.Location, skip func(int) bool) []row {
	idx := make([]int, 0, len(ts))
	var other []int
	for i, t := range ts {
		if skip != nil && skip(i) {
			continue
		}
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
	if r.label != "" {
		return r.label
	}
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

func (m *Model) rows() []row {
	if m.stream {
		if m.streamRows == nil {
			m.streamRows = m.buildStream()
		}
		return m.streamRows
	}
	c := &m.chans[m.cur]
	if c.rows == nil && c.page != nil {
		ts := c.page.Topics
		c.rows = buildRows(ts, time.Local, func(i int) bool { return !m.showHidden && topicHidden(ts[i], c.hidden) })
		if c.rows == nil {
			c.rows = []row{} // 全部を隠したとき: nil は「作り直す」の印なので、空の行を覚える (描くたびに数え直さない)
		}
	}
	return c.rows
}

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
	rows := m.rows()
	m.fitList(1)
	w := m.leftWidth() - cardChrome(cardPadX(m.w))
	var out []string
	if len(rows) == 0 {
		head, hint := m.emptyList()
		out = emptyBlock(head, hint, w, m.listRows())
	}
	end := min(m.list.Offset+m.listRows(), len(rows))
	for i := m.list.Offset; i < end; i++ {
		r := rows[i]
		if r.header() {
			out = append(out, accent(termwidth.Truncate(dayLabel(r, m.now()), w, "…")))
			continue
		}
		c := &m.chans[m.ownerOf(r)]
		t := c.page.Topics[r.topic]
		cur, dot := " ", " "
		if i == m.list.Cursor {
			cur = pink("▌")
		}
		hidden := topicHidden(t, c.hidden)
		unread := !hidden && topicUnread(t, c.read, c.mentions) // 隠した話題は未読に数えない (タブの未読の数と揃える)
		if unread {
			dot = pink("●")
		}
		count := fmt.Sprintf(" %d", render.Count(t))
		text := firstMark(t) + " " + termsafe.PlainLine(t.Title)
		if m.stream {
			text = firstMark(t) + " " + c.ch.Label() + " " + termsafe.PlainLine(t.Title) // どのタブの話題か
			if n := newCount(t, c.read, c.mentions); unread && n > 0 {
				count = fmt.Sprintf(" +%d", n) // 前に読んだ後に増えた他の人の発言
			}
		}
		room := max(w-3-termwidth.Of(count), 1)
		text = termwidth.Truncate(text, room, "…")
		pad := termwidth.PadSpaces(room - termwidth.Of(text))
		switch {
		case hidden:
			text = dim(text) // z で出している隠した話題
		case unread || i == m.list.Cursor:
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

// firstMark は一覧に出す印 2 つ: 1 つ目は 緊急 → 要対応 → 必読 → 状態 の順で最初に当てはまるもの、2 つ目はテーマ。どの印も表示幅は 2。
func firstMark(t board.Topic) string {
	return stateIcon(t) + render.ThemeIcon(t)
}

func stateIcon(t board.Topic) string {
	switch {
	case t.Urgent:
		return render.UrgentMark
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

// emptyList は一覧に出す話題が無いときに、一覧の真ん中に出す見出しと案内 (案内は区切りのよい所で行を分けておく)。
// 読み込み中は、前に読んだ板の話題を全部隠していて一覧が空のときも「読み込み中」と出す。話題を全部隠しただけのときは、隠したことには触れず、
// 話題が無いときの文言を出す (ユーザーの決定 2026-11-11: 隠した話題は無いものとして扱う。z で出せることはキーの案内にある)。
func (m *Model) emptyList() (head string, hint []string) {
	if m.stream {
		oldest := time.Time{}
		for _, c := range m.chans {
			if c.loading && (oldest.IsZero() || c.started.Before(oldest)) {
				oldest = c.started
			}
		}
		if !oldest.IsZero() {
			return m.spin(oldest) + " 読み込み中", []string{"読み終わったタブから、", "ここに話題が並びます"}
		}
		return "📭 新着はありません", []string{"どのタブにも", "期間内の話題がありません"}
	}
	c := m.chans[m.cur]
	switch {
	case c.loading:
		return m.spin(c.started) + " 読み込み中", []string{"読み終わると、", "ここに話題が並びます"}
	case c.page == nil && c.err != nil:
		return "🚨 読めませんでした", []string{"r で読み直す"}
	case c.page == nil:
		return "まだ読んでいません", []string{"r で読む"}
	case len(c.page.Topics) == 0:
		return "📭 話題はありません", []string{"期間内の発言が", "ありません"}
	}
	return "📭 話題はありません", []string{"新しい話題が来ると、", "ここに並びます"}
}

// emptyBlock は見出しと案内 (淡い色) を、幅 w・rows 行の欄の真ん中に置いた行。欄に収まらない行は折り返す (切ると案内が読めない)。
func emptyBlock(head string, hint []string, w, rows int) []string {
	lines := wrap(head, w)
	heads := len(lines)
	for _, h := range hint {
		lines = append(lines, wrap(h, w)...)
	}
	top := max((rows-len(lines))/2, 0)
	out := make([]string, top, top+len(lines)) // 上の余白の空行
	for i, l := range lines {
		l = termwidth.PadSpaces(max((w-termwidth.Of(l))/2, 0)) + l
		if i >= heads {
			l = dim(l)
		}
		out = append(out, l)
	}
	return out
}
