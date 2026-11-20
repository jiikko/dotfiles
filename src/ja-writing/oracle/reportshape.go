package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReportShapeFile は、子の --json の形が崩れたときの人が読む出力の記録 (testdata/golden/ の中)。本家の lint の render_report・render_findings と
// diff の report・report_parts・detector_summary に、欄を 1 つずつ書き換えた dict を通した結果を、書き換え 1 つごとの digest で残す。
// check はこの 5 つの例外を「子の JSON から人が読む形を作れません」の誤りにするので、Go は「本家が例外 ⇔ Go が error」を守る。
// 書き換えの並びと digest の作り方は py/record_report_shape.py の冒頭。
const ReportShapeFile = "report-shape.json"

// ReportShapeDiffInputs は基準の dict を作る diff の入力 (testdata/diff/ の中の名前と、文書の種類。種類を付けた組の基準の名前は「名前@種類」)。
// 節と候補の種類 (硬化・勢い・慎重表現・値・敬語・文体の切り替えの subkind) ごとに、候補が出る組を 1 つ以上選んだ。
var ReportShapeDiffInputs = []struct{ Name, DocType string }{{"actor-added", ""}, {"comma-increase", ""}, {"full-rewrite", ""}, {"heading-level", ""},
	{"hedges-lost", ""}, {"immutable-citation", ""}, {"immutable-stat", ""}, {"immutable-addressee", ""}, {"number-replaced", ""}, {"politeness-mail", ""},
	{"ending-flags", "mail"}, {"register-to-plain", "mail"}, {"register-to-polite", ""}, {"qualifier-lost", ""}, {"register-mixed", ""},
	{"stiff-nominal", ""}, {"stiff-no-chain", ""}, {"certainty-chain", ""}, {"tone-emoji", ""}, {"tone-intensifier", ""}}

// reportShapeLintS1 は lint の基準 s1 の文 (太字・語の一覧の候補が出る短い文)。
const reportShapeLintS1 = "# 見出し\n\n基本的に、これは**とても**重要なことであると考えられます。ヤバい。マジで。\n\n- 項目1\n- 項目2\n\nなお、なお、なお、という感じでしょうか？  \n"

type reportShapeLintInput struct {
	Name    string  `json:"name"`
	Text    string  `json:"text"`
	DocType *string `json:"doc_type"`
	Reader  *string `json:"reader"`
	Words   bool    `json:"words"`
}

type reportShapeDiffInput struct {
	Name    string  `json:"name"`
	Old     string  `json:"old"`
	New     string  `json:"new"`
	DocType *string `json:"doc_type"`
}

// ReportShapeBase は書き換える前の dict (名前は "lint/…" か "diff/…") と、それを書き換えた全部の dict を本家に通した結果。Digests は書き換えの並び
// (reportshape.Plans) の順に 1 つずつで、その書き換えでの呼び出しの結果 (例外なら "E"、ならなければ戻り値の SHA-256 の先頭 16 字) を関数の順に "\n" で
// つないだものの SHA-256 の先頭 16 字。関数の順は lint なら render_report・render_findings、diff なら report_parts・report・detector_summary
// (stiffness・tone・hedges・immutable・politeness・register)。Plan は書き換えの並びの digest、Raised と Compared は本家が例外になった呼び出しとならなかった
// 呼び出しの数。
type ReportShapeBase struct {
	Name     string          `json:"name"`
	D        json.RawMessage `json:"d"`
	Plan     string          `json:"plan"`
	Raised   int             `json:"raised"`
	Compared int             `json:"compared"`
	Digests  []string        `json:"digests"`
}

// ReportShapeGolden は report-shape.json の中身。
type ReportShapeGolden struct {
	Header Header            `json:"header"`
	Bases  []ReportShapeBase `json:"bases"`
}

// ReportShapeDetail は書き換え 1 つの本家の結果 (食い違った書き換えを調べる用)。Results は関数ごとの "E:<例外の型>" か戻り値の digest、Outputs は戻り値の文字列
// (例外なら nil)。
type ReportShapeDetail struct {
	Base    string    `json:"base"`
	Index   int       `json:"index"`
	Path    []any     `json:"path"`
	Variant string    `json:"variant"`
	Digest  string    `json:"digest"`
	Results []string  `json:"results"`
	Outputs []*string `json:"outputs"`
}

// MarshalReportShape は ReportShapeGolden を決まったバイト列にする (基準 1 つを 1 行)。
func MarshalReportShape(g ReportShapeGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	bases, err := marshalLines(g.Bases)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"bases\": " + bases + "\n}\n"), nil
}

type reportShapeIn struct {
	ScriptsDir string                 `json:"scripts_dir"`
	DiffInputs []reportShapeDiffInput `json:"diff_inputs"`
	LintInputs []reportShapeLintInput `json:"lint_inputs"`
	Words      map[string]string      `json:"words"`
	WordsPath  string                 `json:"words_path"`
	Detail     *reportShapeDetailIn   `json:"detail,omitempty"`
}

type reportShapeDetailIn struct {
	Base  string `json:"base"`
	Index int    `json:"index"`
}

// RecordReportShape は基準の dict を本家の diff() と lint_text() で作り、書き換えた全部の dict を本家の 5 つの関数に通して記録にする。
func (r Runner) RecordReportShape(ctx context.Context, header Header, corpus Corpus, diffRoot, wordsDir string) (ReportShapeGolden, error) {
	in, err := reportShapeInput(corpus, diffRoot, wordsDir)
	if err != nil {
		return ReportShapeGolden{}, err
	}
	var out struct {
		Bases []ReportShapeBase `json:"bases"`
	}
	if err := r.runReportShape(ctx, in, &out); err != nil {
		return ReportShapeGolden{}, err
	}
	for _, b := range out.Bases {
		if len(b.Digests) == 0 || b.Raised+b.Compared == 0 {
			return ReportShapeGolden{}, fmt.Errorf("report-shape の基準 %s の書き換えか呼び出しが 0 件です", b.Name)
		}
	}
	if len(out.Bases) != len(ReportShapeDiffInputs)+len(reportShapeLintInputs) {
		return ReportShapeGolden{}, fmt.Errorf("report-shape の基準が %d 個 (入力は %d 個)", len(out.Bases), len(ReportShapeDiffInputs)+len(reportShapeLintInputs))
	}
	return ReportShapeGolden{Header: header, Bases: out.Bases}, nil
}

// ReportShapeDetailOf は基準 base の index 番目の書き換えを本家に通し、呼び出しごとの結果と戻り値を返す (記録と食い違った書き換えを調べる用)。
func (r Runner) ReportShapeDetailOf(ctx context.Context, corpus Corpus, diffRoot, wordsDir, base string, index int) (ReportShapeDetail, error) {
	in, err := reportShapeInput(corpus, diffRoot, wordsDir)
	if err != nil {
		return ReportShapeDetail{}, err
	}
	in.Detail = &reportShapeDetailIn{Base: base, Index: index}
	var out ReportShapeDetail
	err = r.runReportShape(ctx, in, &out)
	return out, err
}

func (r Runner) runReportShape(ctx context.Context, in reportShapeIn, out any) error {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return err
	}
	defer sb.remove()
	in.ScriptsDir = sb.tree.Path(ScriptsDir)
	return r.runDriver(ctx, sb, "record_report_shape.py", in, out)
}

// reportShapeLintInputs は lint の基準を作る入力 (path が空でなければ入力の集まりから読む)。s1 は種類・読者・語の一覧を使う短い文、706 は種類と語の一覧、
// theme は指定なし。
var reportShapeLintInputs = []struct {
	name, path, text string
	docType, reader  *string
	words            bool
}{{name: "s1", text: reportShapeLintS1, docType: strPtr("slides"), reader: strPtr("general"), words: true},
	{name: "706", path: "dotfiles/706-design-chat-log-topic-reader.md", docType: strPtr("slides"), words: true},
	{name: "theme", path: "dotfiles/theme-colors.md"}}

func reportShapeInput(corpus Corpus, diffRoot, wordsDir string) (reportShapeIn, error) {
	in := reportShapeIn{Words: map[string]string{}, WordsPath: "testdata/words"}
	for _, c := range ReportShapeDiffInputs {
		name := c.Name
		old, err := os.ReadFile(filepath.Join(diffRoot, name, "old.md"))
		if err != nil {
			return in, err
		}
		rewritten, err := os.ReadFile(filepath.Join(diffRoot, name, "new.md"))
		if err != nil {
			return in, err
		}
		input := reportShapeDiffInput{Name: name, Old: string(old), New: string(rewritten)}
		if c.DocType != "" {
			input.DocType = &c.DocType
		}
		in.DiffInputs = append(in.DiffInputs, input)
	}
	for _, c := range reportShapeLintInputs {
		text := c.text
		if c.path != "" {
			var err error
			if text, err = corpus.Read(c.path); err != nil {
				return in, err
			}
		}
		in.LintInputs = append(in.LintInputs, reportShapeLintInput{Name: c.name, Text: text, DocType: c.docType, Reader: c.reader, Words: c.words})
	}
	entries, err := os.ReadDir(wordsDir)
	if err != nil {
		return in, fmt.Errorf("語の一覧 %s を読めません: %w", wordsDir, err)
	}
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(wordsDir, e.Name()))
		if err != nil {
			return in, err
		}
		in.Words[e.Name()] = string(body)
	}
	return in, nil
}
