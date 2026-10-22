// tfprobe は treefiler の画面 (filer.Model) を本物のディレクトリでヘッドレスに動かし、操作ごとの所要と、サイクルごとの
// 資源 (goroutine・fd・子プロセス・GC 後のヒープ) を測る。直す前と後に同じ台本で回して数字を比べるための道具
// (使い方と読み方は src/treefiler/CLAUDE.md の「実物で測る」)。
//
//	go run ./tools/tfprobe -root ~/dotfiles/src -out <dir> [-cycles 5] [-live 3000] [-cpuprofile] [-memprofile]
//
// 呼ぶのは main.go と glogx が使う公開の面 (New / Changed / Resize / HandleKey / Paste / Advance / View / Busy / Animating /
// Place / Close) だけ。端末・tea.Exec・toast は通らない。tick は main.go と同じ形 (動いている間 16ms・裏の処理を待つ間 80ms) で回す。
//
// 🚨 root を書き換えない: 台本は読むだけのキーに限る (a / m / ! / s / o は押さない。d は木の上ではゴミ箱の確認を開くので、
// タイルが開いたことを確かめてからタイルの中でだけ押す)。ライブ更新と diff は、-live の件数で
// 作った一時ディレクトリ (git の repo) だけを書き換える。設定と「前回の場所」は TREEFILER_CONFIG_DIR を一時ディレクトリへ
// 向けて、本物を読み書きしない。
//
// 🚨 計測の記録をメモリに溜めない: 行はその場で steps.tsv / cycles.tsv へ書き、メモリには段ごとの集計 (段の数で頭打ち) だけを持つ。
// 溜めると、サイクルを重ねたときの heap_objects の増分に道具自身の記録が混ざり、リークの判定に使えない (1 サイクル約 80 個)。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"golang.org/x/sys/unix"

	"subproc"
	"treefiler/filer"
)

type options struct {
	root, out, file, md, query  string
	cycles, width, height, live int
	settle                      time.Duration
	cpuprofile, memprofile      bool
}

func main() {
	var o options
	flag.StringVar(&o.root, "root", ".", "開くフォルダ")
	flag.StringVar(&o.out, "out", "", "結果を書くフォルダ (必須。無ければ作る)")
	flag.StringVar(&o.file, "file", "treefiler/filer/model.go", "タイルで開くファイル (root からの相対。無ければその段を飛ばす)")
	flag.StringVar(&o.md, "md", "treefiler/README.md", "タイルで開く Markdown (root からの相対。無ければその段を飛ばす)")
	flag.StringVar(&o.query, "query", "tr", "検索で打つ文字列 (root の直下の名前の中から探す。/ はカーソルの列の中の検索)")
	flag.IntVar(&o.cycles, "cycles", 5, "台本を回す回数 (サイクルごとに Model を作って閉じる = glogx の F の開閉)")
	flag.IntVar(&o.width, "width", 200, "画面の幅")
	flag.IntVar(&o.height, "height", 50, "画面の高さ")
	flag.IntVar(&o.live, "live", 3000, "ライブ更新の段で一時フォルダに作るファイルの数 (0 でライブ更新と diff の段を飛ばす)")
	flag.DurationVar(&o.settle, "settle", 60*time.Second, "裏の処理が落ち着くのを待つ上限 (超えたら判定不能として止める)")
	flag.BoolVar(&o.cpuprofile, "cpuprofile", false, "out に cpu.pprof を書く")
	flag.BoolVar(&o.memprofile, "memprofile", false, "out に mem.pprof (最後のサイクルの後の heap。全確保を記録する) を書く")
	flag.Parse()
	if o.out == "" || o.cycles < 1 {
		fmt.Fprintln(os.Stderr, "usage: tfprobe -root DIR -out DIR [-cycles N] (-h で一覧)")
		os.Exit(2)
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "tfprobe:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	root, err := filepath.Abs(o.root)
	if err != nil {
		return err
	}
	o.root = root
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	cfg, err := os.MkdirTemp("", "tfprobe-config-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(cfg) }() //nolint:forbidigo // 自前の一時ディレクトリ
	if err := os.Setenv("TREEFILER_CONFIG_DIR", cfg); err != nil {
		return err
	}
	if o.memprofile {
		runtime.MemProfileRate = 1 // 全確保を記録する (既定の 512KB ごとの標本では、サイクルごとに数十個ずつ残る小さいものが見えない)
	}
	p, err := newProbe(o)
	if err != nil {
		return err
	}
	defer p.closeFiles()
	if o.cpuprofile {
		f, err := os.Create(filepath.Join(o.out, "cpu.pprof"))
		if err != nil {
			return err
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}
	p.snapshot(0, "before")
	for c := 1; c <= o.cycles; c++ {
		if err := p.cycle(c); err != nil {
			return fmt.Errorf("cycle %d: %w", c, err)
		}
		p.snapshot(c, "closed")
	}
	// 閉じた Model の裏の処理 (走査・git) が遅れて終わる分を待ってから、もう 1 回数える
	time.Sleep(2 * time.Second) // sleep-ok: 否定の確認: 「閉じた後に残る goroutine が無い」は待つべき成立条件が無い
	p.snapshot(o.cycles, "after-2s")
	if o.memprofile {
		f, err := os.Create(filepath.Join(o.out, "mem.pprof"))
		if err != nil {
			return err
		}
		runtime.GC()
		err = pprof.WriteHeapProfile(f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return p.summarize()
}

type probe struct {
	o            options
	stepsF, cycF *os.File
	agg          map[string]*phaseAgg
	order        []string
	cycles       []cycleRow // サイクルの数 + 2 で頭打ち (ポインタを持たない値なので、ヒープのオブジェクトは増えない)
	lastNotes    []string
}

// phaseAgg は 1 つの段のサイクルをまたいだ集計。値はサイクルごとに 1 つずつ足す。
type phaseAgg struct {
	p50, p95, max, settle, fp95, fmax, allocKB []float64
	worst                                      float64 // 1 回 (打鍵かフレーム) の最大
	worstAt                                    string  // それを出したサイクルと打鍵
	keys                                       int
	state                                      string // 最後のサイクルの段の後の Place
}

type cycleRow struct {
	cycle                     int
	when                      string
	goroutines, fds, children int
	heapInuse, heapObjects    uint64
}

func newProbe(o options) (*probe, error) {
	sf, err := os.Create(filepath.Join(o.out, "steps.tsv"))
	if err != nil {
		return nil, err
	}
	cf, err := os.Create(filepath.Join(o.out, "cycles.tsv"))
	if err != nil {
		_ = sf.Close()
		return nil, err
	}
	fmt.Fprintln(sf, "cycle\tphase\tkeys\tp50_ms\tp95_ms\tmax_ms\tmax_key\tsum_ms\tsettle_ms\tframes\tframe_p95_ms\tframe_max_ms\tmallocs\talloc_kb\tstate\tnote")
	fmt.Fprintln(cf, "cycle\twhen\tgoroutines\tfds\tchildren\theap_inuse_kb\theap_objects")
	return &probe{o: o, stepsF: sf, cycF: cf, agg: map[string]*phaseAgg{}, cycles: make([]cycleRow, 0, o.cycles+2)}, nil
}

func (p *probe) closeFiles() {
	_ = p.stepsF.Close()
	_ = p.cycF.Close()
}

func (p *probe) snapshot(c int, when string) {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	r := cycleRow{cycle: c, when: when, goroutines: runtime.NumGoroutine(), fds: countFDs(), children: countChildren(),
		heapInuse: ms.HeapInuse, heapObjects: ms.HeapObjects}
	p.cycles = append(p.cycles, r)
	fmt.Fprintf(p.cycF, "%d\t%s\t%d\t%d\t%d\t%d\t%d\n", r.cycle, r.when, r.goroutines, r.fds, r.children, r.heapInuse/1024, r.heapObjects)
}

// countFDs は開いている fd の数。/dev/fd は macOS で読めない (ReadDir が閉じた fd の lstat で失敗する) ので、
// 上限までの番号を fcntl(F_GETFD) で 1 つずつ確かめる。
func countFDs() int {
	var rl unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &rl); err != nil {
		return -1
	}
	n := 0
	for fd := range int(min(rl.Cur, 1<<16)) {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
			n++
		}
	}
	return n
}

// countChildren は自分の子プロセスの数 (git が終わらずに残っていないか。-1 は数えられない)。
func countChildren() int {
	cmd := subproc.Command("pgrep", "-P", strconv.Itoa(os.Getpid()))
	out, err := cmd.Output()
	if err != nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
		return 0 // 該当なし
	}
	if err != nil {
		return -1
	}
	return len(strings.Fields(string(out)))
}

// session は 1 つの Model を main.go と同じ形で動かす。
type session struct {
	p       *probe
	m       *filer.Model
	cycle   int
	root    string       // Place のパスを相対で書く基準
	signals atomic.Int64 // ライブ更新の合図を受けた回数
	done    chan struct{}
	rec     *phaseRec     // 計測中の段 (phase の中だけ)
	note    string        // 段の中で分かったこと (phaseNote が行の note に足して空にする)
	skew    time.Duration // Model に見せる時計の進み (打鍵ごとに keyGap 進める)
}

// keyGap は打鍵と打鍵の間に流れる時間。main.go では打鍵の間に 16ms ごとの tick が挟まり、動きが進む。待たずに続けて打つと
// Advance の dt がほぼ 0 になり、動きが止まったまま打鍵だけを測ることになるので、Model に見せる時計をこの分ずつ進める。
const keyGap = 16 * time.Millisecond

// clock は Model に見せる「今」(壁時計 + 打鍵で進めた分。戻らない)。
func (s *session) clock() time.Time { return time.Now().Add(s.skew) }

// phaseRec は計測中の 1 段。段が終わったら行に書いて集計へ足し、捨てる。
type phaseRec struct {
	steps  []time.Duration
	maxKey string
	frames []time.Duration
}

func (p *probe) open(c int, dir, phase string) (*session, error) {
	s := &session{p: p, cycle: c, root: dir, done: make(chan struct{})}
	var err error
	s.phase(phase, func() {
		s.m, err = filer.New(dir, filer.Options{Now: s.clock})
		if err != nil {
			return
		}
		ch := s.m.Changed()
		go func() { // main.go の waitChange と同じ: 合図を受けて Advance で取り込む (ここでは数えるだけ。取り込みは frame)
			defer close(s.done)
			for range ch {
				s.signals.Add(1)
			}
		}()
		s.m.Resize(p.o.width, p.o.height)
		s.frame()
	})
	return s, err
}

// close は main.go の Quit と同じく Close を呼び、合図の待ちが終わったことを確かめる (Close がチャネルを閉じる)。
func (s *session) close() error {
	s.m.Close()
	select {
	case <-s.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("閉じた (Close) 後も合図のチャネルが閉じない")
	}
}

func (s *session) frame() time.Duration {
	t0 := time.Now()
	s.m.Advance(s.clock())
	s.m.View()
	return time.Since(t0)
}

// keys は打鍵を main.go と同じ順 (HandleKey → Advance → View) で処理し、1 打ずつの所要を段に記録する。打鍵の間は keyGap とみなす。
func (s *session) keys(ks ...string) {
	for _, k := range ks {
		s.skew += keyGap
		t0 := time.Now()
		s.m.HandleKey(k)
		s.m.Advance(s.clock())
		s.m.View()
		d := time.Since(t0)
		if r := s.rec; r != nil {
			if len(r.steps) == 0 || d > slices.Max(r.steps) {
				r.maxKey = k + "#" + strconv.Itoa(len(r.steps)+1)
			}
			r.steps = append(r.steps, d)
		}
	}
}

// settle は動きと裏の処理が止まるまで main.go の tick の周期で回し、1 フレームずつの所要を段に記録する。
func (s *session) settle() (time.Duration, error) {
	t0 := time.Now()
	for {
		d := s.frame()
		if s.rec != nil {
			s.rec.frames = append(s.rec.frames, d)
		}
		switch {
		case s.m.Animating():
			time.Sleep(16 * time.Millisecond) // sleep-ok: tick: main.go の frameInterval と同じ周期で描く
		case s.m.Busy():
			time.Sleep(80 * time.Millisecond) // sleep-ok: tick: main.go の busyInterval と同じ周期で取り込む
		default:
			return time.Since(t0), nil
		}
		if time.Since(t0) > s.p.o.settle {
			return time.Since(t0), fmt.Errorf("%s 待っても止まらない (Busy=%v Animating=%v)", s.p.o.settle, s.m.Busy(), s.m.Animating())
		}
	}
}

func (s *session) phase(name string, f func()) { s.phaseNote(name, "", f) }

// phaseNote は f の中の打鍵を記録し、最後に settle まで待って 1 行を書く。
func (s *session) phaseNote(name, note string, f func()) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r := &phaseRec{}
	s.rec = r
	f()
	if s.note != "" {
		note = strings.TrimSpace(note + " " + s.note)
		s.note = ""
	}
	var settle time.Duration
	state := ""
	if s.m != nil {
		var err error
		settle, err = s.settle()
		if err != nil {
			note = strings.TrimSpace(note + " " + err.Error())
		}
		state = s.state()
	}
	s.rec = nil
	if strings.HasSuffix(name, "-diff") && !strings.Contains(state, "(diff)") {
		note = strings.TrimSpace(note + " 空振り: diff を出せない (変更の無いファイル)")
	}
	runtime.ReadMemStats(&after)
	s.p.record(s.cycle, name, r, settle, after.Mallocs-before.Mallocs, after.TotalAlloc-before.TotalAlloc, state, note)
}

// state は段の後の Place (空振りを見分ける: 開いたフォルダの数・タイル・カーソル)。
func (s *session) state() string {
	pl := s.m.Place()
	rel := func(p string) string {
		if r, err := filepath.Rel(s.root, p); err == nil {
			return r
		}
		return p
	}
	tiles := make([]string, 0, len(pl.Tiles))
	for _, t := range pl.Tiles {
		tl := rel(t.Path)
		if t.Diff {
			tl += "(diff)"
		}
		tiles = append(tiles, tl)
	}
	return fmt.Sprintf("opened=%d tiles=[%s] cursor=%s", len(pl.Opened), strings.Join(tiles, ","), rel(pl.Cursor))
}

func (p *probe) record(c int, name string, r *phaseRec, settle time.Duration, mallocs, byts uint64, state, note string) {
	p50, p95, mx, sum := stats(r.steps)
	_, fp95, fmx, _ := stats(r.frames)
	fmt.Fprintf(p.stepsF, "%d\t%s\t%d\t%.2f\t%.2f\t%.2f\t%s\t%.1f\t%.1f\t%d\t%.2f\t%.2f\t%d\t%d\t%s\t%s\n", c, name, len(r.steps), ms(p50), ms(p95), ms(mx),
		r.maxKey, ms(sum), ms(settle), len(r.frames), ms(fp95), ms(fmx), mallocs, byts/1024, state, note)
	a := p.agg[name]
	if a == nil {
		a = &phaseAgg{}
		p.agg[name] = a
		p.order = append(p.order, name)
	}
	a.p50, a.p95, a.max = append(a.p50, ms(p50)), append(a.p95, ms(p95)), append(a.max, ms(mx))
	a.settle, a.fp95, a.fmax = append(a.settle, ms(settle)), append(a.fp95, ms(fp95)), append(a.fmax, ms(fmx))
	a.allocKB = append(a.allocKB, float64(byts/1024))
	a.keys = len(r.steps)
	if ms(mx) > a.worst {
		a.worst, a.worstAt = ms(mx), "c"+strconv.Itoa(c)+" "+r.maxKey
	}
	if ms(fmx) > a.worst {
		a.worst, a.worstAt = ms(fmx), "c"+strconv.Itoa(c)+" frame"
	}
	if c == p.o.cycles {
		a.state = state
		if note != "" {
			p.lastNotes = append(p.lastNotes, name+": "+note)
		}
	}
}

func (p *probe) cycle(c int) error {
	s, err := p.open(c, p.o.root, "open")
	if err != nil {
		return err
	}
	s.phase("explore", func() { s.explore(exploreKeys) })
	s.phase("explode", func() { // root へ飛んで配下を全部開く (explore の後のカーソルは深い所にいる)
		s.keys(":")
		s.m.Paste(p.o.root)
		s.keys("enter", "e")
	})
	// j / k と / はカーソルの列 (兄弟) の中で動く。root の列は root だけなので、l で root の直下の列へ入ってから動く
	s.phase("move", func() { s.keys(moveKeys(columnSize(p.o.root))...) })
	s.phase("search", func() {
		s.keys("/")
		s.keys(chars(p.o.query)...)
		s.keys("enter")
		s.keys(repeat("n", 30)...)
	})
	for _, t := range []struct{ name, rel string }{{"tile-code", p.o.file}, {"tile-md", p.o.md}} {
		s.tile(t.name, t.rel)
	}
	s.phase("collapse", func() { s.keys("C", "h") })
	s.phase("refresh", func() { s.keys("r") })
	if err := s.close(); err != nil {
		return err
	}
	if err := p.busyClose(c); err != nil {
		return err
	}
	if p.o.live > 0 {
		return p.liveCycle(c)
	}
	return nil
}

// busyClose は裏の処理 (配下の走査・git) が走っている最中に閉じる (glogx で F を開いてすぐ閉じる形)。Close は見張りしか
// 止めないので、残った処理が終わらずに goroutine・子プロセスを抱える経路はここで出る: 閉じた後、goroutine と子プロセスが
// 開く前の数へ戻るまで待ち、その時間を busy-close の行に書く。戻らなければリークの候補として止める。戻るまで待つのは、
// 残りの処理を後の段の計測とサイクルの資源の行に混ぜないためでもある。
func (p *probe) busyClose(c int) error {
	g0, ch0 := runtime.NumGoroutine(), countChildren()
	b, err := p.open(c, p.o.root, "busy-open")
	if err != nil {
		return err
	}
	r := &phaseRec{}
	b.rec = r
	b.keys("h", "e", "r")
	b.rec = nil
	busy := b.m.Busy()
	if err := b.close(); err != nil {
		return err
	}
	t0 := time.Now()
	note := "閉じた時に裏の処理が走っていた"
	if !busy {
		note = "空振り: 閉じる前に裏の処理が終わっていた"
	}
	for runtime.NumGoroutine() > g0 || countChildren() > ch0 {
		if time.Since(t0) > p.o.settle {
			note += fmt.Sprintf("・%s 待っても goroutine %d → %d / 子 %d → %d に戻らない (リークの候補)", p.o.settle, g0, runtime.NumGoroutine(), ch0, countChildren())
			p.record(c, "busy-close", r, time.Since(t0), 0, 0, "", note)
			return errors.New(note)
		}
		time.Sleep(50 * time.Millisecond) // sleep-ok: tick: 残った処理が終わるのを数で待つ刻み
	}
	p.record(c, "busy-close", r, time.Since(t0), 0, 0, "", note)
	return nil
}

// tile は rel を : で探してタイルで開き、スクロール・diff の切り替え・閉じるを測る。
func (s *session) tile(name, rel string) {
	abs := filepath.Join(s.root, rel)
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		s.phaseNote(name, "skip: "+rel+" が無い", func() {})
		return
	}
	s.phase(name, func() {
		s.keys(":")
		s.m.Paste(abs)
		s.keys("enter", "enter") // : の入力欄を確定してカーソルを移し、タイルで開く
		s.keys(repeat("j", 300)...)
		s.keys("G", "g")
	})
	if len(s.m.Place().Tiles) == 0 { // 開かなかったのに d を押すと、木の上のファイルのゴミ箱の確認になる
		s.phaseNote(name+"-diff", "skip: タイルが開かない", func() {})
		s.phaseNote(name+"-close", "skip: タイルが開かない", func() {})
		return
	}
	s.phase(name+"-diff", func() { s.keys("d") })
	s.phase(name+"-close", func() { s.keys("d", "q") })
}

// liveCycle は一時フォルダ (git の repo) に o.live 件のファイルと変更のある code.go を作って開き、diff を出し、全部の mtime を
// 進めて、合図から反映までを測る。
func (p *probe) liveCycle(c int) error {
	dir, err := os.MkdirTemp("", "tfprobe-live-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }() //nolint:forbidigo // 自前の一時ディレクトリ
	if err := makeLiveRepo(dir, p.o.live); err != nil {
		return err
	}
	s, err := p.open(c, dir, "live-open-root")
	if err != nil {
		return err
	}
	s.tile("live-tile", "code.go")
	s.phase("live-open", func() { // big へ飛んで開く (タイルの段でカーソルは code.go にいる)
		s.keys(":")
		s.m.Paste(filepath.Join(dir, "big"))
		s.keys("enter", "l")
	})
	var waitErr error
	s.phase("live-touch", func() {
		before := s.signals.Load()
		later := time.Now().Add(time.Hour)
		for i := range p.o.live {
			_ = os.Chtimes(filepath.Join(dir, "big", "f"+strconv.Itoa(i)), later, later)
		}
		deadline := time.Now().Add(p.o.settle)
		for s.signals.Load() == before {
			if time.Now().After(deadline) {
				waitErr = errors.New("ライブ更新の合図が来ない")
				return
			}
			time.Sleep(20 * time.Millisecond) // sleep-ok: tick: 合図 (watch のポーリング) を条件で待つ刻み
		}
		s.rec.frames = append(s.rec.frames, s.frame()) // 合図を取り込む (main.go の changedMsg → Advance)。いちばん重いフレームなので記録に入れる
		s.note = fmt.Sprintf("合図 +%d・取り込んだ後の動き=%v", s.signals.Load()-before, s.m.Animating())
	})
	if waitErr != nil {
		return waitErr
	}
	return s.close()
}

// makeLiveRepo は dir を git の repo にし、big/ に n 件の空のファイルと、commit の後で書き換えた code.go を置く。
func makeLiveRepo(dir string, n int) error {
	if err := os.Mkdir(filepath.Join(dir, "big"), 0o755); err != nil {
		return err
	}
	for i := range n {
		if err := writeFile(filepath.Join(dir, "big", "f"+strconv.Itoa(i)), ""); err != nil {
			return err
		}
	}
	code := filepath.Join(dir, "code.go")
	if err := writeFile(code, strings.Repeat("package x\n\nfunc f() int { return 1 }\n", 200)); err != nil {
		return err
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=tfprobe", "-c", "user.email=tfprobe@example.invalid", "commit", "-qm", "init"}} {
		cmd := subproc.GitCommand(context.Background(), args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", args[0], err, out)
		}
	}
	return writeFile(code, strings.Repeat("package x\n\nfunc f() int { return 2 }\n", 200))
}

func writeFile(path, body string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// summarize は段ごとにサイクル間の中央値 (マシンの負荷の揺れに強い) と、最悪の 1 回とその出どころを出し、資源の推移を判定する。
func (p *probe) summarize() error {
	f, err := os.Create(filepath.Join(p.o.out, "summary.txt"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := io.MultiWriter(f, os.Stdout)
	fmt.Fprintf(w, "root=%s cycles=%d size=%dx%d live=%d %s %s/%s\n\n", p.o.root, p.o.cycles, p.o.width, p.o.height, p.o.live,
		runtime.Version(), runtime.GOOS, runtime.GOARCH)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "phase\tkeys\tp50_ms\tp95_ms\tmax_ms\tsettle_ms\tframe_p95\tframe_max\talloc_kb\tworst_ms\tworst_at\t")
	for _, name := range p.order {
		a := p.agg[name]
		fmt.Fprintf(tw, "%s\t%d\t%.2f\t%.2f\t%.2f\t%.0f\t%.2f\t%.2f\t%.0f\t%.2f\t%s\t\n", name, a.keys, median(a.p50), median(a.p95), median(a.max),
			median(a.settle), median(a.fp95), median(a.fmax), median(a.allocKB), a.worst, a.worstAt)
	}
	_ = tw.Flush()
	fmt.Fprintln(w, "\n(worst 以外はサイクル間の中央値。worst_at = 最悪の 1 回を出したサイクルと打鍵 (frame は待ちの間の描画)。全サイクルは steps.tsv)")
	fmt.Fprintln(w, "\n段の後の状態 (最後のサイクル。空振りの見分け):")
	for _, name := range p.order {
		fmt.Fprintf(w, "  %-16s %s\n", name, p.agg[name].state)
	}
	for _, n := range p.lastNotes {
		fmt.Fprintln(w, "注:", n)
	}
	p.resources(w)
	return nil
}

// resources は資源の推移を出す。1 サイクル目は温まる前 (ファイルの cache・確保の初回) なので、増え続けているかは
// 1 サイクル目の後から最後までの増分で見る。サイクルを重ねて増え続けるものがリークの候補。
func (p *probe) resources(w io.Writer) {
	if len(p.cycles) < 3 {
		return
	}
	// goroutine・fd・子プロセスは最後の 2 秒待った後の行で見る (busy-open は裏の処理が走っている最中に閉じるので、各サイクルの
	// 閉じた直後の行には終わりかけの処理が載る)。ヒープの増え方はサイクルの閉じた直後の行どうしで割る (2 秒待ちの差を混ぜない)
	a, end := p.cycles[1], p.cycles[len(p.cycles)-1]
	last := p.cycles[len(p.cycles)-2]
	fmt.Fprintf(w, "\n資源 (1 サイクル目の後 → %s):\n", end.when)
	fmt.Fprintf(w, "  goroutines %d → %d / fds %d → %d / children %d → %d\n", a.goroutines, end.goroutines, a.fds, end.fds, a.children, end.children)
	if n := last.cycle - a.cycle; n >= 1 {
		fmt.Fprintf(w, "  heap_objects %d → %d (%+.1f/cycle) / heap_inuse %dKB → %dKB (%+.1fKB/cycle)\n", a.heapObjects, last.heapObjects,
			float64(int64(last.heapObjects)-int64(a.heapObjects))/float64(n), a.heapInuse/1024, last.heapInuse/1024,
			float64(int64(last.heapInuse)-int64(a.heapInuse))/1024/float64(n))
	} else {
		fmt.Fprintln(w, "  heap の増え方はサイクルが 2 つ以上のときだけ出す")
	}
	if p.o.cpuprofile {
		fmt.Fprintln(w, "  (-cpuprofile の書き込みで goroutine と fd が 1 つずつ多い)")
	}
	fmt.Fprintln(w, "(全サイクルは cycles.tsv。増え方はサイクルを増やして (-cycles 20 など) 頭打ちになるかで見る)")
}

func repeat(k string, n int) []string { return slices.Repeat([]string{k}, n) }

// exploreKeys / exploreDepth は explore の段の打鍵の数と、潜る深さの上限 (root の直下 = 1)。
const (
	exploreKeys  = 600
	exploreDepth = 6
)

// explore は閉じた木を、キーを押しっぱなしにした間隔で渡り歩く: フォルダなら潜る (l)・同じ列で何個か動く (j / k)・親へ戻る (h) を
// 種を固定した乱数で選ぶ (サイクルをまたいで同じ道を辿る)。潜るたびにフォルダを読み、木が右へ育ち、カメラが動く。
// l はフォルダの上でだけ押す (ファイルの上ではタイルが開く)。h は root の直下より浅くは押さない (root では root を上げる)。
func (s *session) explore(n int) {
	rng := rand.New(rand.NewPCG(1, 2))
	for sent := 0; sent < n; {
		cur := s.m.Place().Cursor
		depth := 0
		if rel, err := filepath.Rel(s.root, cur); err == nil && rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		fi, err := os.Lstat(cur)
		isDir := err == nil && fi.IsDir()
		switch r := rng.IntN(10); {
		case isDir && depth < exploreDepth && r < 5:
			s.keys("l")
			sent++
		case r < 8 || depth <= 1: // 列の中を上下に何個か (下だけだと列の末尾に張り付く)
			k, key := 1+rng.IntN(3), "j"
			if rng.IntN(3) == 0 {
				key = "k"
			}
			s.keys(repeat(key, k)...)
			sent += k
		default:
			s.keys("h")
			sent++
		}
	}
}

// columnSize は root の直下で木に見える項目の数 (既定の設定は . で始まる名前を隠す)。
func columnSize(root string) int {
	es, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range es {
		if !strings.HasPrefix(e.Name(), ".") {
			n++
		}
	}
	return n
}

// moveKeys は列へ入り (l)、端から端までの往復を約 800 打まで繰り返して、列の最後で止める (元の位置に戻ると、j が効かなくても
// 段の後の状態が変わらない。最後で止めるのは、続く検索で動いたことが状態に出るように: 検索は最後の項目以外に当てる)。
func moveKeys(n int) []string {
	ks := []string{"l"}
	for n > 1 && len(ks) < 800 {
		ks = append(ks, repeat("j", n-1)...)
		ks = append(ks, repeat("k", n-1)...)
	}
	return append(ks, repeat("j", max(1, n-1))...)
}

func chars(text string) []string {
	out := make([]string, 0, len(text))
	for _, r := range text {
		out = append(out, string(r))
	}
	return out
}

func stats(ds []time.Duration) (p50, p95, mx, sum time.Duration) {
	if len(ds) == 0 {
		return
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	for _, d := range s {
		sum += d
	}
	return s[len(s)/2], s[min(len(s)-1, len(s)*95/100)], s[len(s)-1], sum
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
