package main

// 実 fsnotify を要さない見張りの検査 (issue 087)。
//
// なぜ別建てか: CI (ubuntu-slim) では fsnotify.NewWatcher が通らず、watcher を前提にした
// 検査は全部 skip される。issues_watch_test.go 側の検査は「実 fsnotify との結合」を見る
// 価値があるので残したまま、**不変条件そのもの**は app.NewDirWatcher seam へフェイクを差して
// どの環境でも走る形で固定する (skip を消して「CI で走っている」ことにはしない)。
//
// 🚨 各検査は v.watch.w がフェイクであることを最初に確かめる。将来 startWatch が seam を
// 通らなくなったら、この検査は「実 fsnotify が要る = CI では skip」へ静かに戻ってしまうため。

import (
	"errors"
	"glogx/internal/issuesview"
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/testenv"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// requireFake は見張りが seam 経由でフェイクを掴んでいることを確かめる (実 fsnotify に
// 戻っていたら、この検査は CI では走らなくなっているので停止させる)。
func requireFake(t *testing.T, v *issuesview.IssuesView) *testenv.FakeWatcher {
	t.Helper()
	fw, ok := v.Fixture().Watcher().(*testenv.FakeWatcher)
	if !ok {
		t.Fatalf("startWatch が newDirWatcher seam を通っていない (w=%T)。この検査は実 fsnotify に依存している", v.Fixture().Watcher())
	}
	return fw
}

// 回帰 (fd 漏れ) を CI でも走らせる版: restartSelf の syscall.Exec は fd テーブルを引き継ぐため、
// cancelAll が watcher を閉じ忘れると viewer を開いたまま r 再起動するたびに fd が漏れ続ける。
func TestCancelAllClosesIssuesWatcherWithoutFsnotify(t *testing.T) {
	testenv.FakeWatcherSeam(t)
	root, _ := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv.Toggle(root)
	m.issuesOv.Receive(scanOf(t, root))
	fw := requireFake(t, &m.issuesOv)

	m.cancelAll()
	if m.issuesOv.Fixture().Watcher() != nil {
		t.Fatal("cancelAll 後も watcher が開いたまま (restart の exec で fd が漏れる)")
	}
	// スロットの nil 化だけでなく実体が Close されたことを直接固定する (Close を消して
	// nil 代入だけにする変異 = fd 漏れの再発をすり抜けさせない)
	if err := fw.Add(root); !errors.Is(err, fsnotify.ErrClosed) {
		t.Fatalf("cancelAll 後の watcher が Close されていない (Add err=%v)", err)
	}
}
