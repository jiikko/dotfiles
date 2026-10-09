package filer

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// walk.go はフォルダの配下でいちばん新しい更新を裏で数える (熱の色の元。spec §5.1)。
//
// 🚨 走査は裏の goroutine 1 本で、結果は Model.Advance で取り込む (描画の経路で I/O をしない)。
// symlink は辿らない (lstat。木と同じ)。1 回の走査はエントリ walkCap 件で打ち切り、未完として残す。

const walkCap = 50_000

type walkResult struct {
	newest   time.Time // 配下全部 (フォルダ自身を含む) の最新
	vis      time.Time // dotfile でないファイルだけの最新 (dotfile を隠している間の色に使う)
	bytes    int64
	complete bool
	stale    bool // 数えた後に中が変わった (値は新しい結果が来るまで残す。熱の色と大きさを一時的に消さない)
}

// restaleInterval は古くなったフォルダを数え直す最短の間隔。毎秒書き込まれるフォルダ (ログ) があると、そのたびに
// root から 5 万件を数え直すことになるので間を空ける (読み直しの r は待たない)。数え直すまでの間は古い値で描く。
const restaleInterval = 5 * time.Second

type walker struct {
	mu      sync.Mutex
	results map[string]walkResult // walker の中だけで持つ (公開しない)
	// delta は取り込み側がまだ取っていない結果。取り込み側は自分の map へ足す
	// 🚨 全体の map を結果ごとに写して公開しない: 子フォルダが多いと 2 乗で伸び、直下 8000 フォルダで 1.87 秒かかった (issue 693)
	delta     map[string]walkResult
	queue     []string // 末尾が新しい依頼。末尾から取る (新しく頼んだものから先に処理する)
	asked     map[string]bool
	last      map[string]time.Time // 最後に数え終えた時刻 (古くなったものを数え直す間隔に使う)
	forgotLog []forgotten          // 走査の途中に古くなったフォルダ (走っている走査の結果を古い印のまま書くため)
	// deferred は間を空けて数え直すのを待っているフォルダと、その期限。待っている間は busy にする
	// 🚨 busy にしないと呼び出し側の tick が止まり、次のキーまで誰も頼み直さず、古い値のまま残った (レビューで再現 2026-10-09)
	deferred map[string]time.Time
	now      func() time.Time // 時計 (テストが進める。数え直しの間隔を実時間で待たない)
	running  bool
	version  int // 結果が増える・古くなるたびに進める (取り込み側が変化を知るため)
}

type forgotten struct {
	dir   string
	force bool // 読み直し (r)。その走査の結果は「数えた時刻」を書かない (数え直しの間隔を待たせない)
}

// deferGrace は期限を過ぎても誰も頼み直さない待ちを捨てるまでの猶予 (見えなくなったフォルダの待ちで tick を回し続けない)。
const deferGrace = time.Second

func newWalker() *walker {
	return &walker{results: map[string]walkResult{}, delta: map[string]walkResult{}, asked: map[string]bool{},
		last: map[string]time.Time{}, deferred: map[string]time.Time{}, now: time.Now}
}

// request は dir の走査を頼む (頼んだことがあれば何もしない)。最後まで数えた新しい結果があれば数えない。
func (w *walker) request(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.asked[dir] {
		return
	}
	r, ok := w.results[dir]
	if ok && r.complete && !r.stale {
		w.asked[dir] = true
		return // 親の走査で、もう最後まで数えてある (見えている子ごとに 5 万件の予算で数え直さない)
	}
	if ok && r.stale && w.now().Sub(w.last[dir]) < restaleInterval {
		w.deferred[dir] = w.last[dir].Add(restaleInterval) // 間を空けて、次に頼まれたときに数え直す (asked にしない)
		return
	}
	delete(w.deferred, dir)
	w.asked[dir] = true
	// 🚨 先頭へ差し込む形 (append([]string{dir}, queue...)) にしない: 毎回全体を写して O(n²) になり、
	// 子フォルダ 2 万個で 0.66 秒 UI が固まった (レビューで実測 2026-10-08)
	w.queue = append(w.queue, dir)
	if !w.running {
		w.running = true
		go w.loop()
	}
}

func related(p, dir string) bool {
	return p == dir || strings.HasPrefix(dir, withSep(p)) || strings.HasPrefix(p, withSep(dir))
}

// forget は dir とその祖先・子孫の結果に古い印を付け、次に頼まれたら数え直す (ライブ更新・読み直し)。
// 値は消さない (新しい結果が来るまで古い値で描く)。force (読み直しの r) は数え直しの間隔を待たない。
func (w *walker) forget(dir string, force bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for p := range w.asked {
		if related(p, dir) {
			delete(w.asked, p)
		}
	}
	for p, r := range w.results {
		if related(p, dir) {
			r.stale = true
			w.results[p], w.delta[p] = r, r
			if force {
				delete(w.last, p)
			}
		}
	}
	if w.running {
		w.forgotLog = append(w.forgotLog, forgotten{dir, force})
	}
	w.version++
}

// busy は走査中か、間を空けて数え直すのを待っているか。期限を猶予だけ過ぎても頼み直されない待ちは捨てる。
func (w *walker) busy() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	for d, due := range w.deferred {
		if now.After(due.Add(deferGrace)) {
			delete(w.deferred, d)
		}
	}
	return w.running || len(w.deferred) > 0
}

// take は取り込んでいない結果と版を返す (版が同じなら何もしない)。返した map は walker から手放す。
func (w *walker) take(have int) (map[string]walkResult, int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if have == w.version {
		return nil, have, false
	}
	d := w.delta
	w.delta = map[string]walkResult{}
	return d, w.version, true
}

// pending は取り込んでいない結果があるか。
func (w *walker) pending(have int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return have != w.version
}

func (w *walker) loop() {
	for {
		w.mu.Lock()
		if len(w.queue) == 0 {
			w.running = false
			w.forgotLog = nil
			w.mu.Unlock()
			return
		}
		dir := w.queue[len(w.queue)-1]
		w.queue = w.queue[:len(w.queue)-1]
		if r, ok := w.results[dir]; ok && r.complete && !r.stale {
			w.mu.Unlock()
			continue // 頼んだ後に親の走査で数え終わった
		}
		mark := len(w.forgotLog)
		w.mu.Unlock()
		budget := walkCap
		found := map[string]walkResult{}
		walkDir(dir, &budget, found)
		w.mu.Lock()
		w.store(found, w.forgotLog[mark:], w.now())
		w.mu.Unlock()
	}
}

// store は 1 回の走査の結果を書く (w.mu を持って呼ぶ)。forgot は走査の途中に古くなったフォルダ。
func (w *walker) store(found map[string]walkResult, forgot []forgotten, now time.Time) {
	for k, v := range found {
		// 完了した新しい結果を、途中で打ち切った結果で上書きしない
		if old, ok := w.results[k]; ok && old.complete && !old.stale && !v.complete {
			continue
		}
		forced := false
		for _, f := range forgot {
			if related(k, f.dir) {
				v.stale = true // 数えている間に中が変わった (この結果は古いかもしれない)
				forced = forced || f.force
			}
		}
		w.results[k], w.delta[k] = v, v
		delete(w.deferred, k)
		if forced {
			delete(w.last, k) // 走査中に r が来た: 数え直しの間隔を待たせない
		} else {
			w.last[k] = now
		}
	}
	w.version++
}

// walkDir は dir の配下を深さ優先で数え、通ったフォルダごとの結果を found に入れる。
func walkDir(dir string, budget *int, found map[string]walkResult) walkResult {
	var r walkResult
	if info, err := os.Lstat(dir); err == nil {
		r.newest = info.ModTime()
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		r.complete = true // 読めないフォルダは自分の mtime だけ (待っても変わらない)
		found[dir] = r
		return r
	}
	r.complete = true
	for _, e := range ents {
		if *budget <= 0 {
			r.complete = false
			break
		}
		*budget--
		info, err := e.Info()
		if err != nil {
			continue
		}
		hidden := strings.HasPrefix(e.Name(), ".")
		if info.ModTime().After(r.newest) {
			r.newest = info.ModTime()
		}
		if e.IsDir() {
			sub := walkDir(filepath.Join(dir, e.Name()), budget, found)
			if sub.newest.After(r.newest) {
				r.newest = sub.newest
			}
			if !hidden && sub.vis.After(r.vis) {
				r.vis = sub.vis
			}
			r.bytes += sub.bytes
			if !sub.complete {
				r.complete = false
			}
			continue
		}
		r.bytes += info.Size()
		if !hidden && info.ModTime().After(r.vis) {
			r.vis = info.ModTime()
		}
	}
	found[dir] = r
	return r
}
