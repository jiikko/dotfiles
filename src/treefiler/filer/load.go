package filer

import (
	"slices"
	"sync"
	"time"
)

// load.go は打鍵で初めて開くフォルダ (l / Enter の潜る・Space / Tab の開く) の中身を UI の外で読む。
// 読み込み・ノードの生成・並べ替えを裏の goroutine で行い、UI は LoadWait だけ結果を待つ。間に合えば今までどおりその場で取り込み、
// 間に合わなければ打鍵の残り (続き) を覚えて戻り、Advance で取り込む (2 万件のフォルダで、打鍵の中で 48ms 止まっていた)。
//
// 起動時の root・a (作る)・> (配下を全部)・: (パスへ飛ぶ)・前回の場所の復元・タイルのリンクの解決・ライブ更新の読み直しは
// 同期のまま (ensureLoaded / load)。どれも読んだ中身をその場で使う。
//
// 状態 × 読み手の期待値の要点:
//   - 次のキーが来たら、そのキーより先に読み込みを待って続きを済ませる (flushLoad。HandleInput の入口)。先打ちのキー (l j・l l) は
//     潜った後の状態に効く (同期で読んでいた頃と同じ順と意味)。止まらないのは、何も打たずに待っている間だけ。
//     キーで続きを捨てる形にしない: l j が A の中ではなく A の隣へ動き、l l が 1 段しか潜らない
//   - loadFlushWait のうちに届かなければ (止まったマウント) 続きは捨て、キーは今の状態に効かせる
//   - 続きを発火するのは、カーソルがまだそのフォルダの上にあり、層 (板・入力欄・確認) とタイルが開いていないときだけ
//     (ライブ更新でフォルダが消えてカーソルが親へ移ったら潜らない)
//   - 頼んだフォルダが先に同期で読まれたら、届いた結果は捨てる。木から外れたフォルダの失敗は知らせない
//   - Close は続きだけを消す (glogx の F は Close の後も同じ Model を開き直す)。結果は開き直した後の Advance で取り込み、記録を下ろす

// LoadWait は打鍵で頼んだ読み込みを UI が待つ長さ。負なら届くまで待つ (同期と同じ。テストと tfprobe が使う:
// 壁時計で同期 / 非同期が分かれると、同じキーの列でも画面が run ごとに変わる)。
var LoadWait = 12 * time.Millisecond

// loadFlushWait は、続きを待っている間に次のキーが来たとき、読み込みを待つ上限。頼んでからの合計で数え、超えたら続きを捨てる
// (キーごとに数えると、止まったマウントで l を押し続けるたびに 1 秒ずつ止まる)。
var loadFlushWait = time.Second

// loadWaiter は読み込みを待つ (テストが待った回数を数える差し込み口)。
var loadWaiter = waitLoad

const (
	loadMax = 2           // 同時に裏で読む本数。超えたら同期で読む (応答しないマウントで goroutine を積み上げない)
	loadHot = time.Second // 頼んでからこの間は Animating を立てて速い周期で取り込む。過ぎたら Busy の遅い周期 (止まったマウントで tick を回し続けない)
)

type loadResult struct {
	kids []*node
	err  error
}

// loadReq は裏で読んでいる 1 本。
type loadReq struct {
	n      *node
	order  sortOrder // 頼んだ時点の並べ方 (取り込むときに違えば並べ直す)
	since  time.Time
	done   chan loadResult // 1 つだけ届く
	gaveUp bool            // 次のキーの前に待ったが届かなかった (止まったマウント)。二度と待たない
}

// loadCont は読み込みを待っている打鍵の残り。descend なら潜る、そうでなければ開く (読み込み中の Space は「開く」に固定)。
type loadCont struct {
	n       *node
	descend bool
}

// loadWG は裏の読み込みの goroutine (テストが終わりを待つ。Close は待たない: 応答しないマウントで閉じられなくなる)。
var loadWG sync.WaitGroup

// requestLoad は n を読む。読み込み済みなら何もせず true。LoadWait のうちに読めて取り込んだら true。
// 間に合わなかった (または既に裏で読んでいる) なら、続きを覚えて false (呼び出し側は打鍵の残りをしない)。
func (m *Model) requestLoad(n *node, descend bool) bool {
	if !n.dir || n.loaded {
		return true
	}
	if r := m.pendingLoad(n); r != nil {
		m.loadCont = &loadCont{n: n, descend: descend}
		return false
	}
	if len(m.loads) >= loadMax {
		m.ensureLoaded(n)
		return true
	}
	r := &loadReq{n: n, order: order, since: m.now(), done: make(chan loadResult, 1)}
	read := m.loadRead
	if read == nil {
		bulk := readBulk // goroutine の中で readBulk を読まない (テストの差し替えと競合する)
		read = func(dir string) ([]dirEntry, error) { return readEntriesVia(bulk, dir) }
	}
	loadWG.Add(1)
	go func() {
		defer loadWG.Done()
		kids, err := readKids(n, read, r.order)
		r.done <- loadResult{kids, err}
	}()
	if res, ok := waitLoad(r.done, LoadWait); ok {
		m.applyLoad(r, res)
		return true
	}
	m.loads = append(m.loads, r)
	m.loadCont = &loadCont{n: n, descend: descend}
	return false
}

func waitLoad(done <-chan loadResult, d time.Duration) (loadResult, bool) {
	if d < 0 {
		return <-done, true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case res := <-done:
		return res, true
	case <-t.C:
		return loadResult{}, false
	}
}

func (m *Model) pendingLoad(n *node) *loadReq {
	for _, r := range m.loads {
		if r.n == n {
			return r
		}
	}
	return nil
}

// applyLoad は届いた結果を木に付ける。その間に同期で読まれていたら捨てる。
func (m *Model) applyLoad(r *loadReq, res loadResult) {
	n := r.n
	if n.loaded {
		return
	}
	if res.err == nil && r.order != order {
		sortKids(res.kids, order) // 待つ間に並べ方が変わった
	}
	n.setKids(res.kids, res.err)
	if n.readErr != nil && m.inTree(n) { // 待つ間に木から外れたフォルダ (ゴミ箱へ移した・rename) の失敗は知らせない
		m.fail("読めません: " + n.name)
	}
}

// flushLoad は読み込みを待っている打鍵の残りを、次のキーより先に済ませる。
func (m *Model) flushLoad() {
	c := m.loadCont
	if c == nil {
		return
	}
	r := m.pendingLoad(c.n)
	if r == nil {
		m.loadCont = nil
		return
	}
	var res loadResult
	select {
	case res = <-r.done: // 既に届いている (諦めた後・上限の後でも、届いていれば続きを済ませる。tick より先にキーが来た窓)
	default:
		d := loadFlushWait - m.now().Sub(r.since)
		if r.gaveUp || d <= 0 {
			r.gaveUp, m.loadCont = true, nil
			return
		}
		var ok bool
		if res, ok = loadWaiter(r.done, d); !ok {
			r.gaveUp, m.loadCont = true, nil // 止まったマウント: 先打ちのキーは今の状態に効かせる。読み込みは届いたら Advance で取り込む
			return
		}
	}
	m.dropLoad(r)
	m.applyLoad(r, res)
	m.continueLoad(c.n)
}

func (m *Model) dropLoad(r *loadReq) {
	i := slices.Index(m.loads, r)
	m.loads = slices.Delete(m.loads, i, i+1)
}

// takeLoads は届いた結果を取り込み、続きをする (Advance から)。
func (m *Model) takeLoads() {
	kept := m.loads[:0]
	for _, r := range m.loads {
		select {
		case res := <-r.done:
			m.applyLoad(r, res)
			m.continueLoad(r.n)
		default:
			kept = append(kept, r)
		}
	}
	clear(m.loads[len(kept):]) // 外した記録が node を掴み続けない
	m.loads = kept
}

func (m *Model) continueLoad(n *node) {
	c := m.loadCont
	if c == nil || c.n != n {
		return
	}
	m.loadCont = nil
	if m.cur != n || m.OwnsKeys() || m.frontTile() != nil {
		return
	}
	if c.descend {
		m.descendLoaded()
		return
	}
	n.expanded = true
	m.moving = true
}

// loadsHot は頼んでから loadHot 以内の読み込みがあるか (Animating に足す)。
func (m *Model) loadsHot() bool {
	now := m.now()
	for _, r := range m.loads {
		if now.Sub(r.since) < loadHot {
			return true
		}
	}
	return false
}
