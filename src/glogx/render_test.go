package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func testCommits() []Commit {
	return []Commit{
		{SHA: "a", ShortSHA: "aaaaaaa", Subject: "Fix invoice calculation", Author: "koji", AuthorEmail: "koji@example.com",
			Date: "Thu Jul 16 19:12:47 2026 +0900", RelDate: "2 hours ago", Decoration: "HEAD -> master",
			Message: "Fix invoice calculation\n\ndetail line"},
		{SHA: "b", ShortSHA: "bbbbbbb", Subject: "Update README", Author: "koji", AuthorEmail: "koji@example.com",
			Date: "Wed Jul 15 10:00:00 2026 +0900", RelDate: "1 day ago", Message: "Update README"},
	}
}

func TestRenderStaticMediumFormat(t *testing.T) {
	// 既定は git log 標準 (medium) 形式に寄せる
	statuses := map[string]CIState{"a": StateSuccess, "b": StateFailure}
	out := RenderStatic(testCommits(), statuses, RenderOpts{})
	for _, want := range []string{
		"✓ commit a (HEAD -> master)",
		"Author: koji <koji@example.com>",
		"Date:   Thu Jul 16 19:12:47 2026 +0900",
		"    Fix invoice calculation",
		"    detail line",
		"✗ commit b",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("medium 形式に %q がありません:\n%s", want, out)
		}
	}
}

func TestRenderStaticOneline(t *testing.T) {
	statuses := map[string]CIState{"a": StateSuccess, "b": StateFailure}
	out := RenderStatic(testCommits(), statuses, RenderOpts{Oneline: true})
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("行数 = %d; want 2:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "✓ aaaaaaa") {
		t.Errorf("成功行 = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "✗ bbbbbbb") {
		t.Errorf("失敗行 = %q", lines[1])
	}
	if !strings.Contains(lines[0], "(HEAD -> master)") {
		t.Errorf("decoration がない: %q", lines[0])
	}
}

func TestRenderLinesPushBoundary(t *testing.T) {
	// 未 push と push 済みの間に ── origin ── の境界線が入る (どこまで push したかの視覚化)
	statuses := map[string]CIState{"a": StateUnpushed, "b": StateSuccess}
	lines := RenderLines(testCommits(), statuses, RenderOpts{})
	ruleIdx, headerB := -1, -1
	for i, l := range lines {
		if strings.Contains(l.Text, " origin ") && strings.Contains(l.Text, "──") {
			ruleIdx = i
		}
		if l.Header && l.CommitIdx == 1 {
			headerB = i
		}
	}
	if ruleIdx == -1 {
		t.Fatalf("境界線が入っていない:\n%s", RenderStatic(testCommits(), statuses, RenderOpts{}))
	}
	if headerB == -1 || ruleIdx != headerB-1 {
		t.Errorf("境界線の位置 = %d; want push 済み先頭ヘッダー (%d) の直前", ruleIdx, headerB)
	}
	// oneline でも入る
	if out := RenderStatic(testCommits(), statuses, RenderOpts{Oneline: true}); !strings.Contains(out, " origin ") {
		t.Errorf("oneline で境界線が入らない:\n%s", out)
	}
	// 全部未 push / repo 不明では入らない
	for name, st := range map[string]map[string]CIState{
		"全部未 push":       {"a": StateUnpushed, "b": StateUnpushed},
		"repo 不明 (全部済み)": {"a": StateSuccess, "b": StateSuccess},
	} {
		if out := RenderStatic(testCommits(), st, RenderOpts{}); strings.Contains(out, " origin ") {
			t.Errorf("%s で境界線が入った:\n%s", name, out)
		}
	}
	// 全部 push 済み (HasRepo) なら先頭に all pushed マークが入る (ユーザー要望)
	all := map[string]CIState{"a": StateSuccess, "b": StateSuccess}
	out := RenderLines(testCommits(), all, RenderOpts{HasRepo: true})
	if len(out) == 0 || !strings.Contains(stripANSI(out[0].Text), "origin (all pushed ✓)") {
		t.Fatalf("全部 push 済みの先頭マークが無い: %q", out[0].Text)
	}
	// 混在時は先頭マークではなく中間の境界線 (二重に出ない)
	mixed := RenderLines(testCommits(), statuses, RenderOpts{HasRepo: true})
	if strings.Contains(stripANSI(mixed[0].Text), "all pushed") {
		t.Fatal("混在時に先頭へ all pushed マークが出た")
	}
	// 全部未 push は HasRepo でも何も出ない
	un := map[string]CIState{"a": StateUnpushed, "b": StateUnpushed}
	if out := RenderStatic(testCommits(), un, RenderOpts{HasRepo: true}); strings.Contains(out, " origin ") {
		t.Fatalf("全部未 push で境界線が入った:\n%s", out)
	}
}

func TestRenderLinesLoadingSpinner(t *testing.T) {
	// statuses に無い SHA は取得中としてスピナーを出す
	out := RenderStatic(testCommits(), map[string]CIState{}, RenderOpts{Oneline: true, Spinner: "⠋"})
	if !strings.HasPrefix(out, "⠋ aaaaaaa") {
		t.Errorf("スピナー行 = %q", out)
	}
}

func TestRenderStaticNoANSIWhenUncolored(t *testing.T) {
	// 非 TTY (Colored=false) では ANSI を一切出さない (issue の完了条件)
	statuses := map[string]CIState{"a": StateSuccess, "b": StatePending}
	for _, oneline := range []bool{false, true} {
		out := RenderStatic(testCommits(), statuses, RenderOpts{Oneline: oneline})
		if strings.Contains(out, "\x1b") {
			t.Errorf("Colored=false (oneline=%v) で ANSI が混入: %q", oneline, out)
		}
	}
}

func TestRenderStaticColored(t *testing.T) {
	statuses := map[string]CIState{"a": StateSuccess, "b": StateFailure}
	out := RenderStatic(testCommits(), statuses, RenderOpts{Colored: true})
	if !strings.Contains(out, ansiGreen+"✓"+ansiReset) {
		t.Errorf("成功記号に色がない: %q", out)
	}
}

func TestRenderStaticWithDiffBody(t *testing.T) {
	commits := testCommits()
	commits[0].Body = " file.go | 2 +-\n 1 file changed"
	statuses := map[string]CIState{"a": StateSuccess, "b": StateSuccess}
	out := RenderStatic(commits, statuses, RenderOpts{})
	// CI 記号はヘッダー行にだけ付く (issue の設計)
	var glyphLines int
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "✓") {
			glyphLines++
		}
	}
	if glyphLines != 2 {
		t.Errorf("記号付き行 = %d; want 2 (ヘッダーのみ):\n%s", glyphLines, out)
	}
	if !strings.Contains(out, " file.go | 2 +-") {
		t.Errorf("本文が保持されていない:\n%s", out)
	}
}

func TestRenderLinesHeaderMapping(t *testing.T) {
	// TUI のカーソル位置決めに使う Header/CommitIdx が正しく付く
	lines := RenderLines(testCommits(), map[string]CIState{"a": StateSuccess, "b": StateSuccess}, RenderOpts{})
	var headers []int
	for i, l := range lines {
		if l.Header {
			headers = append(headers, i)
			if !strings.Contains(l.Text, "commit") {
				t.Errorf("ヘッダー行が commit 行でない: %q", l.Text)
			}
		}
	}
	if len(headers) != 2 {
		t.Fatalf("ヘッダー行数 = %d; want 2", len(headers))
	}
	if lines[headers[0]].CommitIdx != 0 || lines[headers[1]].CommitIdx != 1 {
		t.Errorf("CommitIdx の対応が不正: %+v", headers)
	}
}

func TestPRBadge(t *testing.T) {
	// コミット行末尾の PR バッジ (行末配置なので oneline の列揃えを崩さない)
	prs := map[string]*PRRef{
		"a": {Number: 12, State: "OPEN"},
		"b": nil, // 確認済みで PR なし
	}
	// medium: ヘッダー行に #12
	lines := RenderLines(testCommits(), map[string]CIState{"a": StateSuccess, "b": StateSuccess},
		RenderOpts{PRs: prs})
	if !strings.Contains(lines[0].Text, "#12") {
		t.Errorf("medium ヘッダーに PR バッジがない: %q", lines[0].Text)
	}
	// PR なしコミットには出ない
	for _, l := range lines {
		if l.Header && l.CommitIdx == 1 && strings.Contains(l.Text, "#") {
			t.Errorf("PR なしコミットにバッジが出た: %q", l.Text)
		}
	}
	// oneline: 行末に #12
	one := RenderLines(testCommits(), map[string]CIState{"a": StateSuccess, "b": StateSuccess},
		RenderOpts{Oneline: true, PRs: prs})
	if !strings.HasSuffix(one[0].Text, "#12") {
		t.Errorf("oneline 行末に PR バッジがない: %q", one[0].Text)
	}
	// 色: OPEN=緑 / MERGED=マゼンタ
	colored := prBadge("a", RenderOpts{Colored: true, PRs: prs})
	if !strings.Contains(colored, ansiGreen) {
		t.Errorf("OPEN が緑でない: %q", colored)
	}
	merged := prBadge("m", RenderOpts{Colored: true, PRs: map[string]*PRRef{"m": {Number: 9, State: "MERGED"}}})
	if !strings.Contains(merged, ansiMagenta) {
		t.Errorf("MERGED がマゼンタでない: %q", merged)
	}
}

func TestRenderDecorationGitStyle(t *testing.T) {
	// git log の配色を尊重: HEAD=cyan / ローカル=green / remote=red / tag=yellow
	o := RenderOpts{Colored: true}
	dc := DefaultDecorColors()
	out := renderDecoration("HEAD -> master, origin/master, origin/HEAD, tag: v1.0, feature/x", o)
	for _, want := range []string{
		dc.HEAD + "HEAD" + ansiReset,
		dc.Branch + "master" + ansiReset,
		dc.RemoteBranch + "origin/master" + ansiReset,
		dc.RemoteBranch + "origin/HEAD" + ansiReset,
		dc.Tag + "tag: v1.0" + ansiReset,
		// "/" を含むだけのローカルブランチは remote 扱いにしない
		dc.Branch + "feature/x" + ansiReset,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("decoration に %q がありません:\n%q", want, out)
		}
	}
	// 非カラーでは素の文字列
	if got := renderDecoration("HEAD -> master", RenderOpts{}); got != "(HEAD -> master)" {
		t.Errorf("非カラー = %q", got)
	}
}

func TestRenderDecorationCustomColors(t *testing.T) {
	// git config color.decorate.* の上書き (RenderOpts.Decor 経由) が効く
	dc := DecorColors{HEAD: "[H]", Branch: "[B]", RemoteBranch: "[R]", Tag: "[T]", Remotes: []string{"upstream"}}
	o := RenderOpts{Colored: true, Decor: &dc}
	out := renderDecoration("upstream/main, origin/main", o)
	if !strings.Contains(out, "[R]upstream/main") {
		t.Errorf("remote 判定が Remotes リストを見ていない: %q", out)
	}
	// origin は Remotes に無いのでローカル扱い
	if !strings.Contains(out, "[B]origin/main") {
		t.Errorf("未登録 remote 名がローカル扱いになっていない: %q", out)
	}
}

func TestRenderLinesWrapsMessage(t *testing.T) {
	// Width > 0 (TUI) ではメッセージ行を切り詰めず端末幅で折り返す (git log と同じ見え方)
	commits := testCommits()[:1]
	commits[0].Message = strings.Repeat("あ", 50) // 表示幅 100
	lines := RenderLines(commits, map[string]CIState{"a": StateSuccess}, RenderOpts{Width: 44})
	var msgLines []string
	for _, l := range lines {
		if strings.HasPrefix(l.Text, "    ") {
			msgLines = append(msgLines, l.Text)
		}
	}
	if len(msgLines) < 3 {
		t.Fatalf("折り返しで複数行になるはずが %d 行: %v", len(msgLines), msgLines)
	}
	joined := strings.Join(msgLines, "")
	if strings.Count(joined, "あ") != 50 {
		t.Errorf("折り返しで文字が失われた: %q", joined)
	}
	for _, l := range msgLines {
		if w := runewidth.StringWidth(l); w > 44 {
			t.Errorf("折り返し後も幅超過 (%d): %q", w, l)
		}
	}
	// Width=0 (静的出力) は折り返さない (端末/パイプに任せる = git log と同じ)
	static := RenderLines(commits, map[string]CIState{"a": StateSuccess}, RenderOpts{})
	var staticMsg int
	for _, l := range static {
		if strings.HasPrefix(l.Text, "    あ") {
			staticMsg++
		}
	}
	if staticMsg != 1 {
		t.Errorf("静的出力でメッセージが折り返されている: %d 行", staticMsg)
	}
}

func TestRenderLinesExpandsBodyTabsInTUI(t *testing.T) {
	// TUI (Width > 0) では diff 本文のタブを展開する (幅計算と端末のタブ展開の食い違いで
	// 表示が崩壊するのを防ぐ)。静的出力 (Width=0) は git log と同じ素通し
	commits := testCommits()[:1]
	commits[0].Body = "+\tfunc main() {\n+\t\treturn\n"
	tui := RenderLines(commits, map[string]CIState{"a": StateSuccess}, RenderOpts{Width: 80})
	for _, l := range tui {
		if strings.Contains(l.Text, "\t") {
			t.Errorf("TUI 描画にタブが残っている: %q", l.Text)
		}
	}
	static := RenderLines(commits, map[string]CIState{"a": StateSuccess}, RenderOpts{})
	found := false
	for _, l := range static {
		if strings.Contains(l.Text, "\t") {
			found = true
		}
	}
	if !found {
		t.Errorf("静的出力でタブが変更されている (git log とのパリティが崩れる)")
	}
}

// Body と対称: mediumLines fallback (Verbatim==nil) でも TUI (Width>0) はコミットメッセージ内の
// タブを展開する。展開しないと clipToWidth が \t を幅0と数え端末が展開して再描画が崩れる
// (Body/decorateVerbatim は展開するのに Message だけ取りこぼしていた片側バグの回帰ガード)。
func TestRenderLinesExpandsMessageTabsInTUI(t *testing.T) {
	commits := testCommits()[:1]
	commits[0].Message = "subject\n\n\tindented body of the message"
	commits[0].Body = "" // Verbatim==nil の mediumLines 経路でメッセージ行を通す
	tui := RenderLines(commits, map[string]CIState{"a": StateSuccess}, RenderOpts{Width: 80})
	for _, l := range tui {
		if strings.Contains(l.Text, "\t") {
			t.Errorf("TUI 描画でメッセージ行のタブが残っている: %q", l.Text)
		}
	}
	// 静的出力 (Width=0) は git log と同じくタブ素通し
	static := RenderLines(commits, map[string]CIState{"a": StateSuccess}, RenderOpts{})
	foundTab := false
	for _, l := range static {
		if strings.Contains(l.Text, "\t") {
			foundTab = true
		}
	}
	if !foundTab {
		t.Error("静的出力でメッセージのタブが変更されている (git log パリティ崩れ)")
	}
}

func TestJapaneseOnelineAlignment(t *testing.T) {
	// 全角 (幅 2) の subject/author が混ざっても列が揃う (幅計算が rune 数でなく
	// 表示幅ベースであることの検証)
	commits := []Commit{
		{SHA: "a", ShortSHA: "aaaaaaa", Subject: "日本語のコミットメッセージ", Author: "川口", RelDate: "2 hours ago"},
		{SHA: "b", ShortSHA: "bbbbbbb", Subject: "ascii subject", Author: "koji", RelDate: "1 day ago"},
	}
	lines := RenderLines(commits, map[string]CIState{"a": StateSuccess, "b": StateFailure}, RenderOpts{Oneline: true})
	datePos := func(text, marker string) int {
		idx := strings.Index(text, marker)
		if idx < 0 {
			t.Fatalf("%q に %q が無い", text, marker)
		}
		return runewidth.StringWidth(text[:idx])
	}
	p1 := datePos(lines[0].Text, "2 hours ago")
	p2 := datePos(lines[1].Text, "1 day ago")
	if p1 != p2 {
		t.Errorf("日時列の開始桁が揃っていない: %d vs %d\n%q\n%q", p1, p2, lines[0].Text, lines[1].Text)
	}
}

func TestJapaneseSubjectTruncation(t *testing.T) {
	// 全角 subject の切り詰めが cap を超えない (全角境界で 1 桁はみ出さない)
	commits := []Commit{{SHA: "a", ShortSHA: "aaaaaaa", Subject: strings.Repeat("長", 50), Author: "k", RelDate: "now"}}
	lines := RenderLines(commits, map[string]CIState{"a": StateSuccess}, RenderOpts{Oneline: true})
	if !strings.Contains(lines[0].Text, "…") {
		t.Fatalf("切り詰めが起きていない: %q", lines[0].Text)
	}
	start := strings.Index(lines[0].Text, "長")
	end := strings.Index(lines[0].Text, "…") + len("…")
	if w := runewidth.StringWidth(lines[0].Text[start:end]); w > subjectWidthCap {
		t.Errorf("subject 列の幅 = %d > cap %d", w, subjectWidthCap)
	}
}

func TestWrapToWidth(t *testing.T) {
	if got := wrapToWidth("short", 10); len(got) != 1 || got[0] != "short" {
		t.Errorf("幅内の行が変更された: %v", got)
	}
	if got := wrapToWidth("", 10); len(got) != 1 || got[0] != "" {
		t.Errorf("空行 = %v", got)
	}
	got := wrapToWidth("abcdefghij", 4)
	if len(got) != 3 || got[0] != "abcd" || got[2] != "ij" {
		t.Errorf("ASCII 折り返し = %v", got)
	}
	// 全角は 2 幅で数える (幅 5 に「ああ」(4) + 次の「あ」は入らない)
	got = wrapToWidth("あああ", 5)
	if len(got) != 2 || got[0] != "ああ" {
		t.Errorf("全角折り返し = %v", got)
	}
	// 半角/全角混在でも各行が幅内に収まり、文字が失われない
	mixed := "aあbいcうdえeお"
	segs := wrapToWidth(mixed, 5)
	if strings.Join(segs, "") != mixed {
		t.Errorf("混在折り返しで文字が失われた: %v", segs)
	}
	for _, seg := range segs {
		if w := runewidth.StringWidth(seg); w > 5 {
			t.Errorf("混在折り返しの幅超過 (%d): %q", w, seg)
		}
	}
}

func TestDecorateDetailLine(t *testing.T) {
	// Web UI 風のマーカー着色 (raw ログには色情報が無いので glog 側で再現)
	tests := []struct {
		line string
		want string
	}{
		{"##[error]Process completed with exit code 1.", ansiRed},
		{"[failure] src/a.go:10", ansiRed},
		{"##[warning]deprecated", ansiYellow},
		{"##[group]Run make test", ansiDim},
		{"##[endgroup]", ansiDim},
	}
	for _, tt := range tests {
		got := decorateDetailLine(tt.line, true)
		if !strings.HasPrefix(got, tt.want) || !strings.HasSuffix(got, ansiReset) {
			t.Errorf("decorate(%q) = %q; want %s で着色", tt.line, got, tt.want)
		}
	}
	// マーカー無しの行と非カラーはそのまま
	if got := decorateDetailLine("plain log line", true); got != "plain log line" {
		t.Errorf("素の行が着色された: %q", got)
	}
	if got := decorateDetailLine("##[error]x", false); got != "##[error]x" {
		t.Errorf("非カラーで着色された: %q", got)
	}
}

func TestClipToWidth(t *testing.T) {
	long := strings.Repeat("x", 100)
	if got := clipToWidth(long, 40); len([]rune(got)) > 40 {
		t.Errorf("幅超過: %q", got)
	}
	colored := ansiGreen + "short" + ansiReset
	if got := clipToWidth(colored, 40); got != colored {
		t.Errorf("幅内の色付き行が変更された: %q", got)
	}
	// fast-path: ANSI 無し・幅内は無改変で素通し
	if got := clipToWidth("plain ascii", 40); got != "plain ascii" {
		t.Errorf("ANSI 無し幅内が変更された: %q", got)
	}
	// fast-path の byte 長ヒューリスティックが全角を誤って素通ししない
	// (あ×30 = 表示幅 60 > 40。byte 長 90 も 40 超なので fast-path を通らず truncate される)
	wide := strings.Repeat("あ", 30)
	if got := clipToWidth(wide, 40); runewidth.StringWidth(got) > 40 {
		t.Errorf("全角行が幅超過のまま素通しされた: 幅 %d", runewidth.StringWidth(got))
	}
}

// dropToColumn は左 N 桁を捨て、cut 前の SGR を replay して右側を復元する
// (overlayCenteredBox の右背景合成に使う。truncateKeepANSI の鏡像)。
func TestDropToColumn(t *testing.T) {
	// 素の ASCII: 先頭 N 桁を落とす
	if got := dropToColumn("abcdef", 2); got != "cdef" {
		t.Errorf(`dropToColumn("abcdef",2)=%q; want "cdef"`, got)
	}
	// n<=0 は素通し
	if got := dropToColumn("abc", 0); got != "abc" {
		t.Errorf(`dropToColumn("abc",0)=%q; want "abc"`, got)
	}
	// n が内容末尾以降なら空
	if got := dropToColumn("abc", 5); got != "" {
		t.Errorf(`dropToColumn("abc",5)=%q; want ""`, got)
	}
	// ANSI: cut より前の色コードを replay し、残り suffix は色を保つ
	got := dropToColumn(ansiGreen+"abcdef"+ansiReset, 2)
	if !strings.HasPrefix(got, ansiGreen) {
		t.Errorf("cut 前の色が replay されていない: %q", got)
	}
	if stripANSI(got) != "cdef" {
		t.Errorf("suffix の内容がずれた: %q (plain=%q)", got, stripANSI(got))
	}
	// 全角グリフが cut をまたぐ場合: そのグリフを落とし空白で列 n に揃える
	// "あい" は各幅 2。n=1 は 'あ'(列0-1) をまたぐ → 'あ' を落とし 1 空白 + 'い'
	if got := dropToColumn("あい", 1); got != " い" {
		t.Errorf(`dropToColumn("あい",1)=%q; want " い"`, got)
	}
}

func TestRenderCached(t *testing.T) {
	head := &Commit{SHA: "a", ShortSHA: "aaaaaaa", Subject: "Fix invoice calculation"}
	out := RenderCached(head, StateSuccess, " 3 files changed", false, "")
	if !strings.HasPrefix(out, "HEAD CI: ✓ aaaaaaa Fix invoice calculation") {
		t.Errorf("ヘッダー = %q", out)
	}
	if !strings.Contains(out, "Staged changes:\n 3 files changed") {
		t.Errorf("staged diff がない:\n%s", out)
	}
	empty := RenderCached(head, StateNone, "", false, "")
	if !strings.Contains(empty, "staged な変更はありません") {
		t.Errorf("空 diff の表示 = %q", empty)
	}
}

// BenchmarkRenderLinesLargePatch は -p の巨大出力での行構築コストの観測用
// (browseModel.lines() のメモ化が効く前提の 1 回分のコスト)。
func BenchmarkRenderLinesLargePatch(b *testing.B) {
	commits := make([]Commit, 20)
	patch := strings.Repeat("+added line of a reasonably long diff body text\n", 500)
	for i := range commits {
		commits[i] = Commit{
			SHA: strings.Repeat("a", 39) + string(rune('a'+i)), ShortSHA: "aaaaaaa",
			Subject: "subject", Author: "koji", AuthorEmail: "k@x", Date: "d", RelDate: "now",
			Message: "subject", Body: patch,
		}
	}
	statuses := map[string]CIState{}
	for _, c := range commits {
		statuses[c.SHA] = StateSuccess
	}
	for b.Loop() {
		RenderLines(commits, statuses, RenderOpts{Width: 120})
	}
}

func TestStatusGlyphAllStates(t *testing.T) {
	want := map[CIState]string{
		StateSuccess: "✓", StateFailure: "✗", StatePending: "●",
		StateNeutral: "⊘", StateNone: "–", StateUnknown: "?", StateUnpushed: "↑",
	}
	for state, glyph := range want {
		if got := StatusGlyph(state, false, ""); got != glyph {
			t.Errorf("StatusGlyph(%s) = %q; want %q", state, got, glyph)
		}
	}
	if got := StatusGlyph(StateLoading, false, "⠙"); got != "⠙" {
		t.Errorf("loading = %q; want spinner", got)
	}
}

// --- verbatim 方式 (git log 実出力の取り込み) ---

func verbatimFixture() ([]string, []Commit) {
	commits := []Commit{
		{SHA: strings.Repeat("a", 40), ShortSHA: "aaaaaaa", Subject: "first"},
		{SHA: strings.Repeat("b", 40), ShortSHA: "bbbbbbb", Subject: "second"},
	}
	raw := []string{
		"\x1b[33mcommit " + commits[0].SHA + "\x1b[m (HEAD -> master)",
		"Author: koji <k@x>",
		"Date:   Sat Jul 19 00:00:00 2026 +0900",
		"",
		"    first",
		"    commit " + commits[1].SHA + " を参照する行", // メッセージ内の言及 (インデント 4) は誤検出しない
		"",
		"commit " + commits[1].SHA,
		"Author: koji <k@x>",
		"Date:   Sat Jul 19 00:00:00 2026 +0900",
		"",
		"    second",
	}
	return raw, commits
}

func TestVerbatimLinesClassifiesHeaders(t *testing.T) {
	raw, commits := verbatimFixture()
	lines := VerbatimLines(raw, commits)
	if lines == nil {
		t.Fatal("照合に失敗した (nil)")
	}
	var headers []int
	for i, l := range lines {
		if l.Header {
			headers = append(headers, i)
		}
	}
	if len(headers) != 2 || headers[0] != 0 || headers[1] != 7 {
		t.Fatalf("ヘッダー位置 = %v; want [0 7]", headers)
	}
	if lines[4].CommitIdx != 0 || lines[11].CommitIdx != 1 {
		t.Errorf("CommitIdx の帰属が誤り: %d %d", lines[4].CommitIdx, lines[11].CommitIdx)
	}
	// 本文は git 出力そのまま (再構築しない)
	if lines[1].Text != raw[1] {
		t.Errorf("本文行が変更された: %q", lines[1].Text)
	}
}

func TestVerbatimLinesMismatchFallsBack(t *testing.T) {
	raw, commits := verbatimFixture()
	if VerbatimLines(raw[:3], commits) != nil {
		t.Error("ヘッダー数不一致で nil にならない (fallback が働かない)")
	}
}

func TestRenderLinesVerbatimDecoratesHeaderOnly(t *testing.T) {
	raw, commits := verbatimFixture()
	v := VerbatimLines(raw, commits)
	statuses := map[string]CIState{commits[0].SHA: StateSuccess, commits[1].SHA: StateFailure}
	o := RenderOpts{Colored: true, Width: 80, Verbatim: v,
		PRs: map[string]*PRRef{commits[0].SHA: {Number: 7, State: "OPEN"}}}
	lines := RenderLines(commits, statuses, o)
	if !strings.HasPrefix(lines[0].Text, ansiGreen+"✓"+ansiReset+" ") {
		t.Errorf("ヘッダーに CI 記号が前置されていない: %q", lines[0].Text)
	}
	if !strings.Contains(lines[0].Text, "#7") {
		t.Errorf("PR バッジが付いていない: %q", lines[0].Text)
	}
	if !strings.Contains(lines[0].Text, raw[0]) {
		t.Errorf("ヘッダーの git 出力部分が改変された: %q", lines[0].Text)
	}
	if lines[1].Text != raw[1] {
		t.Errorf("Author 行が git log 出力と一致しない (verbatim 契約違反): %q", lines[1].Text)
	}
	// 色なし長行は幅で折り返し、タブは展開される
	raw2 := append(append([]string{}, raw...), "")
	raw2[4] = "    " + strings.Repeat("あ", 60)
	v2 := VerbatimLines(raw2, commits)
	lines2 := RenderLines(commits, statuses, RenderOpts{Width: 40, Verbatim: v2})
	count := 0
	for _, l := range lines2 {
		count += strings.Count(l.Text, "あ")
	}
	if count != 60 {
		t.Errorf("折り返しで文字が欠けた: あ %d 文字 (want 60)", count)
	}
}
