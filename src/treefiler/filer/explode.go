package filer

import (
	"os"
	"path/filepath"
	"sync"
)

// explode.go は `e` (配下のフォルダを全部開く。spec §5.4)。裏で幅優先に辿り、終わったら一斉に開く。
// 隠したフォルダと git が無視するフォルダには降りない (対象のフォルダ自身は開く)。symlink は辿らない。

const (
	explodeMaxDirs    = 400
	explodeMaxEntries = 6000
)

type explodeJob struct {
	mu       sync.Mutex
	target   string
	dirs     []string // 開くフォルダ (幅優先の順)
	stopped  bool     // 上限で止めた
	done     bool
	canceled bool
}

func (j *explodeJob) cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.canceled = true
}

func (j *explodeJob) finished() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done
}

func (j *explodeJob) count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.dirs)
}

// startExplode は target の配下を辿り始める。skip は降りないフォルダの判定 (隠す・無視する)。
func startExplode(target string, skip func(path, name string) bool) *explodeJob {
	j := &explodeJob{target: target}
	go func() {
		queue := []string{target}
		entries := 0
		var out []string
		stopped := false
		for len(queue) > 0 {
			j.mu.Lock()
			canceled := j.canceled
			j.mu.Unlock()
			if canceled {
				break
			}
			if len(out) >= explodeMaxDirs || entries >= explodeMaxEntries {
				stopped = true
				break
			}
			d := queue[0]
			queue = queue[1:]
			ents, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			out = append(out, d)
			entries += len(ents)
			for _, e := range ents {
				if e.IsDir() && !skip(filepath.Join(d, e.Name()), e.Name()) {
					queue = append(queue, filepath.Join(d, e.Name()))
				}
			}
			j.mu.Lock()
			j.dirs = append([]string(nil), out...)
			j.mu.Unlock()
		}
		j.mu.Lock()
		j.dirs, j.stopped, j.done = out, stopped, true
		j.mu.Unlock()
	}()
	return j
}

func (m *Model) explode() {
	if m.exploding != nil {
		return
	}
	target := m.cur // フォルダの上でだけ呼ばれる (ファイルの e はエディタ。model.go の treeKeys)
	showHidden, intoIgnored := m.set.ShowHidden, m.set.ExplodeIgnore
	snap := m.gitSnap
	m.exploding = startExplode(target.path(), func(path, name string) bool {
		if !showHidden && hiddenName(name) {
			return true
		}
		return !intoIgnored && snap.state(path, true) == gitIgn
	})
}

// takeExplode は辿り終えた explode を取り込み、見つけたフォルダを一斉に開く。
func (m *Model) takeExplode() {
	j := m.exploding
	if j == nil || !j.finished() {
		return
	}
	m.exploding = nil
	if j.canceled {
		return
	}
	for _, d := range j.dirs {
		n := m.loadPath(d)
		if n != nil && n.dir {
			n.expanded = true
		}
	}
	msg := "開いた: " + itoa(len(j.dirs)) + " フォルダ"
	if j.stopped {
		msg = "最初の " + itoa(len(j.dirs)) + " フォルダを開いた (上限で止めた)"
	}
	m.notices = append(m.notices, Notice{Text: msg, OK: true})
	m.moving = true
}

// loadPath は root の中の絶対パスの項目を、途中のフォルダを (読めなくても黙って) 読み込みながら探す。フォルダなら中も読む。
func (m *Model) loadPath(abs string) *node {
	n := m.lookup(abs, (*node).load)
	if n != nil && n.dir && !n.loaded {
		n.load()
	}
	return n
}
