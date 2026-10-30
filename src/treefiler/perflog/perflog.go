// Package perflog は bin/treefiler の打鍵から端末への書き出しまでの所要を、追記型のログに 1 行ずつ書く (TREEFILER_PERFLOG)。
// 読むのは tools/tfperf (隔離した tmux から打鍵し、送った時刻と組にして集計する)。
//
// 行は `<unix ns>\t<kind>\t<値...>`。時刻は壁時計の unix ns で、tfperf が打鍵を送った時刻と同じ時計で比べる。
//
//	update <ns> <msg> <key> <anim>  Update 1 回の所要。msg は key / tick / other、anim は Update の後に動き (Animating) が続くか (1/0)
//	view   <ns> <bytes>             View 1 回の所要と、組み立てた画面の大きさ
//	write  <ns> <bytes>             端末への書き込み 1 回 (bubbletea の flush) の所要と大きさ。読み手 (端末・tmux) が詰まると ns が伸びる
//
// 行頭の時刻は各処理が終わった時刻。
//
// 🚨 bubbletea の書き込みは描画の goroutine が自分の周期 (60fps) で行い、1 周期に 2 回 (端末への指示の列と画面の差分) 書くことがある。
// write の行の数はフレームの数より多くなりうる。
package perflog

import (
	"bufio"
	"fmt"
	"os"
	"sync"
	"time"
)

// Log は nil のとき何もしない (ログを取らない起動では呼び出し側に分岐を書かせない)。
type Log struct {
	mu      sync.Mutex // Update と View は bubbletea の同じ goroutine、write は描画の goroutine から来る
	f       *os.File
	w       *bufio.Writer
	now     func() time.Time
	started bool // 最初の view を書き出した (tfperf が起動を見届ける合図)
}

// Open は path へ追記する。now は時計 (main が time.Now を渡す。filer の外でも「今」を直に読まない)。
func Open(path string, now func() time.Time) (*Log, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Log{f: f, w: bufio.NewWriterSize(f, logBuffer), now: now}, nil
}

// Now は時計を読む (nil のときは零値。測らない起動で time.Now を呼ばない)。
func (l *Log) Now() time.Time {
	if l == nil {
		return time.Time{}
	}
	return l.now()
}

// Since は start からの所要 (ns)。
func (l *Log) Since(start time.Time) int64 { return l.now().Sub(start).Nanoseconds() }

// logBuffer は行を溜める大きさ。ファイルへ書き出すのは最初の view・溜まりきったとき・Close だけにする
// (打鍵や tick ごとに書き出すと、その書き込みが event loop の上で次の打鍵を待たせ、測る所要に入る)。
// 1 行は 50 byte 前後で、tfperf の 1 run (数十打) は数百 KB に収まる。
const logBuffer = 4 << 20

// Event は 1 行書く。
func (l *Log) Event(kind string, vals ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%d\t%s", l.now().UnixNano(), kind)
	for _, v := range vals {
		fmt.Fprintf(l.w, "\t%v", v)
	}
	l.w.WriteByte('\n') //nolint:errcheck // bufio の誤りは Flush で返る
	if kind == "view" && !l.started {
		l.started = true
		l.w.Flush() //nolint:errcheck // 誤りは Close の Flush で返る
	}
}

// Flush は溜めた行をファイルへ書く。
func (l *Log) Flush() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Flush()
}

// Close は書き出して閉じる。
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	err := l.Flush()
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Output は端末への書き込みを測る。*os.File を埋め込むので、bubbletea は Fd から端末として扱い続ける
// (term.File を満たさない writer を渡すと、bubbletea は端末の大きさを取れず raw の扱いも変わる)。
type Output struct {
	*os.File
	Log *Log
}

func (o Output) Write(p []byte) (int, error) {
	start := o.Log.now()
	n, err := o.File.Write(p)
	o.Log.Event("write", o.Log.Since(start), n)
	return n, err
}
