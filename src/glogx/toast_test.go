package main

import (
	"strings"
	"testing"
)

// visible は「1 枚でも出ているか」のテスト用ショートハンド。本番の描画は boxLines、フレーム
// 判定は animating を使うので、この畳んだ判定はテストの assert からしか要らない。
func (s *toast) visible() bool { return s.toastItem.visible() || len(s.older) > 0 }

// show → entering、tick で右画面外から左へ滑り込み (shown 0→boxWidth)、入場完了で holding +
// 退場タイマー、startLeaving → leaving、tick で右へ滑り出て hidden、という一連の状態遷移。
func TestToastLifecycle(t *testing.T) {
	var to toast
	if to.visible() || to.animating() {
		t.Fatal("初期は非表示・非アニメ")
	}
	to.show("done", true)
	if !to.visible() || to.phase != toastEntering || !to.ok || to.text != "done" {
		t.Fatalf("show 後: phase=%d visible=%v ok=%v text=%q", to.phase, to.visible(), to.ok, to.text)
	}
	boxW := to.boxWidth(false)
	if boxW < 10 {
		t.Fatalf("箱幅が不足: %d", boxW)
	}
	// 入場: holding になるまで advance。退場タイマー (holdCmd) は入場完了時に 1 回だけ返る
	var holdCmds, guard int
	for to.phase == toastEntering && guard < 100 {
		if cmd := to.advance(false); cmd != nil {
			holdCmds++
		}
		guard++
	}
	if to.phase != toastHolding || to.shown != boxW {
		t.Fatalf("入場完了後: phase=%d shown=%d (want holding/%d)", to.phase, to.shown, boxW)
	}
	if holdCmds != 1 {
		t.Errorf("退場タイマーは入場完了時に 1 回だけ返るべき: %d", holdCmds)
	}
	if to.animating() {
		t.Error("holding 中は animating=false (tick 不要)")
	}
	// holding 明け → leaving
	to.startLeaving(toastMsg{seq: to.seq})
	if to.phase != toastLeaving || !to.animating() {
		t.Fatalf("startLeaving 後: phase=%d animating=%v", to.phase, to.animating())
	}
	// 退場: hidden まで
	guard = 0
	for to.visible() && guard < 100 {
		to.advance(false)
		guard++
	}
	if to.phase != toastHidden || to.visible() || to.text != "" {
		t.Fatalf("退場完了後: phase=%d visible=%v text=%q", to.phase, to.visible(), to.text)
	}
}

// advanceToHolding は entering のトーストを holding まで tick で進める (テスト用ヘルパー)。
func advanceToHolding(to *toast) {
	for guard := 0; to.phase == toastEntering && guard < 100; guard++ {
		to.advance(false)
	}
}

// 連続 push/pull: 前のトーストの退場タイマー (古い seq) は後のトーストを leaving にしない。
// 新トーストを holding まで進めた状態で試すことで、phase 条件では弾けず seq ガードだけが
// 分岐を左右する場面を作る (startLeaving の `msg.seq == t.seq` を消すと最初の assert が落ちる)。
func TestToastStaleTimerDoesNotLeaveNewer(t *testing.T) {
	var to toast
	to.show("first", true)
	oldSeq := to.seq
	advanceToHolding(&to)    // 1つ目を holding へ
	to.show("second", false) // 上書き (seq 前進・entering へリセット)
	advanceToHolding(&to)    // 2つ目も holding へ = phase==holding は満たされ、残る守りは seq のみ

	// 古い世代のタイマー (oldSeq) が届いても、seq 不一致なので新トーストを退場させない。
	to.startLeaving(toastMsg{seq: oldSeq})
	if to.phase != toastHolding || to.text != "second" {
		t.Errorf("古い seq のタイマーが新トーストを退場させた: phase=%d text=%q", to.phase, to.text)
	}
	// 正しい世代のタイマーなら退場に入る (seq ガードは一致時に通す、の対検証)。
	to.startLeaving(toastMsg{seq: to.seq})
	if to.phase != toastLeaving {
		t.Errorf("正しい seq で退場に入らない: phase=%d", to.phase)
	}
}

// 入場途中は箱の左 shown カラムだけ (可視幅=shown、全幅未満)、holding で全幅が出る。横スライド。
func TestToastBoxLinesRevealsLeftColumns(t *testing.T) {
	var to toast
	to.show("pushed", true) // ASCII のみ (全角境界の半端幅を避け、可視幅=shown を厳密比較)
	boxW := to.boxWidth(false)
	full := to.fullBox(false)
	// 入場 1 フレーム: 全行が出るが、各行の可視幅は shown (<boxW) に切られている
	to.advance(false)
	got := to.toastItem.boxLines(false) // 1 枚分の描画を見る (スタックの行数上限とは別)
	if len(got) != len(full) {
		t.Errorf("スライド中も全行が出るべき: got=%d 行 want=%d 行", len(got), len(full))
	}
	wv := dispWidth(got[0])
	if wv != to.shown || wv >= boxW {
		t.Errorf("入場途中の可視幅が左スライドでない: 可視幅=%d shown=%d boxW=%d", wv, to.shown, boxW)
	}
	// holding まで進めると全幅 + ✓/text
	advanceToHolding(&to)
	lines := to.toastItem.boxLines(false)
	plain := stripANSI(strings.Join(lines, "\n"))
	if dispWidth(lines[0]) != boxW || !strings.Contains(plain, "✓") || !strings.Contains(plain, "pushed") {
		t.Errorf("全表示に ✓/pushed が無い / 全幅でない:\n%s", plain)
	}
	// 失敗は ✗
	var ng toast
	ng.show("failed", false)
	advanceToHolding(&ng)
	if !strings.Contains(stripANSI(strings.Join(ng.toastItem.boxLines(false), "\n")), "✗") {
		t.Error("失敗トーストに ✗ が無い")
	}
	// 非表示は nil
	var empty toast
	if empty.toastItem.boxLines(false) != nil {
		t.Error("非表示で nil を返さない")
	}
}

// 右下合成: box は window の下端行に載り、その行の左背景は保持され、対象外の行は不変。
func TestOverlayBoxBottomRightKeepsLeftAndAnchorsBottom(t *testing.T) {
	window := []string{"row0-left", "row1-left", "row2-left", "row3-left"}
	out := overlayBoxBottomRight(window, []string{"BBB"}, 20, false)
	if !strings.Contains(out[3], "BBB") {
		t.Errorf("box が下端に載っていない: %q", out[3])
	}
	if !strings.HasPrefix(out[3], "row3-left") {
		t.Errorf("下端行の左背景が保持されていない: %q", out[3])
	}
	if out[0] != "row0-left" {
		t.Errorf("box 対象外の行が変わった: %q", out[0])
	}
}

// 新しい通知は上に積まれ、古い通知は下から抜けていく (ユーザー要望 2026-07-31)。
func TestToastStacksNewestOnTop(t *testing.T) {
	var s toast
	s.show("1 番目", true)
	s.show("2 番目", true)
	s.show("3 番目", true)

	items := s.items()
	if len(items) != 3 {
		t.Fatalf("枚数 = %d, want 3", len(items))
	}
	// items は上から下。最新が上、最古が下
	for i, want := range []string{"3 番目", "2 番目", "1 番目"} {
		if items[i].text != want {
			t.Errorf("上から %d 枚目 = %q, want %q", i+1, items[i].text, want)
		}
	}
	// 埋め込みは常に最新を指す (呼び出し側とテストが t.text で読む前提)
	if s.text != "3 番目" {
		t.Errorf("埋め込みが最新でない: %q", s.text)
	}
}

// 上限を超えたら一番古い (一番下) を捨てる。画面を覆わないための guard。
func TestToastStackCapsOldest(t *testing.T) {
	var s toast
	for _, txt := range []string{"1", "2", "3", "4", "5"} {
		s.show(txt, true)
	}
	items := s.items()
	if len(items) != toastStackMax {
		t.Fatalf("枚数 = %d, want %d (上限)", len(items), toastStackMax)
	}
	if items[0].text != "5" {
		t.Errorf("最上段 = %q, want 5 (最新)", items[0].text)
	}
	for _, it := range items {
		if it.text == "1" || it.text == "2" {
			t.Errorf("古い通知が残っている: %q", it.text)
		}
	}
}

// 抜けた枚は取り除かれ、残りが繰り上がる (下から抜けていく)。
func TestToastStackRemovesFinishedFromBottom(t *testing.T) {
	var s toast
	s.show("古い", true)
	s.show("新しい", true)
	// 入場を終わらせる
	for range toastSlideFrames + 2 {
		s.advance(false)
	}
	// 古い方 (下) の静止が明けて退場 → 抜け切るまで進める
	oldSeq := s.older[0].seq
	s.startLeaving(toastMsg{seq: oldSeq})
	for range toastSlideFrames + 2 {
		s.advance(false)
	}
	if len(s.older) != 0 {
		t.Errorf("抜けた枚が残っている: %+v", s.older)
	}
	if s.text != "新しい" || !s.visible() {
		t.Errorf("残るべき枚が消えた: text=%q visible=%v", s.text, s.visible())
	}
}

// 退場タイマーは枚ごとに独立 (seq が一致した枚だけ動く)。
func TestToastStackLeavingIsPerItem(t *testing.T) {
	var s toast
	s.show("古い", true)
	s.show("新しい", true)
	for range toastSlideFrames + 2 {
		s.advance(false)
	}
	s.startLeaving(toastMsg{seq: s.older[0].seq})
	if s.older[0].phase != toastLeaving {
		t.Errorf("下の枚が退場に入らない: %v", s.older[0].phase)
	}
	if s.phase == toastLeaving {
		t.Error("関係ない上の枚まで退場に入った (seq の取り違え)")
	}
}

// 描画は上から下に並ぶ (新しいものが上)。
func TestToastStackBoxLinesOrder(t *testing.T) {
	var s toast
	s.show("古い通知", true)
	s.show("新しい通知", true)
	for range toastSlideFrames + 2 {
		s.advance(false)
	}
	out := strings.Join(s.boxLines(false, 100), "\n")
	iNew, iOld := strings.Index(out, "新しい通知"), strings.Index(out, "古い通知")
	if iNew < 0 || iOld < 0 {
		t.Fatalf("両方描かれていない:\n%s", out)
	}
	if iNew > iOld {
		t.Errorf("新しい通知が下に来ている (上に積むはず):\n%s", out)
	}
}

// 進行中トースト (…シアン) は新しい通知が来たら退く。⚠️ 積んだままにすると「PR を検索中...」の
// 下に結果が並び、終わったのに検索中と書いてある状態が数秒残る (実測 2026-07-31)。
func TestToastInfoIsSupersededByResult(t *testing.T) {
	var s toast
	s.showInfo("PR を検索中...")
	s.show("PR #123 を開きます", true)
	items := s.items()
	if len(items) != 1 {
		t.Fatalf("枚数 = %d, want 1 (進行中は退く): %+v", len(items), items)
	}
	if items[0].text != "PR #123 を開きます" {
		t.Errorf("残ったのが結果でない: %q", items[0].text)
	}
	// 下に積まれていた進行中も落ちる
	var s2 toast
	s2.show("先行の結果", true)
	s2.showInfo("検索中...")
	s2.show("新しい結果", true)
	for _, it := range s2.items() {
		if it.info {
			t.Errorf("進行中が残っている: %q", it.text)
		}
	}
	if len(s2.items()) != 2 {
		t.Errorf("枚数 = %d, want 2 (結果 2 枚)", len(s2.items()))
	}
}

// 箱の行数は一定 (上罫線 + 内容 + 下罫線 + 影)。行数上限の計算がこれに依存している。
func TestToastBoxLineCount(t *testing.T) {
	var s toast
	s.show("x", true)
	advanceToHolding(&s)
	if got := len(s.toastItem.boxLines(false)); got != toastBoxLines {
		t.Errorf("箱の行数 = %d, want %d (toastBoxLines を直すこと)", got, toastBoxLines)
	}
}

// 行数上限を超える古い枚は出さない。箱の途中で切らない。最新は上限を超えても出す。
func TestToastBoxLinesRespectsMaxLines(t *testing.T) {
	var s toast
	for _, txt := range []string{"1", "2", "3"} {
		s.show(txt, true)
	}
	for range toastSlideFrames + 2 {
		s.advance(false)
	}
	for _, c := range []struct {
		maxLines  int
		wantBoxes int
	}{
		{100, 3},
		{toastBoxLines * 2, 2},
		{toastBoxLines, 1},
		{1, 1}, // 上限より箱が大きくても最新 1 枚は出す (見えない通知より覆う通知)
	} {
		got := len(s.boxLines(false, c.maxLines))
		if want := c.wantBoxes * toastBoxLines; got != want {
			t.Errorf("maxLines=%d: %d 行 (%d 箱), want %d 行 (%d 箱)",
				c.maxLines, got, got/toastBoxLines, want, c.wantBoxes)
		}
	}
}
