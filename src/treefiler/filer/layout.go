package filer

import "sort"

// layout.go は木の配置 (spec §3) と線の格子 (spec §2.1)。配置は「目標位置」で、アニメはそこへ向かう (anim.go)。

type place struct {
	x, y   int
	active bool // 親が spine 上 (spec §1.4 の Placed.active)
}

// kids は表示する子 (dotfile は隠す。ただしカーソルへの経路にあるものは見せる。spec §5.5)。
// hasKids は n に見える子がいるか (kids と同じ判定。描画で毎フレーム呼ぶので、スライスを作らずに数える)。
func (m *Model) hasKids(n *node) bool {
	for _, k := range n.kids {
		if m.showHidden || !k.hidden() || m.onPath(k) {
			return true
		}
	}
	return false
}

func (m *Model) kids(n *node) []*node {
	if m.showHidden {
		return n.kids
	}
	out := make([]*node, 0, len(n.kids))
	for _, k := range n.kids {
		if !k.hidden() || m.onPath(k) {
			out = append(out, k)
		}
	}
	return out
}

func (m *Model) onPath(n *node) bool {
	for p := m.cur; p != nil; p = p.parent {
		if p == n {
			return true
		}
	}
	return false
}

// spine は root からカーソルまでと、カーソルが開いたフォルダなら「最後にいた子 / 先頭の子」を辿った続き。
// spine は全部 y=0 に並ぶ (固定の選択線。spec §3.1)。
func (m *Model) spine() map[*node]bool {
	s := map[*node]bool{}
	for n := m.cur; n != nil; n = n.parent {
		s[n] = true
	}
	n := m.cur
	for n.dir && n.expanded {
		ks := m.kids(n)
		if len(ks) == 0 {
			break
		}
		next := n.last
		if next == nil || !contains(ks, next) {
			next = ks[0]
		}
		s[next] = true
		n = next
	}
	return s
}

func contains(ns []*node, n *node) bool {
	for _, k := range ns {
		if k == n {
			return true
		}
	}
	return false
}

// layout は表示する全ノードの目標位置を返す (列ごとに、親ごとの子のブロックを縦に詰める。spec §3.1・§3.3 の capped)。
func (m *Model) layout() map[*node]place {
	sp := m.spine()
	pos := map[*node]place{m.root: {0, 0, true}}
	colX := map[int]int{0: 0}
	cols := map[int][]*node{0: {m.root}}
	for depth := 0; ; depth++ {
		var parents []*node
		namew := 0
		for _, n := range cols[depth] {
			namew = max(namew, nameCells(n))
			if n.dir && n.expanded && len(m.kids(n)) > 0 {
				parents = append(parents, n)
			}
		}
		if m.set.Columns == "equal" {
			namew = labelMax // equal = どの列も Column width (spec §3.2)
		}
		m.setNameWidth(depth, namew)
		w := namew
		if dw := m.detailsWidth(); dw > 0 {
			w += 2 + dw
		}
		if len(parents) == 0 {
			break
		}
		sort.Slice(parents, func(i, j int) bool { return pos[parents[i]].y < pos[parents[j]].y })
		y0 := make([]int, len(parents))
		pin := -1
		for j, p := range parents {
			ks := m.kids(p)
			y0[j] = pos[p].y - (len(ks)-1)/2
			if sp[p] {
				for i, k := range ks {
					if sp[k] {
						pin, y0[j] = j, -i
					}
				}
			}
		}
		blen := func(j int) int { return len(m.kids(parents[j])) }
		if pin >= 0 {
			for j := pin - 1; j >= 0; j-- {
				y0[j] = min(y0[j], y0[j+1]-1-blen(j))
			}
			for j := pin + 1; j < len(parents); j++ {
				y0[j] = max(y0[j], y0[j-1]+blen(j-1)+1)
			}
		} else {
			for j := 1; j < len(parents); j++ {
				y0[j] = max(y0[j], y0[j-1]+blen(j-1)+1)
			}
		}
		ups, downs := 0, 0
		for j, p := range parents {
			py := pos[p].y
			if y0[j]+blen(j)-1 < py {
				ups++
			} else if y0[j] > py {
				downs++
			}
		}
		lanes := max(1, min(max(w/3, 2), max(ups, downs)))
		// 次の列の x = 列の x + 列の幅 + lanes + gap (3 以上) + branch (spec §3.2)
		nx := colX[depth] + w + lanes + max(m.set.ColumnGap, 3) + m.set.BranchOffset
		colX[depth+1] = nx
		for j, p := range parents {
			for i, k := range m.kids(p) {
				pos[k] = place{nx, (y0[j] + i) * (1 + m.set.RowSpacing), sp[p]} // 行の間隔は空行を足す (spec §3.1。線は空行を貫く)
				cols[depth+1] = append(cols[depth+1], k)
			}
		}
	}
	return pos
}

// labelWidth は項目の表示幅。詳細を出していれば、列の最長の名前の後ろに右揃えで置くので、列の中で同じ幅になる
// (spec §3.4 の name details: `pad = namew - 名前幅 + 2 + dw - details幅`)。
func (m *Model) labelWidth(n *node) int {
	dw := m.detailsWidth()
	if dw == 0 {
		return nameCells(n)
	}
	return m.nameWidth(n) + 2 + dw
}

// nameCells は名前の表示幅。フォルダは後ろの `/` の 1 桁を含む (ファイルと見分けるため。ユーザー回答 2026-10-09)。
func nameCells(n *node) int {
	w := widthOf(n.label())
	if n.dir {
		w++
	}
	return w
}

// nameWidth は n の列の名前の幅 (layout が数えた最長。数えていない列 (消えていく途中など) なら自分の名前の幅)。
func (m *Model) nameWidth(n *node) int {
	own := nameCells(n)
	if d := n.depth(); d < len(m.nameW) {
		return max(m.nameW[d], own)
	}
	return own
}

func (m *Model) setNameWidth(depth, w int) {
	if depth == 0 {
		m.nameW = m.nameW[:0] // layout の度に数え直す (毎フレーム呼ばれるので slice は使い回す)
	}
	for len(m.nameW) <= depth {
		m.nameW = append(m.nameW, 0)
	}
	m.nameW[depth] = w
}

// ---------- 線 (spec §2.1 の double) ----------

const (
	dirUp    = 1
	dirDown  = 2
	dirLeft  = 4
	dirRight = 8
)

// lineGlyphs は線の形ごとの、mask (上下左右のビット) から文字の表 (spec §2.1)。
var lineGlyphs = map[string][]string{
	"rounded": {"·", "│", "│", "│", "─", "╯", "╮", "┤", "─", "╰", "╭", "├", "─", "┴", "┬", "┼"},
	"square":  {"·", "│", "│", "│", "─", "┘", "┐", "┤", "─", "└", "┌", "├", "─", "┴", "┬", "┼"},
	"heavy":   {"·", "┃", "┃", "┃", "━", "┛", "┓", "┫", "━", "┗", "┏", "┣", "━", "┻", "┳", "╋"},
	"double":  {"·", "║", "║", "║", "═", "╝", "╗", "╣", "═", "╚", "╔", "╠", "═", "╩", "╦", "╬"},
	"ascii":   {".", "|", "|", "|", "-", "+", "+", "+", "-", "+", "+", "+", "-", "+", "+", "+"},
}

// tubeGlyph は rounded / square で経路の横線を二重の「管」にした文字 (spec §2.1)。
var tubeGlyph = map[int]string{15: "╪", 13: "╧", 14: "╤", 11: "╞", 9: "╘", 10: "╒", 7: "╡", 5: "╛", 6: "╕"}

// mixedGlyph は heavy / double で、片方の軸だけが細いセルの文字 (spec §2.1 の MIXED)。[0] は縦太・横細、[1] は縦細・横太。
var mixedGlyph = map[string][2][]string{
	"heavy": {
		{"·", "┃", "┃", "┃", "─", "┚", "┒", "┨", "─", "┖", "┎", "┠", "─", "┸", "┰", "╂"},
		{"·", "│", "│", "│", "━", "┙", "┑", "┥", "━", "┕", "┍", "┝", "━", "┷", "┯", "┿"},
	},
	"double": {
		{"·", "║", "║", "║", "─", "╜", "╖", "╢", "─", "╙", "╓", "╟", "─", "╨", "╥", "╫"},
		{"·", "│", "│", "│", "═", "╛", "╕", "╡", "═", "╘", "╒", "╞", "═", "╧", "╤", "╪"},
	},
}

// glyph は線の 1 セルの文字。
func glyph(style string, lc *lineCell) string {
	m := lc.mask & 15
	if mx, ok := mixedGlyph[style]; ok && m&^lc.thick != 0 {
		// git が無視する枝だけが通る軸は細線に落とす (spec §2.1 の thin)
		axis := func(bits int) bool { return m&bits&lc.thick != 0 || m&bits == 0 }
		v, h := axis(dirUp|dirDown), axis(dirLeft|dirRight)
		switch {
		case v && !h:
			return mx[0][m]
		case !v && h:
			return mx[1][m]
		case !v && !h:
			return lineGlyphs["square"][m]
		}
	}
	tube := lc.emph == emRoute && m&(dirLeft|dirRight) != 0
	switch {
	case tube && (style == "rounded" || style == "square"):
		if g, ok := tubeGlyph[m]; ok {
			return g
		}
		return "═"
	case tube && style == "ascii":
		if m&(dirUp|dirDown) == 0 {
			return "="
		}
		return "+"
	}
	gs, ok := lineGlyphs[style]
	if !ok {
		gs = lineGlyphs["double"]
	}
	return gs[m]
}

type emphasis int

const (
	emDim emphasis = iota
	emActive
	emRoute
)

type lineCell struct {
	mask  int
	thick int // 太い線 (git が無視するものでない枝) が寄与したビット
	emph  emphasis
	owner *node // その枝が生える親 (線の色の元。spec §1.4 の sap)
}

type lineGrid map[[2]int]*lineCell

func (g lineGrid) add(x, y, bits int, emph emphasis, owner *node, thick bool) {
	k := [2]int{x, y}
	c := g[k]
	if c == nil {
		c = &lineCell{}
		g[k] = c
	}
	c.mask |= bits
	if thick {
		c.thick |= bits
	}
	if emph >= c.emph {
		c.emph, c.owner = emph, owner
	}
}

func (g lineGrid) hseg(a, b, y int, emph emphasis, owner *node, thick bool) {
	if a > b {
		a, b = b, a
	}
	for x := a; x <= b; x++ {
		bits := 0
		if x > a {
			bits |= dirLeft
		}
		if x < b {
			bits |= dirRight
		}
		if bits != 0 {
			g.add(x, y, bits, emph, owner, thick)
		}
	}
}

func (g lineGrid) vseg(x, a, b int, emph emphasis, owner *node, thick bool) {
	if a > b {
		a, b = b, a
	}
	for y := a; y <= b; y++ {
		bits := 0
		if y > a {
			bits |= dirUp
		}
		if y < b {
			bits |= dirDown
		}
		if bits != 0 {
			g.add(x, y, bits, emph, owner, thick)
		}
	}
}

// thinBranch は n の枝を細線にするか (git が無視するものを沈めているとき。spec §2.1 の thin)。
func (m *Model) thinBranch(n *node) bool {
	return m.set.DimIgnored && m.gitState(n) == gitIgn
}

// lines は「今描いている位置」(アニメ中の位置) から線を引く (spec §4.2: 線はアニメ中の位置から毎フレーム引く)。
// ylo..yhi は画面に出る行 (木の座標)。その外の線は作らない: 5 万件のフォルダで 1 フレーム 25ms かかり、線の格子が半分を占めた
// (監査で実測 2026-10-09。issue 693)。縦線は 1 行外まで残す (端のセルの継ぎ目の形は隣のセルのビットで決まる)。
func (m *Model) lines(now map[*node][2]int, ylo, yhi int) lineGrid {
	sp := m.spine()
	g := lineGrid{}
	for _, pn := range m.order {
		pp, ok := now[pn]
		if !ok || !pn.dir || !pn.expanded {
			continue
		}
		var kp [][2]int
		var routeKid *node
		for _, k := range m.kids(pn) {
			if q, ok := now[k]; ok {
				kp = append(kp, q)
				if sp[k] && sp[pn] {
					routeKid = k
				}
			}
		}
		if len(kp) == 0 {
			continue
		}
		emph := emDim
		if sp[pn] {
			emph = emActive
		}
		minX, y0, y1 := kp[0][0], kp[0][1], kp[0][1]
		for _, q := range kp {
			minX, y0, y1 = min(minX, q[0]), min(y0, q[1]), max(y1, q[1])
		}
		if max(y1, pp[1]) < ylo-1 || min(y0, pp[1]) > yhi+1 {
			continue // ブロックごと画面の外
		}
		clip := func(a, b int) (int, int, bool) {
			if a > b {
				a, b = b, a
			}
			a, b = max(a, ylo-1), min(b, yhi+1)
			return a, b, a <= b
		}
		start := pp[0] + m.labelWidth(pn) + 1
		bar := minX - 1 - m.set.BranchOffset
		if bar < start {
			continue // まだ展開の途中
		}
		blockThick := false // 縦棒と肘は、ブロックに無視されていない子が 1 つでもあれば太い
		for _, k := range m.kids(pn) {
			if _, ok := now[k]; ok && !m.thinBranch(k) {
				blockThick = true
				break
			}
		}
		if a, b, ok := clip(y0, y1); ok {
			g.vseg(bar, a, b, emph, pn, blockThick)
		}
		for _, k := range m.kids(pn) {
			q, ok := now[k]
			if !ok || q[1] < ylo || q[1] > yhi {
				continue
			}
			e := emph
			if k == routeKid {
				e = emRoute
			}
			thick := !m.thinBranch(k)
			if q[0]-1 > bar {
				g.hseg(bar, q[0]-1, q[1], e, pn, thick)
			} else {
				g.add(bar, q[1], dirRight, e, pn, thick) // 枝の長さ 0 は分かれ目が名前に接する
			}
		}
		py := pp[1]
		je := emph
		if routeKid != nil {
			je = emRoute
		}
		level := py >= y0 && py <= y1
		if level {
			g.hseg(start, bar, py, je, pn, blockThick)
		} else {
			tx := max(bar-1, start)
			g.hseg(start, tx, py, je, pn, blockThick)
			if a, b, ok := clip(py, y0); ok {
				g.vseg(tx, a, b, je, pn, blockThick)
			}
			g.hseg(tx, bar, y0, je, pn, blockThick)
		}
		if routeKid != nil {
			ty := y0
			if level {
				ty = py
			}
			if a, b, ok := clip(ty, now[routeKid][1]); ok {
				g.vseg(bar, a, b, emRoute, pn, !m.thinBranch(routeKid))
			}
		}
	}
	return g
}
