package main

import (
	"testing"

	"glogx/internal/gitdata"
)

// handleKey の段の優先 (2 つの段が同時に成り立つときに、どちらが先に受けるか)。各段を 1 つずつ確かめるテストは、
// 段の並びを入れ替えても緑のまま通る (隣り合う段を入れ替える変異で確かめた) ので、同時に成り立つ状態を作って見る。

// 再起動ダイアログが出ている間の C / X は、ダイアログが受けて閉じる。update を始めない
// (始めると、ダイアログが残ったまま update の確認が重なる)。
func TestRestartPromptTakesUpdateKeys(t *testing.T) {
	for _, key := range []string{"C", "X"} {
		t.Run(key, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
			m.restartPending = true
			if !m.restartPromptVisible() {
				t.Fatal("前提: 再起動ダイアログが出ていない")
			}
			m.handleKey(key)
			if m.actModal.active() {
				t.Errorf("ダイアログが出ている間の %s で update が始まった", key)
			}
			if m.restartPending {
				t.Errorf("%s でダイアログが閉じない", key)
			}
		})
	}
}

// 右上の利用枠は、一覧の上に重ねた板 (diff) を開いている間のキーでも消える (U で出し、次のキーで消える語彙)。
func TestKeyOverDiffDismissesUsage(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	if !m.usageOv.Visible() {
		m.toggleUsage()
	}
	m.diffOv.Open(m.commits[0].SHA)
	if !m.usageOv.Visible() || !m.diffOv.Visible() {
		t.Fatalf("前提: usage=%v diff=%v", m.usageOv.Visible(), m.diffOv.Visible())
	}
	m.handleKey("j")
	if m.usageOv.Visible() {
		t.Error("diff を開いている間のキーで利用枠が消えない")
	}
}

// PR 状態のポップアップを開いている間の i は、ポップアップが受ける。issues viewer を重ねて開かない。
func TestPRStatusTakesIssuesKey(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	m.prStatusOv.Open(m.commits[0].SHA)
	if !m.prStatusOv.Visible() {
		t.Fatal("前提: PR 状態のポップアップが出ていない")
	}
	m.handleKey("i")
	if m.activeFullScreen() == FullScreenIssues {
		t.Error("PR 状態のポップアップの上で i が issues viewer を開いた")
	}
}
