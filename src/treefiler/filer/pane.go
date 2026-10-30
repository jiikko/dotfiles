package filer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/jiikko/dotfiles/src/tuikit/gutter"
	"github.com/jiikko/dotfiles/src/tuikit/highlight"
	"github.com/jiikko/dotfiles/src/tuikit/markdown"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// pane.go は右の 3 割に常に出すプレビューの欄 (2026-10-30 ユーザー依頼「左 7 割をツリーにして、右 3 割を preview に。
// 切り替えられるように。preview は非同期でファイラーの部分の動作を優先」)。p で開閉し、設定 (Preview pane) に保存する。
// フォルダの上では空欄 (同日ユーザー回答)。Enter は今までどおり中央にタイルを開く。
//
// 読み込みは UI の goroutine でしない: カーソルの変化ごとに別の goroutine で先頭だけ読んで整形し、結果は Advance で取り込む。
// 押しっぱなしの間 (pulse.go の holding) は頼まない。届いた結果のうち、今のカーソルの頼みでないものは捨てる (世代で見る)。

const (
	paneShare = 0.3 // 画面の幅に占める欄の割合
	paneMinW  = 30  // 欄がこの桁より狭くなる画面では欄を出さない (本文が読める幅にならない)
)

// paneKey は欄の中身を決めるもの。変わったら読み直す (ファイルの更新はライブ更新が mtime と size を書き換える)。
type paneKey struct {
	path      string
	raw, name string
	mtime     int64
	size      int64
	w, h      int
	wrap      bool
}

// paneResult は 1 回の読み込みの結果。
type paneResult struct {
	gen   int
	key   paneKey
	lines []string // SGR つき
	nums  []int    // 左の溝の行番号 (0 = 出さない)
	gw    int      // 溝の桁 (0 = 溝なし)
	note  string   // 本文の代わりに出す 1 行 (開けない理由・空のファイル)
}

type paneState struct {
	gen     int         // 最後に頼んだ世代
	asked   paneKey     // 最後に頼んだ中身 (同じなら頼み直さない)
	waiting bool        // 最後の頼みの結果がまだ届いていない (Busy)
	shown   *paneResult // 描いている結果
	mu      sync.Mutex
	latest  *paneResult // goroutine が置いた最新の結果 (Advance で shown へ移す)
	// loader は裏で走らせる読み込み (nil なら loadPane)。テストが止める読み込みに差し替えて、UI の goroutine で読んでいない
	// ことを確かめる。Model ごとに持つ: パッケージの変数にすると、前のテストの goroutine が読むのと差し替えが競合する
	loader func(paneKey) paneResult
}

// paneWidth は欄の桁 (出さないなら 0)。
func (m *Model) paneWidth() int {
	if !m.set.Preview {
		return 0
	}
	if w := int(float64(m.w) * paneShare); w >= paneMinW {
		return w
	}
	return 0
}

// treeWidth は木を描く幅 (欄の左の縦線まで)。
func (m *Model) treeWidth() int { return m.w - m.paneWidth() }

// paneBody は欄の本文の桁と行 (縦線と左右の余白・見出しの行を除く)。
func (m *Model) paneBody() (w, h int) { return m.paneWidth() - 3, m.canvasH() - 1 }

// stepPane は今のカーソルの中身を頼み、届いた結果を取り込む。
// 押しっぱなしの間は頼まない (離したときに頼む)。
func (m *Model) stepPane() {
	m.pane.mu.Lock()
	if r := m.pane.latest; r != nil && r.gen == m.pane.gen {
		m.pane.shown, m.pane.latest, m.pane.waiting = r, nil, false
	}
	m.pane.mu.Unlock()
	if m.paneWidth() == 0 || m.cur.dir {
		// 出さない間は待たない: 読み込みが返らないファイル (止まったネットワークのマウント) で Busy が下りず、tick が回り続ける。
		// 頼んだ記録も消す: 消さないと、同じファイルへ戻ったときに「頼み済み」で頼み直さず、待ちもしないので、届いた結果を
		// 取り込む Advance が来ないまま前の本文が薄く残る (読み込みが返らないなら二度と頼まない)
		m.pane.waiting, m.pane.asked = false, paneKey{}
		return
	}
	if m.holding {
		return
	}
	k := m.paneKeyNow()
	if k == m.pane.asked {
		return
	}
	m.pane.gen++
	m.pane.asked, m.pane.waiting = k, true
	gen := m.pane.gen
	load := m.pane.loader
	if load == nil {
		load = loadPane
	}
	go func() {
		m.pane.offer(loadPaneSafe(load, k, gen))
	}()
}

// paneKeyNow は今のカーソルと欄の大きさから決まる中身の鍵 (ファイルを読まない。path は node が持つ値)。
func (m *Model) paneKeyNow() paneKey {
	w, h := m.paneBody()
	return paneKey{path: m.cur.path(), raw: m.cur.raw, name: m.cur.name, mtime: m.cur.mtime.UnixNano(), size: m.cur.size,
		w: w, h: h, wrap: m.set.Wrap}
}

// offer は裏の goroutine が結果を置く。前に置かれたものより新しい世代だけを置く (古い頼みが遅れて届いても、新しい結果を消さない)。
func (p *paneState) offer(r paneResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.latest == nil || p.latest.gen < r.gen {
		p.latest = &r
	}
}

// loadPaneSafe は load を呼び、panic を欄の 1 行に変える: 裏の goroutine の panic は回復されずにプロセスごと落ち、
// 端末も戻らない (タイルの読み込みは bubbletea の Update の中で走り、そこで回復されていた)。
func loadPaneSafe(load func(paneKey) paneResult, k paneKey, gen int) (r paneResult) {
	defer func() {
		if v := recover(); v != nil {
			r = paneResult{gen: gen, key: k, note: fmt.Sprintf("表示できません (内部エラー: %v): %s", v, k.name)}
		}
	}()
	r = load(k)
	r.gen = gen
	return r
}

// paneLoading は今出すべき中身を待っているか (Busy。待つ間は呼び出し側が Advance を回し、届いた結果を取り込ませる)。
func (m *Model) paneLoading() bool { return m.pane.waiting }

// loadPane は k のファイルの先頭を読み、欄の高さ分の行を整形する。UI の goroutine から呼ばない (ファイルを読む)。
func loadPane(k paneKey) paneResult {
	r := paneResult{key: k}
	src, msg := openFor(k.path, k.raw, k.name)
	if msg != "" {
		r.note = msg
		return r
	}
	gw := gutter.Width(src.estLines())
	if k.w-gw < gutterMinText {
		gw = 0
	}
	tw := max(k.w-gw, 1)
	if isMarkdown(k.raw) {
		src.limit = markdownCap
		src.ensureBytes(markdownCap)
		out, nums := markdown.Render(strings.Join(src.lines, "\n"), tw, true)
		for i := 0; i < len(out) && i < k.h; i++ {
			n := 0
			if i < len(nums) && nums[i] > 0 {
				n = src.srcLine(nums[i] - 1)
			}
			r.lines, r.nums = append(r.lines, out[i]), append(r.nums, n)
		}
	} else {
		src.ensure(k.h)
		hl := highlight.ForPath(k.raw)
	lines:
		for i, l := range src.lines {
			pieces := []string{l}
			if k.wrap {
				pieces = wrapCells(l, tw)
			}
			for j, p := range pieces {
				if len(r.lines) == k.h {
					break lines
				}
				n := 0
				if j == 0 {
					n = src.srcLine(i)
				}
				if hl != nil {
					p = hl(p)
				}
				r.lines, r.nums = append(r.lines, p), append(r.nums, n)
			}
		}
	}
	if len(r.lines) == 0 {
		r.note = "(空のファイル)"
	}
	r.gw = gw
	return r
}

// drawPane は欄を描く (木の上に重ねる。タイルはさらにその上)。見出しは今のカーソル、本文は届いた結果。
// 今のカーソルの結果がまだなら、前の本文を薄く残す (読み込み中と分かる・空にして瞬かせない)。フォルダの上では空欄。
func (m *Model) drawPane(c *canvas, ch int) {
	pw := m.paneWidth()
	if pw == 0 {
		return
	}
	x0 := m.w - pw
	c.clear(x0, 0, m.w-1, ch-1, cBg)
	for y := range ch {
		c.put(x0, y, "│", cAccDim, false)
	}
	if m.cur.dir {
		return
	}
	bw, _ := m.paneBody()
	head := termwidth.Truncate(m.cur.name+" · "+human(m.cur.size), bw, "…")
	c.put(x0+2, 0, head, cText, true)
	r := m.pane.shown
	if r == nil {
		return
	}
	fresh := r.gen == m.pane.gen && r.key == m.paneKeyNow() // 鍵ごと比べる (幅を変えた直後の古い幅の本文・書き換わる前の本文を薄くする)
	if r.note != "" {
		if fresh {
			c.put(x0+2, 1, termwidth.Truncate(r.note, bw, "…"), cMuted, false)
		}
		return
	}
	tx := x0 + 2 + r.gw
	for i, l := range r.lines {
		y := 1 + i
		if y >= ch {
			break
		}
		if r.gw > 0 && r.nums[i] > 0 {
			c.put(x0+2, y, gutter.Format(r.nums[i], r.gw), cMuted, false)
		}
		if fresh {
			c.putSGR(tx, y, l, bw-r.gw, cText)
		} else {
			c.put(tx, y, termwidth.Truncate(termwidth.StripSGR(l), bw-r.gw, ""), mix(cMuted, cBg, 0.4), false)
		}
	}
}
