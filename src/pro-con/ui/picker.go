package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"glogx/issues"
	"tuikit/listnav"

	"pro-con/backend"
)

// issue の一覧から選んで「これやって」と依頼する画面 (i で開閉)。issue の読み方 (状態 = ファイルの位置、
// epic/<name>/ の 2 段、next/ の目印) は glogx の issues viewer と同じ glogx/issues に任せる (判定を 2 実装にしない)。
// repo のタブではその repo だけ、global では設定の全 repo。完了 (done) は出さない。

type pickRow struct {
	repo  backend.Repo
	iss   *issues.Issue // 見出し行 (repo / epic) は nil のことがある
	head  string        // repo の見出し ("" でなければ選べない行)
	epic  string        // epic の見出し行ならその名前
	child bool          // epic の子 (字下げして出す)
	kids  []string      // epic の見出し行: 未完了の子のパス
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
	switch k.String() {
	case "ctrl+c":
		return tea.Quit
	case "i", "q", "esc":
		m.picker.open = false
	case "enter":
		m.pickTarget()
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

// pickTarget は選んだ行を依頼の対象にして、補足の入力欄を開く (空のまま Enter でもよい)。
func (m *Model) pickTarget() {
	p := &m.picker
	if p.cursor >= len(p.rows) || !p.rows[p.cursor].selectable() {
		return
	}
	row := p.rows[p.cursor]
	t := &backend.IssueTarget{Epic: row.epic, Children: row.kids}
	if row.iss != nil {
		n, _ := strconv.Atoi(row.iss.Number)
		t.Number, t.Title, t.Path = n, row.iss.Display(), row.iss.Path
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
		var line string
		switch {
		case r.head != "":
			line = sgrBold + fg(51) + " " + r.head + sgrReset
		case r.epic != "":
			title := ""
			if r.iss != nil {
				title = " #" + r.iss.Number + " " + r.iss.Display()
			}
			line = fmt.Sprintf(" ▸ epic %s%s  %s(未完了の子 %d)%s", r.epic, title, sgrDim, len(r.kids), sgrReset)
		default:
			indent := " "
			if r.child {
				indent = "     "
			}
			line = fmt.Sprintf("%s#%s %s  %s%s%s", indent, r.iss.Number, r.iss.Display(), sgrDim, r.iss.Status.String(), sgrReset)
		}
		if i == m.picker.cursor {
			line = fg(202) + "▌" + sgrReset + sgrBold + strings.TrimPrefix(line, " ")
		}
		out = append(out, boxLine(border, line, w))
	}
	if len(m.picker.warns) > 0 {
		out = append(out, boxLine(border, sgrYellow+" 警告: "+m.picker.warns[0]+sgrReset, w))
	}
	return append(out, boxBottom(border, w))
}
