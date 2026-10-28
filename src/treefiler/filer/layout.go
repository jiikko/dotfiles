package filer

import "sort"

// layout.go は木の配置 (spec §3) と線の格子 (spec §2.1)。配置は「目標位置」で、アニメはそこへ向かう (anim.go)。

type place struct {
	x, y   int
	active bool // 親が spine 上 (spec §1.4 の Placed.active)
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
		if !m.isKidOf(n, next) {
			next = ks[0]
		}
		s[next] = true
		n = next
	}
	return s
}

// colEntry は次の列に子のブロックを置く親と、その目標の行。
type colEntry struct {
	n *node
	y int
}

// layout は表示するノードの目標位置を返す (列ごとに、親ごとの子のブロックを縦に詰める。spec §3.1・§3.3 の capped)。
// 巨大なブロックは画面の近くの子だけを入れる (virt.go)。仮想化しない配置 (layoutWith(false)) は検査の正解役 (column_test.go・virt_test.go) が使う。
// 🚨 返す map は使い回しの入れ物で、次の layout の呼び出しで中身が変わる。呼び出し側は受け取った関数の中だけで使い、
// 持ち越さない (5 万件のフォルダで、毎 tick の作り直しが確保の大半だった)。
func (m *Model) layout() map[*node]place { return m.layoutWith(true) }

func (m *Model) layoutWith(virt bool) map[*node]place {
	m.layoutSeq++
	m.resetVirt()
	sp := m.spine()
	spKid := map[*node]*node{} // spine 上の親 → spine 上の子
	for n := range sp {
		if n.parent != nil {
			spKid[n.parent] = n
		}
	}
	pos := m.posBuf.take(len(m.anims) + 1)
	pos[m.root] = place{0, 0, true}
	var parents []colEntry
	if m.root.dir && m.root.expanded && len(m.kids(m.root)) > 0 {
		parents = append(parents, colEntry{m.root, 0})
	}
	namew := nameCells(m.root)
	colX := 0
	stride := 1 + m.set.RowSpacing // 行の間隔は空行を足す (spec §3.1。線は空行を貫く)
	for depth := 0; ; depth++ {
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
		sort.Slice(parents, func(i, j int) bool { return parents[i].y < parents[j].y })
		y0 := make([]int, len(parents))
		pin := -1
		for j, p := range parents {
			ks := m.kids(p.n)
			y0[j] = p.y - (len(ks)-1)/2
			if k := spKid[p.n]; sp[p.n] && m.isKidOf(p.n, k) {
				pin, y0[j] = j, -k.vidx
			}
		}
		blen := func(j int) int { return len(m.kids(parents[j].n)) }
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
			if y0[j]+blen(j)-1 < p.y {
				ups++
			} else if y0[j] > p.y {
				downs++
			}
		}
		lanes := max(1, min(max(w/3, 2), max(ups, downs)))
		// 次の列の x = 列の x + 列の幅 + lanes + gap (3 以上) + branch (spec §3.2)
		nx := colX + w + lanes + max(m.set.ColumnGap, 3) + m.set.BranchOffset
		colX = nx
		var next []colEntry
		namew = 0
		for j, p := range parents {
			v := m.visOf(p.n)
			namew = max(namew, v.maxName)
			act := sp[p.n]
			m.noteGeo(p.n, v, y0[j], stride, nx)
			big := m.virtualizes(p.n, len(v.kids))
			if big {
				p.n.virtSeq = m.layoutSeq // 配置の仕方 (仮想化しない配置・全部を動かす期間) に依らず履歴を続ける (敵対レビュー 2 周目)
			}
			if virt && big && !m.inHold(&p.n.openHold) {
				next = m.placeVirtual(pos, p.n, v, y0[j], stride, nx, act, spKid[p.n], next)
				continue
			}
			for i, k := range v.kids {
				y := (y0[j] + i) * stride
				pos[k] = place{nx, y, act}
				if k.dir && k.expanded && len(m.kids(k)) > 0 {
					next = append(next, colEntry{k, y})
				}
			}
		}
		parents = next
	}
	m.posBuf.fit(len(pos))
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
	w := n.labelCells()
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
func glyph(style string, lc lineCell) string {
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

// lineGrid はセルの位置から線の部品へ。セルは値で持つ: 毎フレーム作る格子でセルごとに確保しない (ポインタで持つと
// 実物の src を上下に動かす 1 打で約 300KB、確保の過半がここだった。tools/tfprobe の move)。
type lineGrid map[[2]int]lineCell

func (g lineGrid) add(x, y, bits int, emph emphasis, owner *node, thick bool) {
	k := [2]int{x, y}
	c := g[k]
	c.mask |= bits
	if thick {
		c.thick |= bits
	}
	if emph >= c.emph {
		c.emph, c.owner = emph, owner
	}
	g[k] = c
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

// linesInto は「今描いている位置」(アニメ中の位置) から線を引く (spec §4.2: 線はアニメ中の位置から毎フレーム引く)。
// ylo..yhi は画面に出る行 (木の座標)。その外の線は作らない: 5 万件のフォルダで 1 フレーム 25ms かかり、線の格子が半分を占めた
// (実測 2026-10-09)。縦線は 1 行外まで残す (端のセルの継ぎ目の形は隣のセルのビットで決まる)。
// 格子は g (空) に作る。描画は Model の gridBuf (reusedMap) の map を毎フレーム使い回す。
func (m *Model) linesInto(g lineGrid, now map[*node][2]int, ylo, yhi int) lineGrid {
	sp := m.spine()
	for _, pn := range m.order {
		pp, ok := now[pn]
		if !ok || !pn.dir || !pn.expanded {
			continue
		}
		kp := m.kpBuf[:0]
		var routeKid *node
		lk := m.lineKids(pn)
		for _, k := range lk {
			if q, ok := now[k]; ok {
				kp = append(kp, q)
				if sp[k] && sp[pn] {
					routeKid = k
				}
			}
		}
		m.kpBuf = kp // 伸ばした分を次の親・次のフレームで使い回す
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
		if _, ok := m.virt[pn]; ok {
			blockThick = m.blockThick(pn) // 仮想化したブロックは全部の子で数える (全部を動かしていれば全部が now に居る)
		} else {
			for _, k := range lk {
				if _, ok := now[k]; ok && !m.thinBranch(k) {
					blockThick = true
					break
				}
			}
		}
		if a, b, ok := clip(y0, y1); ok {
			g.vseg(bar, a, b, emph, pn, blockThick)
		}
		for _, k := range lk {
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

// lineKids は線を引く子 (仮想化したブロックはアニメを持つ子だけ。virt.go の collectDrawn)。
func (m *Model) lineKids(pn *node) []*node {
	if b, ok := m.virt[pn]; ok {
		return b.drawn
	}
	return m.kids(pn)
}

// reusedMap は毎 tick 作り直さない map の入れ物。take は中身を空にして返す。🚨 Go の map は clear しても容量が残り、反復は
// 容量に比例するので、要る数が容量の 1/4 を切ったら作り直す (巨大なフォルダを見た後に小さなフォルダへ移ると、毎 tick 空の大きな
// map を舐めることになる)。
type reusedMap[K comparable, V any] struct {
	m   map[K]V
	cap int
}

// fit は入れ物に実際に入った数を覚える (take の見込みより多く入れると map は膨らむので、作り直しの判定をその数で行う。
// 検索の layoutAll が 5 万件を入れた後、膨らんだ map を毎フレーム舐め続けた)。
func (r *reusedMap[K, V]) fit(n int) { r.cap = max(r.cap, n) }

func (r *reusedMap[K, V]) take(n int) map[K]V {
	if r.m == nil || n > r.cap || n*4 < r.cap {
		r.cap = max(n, 16)
		r.m = make(map[K]V, r.cap)
		return r.m
	}
	clear(r.m)
	return r.m
}
