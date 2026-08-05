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
		_ = m.View().Content
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
		_ = m.View().Content
	}
}

// status viewer の 1 フレーム。⚠️ 一覧と別に測る理由: この画面だけ「毎フレーム・全行」で
// worktreeRow.dispPath() を通る (パスは git へ渡す生の値と表示用を分けているため、表示側は
// 呼ぶたびに無害化を通す)。一覧側の無害化は取り込み時に 1 回で済むので、ここが唯一
// 「無害化がフレーム予算に乗る」経路になる。
//
//	go test -run '^$' -bench BenchmarkStatusView -benchmem .
func benchStatusBrowse(tb testing.TB, files, w, h int) *browseModel {
	tb.Helper()
	m := benchBrowse(tb, 20, w, h)
	recs := make([]string, 0, files+1)
	recs = append(recs, "## master...origin/master [ahead 2]")
	for i := range files {
		// 実際に見る形に寄せる: 深いパス + 半分は staged / 半分は unstaged
		p := "src/glogx/internal/deeply/nested/module" + strconv.Itoa(i) + "/handler.go"
		if i%2 == 0 {
			recs = append(recs, "M  "+p)
		} else {
			recs = append(recs, " M "+p)
		}
	}
	var b strings.Builder
	for _, r := range recs {
		b.WriteString(r)
		b.WriteByte(0)
	}
	m.statusOv.shown = true
	m.statusOv.receive(statusLoadMsg{st: parseWorktreeStatus(b.String()), gen: m.statusOv.gen})
	return m
}

func BenchmarkStatusViewFrame(b *testing.B) {
	m := benchStatusBrowse(b, 40, 120, 40)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View().Content
	}
}

// 変更ファイルが多い repo (大きな merge / 大量の untracked) でのスケール確認。
// 可視は 40 行程度なので、ここが件数に比例するなら「見えない行のために毎フレーム働いている」。
func BenchmarkStatusViewFrame2000(b *testing.B) {
	m := benchStatusBrowse(b, 2000, 120, 40)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View().Content
	}
}
