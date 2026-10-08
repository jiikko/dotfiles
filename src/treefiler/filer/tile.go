package filer

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// tile.go はプレビューのタイル (spec §0.2)。ドラクエの対戦画面のように手前へ重なる。置き場所は 4 箇所で順に使い、
// 5 枚目は 1 枚目の場所に重なる。枚数の上限は無い。

type link struct {
	line, col int    // 本文の行と、その行の中の表示桁
	text      string // 本文に書かれた文字列
	path      string // 解決した実在のファイル
}

type tile struct {
	n       *node
	src     *textSource
	slot    int
	open    tween
	closing bool
	from    rect // 育ち始める矩形 (1 枚目は木のカーソル行、2 枚目以降は選んだリンク)
	scroll  int
	sy      tween
	jump    bool
	sel     int
	links   map[int][]link // 行ごとのリンク (計算済みの行だけ)
}

// slotRect は i 番目の置き場所 (spec §0.2)。1 枚目は中央、2〜4 枚目は中央から右下へ同じ幅ずつずらす。
func slotRect(i, w, h int) rect {
	tw, th := w*60/100, h*70/100
	cx, cy := (w-tw)/2, (h-th)/2
	dx := (w - tw - 2 - cx) / 3
	dy := (h - th - 1 - cy) / 3
	return rect{cx + i*dx, cy + i*dy, tw, th}
}

func lerpRect(a, b rect, t float64) rect {
	l := func(p, q int) int { return int(float64(p) + (float64(q)-float64(p))*t + 0.5) }
	return rect{l(a.x, b.x), l(a.y, b.y), l(a.w, b.w), l(a.h, b.h)}
}

// pathDelims はリンクの候補の切れ目 (空白と、パスの前後に来やすい記号)。
func pathDelim(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	return strings.ContainsRune("()[]{}<>\"'`,;|=", r)
}

// linksOf は行の中から、実在するファイルへ解決できるパスだけを拾う。本文の表記は root か、そのファイルのフォルダからの相対。
// 末尾の . : と `:12` (行番号) は外して解決する。
func linksOf(line string, ln int, root, base string, cache map[string]string) []link {
	var out []link
	i := 0
	for i < len(line) {
		r, size := decodeRune(line[i:])
		if pathDelim(r) {
			i += size
			continue
		}
		j := i
		for j < len(line) {
			r2, s2 := decodeRune(line[j:])
			if pathDelim(r2) {
				break
			}
			j += s2
		}
		tok := line[i:j]
		if p := resolveLink(tok, root, base, cache); p != "" {
			out = append(out, link{line: ln, col: widthOf(line[:i]), text: trimLinkTail(tok), path: p})
		}
		i = j
	}
	return out
}

func trimLinkTail(tok string) string {
	tok = strings.TrimRight(tok, ".:")
	if k := strings.LastIndex(tok, ":"); k > 0 && isAllDigits(tok[k+1:]) {
		tok = tok[:k]
	}
	return tok
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

func resolveLink(tok, root, base string, cache map[string]string) string {
	t := trimLinkTail(tok)
	if t == "" || t == "." || t == ".." || !strings.ContainsAny(t, "/.") {
		return ""
	}
	if p, ok := cache[t]; ok {
		return p
	}
	found := ""
	for _, dir := range []string{root, base} {
		p := t
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, t)
		}
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			found = p
			break
		}
	}
	cache[t] = found
	return found
}

// tileLinks は読めている行のうち lines 番目までのリンク (計算済みは使い回す)。
func (m *Model) tileLinks(t *tile, upto int) []link {
	if t.links == nil {
		t.links = map[int][]link{}
	}
	var out []link
	base := filepath.Dir(t.n.path())
	for ln := 0; ln < upto && ln < len(t.src.lines); ln++ {
		ls, ok := t.links[ln]
		if !ok {
			ls = linksOf(t.src.lines[ln], ln, m.root.path(), base, m.linkCache)
			t.links[ln] = ls
		}
		out = append(out, ls...)
	}
	return out
}

func (m *Model) frontTile() *tile {
	if len(m.tiles) == 0 {
		return nil
	}
	return m.tiles[len(m.tiles)-1]
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
	n.raw, n.abs = path, path // root の外のファイル (parent が無い)
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
