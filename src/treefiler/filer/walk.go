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
}

type walker struct {
	mu sync.Mutex
	// results は公開した後に書き換えない (取り込み側へ写さずに渡すため)。更新は新しい map を作って差し替える
	results map[string]walkResult
	queue   []string // 末尾が新しい依頼。末尾から取る (新しく頼んだものから先に処理する)
	asked   map[string]bool
	running bool
	version int // 結果が増えるたびに進める (取り込み側が変化を知るため)
}

func newWalker() *walker {
	return &walker{results: map[string]walkResult{}, asked: map[string]bool{}}
}

// request は dir の走査を頼む (頼んだことがあれば何もしない)。
func (w *walker) request(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.asked[dir] {
		return
	}
	w.asked[dir] = true
	if r, ok := w.results[dir]; ok && r.complete {
		return // 親の走査で、もう最後まで数えてある (見えている子ごとに 5 万件の予算で数え直さない)
	}
	// 🚨 先頭へ差し込む形 (append([]string{dir}, queue...)) にしない: 毎回全体を写して O(n²) になり、
	// 子フォルダ 2 万個で 0.66 秒 UI が固まった (レビューで実測 2026-10-08)
	w.queue = append(w.queue, dir)
	if !w.running {
		w.running = true
		go w.loop()
	}
}

// forget は dir とその祖先・子孫の結果を捨て、次に頼まれたら走査し直す (読み直しのとき)。
func (w *walker) forget(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	related := func(p string) bool {
		return p == dir || strings.HasPrefix(dir, p+string(filepath.Separator)) || strings.HasPrefix(p, dir+string(filepath.Separator))
	}
	for p := range w.asked {
		if related(p) {
			delete(w.asked, p)
		}
	}
	next := make(map[string]walkResult, len(w.results))
	for p, r := range w.results {
		if !related(p) {
			next[p] = r
		}
	}
	w.results = next
	w.version++
}

func (w *walker) busy() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// snapshot は結果と版を返す (版が同じなら何もしない)。返す map は公開後に書き換えないので写さない。
func (w *walker) snapshot(have int) (map[string]walkResult, int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if have == w.version {
		return nil, have, false
	}
	return w.results, w.version, true
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
			w.mu.Unlock()
			return
		}
		dir := w.queue[len(w.queue)-1]
		w.queue = w.queue[:len(w.queue)-1]
		w.mu.Unlock()
		budget := walkCap
		found := map[string]walkResult{}
		walkDir(dir, &budget, found)
		w.mu.Lock()
		next := make(map[string]walkResult, len(w.results)+len(found))
		for k, v := range w.results {
			next[k] = v
		}
		for k, v := range found {
			// 完了した結果を、途中で打ち切った結果で上書きしない
			if old, ok := next[k]; ok && old.complete && !v.complete {
				continue
			}
			next[k] = v
		}
		w.results = next
		w.version++
		w.mu.Unlock()
	}
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
