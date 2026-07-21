package main

import (
	"strings"
	"testing"
	"time"

	"glogx/usage"

	"github.com/mattn/go-runewidth"
)

// overlayBoxTopRight は box を右上へ右揃えで重ね、覆った各行の表示幅が width ちょうどに
// 揃うこと。box 行より下のウィンドウ行は変わらないこと。
func TestOverlayBoxTopRightAligns(t *testing.T) {
	window := []string{"commit line one", "author line two", "date line three", "keep me"}
	box := []string{"┌ usage ─┐", "│ 5h ok │", "└────────┘"}
	width := 40
	got := overlayBoxTopRight(window, box, width, false)

	for i, b := range box {
		if !strings.HasSuffix(got[i], b) {
			t.Errorf("行 %d が box 行で終わっていない: %q", i, got[i])
		}
		if w := runewidth.StringWidth(stripANSI(got[i])); w != width {
			t.Errorf("行 %d の表示幅 = %d, want %d", i, w, width)
		}
	}
	if got[3] != "keep me" {
		t.Errorf("box より下の行を壊した: %q", got[3])
	}
}

// 覆う行の左側 (見えている部分) の色は保持される (取得中に上部行の色が抜けない回帰)。
func TestOverlayBoxTopRightKeepsLeftColor(t *testing.T) {
	colored := ansiGreen + "green subject text here" + ansiReset
	window := []string{colored}
	box := []string{"┌ usage ┐"}
	got := overlayBoxTopRight(window, box, 40, true)
	if !strings.Contains(got[0], ansiGreen) {
		t.Errorf("左側の色 (%q) が保持されていない: %q", ansiGreen, got[0])
	}
	// 幅は width ちょうど、右端は box。
	if w := runewidth.StringWidth(stripANSI(got[0])); w != 40 {
		t.Errorf("表示幅 = %d, want 40", w)
	}
	if !strings.HasSuffix(got[0], box[0]) {
		t.Errorf("右端が box で終わっていない: %q", got[0])
	}
}

// box が window より高くても (行数超過) パニックせず、収まる分だけ重ねる。
func TestOverlayBoxTopRightTallBox(t *testing.T) {
	window := []string{"only one row"}
	box := []string{"row0", "row1", "row2"}
	got := overlayBoxTopRight(window, box, 20, false)
	if len(got) != 1 {
		t.Fatalf("行数が変わった: %d", len(got))
	}
	if w := runewidth.StringWidth(stripANSI(got[0])); w != 20 {
		t.Errorf("表示幅 = %d, want 20", w)
	}
}

func TestOverlayBoxTopRightEmpty(t *testing.T) {
	if got := overlayBoxTopRight(nil, []string{"x"}, 10, false); got != nil {
		t.Errorf("空ウィンドウで nil を返さない: %v", got)
	}
	window := []string{"a"}
	_ = overlayBoxTopRight(window, nil, 10, false)          // 空 box: 何もしない
	_ = overlayBoxTopRight(window, []string{"x"}, 0, false) // width0: panic しなければ OK
}

// 起動時は表示、任意キーで非表示、U で再表示 (ユーザー要望の「何か押したら消える」)。
func TestUsageOverlayDismiss(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)
	if !m.usageVisible {
		t.Fatal("起動時に usageVisible=false")
	}
	m.handleKey("j") // 何かキー → 消える
	if m.usageVisible {
		t.Error("キー押下後も usageVisible=true (消えていない)")
	}
	m.handleKey("U") // U で再表示
	if !m.usageVisible {
		t.Error("U で再表示されない")
	}
	m.handleKey("U") // U でまた非表示 (トグル)
	if m.usageVisible {
		t.Error("U トグルで非表示にならない")
	}
}

// U は push 確認モーダルを素通りせず、通常キー = キャンセルとして扱われる (footgun 回帰)。
func TestUsageToggleDoesNotBypassConfirmModal(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.pushConfirm = true
	visBefore := m.usageVisible
	m.handleKey("U")
	if m.pushConfirm {
		t.Error("U が push 確認モーダルをキャンセルしていない (残った確認へ Enter で誤 push する footgun)")
	}
	if m.usageVisible != visBefore {
		t.Error("モーダル中の U が usage をトグルした (モーダルのキャンセル語彙を優先すべき)")
	}
}

// U は tmux prefix pending を素通りせず、通常キーとして pending を消費する (次キー誤飲み込み回帰)。
func TestUsageToggleDoesNotBypassPrefixPending(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.tmuxPrefix = "ctrl+t"
	m.prefixPending = true
	m.handleKey("U")
	if m.prefixPending {
		t.Error("U が prefixPending を消費していない (次キーが誤って飲み込まれる残留)")
	}
}

// 取得待ち = spinnerActive で tick が回る (スピナーが animate する前提)。取得完了で止まる。
func TestUsageLoadingDrivesSpinner(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil) // toFetch なし = CI fetch は動かない
	if !m.usageLoading() {
		t.Fatal("起動直後は usageLoading=true のはず")
	}
	if !m.spinnerActive() {
		t.Error("usage 取得中に spinnerActive=false (tick が回らずスピナーが止まる)")
	}
	// 結果到着でローディング終了 → spinner 対象から外れる。
	m.usageSnap = &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(time.Hour)},
	}}
	if m.usageLoading() {
		t.Error("snap 到着後も usageLoading=true")
	}
	if m.spinnerActive() {
		t.Error("他に動くものが無いのに spinnerActive=true (tick が止まらない)")
	}
}

// 取得中の box はスピナー行を含み、成功時は枠ごとに 1 行 + 罫線で複数行になる。
func TestUsageBoxLines(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)

	loading := m.usageBoxLines()
	if len(loading) < 3 { // 上罫線 + 内容 + 下罫線 (影付きは更に多い)
		t.Fatalf("取得中の box 行数が少ない: %d", len(loading))
	}
	joined := strings.Join(loading, "\n")
	if !strings.Contains(stripANSI(joined), "取得中") {
		t.Errorf("取得中 box に '取得中' が無い:\n%s", stripANSI(joined))
	}

	m.usageSnap = &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(4 * time.Hour)},
		{Label: "7d", Percent: 29, ResetAt: time.Now().Add(50 * time.Hour)},
	}}
	box := m.usageBoxLines()
	plain := stripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, "5h") || !strings.Contains(plain, "7d") {
		t.Errorf("成功 box に 5h/7d が無い:\n%s", plain)
	}

	// 非表示なら nil。
	m.usageVisible = false
	if m.usageBoxLines() != nil {
		t.Error("非表示で nil を返さない")
	}
}
