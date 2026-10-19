package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glogx/internal/gitdata"
	"glogx/internal/issuesview"
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/testenv"
	"glogx/issues"
)

// issuesView は browseModel を参照しないので、モデルを組まずに直接駆動できる
// (この独立性が「結合を最後にする」を成立させている。壊れたら結合コストが跳ね上がる)。

// loadedView は receive 済みの viewer を返す。
func loadedView(list ...*issues.Issue) *issuesview.IssuesView {
	v := newTestIssuesView()
	v.Fixture().SetShown(true)
	v.Receive(issuesview.FixtureScanMsg("", []string{"/repo/issues"}, list))
	return &v
}

func renderOpts(page int) issuesview.IssuesRenderOpts {
	return issuesview.IssuesRenderOpts{Width: 80, Page: page, Colored: false}
}

// hint を組む予算と描画側が切る幅は同じ 1 か所 (browseModel.hintWidth) から取る。
// TestStatusHintUsesRenderBudget の issues viewer 版。
func TestIssuesHintUsesRenderBudget(t *testing.T) {
	assertHintFitsAtEveryWidth(t, "", func(m *browseModel) {
		m.issuesOv.Fixture().SetShown(true)
		m.issuesOv.Receive(issuesview.FixtureScanMsg("", []string{"/repo/issues"}, issuesviewtest.SampleIssues()))
	})
}

// Msg 経路 (issuesScanMsg) からの notice 配達の回帰テスト。
// rebindOpen が置く「開いていた issue が見つかりません」は、打鍵を待たずスキャン結果を
// 受けたフレームで配達される (トースト + w でコピーできる lastWarning)。
// takeNotice が打鍵経路にしか無いと、本文が無言で畳まれて見え、直後に q を押すと
// 理由が 1 度も描かれないままプロセスが終わった。
// 🚨 testenv.NoticeOKOf(v.takeNotice()) を直接 assert しない: それは配達経路 (browseModel の Update) を通らず、
// 配達ブロックを丸ごと削っても green のままになる (この issue 自身が見つけた false green)。
func TestIssuesScanMsgDeliversRebindNotice(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	m.handleKey("i")
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n\n本文の行\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := issuesviewtest.Path2Issue(dir, "001-feat-x.md")
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	m.issuesOv.Fixture().SetCwd(root)
	m.Update(issuesview.FixtureScanMsg(root, []string{dir}, []*issues.Issue{iss}))
	m.issuesOv.Fixture().FinishAnim()
	m.handleKey("enter")
	m.issuesOv.Fixture().FinishDrawer() // 本文モードへ

	// 別プロセスがファイルを消したスキャン結果が Msg 経路で届く (キーは押さない)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m.Update(issuesview.FixtureScanMsg(root, []string{dir}, nil))

	if !strings.Contains(m.lastWarning, "見つかりません") {
		t.Errorf("畳んだ理由が lastWarning に届いていない (q で恒久喪失する): %q", m.lastWarning)
	}
	if !m.toast.Visible() {
		t.Error("畳んだ理由のトーストが積まれていない")
	}
	if text, _ := testenv.NoticeOKOf(m.issuesOv.TakeNotice()); text != "" {
		t.Errorf("notice が取り出されずに残っている (配達漏れ): %q", text)
	}
}

// browseModel の配線: issuesScanMsg の case が receive の戻り値 (取り直しの予約) を返すこと。
// 🚨 ここを捨てても issuesView 単体のテストは全部通り、lint も通らない (errcheck 系は error 以外の
// 戻り値放棄を見ない)。予約機構の可視効果はこの 1 行に乗っているので model 層で固定する。
func TestBrowseIssuesScanMsgPropagatesRescanCmd(t *testing.T) {
	iss := issuesviewtest.RealIssue(t)
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(iss)

	if cmd := m.issuesOv.Fixture().StartScan(); cmd == nil {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}
	if cmd := m.issuesOv.ReloadAfterEdit(); cmd != nil || !m.issuesOv.Fixture().RescanPending() {
		t.Fatalf("前提が崩れた: 予約が立っていない (cmd=%v pending=%v)", cmd != nil, m.issuesOv.Fixture().RescanPending())
	}

	_, cmd := m.Update(issuesview.FixtureScanMsg("", []string{iss.Dir}, []*issues.Issue{iss}))
	if cmd == nil {
		t.Error("issuesScanMsg の case が取り直しの Cmd を捨てている (予約が失われる)")
	}
}

// newTestIssuesView は閉じる演出を切った viewer。🚨 既存テストは「close したら即座に畳まれて
// いる」前提で書かれているので、演出を挟むと全て 1 拍待ちになって読めなくなる。演出そのものは
// issues_close_anim_test.go が明示的に on にして検査する (newTestBrowse の zoom.off と同じ)。
func newTestIssuesView() issuesview.IssuesView {
	v := issuesview.NewIssuesView()
	v.Fixture().SetCloseAnim(false)
	return v
}

// URL ピッカー入力中は、外側の U 横取りを止めて viewer へ委譲する。
//
// urlPicker は doc で「印字文字はすべて検索語に流す。ここで個別のキーを先に横取りすると、
// その文字を含む URL を検索できなくなる」と宣言しているのに、tui.go が委譲の 3 行前で
// 大文字 U だけを奪っていた。`github.com/Ueno/...` のような URL を絞り込めない (silent)。
func TestIssuesViewerOwnsKeysDuringURLPicker(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(issuesviewtest.RealIssue(t))
	if !m.issuesOv.Fixture().OpenURLPicker([]string{"https://github.com/Ueno/repo", "https://example.com/x"}) {
		t.Fatal("前提が崩れた: URL ピッカーが開かない")
	}
	// 🚨 起動時グランスの既定を継承しないこと。継承すると片方向で vacuous になる
	m.usageOv.Fixture().SetVisible(false)

	m.handleKey("U")

	// 本命の主張はこちら: キーが viewer へ届いているか
	if q := m.issuesOv.Fixture().URLPickerQuery(); q != "U" {
		t.Errorf("U が検索語に届いていない (外側が横取りしている): query=%q (期待 \"U\")", q)
	}
	// 🚨 こちらは別の主張 (残量モーダルを開かない)。混ぜて 1 つの非難文にすると、dismiss の
	// 位置を変えただけで「キーを奪っている」と誤診する
	if m.usageOv.Visible() {
		t.Error("URL ピッカー入力中の U が残量モーダルを開いた")
	}
}

// 🚨 述語の 2 節目 (numFilter.typing) を pin する。これが無いと節ごと削る変異も、
// typing→active の 1 語変異も**全テスト green のまま通る**。
//
// active でなく typing を見る理由: 絞り込みを確定した後 (typing=false, active=true) は通常の
// ナビゲーションなので、そこで U を殺すと**絞り込みを解くまで U が恒久的に死ぬ**。
func TestIssuesViewerOwnsKeysWhileTypingNumberFilter(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(issuesviewtest.RealIssue(t))
	m.issuesOv.Fixture().SetNumberFilterTyping()
	if !m.issuesOv.OwnsKeys() {
		t.Fatal("番号入力中に ownsKeys() が false")
	}
	m.usageOv.Fixture().SetVisible(false)
	m.handleKey("U")
	if m.usageOv.Visible() {
		t.Error("番号入力中の U が残量モーダルを開いた (入力モードのキーを外側が奪っている)")
	}

	// 確定後は通常のナビゲーションに戻る = U は外側で受ける
	// 🚨 数字を打ってから確定すること。空入力の確定は confirmNumberFilter が解除へ回すので active も
	//   false になり、typing→active の変異を素通りさせる (再現手順は / → 0 → Enter)
	if !m.issuesOv.Fixture().EditNumberFilter("0") {
		t.Fatal("前提が崩れた: 番号フィルタが数字を受け付けない")
	}
	m.issuesOv.Fixture().ConfirmNumberFilter()
	if !m.issuesOv.Fixture().NumberFilterActive() {
		t.Fatal("前提が崩れた: 確定後に絞り込みが残っていない")
	}
	if m.issuesOv.OwnsKeys() {
		t.Fatal("絞り込み確定後も ownsKeys() が true (U が恒久的に死ぬ)")
	}
	releaseKey(m) // 指を離す (上の U とキーリピート判定を跨ぐ)
	m.handleKey("U")
	if !m.usageOv.Visible() {
		t.Error("絞り込み確定後に U が効かない (typing でなく active を見ている)")
	}
}

// 🚨 述語の 3 節目 (markNext.active) を pin する。
//
// 変更前は確認中の U で usage が開き、**確認が armed のまま裏に残った** (続く y で実ファイル移動)。
// 変更後は U が「y/Enter 以外 = 取り消し」に落ちる (spec の「知らないキーを押したら実ファイルが
// 動いた、を作らない」と一致)。安全側の変化なので固定する。
func TestIssuesViewerOwnsKeysDuringMarkNextConfirm(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(issuesviewtest.RealIssue(t))
	m.issuesOv.Fixture().SetMarkNextActive()
	if !m.issuesOv.OwnsKeys() {
		t.Fatal("確認中に ownsKeys() が false")
	}
	m.usageOv.Fixture().SetVisible(false)

	m.handleKey("U")

	if m.usageOv.Visible() {
		t.Error("確認中の U が残量モーダルを開いた (確認が armed のまま裏に残る)")
	}
	if m.issuesOv.Fixture().MarkNextActive() {
		t.Error("確認が取り消されていない (知らないキーで実ファイルが動く側に倒れている)")
	}
}

// 🚨 ガードが広すぎないこと: viewer を開いているだけ (どのモードにも入っていない) なら
// U は従来どおり外側で受ける (ユーザー要望 2026-08-01 の「viewer の上でも U は効く」)。
// これが無いと「ownsKeys() を常に true にする」変異が素通りする。
func TestIssuesViewerUsageStillWorksWhenNotOwningKeys(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(issuesviewtest.RealIssue(t))
	if m.issuesOv.OwnsKeys() {
		t.Fatal("前提が崩れた: 一覧モードで ownsKeys() が true")
	}
	m.usageOv.Fixture().SetVisible(false)

	m.handleKey("U")

	if !m.usageOv.Visible() {
		t.Error("viewer の一覧モードで U が効かない (横取りを止めすぎている)")
	}
}
