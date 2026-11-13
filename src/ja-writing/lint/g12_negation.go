package lint

import (
	"sort"
	"strconv"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// G12 否定の対比 (negative_parallelism・negative_parallelism_density・bold_label_list。本家 yomiyasu_lint.py の 1703-1760・2839-2905・5730・5855-5876・5907-5922)。正規表現は g12_tables.go。

// g12State は G12 が文書の中で持つ状態 (行のループの外の変数 negation_lines・bold_label_lines と、g12Prepare が作る extra_negation_lines)。
type g12State struct {
	extraNegationLines map[int]bool
	negationLines      []int
	boldLabelLines     []int
}

type visiblePart struct {
	line    int
	visible string
}

// g12Prepare は行のループの前に 1 回。本家の _extra_negation_line_numbers(analysis)。段落のブロックごとに、改行で折り返した文を 1 つにつないで「でもない。」の型を探す (ブロックをまたがない)。
func (s *lintState) g12Prepare() {
	s.g12.extraNegationLines = map[int]bool{}
	scan := func(parts []visiblePart) {
		if len(parts) == 0 {
			return
		}
		var joined strings.Builder
		var offsets []int
		total := 0
		for _, p := range parts {
			offsets = append(offsets, total)
			total += pyunicode.RuneCount(p.visible)
			joined.WriteString(p.visible)
		}
		if !strings.Contains(joined.String(), "でもない。") {
			return
		}
		matches, err := negationExtra.Get().Finditer(pyre.NewText(joined.String()), 0, pyre.NoEnd)
		if err != nil {
			panic(err)
		}
		for _, m := range matches {
			// bisect_right(offsets, start) - 1
			index := sort.Search(len(offsets), func(i int) bool { return offsets[i] > m.Start(0) }) - 1
			s.g12.extraNegationLines[parts[index].line] = true
		}
	}
	for _, block := range s.an.Blocks {
		if block.Kind != "paragraph" {
			continue
		}
		var parts []visiblePart
		for _, row := range block.Lines {
			visible := s.doc.proseVisibleText(row.Line)
			if row.QuoteDepth != 0 || s.doc.imageBreakRow(row, visible) {
				scan(parts)
				parts = nil
				continue
			}
			if visible = pystr.Strip(stripEmphasisMarks(visible)); visible != "" {
				parts = append(parts, visiblePart{row.Line, visible})
			}
		}
		scan(parts)
	}
}

// g12RecordBoldLabel は表でも見出しでもない行の最初に呼ばれる (太字の見出し語とコロンで始まる箇条書きの行を覚える)。
func (s *lintState) g12RecordBoldLabel(r *rowCtx) {
	if mustMatchAt0(boldLabelItem, r.line) {
		s.g12.boldLabelLines = append(s.g12.boldLabelLines, r.lineNo)
	}
}

const (
	negationParallelMessage = "「AではなくB」構文が検出されました。否定を外しても主張が変わらない場合は肯定文を検討してください。ただし、誤解の訂正や見方の切り替えなど意味・比重を担っている否定なら、無理に肯定化せずそのまま残してください。"
	negationStackedMessage  = "否定を重ねてから言い切る構文（「AでもBでもない。Cだ。」「Aじゃない、Bです。」）が検出されました。否定している内容を誰も主張していないなら、言い切る文だけにしてください。読み手の思い込みを正す否定なら、一つの文にまとめて残してください。直前で挙げた二つ（問いなど）を両方とも打ち消すだけなら、「どちらでもない。」（敬体なら「どちらでもありません。」）と短く受けてもかまいません。打ち消すほかに、問いにない強めの語や限定、なぜ違うかの説明を含む否定は短くしないでください。"
)

// hasNegativeParallelism は本家の _has_negative_parallelism (「ではなく」の前に「。」「、」でない字があり、後ろに「。」でない字が続く。後ろは「、」を 1 つ挟んでもよい)。
func hasNegativeParallelism(text string) bool {
	rs := pyunicode.Runes(text)
	literal := pyunicode.Runes("ではなく")
	for position := 0; position+len(literal) <= len(rs); position++ {
		if string(rs[position:position+len(literal)]) != string(literal) {
			continue
		}
		end := position + len(literal)
		if position > 0 && !strings.ContainsRune("。、", rs[position-1]) && end < len(rs) {
			if !strings.ContainsRune("。、", rs[end]) {
				return true
			}
			if rs[end] == '、' && end+1 < len(rs) && !strings.ContainsRune("。、", rs[end+1]) {
				return true
			}
		}
	}
	return false
}

// g12Negation は行の中の negative_parallelism の判定。出した行は覚えて、g12Document が数える。
func (s *lintState) g12Negation(r *rowCtx) {
	plain := r.plainText
	message := ""
	switch {
	case strings.Contains(plain, "ではなく") && strings.Count(plain, "ではなく") == len(mustFindall(negationAlso, plain)):
	case hasNegativeParallelism(plain) && strings.Contains(plain, "ではなく"):
		message = negationParallelMessage
	case s.g12.extraNegationLines[r.lineNo] ||
		(strings.Contains(plain, "でもない。") && mustSearch(negationExtra, plain)) ||
		(strings.Contains(plain, "じゃない、") && s.anyJanaiSentence(r)):
		message = negationStackedMessage
	}
	if message == "" {
		return
	}
	s.g12.negationLines = append(s.g12.negationLines, r.lineNo)
	s.add(newFinding("negative_parallelism", r.lineNo, "info", message, r.stripped))
}

func (s *lintState) anyJanaiSentence(r *rowCtx) bool {
	for _, sentence := range withUndecorated(r.lineSentences, s.doc.endClosersFor(r.lineNo)) {
		if mustMatchAt0(negationJanai, sentence) {
			return true
		}
	}
	return false
}

func joinLines(lines []int) string {
	var out []string
	for _, n := range lines[:min(5, len(lines))] {
		out = append(out, strconv.Itoa(n))
	}
	return "行: " + strings.Join(out, ", ")
}

// g12Document は行のループの後に呼ばれる (negative_parallelism_density は 3 件目の行・bold_label_list は 1 件目の行)。
func (s *lintState) g12Document() {
	if n := len(s.g12.negationLines); n >= 3 {
		s.add(newFinding("negative_parallelism_density", s.g12.negationLines[2], "info",
			"否定の対比（「AではなくB」など）が文書内に"+strconv.Itoa(n)+"か所あります。1か所ずつの役割を確かめ、言い切れば足りる箇所だけを肯定文にしてください。", joinLines(s.g12.negationLines)))
	}
	if n := len(s.g12.boldLabelLines); n >= 3 {
		s.add(newFinding("bold_label_list", s.g12.boldLabelLines[0], "info",
			"太字の見出し語とコロンで始まる箇条書きが"+strconv.Itoa(n)+"行あります。値が日時や場所、担当、数値などの短い事実なら残してください。見出し語が抽象的で、値が一文の説明なら、地の文にするか、太字とコロンを外してください。並列関係が明確で読みやすい箇条書きは箇条書きのまま残し、項目の区別と数・順序は変えないでください。", joinLines(s.g12.boldLabelLines)))
	}
}
