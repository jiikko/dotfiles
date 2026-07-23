package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

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

// 連続 push/pull: 前のトーストの退場タイマー (古い seq) は後のトーストを leaving にしない。
func TestToastStaleTimerDoesNotLeaveNewer(t *testing.T) {
	var to toast
	to.show("first", true)
	oldSeq := to.seq
	for guard := 0; to.phase == toastEntering && guard < 100; guard++ {
		to.advance(false) // holding へ
	}
	to.show("second", false) // 上書き (seq 前進・entering へ)
	to.startLeaving(toastMsg{seq: oldSeq})
	if to.phase == toastLeaving || to.text != "second" {
		t.Errorf("古いタイマーが新トーストを退場させた: phase=%d text=%q", to.phase, to.text)
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
	got := to.boxLines(false)
	if len(got) != len(full) {
		t.Errorf("スライド中も全行が出るべき: got=%d 行 want=%d 行", len(got), len(full))
	}
	wv := runewidth.StringWidth(stripANSI(got[0]))
	if wv != to.shown || wv >= boxW {
		t.Errorf("入場途中の可視幅が左スライドでない: 可視幅=%d shown=%d boxW=%d", wv, to.shown, boxW)
	}
	// holding まで進めると全幅 + ✓/text
	for guard := 0; to.phase == toastEntering && guard < 100; guard++ {
		to.advance(false)
	}
	lines := to.boxLines(false)
	plain := stripANSI(strings.Join(lines, "\n"))
	if runewidth.StringWidth(stripANSI(lines[0])) != boxW || !strings.Contains(plain, "✓") || !strings.Contains(plain, "pushed") {
		t.Errorf("全表示に ✓/pushed が無い / 全幅でない:\n%s", plain)
	}
	// 失敗は ✗
	var ng toast
	ng.show("failed", false)
	for guard := 0; ng.phase == toastEntering && guard < 100; guard++ {
		ng.advance(false)
	}
	if !strings.Contains(stripANSI(strings.Join(ng.boxLines(false), "\n")), "✗") {
		t.Error("失敗トーストに ✗ が無い")
	}
	// 非表示は nil
	var empty toast
	if empty.boxLines(false) != nil {
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
