package main

import (
	"errors"
	"testing"

	"glogx/internal/gitdata"
	"glogx/internal/issuesview"
	"glogx/internal/issuesview/issuesviewtest"

	"github.com/fsnotify/fsnotify"
)

// 🚨 このファイルの検査のうち `v.watch.w == nil` で skip するものは**実 fsnotify との結合**を
// 見ている (CI = ubuntu-slim では NewWatcher が通らないので走らない)。不変条件そのものは
// issues_watch_seam_test.go がフェイクを差して環境非依存に固定しているので、そちらを消さないこと。
// CI でも走る組: 再 Add / イベント経路 / チェーンの積み上がり / cancelAll の Close。
// CI では未カバーのまま (受け入れ済み): 閉じたときの closed 観測・死んだ後のポーリング縮退・世代の弾き。

// observe は watchCmd と同じ指紋を同期で計算する (tea.Tick を待たずにテストで駆動するため)。
// 対象集合は production と同じ watchTargets を通す (ここで再実装すると本番とずれる)。
func observeIssues(v *issuesview.IssuesView) issuesview.IssuesWatchMsg {
	return v.Fixture().Observe()
}

// scanOf は実ファイルを走査した結果を返す。🚨 中身を再実装せず production の scanIssues を
// 呼ぶ: 指紋の取り方がずれると「テストでは基準が揃うのに本番ではずれる」形で穴を見逃す。
func scanOf(t *testing.T, root string) issuesview.IssuesScanMsg {
	t.Helper()
	msg := issuesview.FixtureScanDir(root)
	if len(issuesview.FixtureScanDirs(msg)) == 0 {
		t.Fatalf("issues ディレクトリが見つからない: %s", root)
	}
	return msg
}

// cancelAll (quit / restart 前の後始末ファネル) は issues viewer の watcher も閉じる。
// 通常終了はプロセス終了が fd を回収するが、restartSelf の syscall.Exec は fd テーブルを
// 引き継ぐため、閉じ忘れると viewer を開いたまま r 再起動するたびに fsnotify の kqueue fd が
// 新プロセスへ漏れ続ける (fsnotify v1.9.0 darwin は kqueue fd に CLOEXEC を付けない)。
func TestCancelAllClosesIssuesWatcher(t *testing.T) {
	root, _ := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv.Toggle(root)
	m.issuesOv.Receive(scanOf(t, root))
	w := m.issuesOv.Fixture().Watcher()
	if w == nil {
		t.Skip("この環境では fsnotify を作れない (ポーリングへ縮退する経路)")
	}
	m.cancelAll()
	if m.issuesOv.Fixture().Watcher() != nil {
		t.Fatal("cancelAll 後も fsnotify watcher が開いたまま (restart の exec で fd が漏れる)")
	}
	// スロットの nil 化だけでなく実体が Close されたことを直接固定する (Close を消して
	// nil 代入だけにする変異 = fd 漏れの再発をすり抜けさせない)
	if err := w.Add(root); !errors.Is(err, fsnotify.ErrClosed) {
		t.Fatalf("cancelAll 後の watcher が Close されていない (Add err=%v)", err)
	}
}

// 結合: viewer を開くと見張りのチェーンが張られ、閉じると止まる。
func TestIssuesViewerWatchWiredIntoUpdate(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	root, _ := issuesviewtest.WatchTree(t, "# 001 feat: x\n")
	m.handleKey("i")
	m.issuesOv.Fixture().SetCwd(root)
	m.Update(scanOf(t, root))
	m.issuesOv.Fixture().FinishAnim()
	if !m.issuesOv.Fixture().PollArmed() {
		t.Fatal("viewer を開いても見張りが張られていない")
	}
	_, cmd := m.Update(observeIssues(&m.issuesOv))
	if cmd == nil {
		t.Fatal("Update が見張りのチェーンを継続しない")
	}
	releaseKey(m)    // 指を離してから押し直す (キーリピート判定を跨ぐ)
	m.handleKey("i") // 閉じる
	if _, cmd := m.Update(observeIssues(&m.issuesOv)); cmd != nil {
		t.Fatal("閉じた後も見張りが続いている")
	}
}
