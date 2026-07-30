package issues

import (
	"strings"
	"testing"
)

// sample は issue 本文で実際に使われている構文を一通り含むテスト用本文
// (実測: 見出し・箇条書き・番号付き・チェックボックス・引用・表・水平線・フェンスコード・
// インラインコード・強調・打ち消し・リンク・ソース側の手折り返し)。
const sample = "---\n" +
	"status: ongoing\n" +
	"---\n" +
	"# 028 refactor: box 引数の整理\n" +
	"\n" +
	"## 背景\n" +
	"\n" +
	"2026-07-25 のトースト改修で box.go を触った際に見つけた改善候補の記録。いずれも**単独では\n" +
	"動作に問題なし**で、trigger 待ちで凍結してよいもの ([`verify.md`](../rules/verify.md))。\n" +
	"\n" +
	"- `buildPanelBoxImpl` の引数 7 個\n" +
	"  - 入れ子の項目\n" +
	"- [ ] 未着手のタスク\n" +
	"- [x] 済みのタスク\n" +
	"1. 番号付きの項目\n" +
	"\n" +
	"> 引用行。~~取り消し~~ も混ざる\n" +
	"\n" +
	"| prefix | 用途 |\n" +
	"|---|---|\n" +
	"| feat | 新機能・機能拡張 |\n" +
	"| bug | 不具合修正 |\n" +
	"\n" +
	"---\n" +
	"\n" +
	"```go\n" +
	"func buildPanelBoxImpl(title string, rows []string, width int, colored bool) []string\n" +
	"```\n" +
	"\n" +
	"snake_case の識別子 no_provider_specific_branch は斜体にしない。\n"

func TestRenderBodyNeverExceedsWidth(t *testing.T) {
	for _, width := range []int{20, 40, 60, 86, 120} {
		for i, ln := range RenderBody(sample, width, false) {
			if w := dispWidth(ln); w > width {
				t.Fatalf("width=%d: 行 %d が幅を超えた (w=%d): %q", width, i, w, ln)
			}
		}
	}
}

func TestRenderBodyPlainHasNoANSI(t *testing.T) {
	for i, ln := range RenderBody(sample, 60, false) {
		if strings.Contains(ln, "\x1b") {
			t.Fatalf("colored=false なのに ANSI が出た (行 %d): %q", i, ln)
		}
	}
}

func TestRenderBodyColoredResetsEveryStyle(t *testing.T) {
	// コードブロック以外の装飾は必ず reset で閉じる (色が次の行へ漏れない)
	for i, ln := range RenderBody(sample, 60, true) {
		if strings.Contains(ln, "\x1b[1m") && !strings.Contains(ln, cReset) {
			t.Fatalf("装飾が閉じられていない (行 %d): %q", i, ln)
		}
	}
}

func TestFrontMatterIsNotRendered(t *testing.T) {
	body := strings.Join(RenderBody(sample, 60, false), "\n")
	if strings.Contains(body, "status: ongoing") {
		t.Fatalf("front matter が本文に出た:\n%s", body)
	}
	if !strings.Contains(body, "028 refactor") {
		t.Fatal("front matter の後の本文が落ちた")
	}
}

func TestParagraphReflowJoinsJapaneseWithoutSpace(t *testing.T) {
	// ソース側の手折り返しを畳んでから popup 幅で折り返す。日本語の連結に空白を入れない
	blocks := parseBlocks("日本語の行が途中で\n折り返されている")
	if len(blocks) != 1 || blocks[0].text != "日本語の行が途中で折り返されている" {
		t.Fatalf("日本語の reflow が想定と違う: %+v", blocks)
	}
}

func TestParagraphReflowJoinsASCIIWithSpace(t *testing.T) {
	blocks := parseBlocks("wrapped english\ntext here")
	if len(blocks) != 1 || blocks[0].text != "wrapped english text here" {
		t.Fatalf("英文の reflow が想定と違う: %+v", blocks)
	}
}

func TestHeadingLevelsGetDistinctMarkers(t *testing.T) {
	lines := RenderBody("# h1\n\n## h2\n\n### h3\n\n#### h4\n", 40, false)
	want := []string{"█ h1", "■ h2", "▸ h3", "· h4"}
	got := make([]string, 0, len(want))
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			got = append(got, ln)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("見出しの行数が想定と違う: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("見出し %d: want %q got %q", i, want[i], got[i])
		}
	}
}

func TestListHangingIndentAlignsUnderMarker(t *testing.T) {
	lines := RenderBody("- 日本語のとても長い項目のテキストが続く場合の折り返し\n", 20, false)
	if len(lines) < 2 {
		t.Fatalf("折り返されていない: %q", lines)
	}
	if !strings.HasPrefix(lines[0], "• ") {
		t.Fatalf("行頭記号が付いていない: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") || strings.HasPrefix(lines[1], "   ") {
		t.Fatalf("継続行が記号の下にぶら下がっていない: %q", lines[1])
	}
}

func TestListNestingAndCheckboxes(t *testing.T) {
	body := strings.Join(RenderBody(sample, 80, false), "\n")
	for _, want := range []string{"• buildPanelBoxImpl", "  ◦ 入れ子の項目", "☐ 未着手のタスク", "☑ 済みのタスク", "1. 番号付きの項目"} {
		if !strings.Contains(body, want) {
			t.Fatalf("箇条書きの整形に %q が出ない:\n%s", want, body)
		}
	}
}

func TestCodeBlockIsTruncatedNotWrapped(t *testing.T) {
	lines := RenderBody("```go\n"+strings.Repeat("x", 200)+"\n```\n", 30, false)
	code := make([]string, 0, 2)
	for _, ln := range lines {
		if strings.HasPrefix(ln, "┃ ") {
			code = append(code, ln)
		}
	}
	if len(code) != 1 {
		t.Fatalf("コード行が折り返された (1 行のはず): %q", code)
	}
	if !strings.HasSuffix(code[0], "…") {
		t.Fatalf("幅超過のコード行が切り詰められていない: %q", code[0])
	}
}

func TestUnterminatedFenceStillRenders(t *testing.T) {
	lines := RenderBody("```go\ncode line\n", 40, false)
	if len(lines) != 1 || !strings.Contains(lines[0], "code line") {
		t.Fatalf("閉じフェンス無しでコードが落ちた: %q", lines)
	}
}

func TestTableColumnsAlignAndFitWidth(t *testing.T) {
	const width = 40
	lines := RenderBody("| a | bbbb |\n|---|---|\n| cc | d |\n", width, false)
	rows := make([]string, 0, 3)
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			rows = append(rows, ln)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("表の行数が想定と違う: %q", rows)
	}
	if dispWidth(rows[0]) != dispWidth(rows[2]) {
		t.Fatalf("桁が揃っていない: %q / %q", rows[0], rows[2])
	}
	if !strings.Contains(rows[1], "─") || !strings.Contains(rows[1], "┼") {
		t.Fatalf("区切り行が罫線になっていない: %q", rows[1])
	}
	for _, r := range rows {
		if dispWidth(r) > width {
			t.Fatalf("表が幅を超えた: %q", r)
		}
	}
}

func TestTableShrinksWidestColumnWhenNarrow(t *testing.T) {
	const width = 24
	for _, ln := range RenderBody("| short | "+strings.Repeat("long", 20)+" |\n|---|---|\n", width, false) {
		if dispWidth(ln) > width {
			t.Fatalf("狭い幅で表が収まっていない (w=%d): %q", dispWidth(ln), ln)
		}
	}
}

func TestInlineStylesAreParsed(t *testing.T) {
	spans := parseInline("**強い** と `コード` と *斜体* と ~~打ち消し~~")
	want := map[style]string{styleStrong: "強い", styleCodeSpan: "コード", styleEm: "斜体", styleStrike: "打ち消し"}
	for st, text := range want {
		found := false
		for _, sp := range spans {
			if sp.Style == st && sp.Text == text {
				found = true
			}
		}
		if !found {
			t.Fatalf("style=%d の %q が解析されていない: %+v", st, text, spans)
		}
	}
}

func TestLinkShowsLabelOnlyNotURL(t *testing.T) {
	spans := parseInline("詳細は [`verify.md`](../_claude/rules/verify.md) を参照")
	for _, sp := range spans {
		if strings.Contains(sp.Text, "_claude/rules") {
			t.Fatalf("リンク先 URL が本文に出た: %+v", spans)
		}
	}
	found := false
	for _, sp := range spans {
		if strings.Contains(sp.Text, "verify.md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("リンクの表示テキストが落ちた: %+v", spans)
	}
}

func TestLinkWithEmptyLabelFallsBackToURL(t *testing.T) {
	spans := parseInline("[](https://example.com/x)")
	if len(spans) == 0 || !strings.Contains(spans[0].Text, "example.com") {
		t.Fatalf("label が空のとき URL を出していない: %+v", spans)
	}
}

func TestSnakeCaseIsNotItalic(t *testing.T) {
	const src = "no_provider_specific_branch は識別子"
	spans := parseInline(src)
	for _, sp := range spans {
		if sp.Style == styleEm {
			t.Fatalf("snake_case が斜体になった: %+v", spans)
		}
	}
	if len(spans) != 1 || spans[0].Text != src {
		t.Fatalf("本文が変わった: %+v", spans)
	}
}

func TestUnclosedInlineMarkerStaysLiteral(t *testing.T) {
	spans := parseInline("これは **閉じていない強調")
	if len(spans) != 1 || !strings.Contains(spans[0].Text, "**閉じていない強調") {
		t.Fatalf("閉じ記号なしの ** が文字として残っていない: %+v", spans)
	}
}

func TestBareURLIsLinkWithoutTrailingPunctuation(t *testing.T) {
	spans := parseInline("出典 https://example.com/a.html。次の文")
	for _, sp := range spans {
		if sp.Style == styleLink {
			if strings.HasSuffix(sp.Text, "。") {
				t.Fatalf("句読点が URL に含まれた: %q", sp.Text)
			}
			if sp.Text != "https://example.com/a.html" {
				t.Fatalf("URL の切り出しが想定と違う: %q", sp.Text)
			}
			return
		}
	}
	t.Fatalf("生 URL がリンクにならなかった: %+v", spans)
}

func TestEscapedMarkerIsLiteral(t *testing.T) {
	spans := parseInline(`\*not em\*`)
	if len(spans) != 1 || spans[0].Text != "*not em*" {
		t.Fatalf("エスケープが効いていない: %+v", spans)
	}
}

func TestRenderBodyDropsVS16(t *testing.T) {
	vs16 := string(rune(0xfe0f))
	body := strings.Join(RenderBody("注意 ⚠"+vs16+" あり\n", 40, false), "\n")
	if strings.Contains(body, vs16) {
		t.Fatalf("VS16 が表示テキストに残った: %q", body)
	}
}

func TestStaticGlyphsHaveNoVS16(t *testing.T) {
	// 自前の静的記号 (箇条書き・チェックボックス・罫線) に VS16 付き絵文字を混ぜない
	// (幅解釈が層ごとに割れる。glogx 本体の TestUsageHasNoVS16 と同じ趣旨)
	body := strings.Join(RenderBody(sample, 80, false), "\n")
	if strings.Contains(body, string(rune(0xfe0f))) {
		t.Fatalf("静的記号に VS16 が混ざっている: %q", body)
	}
}

func TestBlankLinesCollapse(t *testing.T) {
	lines := RenderBody("a\n\n\n\n\nb\n", 40, false)
	if len(lines) != 3 || lines[0] != "a" || lines[1] != "" || lines[2] != "b" {
		t.Fatalf("空行が畳まれていない: %q", lines)
	}
}

func TestHorizontalRuleFillsWidth(t *testing.T) {
	for _, ln := range RenderBody("a\n\n---\n\nb\n", 30, false) {
		if strings.HasPrefix(ln, "─") && dispWidth(ln) != 30 {
			t.Fatalf("水平線が幅一杯でない (w=%d): %q", dispWidth(ln), ln)
		}
	}
}

func TestQuoteIsPrefixedOnEveryLine(t *testing.T) {
	lines := RenderBody("> 引用のとても長い日本語の行が折り返される場合の見え方\n", 20, false)
	if len(lines) < 2 {
		t.Fatalf("引用が折り返されていない: %q", lines)
	}
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "┃ ") {
			t.Fatalf("引用の行 %d に縦線が無い: %q", i, ln)
		}
	}
}
