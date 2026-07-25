package main

import (
	"strconv"
	"strings"
	"testing"
)

// View 1 フレーム分のコスト観測用ベンチ (CI では回さない。BenchmarkRenderLinesLargePatch と
// 同じ「必要なときにローカルで叩く」位置づけ)。
//
// 測る意味: tickMsg は spinnerActive の間 12.5fps (アニメ中は 30fps) で View を回す。RenderLines
// は linesCache でメモ化されるが、View 自体 — 可視行の clip / overlay 合成 / 最外周フレーム
// (wrapWindowFrame → buildPanelBoxImpl) / 最終結合 — は毎フレーム走る。ここが素朴だと
// フレーム描画のたびに出力サイズ級のゴミを吐く (2026-07-25 の perf 監査で実測 56KB/frame)。
//
//	go test -run '^$' -bench BenchmarkView -benchmem .
//
// newTestBrowse は 80×10 + NoFrame:true でフレームを踏まないので、ここでは別に組む
// (最外周フレームが有効な既定の見た目を測りたい)。
func benchBrowse(tb testing.TB, n, w, h int) *browseModel {
	tb.Helper()
	commits := make([]Commit, n)
	raw := make([]string, 0, n*6)
	for i := range commits {
		sha := strings.Repeat(strconv.Itoa(i%10), 40)
		subject := "Fix invoice calculation for edge case " + strconv.Itoa(i)
		commits[i] = Commit{
			SHA: sha, ShortSHA: sha[:7], Subject: subject, Author: "koji",
			AuthorEmail: "k@example.com", Date: "Thu Jul 16 19:12:47 2026 +0900",
			RelDate: "3 hours ago", Message: subject,
		}
		// git log --color=always の medium 形式を模す (既定表示は verbatim 経路)
		raw = append(raw,
			"\x1b[33mcommit "+sha+"\x1b[m",
			"Author: koji <k@example.com>",
			"Date:   Thu Jul 16 19:12:47 2026 +0900",
			"",
			"    "+subject,
			"",
		)
	}
	statuses := make(map[string]CIState, n)
	for i, c := range commits {
		if i%2 == 0 {
			statuses[c.SHA] = StateSuccess
		} else {
			statuses[c.SHA] = StateFailure
		}
	}
	m := newBrowseModel(commits, statuses, nil, Repo{Owner: "o", Name: "r"}, true,
		&Options{}, true, w, h)
	tb.Cleanup(m.cancel)
	m.verbatim = VerbatimLines(raw, commits)
	if m.verbatim == nil {
		tb.Fatal("verbatim の構築に失敗 (ヘッダー照合がずれている)")
	}
	m.usageOv = usageOverlay{} // 起動時グランスは測定対象外 (dismiss 後の定常状態を測る)
	m.lines()                  // linesCache を温める = 毎フレーム走る分だけを測る
	return m
}

// 定常フレーム (スピナーが回っているだけ。リスト行は linesCache から)
func BenchmarkViewSteady(b *testing.B) {
	m := benchBrowse(b, 20, 120, 40)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

// job パネルを重ねた状態のフレーム (buildPanelBox がもう 1 段走る)
func BenchmarkViewWithPanel(b *testing.B) {
	m := benchBrowse(b, 20, 120, 40)
	m.panelSHA = m.commits[3].SHA
	m.details[m.panelSHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: StateFailure, URL: "https://github.com/o/r/runs/2"},
		{Name: "test", State: StatePending, URL: "https://github.com/o/r/runs/3"},
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}
