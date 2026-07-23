package main

import (
	"strings"
	"testing"
)

// show → entering、tick で下端からせり上がり (revealed 0→full)、入場完了で holding + 退場タイマー、
// startLeaving → leaving、tick で縮んで hidden、という一連の状態遷移。
func TestToastLifecycle(t *testing.T) {
	var to toast
	if to.visible() || to.animating() {
		t.Fatal("初期は非表示・非アニメ")
	}
	to.show("done", true)
	if !to.visible() || to.phase != toastEntering || !to.ok || to.text != "done" {
		t.Fatalf("show 後: phase=%d visible=%v ok=%v text=%q", to.phase, to.visible(), to.ok, to.text)
	}
	full := len(to.fullBox(false))
	if full < 3 {
		t.Fatalf("box 行数が不足: %d", full)
	}
	// 入場: full 回 advance で holding + 退場タイマーが 1 回だけ返る
	var holdCmds int
	for range full {
		if cmd := to.advance(full); cmd != nil {
			holdCmds++
		}
	}
	if to.phase != toastHolding || to.revealed != full {
		t.Fatalf("入場完了後: phase=%d revealed=%d (want holding/%d)", to.phase, to.revealed, full)
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
	// 退場: full 回 advance で hidden
	for range full {
		to.advance(full)
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
	full := len(to.fullBox(false))
	for range full {
		to.advance(full) // holding へ
	}
	to.show("second", false) // 上書き (seq 前進・entering へ)
	to.startLeaving(toastMsg{seq: oldSeq})
	if to.phase == toastLeaving || to.text != "second" {
		t.Errorf("古いタイマーが新トーストを退場させた: phase=%d text=%q", to.phase, to.text)
	}
}

// 入場途中は下端 revealed 行だけ (せり上がり)、holding で全行が出る。
func TestToastBoxLinesRevealsFromBottom(t *testing.T) {
	var to toast
	to.show("pushed", true)
	full := to.fullBox(false)
	// entering・revealed=1: 下端 1 行のみ (full の最終行)
	to.advance(len(full))
	got := to.boxLines(false)
	if len(got) != 1 || stripANSI(got[0]) != stripANSI(full[len(full)-1]) {
		t.Errorf("せり上がり 1 行目が box 下端でない: got=%d行", len(got))
	}
	// holding まで進めると全行 + ✓/text
	for range full {
		to.advance(len(full))
	}
	plain := stripANSI(strings.Join(to.boxLines(false), "\n"))
	if len(to.boxLines(false)) != len(full) || !strings.Contains(plain, "✓") || !strings.Contains(plain, "pushed") {
		t.Errorf("全表示に ✓/pushed が無い / 行数不一致:\n%s", plain)
	}
	// 失敗は ✗
	var ng toast
	ng.show("failed", false)
	for range len(ng.fullBox(false)) {
		ng.advance(len(ng.fullBox(false)))
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
