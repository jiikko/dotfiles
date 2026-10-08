package filer

import (
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// Options は Model の外から決めるもの。
type Options struct {
	// Now は「今」。熱の色が今に依存するので、テストと撮影で固定するために注入する。nil なら time.Now
	Now func() time.Time
}

// Result はキーを処理した結果、呼び出し側にしてほしいこと。
type Result int

const (
	// None は何もしない。
	None Result = iota
	// Quit は終わりたい (単体ならプログラムを、glogx なら glogx ごと終える。spec §0.1)。
	Quit
)

// Notice は呼び出し側に toast で出してほしい知らせ。OK=false は失敗 (✗ 赤。glogx-ui-guide §9)。
type Notice struct {
	Text string
	OK   bool
}

type anim struct {
	x, y, alpha tween
	ghost       bool
	w           int
}

// Model は treefiler の画面 1 枚分の状態。
type Model struct {
	root, cur  *node
	w, h       int
	now        func() time.Time
	anims      map[*node]*anim
	order      []*node // 描画順 (生まれた順)
	camX, camY tween
	tiles      []*tile
	showHidden bool
	moving     bool
	lastAdv    time.Time
	notices    []Notice
	linkCache  map[string]string
}

// New は dir を root にした Model を作る。カーソルは root の最初の子 (無ければ root)。
func New(dir string, opts Options) (*Model, error) {
	root, err := newRoot(dir)
	if err != nil {
		return nil, err
	}
	m := &Model{root: root, cur: root, now: opts.Now, anims: map[*node]*anim{}, linkCache: map[string]string{}}
	if m.now == nil {
		m.now = time.Now
	}
	if root.readErr != nil {
		m.fail("読めません: " + root.name)
	}
	if ks := m.kids(root); len(ks) > 0 {
		m.cur = ks[0]
		root.last = ks[0]
	}
	return m, nil
}

// Resize は画面の大きさを伝える (最下行はステータスバー)。
func (m *Model) Resize(w, h int) { m.w, m.h = w, h }

// Animating は動いている途中か (呼び出し側はこの間だけ高い周期で Advance を呼ぶ)。
func (m *Model) Animating() bool { return m.moving }

// OwnsKeys は入力モード中か (glogx の横断キーを譲る判定に使う。spec §0.3)。今は入力欄を持たない。
func (m *Model) OwnsKeys() bool { return false }

// TakeNotices は溜まった知らせを取り出す。
func (m *Model) TakeNotices() []Notice {
	n := m.notices
	m.notices = nil
	return n
}

// fail は失敗の知らせ (押したキーが効かなかった理由。glogx-ui-guide §9 の ✗ 赤)。
func (m *Model) fail(text string) { m.notices = append(m.notices, Notice{Text: text}) }

func (m *Model) canvasH() int { return max(m.h-1, 1) }

// ---------- 時間 ----------

// Advance は now まで動きを進める。まだ動いているかを返す。
func (m *Model) Advance(now time.Time) bool {
	dt := 0.0
	if !m.lastAdv.IsZero() {
		dt = math.Min(now.Sub(m.lastAdv).Seconds(), 0.05)
	}
	m.lastAdv = now
	m.moving = m.step(dt)
	return m.moving
}

func (m *Model) sync(pos map[*node]place) {
	for n := range pos {
		if a, ok := m.anims[n]; ok {
			a.ghost = false
			a.w = labelWidth(n)
		}
	}
	// 生まれる順は親が先 (列順) になるよう、深さの浅い順に足す
	var born []*node
	for n := range pos {
		if _, ok := m.anims[n]; !ok {
			born = append(born, n)
		}
	}
	sortByDepth(born)
	for _, n := range born {
		p := pos[n]
		a := &anim{w: labelWidth(n)}
		a.x.v, a.y.v = float64(p.x), float64(p.y)
		// 新しい項目は、いちばん近いアニメ中の祖先の右端から湧く (spec §4.2)
		for anc := n.parent; anc != nil; anc = anc.parent {
			if pa, ok := m.anims[anc]; ok && !pa.ghost {
				a.x.v, a.y.v = pa.x.v+float64(pa.w), pa.y.v
				break
			}
		}
		if n.parent == nil {
			a.alpha.v = 1
		}
		m.anims[n] = a
		m.order = append(m.order, n)
	}
	for n, a := range m.anims {
		if _, ok := pos[n]; !ok {
			a.ghost = true
		}
	}
}

// sortByDepth は浅い順、同じ深さはパス順 (map の反復順に依らず、描く順を毎回同じにする)。
func sortByDepth(ns []*node) {
	sort.Slice(ns, func(i, j int) bool {
		if di, dj := ns[i].depth(), ns[j].depth(); di != dj {
			return di < dj
		}
		return ns[i].path() < ns[j].path()
	})
}

// ghostTarget は消える項目が縮んでいく先 (いちばん近い生きた祖先の右端)。
func (m *Model) ghostTarget(n *node) (float64, float64) {
	for anc := n.parent; anc != nil; anc = anc.parent {
		if pa, ok := m.anims[anc]; ok && !pa.ghost {
			return pa.x.v + float64(pa.w), pa.y.v
		}
	}
	return 0, 0
}

func (m *Model) step(dt float64) bool {
	pos := m.layout()
	m.sync(pos)
	moving := false
	kept := m.order[:0]
	for _, n := range m.order {
		a := m.anims[n]
		if a == nil {
			continue
		}
		if a.ghost {
			tx, ty := m.ghostTarget(n)
			moving = a.alpha.step(0, fadeOutDur, dt) || moving
			moving = a.x.step(tx, moveDur, dt) || moving
			moving = a.y.step(ty, moveDur, dt) || moving
			if a.alpha.v <= 0.02 {
				delete(m.anims, n)
				continue
			}
		} else {
			p := pos[n]
			moving = a.alpha.step(1, fadeInDur, dt) || moving
			moving = a.x.step(float64(p.x), moveDur, dt) || moving
			moving = a.y.step(float64(p.y), moveDur, dt) || moving
		}
		kept = append(kept, n)
	}
	m.order = kept
	// カメラ: カーソルの名前の左端を画面の 38% に、選択線を画面の縦の中央に (spec §4.5)
	cp := pos[m.cur]
	moving = m.camX.step(float64(cp.x)-float64(m.w)*0.38, camDur, dt) || moving
	moving = m.camY.step(float64(cp.y)-float64(m.canvasH()/2), camDur, dt) || moving
	kt := m.tiles[:0]
	for _, t := range m.tiles {
		target := 1.0
		if t.closing {
			target = 0
		}
		moving = t.open.step(target, popupDur, dt) || moving
		moving = t.sy.step(float64(t.scroll), scrollDur, dt) || moving
		if t.closing && t.open.v <= 0.001 {
			continue
		}
		kt = append(kt, t)
	}
	m.tiles = kt
	return moving
}

// snapAll は動いている途中のものを全部終点へ飛ばす。消えかけの項目と閉じかけのタイルはその場で捨てる。
// キー入力のたびに呼ぶ (初動は速く・終わりはゆっくり・キーが来たら終わりを打ち切って次へ。spec §0.1)。
func (m *Model) snapAll() {
	kept := m.order[:0]
	for _, n := range m.order {
		a := m.anims[n]
		if a == nil {
			continue
		}
		if a.ghost {
			delete(m.anims, n)
			continue
		}
		a.x.snap()
		a.y.snap()
		a.alpha.snap()
		kept = append(kept, n)
	}
	m.order = kept
	m.camX.snap()
	m.camY.snap()
	kt := m.tiles[:0]
	for _, t := range m.tiles {
		if t.closing {
			continue
		}
		t.open.snap()
		t.sy.snap()
		kt = append(kt, t)
	}
	m.tiles = kt
}

// ---------- キー ----------

// HandleKey は 1 打鍵を処理する (キーの表記は bubbletea の KeyPressMsg.String())。
func (m *Model) HandleKey(k string) Result {
	m.snapAll()
	defer func() { m.moving = true }() // 目標が変わったので次の Advance で動き出す
	if k == "ctrl+c" {
		return Quit // どこからでも即終了 (glogx-ui-guide §1)
	}
	if m.frontTile() != nil {
		m.tileKey(k)
		return None
	}
	return m.treeKey(k)
}

func (m *Model) frontTile() *tile {
	if len(m.tiles) == 0 {
		return nil
	}
	return m.tiles[len(m.tiles)-1]
}

func (m *Model) siblings() []*node {
	if m.cur.parent == nil {
		return []*node{m.cur}
	}
	return m.kids(m.cur.parent)
}

// moveSibling は同じ階層 (兄弟) の中で delta だけ動く。端では止まる (spec §0.1: j を押しっぱなしでも潜らない)。
func (m *Model) moveSibling(delta int) {
	s := m.siblings()
	for i, n := range s {
		if n == m.cur {
			m.setCur(s[max(0, min(len(s)-1, i+delta))])
			return
		}
	}
}

func (m *Model) setCur(n *node) {
	m.cur = n
	if n.parent != nil {
		n.parent.last = n
	}
}

func (m *Model) halfPage() int { return max(1, m.canvasH()/4) }

func (m *Model) treeKey(k string) Result {
	// 画面固有の動作キーを先に捌き、残りを listnav.MotionOf へ渡す (glogx-ui-guide §2)。
	// Space / Tab は treebeard の開閉 (spec §0.1 の「o 以外は treebeard で上書き」)
	switch k {
	case "q", "esc", "ctrl+c":
		return Quit
	case "l", "right", "ctrl+f", "enter":
		m.descend()
	case "h", "left", "ctrl+b":
		// ctrl+b を ← の別名にするのは glogx-ui-guide §2 の例外 (pro-con のボードと同じ「← が左の列へ動くだけ」の画面)
		if m.cur.parent != nil {
			m.setCur(m.cur.parent)
		}
	case "space", " ", "tab":
		if m.cur.dir {
			m.ensureLoaded(m.cur)
			m.cur.expanded = !m.cur.expanded
		}
	case "c":
		if m.cur.dir {
			m.cur.expanded = false
		}
	case "C":
		m.collapseOthers()
	case "J":
		m.moveSibling(10)
	case "K":
		m.moveSibling(-10)
	case "-", "backspace":
		m.rerootUp()
	case ".":
		m.showHidden = !m.showHidden
	default:
		switch listnav.MotionOf(k) {
		case listnav.Down:
			m.moveSibling(1)
		case listnav.Up:
			m.moveSibling(-1)
		case listnav.HalfDown:
			m.moveSibling(m.halfPage())
		case listnav.HalfUp:
			m.moveSibling(-m.halfPage())
		case listnav.Top:
			m.setCur(m.siblings()[0])
		case listnav.Bottom:
			s := m.siblings()
			m.setCur(s[len(s)-1])
		case listnav.None:
		}
	}
	return None
}

func (m *Model) ensureLoaded(n *node) {
	if n.dir && !n.loaded {
		n.load()
		if n.readErr != nil {
			m.fail("読めません: " + n.name)
		}
	}
}

// descend はフォルダなら開いて潜り (最後にいた子か先頭へ)、ファイルならタイルを開く。
func (m *Model) descend() {
	if !m.cur.dir {
		m.openTile(m.cur, m.cursorRect())
		return
	}
	m.ensureLoaded(m.cur)
	ks := m.kids(m.cur)
	if len(ks) == 0 {
		return
	}
	m.cur.expanded = true
	next := m.cur.last
	if next == nil || !contains(ks, next) {
		next = ks[0]
	}
	m.setCur(next)
}

// collapseOthers はカーソルの祖先以外を全部畳む (カーソル自身も畳む。treebeard の C。spec §6.1・§10)。
func (m *Model) collapseOthers() {
	var walk func(n *node)
	walk = func(n *node) {
		if n.dir && (!m.onPath(n) || n == m.cur) {
			n.expanded = false
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(m.root)
	m.root.expanded = true
}

// rerootUp は root の親を新しい root にする (- / Backspace。spec §5.7)。
func (m *Model) rerootUp() {
	old := m.root
	parentPath := old.path()
	if i := strings.LastIndexByte(parentPath, '/'); i > 0 {
		parentPath = parentPath[:i]
	} else {
		return // もう / まで来ている
	}
	nr, err := newRoot(parentPath)
	if err != nil {
		m.fail("1 段上へ行けません: " + err.Error())
		return
	}
	// 新しい root の子のうち旧 root と同じ名前の項目を旧 root で置き換える (開いた状態とアニメを保つ)
	found := false
	for i, k := range nr.kids {
		if k.raw == baseName(old.raw) {
			old.raw, old.parent = k.raw, nr
			nr.kids[i] = old
			found = true
		}
	}
	if !found {
		// 親の中に見つからない (消えた / 読めない) なら付け替えない。付け替えると旧 root が孤児になり、カーソルが木の外へ出る
		m.fail("1 段上へ行けません: " + old.name + " が親の中に見つかりません")
		return
	}
	nr.last = old
	m.root = nr
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (m *Model) cursorRect() rect {
	a := m.anims[m.cur]
	if a == nil {
		return rect{m.w / 2, m.canvasH() / 2, 10, 1}
	}
	return rect{int(math.Round(a.x.v-m.camX.v)) - 1, int(math.Round(a.y.v - m.camY.v)), a.w + 2, 1}
}

func (m *Model) openTile(n *node, from rect) {
	if msg := refuse(n); msg != "" {
		m.fail(msg)
		return
	}
	src, err := openText(n.path())
	if err != nil {
		switch {
		case errors.Is(err, errBinary):
			m.fail("バイナリは表示できません: " + n.name)
		case errors.Is(err, errNotRegular):
			m.fail("通常のファイルではないので開けません: " + n.name)
		default:
			m.fail("開けません: " + n.name)
		}
		return
	}
	t := &tile{n: n, src: src, slot: len(m.tiles) % 4, from: from}
	src.ensure(2 * m.tileRows(t))
	m.tiles = append(m.tiles, t)
}

// bottomLoadLines は G で 1 回に読み足す行数の上限。
const bottomLoadLines = 20000

func (m *Model) tileRows(t *tile) int { return max(slotRect(t.slot, m.w, m.canvasH()).h-2, 1) }

// maxScroll はタイルのスクロールの上限 (末尾まで読めていなければ、読めている所まで)。
func (m *Model) maxScroll(t *tile) int { return max(0, len(t.src.lines)-m.tileRows(t)) }

// nextFile は n と同じフォルダの、開けるファイルを delta 方向に探す (タイルの J / K。glogx-ui-guide §6)。
func (m *Model) nextFile(n *node, delta int) *node {
	if n.parent == nil {
		return nil
	}
	s := m.kids(n.parent)
	i := 0
	for j, k := range s {
		if k == n {
			i = j
		}
	}
	for j := i + delta; j >= 0 && j < len(s); j += delta {
		if !s[j].dir && refuse(s[j]) == "" {
			return s[j]
		}
	}
	return nil
}

func (m *Model) tileKey(k string) {
	t := m.frontTile()
	rows := m.tileRows(t)
	if t.jump {
		if m.jumpKey(t, k, rows) {
			return
		}
	}
	switch k {
	// 閉じるキーは glogx-ui-guide §3 (q Esc h ← と、開閉 toggle の Enter)
	case "q", "esc", "h", "left", "enter":
		t.closing = true
		return
	case "tab":
		t.src.ensure(t.scroll + 2*rows)
		ls := m.tileLinks(t, t.scroll+rows)
		for i, l := range ls {
			if l.line >= t.scroll {
				t.jump, t.sel = true, i
				return
			}
		}
		m.fail("この範囲に開けるパスがありません")
		return
	case "J", "K", "shift+down", "shift+up":
		delta := 1
		if k == "K" || k == "shift+up" {
			delta = -1
		}
		m.replaceTile(t, delta)
		return
	}
	switch listnav.MotionOf(k) {
	case listnav.Down:
		t.scroll++
	case listnav.Up:
		t.scroll--
	case listnav.HalfDown:
		t.scroll += listnav.Half(rows)
	case listnav.HalfUp:
		t.scroll -= listnav.Half(rows)
	case listnav.Top:
		t.scroll = 0
	case listnav.Bottom:
		// 末尾へ飛んでも全部は読まない (巨大なログで固まる)。今より bottomLoadLines 行先まで読み、読めた所の末尾へ。
		// まだ続きがあれば位置表示に + が付き、もう一度 G で先へ進む
		t.src.ensure(len(t.src.lines) + bottomLoadLines)
		t.scroll = m.maxScroll(t)
	case listnav.None:
	}
	t.src.ensure(t.scroll + 2*rows)
	t.scroll = max(0, min(t.scroll, m.maxScroll(t)))
}

// jumpKey はジャンプモードのキー。捌いたら true (捌かないキーはモードを抜けてからタイルのキーとして効く。glogx-ui-guide §6)。
func (m *Model) jumpKey(t *tile, k string, rows int) bool {
	ls := m.tileLinks(t, len(t.src.lines))
	if len(ls) == 0 {
		t.jump = false
		return false
	}
	t.sel = max(0, min(t.sel, len(ls)-1))
	switch {
	case k == "tab" || listnav.MotionOf(k) == listnav.Down:
		t.sel = (t.sel + 1) % len(ls)
	case k == "shift+tab" || listnav.MotionOf(k) == listnav.Up:
		t.sel = (t.sel - 1 + len(ls)) % len(ls)
	case listnav.MotionOf(k) == listnav.Top:
		t.sel = 0
	case listnav.MotionOf(k) == listnav.Bottom:
		t.sel = len(ls) - 1
	case k == "enter":
		l := ls[t.sel]
		t.jump = false
		r := slotRect(t.slot, m.w, m.canvasH())
		from := rect{r.x + 2 + l.col, r.y + 1 + l.line - t.scroll, widthOf(l.text), 1}
		m.openTile(m.nodeFor(l.path), from)
		return true
	case k == "esc" || k == "q" || k == "h" || k == "left":
		t.jump = false
		return true
	default:
		t.jump = false
		return false
	}
	// 選んだリンクが見えるようにスクロールする
	if l := ls[t.sel].line; l < t.scroll {
		t.scroll = l
	} else if l >= t.scroll+rows {
		t.scroll = l - rows + 1
	}
	return true
}

// nodeFor はリンク先のパスの項目。木の中にあればそれを、無ければ単独の項目を作る (木は変えない)。
func (m *Model) nodeFor(path string) *node {
	rootPath := m.root.path()
	if rel, ok := strings.CutPrefix(path, rootPath+"/"); ok {
		cur := m.root
		for part := range strings.SplitSeq(rel, "/") {
			m.ensureLoaded(cur)
			var next *node
			for _, k := range cur.kids {
				if k.raw == part {
					next = k
				}
			}
			if next == nil {
				break
			}
			cur = next
		}
		if cur.path() == path {
			return cur
		}
	}
	info, _ := os.Lstat(path) // 消えていても項目は作る (開くときに「開けません」になる)
	n := newNode(baseName(path), info, nil)
	n.raw = path // root の外のファイル。parent が無いので path() は raw をそのまま返す
	return n
}

// replaceTile は手前のタイルを開いたまま同じフォルダの隣のファイルへ差し替える。木のカーソルも一緒に動かす。
func (m *Model) replaceTile(t *tile, delta int) {
	if t.n.parent == nil {
		// ジャンプで開いた root の外のファイル。隣を辿る木が無い
		m.fail("このファイルは木の外にあるので隣へ送れません")
		return
	}
	n := m.nextFile(t.n, delta)
	for n != nil {
		src, err := openText(n.path())
		if err == nil {
			if m.cur == t.n {
				m.setCur(n)
			}
			t.n, t.src, t.links, t.scroll, t.sy, t.jump = n, src, nil, 0, tween{}, false
			src.ensure(2 * m.tileRows(t))
			return
		}
		n = m.nextFile(n, delta) // バイナリは飛ばす
	}
	if delta > 0 {
		m.fail("これが最後のファイルです")
	} else {
		m.fail("これが最初のファイルです")
	}
}

// ---------- 描画 ----------

// View は今の画面 (w×h、最下行はステータスバー) を行ごとに返す。
func (m *Model) View() []string {
	if m.w <= 0 || m.h <= 0 {
		return nil
	}
	return m.draw().lines()
}

func (m *Model) draw() *canvas {
	c := newCanvas(m.w, m.h)
	ch := m.canvasH()
	ox, oy := int(math.Round(m.camX.v)), int(math.Round(m.camY.v))
	pos := m.layout()
	now := map[*node][2]int{}
	for _, n := range m.order {
		if a := m.anims[n]; a != nil && !a.ghost && a.alpha.v > 0.05 {
			now[n] = [2]int{int(math.Round(a.x.v)), int(math.Round(a.y.v))}
		}
	}
	// カーソル: 名前の背景を glogx のカーソル色で塗るだけ (spec §0.1)
	if p, ok := now[m.cur]; ok && p[1]-oy < ch {
		c.fillBg(p[0]-1-ox, p[1]-oy, p[0]+labelWidth(m.cur)-ox, p[1]-oy, cCursorBg)
	}
	m.drawLines(c, now, ox, oy, ch)
	m.drawNames(c, pos, ox, oy, ch)
	for i, t := range m.tiles {
		front := i == len(m.tiles)-1
		p := math.Max(0, math.Min(1, t.open.v))
		r := lerpRect(t.from, slotRect(t.slot, m.w, ch), p)
		if front {
			c.dim(r, 0.55*p)
		}
		m.drawTile(c, t, r, front, p)
	}
	m.statusBar(c)
	return c
}

func (m *Model) drawLines(c *canvas, now map[*node][2]int, ox, oy, ch int) {
	ageNow := m.now()
	for k, lc := range m.lines(now) {
		x, y := k[0]-ox, k[1]-oy
		if y >= ch {
			continue
		}
		p := c.at(x, y)
		if p == nil {
			continue
		}
		sap := heat(ageNow.Sub(lc.owner.mtime).Seconds())
		var col rgb
		switch lc.emph {
		case emRoute:
			col = mix(cAccRoute, sap, 0.18)
		case emActive:
			col = mix(cAccAct, sap, 0.42)
		case emDim:
			col = mix(cAccDim, mix(sap, cBg, 0.55), 0.35)
		}
		p.s, p.fg = doubleGlyph[lc.mask&15], col
	}
}

func (m *Model) drawNames(c *canvas, pos map[*node]place, ox, oy, ch int) {
	ageNow := m.now()
	for pass := range 2 { // 消えかけの項目を先に、生きている項目を後に描く
		for _, n := range m.order {
			a := m.anims[n]
			if a == nil || (pass == 0) != a.ghost {
				continue
			}
			x, y := int(math.Round(a.x.v))-ox, int(math.Round(a.y.v))-oy
			if y < 0 || y >= ch {
				continue
			}
			base := heat(ageNow.Sub(n.mtime).Seconds())
			switch {
			case m.onPath(n):
				base = cRouteTxt
			case pos[n].active:
			default:
				base = mix(base, cBg, 0.32) // 選択線から外れた枝 (spec §1.4 の off_line。focus_dim 既定 6)
			}
			c.put(x, y, n.label(), mix(cBg, base, a.alpha.v), n.dir || m.onPath(n))
			if a.ghost {
				continue
			}
			bx := x + a.w + 1
			switch {
			case n.dir && !n.expanded && (!n.loaded || len(m.kids(n)) > 0):
				c.put(bx, y, "›", mix(cBg, base, a.alpha.v*0.55), false)
			case !n.dir && n.git != 0:
				c.put(bx, y, string(n.git), mix(cBg, gitColor[n.git], a.alpha.v), true)
			}
		}
	}
}

func (m *Model) drawTile(c *canvas, t *tile, r rect, front bool, p float64) {
	if r.w < 4 || r.h < 2 {
		return
	}
	c.clear(r.x, r.y, r.x+r.w-1, r.y+r.h-1, cPop)
	bc, tc := mix(cPop, cAccRoute, p), cText
	if !front {
		bc, tc = mix(cPop, cAccAct, p), cMuted
	}
	inner := r.w - 2
	c.put(r.x, r.y, "╭"+strings.Repeat("─", inner)+"╮", bc, false)
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		c.put(r.x, y, "│", bc, false)
		c.put(r.x+r.w-1, y, "│", bc, false)
	}
	c.put(r.x, r.y+r.h-1, "╰"+strings.Repeat("─", inner)+"╯", bc, false)
	title := termwidth.Truncate(fmt.Sprintf(" %s · %s ", t.n.name, human(t.n.size)), max(inner-2, 1), "…")
	c.put(r.x+2, r.y, title, tc, true)
	if p < 0.9 {
		return // 開き切る前は枠だけ (spec §4.6)
	}
	rows := r.h - 2
	sy := int(math.Round(t.sy.v))
	lw := r.w - 4
	var links []link
	if t.jump && front {
		links = m.tileLinks(t, sy+rows)
	}
	if len(t.src.lines) == 0 {
		c.put(r.x+2, r.y+1, "(空のファイル)", cMuted, false)
	}
	for i := 0; i < rows && sy+i < len(t.src.lines); i++ {
		ln := sy + i
		c.put(r.x+2, r.y+1+i, termwidth.Truncate(t.src.lines[ln], lw, ""), cText, false)
		for li, l := range links {
			if l.line != ln {
				continue
			}
			for k := range widthOf(l.text) {
				if l.col+k >= lw {
					break
				}
				if q := c.at(r.x+2+l.col+k, r.y+1+i); q != nil {
					q.ul = true
					q.rev = li == t.sel
				}
			}
		}
	}
	total := strconv.Itoa(len(t.src.lines))
	if !t.src.eof {
		total += "+" // まだ末尾まで読んでいない
	}
	posLabel := fmt.Sprintf(" %d/%s ", sy+1, total)
	if t.jump && front {
		posLabel = " tab: パスを選ぶ · enter: 重ねて開く · esc: 戻る " + posLabel
	}
	c.put(r.x+r.w-2-widthOf(posLabel), r.y+r.h-1, posLabel, cMuted, false)
	if maxS := m.maxScroll(t); maxS > 0 && len(t.src.lines) > 0 {
		th := max(1, rows*rows/len(t.src.lines))
		ty := int(float64(rows-th) * float64(min(sy, maxS)) / float64(maxS))
		for y := range rows {
			g, col := "│", cAccDim
			if y >= ty && y < ty+th {
				g, col = "┃", cAccRoute
			}
			c.put(r.x+r.w-1, r.y+1+y, g, col, false)
		}
	}
}

func human(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}

func ago(s float64) string {
	switch {
	case s < 60:
		return fmt.Sprintf("%ds ago", int(s))
	case s < 3600:
		return fmt.Sprintf("%dm ago", int(s/60))
	case s < 86400:
		return fmt.Sprintf("%dh ago", int(s/3600))
	case s < 365*86400:
		return fmt.Sprintf("%dd ago", int(s/86400))
	}
	return fmt.Sprintf("%.1fy ago", s/(365*86400))
}

type seg struct {
	s    string
	c    rgb
	bold bool
}

// statusBar は最下行 (spec §3.4)。左にパンくず、右に件数・大きさ・熱の点と経過時間・凡例・キーの案内。
func (m *Model) statusBar(c *canvas) {
	y := m.h - 1
	c.clear(0, y, m.w-1, y, cBar)
	var chain []*node
	for n := m.cur; n != nil; n = n.parent {
		chain = append([]*node{n}, chain...)
	}
	age := m.now().Sub(m.cur.mtime).Seconds()
	meta := human(m.cur.size)
	if m.cur.dir {
		meta = "folder"
		if m.cur.loaded {
			meta = fmt.Sprintf("%d items", len(m.kids(m.cur)))
		}
	}
	right := []seg{{meta + " · ", cMuted, false}, {"● ", heat(age), false}, {ago(age), cText, false}, {"   ", cMuted, false}}
	legend := []seg{{"now ", cMuted, false}}
	for _, st := range ember {
		legend = append(legend, seg{"▮", st.c, false})
	}
	legend = append(legend, seg{" old   ", cMuted, false})
	hints := []seg{{"q", cAccRoute, true}, {" quit ", cMuted, false}}
	if m.frontTile() != nil {
		hints = []seg{{"tab", cAccRoute, true}, {" link  ", cMuted, false}, {"q", cAccRoute, true}, {" close ", cMuted, false}}
	}
	segW := func(ss []seg) int {
		w := 0
		for _, s := range ss {
			w += widthOf(s.s)
		}
		return w
	}
	crumbW := 1
	for i, n := range chain {
		if i > 0 {
			crumbW += 3
		}
		crumbW += widthOf(n.name)
	}
	// 幅が足りなければ凡例から落とす。抜ける手段 (q) は残す (glogx-ui-guide §5)
	all := append(append(append([]seg{}, right...), legend...), hints...)
	if crumbW+2+segW(all) > m.w {
		all = append(append([]seg{}, right...), hints...)
	}
	if crumbW+2+segW(all) > m.w {
		all = hints
	}
	x := c.put(0, y, " ", cMuted, false)
	room := m.w - segW(all) - 2
	for i, n := range chain {
		if x >= room {
			break
		}
		if i > 0 {
			x = c.put(x, y, " › ", cAccAct, false)
		}
		col, bold := cMuted, false
		if i == len(chain)-1 {
			col, bold = cText, true
		}
		x = c.put(x, y, termwidth.Truncate(n.name, max(room-x, 1), "…"), col, bold)
	}
	rx := m.w - segW(all)
	for _, s := range all {
		rx = c.put(rx, y, s.s, s.c, s.bold)
	}
}

func widthOf(s string) int { return termwidth.Of(s) }

func decodeRune(s string) (rune, int) { return utf8.DecodeRuneInString(s) }
