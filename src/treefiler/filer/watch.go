package filer

import (
	"os"
	"slices"
	"sync"
	"time"
)

// watch.go はライブ更新 (spec §5.2)。開いているフォルダを watchEvery ごとに読み比べ、変わったら知らせる。
//
// 🚨 fsnotify は使わない: macOS の kqueue はフォルダの中のファイルごとに fd を開くので、大きなフォルダを開くと
// fd の上限 (既定 256) に当たりうる。treebeard と同じポーリングにする。
// 🚨 呼び出し側を起こすのは Changed のチャネルだけ (変化が無い間は tick を回さない。glogx の方針)。

const watchEvery = time.Second

type entrySig struct {
	mtime time.Time
	size  int64
	dir   bool
}

type dirChange struct {
	dir     string
	changed []string // 増えた・変わった項目の名前
	removed []string // 消えた項目の名前 (見える項目ならフォルダ自身を光らせる)
	listing bool     // 項目が増えた・消えた
}

type watcher struct {
	mu      sync.Mutex
	want    []string
	seen    map[string]map[string]entrySig
	pending []dirChange
	ch      chan struct{}
	stop    chan struct{}
	running bool
	paused  bool // 設定の Live が off (読み比べを休む。基準は残すので、on に戻すと休んでいた間の変化も届く)
}

func newWatcher() *watcher { return &watcher{seen: map[string]map[string]entrySig{}} }

// start はポーリングを始め、変化の合図のチャネルを返す (走っていればそのまま返す)。
func (w *watcher) start() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return w.ch
	}
	w.running = true
	w.ch = make(chan struct{}, 1)
	w.stop = make(chan struct{})
	go w.loop(w.stop, w.ch)
	return w.ch
}

// close はポーリングを止め、合図のチャネルを閉じる (待っている呼び出し側を起こして終わらせる)。
func (w *watcher) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	w.running = false
	close(w.stop)
	// 基準と溜まった変化を捨てる: 開き直すと木は読み直される (Refresh) ので、閉じる前の基準と比べると閉じていた間の変化が
	// 偽の光で出た (監査で再現 2026-10-09。issue 691)。開き直した後の setDirs が木の中身から基準を取り直す
	w.seen = map[string]map[string]entrySig{}
	w.pending = nil
}

func (w *watcher) pause(p bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused = p
}

// setDirs は見るフォルダを差し替える (開いているフォルダ)。base は初めて見るフォルダの基準 (木が読み込んだときの中身)。
// 🚨 基準をポーリングの最初の観測にしない: 読み込んでから最初の観測 (最大 1 秒後) までの変化を取りこぼす (レビューの指摘 2026-10-08)
func (w *watcher) setDirs(dirs []string, base func(dir string) map[string]entrySig) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.want = dirs
	for _, d := range dirs {
		if _, ok := w.seen[d]; !ok {
			w.seen[d] = base(d)
		}
	}
}

// current は今の合図のチャネル (走っていなければ nil)。始めはしない。
func (w *watcher) current() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return nil
	}
	return w.ch
}

func (w *watcher) take() []dirChange {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.pending
	w.pending = nil
	return p
}

func (w *watcher) loop(stop chan struct{}, ch chan struct{}) {
	defer close(ch)
	t := time.NewTicker(watchEvery)
	defer t.Stop()
	metas := map[string]dirMeta{} // この loop だけが使う (開き直すと作り直す)
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		w.mu.Lock()
		dirs := append([]string(nil), w.want...)
		paused := w.paused
		w.mu.Unlock()
		if paused {
			continue
		}
		var found []dirChange
		next := make(map[string]map[string]entrySig, len(dirs))
		for _, d := range dirs {
			var mtime time.Time
			if info, err := os.Lstat(d); err == nil {
				mtime = info.ModTime()
			}
			meta, read := metas[d].next(mtime)
			metas[d] = meta
			if !read {
				continue // 基準はそのまま (commit は今の want に在る基準を消さない)
			}
			sig := readSig(d)
			metas[d] = dirMeta{mtime: mtime, entries: len(sig)}
			next[d] = sig
			w.mu.Lock()
			old, ok := w.seen[d]
			w.mu.Unlock()
			if !ok || sig == nil {
				continue
			}
			if c, changed := diffSig(d, old, sig); changed {
				found = append(found, c)
			}
		}
		select {
		case <-stop:
			return // 止めた後の周は書き込まない (遅い ReadDir の間に開き直すと、新しいポーリングと混ざる)
		default:
		}
		if !w.commit(stop, next, found) {
			return
		}
		if len(found) > 0 {
			select {
			case ch <- struct{}{}:
			default: // 既に合図が溜まっている
			}
		}
	}
}

// bigDirEntries を超えるフォルダは、フォルダ自身の mtime が変わらない限り bigDirEvery 周に 1 回だけ全部を読む。
// 🚨 5 万件のフォルダは 1 回の readSig が 154ms で、毎秒読むと裏の CPU を 16% 前後使い続けた (監査で実測 2026-10-09。issue 693)。
// 項目の追加・削除・名前の変更はフォルダの mtime に出るので次の周で読む。既存のファイルの書き換えは mtime に出ないので、最大 bigDirEvery 秒遅れて映る
const (
	bigDirEntries = 5000
	bigDirEvery   = 5
)

type dirMeta struct {
	mtime   time.Time
	entries int
	skipped int
}

// next は今の周に全部を読むか (read) と、更新した記録を返す。
func (d dirMeta) next(mtime time.Time) (dirMeta, bool) {
	if d.entries <= bigDirEntries || !mtime.Equal(d.mtime) || d.skipped+1 >= bigDirEvery {
		return dirMeta{mtime: d.mtime, entries: d.entries}, true
	}
	d.skipped++
	return d, false
}

func readSig(dir string) map[string]entrySig {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sig := make(map[string]entrySig, len(ents))
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		sig[e.Name()] = entrySig{info.ModTime(), info.Size(), e.IsDir()}
	}
	return sig
}

func diffSig(dir string, old, cur map[string]entrySig) (dirChange, bool) {
	c := dirChange{dir: dir}
	for name, s := range cur {
		o, ok := old[name]
		switch {
		case !ok:
			c.listing = true
			c.changed = append(c.changed, name)
		case o != s:
			c.changed = append(c.changed, name)
		}
	}
	for name := range old {
		if _, ok := cur[name]; !ok {
			c.listing = true
			c.removed = append(c.removed, name)
		}
	}
	return c, c.listing || len(c.changed) > 0
}

// commit は 1 周の観測を基準へ書き、変化を溜める。止められていたら書かずに false (lock の中で確かめる: close は lock を
// 持って基準を空にするので、その後に古い周が書き戻さない)。
func (w *watcher) commit(stop chan struct{}, next map[string]map[string]entrySig, found []dirChange) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-stop:
		return false
	default:
	}
	for d, sig := range next {
		if sig != nil {
			w.seen[d] = sig
		}
	}
	for d := range w.seen {
		// 閉じたフォルダは忘れる (開き直したら木の中身を基準にし直す)。🚨 今の want に在るものは消さない:
		// この周の途中に setDirs が足した基準を、周の始めの一覧に無いからと消すと、開いた直後の変化を取りこぼした (issue 691)
		if _, ok := next[d]; !ok && !slices.Contains(w.want, d) {
			delete(w.seen, d)
		}
	}
	w.pending = append(w.pending, found...)
	return true
}
