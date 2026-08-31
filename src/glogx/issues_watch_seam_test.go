package main

// 実 fsnotify を要さない見張りの検査 (issue 087)。
//
// なぜ別建てか: CI (ubuntu-slim) では fsnotify.NewWatcher が通らず、watcher を前提にした
// 検査は全部 skip される。issues_watch_test.go 側の検査は「実 fsnotify との結合」を見る
// 価値があるので残したまま、**不変条件そのもの**は newDirWatcher seam へフェイクを差して
// どの環境でも走る形で固定する (skip を消して「CI で走っている」ことにはしない)。
//
// ⚠️ 各検査は v.watch.w がフェイクであることを最初に確かめる。将来 startWatch が seam を
// 通らなくなったら、この検査は「実 fsnotify が要る = CI では skip」へ静かに戻ってしまうため。

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// fakeWatcher は dirWatcher のフェイク。Add の履歴と watch 集合を観測できる。
//
// 実 fsnotify に合わせている点:
//   - 存在しないパスの Add は失敗する (watch も張られない)
//   - Close でイベントチャネルが閉じる (待っている goroutine が解ける)
//
// 合わせていない点 (明示的にテストが動かす): ディレクトリが消えたときの watch の消失は
// OS が黙ってやるので、フェイクでは loseWatch で再現する (Add の失敗契機に混ぜると
// 「Add を呼ばなくても消えている」形になり、再 Add の検査が空回りしうる)。
type fakeWatcher struct {
	mu     sync.Mutex
	added  []string // Add の呼び出し履歴 (成否に関わらず、順に全件)
	list   []string // 現に張られている watch
	closed bool
	events chan fsnotify.Event
	errors chan error
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{events: make(chan fsnotify.Event, 16), errors: make(chan error, 4)}
}

func (f *fakeWatcher) Add(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return fsnotify.ErrClosed
	}
	f.added = append(f.added, dir)
	if _, err := os.Stat(dir); err != nil {
		return err // 消えているディレクトリは張れない
	}
	if !slices.Contains(f.list, dir) {
		f.list = append(f.list, dir)
	}
	return nil
}

func (f *fakeWatcher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil // 冪等 (二重 Close で panic しない)
	}
	f.closed = true
	close(f.events)
	close(f.errors)
	return nil
}

func (f *fakeWatcher) WatchList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.list...)
}

func (f *fakeWatcher) Events() <-chan fsnotify.Event { return f.events }
func (f *fakeWatcher) Errors() <-chan error          { return f.errors }

// loseWatch は「ディレクトリが消えて OS が watch を黙って落とした」を再現する。
func (f *fakeWatcher) loseWatch(dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list = slices.DeleteFunc(f.list, func(s string) bool { return s == dir })
}

// addCount は dir に対する Add の呼び出し回数 (成否を問わない)。
func (f *fakeWatcher) addCount(dir string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.added {
		if d == dir {
			n++
		}
	}
	return n
}

// emit はイベントを 1 本流す (実 fsnotify の代わり)。
//
// ⚠️ 送信までロックを持ったままにする。closed を読んでから解錠して送ると、その隙間に Close が
// 入って「閉じたチャネルへの送信」で panic する (red team が 2000 回ループで再現)。events は
// バッファ 16 で、ドレインするのは検査対象のコードだけ (Add/Close/WatchList はチャネルを読まない)
// なので、ロックを持ったままの送信でデッドロックしない。
func (f *fakeWatcher) emit(ev fsnotify.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.events <- ev
}

// fakeWatcherSeam は newDirWatcher をフェイクへ差し替え、そのフェイクを返す。
//
// ⚠️ seam は 2 つの見張り (issues / git log) の共用なので、1 つのテストで両方を起動すると
// **同じフェイクを 2 つが掴んでイベントチャネルを取り合う**。両方を動かすテストを書くなら
// フェイクを見張りごとに分けること。
func fakeWatcherSeam(t *testing.T) *fakeWatcher {
	t.Helper()
	fw := newFakeWatcher()
	prev := newDirWatcher
	newDirWatcher = func() (dirWatcher, error) { return fw, nil }
	t.Cleanup(func() {
		newDirWatcher = prev
		_ = fw.Close()
	})
	return fw
}

// requireFake は見張りが seam 経由でフェイクを掴んでいることを確かめる (実 fsnotify に
// 戻っていたら、この検査は CI では走らなくなっているので停止させる)。
func requireFake(t *testing.T, v *issuesView) *fakeWatcher {
	t.Helper()
	fw, ok := v.watch.w.(*fakeWatcher)
	if !ok {
		t.Fatalf("startWatch が newDirWatcher seam を通っていない (w=%T)。この検査は実 fsnotify に依存している", v.watch.w)
	}
	return fw
}

// 回帰 (2026-08-21 実測 / issue 087): 消えて戻ったディレクトリを再び watch する。
// issues_watch_test.go の同名検査は実 fsnotify 版で CI では skip されるため、不変条件は
// ここで環境非依存に固定する。
func TestIssuesWatchReAddsRecreatedDirWithoutFsnotify(t *testing.T) {
	fakeWatcherSeam(t)
	root, path := watchTree(t, "# 001 feat: x\n")
	done := filepath.Join(root, "issues", "done")
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	// ⚠️ 空ディレクトリは watch 対象にならない (FindDirs は markdown を含むものだけ拾う)
	writeIssue(t, filepath.Join(done, "000-feat-old.md"), "# 000 feat: old\n", time.Now().Add(-time.Hour))
	v := openedWatchView(t, root, path)
	fw := requireFake(t, v)
	if !slices.Contains(fw.WatchList(), done) {
		t.Fatalf("前提: done/ が watch されていない: %v", fw.WatchList())
	}

	// ディレクトリが消える (git switch 相当)。OS は watch を黙って落とす
	if err := os.RemoveAll(done); err != nil {
		t.Fatal(err)
	}
	fw.loseWatch(done)
	v.startWatch() // 消えている間の取り直し (Add は失敗する)
	if slices.Contains(fw.WatchList(), done) {
		t.Fatal("消えているディレクトリが watch されている (フェイクが実 fsnotify と食い違っている)")
	}

	before := fw.addCount(done)
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	v.startWatch() // 戻った後の取り直し: ここで再び Add されなければならない

	if fw.addCount(done) == before {
		t.Fatal("戻ってきた done/ に Add が呼ばれていない (「Add 済み」を覚えて skip している)")
	}
	if !slices.Contains(fw.WatchList(), done) {
		t.Fatalf("消えて戻った done/ が再 watch されていない: %v", fw.WatchList())
	}
}

// イベント駆動経路 (eventCmd → 指紋 → 取り直し) を実 fsnotify 無しで通す。CI ではこの経路が
// 一度も走っていなかった (指紋ポーリングだけで緑になっていた)。
func TestIssuesWatchEventPathWithoutFsnotify(t *testing.T) {
	fakeWatcherSeam(t)
	root, path := watchTree(t, "# 001 feat: 編集前\n")
	v := openedWatchView(t, root, path)
	fw := requireFake(t, v)

	v.watch.evArmed = false // 既に張られているチェーンをテストが引き取る
	cmd := v.eventCmd()
	if cmd == nil {
		t.Fatal("イベント待ちの Cmd が作れない")
	}
	// ⚠️ 先に走らせて「イベントが来るまで返らない」ことを確かめる。emit の後で走らせると、
	// select を消して即座に指紋を返す実装でも緑になる (指紋は emit 前の書き込みを拾うため)。
	// 待ちの検査は片側だけ厳しい: ブロックしている実装は絶対に早く返らないので、CI が遅くても
	// 偽の失敗にはならない (偽の成功に倒れるだけ)。
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case early := <-got:
		t.Fatalf("イベントが来る前に観測が返った (eventCmd がチャネルを待っていない): %+v", early)
	case <-time.After(150 * time.Millisecond):
	}

	writeIssue(t, path, "# 001 feat: 編集後\n", time.Now())
	fw.emit(fsnotify.Event{Name: path, Op: fsnotify.Write})

	var raw tea.Msg
	select {
	case raw = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("イベントを流したのに観測が返らない")
	}
	msg, ok := raw.(issuesWatchMsg)
	if !ok || msg.closed {
		t.Fatalf("イベントで観測が返らない: %+v", msg)
	}
	if !msg.fromEvent {
		t.Fatal("イベント由来の観測に fromEvent が立っていない (札の降ろし先が変わる)")
	}
	if msg.fp == v.watch.seen {
		t.Fatal("イベント経路の指紋が変化を捉えていない")
	}
	v.handleWatch(msg)                                                // 1 回目: 安定待ち
	reload := v.handleWatch(issuesWatchMsg{fp: msg.fp, gen: msg.gen}) // 2 回目: 安定 → 取り直し
	if reload == nil {
		t.Fatal("イベント由来の変化で取り直しが走らない")
	}
	v.receive(drainScanMsg(t, reload))
	if got := v.rows[0].Display(); got != "feat: 編集後" {
		t.Fatalf("イベント経由の編集が反映されていない: %q", got)
	}
}

// 回帰 (リーク監査 2026-08-01) を CI でも走らせる版: ポーリング由来の観測でイベント待ちの札まで
// 降ろすと、観測 1 回につきイベント待ちの goroutine が 1 本積み上がる。
//
// ⚠️ liveEventChains の 300ms 窓は、この検査が CI (ubuntu-slim) で初めて無条件に走る箇所になる。
// 極端な混雑下での flaky は理論上否定できないが、CPU 飽和 + GOMAXPROCS=1 + -race で 80 回
// 回しても再現しなかった (red team 実測 2026-08-21)。実際に intermittent な失敗が出たら、
// 窓を広げる推測ベースの防御ではなく「チャネル close の完了を ack で同期する」形へ直すこと。
func TestIssuesWatchPollDoesNotStackEventChainsWithoutFsnotify(t *testing.T) {
	fakeWatcherSeam(t)
	root, path := watchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	requireFake(t, v)

	msgs := make(chan tea.Msg, 64)
	v.watch.evArmed = false // 既に張られている札をテストが引き取り、実体の goroutine を作る
	runWatchChains(t, v.eventCmd(), msgs)

	for range 3 {
		runWatchChains(t, v.handleWatch(v.observe()), msgs) // ポーリング由来 (fromEvent=false)
	}
	if n := liveEventChains(v, msgs); n != 1 {
		t.Fatalf("イベント待ちの goroutine が %d 本生きている (1 本のはず): 観測のたびに積み上がっている", n)
	}
}

// 回帰 (fd 漏れ) を CI でも走らせる版: restartSelf の syscall.Exec は fd テーブルを引き継ぐため、
// cancelAll が watcher を閉じ忘れると viewer を開いたまま r 再起動するたびに fd が漏れ続ける。
func TestCancelAllClosesIssuesWatcherWithoutFsnotify(t *testing.T) {
	fakeWatcherSeam(t)
	root, _ := watchTree(t, "# 001 feat: x\n")
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv.toggle(root)
	m.issuesOv.receive(scanOf(t, root))
	fw := requireFake(t, &m.issuesOv)

	m.cancelAll()
	if m.issuesOv.watch.w != nil {
		t.Fatal("cancelAll 後も watcher が開いたまま (restart の exec で fd が漏れる)")
	}
	// スロットの nil 化だけでなく実体が Close されたことを直接固定する (Close を消して
	// nil 代入だけにする変異 = fd 漏れの再発をすり抜けさせない)
	if err := fw.Add(root); !errors.Is(err, fsnotify.ErrClosed) {
		t.Fatalf("cancelAll 後の watcher が Close されていない (Add err=%v)", err)
	}
}
