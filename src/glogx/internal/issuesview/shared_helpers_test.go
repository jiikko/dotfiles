package issuesview

import (
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/testenv"
	"glogx/issues"
	"math"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (両方のテストが使っていたもの。main の側は公開の口と Fixture へ書き換えて減らしていく)。

// loadedView は receive 済みの viewer を返す。
func loadedView(list ...*issues.Issue) *IssuesView {
	v := newTestIssuesView()
	v.shown = true
	v.Receive(IssuesScanMsg{dirs: []string{"/repo/issues"}, issues: list})
	return &v
}

// vp は窓の寸法 (キー処理へ渡す)。幅は renderOpts と同じにする — テストだけ幅 0 で駆動すると、
// 幅で行数が変わるヘッダーを足したときに本番でだけ page 分割がずれる。
func vp(page int) IssuesViewport { return renderOpts(page).Viewport() }

func renderOpts(page int) IssuesRenderOpts {
	return IssuesRenderOpts{Width: 80, Page: page, Colored: false}
}

// newTestIssuesView は閉じる演出を切った viewer。🚨 既存テストは「close したら即座に畳まれて
// いる」前提で書かれているので、演出を挟むと全て 1 拍待ちになって読めなくなる。演出そのものは
// issues_close_anim_test.go が明示的に on にして検査する (newTestBrowse の zoom.off と同じ)。
func newTestIssuesView() IssuesView {
	v := NewIssuesView()
	v.closeAnimOff = true
	return v
}

// drainScanMsg は tea.Cmd (Batch を含む) から issuesScanMsg を取り出す。
func drainScanMsg(t *testing.T, cmd tea.Cmd) IssuesScanMsg {
	t.Helper()
	switch msg := cmd().(type) {
	case IssuesScanMsg:
		return msg
	case tea.BatchMsg:
		for _, c := range msg {
			if c == nil {
				continue
			}
			if scan, ok := c().(IssuesScanMsg); ok {
				return scan
			}
		}
	}
	t.Fatal("Cmd から issuesScanMsg が取れない")
	return IssuesScanMsg{}
}

// landBody は一覧のカーソル行を Enter で開き、引き出しの着地まで済ませる。
// 🚨 着地させずに描画を検査すると、引き出しの幅が 0 のままで本文が描かれず、一覧側の文字列に
// 一致して緑になる (本文ヘッダーの状態の assert が一覧の空表示の案内に一致する)。
func landBody(t *testing.T, v *IssuesView, page int) {
	t.Helper()
	v.HandleKey("enter", vp(page))
	if v.open == nil || v.body == nil {
		t.Fatal("本文モードに入れていない")
	}
	v.drawer.finish()
}

// pagerShown は p の今の表示位置 (glide 中は途中位置)。行数の上限で丸めない。
func pagerShown(p *listnav.Pager) int { return p.DrawOffset(math.MaxInt32, 1) }

// realIssuesView は実ファイルを持つ viewer を作る (openBody は ReadBody を通るので、
// fake パスの loadedView では本文モードに入れない)。
func realIssuesView(t *testing.T) *IssuesView {
	t.Helper()
	dir := t.TempDir()
	names := []string{"030", "029"}
	list := make([]*issues.Issue, 0, len(names))
	for _, n := range names {
		list = append(list, issuesviewtest.WriteRealIssue(t, dir, n+"-feat-x.md", "# "+n+" feat: x\n\n本文の段落。\n\n- 箇条書き\n"))
	}
	return loadedView(list...)
}

// requireFake は見張りが seam 経由でフェイクを掴んでいることを確かめる (実 fsnotify に
// 戻っていたら、この検査は CI では走らなくなっているので停止させる)。
func requireFake(t *testing.T, v *IssuesView) *testenv.FakeWatcher {
	t.Helper()
	fw, ok := v.watch.W.(*testenv.FakeWatcher)
	if !ok {
		t.Fatalf("startWatch が newDirWatcher seam を通っていない (w=%T)。この検査は実 fsnotify に依存している", v.watch.W)
	}
	return fw
}

// observe は watchCmd と同じ指紋を同期で計算する (tea.Tick を待たずにテストで駆動するため)。
// 対象集合は production と同じ watchTargets を通す (ここで再実装すると本番とずれる)。
func observeIssues(v *IssuesView) IssuesWatchMsg {
	return IssuesWatchMsg{fp: issuesFingerprint(v.watchTargets())}
}

// scanOf は実ファイルを走査した結果を返す。🚨 中身を再実装せず production の scanIssues を
// 呼ぶ: 指紋の取り方がずれると「テストでは基準が揃うのに本番ではずれる」形で穴を見逃す。
func scanOf(t *testing.T, root string) IssuesScanMsg {
	t.Helper()
	msg := scanIssues(root)
	if len(msg.dirs) == 0 {
		t.Fatalf("issues ディレクトリが見つからない: %s", root)
	}
	return msg
}
