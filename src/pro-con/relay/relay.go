// Package relay は画面の中継 (issue 443): 画面は描くたびに最新の 1 枚を状態の置き場の relay/<id>.frame に置き、
// 外の Claude が `pro-con screen` で読む。
//
//   - 画面は開いている間 relay/<id>.lock を flock で持つ。読む側は flock が持たれている画面だけを「開いている」とみなす
//     (落ちた画面の flock は OS が外す)。読む側は何も消さない・書かない (441 の守ること 1)
//   - 描画を待たせない (441 の守ること 3): Put は最新の 1 枚を置き場 (メモリ) に置いて戻るだけ。書き出しは別の goroutine が、
//     最短 minInterval ごとに最新の 1 枚だけを書く (途中の枚は捨てる)。書き出しが詰まっても Put は待たない
//   - 自分だけ (441 の守ること 2): relay/ は 0700 (symlink なら使わない)、ファイルは 0600。書きかけは読ませない (一時ファイルに書いて rename)
//   - 中身 (文字・大きさ・状態) が変わらない描き直し (1 秒の tick 等) は書かない。Frame.At は最後に中身が変わった時刻
//   - 落ちた画面 (端末を閉じた・kill -9) の残りは、次に開く画面が片付ける (flock の外れた印とその 1 枚だけ)。読む側は消さない
//
// 画面の印 (package presence) とは別の置き場にする: 見ているだけの画面 (--view) は presence の印を置かない
// (置くと、ほかの画面の「最後の画面か」の数えに入る) が、中継はする。
package relay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Dir は置き場の中の中継の置き場。
const Dir = "relay"

// minInterval は書き出しの最短の間隔 (演出の 1 コマごとには書かない。最後の 1 枚は必ず書く)。
var minInterval = 100 * time.Millisecond

// Frame は画面 1 枚。
type Frame struct {
	ID     string    `json:"id"`
	View   bool      `json:"view"` // 見ているだけの画面 (--view)
	PID    int       `json:"pid"`
	At     time.Time `json:"at"` // 最後に中身が変わった時刻 (中身が同じ描き直しでは進まない)
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Plain  string    `json:"plain"` // 色を落とした文字 (書き出しの側で ANSI から作る。Put は ANSI だけ渡す)
	ANSI   string    `json:"ansi"`  // 端末へ出したままの文字 (色つき)
	// State は画面の状態 (タブ・選択・モード等)。画面が決める
	State map[string]string `json:"state,omitempty"`
}

// Writer は画面 1 つの中継の書き手。
type Writer struct {
	dir, id  string
	lock     *os.File
	lockPath string
	write    func(path string, data []byte) error // テストが差し替える (書き出しが詰まっても Put が待たないことを見る)

	mu      sync.Mutex
	latest  *Frame
	written *Frame // 最後に書いた 1 枚 (中身が同じなら書かない。書き出しの goroutine だけが触る)
	wake    chan struct{}
	done    chan struct{}
	wg      sync.WaitGroup
	closed  bool
}

// Open は置き場 dir にこの画面の中継を置く (flock を持つ)。
func Open(dir string) (*Writer, error) {
	d := filepath.Join(dir, Dir)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, err
	}
	if st, err := os.Lstat(d); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("中継の置き場 %s がディレクトリではない (symlink は使わない)", d)
	}
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(b[:])
	// 🚨 一時名で作って flock を取ってから印の名前にする。印の名前で作ってから flock すると、その間に読む側 (held) が共有の flock を
	// 取れて、この画面の flock が EWOULDBLOCK で失敗する (敵対的レビュー 2026-09-25 P2。presence と同じ形)
	tmp := filepath.Join(d, ".tmp-"+id)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	lock := filepath.Join(d, id+".lock")
	if err := os.Rename(tmp, lock); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	sweep(d)
	w := &Writer{dir: d, id: id, lock: f, lockPath: lock, write: writeAtomic, wake: make(chan struct{}, 1), done: make(chan struct{})}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// ID はこの画面の中継の id。
func (w *Writer) ID() string { return w.id }

// Put は最新の 1 枚を置く。待たない (書き出しは別の goroutine)。
func (w *Writer) Put(f Frame) {
	f.ID, f.PID = w.id, os.Getpid()
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.latest = &f
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default: // もう起こしてある (書き出しは最新の 1 枚を取る)
	}
}

func (w *Writer) loop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case <-w.wake:
		}
		w.mu.Lock()
		f := w.latest
		w.latest = nil
		w.mu.Unlock()
		if f != nil && w.written != nil && sameContent(*f, *w.written) {
			f = nil // 中身が変わらない描き直し (tick 等) は書かない。At も進めない
		}
		if f != nil {
			f.Plain = ansi.Strip(f.ANSI) // 描画の側でやらない (画面を待たせない)
			if data, err := json.Marshal(f); err == nil && w.write(filepath.Join(w.dir, w.id+".frame"), data) == nil {
				// 🚨 書けたときだけ「最後に書いた 1 枚」にする。書く前に置くと、書き出しに失敗した 1 枚と同じ中身の描き直しを
				// 「もう書いた」と飛ばし、古い 1 枚が次に中身が変わるまで残る (敵対的レビュー 2026-09-25 2 周目 P2)
				w.written = f
			} // 書けなくても画面は止めない (中継は見るための口。次の描き直しで書き直す)
		}
		select {
		case <-w.done:
			return
		case <-time.After(minInterval):
		}
	}
}

// Close は中継をやめ、この画面のファイルを消す (書き出しの途中なら、その 1 枚を書き終えるまで待つ)。
func (w *Writer) Close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.mu.Unlock()
	close(w.done)
	w.wg.Wait()
	_ = os.Remove(filepath.Join(w.dir, w.id+".frame"))
	_ = os.Remove(w.lockPath)
	_ = syscall.Flock(int(w.lock.Fd()), syscall.LOCK_UN)
	_ = w.lock.Close()
}

func sameContent(a, b Frame) bool {
	if a.ANSI != b.ANSI || a.View != b.View || a.Width != b.Width || a.Height != b.Height || len(a.State) != len(b.State) {
		return false
	}
	for k, v := range a.State {
		if b.State[k] != v {
			return false
		}
	}
	return true
}

// staleTmp は一時ファイル (.tmp-*) を落ちた画面の残りとみなすまでの時間 (作ってから rename するまでは一瞬。presence と同じ)。
const staleTmp = time.Minute

// sweep は落ちた画面の残り (flock の外れた印と、その 1 枚・印の無い 1 枚・古い一時ファイル) を消す。開く画面だけが呼ぶ (読む側は消さない)。
// 印は一時名で flock を取ってから rename するので、開いている画面の印が「外れている」に見えることは無い。
// 🚨 消すのは relay/ の中の <id>.lock と <id>.frame だけ (名前で絞る。ほかのファイル・ディレクトリには触らない)
func sweep(d string) {
	es, err := os.ReadDir(d)
	if err != nil {
		return
	}
	alive := map[string]bool{}
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".tmp-") && e.Type().IsRegular() { // 作ってから rename するまでの間に落ちた残り。作っている最中のもの (新しい) には触らない
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTmp {
				_ = os.Remove(filepath.Join(d, e.Name()))
			}
		}
	}
	for _, e := range es {
		if id, ok := strings.CutSuffix(e.Name(), ".lock"); ok && !strings.HasPrefix(id, ".") && e.Type().IsRegular() {
			p := filepath.Join(d, e.Name())
			if h, err := held(p); err != nil || h {
				alive[id] = true // 持たれている (または確かめられない) 印は消さない
				continue
			}
			_ = os.Remove(p)
		}
	}
	for _, e := range es {
		if id, ok := strings.CutSuffix(e.Name(), ".frame"); ok && !alive[id] && e.Type().IsRegular() {
			_ = os.Remove(filepath.Join(d, e.Name()))
		}
	}
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path) // CreateTemp は 0600 で作る
}

// Screen は読む側から見た画面 1 つ。
type Screen struct {
	ID   string
	Open bool   // flock が持たれている (画面が開いている)。偽なら落ちた画面の残り
	Path string // 最新の 1 枚のファイル (まだ 1 枚も書いていなければ空)
}

// List は置き場 dir の中継を並べる (id の順)。何も書かない。
func List(dir string) ([]Screen, error) {
	d := filepath.Join(dir, Dir)
	es, err := os.ReadDir(d)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Screen
	for _, e := range es {
		id, ok := strings.CutSuffix(e.Name(), ".lock")
		if !ok || strings.HasPrefix(id, ".") || !e.Type().IsRegular() { // 普通のファイルでない印 (FIFO 等) は開かない (止まる)
			continue
		}
		open, err := held(filepath.Join(d, e.Name()))
		if err != nil {
			continue // 確かめられない印 1 つで一覧全体を失敗させない (--follow が開いている画面を「閉じた」と読む)
		}
		s := Screen{ID: id, Open: open}
		if _, err := os.Stat(filepath.Join(d, id+".frame")); err == nil {
			s.Path = filepath.Join(d, id+".frame")
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// held は flock が持たれているか。読むだけで開く (O_RDONLY)。持たれていなければ共有の flock が一瞬取れるので、すぐ外す
// (ファイルの中身も時刻も変えない)。
func held(p string) (bool, error) {
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("中継の印 %s を確かめられない: %w", p, err)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}

// Read は 1 枚を読む。
func Read(path string) (Frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return Frame{}, fmt.Errorf("中継の 1 枚 (%s) を読めない: %w", path, err)
	}
	return f, nil
}
