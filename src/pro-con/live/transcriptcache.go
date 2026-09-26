package live

import (
	"os"
	"sync"
	"time"
)

// TranscriptCache は transcript の末尾を読んだ結果を、ファイルの大きさと更新時刻が変わるまで使い回す (issue 503)。
// 画面の読み直し (Backend.transcript) と dispatcher の tick (dispatchercmd.go の Transcript) が使う。複数の goroutine から呼んでよい。
// 🚨 返す Transcript のスライスは呼び元どうしで共有する (書き換えない)。
type TranscriptCache struct {
	Read func(path string) (Transcript, error) // 読み方 (nil なら ReadTail)

	mu  sync.Mutex
	seq uint64
	m   map[string]cachedTranscript
}

type cachedTranscript struct {
	size  int64
	mtime time.Time
	t     Transcript
	used  uint64 // 最後に使った順番 (上限を超えたら一番古いものを捨てる)
}

// transcriptCacheMax は覚えておく transcript の数。dispatcher は何日も動くので、終わった session の分を溜め続けない。
const transcriptCacheMax = 64

// Get は path の transcript の末尾。前に読んだときから大きさも更新時刻も変わっていなければ読み直さない。
func (c *TranscriptCache) Get(path string) (Transcript, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Transcript{}, err
	}
	c.mu.Lock()
	if e, ok := c.m[path]; ok && e.size == st.Size() && e.mtime.Equal(st.ModTime()) {
		c.seq++
		e.used = c.seq
		c.m[path] = e
		c.mu.Unlock()
		return e.t, nil
	}
	c.mu.Unlock()
	read := c.Read
	if read == nil {
		read = ReadTail
	}
	t, err := read(path) // 読んでいる間は lock を持たない (別の session の読み取りを待たせない)
	if err != nil {
		return Transcript{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]cachedTranscript{}
	}
	c.seq++
	c.m[path] = cachedTranscript{size: st.Size(), mtime: st.ModTime(), t: t, used: c.seq}
	for len(c.m) > transcriptCacheMax {
		oldest, at := "", c.seq+1
		for p, e := range c.m {
			if e.used < at {
				oldest, at = p, e.used
			}
		}
		delete(c.m, oldest)
	}
	return t, nil
}
