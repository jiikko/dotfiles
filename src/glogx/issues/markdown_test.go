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

// renderLines は RenderBody の行だけを取るテスト用ヘルパー (行番号は別テストで見る)。
func renderLines(src string, width int, colored bool) []string {
	lines, _ := RenderBody(src, width, colored)
	return lines
}

func TestRenderBodyNeverExceedsWidth(t *testing.T) {
	for _, width := range []int{20, 40, 60, 86, 120} {
		for i, ln := range renderLines(sample, width, false) {
			if w := dispWidth(ln); w > width {
				t.Fatalf("width=%d: 行 %d が幅を超えた (w=%d): %q", width, i, w, ln)
			}
		}
	}
}

func TestRenderBodyPlainHasNoANSI(t *testing.T) {
	for i, ln := range renderLines(sample, 60, false) {
		if strings.Contains(ln, "\x1b") {
			t.Fatalf("colored=false なのに ANSI が出た (行 %d): %q", i, ln)
		}
	}
}

func TestRenderBodyColoredResetsEveryStyle(t *testing.T) {
	// コードブロック以外の装飾は必ず reset で閉じる (色が次の行へ漏れない)
	for i, ln := range renderLines(sample, 60, true) {
		if strings.Contains(ln, "\x1b[1m") && !strings.Contains(ln, cReset) {
			t.Fatalf("装飾が閉じられていない (行 %d): %q", i, ln)
		}
	}
}

func TestFrontMatterIsNotRendered(t *testing.T) {
	body := strings.Join(renderLines(sample, 60, false), "\n")
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

func TestParagraphReflowSpacesJapaneseLatinBoundary(t *testing.T) {
	// 日本語とラテン語の境界では空白を入れる (この repo の文章の書き方に合わせる)
	cases := map[string]string{
		"popup は\npane ではなく": "popup は pane ではなく",
		"これは tmux\n非依存で":     "これは tmux 非依存で",
		"日本語の行が途中で\n折り返される":  "日本語の行が途中で折り返される",  // 日本語どうしは詰める
		"しまう。\ntmux 3.7 で検証": "しまう。tmux 3.7 で検証", // 和文の句読点の後は詰める
		"選択コピー\nすることは":       "選択コピーすることは",       // 長音記号 ー の後も詰める
		"中黒・\nnext":          "中黒・next",
	}
	for src, want := range cases {
		blocks := parseBlocks(src)
		if len(blocks) != 1 || blocks[0].text != want {
			t.Fatalf("%q の reflow: want %q got %+v", src, want, blocks)
		}
	}
}

func TestHeadingLevelsGetDistinctMarkers(t *testing.T) {
	lines := renderLines("# h1\n\n## h2\n\n### h3\n\n#### h4\n", 40, false)
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
	lines := renderLines("- 日本語のとても長い項目のテキストが続く場合の折り返し\n", 20, false)
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
	body := strings.Join(renderLines(sample, 80, false), "\n")
	for _, want := range []string{"• buildPanelBoxImpl", "  ◦ 入れ子の項目", "☐ 未着手のタスク", "☑ 済みのタスク", "1. 番号付きの項目"} {
		if !strings.Contains(body, want) {
			t.Fatalf("箇条書きの整形に %q が出ない:\n%s", want, body)
		}
	}
}

func TestCodeBlockIsTruncatedNotWrapped(t *testing.T) {
	lines := renderLines("```go\n"+strings.Repeat("x", 200)+"\n```\n", 30, false)
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
	lines := renderLines("```go\ncode line\n", 40, false)
	if len(lines) != 1 || !strings.Contains(lines[0], "code line") {
		t.Fatalf("閉じフェンス無しでコードが落ちた: %q", lines)
	}
}

func TestIndentedFenceInsideListIsCode(t *testing.T) {
	// ⚠️ 回帰防止: 箇条書きの中のコードブロックは 4 桁以上インデントされる。フェンスを 0-3 桁に
	// 縛ると、中身が項目の継続行として散文に連結され (reflowJoin) コードが壊れて出る。
	src := "1. 設定内容:\n" +
		"     ```yaml\n" +
		"     Token name: updater\n" +
		"     Repository access: Selected\n" +
		"       - jiikko/example\n" +
		"     ```\n" +
		"\n" +
		"次の段落。\n"
	lines := renderLines(src, 60, false)
	code := make([]string, 0, 3)
	for _, ln := range lines {
		if strings.HasPrefix(ln, "┃ ") {
			code = append(code, ln)
		}
	}
	if len(code) != 3 {
		t.Fatalf("インデントされたフェンスがコードになっていない: %q", lines)
	}
	// 開きフェンスと同じ深さは落とし、相対インデントは保つ
	if got := code[0]; got != "┃ Token name: updater" {
		t.Fatalf("コード行が dedent されていない: %q", got)
	}
	if got := code[2]; got != "┃   - jiikko/example" {
		t.Fatalf("相対インデントが失われた: %q", got)
	}
	// フェンス記号そのものは本文に出ない
	if joined := strings.Join(lines, "\n"); strings.Contains(joined, "```") {
		t.Fatalf("フェンス記号が本文に出た:\n%s", joined)
	}
}

func TestTableSeparatorIsPositionalNotShape(t *testing.T) {
	// 全セルがハイフンのデータ行 (空欄のプレースホルダ) を区切り行と誤判定すると、
	// renderTable が内容を罫線へ置き換えて黙って消す。
	lines := renderLines("| a | b |\n|--|--|\n| - | - |\n| x | y |\n", 40, false)
	rows := make([]string, 0, 4)
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			rows = append(rows, ln)
		}
	}
	if len(rows) != 4 {
		t.Fatalf("表の行数が想定と違う: %q", rows)
	}
	// 実データに存在する短い区切り行 (|--|--|) はヘッダー直下なので罫線になる
	if !strings.Contains(rows[1], "┼") {
		t.Fatalf("ヘッダー直下の短い区切り行が罫線になっていない: %q", rows[1])
	}
	if !strings.Contains(rows[2], "-") || strings.Contains(rows[2], "┼") {
		t.Fatalf("データ行が罫線に置き換わって消えた: %q", rows[2])
	}
}

func TestTableColumnsAlignAndFitWidth(t *testing.T) {
	const width = 40
	lines := renderLines("| a | bbbb |\n|---|---|\n| cc | d |\n", width, false)
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
	for _, ln := range renderLines("| short | "+strings.Repeat("long", 20)+" |\n|---|---|\n", width, false) {
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
	body := strings.Join(renderLines("注意 ⚠"+vs16+" あり\n", 40, false), "\n")
	if strings.Contains(body, vs16) {
		t.Fatalf("VS16 が表示テキストに残った: %q", body)
	}
}

func TestStaticGlyphsHaveNoVS16(t *testing.T) {
	// 自前の静的記号 (箇条書き・チェックボックス・罫線) に VS16 付き絵文字を混ぜない
	// (幅解釈が層ごとに割れる。glogx 本体の TestUsageHasNoVS16 と同じ趣旨)
	body := strings.Join(renderLines(sample, 80, false), "\n")
	if strings.Contains(body, string(rune(0xfe0f))) {
		t.Fatalf("静的記号に VS16 が混ざっている: %q", body)
	}
}

func TestBlankLinesCollapse(t *testing.T) {
	lines := renderLines("a\n\n\n\n\nb\n", 40, false)
	if len(lines) != 3 || lines[0] != "a" || lines[1] != "" || lines[2] != "b" {
		t.Fatalf("空行が畳まれていない: %q", lines)
	}
}

func TestHorizontalRuleFillsWidth(t *testing.T) {
	for _, ln := range renderLines("a\n\n---\n\nb\n", 30, false) {
		if strings.HasPrefix(ln, "─") && dispWidth(ln) != 30 {
			t.Fatalf("水平線が幅一杯でない (w=%d): %q", dispWidth(ln), ln)
		}
	}
}

func TestQuoteIsPrefixedOnEveryLine(t *testing.T) {
	lines := renderLines("> 引用のとても長い日本語の行が折り返される場合の見え方\n", 20, false)
	if len(lines) < 2 {
		t.Fatalf("引用が折り返されていない: %q", lines)
	}
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "┃ ") {
			t.Fatalf("引用の行 %d に縦線が無い: %q", i, ln)
		}
	}
}

// 本文の左に出す行番号は「ソース (.md) の行番号」で、表示行の連番ではない
// (ユーザー選定 2026-08-01)。⚠️ 段落は複数のソース行を畳み、折り返しは 1 行を複数行に割るので、
// 番号はブロックの先頭の表示行にだけ出す。同じ番号を続き行にも並べると「その番号の行がそこに
// ある」と読めてしまい、外 (nvim / Claude Code) へ持ち出したとき指す先がずれる。
func TestRenderBodySrcLineNumbers(t *testing.T) {
	src := "# 見出し\n" + // 1
		"\n" + // 2
		"段落の 1 行目で、これは折り返すくらい十分に長い日本語の文章にしてある。\n" + // 3
		"段落の 2 行目 (ソースでは別の行だが、整形では 1 段落に畳まれる)。\n" + // 4
		"\n" + // 5
		"```go\n" + // 6
		"a := 1\n" + // 7
		"b := 2\n" + // 8
		"```\n" // 9
	lines, nums := RenderBody(src, 30, false)
	if len(lines) != len(nums) {
		t.Fatalf("行と行番号の本数が違う: %d vs %d", len(lines), len(nums))
	}
	first := func(want string) int {
		t.Helper()
		for i, ln := range lines {
			if strings.Contains(ln, want) {
				return i
			}
		}
		t.Fatalf("%q の行が無い:\n%s", want, strings.Join(lines, "\n"))
		return -1
	}
	if got := nums[first("見出し")]; got != 1 {
		t.Fatalf("見出しの行番号が違う: %d (1 のはず)", got)
	}
	// 段落は 3 行目から始まり、畳まれた 4 行目・折り返しの続き行には番号を出さない
	para := first("段落の 1 行目")
	if nums[para] != 3 {
		t.Fatalf("段落の行番号が違う: %d (3 のはず)", nums[para])
	}
	if nums[para+1] != 0 {
		t.Fatalf("折り返し/畳み込みの続き行に番号が出た: %d", nums[para+1])
	}
	// コードは折り返さないのでソース行と 1:1 (フェンスの次の行から)
	if got := nums[first("a := 1")]; got != 7 {
		t.Fatalf("コード 1 行目の行番号が違う: %d (7 のはず)", got)
	}
	if got := nums[first("b := 2")]; got != 8 {
		t.Fatalf("コード 2 行目の行番号が違う: %d (8 のはず)", got)
	}
}

// front matter は本文に出さないぶん、行番号がその行数だけずれてはいけない。
func TestRenderBodySrcLineNumbersSkipFrontMatter(t *testing.T) {
	src := "---\n" + // 1
		"status: ongoing\n" + // 2
		"---\n" + // 3
		"# 見出し\n" // 4
	_, nums := RenderBody(src, 40, false)
	if len(nums) == 0 || nums[0] != 4 {
		t.Fatalf("front matter のぶん行番号がずれている: %v (先頭は 4 のはず)", nums)
	}
}
