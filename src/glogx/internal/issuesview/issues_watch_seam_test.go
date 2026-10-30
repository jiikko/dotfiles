package issuesview

import (
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/testenv"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"

	"testwait"
)

// 回帰 (2026-08-21 実測): 消えて戻ったディレクトリを再び watch する。
// issues_watch_test.go の同名検査は実 fsnotify 版で CI では skip されるため、不変条件は
// ここで環境非依存に固定する。
func TestIssuesWatchReAddsRecreatedDirWithoutFsnotify(t *testing.T) {
	testenv.FakeWatcherSeam(t)
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	done := filepath.Join(root, "issues", "done")
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	// 🚨 空ディレクトリは watch 対象にならない (FindDirs は markdown を含むものだけ拾う)
	issuesviewtest.WriteIssue(t, filepath.Join(done, "000-feat-old.md"), "# 000 feat: old\n", time.Now().Add(-time.Hour))
	v := openedWatchView(t, root, path)
	fw := requireFake(t, v)
	if !slices.Contains(fw.WatchList(), done) {
		t.Fatalf("前提: done/ が watch されていない: %v", fw.WatchList())
	}

	// ディレクトリが消える (git switch 相当)。OS は watch を黙って落とす
	if err := os.RemoveAll(done); err != nil {
		t.Fatal(err)
	}
	fw.LoseWatch(done)
	v.startWatch() // 消えている間の取り直し (Add は失敗する)
	if slices.Contains(fw.WatchList(), done) {
		t.Fatal("消えているディレクトリが watch されている (フェイクが実 fsnotify と食い違っている)")
	}

	before := fw.AddCount(done)
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	v.startWatch() // 戻った後の取り直し: ここで再び Add されなければならない

	if fw.AddCount(done) == before {
		t.Fatal("戻ってきた done/ に Add が呼ばれていない (「Add 済み」を覚えて skip している)")
	}
	if !slices.Contains(fw.WatchList(), done) {
		t.Fatalf("消えて戻った done/ が再 watch されていない: %v", fw.WatchList())
	}
}

// イベント駆動経路 (eventCmd → 指紋 → 取り直し) を実 fsnotify 無しで通す。CI ではこの経路が
// 一度も走っていなかった (指紋ポーリングだけで緑になっていた)。
func TestIssuesWatchEventPathWithoutFsnotify(t *testing.T) {
	testenv.FakeWatcherSeam(t)
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: 編集前\n")
	v := openedWatchView(t, root, path)
	fw := requireFake(t, v)

	v.watch.EvArmed = false // 既に張られているチェーンをテストが引き取る
	cmd := v.eventCmd()
	if cmd == nil {
		t.Fatal("イベント待ちの Cmd が作れない")
	}
	// 🚨 先に走らせて「イベントが来るまで返らない」ことを確かめる。emit の後で走らせると、
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

	issuesviewtest.WriteIssue(t, path, "# 001 feat: 編集後\n", time.Now())
	fw.Emit(fsnotify.Event{Name: path, Op: fsnotify.Write})

	raw := testwait.Recv(t, "イベントを流したのに観測が返らない", got, testwait.Within(5*time.Second))
	msg, ok := raw.(IssuesWatchMsg)
	if !ok || msg.closed {
		t.Fatalf("イベントで観測が返らない: %+v", msg)
	}
	if !msg.fromEvent {
		t.Fatal("イベント由来の観測に fromEvent が立っていない (札の降ろし先が変わる)")
	}
	if msg.fp == v.watch.seen {
		t.Fatal("イベント経路の指紋が変化を捉えていない")
	}
	v.HandleWatch(msg)                                                // 1 回目: 安定待ち
	reload := v.HandleWatch(IssuesWatchMsg{fp: msg.fp, gen: msg.gen}) // 2 回目: 安定 → 取り直し
	if reload == nil {
		t.Fatal("イベント由来の変化で取り直しが走らない")
	}
	v.Receive(drainScanMsg(t, reload))
	if got := v.rows[0].Display(); got != "feat: 編集後" {
		t.Fatalf("イベント経由の編集が反映されていない: %q", got)
	}
}

// 回帰 (リーク監査 2026-08-01) を CI でも走らせる版: ポーリング由来の観測でイベント待ちの札まで
// 降ろすと、観測 1 回につきイベント待ちの goroutine が 1 本積み上がる。
//
// 🚨 liveEventChains の 300ms 窓は、実 fsnotify を使わずに無条件に走る。
// 極端な混雑下での flaky は理論上否定できないが、CPU 飽和 + GOMAXPROCS=1 + -race で 80 回
// 回しても再現しなかった (実測 2026-08-21)。実際に intermittent な失敗が出たら、
// 窓を広げる推測ベースの防御ではなく「チャネル close の完了を ack で同期する」形へ直すこと。
func TestIssuesWatchPollDoesNotStackEventChainsWithoutFsnotify(t *testing.T) {
	testenv.FakeWatcherSeam(t)
	root, path := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	v := openedWatchView(t, root, path)
	requireFake(t, v)

	msgs := make(chan tea.Msg, 64)
	v.watch.EvArmed = false // 既に張られている札をテストが引き取り、実体の goroutine を作る
	runWatchChains(t, v.eventCmd(), msgs)

	for range 3 {
		runWatchChains(t, v.HandleWatch(observeIssues(v)), msgs) // ポーリング由来 (fromEvent=false)
	}
	if n := liveEventChains(v, msgs); n != 1 {
		t.Fatalf("イベント待ちの goroutine が %d 本生きている (1 本のはず): 観測のたびに積み上がっている", n)
	}
}
