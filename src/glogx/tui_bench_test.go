package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"glogx/issues"
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
	return benchBrowseSubjects(tb, n, w, h, false)
}

// benchBrowseSubjects は benchBrowse の本体。ja=true で commit subject を日本語混在にする。
//
// ⚠️ 日本語を別に測る理由: 幅計算の fast-path (termwidth の fastDispWidth) は CJK を
// 受理せず ansi へ委ねるので、**日本語の subject を含む行はフレームの中で唯一 fast-path を
// 通れない行**になる。ASCII 固定のフィクスチャだけで測ると fast-path の効果を過大評価する
// (この repo 自身の commit message は日本語なので、実運用は ja=true 側に近い)。
// 2026-08-14 の敵対的レビュー R2 の指摘で追加。
func benchBrowseSubjects(tb testing.TB, n, w, h int, ja bool) *browseModel {
	tb.Helper()
	commits := make([]Commit, n)
	raw := make([]string, 0, n*6)
	for i := range commits {
		sha := strings.Repeat(strconv.Itoa(i%10), 40)
		subject := "Fix invoice calculation for edge case " + strconv.Itoa(i)
		if ja {
			// 表示幅を実物に寄せる: この repo の直近 300 commit の subject は 98.0% が CJK を
			// 含み、表示幅の中位は 75 セル。短い subject にすると fast-path を外す行の割合が
			// 小さくなり、効果を過大評価する (2026-08-14 の R3 レビューで実測差 4〜5%)
			subject = "fix(glogx): 請求計算の境界条件を是正し、回帰を実測で固定する (レビュー反映) " + strconv.Itoa(i)
		}
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

// benchPanelBrowse は job パネルを重ねた状態のモデル (ASCII/JA 対照の共有 fixture)。
func benchPanelBrowse(tb testing.TB, ja bool) *browseModel {
	tb.Helper()
	m := benchBrowseSubjects(tb, 20, 120, 40, ja)
	m.panelSHA = m.commits[3].SHA
	m.details[m.panelSHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: StateFailure, URL: "https://github.com/o/r/runs/2"},
		{Name: "test", State: StatePending, URL: "https://github.com/o/r/runs/3"},
	}
	return m
}

// job パネルを重ねた状態のフレーム (buildPanelBox がもう 1 段走る)
func BenchmarkViewWithPanel(b *testing.B) {
	m := benchPanelBrowse(b, false)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View().Content
	}
}

// BenchmarkViewWithPanel の日本語 subject 対照 (issue 055)。CI の予算には入れない
// (回帰検出は ASCII 側で足り、対照はローカルで「効果が内容に依存するか」を見る用)。
// 入れるなら budgets と bench_glogx.sh の両方を触ること。
func BenchmarkViewWithPanelJA(b *testing.B) {
	m := benchPanelBrowse(b, true)
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

// benchIssuesBrowse は issues viewer を開いた定常状態のモデル (issue 062)。
// viewLines が窓ごと差し替える全画面ビューは status と issues の 2 つで、issues 側は
// これまでどのベンチ・確保ゲートの視界にも入っていなかった。fixture は実ファイルを
// 一度だけ scan して組む (View 中はファイルを読まない。issue 052 のテストがその不変条件を守る)。
func benchIssuesBrowse(tb testing.TB, files, w, h int) *browseModel {
	tb.Helper()
	m := benchBrowse(tb, 20, w, h)
	root := tb.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		tb.Fatal(err)
	}
	for i := range files {
		name := fmt.Sprintf("%03d-feat-bench-issue.md", i+1)
		body := fmt.Sprintf("# %03d feat: bench issue fixture %d\n\nbody line\n", i+1, i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	found, warnings := issues.Scan([]string{dir})
	for _, iss := range found {
		_ = iss.LoadMeta()
	}
	m.handleKey("i")
	m.issuesOv.cwd = root
	m.Update(issuesScanMsg{root: root, dirs: []string{dir}, issues: found, warnings: warnings})
	m.issuesOv.finishAnim() // 開きドロワーの演出後 = 定常フレームを測る
	return m
}

func BenchmarkIssuesViewFrame(b *testing.B) {
	m := benchIssuesBrowse(b, 40, 120, 40)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View().Content
	}
}

// issue 件数が多い repo でのスケール確認 (BenchmarkStatusViewFrame2000 と同じ意図)。
// 可視は 40 行程度なので、ここが件数に比例するなら「見えない行のために毎フレーム働いている」。
func BenchmarkIssuesViewFrame2000(b *testing.B) {
	m := benchIssuesBrowse(b, 2000, 120, 40)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View().Content
	}
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

// 日本語の commit subject を含む一覧フレーム。BenchmarkViewSteady (ASCII 固定) と対で見る:
// 差が fast-path を通れない行のコストになる。CI の予算 (tests/glogx/bench_budgets.ci) には
// 入れていない (対照用のローカル指標。入れるなら budgets と bench_glogx.sh の両方を触ること)。
func BenchmarkViewSteadyJA(b *testing.B) {
	m := benchBrowseSubjects(b, 20, 120, 40, true)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View().Content
	}
}
