package filer

import (
	"os"
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
			sig := readSig(d)
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
		w.mu.Lock()
		for d, sig := range next {
			if sig != nil {
				w.seen[d] = sig
			}
		}
		for d := range w.seen {
			if _, ok := next[d]; !ok {
				delete(w.seen, d) // 閉じたフォルダは忘れる (開き直したら木の中身を基準にし直す)
			}
		}
		w.pending = append(w.pending, found...)
		w.mu.Unlock()
		if len(found) > 0 {
			select {
			case ch <- struct{}{}:
			default: // 既に合図が溜まっている
			}
		}
	}
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
