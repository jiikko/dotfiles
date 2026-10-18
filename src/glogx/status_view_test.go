package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"glogx/internal/gitdata"
	"glogx/internal/statusview"
	"glogx/internal/term"
)

// --- ヘルパー ---

// newTestStatusView は「開いた状態で porcelain を読み込んだ」viewer を返す。
// 🚨 toggle() は通さない: 返り値の tea.Batch には tea.Tick (自動更新 1.5s / プレビュー 120ms) が
// 混ざっており、テストで実行すると本当に待つことになる。読み込みは loadCmd だけを同期実行する。
func newTestStatusView(t *testing.T, porcelain string) *statusview.StatusView {
	t.Helper()
	v := statusview.NewStatusView()
	v.Fixture().SetCloseAnim(false)
	v.Fixture().SetShown(true)
	stubWorktreeStatus(t, porcelain, nil)
	applyStatusLoad(t, &v)
	return &v
}

// stubWorktreeStatus は loadWorktreeStatus を差し替える (返す porcelain を後から差し替えられる
// ように、ポインタ経由で読む)。返り値のポインタへ書くと次の読み込みの結果が変わる。
func stubWorktreeStatus(t *testing.T, porcelain string, err error) *struct {
	out   string
	err   error
	calls int
} {
	t.Helper()
	state := &struct {
		out   string
		err   error
		calls int
	}{out: porcelain, err: err}
	t.Cleanup(statusview.FixtureStubWorktree(func() (string, error) {
		state.calls++
		return state.out, state.err
	}))
	return state
}

// applyStatusLoad は v.loadCmd() を同期実行して結果を receive へ渡す (tick を含まない Cmd なので
// そのまま実行できる)。
func applyStatusLoad(t *testing.T, v *statusview.StatusView) {
	t.Helper()
	cmd := v.LoadCmd()
	if cmd == nil {
		t.Fatal("loadCmd() = nil (取得中のまま?)")
	}
	msg, ok := cmd().(statusview.StatusLoadMsg)
	if !ok {
		t.Fatalf("loadCmd() の msg = %T, want statusLoadMsg", msg)
	}
	v.Receive(msg)
}

// deliverStatus は「git status が返ってきた」ことにして viewer へ流す (browseModel 経由の
// テスト用)。🚨 Update から返った Cmd を実行する経路は使えない: toggle の Batch には tick が
// 混ざっており、実行すると本当に待つ (newTestStatusView の doc と同じ理由)。
func deliverStatus(t *testing.T, v *statusview.StatusView, porcelain string) {
	t.Helper()
	v.Fixture().ReceivePorcelain(porcelain)
}

// --- stage / unstage ---

// --- カーソル (spec 4 節の不変条件 2 / 5 節) ---

// --- X (変更を捨てる) ---

// --- 自動更新 ---

// --- 描画 ---

// --- 全画面 diff (d) ---

// --- browseModel との統合 ---

func TestStatusViewerKeyTogglesFromList(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !m.statusOv.Visible() {
		t.Fatal("s で status viewer が開かない")
	}
	releaseKey(m)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.statusOv.Visible() {
		t.Fatal("s の 2 回目で閉じない (toggle でない)")
	}
}

func TestStatusViewerNotOpenedWhileJobPanelOpen(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	withJobs(m, 0)
	m.panelSHA = m.commits[0].SHA
	stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.statusOv.Visible() {
		t.Fatal("job パネル表示中に s で status viewer が開いた (パネルの語彙を優先する契約)")
	}
}

// u は viewer 内では pull を開かず「pull は p」と案内する (spec 3 節。押しても無言にしない)。
// 🚨 「確認が開かない」だけでは不十分: ガードを外しても viewer のキーに無い u は素通りして
// 何も起きないため、トーストが出ることまで固定する (ミューテーション検証 2026-08-03)。
func TestStatusViewerUKeyGuidesToP(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.statusOv.Fixture().SetShown(true)
	m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.actModal.pullConfirm {
		t.Fatal("u が status viewer 中に pull 確認を開いた (pull は p の契約)")
	}
	if !m.statusOv.Visible() {
		t.Fatal("u で viewer が閉じた")
	}
	if !m.toast.Visible() || !strings.Contains(m.toast.Text(), "p") {
		t.Fatalf("u の案内が出ていない: %q", m.toast.Text())
	}
}

// p で pull --rebase の確認を開く (ユーザー要望 2026-08-05)。
//
// 🚨 spec 3 節は「staging の途中から remote 操作へ滑る導線を作らない」として b/u を遮断して
// いた。p を通すのはその判断の一部を覆すもので、キーを一覧の u と分けているのが折衷点:
// 誤爆しやすい隣接キーではなく、明示的に p を押したときだけ remote に触る。
func TestStatusViewerPullKeyOpensConfirm(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.statusOv.Fixture().SetShown(true)
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !m.actModal.pullConfirm {
		t.Fatal("p で pull --rebase の確認が開かない")
	}
	if !m.statusOv.Visible() {
		t.Error("p で viewer が閉じた (確認は viewer の上に重ねる)")
	}
	// 🚨 確認モーダルが viewer の上に描かれること。キーは viewer より先に actModal が捌くので、
	// 描かれないと「見えないモーダルが y/N を持つ」= 画面の指示と行き先が食い違う。
	out := term.StripANSI(m.View().Content)
	if !strings.Contains(out, "pull") {
		t.Fatalf("pull 確認が viewer の上に出ていない:\n%s", out)
	}
	// y は viewer でなく確認モーダルへ届く (actModal が先に捌く契約)
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.actModal.pullConfirm {
		t.Error("y が確認モーダルに届いていない")
	}
}

// pull の成功で status viewer をその場で読み直す。自動更新は 1.5 秒周期なので、待たせると
// ヘッダーの ahead/behind が古いまま残り「p が効いていない」ように見える。
func TestPullReloadsOpenStatusViewer(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	stubWorktreeStatus(t, statusRec("## master...origin/master [behind 1]", " M a.go"), nil)
	m.statusOv.Fixture().SetShown(true)
	m.Update(pullMsg{})
	if !m.statusOv.Fixture().Loading() {
		t.Error("pull 後に status viewer を読み直していない (1.5 秒間ヘッダーが古いまま残る)")
	}
}

// b で push の確認を開く (ユーザー要望 2026-08-07。pull の p と同じく確認は viewer の上に重ねる)。
func TestStatusViewerPushKeyOpensConfirm(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.statuses[m.commits[0].SHA] = gitdata.StateUnpushed
	m.statusOv.Fixture().SetShown(true)
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if !m.actModal.pushConfirm {
		t.Fatal("b で push の確認が開かない")
	}
	if !m.statusOv.Visible() {
		t.Error("b で viewer が閉じた (確認は viewer の上に重ねる)")
	}
	out := term.StripANSI(m.View().Content)
	if !strings.Contains(out, "push") {
		t.Fatalf("push 確認が viewer の上に出ていない:\n%s", out)
	}
	// y は viewer でなく確認モーダルへ届く (actModal が先に捌く契約)
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.actModal.pushConfirm {
		t.Error("y が確認モーダルに届いていない")
	}
}

// 未 push が無いときの b は確認を開かず、理由をトーストで返す (一覧側と同じ no-op 通知)。
func TestStatusViewerPushKeyNoUnpushedShowsToast(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.statusOv.Fixture().SetShown(true)
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if m.actModal.pushConfirm {
		t.Fatal("未 push なしで push 確認が開いた")
	}
	if !m.toast.Visible() {
		t.Fatal("未 push なしの理由がトーストで出ていない")
	}
}

// push の成功で status viewer をその場で読み直し、push 演出は出さない (viewer が全画面で
// コミット一覧が見えないため。ヘッダーの ahead を即消すのは pull と同じ理由)。
func TestPushReloadsOpenStatusViewerWithoutAnim(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.statuses[m.commits[0].SHA] = gitdata.StateUnpushed
	stubWorktreeStatus(t, statusRec("## master...origin/master [ahead 1]", " M a.go"), nil)
	m.statusOv.Fixture().SetShown(true)
	m.Update(pushMsg{})
	if !m.statusOv.Fixture().Loading() {
		t.Error("push 後に status viewer を読み直していない (ヘッダーの ahead が古いまま残る)")
	}
	if m.pushAnimating {
		t.Error("viewer 表示中に push 演出が始まった (画面に無い一覧を演出しても再取得が遅れるだけ)")
	}
}

func TestStatusViewerRendersFullScreenAndHint(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	deliverStatus(t, &m.statusOv, statusRec(" M a.go"))
	view := m.View().Content
	if !strings.Contains(view, "a.go") {
		t.Fatalf("status viewer の内容が描かれていない:\n%s", view)
	}
	if strings.Contains(view, "subject") {
		t.Fatalf("全画面のはずが commit 一覧が残っている:\n%s", view)
	}
	if !strings.Contains(view, "stage/unstage") {
		t.Fatalf("hint 行が status viewer のものになっていない:\n%s", view)
	}
	// remote 操作キー (b/p) は hint に出す (発見性。ユーザー要望 2026-08-07)。
	// 🚨 view でなく hint() を直接、しかも**広い幅**で見る: hint は幅に応じて優先度の低い項目を
	// 落とすので、テストの 80 桁では b/p は出ない (出るのは「今の幅で確実に読める
	// もの」だけ。全キーの正本は --help / README)
	hint := m.statusOv.Hint(200)
	if !strings.Contains(hint, "b: push") || !strings.Contains(hint, "p: pull") {
		t.Fatalf("hint に remote 操作キー (b: push / p: pull) が出ていない: %q", hint)
	}
}

func TestStatusViewerNoticeBecomesToast(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	stubWorktreeStatus(t, statusRec("M  a.go"), nil)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	deliverStatus(t, &m.statusOv, statusRec("M  a.go"))
	m.Update(tea.KeyPressMsg{Code: 'X', Text: "X", Mod: tea.ModShift})
	if !m.toast.Visible() && m.lastWarning == "" {
		t.Fatal("viewer の notice がトースト/警告に出ていない")
	}
}

// --- 頑健性 (壊しにいく観点) ---

// hint を組む予算と、描画側が切る幅は同じ 1 か所から取る (browseModel.hintWidth)。
// 🚨 2 か所に式を書くと「収まるつもりで組んだ hint が黙って切られる」形でずれる。
func TestStatusHintUsesRenderBudget(t *testing.T) {
	// 🚨 幅を 1 点で見ない。項目単位で採るので、ある幅では予算のずれ 2 桁が余白に吸われて
	// 表に出ない。ずれが「切り詰めの …」として現れる幅は幅の刻みでしか見つからない
	// (変異検証で実測 2026-09-01: 1 点だけの検査は、組む側だけ予算をずらす変異を素通りした)。
	for w := frameMinWidth; w <= 140; w++ {
		m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
		// newTestBrowse は NoFrame なので、フレームぶん 2 桁引く経路を明示的に有効化する
		m.showFrame, m.width, m.height = true, w, 40
		if !m.frameActive() {
			t.Fatalf("w=%d: 前提が崩れた (フレームが有効でない)", w)
		}
		m.handleKey("s")
		releaseKey(m)
		line := m.hintLine()
		if got := term.DispWidth(term.StripANSI(line)); got > w {
			t.Errorf("w=%d: hint 行が端末幅を超えた (%d 桁): %q", w, got, line)
		}
		// 切り詰めの … が出ていない = 組む側の予算と描画側の clip 幅が一致している
		if strings.Contains(line, "…") {
			t.Errorf("w=%d: hint が切り詰められている (予算がずれている): %q", w, line)
		}
	}
}
