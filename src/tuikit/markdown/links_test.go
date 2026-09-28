package markdown

import (
	"regexp"
	"strings"
	"testing"

	"tuikit/termwidth"
)

// segText は Seg が指す画面上の文字列 (色なしの出力から切り出す)。
func segText(lines []string, s Seg) string {
	return termwidth.Slice(lines[s.Line], s.Col, s.Col+s.Width)
}

// linkText は Link の全セグメントをつないだ文字列。
func linkTextOf(lines []string, l Link) string {
	var b strings.Builder
	for _, s := range l.Segs {
		b.WriteString(segText(lines, s))
	}
	return b.String()
}

func TestRenderLinksCollectsCodeSpansAndLinks(t *testing.T) {
	src := "- 実装は `src/a.go:12` と [仕様](../docs/spec.md#x) を見る\n\n| 列 | パス |\n|---|---|\n| a | `bin/x` |\n"
	lines, _, links := RenderLinks(src, 60, false, nil)
	want := []struct {
		kind LinkKind
		dest string
		text string
	}{
		{LinkCode, "src/a.go:12", "src/a.go:12"},
		{LinkDest, "../docs/spec.md#x", "仕様"},
		{LinkCode, "bin/x", "bin/x"},
	}
	if len(links) != len(want) {
		t.Fatalf("リンク数 %d (want %d): %+v", len(links), len(want), links)
	}
	for i, w := range want {
		l := links[i]
		if l.Kind != w.kind || l.Dest != w.dest || linkTextOf(lines, l) != w.text {
			t.Errorf("links[%d] = %v %q 画面 %q (want %v %q %q)", i, l.Kind, l.Dest, linkTextOf(lines, l), w.kind, w.dest, w.text)
		}
	}
}

// 折り返しで割れたリンクは 1 つの Link の複数セグメントになる (別リンクに数えない)。
func TestRenderLinksWrappedLinkIsOneLink(t *testing.T) {
	src := "前置き [とても長いラベルのリンクです](a.md) 後ろ"
	lines, _, links := RenderLinks(src, 16, false, nil)
	if len(links) != 1 {
		t.Fatalf("リンク数 %d (want 1): %+v", len(links), links)
	}
	if n := len(links[0].Segs); n < 2 {
		t.Fatalf("幅 16 で折り返したのにセグメントが %d 個: %q", n, lines)
	}
	if got := linkTextOf(lines, links[0]); got != "とても長いラベルのリンクです" {
		t.Fatalf("セグメントをつないだ文字列 %q", got)
	}
}

// 同じ dest が 2 回出たら 2 リンク。ラベル内のコードスパンは外側のリンク 1 つに畳む。
func TestRenderLinksOccurrencesAndNestedCode(t *testing.T) {
	src := "`a/b` と `a/b`、[`x.md`](../y.md)"
	lines, _, links := RenderLinks(src, 60, false, nil)
	if len(links) != 3 {
		t.Fatalf("リンク数 %d (want 3): %+v", len(links), links)
	}
	if links[0].Segs[0].Col == links[1].Segs[0].Col {
		t.Fatalf("同じ dest の 2 出現が同じ位置を指している: %+v", links)
	}
	if l := links[2]; l.Kind != LinkDest || l.Dest != "../y.md" || linkTextOf(lines, l) != "x.md" || len(l.Segs) != 1 {
		t.Fatalf("ラベル内コードスパンのリンク: %+v 画面 %q", l, linkTextOf(lines, l))
	}
}

// 空白なしで隣り合う 2 つのコードスパンを 1 リンクに潰さない (mergeCells / mergeSpans の link 区切り)。
func TestRenderLinksAdjacentSpansStaySeparate(t *testing.T) {
	_, _, links := RenderLinks("`a/b`**x**`c/d`", 60, false, nil)
	if len(links) != 2 {
		t.Fatalf("リンク数 %d (want 2): %+v", len(links), links)
	}
	lines, _, links := RenderLinks("[a](x.md)[b](y.md)", 60, false, nil)
	if len(links) != 2 || linkTextOf(lines, links[0]) != "a" || linkTextOf(lines, links[1]) != "b" {
		t.Fatalf("隣接リンク: %+v 画面 %q", links, lines)
	}
}

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// 強調は塗るだけで、行・桁を変えない (呼び出し側は mark なしで取った位置を当ててよい)。
func TestRenderLinksMarksDoNotMoveLayout(t *testing.T) {
	src := "# 見出し `h/i`\n\n> 引用 `q/r` と [ラベル](z.md)\n\n本文の `長い/パス/です/ね/これは/折り返す.go` と `x`\n"
	for _, w := range []int{8, 13, 20, 40, 80} {
		plain, nums, links := RenderLinks(src, w, false, nil)
		mark := func(i int) LinkMark {
			if i == 1 {
				return LinkSelected
			}
			return LinkMarked
		}
		colored, nums2, links2 := RenderLinks(src, w, true, mark)
		if len(plain) != len(colored) || len(nums) != len(nums2) || len(links) != len(links2) {
			t.Fatalf("w=%d: mark で行数/リンク数が変わった", w)
		}
		for i := range plain {
			if got := sgrRe.ReplaceAllString(colored[i], ""); got != plain[i] {
				t.Fatalf("w=%d 行 %d: mark ありの文字 %q != なし %q", w, i, got, plain[i])
			}
		}
	}
}

// 選択中のリンクには反転が乗り、選べるリンクには下線が乗る (リンクでない所には乗らない)。
func TestRenderLinksPaintsMarks(t *testing.T) {
	src := "aa `p/q` bb `r/s` cc"
	out, _, links := RenderLinks(src, 60, true, func(i int) LinkMark {
		if i == 0 {
			return LinkSelected
		}
		return LinkMarked
	})
	if len(links) != 2 {
		t.Fatalf("リンク数 %d", len(links))
	}
	line := out[0]
	if !strings.Contains(line, "\x1b[7m\x1b[1mp/q") {
		t.Fatalf("選択中のリンクに反転が無い: %q", line)
	}
	if !strings.Contains(line, "\x1b[4mr/s") {
		t.Fatalf("選べるリンクに下線が無い: %q", line)
	}
	if strings.Count(line, "\x1b[7m") != 1 {
		t.Fatalf("反転が選択以外にも乗った: %q", line)
	}
	// mark=nil なら Render と完全に同じ
	plain, _ := Render(src, 60, true)
	nomark, _, _ := RenderLinks(src, 60, true, nil)
	if strings.Join(plain, "\n") != strings.Join(nomark, "\n") {
		t.Fatalf("mark=nil で Render と出力が違う")
	}
}

// 行末で切られる幅 2 の字は字ごと落ちるので、その字だけのリンクは画面に出ない = 一覧に載せない。
func TestRenderLinksWideCharAtClipEdge(t *testing.T) {
	// 5 列 × 最小 3 桁の表を幅 14 に入れると行が溢れ、出口の clipToWidth が末尾を切る。
	// 最終列のコードスパンが「残り 1 桁から始まる全角 1 字」になる位置を幅を掃いて探す
	src := "| a | b | c | d | e |\n|---|---|---|---|---|\n| x | y | z | w | `全` |\n"
	hit := false
	for w := 10; w <= 20; w++ {
		lines, _, links := RenderLinks(src, w, false, nil)
		for _, l := range links {
			for _, s := range l.Segs {
				if s.Width == 0 || strings.TrimSpace(segText(lines, s)) == "" {
					t.Fatalf("w=%d: 画面に出ていないリンクが載った: %+v 行 %q", w, s, lines[s.Line])
				}
			}
		}
		if len(links) == 0 {
			hit = true // 全角の字が落ちてリンクごと消えた幅がある (この検査が空振りしていない証拠)
		}
	}
	if !hit {
		t.Fatal("前提: 全角のリンクが clip で落ちる幅が 1 つも無い (検査が空振りしている)")
	}
}

// 表のセルで切り詰めて画面に出ないリンクは載らず、一部だけ出るリンクは見える桁までに切る。
func TestRenderLinksClipped(t *testing.T) {
	src := "| a | b |\n|---|---|\n| `abcdefghijklmnop/q` | `zz/yy` |\n"
	lines, _, links := RenderLinks(src, 12, false, nil)
	if len(links) == 0 {
		t.Fatalf("表のリンクが 1 つも取れていない (検査が空振りする): %q", lines)
	}
	for _, l := range links {
		for _, s := range l.Segs {
			if s.Col+s.Width > termwidth.Of(lines[s.Line]) {
				t.Fatalf("セグメントが行の外を指す: %+v 行 %q", s, lines[s.Line])
			}
			if strings.Contains(segText(lines, s), "…") {
				t.Fatalf("省略記号までリンクに含めた: %+v %q", s, segText(lines, s))
			}
		}
	}
	// 出口の clipToWidth で末尾が落ちる行: 列が多く、最小列幅 (tableColWidths の minCol) まで詰めても
	// 幅に収まらない表。行そのものが width を超え、Render の出口で "…" に切られる
	src = "| a | b | c | d | e |\n|---|---|---|---|---|\n| `p/q` | x | y | z | `r/s` |\n"
	lines, _, links = RenderLinks(src, 14, false, nil)
	if len(links) == 0 || !strings.Contains(lines[2], "…") {
		t.Fatalf("前提: 出口で切られる行にリンクがある (links=%d 行 %q)", len(links), lines)
	}
	for _, l := range links {
		for _, s := range l.Segs {
			if s.Width == 0 || s.Col+s.Width > termwidth.Of(lines[s.Line]) || strings.Contains(segText(lines, s), "…") {
				t.Fatalf("clip 後の行の外/省略記号を指す: %+v 行 %q", s, lines[s.Line])
			}
		}
	}
}
