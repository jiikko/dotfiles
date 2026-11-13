package bold

import (
	"fmt"
	"testing"

	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

const (
	goldenDir = "../../testdata/golden"
	corpusDir = "../../testdata/corpus"
)

func readCases(t *testing.T) []oracle.BoldCase {
	t.Helper()
	g, err := oracle.ReadBoldGolden(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Cases) == 0 {
		t.Fatal("太字の記録に case が 1 つも無い")
	}
	return g.Cases
}

func input(t *testing.T, c oracle.BoldCase) string {
	t.Helper()
	w, err := c.Input(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	return string(w)
}

// TestProblemsMatchUpstream は、本家の lint と diff の bold_problems (記録を作るときに 2 つが同じ答えであることを確かめてある) と、Go の Problems が同じ答えを返すかを見る。
func TestProblemsMatchUpstream(t *testing.T) {
	cases := readCases(t)
	compared, problems, failures := 0, 0, 0
	for _, c := range cases {
		got, err := Problems(input(t, c), c.SkipFrontmatter)
		if err != nil {
			t.Fatalf("%s (skip_frontmatter=%v): %v", c.Name, c.SkipFrontmatter, err)
		}
		compared++
		problems += len(c.Problems)
		if msg := diffProblems(got, c.Problems); msg != "" {
			failures++
			if failures <= 10 {
				t.Errorf("%s (skip_frontmatter=%v): %s", c.Name, c.SkipFrontmatter, msg)
			}
		}
	}
	if failures > 10 {
		t.Errorf("ほか %d 件の case が本家と違う", failures-10)
	}
	if problems == 0 {
		t.Fatal("記録の候補が 0 件 (入力が太字の枝を通っていない)")
	}
	// 記録の case が欠けても一致の試験は緑のまま通るので、足した入力が全部 (skip_frontmatter の 2 通りで) 記録にあることを見る
	recorded := map[string]int{}
	for _, c := range cases {
		recorded[c.Name]++
	}
	for _, x := range oracle.BoldExtraTexts() {
		if recorded[x.Name] != 2 {
			t.Errorf("足した入力 %s の記録が %d 件 (2 件のはず)。make golden で取り直す", x.Name, recorded[x.Name])
		}
	}
	t.Logf("本家と一致: %d case・候補 %d 件", compared-failures, problems)
}

func diffProblems(got []Problem, want []oracle.BoldProblem) string {
	if len(got) != len(want) {
		return fmt.Sprintf("候補の数が違う (Go %d・本家 %d)。Go %+v / 本家 %+v", len(got), len(want), got, want)
	}
	for k := range got {
		w := Problem{Line: want[k].Line, Found: string(want[k].Found), Suggest: string(want[k].Suggest), How: want[k].How}
		if got[k] != w {
			return fmt.Sprintf("%d 番目の候補が違う。Go %+v / 本家 %+v", k, got[k], w)
		}
	}
	return ""
}

// TestRepairIndexAgreesWithReparse は、近道 (_BoldRepairIndex.decide) が答えを出した案ごとに、解析し直した答えと同じかを見る
// (本家のコメント 2396-2408 行の主張)。本家は近道を、印の多い段落で解析し直した案が 2 つを超えたときにしか使わないので、
// ここでは条件を問わず、案を確かめられる全部の組で近道を作って比べる。
func TestRepairIndexAgreesWithReparse(t *testing.T) {
	cases := readCases(t)
	decided, unknown := 0, 0
	for _, c := range cases {
		text := input(t, c)
		if !contains(pyunicode.Runes(text), "**") {
			continue
		}
		a, err := visibility.AnalyzeMarkdown(text, c.SkipFrontmatter)
		if err != nil {
			t.Fatal(err)
		}
		forEachBlockPair(t, text, a, func(block []rune, p pair, sc *scanContext) {
			if !eligible(block, p.i, p.j, sc) {
				return
			}
			repair, err := repairIndexOf(block, sc)
			if err != nil {
				t.Fatal(err)
			}
			if repair == nil {
				return
			}
			for _, tr := range tries(block, p.i, p.j) {
				v := repair.decide(p.i, p.j, tr.shape, tr.middle)
				if v == decideUnknown {
					unknown++
					continue
				}
				decided++
				ok, err := verifyByReparse(block, p.i, p.j, tr.middle, sc)
				if err != nil {
					t.Fatal(err)
				}
				if ok != (v == decideOK) {
					t.Errorf("%s: 組 (%d, %d) の案 %q で、近道は %v・解析し直すと %v", c.Name, p.i, p.j, str(tr.middle), v == decideOK, ok)
				}
			}
		})
	}
	if decided == 0 {
		t.Fatal("近道が答えを出した案が 1 つも無い (入力が近道を通っていない)")
	}
	t.Logf("近道が答えを出した案 %d・決めずに解析し直しへ回した案 %d", decided, unknown)
}

// forEachBlockPair は ProblemsWithAnalysis と同じ回し方で、ブロックごとの組を f に渡す。
func forEachBlockPair(t *testing.T, text string, a *visibility.Analysis, f func(block []rune, p pair, c *scanContext)) {
	t.Helper()
	runes := pyunicode.Runes(text)
	for _, block := range a.Blocks {
		switch block.Kind {
		case "code", "frontmatter", "html_block", "reference_definition":
			continue
		}
		rows := block.Lines
		begin := rows[0].Start
		last := rows[len(rows)-1]
		finish := last.Start + len(pyunicode.Runes(last.Raw))
		blockText := runes[begin:finish]
		var blockMasks [][2]int
		for _, m := range a.MaskSpans {
			if m[1] <= begin || m[0] >= finish {
				continue
			}
			blockMasks = append(blockMasks, [2]int{max(m[0], begin) - begin, min(m[1], finish) - begin})
		}
		pairs, gfmPairs, commonPairs, c := scan(blockText, runsFromMasks(blockText, blockMasks))
		for _, p := range pairs {
			if gfmPairs[p] && commonPairs[p] {
				continue
			}
			f(blockText, p, c)
		}
	}
}
