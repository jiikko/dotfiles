package ui

// 詳細から D で開く差分の板 (issue 508): 取り込む先 (origin/master) との merge-base から作業ツリーまでの差分を、ファイルごとに畳める
// 全幅の板で読む。本文は dispatcher が裏で集めて store.DiffPath に書いたもの (画面は git を叩かない = 441)。読むのと色付け
// (tuikit/highlight)・板の行への組み立ては開いたときに 1 回だけ、裏の tea.Cmd で回す (毎フレームは窓の行を描くだけ = 529)。
// 🚨 本文は PG の worktree の中身 = untrusted。dispatcher が termsafe.PlainLine で無害化してから書き、ここは色を足すだけ。

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/highlight"
	"tuikit/layout"
	"tuikit/listnav"
	"tuikit/termwidth"

	"pro-con/card"
)

const sgrReverse = "\x1b[7m" // 選んでいるファイルの見出し (見本の形。issue 508)

// diffView は差分の板の状態。
// 板の行は「ファイルの見出し 1 行 + (畳んでいなければ) 本文の行」をファイル順に並べたもの (+ 切った知らせの 1 行)。
// 全行を並べた列は持たない: 描く (毎フレーム) のは窓の数十行だけなので、行 r は starts から引く (issue 529)。
type diffView struct {
	open    bool
	cardID  string
	loading bool
	err     error
	files   []card.DiffFile
	body    [][]string // files と同じ並び。見出しの行 (index / --- / +++) を除いて字下げした本文の行 (読んだときに 1 回だけ作る)
	folded  []bool     // files と同じ並び。true = 畳んでいる
	starts  []int      // files と同じ並び。各ファイルの見出しの行の位置 (畳みを変えたら relayout で作り直す)
	total   int        // 板の行数 (切った知らせの行を含む)
	cur     int        // 選んでいるファイル (見出しを反転。enter で畳む・J / K で送る)
	sum     card.DiffSummary
	base    string // 取り込む先の名前 (origin/master。repo によっては origin/main)
	pager   listnav.Pager
}

// diffLoadedMsg は本文を読んで色付けし、ファイルごとの板の本文に組んだ結果。
type diffLoadedMsg struct {
	cardID string
	files  []card.DiffFile
	body   [][]string
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
	m.diff = diffView{open: true, cardID: c.ID, loading: true, sum: *c.Progress.Diff, base: c.Progress.Base}
	path, id := c.Progress.Diff.Path, c.ID
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return diffLoadedMsg{cardID: id, err: err}
		}
		raw := strings.Split(string(data), "\n")
		files := card.SplitDiff(raw)
		return diffLoadedMsg{cardID: id, files: files, body: diffBody(highlight.Diff(raw), files)}
	}
}

// diffBody はファイルごとの板の本文の行。見出しの行 (diff --git / index / --- / +++) は板のファイルの見出しの 1 行に
// 畳むので除き (名前と数はそこに出す)、残りは字下げする。lines は色付けした本文 (置き場の本文と 1 対 1)。
func diffBody(lines []string, files []card.DiffFile) [][]string {
	body := make([][]string, len(files))
	for i, f := range files {
		inHunk := false
		for j := f.Start + 1; j < f.End && j < len(lines); j++ {
			raw := ansi.Strip(lines[j])
			inHunk = inHunk || strings.HasPrefix(raw, "@@")
			if !inHunk && (strings.HasPrefix(raw, "index ") || strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "+++ ")) {
				continue
			}
			body[i] = append(body[i], "  "+lines[j])
		}
	}
	return body
}

// onDiffLoaded は読み終えた本文を板に入れる (読んでいる間に閉じた・別のカードへ開き直したなら捨てる)。
func (m *Model) onDiffLoaded(msg diffLoadedMsg) {
	if !m.diff.open || m.diff.cardID != msg.cardID {
		return
	}
	m.diff.loading, m.diff.err, m.diff.files, m.diff.body = false, msg.err, msg.files, msg.body
	m.diff.folded = make([]bool, len(msg.files))
	m.diff.relayout()
	m.diff.pager.Reset()
}

// handleDiffKey は板を開いている間のキー。D / q / esc / h / ← で閉じる (開いたキーでも閉じる。docs/glogx-ui-guide.md §3)。
func (m *Model) handleDiffKey(k string) tea.Cmd {
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
			m.diff.relayout()
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
		m.diff.relayout()
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
		m.diff.pager.Move(mo, m.diff.total, n, glideFrames)
		m.diff.pager.Clamp(m.diff.total, n)
		m.followScroll(n)
		return m.startFrames()
	}
	return nil
}

// followScroll は、選んでいるファイルの行が窓 (n 行) から外れたら、上端の行のファイルを選び直す (送った先で enter が効く)。
func (m *Model) followScroll(n int) {
	d := &m.diff
	off := d.pager.Offset
	if off >= d.total || d.cur >= len(d.starts) {
		return
	}
	end := d.total // 選んでいるファイルの行の終わり (次のファイルの見出し。最後のファイルは切った知らせの行まで)
	if d.cur+1 < len(d.starts) {
		end = d.starts[d.cur+1]
	}
	if d.starts[d.cur] < off+n && off < end {
		return
	}
	d.cur = d.fileAt(off)
}

// relayout は畳みを反映して各ファイルの見出しの位置と板の行数を作り直す (O(ファイル数)。畳みを変えたら呼ぶ)。
func (d *diffView) relayout() {
	d.starts = d.starts[:0]
	r := 0
	for i := range d.files {
		d.starts = append(d.starts, r)
		r++
		if !d.folded[i] {
			r += len(d.body[i])
		}
	}
	if d.sum.Cut && r > 0 { // キーの送りと描く行数を揃えるため、知らせも行に含める (末尾まで送れば見える)
		r++
	}
	d.total = r
}

// fileAt は行 r が属するファイル (切った知らせの行は最後のファイル)。
func (d *diffView) fileAt(r int) int {
	return max(sort.Search(len(d.starts), func(i int) bool { return d.starts[i] > r })-1, 0)
}

// rowText は行 r の文字。見出しの行は選んでいるファイル cur と畳みで変わるので、描くときに作る。
func (d *diffView) rowText(r int) string {
	f := d.fileAt(r)
	k := r - d.starts[f] - 1 // ファイルの本文の何行目か (-1 = 見出し)
	switch {
	case k < 0:
		return d.headerText(f)
	case !d.folded[f] && k < len(d.body[f]):
		return d.body[f][k]
	}
	return sgrYellow + fmt.Sprintf("  (%d 行で切った。続きは worktree で git diff --merge-base %s)", card.DiffMaxLines, d.base) + sgrReset
}

// headerText はファイル f の見出しの行 (畳みの印・名前・足した / 消した行の数。選んでいれば反転)。
func (d *diffView) headerText(f int) string {
	mark := "▾"
	if d.folded[f] {
		mark = "▸"
	}
	name := sgrBold + mark + " " + d.files[f].Path + sgrReset
	if f == d.cur {
		name = sgrReverse + name
	}
	return fmt.Sprintf("%s  %s+%d%s %s-%d%s", name, fg(46), d.files[f].Add, sgrReset, sgrRed, d.files[f].Del, sgrReset)
}

// diffJumpTo はファイル f の見出しを上端へ送る (末尾で送りきれなければ Clamp が収める)。
func (m *Model) diffJumpTo(f int) {
	if f >= len(m.diff.starts) {
		return
	}
	m.diff.pager.Stop()
	m.diff.pager.Offset = m.diff.starts[f]
	m.diff.pager.Clamp(m.diff.total, m.diffBodyRows())
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
	cut := ""
	if sum.Cut {
		cut = fmt.Sprintf("  %s(%d 行で切った)%s", sgrYellow, card.DiffMaxLines, sgrReset)
	}
	// 狭いと後ろから切れるので、数と切った印を説明より前に置く
	title := fmt.Sprintf(" %s%s%s の差分  %d ファイル +%d -%d%s  %s%s との merge-base から作業ツリーまで (未 commit を含む)%s",
		sgrBold, m.diff.cardID, sgrReset, sum.Files, sum.Add, sum.Del, cut, sgrDim, m.diff.base, sgrReset)
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
		total := m.diff.total
		m.diff.pager.Clamp(total, n)
		off := m.diff.pager.DrawOffset(total, n)
		win := make([]string, n)
		for i := range win {
			if off+i < total {
				win[i] = " " + termwidth.Truncate(m.diff.rowText(off+i), w-1-layout.ScrollbarWidth, "…")
			}
		}
		body = layout.Scrollbar(win, w, total, off, true)
	}
	return layout.PadTo(append(out, body...), len(region))[:len(region)]
}
