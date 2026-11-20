package oracle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ReportShapeFile は、子の --json の形が崩れたときの人が読む出力の記録 (testdata/golden/ の中)。本家の lint の render_report・render_findings と
// diff の report・report_parts・detector_summary に、欄を 1 つずつ書き換えた dict を通し、例外になったか (ならなければ戻り値の SHA-256 の先頭 16 字) を残す。
// check はこの 5 つの例外を「子の JSON から人が読む形を作れません」の誤りにするので、Go は「本家が例外 ⇔ Go が error」を守る。
// 書き換えの規則と記録に残す書き換えの選び方は py/record_report_shape.py の冒頭。
const ReportShapeFile = "report-shape.json"

// ReportShapeDiffInputs は基準の dict を作る diff の入力 (testdata/diff/ の中の名前と、文書の種類。種類を付けた組の基準の名前は「名前@種類」)。
// 節と候補の種類 (硬化・勢い・慎重表現・値・敬語・文体の切り替えの subkind) ごとに、候補が出る組を 1 つ以上選んだ。
var ReportShapeDiffInputs = []struct{ Name, DocType string }{{"actor-added", ""}, {"comma-increase", ""}, {"full-rewrite", ""}, {"heading-level", ""},
	{"hedges-lost", ""}, {"immutable-citation", ""}, {"immutable-stat", ""}, {"immutable-addressee", ""}, {"number-replaced", ""}, {"politeness-mail", ""},
	{"ending-flags", "mail"}, {"register-to-plain", "mail"}, {"register-to-polite", ""}, {"qualifier-lost", ""}, {"register-mixed", ""},
	{"stiff-nominal", ""}, {"stiff-no-chain", ""}, {"certainty-chain", ""}, {"tone-emoji", ""}, {"tone-intensifier", ""}}

// reportShapeLintS1 は lint の基準の 1 つ (種類・読者・語の一覧を使い、太字・語の一覧の候補が出る短い文)。
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

// ReportShapeBase は書き換える前の dict (名前は "lint/…" か "diff/…")。
type ReportShapeBase struct {
	Name string          `json:"name"`
	D    json.RawMessage `json:"d"`
}

// ReportShapeMutation は書き換え 1 つと、関数ごとの本家の結果 ("E:<例外の型>" か戻り値の SHA-256 の先頭 16 字)。Results の並びは
// lint なら render_report・render_findings、diff なら report_parts・report・detector_summary (stiffness・tone・hedges・immutable・politeness・register)。
type ReportShapeMutation struct {
	Base    string   `json:"base"`
	Path    []any    `json:"path"`
	Variant string   `json:"variant"`
	Results []string `json:"results"`
}

// ReportShapeGolden は report-shape.json の中身。
type ReportShapeGolden struct {
	Header    Header                `json:"header"`
	Bases     []ReportShapeBase     `json:"bases"`
	Mutations []ReportShapeMutation `json:"mutations"`
}

// MarshalReportShape は ReportShapeGolden を決まったバイト列にする (基準と書き換えを 1 行 1 つ)。
func MarshalReportShape(g ReportShapeGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	bases, err := marshalLines(g.Bases)
	if err != nil {
		return nil, err
	}
	mutations, err := marshalLines(g.Mutations)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"bases\": " + bases + ",\n\"mutations\": " + mutations + "\n}\n"), nil
}

// RecordReportShape は基準の dict を本家の diff() と lint_text() で作り、書き換えた dict を本家の 5 つの関数に通して記録にする。
func (r Runner) RecordReportShape(ctx context.Context, header Header, corpus Corpus, diffRoot, wordsDir string) (ReportShapeGolden, error) {
	in := struct {
		ScriptsDir string                 `json:"scripts_dir"`
		DiffInputs []reportShapeDiffInput `json:"diff_inputs"`
		LintInputs []reportShapeLintInput `json:"lint_inputs"`
		Words      map[string]string      `json:"words"`
		WordsPath  string                 `json:"words_path"`
	}{Words: map[string]string{}, WordsPath: "testdata/words"}
	for _, c := range ReportShapeDiffInputs {
		name := c.Name
		old, err := os.ReadFile(filepath.Join(diffRoot, name, "old.md"))
		if err != nil {
			return ReportShapeGolden{}, err
		}
		rewritten, err := os.ReadFile(filepath.Join(diffRoot, name, "new.md"))
		if err != nil {
			return ReportShapeGolden{}, err
		}
		input := reportShapeDiffInput{Name: name, Old: string(old), New: string(rewritten)}
		if c.DocType != "" {
			input.DocType = &c.DocType
		}
		in.DiffInputs = append(in.DiffInputs, input)
	}
	slides, general := "slides", "general"
	in.LintInputs = append(in.LintInputs, reportShapeLintInput{Name: "s1", Text: reportShapeLintS1, DocType: &slides, Reader: &general, Words: true})
	for _, c := range []struct {
		name, path string
		docType    *string
		words      bool
	}{{"706", "dotfiles/706-design-chat-log-topic-reader.md", &slides, true}, {"theme", "dotfiles/theme-colors.md", nil, false}} {
		text, err := corpus.Read(c.path)
		if err != nil {
			return ReportShapeGolden{}, err
		}
		in.LintInputs = append(in.LintInputs, reportShapeLintInput{Name: c.name, Text: text, DocType: c.docType, Words: c.words})
	}
	entries, err := os.ReadDir(wordsDir)
	if err != nil {
		return ReportShapeGolden{}, fmt.Errorf("語の一覧 %s を読めません: %w", wordsDir, err)
	}
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(wordsDir, e.Name()))
		if err != nil {
			return ReportShapeGolden{}, err
		}
		in.Words[e.Name()] = string(body)
	}
	sb, err := r.newStagedSandbox()
	if err != nil {
		return ReportShapeGolden{}, err
	}
	defer sb.remove()
	in.ScriptsDir = sb.tree.Path(ScriptsDir)
	var out struct {
		Bases     map[string]json.RawMessage `json:"bases"`
		Mutations []ReportShapeMutation      `json:"mutations"`
	}
	if err := r.runDriver(ctx, sb, "record_report_shape.py", in, &out); err != nil {
		return ReportShapeGolden{}, err
	}
	if len(out.Mutations) == 0 {
		return ReportShapeGolden{}, errors.New("report-shape の書き換えが 0 件です")
	}
	g := ReportShapeGolden{Header: header, Mutations: out.Mutations}
	names := make([]string, 0, len(out.Bases))
	for name := range out.Bases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		g.Bases = append(g.Bases, ReportShapeBase{Name: name, D: out.Bases[name]})
	}
	return g, nil
}
