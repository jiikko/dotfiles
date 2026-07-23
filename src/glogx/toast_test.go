package main

import (
	"strings"
	"testing"
)

func TestToastShowAndDismiss(t *testing.T) {
	var to toast
	if to.visible() {
		t.Fatal("初期は非表示のはず")
	}
	cmd := to.show("done", true)
	if cmd == nil || !to.visible() || to.text != "done" || !to.ok {
		t.Fatalf("show 後: visible=%v text=%q ok=%v cmd=%v", to.visible(), to.text, to.ok, cmd != nil)
	}
	to.dismiss(toastMsg{seq: to.seq}) // 世代一致 → 消える
	if to.visible() {
		t.Error("一致 seq の dismiss で消えない")
	}
}

// 連続 push/pull で前のトーストのタイマーが後のトーストを消さない (seq 世代ガード)。
func TestToastStaleTimerDoesNotClearNewer(t *testing.T) {
	var to toast
	to.show("first", true)
	oldSeq := to.seq
	to.show("second", false) // 上書きで seq が進む
	to.dismiss(toastMsg{seq: oldSeq})
	if !to.visible() || to.text != "second" {
		t.Errorf("古いタイマーが新しいトーストを消した: visible=%v text=%q", to.visible(), to.text)
	}
	to.dismiss(toastMsg{seq: to.seq})
	if to.visible() {
		t.Error("現世代 dismiss で消えない")
	}
}

func TestToastBoxLinesMarksSuccessAndFailure(t *testing.T) {
	okT := toast{text: "pushed", ok: true}
	okPlain := stripANSI(strings.Join(okT.boxLines(false), "\n"))
	if !strings.Contains(okPlain, "✓") || !strings.Contains(okPlain, "pushed") {
		t.Errorf("成功トーストに ✓/text が無い:\n%s", okPlain)
	}
	ngT := toast{text: "failed", ok: false}
	ngPlain := stripANSI(strings.Join(ngT.boxLines(false), "\n"))
	if !strings.Contains(ngPlain, "✗") || !strings.Contains(ngPlain, "failed") {
		t.Errorf("失敗トーストに ✗/text が無い:\n%s", ngPlain)
	}
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
