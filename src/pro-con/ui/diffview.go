package ui

// 詳細から D で開く差分の板 (issue 508): 取り込む先 (origin/master) との merge-base から作業ツリーまでの差分を、ファイルごとに畳める
// 全幅の板で読む。本文は dispatcher が裏で集めて store.DiffPath に書いたもの (画面は git を叩かない = 441)。読むのと色付け
// (tuikit/highlight) は開いたときに 1 回だけ、裏の tea.Cmd で回す (毎フレームは描くだけ)。
// 🚨 本文は PG の worktree の中身 = untrusted。dispatcher が termsafe.PlainLine で無害化してから書き、ここは色を足すだけ。

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/highlight"
	"tuikit/layout"
	"tuikit/listnav"

	"pro-con/card"
)

const sgrReverse = "\x1b[7m" // 選んでいるファイルの見出し (見本の形。issue 508)

// diffView は差分の板の状態。
type diffView struct {
	open    bool
	cardID  string
	loading bool
	err     error
	lines   []string // 色付けした本文 (行は置き場の本文と 1 対 1)
	files   []card.DiffFile
	folded  []bool // files と同じ並び。true = 畳んでいる
	cur     int    // 選んでいるファイル (見出しを反転。enter で畳む・J / K で送る)
	sum     card.DiffSummary
	pager   listnav.Pager
}

// diffLoadedMsg は本文を読んで色付けした結果。
type diffLoadedMsg struct {
	cardID string
	lines  []string
	files  []card.DiffFile
	err    error
}

// openDiff は開いているカード (詳細) の差分の板を開く。
func (m *Model) openDiff() tea.Cmd {
	c, ok := m.drawerCardData()
	if !ok {
		return nil
	}
	if c.Progress == nil || c.Progress.Diff == nil || c.Progress.Diff.Path == "" {
		m.refuse(c.ID + ": 差分がまだ無い (worktree が無いか、dispatcher がまだ集めていない)")
		return nil
	}
	m.diff = diffView{open: true, cardID: c.ID, loading: true, sum: *c.Progress.Diff}
	path, id := c.Progress.Diff.Path, c.ID
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return diffLoadedMsg{cardID: id, err: err}
		}
		raw := strings.Split(string(data), "\n")
		return diffLoadedMsg{cardID: id, lines: highlight.Diff(raw), files: card.SplitDiff(raw)}
	}
}

// onDiffLoaded は読み終えた本文を板に入れる (読んでいる間に閉じた・別のカードへ開き直したなら捨てる)。
func (m *Model) onDiffLoaded(msg diffLoadedMsg) {
	if !m.diff.open || m.diff.cardID != msg.cardID {
		return
	}
	m.diff.loading, m.diff.err, m.diff.lines, m.diff.files = false, msg.err, msg.lines, msg.files
	m.diff.folded = make([]bool, len(msg.files))
	m.diff.pager.Reset()
}

// handleDiffKey は板を開いている間のキー。D / q / esc / h / ← で閉じる (開いたキーでも閉じる。docs/glogx-ui-guide.md §3)。
func (m *Model) handleDiffKey(k string) tea.Cmd {
	rows, owner := m.diffRows()
	switch k {
	case "D", "q", "esc", "h", "left":
		m.diff = diffView{}
		return nil
	case "ctrl+c":
		m.diff = diffView{}
		return m.requestQuit()
	case "enter": // 選んでいるファイルを畳む / 開く。見出しを上端へ
		if f := m.diff.cur; f < len(m.diff.folded) {
			m.diff.folded[f] = !m.diff.folded[f]
			m.diffJumpTo(f)
		}
		return nil
	case "z": // 1 つでも開いていれば全部畳む、全部畳んでいれば全部開く
		anyOpen := false
		for _, f := range m.diff.folded {
			anyOpen = anyOpen || !f
		}
		for i := range m.diff.folded {
			m.diff.folded[i] = anyOpen
		}
		m.diff.pager.Reset()
		return nil
	case "J", "K": // 次 / 前のファイルを選び、見出しを上端へ (端では止める)
		if len(m.diff.files) == 0 {
			return nil
		}
		if k == "J" {
			m.diff.cur = min(m.diff.cur+1, len(m.diff.files)-1)
		} else {
			m.diff.cur = max(m.diff.cur-1, 0)
		}
		m.diffJumpTo(m.diff.cur)
		return nil
	}
	if mo := listnav.MotionOf(k); mo != listnav.None {
		n := m.diffBodyRows()
		m.diff.pager.Move(mo, len(rows), n, glideFrames)
		m.diff.pager.Clamp(len(rows), n)
		m.followScroll(rows, owner, n)
		return m.startFrames()
	}
	return nil
}

// followScroll は、選んでいるファイルの行が窓から外れたら、上端の行のファイルを選び直す (送った先で enter が効く)。
func (m *Model) followScroll(rows []diffRow, owner []int, n int) {
	off := m.diff.pager.Offset
	for i := off; i < min(off+n, len(owner)); i++ {
		if owner[i] == m.diff.cur {
			return
		}
	}
	if off < len(owner) {
		m.diff.cur = owner[off]
	}
}

// diffRow は板の 1 行 (ファイルの見出しか本文の行)。
type diffRow struct {
	text   string
	header bool
}

// diffRows は畳みを反映した板の全行と、各行のファイルの番号。
// 見出しの行 (diff --git / index / --- / +++) はファイルの見出しの 1 行に畳む (名前と数はそこに出す)。
func (m *Model) diffRows() ([]diffRow, []int) {
	var rows []diffRow
	var owner []int
	for i, f := range m.diff.files {
		mark := "▾"
		if m.diff.folded[i] {
			mark = "▸"
		}
		name := sgrBold + mark + " " + f.Path + sgrReset
		if i == m.diff.cur {
			name = sgrReverse + sgrBold + mark + " " + f.Path + sgrReset
		}
		rows = append(rows, diffRow{text: fmt.Sprintf("%s  %s+%d%s %s-%d%s", name, fg(46), f.Add, sgrReset, sgrRed, f.Del, sgrReset), header: true})
		owner = append(owner, i)
		if m.diff.folded[i] {
			continue
		}
		inHunk := false
		for j := f.Start + 1; j < f.End && j < len(m.diff.lines); j++ {
			raw := ansi.Strip(m.diff.lines[j])
			inHunk = inHunk || strings.HasPrefix(raw, "@@")
			if !inHunk && (strings.HasPrefix(raw, "index ") || strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "+++ ")) {
				continue
			}
			rows = append(rows, diffRow{text: "  " + m.diff.lines[j]})
			owner = append(owner, i)
		}
	}
	return rows, owner
}

// diffJumpTo はファイル f の見出しを上端へ送る (末尾で送りきれなければ Clamp が収める)。
func (m *Model) diffJumpTo(f int) {
	rows, owner := m.diffRows()
	for i, o := range owner {
		if o == f && rows[i].header {
			m.diff.pager.Stop()
			m.diff.pager.Offset = i
			m.diff.pager.Clamp(len(rows), m.diffBodyRows())
			return
		}
	}
}

// diffBodyRows は本文を出す行数 (板の見出しと罫線の 2 行を引く)。
func (m *Model) diffBodyRows() int { return max(m.drawerRegionRows()-2, 1) }

// overlayDiff は領域 (ヘッダと下端の群のあいだ) を差分の板で置き換える。
func (m *Model) overlayDiff(region []string) []string {
	if !m.diff.open {
		return region
	}
	w := m.width
	sum := m.diff.sum
	title := fmt.Sprintf(" %s%s%s の差分  origin/master との merge-base から作業ツリーまで (未 commit を含む)  %s%d ファイル +%d -%d%s",
		sgrBold, m.diff.cardID, sgrReset, sgrDim, sum.Files, sum.Add, sum.Del, sgrReset)
	out := []string{fit(title, w), fg(240) + strings.Repeat("─", w) + sgrReset}
	n := m.diffBodyRows()
	var body []string
	switch {
	case m.diff.loading:
		body = []string{sgrDim + "  読んでいる…" + sgrReset}
	case m.diff.err != nil:
		body = []string{sgrRed + "  差分の本文を読めない: " + m.diff.err.Error() + sgrReset}
	case len(m.diff.files) == 0:
		body = []string{sgrDim + "  取り込む先との差分は無い (未追跡のファイルは差分に入らない。名前は詳細の「未 commit の変更」)" + sgrReset}
	default:
		rows, _ := m.diffRows()
		if sum.Cut {
			rows = append(rows, diffRow{text: sgrYellow + fmt.Sprintf("  (%d 行で切った。続きは worktree で git diff --merge-base origin/master)", card.DiffMaxLines) + sgrReset})
		}
		m.diff.pager.Clamp(len(rows), n)
		off := m.diff.pager.DrawOffset(len(rows), n)
		win := make([]string, n)
		for i := range win {
			if off+i < len(rows) {
				win[i] = " " + ansi.Truncate(rows[off+i].text, w-1-layout.ScrollbarWidth, "…")
			}
		}
		body = layout.Scrollbar(win, w, len(rows), off, true)
	}
	return layout.PadTo(append(out, body...), len(region))[:len(region)]
}
