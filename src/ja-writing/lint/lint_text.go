package lint

import (
	"strings"

	"ja-writing/internal/pystr"
	"ja-writing/visibility"
)

// lintState は lint_text が 1 つの文書について持つ状態。群は lintState のメソッドとして、自分のファイル (g01_metrics.go など) に検出を書く。
//
// 群ごとの呼び出し口の約束 (README の「lint の群の足し方」):
//   - 群の検出は gNN_*.go の lintState のメソッド。骨組み (このファイル) は、本家の lint_text と同じ順に呼ぶだけで、群の中身を知らない。
//   - 群が行をまたいで持つ状態 (本家の行のループの外の変数) は、群の gNNState (自分のファイルで定義する構造体) に持ち、lintState の gNN の欄に置く。持つ状態の無い群は欄を持たない。
//   - 骨組みが持つのは、複数の群が読む状態だけ: previousSentence (行のループの途中で消える。short_mochiron が読む) と kikuLines (比喩の「効く」の窓。G8 が読む)・
//     指標 (metrics・listGateChars・proseChars)・引用を伏せた文 (rule9)・段落の文の記録 (sentences)・findings (これまでの候補。後から足す群が重なりを見るために読む)。
//   - 候補は s.add で findings の末尾に足す。足す順が出力の順 (JSON の findings の順) なので、本家の順のまま呼ぶ。
type lintState struct {
	lintContext
	text string
	doc  *document
	an   *visibility.Analysis

	metrics       metrics
	listGateChars int
	proseChars    int
	rule9         map[int][]contextSentence
	sentences     []plainSentence
	dashPossible  bool

	findings []finding

	previousSentence string
	kikuLines        []string

	// trace は行のループが群の口を呼ぶ直前に、口の名前と行を受け取る (テストだけが使う。行のループの分岐と順を検査するため)。
	trace func(step string, r *rowCtx)

	g11 g11State
	g12 g12State
	g13 g13State
}

func (s *lintState) add(f finding) { s.findings = append(s.findings, f) }

func (s *lintState) step(name string, r *rowCtx) {
	if s.trace != nil {
		s.trace(name, r)
	}
}

// rowCtx は行のループの 1 行分 (本家の行のループの局所の変数)。lineSentences は、表と見出しの行を過ぎてから作る。
type rowCtx struct {
	row      *visibility.Line
	lineNo   int
	line     string
	stripped string
	// scanText は見える文字 (保護された範囲を空白にした行) の前後の空白を除いたもの。plainText はそこから太字の記号 (__ と *) を除いたもの。
	scanText  string
	plainText string
	// sentenceBefore はこの行の前の、地の文の最後の文 (空行で区切った Q&A を保つ。short_mochiron が見る)。
	sentenceBefore string
	// kikuBefore は比喩の「効く」の窓 (この行より前の、直近の kikuWindowLines 行)。
	kikuBefore    []string
	lineSentences []string
}

// lintText は本家の lint_text (5512-5988)。
func lintText(text string, c lintContext) (*result, error) {
	s, err := detect(text, c)
	if err != nil {
		return nil, err
	}
	return s.finish(), nil
}

// detect は lint_text の検出の部分 (5512-5932)。s.findings が、種類・allow・ダッシュの上限で外す前の候補 (検出の生の並び。本家の analysis["_findings"] に project_term を足したもの)。
func detect(text string, c lintContext) (*lintState, error) { return detectTraced(text, c, nil) }

func detectTraced(text string, c lintContext, trace func(string, *rowCtx)) (*lintState, error) {
	an, err := visibility.AnalyzeMarkdown(text, true)
	if err != nil {
		return nil, err
	}
	s := &lintState{lintContext: c, text: text, an: an, doc: newDocument(an), trace: trace}
	s.rule9 = s.doc.rule9Context()
	s.g12Prepare()
	if s.metrics, s.listGateChars, s.proseChars, err = s.doc.metricsAndListGate(); err != nil {
		return nil, err
	}
	s.sentences = s.doc.plainSentenceRecords()
	s.dashPossible = strings.ContainsAny(text, "—―─━")

	s.g01Document()
	s.g02SentenceEnd()
	if err := s.g03Bold(); err != nil {
		return nil, err
	}
	s.eachRow()
	s.g12Document()

	s.g14WordFindings()
	s.g15RenyoFindings()
	s.g16AiPhraseFindings()
	s.g17SlideCommaFindings()
	s.g18ShapeFindings()
	s.g19DocumentFindings()
	s.g20ProjectTermFindings()
	return s, nil
}

// eachRow は本家の行のループ (5655-5904)。行の中の順が、1 行で出る候補の順 (JSON の findings の順) になる。
func (s *lintState) eachRow() {
	for _, row := range s.an.Lines {
		r := s.beginRow(row)
		if r == nil {
			continue
		}
		s.step("g04Emoji", r)
		s.g04Emoji(r)
		// 引用は、見える絵文字を除いて、語の検査を当てない (リストや見出しが引用の中にあっても)。
		if row.QuoteDepth != 0 || row.Kind == "quote" {
			s.previousSentence = ""
			continue
		}
		// 表のセルのダッシュは「該当なし」を表すことが多いので、表は見ない。
		if row.Kind != "table" && s.dashPossible {
			s.step("g05Dash", r)
			s.g05Dash(r)
		}
		if row.Kind == "heading" {
			s.step("g06Heading", r)
			s.g06Heading(r)
			s.step("g08VocabularyHeading", r)
			s.g08Vocabulary(r, r.stripped)
			continue
		}
		if row.Kind == "table" {
			s.previousSentence = ""
			continue
		}
		s.step("g12RecordBoldLabel", r)
		s.g12RecordBoldLabel(r)
		for _, part := range splitAfterMarks(r.plainText, "。！？!?") {
			if clean := pystr.Strip(part); clean != "" {
				r.lineSentences = append(r.lineSentences, clean)
			}
		}
		for _, step := range bodySteps {
			s.step(step.name, r)
			step.run(s, r)
		}
	}
}

// beginRow は行のループの 1 行の入口 (本家の 5656-5676)。検査しない行 (コード・frontmatter・HTML のブロック・参照の定義、見える文字が無い行) は nil を返す。
// 前の行の文 (previousSentence) を消す行と、比喩の「効く」の窓を進める行はここで決まる。
func (s *lintState) beginRow(row *visibility.Line) *rowCtx {
	if isOpaqueRow(row.Kind) {
		s.previousSentence = ""
		return nil
	}
	r := &rowCtx{row: row, lineNo: row.Line, line: row.Raw, stripped: pystr.Strip(row.Raw)}
	r.scanText = pystr.Strip(s.doc.proseVisibleText(r.lineNo))
	if row.Kind == "table" || row.Kind == "thematic_break" || row.Kind == "quote" || row.QuoteDepth != 0 || s.doc.imageBreakRow(row, r.scanText) {
		s.previousSentence = ""
	}
	if r.scanText == "" {
		return nil
	}
	// 空行で区切った Q&A は保ち、別の内容があればその最後の文へ更新する。
	r.sentenceBefore = s.previousSentence
	r.plainText = stripEmphasisMarks(r.scanText)
	s.previousSentence = r.plainText
	r.kikuBefore = s.g08Window(r.plainText)
	return r
}

// bodySteps は表と見出しと引用でない行 (地の文の行) で、本家の順に呼ぶ群の口 (5737-5904)。順が 1 行で出る候補の順になる。
var bodySteps = []struct {
	name string
	run  func(*lintState, *rowCtx)
}{
	{"g07TrailingColon", (*lintState).g07TrailingColon},
	{"g08Vocabulary", func(s *lintState, r *rowCtx) { s.g08Vocabulary(r, pystr.Strip(r.line)) }},
	{"g08Translated", (*lintState).g08Translated},
	{"g09Filler", (*lintState).g09Filler},
	{"g09InfoSentences", (*lintState).g09InfoSentences},
	{"g10InstructionResidue", (*lintState).g10InstructionResidue},
	{"g11ShortSentences", (*lintState).g11ShortSentences},
	{"g12Negation", (*lintState).g12Negation},
	{"g13PauseComma", (*lintState).g13PauseComma},
}
