package filer

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/lineedit"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// finder.go は `/` (root 以下の全部のファイルとフォルダを名前で探す中央のポップアップ)。開いていないフォルダの中も探す
// (開いている木の中だけを探す検索は `t`。search.go)。裏の goroutine で root から幅優先に辿り、見つかった分を Advance で取り込んで
// 今の語で照らす。Enter で選んだ候補へ、途中のフォルダを開いてカーソルを移す (`:` と同じ reveal)。
// 隠したフォルダの中も探す (ユーザー決定 2026-11-07)。`.git` の中と、git が無視するフォルダ (`>` と同じ gitSnap の判定) には降りない。
// symlink は辿らない (木と同じ)。

// finderMax は辿って集める項目の上限。超えたら止めて「上限で止めた」と出す (~ を root にしたときに辿り終わらず、照らす量も増え続けないように)。
// 開くときに読んで走査へ渡す (goroutine の中で読まない: テストが下げる値と競合する)。
var finderMax = 100000

// finderRankTiers は候補の並びの段の数。名前 (最後の要素) に一致したものを先に、パスの途中にしか一致しないものを後に置き、
// それぞれの中は fuzzy の段 (前方一致 → 語の頭 → 部分 → 文字が順に含まれる) で並べる。同じ段の中は幅優先の順 (浅い方が先)。
const finderRankTiers = 2 * rankNone

type finderState struct {
	active bool
	line   lineedit.Line
	hist   histCursor
	scan   *finderScan
	root   string       // 辿り始めた root (候補の相対パスの起点)
	paths  []finderPath // 取り込んだ全部 (幅優先の順)
	query  string       // tiers を引いた語
	tiers  [finderRankTiers][]finderHit
	hits   []finderHit // tiers をつないだもの (dirty なら作り直す)
	dirty  bool
	sel    int // 選んでいる候補の番号 (hits の中)。並びを作り直しても同じ項目を指すように list が引き直す
	top    int // 一覧の窓の先頭
	capped bool
	done   bool
}

// finderPath は辿って見つけた 1 項目。rel は木で探す綴り (root からの相対)、shown は画面に出す綴り (無害化した rel)。
type finderPath struct {
	rel, shown string
	dir        bool
}

type finderHit struct {
	p     *finderPath
	spans [][2]int // shown の中の一致の位置 (バイト)
}

// finderScan は裏で辿っている 1 回 (goroutine から書き、Advance が取り出す)。
type finderScan struct {
	mu     sync.Mutex
	found  []finderPath // まだ取り込んでいない分
	done   bool
	capped bool
	stopFlag
}

func (s *finderScan) take() (found []finderPath, done, capped bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found, s.found = s.found, nil
	return found, s.done, s.capped
}

// startFinderScan は root の配下を幅優先に辿り始める。limit 件で止める。skip は降りないフォルダの判定 (git が無視するもの)。
func startFinderScan(root string, limit int, skip func(path string) bool) *finderScan {
	s := &finderScan{}
	finderWG.Add(1)
	go func() {
		defer finderWG.Done()
		queue := []string{root}
		total := 0
		capped := false
	walk:
		for len(queue) > 0 {
			if s.canceled() {
				break
			}
			d := queue[0]
			queue = queue[1:]
			ents, err := os.ReadDir(d)
			if err != nil {
				continue // 読めないフォルダ (権限) は飛ばす。中を探せないだけで、フォルダ自身は親の段で候補に入っている
			}
			batch := make([]finderPath, 0, len(ents))
			for _, e := range ents {
				if total >= limit {
					capped = true
					s.publish(batch)
					break walk
				}
				p := filepath.Join(d, e.Name())
				rel, err := filepath.Rel(root, p)
				if err != nil {
					continue
				}
				dir := e.IsDir() // DirEntry の種類は symlink を辿らない (symlink のフォルダは降りない)
				batch = append(batch, finderPath{rel: rel, shown: termsafe.PlainLine(rel), dir: dir})
				total++
				if dir && e.Name() != ".git" && !skip(p) {
					queue = append(queue, p)
				}
			}
			s.publish(batch)
		}
		s.mu.Lock()
		s.done, s.capped = true, capped
		s.mu.Unlock()
	}()
	return s
}

func (s *finderScan) publish(batch []finderPath) {
	if len(batch) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.found = append(s.found, batch...)
}

// finderWG は裏で辿っている goroutine (テストが終わりを待つ。Close は待たない: 応答しないマウントで閉じられなくなる)。
var finderWG sync.WaitGroup

// startFinder は `/`。git が無視するフォルダの判定は、開いた時点の gitSnap で決める (`>` と同じ。Git の設定が off・取得の前なら降りる)。
// gitSnap が持つのは開いているフォルダの repo だけなので、閉じたフォルダの中の repo の node_modules などには降りる (候補が増え、上限に早く届く)。
// 降りないようにするには、走査が repo の根 (.git を持つフォルダ) に入ったところで git に問い合わせる形が要る (repo ごとに git を起こす)。
func (m *Model) startFinder() {
	snap := m.gitSnap
	root := m.root.path()
	m.finder = finderState{active: true, root: root, hist: histCursor{at: len(m.searchHist)},
		scan: startFinderScan(root, finderMax, func(p string) bool { return snap.state(p, true) == gitIgn })}
}

// endFinder は閉じて辿るのを止める。取り込んだ候補は手放す (閉じた後は誰も読まない。10 万件を掴み続けない)。
func (m *Model) endFinder() {
	if m.finder.scan != nil {
		m.finder.scan.cancel()
	}
	m.finder = finderState{}
}

// finderScanning は裏で辿っている途中か (Busy に足す。取り込んでいない分がある間も真)。
func (m *Model) finderScanning() bool { return m.finder.active && !m.finder.done }

// takeFinder は見つかった分を取り込み、今の語で照らして候補に足す (Advance から)。
func (m *Model) takeFinder() {
	f := &m.finder
	if !f.active || f.scan == nil || f.done {
		return
	}
	found, done, capped := f.scan.take()
	from := len(f.paths)
	f.paths = append(f.paths, found...)
	for i := from; i < len(f.paths); i++ {
		f.match(&f.paths[i])
	}
	f.done, f.capped = done, capped
}

// match は p を今の語で照らし、一致すれば段に足す。
func (f *finderState) match(p *finderPath) {
	if f.query == "" {
		f.tiers[0] = append(f.tiers[0], finderHit{p: p})
		f.dirty = true
		return
	}
	base := strings.LastIndexByte(p.shown, '/') + 1
	if r, spans := fuzzy(p.shown[base:], f.query); r < rankNone {
		for i := range spans {
			spans[i][0] += base
			spans[i][1] += base
		}
		f.tiers[r] = append(f.tiers[r], finderHit{p: p, spans: spans})
		f.dirty = true
		return
	}
	if r, spans := fuzzy(p.shown, f.query); r < rankNone {
		f.tiers[rankNone+r] = append(f.tiers[rankNone+r], finderHit{p: p, spans: spans})
		f.dirty = true
	}
}

// refilter は語が変わった後に、取り込んだ全部を照らし直す。選んでいる候補は先頭へ戻す (良い一致が先頭に来る)。
func (f *finderState) refilter() {
	f.query = f.line.String()
	for i := range f.tiers {
		f.tiers[i] = f.tiers[i][:0]
	}
	for i := range f.paths {
		f.match(&f.paths[i])
	}
	f.hits = f.hits[:0] // 前の語の並びの選択を引き継がない (list が引き直す項目を無くし、先頭を選ぶ)
	f.dirty, f.sel, f.top = true, 0, 0
}

// list は候補を良い段から並べたもの。作り直すときは、直前の並びで選んでいた項目を選び続ける: 走査中の取り込みが前の段に一致を
// 足しても、▶ の付いた (Enter で移る) 候補は語を打ち替えるまで変わらない (Tab で選んだかによらない。番号だけを持つと、見ていない候補へ移る)。
func (f *finderState) list() []finderHit {
	if f.dirty {
		var keep *finderPath
		if f.sel < len(f.hits) {
			keep = f.hits[f.sel].p
		}
		f.hits = f.hits[:0]
		for _, t := range f.tiers {
			f.hits = append(f.hits, t...)
		}
		f.dirty = false
		if keep != nil {
			for i, h := range f.hits {
				if h.p == keep {
					f.sel = i
					break
				}
			}
		}
	}
	return f.hits
}

// finderKey はポップアップを開いている間のキー。lineedit の編集キーを先に渡し、移動・履歴・確定・取り消しだけを自分で捌く (検索の t と同じ語彙)。
func (m *Model) finderKey(key, text string) {
	f := &m.finder
	switch {
	case key == "enter":
		hits := f.list()
		if len(hits) == 0 {
			return // 移る先が無い。閉じずに打ち直せるようにする (抜けるのは esc)
		}
		if q := f.line.String(); q != "" {
			m.rememberSearch(q)
		}
		p := *hits[min(f.sel, len(hits)-1)].p
		abs := filepath.Join(f.root, p.rel)
		m.endFinder()
		if !m.reveal(abs) {
			m.fail("移動できません: 木の中に見つかりません (消えた・root が変わった): " + p.shown)
		}
		return
	case key == "esc" || key == "ctrl+c":
		m.endFinder()
		return
	case key == "tab" || key == "ctrl+n":
		f.step(1)
		return
	case key == "shift+tab" || key == "ctrl+p":
		f.step(-1)
		return
	case key == "up" || key == "down": // 検索の履歴を辿る (t と同じ履歴)
		d := 1
		if key == "up" {
			d = -1
		}
		if f.hist.step(m.searchHist, &f.line, d) {
			f.refilter()
		}
		return
	case (key == "backspace" || key == "ctrl+h") && f.line.Empty(): // 空欄の backspace で閉じる (検索の t と同じ)
		m.endFinder()
		return
	default:
		before := f.line.String()
		if !f.line.Key(key, text) {
			return
		}
		if f.line.String() == before {
			return // カーソルを動かしただけ
		}
		f.hist.at = len(m.searchHist)
	}
	f.refilter()
}

// step は選ぶ候補を d だけ回す。
func (f *finderState) step(d int) {
	if hits := f.list(); len(hits) > 0 {
		f.sel = listnav.Cycle(f.sel, d, len(hits))
	}
}

// finderPaste は貼り付けた文字を語に足す。
func (m *Model) finderPaste(text string) {
	m.finder.line.Insert(text)
	m.finder.hist.at = len(m.searchHist)
	m.finder.refilter()
}

const finderPrompt = "> "

// finderRect はポップアップの外形 (描画とキャレットが同じ値を使う)。収まらなければ ok = false (最下行に出す)。
func (m *Model) finderRect() (rect, bool) {
	w := min(m.w-4, 100)
	h := min(m.h-2, 24)
	if w < finderMinW || h < 6 {
		return rect{}, false
	}
	return rect{(m.w - w) / 2, max((m.h-1-h)/2, 0), w, h}, true
}

// finderField は入力欄の窓 (表示する語とキャレットの桁)。窓の後ろは切って返す (lineedit.Window は後ろを切らない)。
func (m *Model) finderField(r rect) (text string, caretX, y int) {
	x := r.x + 2 + widthOf(finderPrompt)
	w := max(r.x+r.w-2-x-finderStatusW, 1)
	text, col := m.finder.line.Window(w)
	return termwidth.Truncate(text, w, ""), x + col, r.y + 1
}

// finderStatusW は入力欄の右に出す件数と走査の様子の桁 (入力の窓から引く)。finderMinW はポップアップを出す最小の幅:
// 枠と余白 (4)・"> " (2)・件数 (finderStatusW) を引いて、入力に 8 桁残る幅。これより狭いと最下行に出す (件数が入力と重なる)。
const (
	finderStatusW = 28 // "100000/100000 · 上限で止めた" が切れない桁
	finderMinW    = 4 + 2 + finderStatusW + 8
)

// finderSmallPrefix は狭い画面で最下行に出すときの頭。finderSmallField は、そのときの入力の窓 (描画とキャレットが同じ値を使う)。
const finderSmallPrefix = "/ "

func (m *Model) finderSmallField() (text string, caretX int) {
	w := max(m.w/2-widthOf(finderSmallPrefix), 1)
	text, col := m.finder.line.Window(w)
	return termwidth.Truncate(text, w, ""), widthOf(finderSmallPrefix) + col
}

func (m *Model) finderStatus() string {
	f := &m.finder
	n := len(f.list())
	s := itoa(min(f.sel+1, n)) + "/" + itoa(n)
	switch {
	case f.capped:
		s += " · 上限で止めた"
	case !f.done:
		s += " " + spinnerFrame(m.now()) + " 探索中"
	}
	return s
}

// drawFinder は中央のポップアップ。題 → 入力欄と件数 → 候補 → 案内。
func (m *Model) drawFinder(c *canvas) {
	r, ok := m.finderRect()
	f := &m.finder
	if !ok {
		text, _ := m.finderSmallField()
		m.bottomHint(c, finderSmallPrefix+text+"  "+m.finderStatus()+" · 画面が小さいので候補を出せません · esc 戻る")
		return
	}
	c.frame(r, cAccRoute)
	c.put(r.x+2, r.y, termwidth.Truncate(" ファイルを探す (root 以下) ", r.w-4, "…"), cText, true)
	text, _, y := m.finderField(r)
	x := c.put(r.x+2, y, finderPrompt, cAccRoute, true)
	c.put(x, y, text, cText, false)
	st := termwidth.Truncate(m.finderStatus(), finderStatusW, "…")
	c.put(r.x+r.w-2-widthOf(st), y, st, cMuted, false)
	rows := r.h - 4 // 枠の上下・入力欄・案内を除いた行
	hits := f.list()
	f.top = listnav.WindowOffset(f.top, f.sel, len(hits), rows)
	inner := r.w - 4
	for i := 0; i < rows && f.top+i < len(hits); i++ {
		at := f.top + i
		m.drawFinderRow(c, r.x+2, y+1+i, inner, hits[at], at == f.sel)
	}
	if len(hits) == 0 && f.query != "" && f.done {
		c.put(r.x+2, y+1, "一致なし", cDot, false)
	}
	hint := "tab 次 · ⇧tab 前 · ↑↓ 履歴 · enter 移動 · esc 戻る"
	c.put(r.x+2, r.y+r.h-2, termwidth.Truncate(hint, inner, "…"), cMuted, false)
}

// drawFinderRow は候補の 1 行。長いパスは頭を切って名前を残し、一致した文字を検索の一致と同じ色で出す。
func (m *Model) drawFinderRow(c *canvas, x, y, w int, h finderHit, sel bool) {
	mark, fg := "  ", cText
	if sel {
		mark, fg = "▶ ", cAccRoute
	}
	x = c.put(x, y, mark, cAccRoute, true)
	s := h.p.shown
	if h.p.dir {
		s += "/"
	}
	avail := w - widthOf(mark)
	cut := 0 // 頭を切った分 (s のバイト)
	if widthOf(s) > avail {
		t := termwidth.TruncateLeft(s, avail, "…")
		cut = len(s) - (len(t) - len("…"))
		x = c.put(x, y, "…", cMuted, false)
		s = s[cut:]
	}
	pos := 0
	for _, sp := range h.spans {
		a, b := max(sp[0]-cut, 0), min(sp[1]-cut, len(s))
		if a >= b || a < pos {
			continue
		}
		x = c.put(x, y, s[pos:a], fg, sel)
		x0 := x
		x = c.put(x, y, s[a:b], cFlash, true)
		for xx := x0; xx < x; xx++ {
			if p := c.at(xx, y); p != nil {
				p.bg = cMatchBg
			}
		}
		pos = b
	}
	c.put(x, y, s[pos:], fg, sel)
}
