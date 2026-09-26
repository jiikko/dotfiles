package ui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"glogx/issues"
	"tuikit/listnav"

	"pro-con/backend"
	"pro-con/card"
)

// issue の一覧から選んで「これやって」と依頼する画面 (i で開閉)。issue の読み方 (状態 = ファイルの位置、
// epic/<name>/ の 2 段、next/ の目印) は glogx の issues viewer と同じ glogx/issues に任せる (判定を 2 実装にしない)。
// repo のタブではその repo だけ、global では設定の全 repo。完了 (done) は出さない。
// 既にカードになっている issue には、紐づいたカードの ID と列を札で出し、Enter で足す前に確かめる (issue 537。見た目は 2026-09-27 に
// 人が見本から選んだ形: 番号と題名の間に列の色の地の札、完了は薄く [完了 C-xxx]、確かめるのは一覧の下の 1 行の y/N)。
// 突き合わせるのは card.Card.Issues (repo + 番号) だけで、題名の番号から推さない。完了のカードは記録 (Snapshot) に在る間だけ出る。

type pickRow struct {
	repo  backend.Repo
	iss   *issues.Issue // 見出し行 (repo / epic) は nil のことがある
	head  string        // repo の見出し ("" でなければ選べない行)
	epic  string        // epic の見出し行ならその名前
	child bool          // epic の子 (字下げして出す)
	kids  []string      // epic の見出し行: 未完了の子のパス
	nums  []int         // epic の見出し行: 未完了の子の番号 (カードの突き合わせ用)
}

// number は行の issue の番号 (issue の無い見出しは 0)。
func (r pickRow) number() int {
	if r.iss == nil {
		return 0
	}
	n, _ := strconv.Atoi(r.iss.Number)
	return n
}

// issueKey は issue とカードを突き合わせる鍵 (card.IssueRef の Status は表示用なので入れない)。
type issueKey struct {
	repo string
	num  int
}

// linkedCard は issue に紐づいたカード 1 枚 (一覧の札に出すもの)。
type linkedCard struct {
	id    string
	state card.State
}

// issueLinks は issue ごとに紐づいたカードを集める。完了でないカードを先に、それぞれ記録の順で並べる。
// 片付けた (Archived) カードも記録に在る間は含める (完了のカードとして薄く出す)。
func issueLinks(cards []card.Card) map[issueKey][]linkedCard {
	links := map[issueKey][]linkedCard{}
	for _, done := range []bool{false, true} {
		for _, c := range cards {
			if (c.State == card.Done) != done || c.Deleting() {
				continue
			}
			for _, ref := range c.Issues {
				k := issueKey{ref.Repo, ref.Number}
				if !slices.ContainsFunc(links[k], func(l linkedCard) bool { return l.id == c.ID }) {
					links[k] = append(links[k], linkedCard{c.ID, c.State})
				}
			}
		}
	}
	return links
}

// linked は行の issue に紐づいたカード。
func (m *Model) linked(r pickRow) []linkedCard {
	if r.iss == nil {
		return nil
	}
	return m.issueCards[issueKey{r.repo.Name, r.number()}]
}

// kidsWithCards は epic の見出し行の、完了でないカードがある子の数。
func (m *Model) kidsWithCards(r pickRow) int {
	n := 0
	for _, num := range r.nums {
		if slices.ContainsFunc(m.issueCards[issueKey{r.repo.Name, num}], func(l linkedCard) bool { return l.state != card.Done }) {
			n++
		}
	}
	return n
}

// cardTags は札の並び (列の色の地に黒字。完了は薄く [完了 C-xxx])。
func cardTags(links []linkedCard) string {
	tags := make([]string, 0, len(links))
	for _, l := range links {
		if l.state == card.Done {
			tags = append(tags, sgrDim+"[完了 "+l.id+"]"+sgrReset)
			continue
		}
		tags = append(tags, bg(stateColor(l.state))+fg(16)+" "+l.id+" "+l.state.Label()+" "+sgrReset)
	}
	return strings.Join(tags, " ")
}

// cardList は確かめの文に出すカードの並び (C-090 (作業中)・C-070 (完了))。
func cardList(links []linkedCard) string {
	parts := make([]string, len(links))
	for i, l := range links {
		parts[i] = l.id + " (" + l.state.Label() + ")"
	}
	return strings.Join(parts, "・")
}

func (r pickRow) selectable() bool { return r.head == "" && (r.iss != nil || r.epic != "") }

type picker struct {
	open   bool
	rows   []pickRow
	cursor int
	offset int // 表示の窓の先頭 (listnav.WindowOffset)
	warns  []string
	target *backend.IssueTarget // Enter で選んだもの (補足の入力中)
	repo   backend.Repo
	dupe   string // 既にカードがある issue を選んだときの確かめの文 (空でなければ y/N を待っている)
}

// loadPicker は今のタブの repo の issue を読む。
func (m *Model) loadPicker() {
	var repos []backend.Repo
	for _, r := range m.repos {
		if m.tab == "" || r.Name == m.tab {
			repos = append(repos, r)
		}
	}
	p := picker{open: true}
	for _, r := range repos {
		dirs := issues.FindDirs(r.Path)
		if len(dirs) == 0 {
			continue
		}
		list, warns := issues.Scan(dirs)
		p.warns = append(p.warns, warns...)
		rows := pickRows(r, list)
		if len(rows) == 0 {
			continue
		}
		if m.tab == "" {
			p.rows = append(p.rows, pickRow{repo: r, head: r.Name})
		}
		p.rows = append(p.rows, rows...)
	}
	m.picker = p
	m.pickerMove(0)
}

// pickRows は 1 つの repo の未完了の issue を、epic ごとに見出しを付けて並べる (epic の見出しは親 issue の行)。
func pickRows(r backend.Repo, list []*issues.Issue) []pickRow {
	var plain []*issues.Issue
	epics := map[string][]*issues.Issue{}
	var epicNames []string
	for _, iss := range list {
		if iss.Status == issues.StatusDone || iss.Number == "" {
			continue
		}
		_ = iss.LoadMeta() // タイトル (H1)。読めなければファイル名の slug で出す
		if iss.GroupKind == issues.GroupEpic {
			if _, ok := epics[iss.Group]; !ok {
				epicNames = append(epicNames, iss.Group)
			}
			epics[iss.Group] = append(epics[iss.Group], iss)
			continue
		}
		plain = append(plain, iss)
	}
	sort.Strings(epicNames)
	var rows []pickRow
	for _, name := range epicNames {
		kids := epics[name]
		head := pickRow{repo: r, epic: name}
		var children []pickRow
		for _, k := range kids {
			if k.Number == name { // group 名と同じ番号の issue が親 (glogx の issues_view.go の groupHead と同じ規則)
				head.iss = k
				continue
			}
			head.kids = append(head.kids, k.Path)
			n, _ := strconv.Atoi(k.Number)
			head.nums = append(head.nums, n)
			children = append(children, pickRow{repo: r, iss: k, child: true})
		}
		rows = append(append(rows, head), children...)
	}
	for _, iss := range plain {
		rows = append(rows, pickRow{repo: r, iss: iss})
	}
	return rows
}

// pickerMove は選べる行だけを渡り歩く (見出しの行は飛ばす)。
func (m *Model) pickerMove(delta int) {
	p := &m.picker
	if len(p.rows) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	i := max(0, min(p.cursor+delta, len(p.rows)-1))
	for i >= 0 && i < len(p.rows) && !p.rows[i].selectable() {
		i += step
	}
	if i < 0 || i >= len(p.rows) { // 端の見出しにぶつかった: 逆向きに探す
		i = max(0, min(p.cursor, len(p.rows)-1))
		for i >= 0 && i < len(p.rows) && !p.rows[i].selectable() {
			i -= step
		}
	}
	if i >= 0 && i < len(p.rows) {
		p.cursor = i
	}
}

func (m *Model) handlePickerKey(k tea.KeyPressMsg) tea.Cmd {
	if m.picker.dupe != "" && k.String() != "ctrl+c" {
		// y だけで足す (Enter は通さない: 選んだ Enter を 2 度押しただけで確かめを越えないように)
		m.picker.dupe = ""
		if k.String() == "y" {
			m.pickTarget()
		} else {
			m.info("足していない (一覧に戻った)")
		}
		return nil
	}
	switch k.String() {
	case "ctrl+c":
		return m.requestQuit()
	case "i", "q", "esc":
		m.picker.open = false
	case "enter":
		m.pickOrAsk()
	default:
		switch listnav.MotionOf(k.String()) {
		case listnav.Down:
			m.pickerMove(1)
		case listnav.Up:
			m.pickerMove(-1)
		case listnav.HalfDown:
			m.pickerMove(listnav.Half(m.pickerRowsShown()))
		case listnav.HalfUp:
			m.pickerMove(-listnav.Half(m.pickerRowsShown()))
		case listnav.Top:
			m.picker.cursor = 0
			m.pickerMove(0)
		case listnav.Bottom:
			m.picker.cursor = len(m.picker.rows) - 1
			m.pickerMove(0)
		case listnav.None:
		}
	}
	return nil
}

// pickOrAsk は選んだ行の issue に既にカードがあれば足す前に確かめ (y/N)、無ければそのまま pickTarget へ進む。
// epic の見出しは、親の issue のカードに加えて、完了でないカードがある子も確かめる (epic ごと足すと子の作業と重なる)。
func (m *Model) pickOrAsk() {
	p := &m.picker
	if p.cursor >= len(p.rows) || !p.rows[p.cursor].selectable() {
		return
	}
	row := p.rows[p.cursor]
	var why []string
	if links := m.linked(row); len(links) > 0 {
		name := "#" + row.iss.Number
		if row.epic != "" {
			name = "epic " + row.epic
		}
		why = append(why, fmt.Sprintf("%s には既にカード %s がある", name, cardList(links)))
	}
	if n := m.kidsWithCards(row); n > 0 {
		why = append(why, fmt.Sprintf("epic %s の子 %d 件にカードがある", row.epic, n))
	}
	if len(why) == 0 {
		m.pickTarget()
		return
	}
	p.dupe = strings.Join(why, "。") + "。それでも足す? y/N"
}

// pickTarget は選んだ行を依頼の対象にして、補足の入力欄を開く (空のまま Enter でもよい)。
func (m *Model) pickTarget() {
	p := &m.picker
	if p.cursor >= len(p.rows) || !p.rows[p.cursor].selectable() {
		return
	}
	row := p.rows[p.cursor]
	t := &backend.IssueTarget{Epic: row.epic, Children: row.kids}
	if row.iss != nil {
		t.Number, t.Title, t.Path = row.number(), row.iss.Display(), row.iss.Path
	}
	p.target, p.repo = t, row.repo
	m.picker.open = false
	m.startInput(inputIssue)
}

func (m *Model) pickerRowsShown() int { return max(1, m.height-12) }

// pickerBlock は一覧の板。
func (m *Model) pickerBlock() []string {
	w := m.width
	border := fg(202)
	scope := "global (設定の全 repo)"
	if m.tab != "" {
		scope = m.tab
	}
	out := []string{boxTop(border, sgrBold+"issue を選んで依頼する — "+scope+sgrReset, w)}
	if len(m.picker.rows) == 0 {
		out = append(out, boxLine(border, sgrDim+" 未完了の issue が無い (完了したものは出さない)"+sgrReset, w))
	}
	shown := m.pickerRowsShown()
	m.picker.offset = listnav.WindowOffset(m.picker.offset, m.picker.cursor, len(m.picker.rows), shown)
	start := m.picker.offset
	for i := start; i < min(len(m.picker.rows), start+shown); i++ {
		r := m.picker.rows[i]
		bold := "" // 札の後ろで太字を戻す (札の地は全リセットで閉じる)
		if i == m.picker.cursor {
			bold = sgrBold
		}
		tags := ""
		if t := cardTags(m.linked(r)); t != "" {
			tags = t + " " + bold
		}
		var line string
		switch {
		case r.head != "":
			line = sgrBold + fg(51) + " " + r.head + sgrReset
		case r.epic != "":
			title := ""
			if r.iss != nil {
				title = " #" + r.iss.Number + " " + tags + r.iss.Display()
			}
			kids := fmt.Sprintf("未完了の子 %d", len(r.kids))
			if n := m.kidsWithCards(r); n > 0 {
				kids += fmt.Sprintf(" · カードあり %d", n)
			}
			line = fmt.Sprintf(" ▸ epic %s%s  %s(%s)%s", r.epic, title, sgrDim, kids, sgrReset)
		default:
			indent := " "
			if r.child {
				indent = "     "
			}
			line = fmt.Sprintf("%s#%s %s%s  %s%s%s", indent, r.iss.Number, tags, r.iss.Display(), sgrDim, r.iss.Status.String(), sgrReset)
		}
		if i == m.picker.cursor {
			line = fg(202) + "▌" + sgrReset + sgrBold + strings.TrimPrefix(line, " ")
		}
		out = append(out, boxLine(border, line, w))
	}
	if m.picker.dupe != "" {
		out = append(out, boxLine(border, " "+sgrBold+fg(214)+m.picker.dupe+sgrReset, w))
	}
	if len(m.picker.warns) > 0 {
		out = append(out, boxLine(border, sgrYellow+" 警告: "+m.picker.warns[0]+sgrReset, w))
	}
	return append(out, boxBottom(border, w))
}
