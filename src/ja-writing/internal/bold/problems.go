package bold

import (
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// Problem は表示されない太字の候補 1 つ (本家の bold_problems の dict)。Suggest は直し方の案が無ければ空。
type Problem struct {
	Line    int
	Found   string
	Suggest string
	How     string
}

// Problems は bold_problems(text, skip_frontmatter)。
func Problems(text string, skipFrontmatter bool) ([]Problem, error) {
	if !contains(pyunicode.Runes(text), "**") {
		return nil, nil
	}
	a, err := visibility.AnalyzeMarkdown(text, skipFrontmatter)
	if err != nil {
		return nil, err
	}
	return ProblemsWithAnalysis(text, a)
}

// ProblemsWithAnalysis は _bold_problems_with_analysis (lint_text が、自分で作った analysis を渡して呼ぶ形)。
// コード・frontmatter・HTML ブロック・参照の定義のブロックは見ない。候補はブロックの順、ブロックの中は組の位置の順。
func ProblemsWithAnalysis(text string, a *visibility.Analysis) ([]Problem, error) {
	runes := pyunicode.Runes(text)
	if !contains(runes, "**") {
		return nil, nil
	}
	var out []Problem
	masks := a.MaskSpans
	maskIndex := 0
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
		if !contains(blockText, "**") {
			continue
		}
		offsets := make([]int, len(rows))
		for k, row := range rows {
			offsets[k] = row.Start - begin
		}
		for maskIndex < len(masks) && masks[maskIndex][1] <= begin {
			maskIndex++
		}
		var blockMasks [][2]int
		for _, m := range masks[maskIndex:] {
			if m[0] >= finish {
				break
			}
			blockMasks = append(blockMasks, [2]int{max(m[0], begin) - begin, min(m[1], finish) - begin})
		}
		runs := runsFromMasks(blockText, blockMasks)
		pairs, gfmPairs, commonPairs, c := scan(blockText, runs)
		// このブロックで直し方の案を確かめる組の残りの数 (近道の表を作るかどうかの目安)
		remaining := 0
		for _, p := range pairs {
			if !gfmPairs[p] || !commonPairs[p] {
				remaining++
			}
		}
		for _, p := range pairs {
			if gfmPairs[p] && commonPairs[p] {
				continue
			}
			c.remaining = remaining
			remaining--
			middle, how, err := fixWithContext(blockText, p.i, p.j, c)
			if err != nil {
				return nil, err
			}
			pre := blockText[max(0, p.i-4):p.i]
			post := blockText[min(p.j+2, len(blockText)):min(p.j+6, len(blockText))]
			found := concat(pre, short(blockText[p.i:p.j+2]), post)
			suggest := ""
			if middle != nil {
				suggest = str(concat(pre, short(middle), post))
			}
			lineIndex := bisectRight(offsets, p.i) - 1
			out = append(out, Problem{Line: rows[lineIndex].Line, Found: str(found), Suggest: suggest, How: how})
		}
	}
	return out, nil
}
