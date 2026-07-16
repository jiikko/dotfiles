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

func TestRenderLinesExpandedDetails(t *testing.T) {
	commits := testCommits()
	statuses := map[string]CIState{"a": StateFailure, "b": StateSuccess}
	o := RenderOpts{
		Expanded: map[string]bool{"a": true},
		Details: map[string][]CheckDetail{
			"a": {{Name: "build", State: StateSuccess}, {Name: "lint", State: StateFailure}},
		},
	}
	out := RenderStatic(commits, statuses, o)
	for _, want := range []string{"    ✓ build", "    ✗ lint"} {
		if !strings.Contains(out, want) {
			t.Errorf("展開行に %q がありません:\n%s", want, out)
		}
	}
	// 展開していない b のヘッダーの後に check 行が混ざらない
	if strings.Count(out, "✓ build") != 1 {
		t.Errorf("展開行が重複:\n%s", out)
	}
}

func TestRenderLinesExpandedStates(t *testing.T) {
	commits := testCommits()[:1]
	statuses := map[string]CIState{"a": StateSuccess}
	// 取得中
	loading := RenderStatic(commits, statuses, RenderOpts{
		Expanded: map[string]bool{"a": true}, DetailsLoading: map[string]bool{"a": true}, Spinner: "⠋",
	})
	if !strings.Contains(loading, "CI job を取得中") {
		t.Errorf("取得中表示がない:\n%s", loading)
	}
	// Check なし
	empty := RenderStatic(commits, statuses, RenderOpts{
		Expanded: map[string]bool{"a": true}, Details: map[string][]CheckDetail{"a": {}},
	})
	if !strings.Contains(empty, "Check はありません") {
		t.Errorf("Check なし表示がない:\n%s", empty)
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
