package filer

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/jiikko/dotfiles/src/tuikit/gutter"
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
	links   map[int][]link // 表示の行ごとのリンク (計算済みの行だけ)
	stale   bool           // ジャンプ中にリンクの解決が古くなった (forgetLinks)。ジャンプを抜けたら links を捨てる
	reread  bool           // ジャンプ中にライブ更新でファイルが書き換わった (liveTiles)。ジャンプを抜けたら開き直す
	failed  string         // ライブ更新で開き直せなかった理由 (同じ理由を変わるたびに出し直さない)
	dated   bool           // diff を出している間にライブ更新で本文が変わった。d で本体へ戻すときに開き直す
	lost    bool           // tileLinks が作り直した並びで、選んでいたリンクを見失った (tileKey が次の打鍵を捨てる)
	pick    *linkPick      // 表示の行を作り直す前に選んでいたリンク (dropLinks)。tileLinks が作り直した並びで同じものに合わせる

	mode   tileMode            // 本体か diff か (d で切り替え)
	diff   *diffJob            // diff の取得 (一度取ったら使い回す)
	hl     func(string) string // コードの色付け (言語が分からなければ nil)
	vlines []viewLine          // 表示の行 (tileview.go)
	vkey   viewKey
	vsrc   int // vlines に取り込んだ元の行の数
}

// slotRect は i 番目の置き場所 (spec §0.2)。1 枚目は中央、2〜4 枚目は中央から右下へ同じ幅ずつずらす。
func slotRect(i, w, h int) rect {
	tw, th := w*80/100, h*85/100
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
	// 🚨 鍵に root と base を含める: 同じ文字列 (util.go) でも、どのフォルダのファイルから読んだかで指す先が違う。
	// 文字列だけを鍵にすると、b/note.txt のリンクが先に開いた a/note.txt の解決 (a/util.go) を使った (再現 2026-10-09)
	key := root + "\x00" + base + "\x00" + t
	if p, ok := cache[key]; ok {
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
	cache[key] = found
	return found
}

// linkPick はジャンプで選んでいるリンクを、表示の行の作り直しをまたいで指すもの。行と桁は折り返しで変わるので使わず、
// パスと字面と、同じパス・字面のリンクの中で何番目か (並びの順は作り直しで変わらない) で指す。
type linkPick struct {
	path, text string
	nth        int
}

// dropLinks は表示の行を作り直すときにリンクを捨てる。ジャンプ中なら、選んでいるリンクを控えておき、tileLinks が作り直した並びで
// 同じものに合わせ直す: 添字のまま残すと、折り返しの変わった並び (端末の幅を変えた・diff の行が届いた) で別のパスを指し、
// 知らせなしに選択が移る (次の enter / e が別のファイルを開く)。
func (t *tile) dropLinks() {
	if t.jump && t.pick == nil {
		var old []link
		for ln := 0; ; ln++ { // links は 0 行目から続けて計算してある (tileLinks)
			ls, ok := t.links[ln]
			if !ok {
				break
			}
			old = append(old, ls...)
		}
		if t.sel < len(old) {
			p := linkPick{path: old[t.sel].path, text: old[t.sel].text}
			for _, l := range old[:t.sel] {
				if l.path == p.path && l.text == p.text {
					p.nth++
				}
			}
			t.pick = &p
		}
	}
	t.links = nil
}

// tileLinks は読めている行のうち lines 番目までのリンク (計算済みは使い回す)。
func (m *Model) tileLinks(t *tile, upto int) []link {
	vl := m.view(t) // 先に作る (作り直すと t.links を捨てる)
	if t.stale && !t.jump {
		t.links, t.stale = nil, false
	}
	if t.links == nil {
		t.links = map[int][]link{}
	}
	pick := t.pick
	t.pick = nil
	if pick != nil && t.jump {
		upto = len(vl) // 選んでいたリンクが作り直した並びのどこにあるかは、全部を見ないと分からない
	}
	var out []link
	base := filepath.Dir(t.n.path())
	for ln := 0; ln < upto && ln < len(vl); ln++ {
		ls, ok := t.links[ln]
		if !ok {
			ls = linksOf(vl[ln].plain, ln, m.root.path(), base, m.linkCache)
			t.links[ln] = ls
		}
		out = append(out, ls...)
	}
	if pick != nil && t.jump {
		t.sel = -1
		for i, l := range out {
			if l.path == pick.path && l.text == pick.text {
				if pick.nth == 0 {
					t.sel = i
					break
				}
				pick.nth--
			}
		}
		switch {
		case len(out) == 0:
			t.sel, t.jump, t.lost = 0, false, true
			m.fail("表示の作り直しで選べるパスがなくなりました")
		case t.sel < 0:
			t.sel, t.lost = 0, true
			m.fail("選んでいたパスが表示の作り直しで見つからないので、先頭のパスを選びました")
		}
		if t.jump {
			// 選んだリンクが見えるようにスクロールする (作り直しで行が動いた・先頭へ寄せた先が画面の外。jumpKey と同じ)
			t.scroll = listnav.WindowOffset(t.scroll, out[t.sel].line, len(vl), m.tileRows(t))
		}
	}
	return out
}

// forgetLinks はリンクの解決を捨てる (フォルダを読み直して、ファイルが増えた・消えたかもしれないとき)。解決は 2 段で覚えている:
// 字面ごとの m.linkCache と、タイルが行ごとに覚えた t.links。片方だけ捨てると、もう片方が無かった頃・在った頃の解決を出し続ける
// ジャンプ中のタイルの links は抜けるまで据え置く (古い印だけ付ける): 捨てると、選んでいる添字が作り直した並びで別のパスを指して
// 知らせなしに選択が移る。抜けさせると、次の enter / e / j が選んだリンクではなくタイルのキーとして効く (vim の .swp でも起きる)。
func (m *Model) forgetLinks() {
	clear(m.linkCache)
	for _, t := range m.tiles {
		if t.jump {
			t.stale = true
			continue
		}
		t.links = nil
	}
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
	t, msg := m.newTile(n, from)
	if msg != "" {
		m.fail(msg)
		return
	}
	m.tiles = append(m.tiles, t)
	m.shut = shutTiles{}
}

// newTile は n を読むタイルを作る (まだ積まない)。開けなければ理由を返す。
func (m *Model) newTile(n *node, from rect) (*tile, string) {
	src, msg := openFor(n.path(), n.raw, n.name)
	if msg != "" {
		return nil, msg
	}
	t := &tile{slot: len(m.tiles) % 4, from: from}
	t.setSource(n, src)
	src.ensure(2 * m.tileRows(t))
	return t, ""
}

// openFor は path のファイルを本文として開く。開けなければ理由 (toast・プレビューの欄に出す文) を返す。
// node を読まない (プレビューの欄は裏の goroutine から呼ぶ)。
func openFor(path, raw, name string) (*textSource, string) {
	if msg := refuseName(raw, name); msg != "" {
		return nil, msg
	}
	src, err := openText(path)
	if err != nil {
		switch {
		case errors.Is(err, errBinary):
			return nil, "バイナリは表示できません: " + name
		case errors.Is(err, errNotRegular):
			return nil, "通常のファイルではないので開けません: " + name
		default:
			return nil, "開けません: " + name
		}
	}
	return src, ""
}

// restoreTile は記憶のタイルを開き切った状態で積む (RestorePlace)。開けなくなったファイルは知らせずに飛ばす: 起動の直後に、
// 打っていない操作の失敗を出さない。
// diff は差分の有無 (diffable) を見ずに出す: git の状態は裏で取り終わる前なので、見ると変更のあるファイルでも本体に落ちる。
// 変更が無くなっていれば「差分はありません」が出る (fetchDiff)。diff のスクロールは戻さない: 行は裏で取るので、着くまで上限が分からない。
// 本体のスクロールは画面の大きさが決まる前に戻す (glogx は描くときに Resize する)。上限で切るのは Resize。
func (m *Model) restoreTile(tp TilePlace) {
	t, msg := m.newTile(m.nodeFor(tp.Path), m.cursorRect())
	if msg != "" {
		return
	}
	if tp.Diff {
		t.setMode(modeDiff)
	} else {
		t.scroll = max(0, tp.Scroll)
		t.src.ensure(t.scroll + 2*m.tileRows(t))
	}
	t.open.step(1, popupDur, 0)
	t.open.snap()
	t.sy.step(float64(t.scroll), scrollDur, 0)
	t.sy.snap()
	m.tiles = append(m.tiles, t)
}

// rereadTiles は開いているタイルの本文を開き直す (エディタ・シェル・! から戻ったとき = ExecDone)。textSource は読んだ所から先を
// 読み足すだけで書き換えを見ないので、開き直さないと古い本文が残り、先へ送ると新しいファイルを古い読み位置から読んで行が崩れる。
// ライブ更新で追記として読み足したタイルは、更新時刻を開いたときのまま残す (followAppend) ので、ここで毎回開き直す: 追記と取り違えた
// 書き換えを拾える唯一の所で、その分、書き込みの続くログを深く送ったタイルは戻るたびに読んだ所まで読み直す。
func (m *Model) rereadTiles() {
	for _, t := range m.tiles {
		if t.closing {
			continue
		}
		if t.mode == modeFile && t.src.unchanged() {
			continue // 変わっていないファイルを開き直すと、深く送ったタイルで読んだ所まで (G 1 回で 2 万行) を毎回読み直す
		}
		m.reopenTile(t, false)
	}
}

// liveTiles は、ライブ更新でフォルダ dir の項目 names が変わったとき、そのファイルを出しているタイルを追いつかせる
// 末尾に足されただけなら読み足しで済ませ (書き込みの続くログ)、書き換わったら開き直す。
// ジャンプ中は開き直さずに印だけ付け、抜けた後 (tileKey) に開き直す: 開き直すとリンクの並びとジャンプが捨てられ、選択が知らせなしに消える。
// 名前で絞る: フォルダの他の項目が変わるたびに、消えたタイルのファイルの「開けません」を出し直さない。
func (m *Model) liveTiles(dir string, names []string) {
	for _, t := range m.tiles {
		p := t.n.path()
		if t.closing || filepath.Dir(p) != dir || !slices.Contains(names, filepath.Base(p)) || t.src.unchanged() {
			continue
		}
		if t.mode == modeDiff {
			// diff は取り直さない: 書き込みの続くファイルで、変わるたびに先頭へ戻して取り直すと読めず、取得が変化の間隔より長いと終わらない。
			// 本文も今は読まない (diff の表示は本文を使わない)。d で本体へ戻すときに開き直す。diff は ExecDone と d で取り直す
			t.dated = true
			continue
		}
		before := len(t.src.lines)
		if t.src.followAppend() {
			if len(t.src.lines) < before {
				t.vlines = nil // 確定させていた末尾の行を取り下げた。足すだけの作り方 (view の grow) と、行の数で見る Markdown では消えないので作り直させる
			}
			t.failed = "" // 読めるようになった。次にまた読めなくなったら知らせる
			continue
		}
		if t.jump {
			t.reread = true
			continue
		}
		m.reopenTile(t, true)
	}
}

// reopenTile はタイルの本文を開き直す。本体のスクロールは保ち、diff は取り直す。開けなくなったファイルは古い本文のまま残して
// 理由を知らせる (黙って古いまま見せない)。live (ライブ更新) では、同じ理由を続けて知らせない: 読めなくなった後も書き手が書き続ける
// ファイルは変わるたびにここへ来る。
func (m *Model) reopenTile(t *tile, live bool) {
	t.reread = false
	if t.mode == modeDiff {
		// diff は本文が同じでも変わる (git add / commit は作業ツリーのファイルを触らない) ので必ず取り直す。本文 (src) は要らない:
		// 消したファイルの diff (全行の削除) も取れる。位置は先頭へ戻す (取り直した行が届くまで上限が分からず、元の位置で切れない)
		if src, msg := openFor(t.n.path(), t.n.raw, t.n.name); msg == "" {
			t.useSource(src) // d で本体へ戻したときに古い本文を出さない
		}
		t.setMode(modeDiff)
		return
	}
	src, msg := openFor(t.n.path(), t.n.raw, t.n.name)
	if msg != "" {
		if !live || msg != t.failed {
			m.fail(msg)
		}
		t.failed = msg
		return
	}
	t.useSource(src)
	t.src.ensure(t.scroll + 2*m.tileRows(t))
	t.scroll = min(t.scroll, m.maxScroll(t))
	t.sy.step(float64(t.scroll), scrollDur, 0) // 縮んだファイルで、古い位置 (本文の外) を描いてから巻き戻さない
	t.sy.snap()
}

func (m *Model) tileRows(t *tile) int { return max(slotRect(t.slot, m.w, m.canvasH()).h-2, 1) }

// maxScroll はタイルのスクロールの上限 (末尾まで読めていなければ、読めている所まで)。
func (m *Model) maxScroll(t *tile) int { return max(0, len(m.view(t))-m.tileRows(t)) }

// nextFile は n と同じフォルダの、木に見えている開けるファイルを delta 方向に探す (タイルの J / K。glogx-ui-guide §6)。
// 起点は隠すものも含む全部の子の中で探す: 隠したファイル (リンクから重ねて開いた dotfile) は木に見えていないが、隣は数えられる。
// n が親の子に居なければ (木から外れた) nil。
func (m *Model) nextFile(n *node, delta int) *node {
	if n.parent == nil {
		return nil
	}
	s := n.parent.kids
	i := slices.Index(s, n)
	if i < 0 {
		return nil
	}
	for j := i + delta; j >= 0 && j < len(s); j += delta {
		if !s[j].dir && m.visible(s[j]) && refuse(s[j]) == "" {
			return s[j]
		}
	}
	return nil
}

func (m *Model) tileKey(k string) {
	t := m.frontTile()
	rows := m.tileRows(t)
	defer func() {
		if t.reread && !t.jump && !t.closing {
			m.reopenTile(t, true) // ジャンプ中に書き換わったファイル (liveTiles)
		}
	}()
	if t.lost {
		// 選んでいたリンクを見失った (知らせは出した) 後の最初の打鍵は捨てる: 効かせると enter / e が知らせを見る前に別のパスを開き、
		// ジャンプを抜けていればタイルのキー (enter / q で閉じる・e / o で本体を開く) に化ける。見失うのは描画 (drawTile が tileLinks を呼ぶ)
		// が先のことが多い (bubbletea は大きさの変更の後に描く) ので、jumpKey の中だけで見ると素通りする
		t.lost = false
		return
	}
	if t.jump {
		if m.jumpKey(t, k, rows) {
			return
		}
	}
	switch k {
	// 閉じるキーは glogx-ui-guide §3 (q Esc h ← と、開閉 toggle の Enter)
	case "q", "esc", "h", "left", "enter":
		m.noteShut()
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
	case "o":
		m.opener.open(t.n.path(), t.n.name)
		return
	case "O":
		m.opener.reveal(t.n.path(), t.n.name)
		return
	case "e":
		m.requestEdit(t.n.path())
		return
	case "d":
		m.toggleDiff(t)
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
	total := len(m.view(t))
	ls := m.tileLinks(t, total)
	if t.lost {
		t.lost = false
		return true // この打鍵の手前で見失った (tileKey の lost の注記)
	}
	if len(ls) == 0 {
		t.jump = false
		return false
	}
	t.sel = max(0, min(t.sel, len(ls)-1))
	switch {
	case k == "tab" || listnav.MotionOf(k) == listnav.Down:
		t.sel = listnav.Cycle(t.sel, 1, len(ls))
	case k == "shift+tab" || listnav.MotionOf(k) == listnav.Up:
		t.sel = listnav.Cycle(t.sel, -1, len(ls))
	case listnav.MotionOf(k) == listnav.Top:
		t.sel = 0
	case listnav.MotionOf(k) == listnav.Bottom:
		t.sel = len(ls) - 1
	case k == "enter":
		l := ls[t.sel]
		t.jump = false
		r := slotRect(t.slot, m.w, m.canvasH())
		from := rect{r.x + 2 + m.tileGutter(t) + l.col, r.y + 1 + l.line - t.scroll, widthOf(l.text), 1}
		m.openTile(m.nodeFor(l.path), from)
		return true
	case k == "e": // 選んだパスをエディタで (issues viewer のジャンプモードの e と同じ)
		t.jump = false
		m.requestEdit(ls[t.sel].path)
		return true
	case k == "esc" || k == "q" || k == "h" || k == "left":
		t.jump = false
		return true
	default:
		t.jump = false
		return false
	}
	// 選んだリンクが見えるようにスクロールする
	t.scroll = listnav.WindowOffset(t.scroll, ls[t.sel].line, total, rows)
	return true
}

// nodeFor はリンク先のパスの項目。木の中にあればそれを (途中のフォルダは読み込む)、無ければ木に繋がない単独の項目を作る。
func (m *Model) nodeFor(path string) *node {
	if n := m.lookup(path, m.ensureLoaded); n != nil {
		return n
	}
	info, _ := os.Lstat(path) // 消えていても項目は作る (開くときに「開けません」になる)
	n := newNode(baseName(path), info, nil)
	n.setRaw(path) // root の外のファイル (parent が無い)
	n.abs = path
	return n
}

// replaceTile は手前のタイルを開いたまま同じフォルダの隣のファイルへ差し替える。木のカーソルも一緒に動かす。
func (m *Model) replaceTile(t *tile, delta int) {
	if t.n.parent == nil {
		// ジャンプで開いた root の外のファイル。隣を辿る木が無い
		m.fail("このファイルは木の外にあるので隣へ送れません")
		return
	}
	if !m.inTree(t.n) { // 見せる子 (m.kids) では見ない: 隠したファイルは居ないが木には在る。親ごと外れた (mv a b) のは親の kids では分からない
		// 開いた後に外で消した・名前を変えた (ライブ更新が kids から外した)。起点が無いまま nextFile に渡すと先頭から数え、
		// K で「これが最初のファイルです」と嘘を言い、J は 2 番目へ飛ぶ
		m.fail("このファイルは木から外れたので隣へ送れません")
		return
	}
	n := m.nextFile(t.n, delta)
	for n != nil {
		src, err := openText(n.path())
		if err == nil {
			if m.cur == t.n {
				m.setCur(n)
			}
			t.setSource(n, src)
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
	bc, tc := mix(cPop, cAccRoute, p), cText
	if !front {
		bc, tc = mix(cPop, cAccAct, p), cMuted
	}
	inner := r.w - 2
	c.frame(r, bc)
	title := fmt.Sprintf(" %s · %s ", t.n.name, human(t.n.size))
	if t.mode == modeDiff {
		title += "· diff "
	}
	title = termwidth.Truncate(title, max(inner-2, 1), "…")
	c.put(r.x+2, r.y, title, tc, true)
	if p < 0.9 {
		return // 開き切る前は枠だけ (spec §4.6)
	}
	rows := r.h - 2
	sy := int(math.Round(t.sy.v))
	vl := m.view(t) // 溝の桁より先に作る (Markdown は作るときに読み進め、溝の桁がそこで変わりうる)
	g := m.tileGutter(t)
	tx, lw := r.x+2+g, r.w-4-g // 本文は行番号の溝の右から
	var links []link
	if t.jump && front {
		links = m.tileLinks(t, sy+rows)
	}
	switch {
	case t.mode == modeDiff && m.diffLoading(t):
		c.put(r.x+2, r.y+1, spinnerFrame(m.now())+" diff を取得中", cMuted, false)
	case len(vl) == 0:
		c.put(r.x+2, r.y+1, "(空のファイル)", cMuted, false)
	}
	for i := 0; i < rows && sy+i < len(vl); i++ {
		ln := sy + i
		if g > 0 {
			c.put(r.x+2, r.y+1+i, gutter.Format(vl[ln].src, g), cMuted, false)
		}
		c.putSGR(tx, r.y+1+i, t.styledLine(ln), lw, cText)
		for li, l := range links {
			if l.line != ln {
				continue
			}
			for k := range widthOf(l.text) {
				if l.col+k >= lw {
					break
				}
				if q := c.at(tx+l.col+k, r.y+1+i); q != nil {
					q.ul = true
					q.rev = li == t.sel
				}
			}
		}
	}
	total := strconv.Itoa(len(vl))
	if t.mode == modeFile && !t.src.eof {
		total += "+" // まだ末尾まで読んでいない
	}
	dl := ""
	if m.diffable(t.n) {
		dl = "d diff · "
		if t.mode == modeDiff {
			dl = "d file · "
		}
	}
	if t.mode == modeFile && t.src.readErr {
		dl = "途中で読めなくなりました · " + dl
	}
	posLabel := fmt.Sprintf(" %s%d/%s ", dl, sy+1, total)
	if t.jump && front {
		posLabel = " tab: パスを選ぶ · enter: 重ねて開く · esc: 戻る " + posLabel
	}
	c.put(r.x+r.w-2-widthOf(posLabel), r.y+r.h-1, posLabel, cMuted, false)
	if maxS := m.maxScroll(t); maxS > 0 && len(vl) > 0 {
		th := max(1, rows*rows/len(vl))
		ty := int(float64(rows-th) * float64(min(sy, maxS)) / float64(maxS))
		// 枠の縦線の上には描かない (枠と見分けが付かない。ユーザー回答 2026-10-09「四隅と見た目を区別して」)。
		// 枠の 1 桁内側 (本文の右の空き列) に、つまみは塗りつぶし・溝は薄い網で描く
		for y := range rows {
			g, col := "░", mix(cPop, cAccDim, 0.8)
			if y >= ty && y < ty+th {
				g, col = "█", cAccRoute
			}
			c.put(r.x+r.w-2, r.y+1+y, g, col, false)
		}
	}
}

// toggleDiff は d (git の diff と本体の切り替え。spec §8.3)。diff を出せないファイルでは理由を知らせる。
func (m *Model) toggleDiff(t *tile) {
	if t.mode == modeDiff {
		t.setMode(modeFile)
		if t.dated {
			m.reopenTile(t, false) // diff を出している間にライブ更新で本文が変わった (liveTiles)。利用者の操作なので開けなければ毎回知らせる
		}
		return
	}
	if !m.diffable(t.n) {
		t.stopDiff()
		m.fail("diff はありません (git の変更が無いファイル)")
		return
	}
	t.setMode(modeDiff)
}

// setMode は本体と diff を切り替え、先頭から出し直す。
func (t *tile) setMode(mode tileMode) {
	t.stopDiff()
	t.mode = mode
	if mode == modeDiff {
		t.diff = startDiff(t.n.path()) // 切り替えるたびに取り直す (開いている間に書き換えた・git add した分を出す)
	}
	t.scroll, t.sy, t.jump, t.links = 0, tween{}, false, nil
	t.vlines, t.vkey, t.vsrc = nil, viewKey{}, 0 // 取り直した diff が前と同じ行の数でも、前の表示を使い回さない
}

func (m *Model) diffLoading(t *tile) bool {
	if t.diff == nil {
		return false
	}
	_, _, done := t.diff.result()
	return !done
}

// stopDiff は取得中の diff の git を止める (結果は使わない)。
func (t *tile) stopDiff() {
	if t.diff != nil && t.diff.cancel != nil {
		t.diff.cancel()
	}
}
