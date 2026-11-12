package oracle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DifflibFile は internal/difflib の正解の記録 (testdata/golden/ の中)。Python の difflib.SequenceMatcher と本家の _ExactSequenceMatcher の結果。
const DifflibFile = "difflib.json"

// SeqPart は文字列の部分。Text を Repeat 回 (0 は 1 回) つなぐか、File (testdata/corpus/ からの相対パス) の中身を使う。
// 大きい入力は記録に中身を書かず、繰り返しの数か入力の集まりのファイルで持つ (記録を小さく保つ)。File の SHA256 は、ファイルが記録を取った後に変わっていないかを見る。
type SeqPart struct {
	Text   WStr   `json:"text,omitempty"`
	Repeat int    `json:"repeat,omitempty"`
	File   string `json:"file,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// DifflibSeq は照合する並び。Kind が "str" の case では Parts をつないだ文字列、"list" と "reuse" の case では文字列のリスト
// (List そのものか、Lines の部分を "\n" で割ったもの)。
type DifflibSeq struct {
	Parts []SeqPart `json:"parts,omitempty"`
	List  []WStr    `json:"list,omitempty"`
	Lines *SeqPart  `json:"lines,omitempty"`
}

// MatcherResult は 1 つの照合の結果。Kind が "reuse" の case では Reuse だけを持つ。
type MatcherResult struct {
	Blocks    [][3]int     `json:"blocks,omitempty"`
	Opcodes   []OpcodeRec  `json:"opcodes,omitempty"`
	Ratio     float64      `json:"ratio,omitempty"`
	Quick     float64      `json:"quick,omitempty"`
	RealQuick float64      `json:"real_quick,omitempty"`
	Finds     [][3]int     `json:"finds,omitempty"`
	Reuse     [][3]float64 `json:"reuse,omitempty"`
}

// OpcodeRec は get_opcodes の 1 つ ([tag, i1, i2, j1, j2])。
type OpcodeRec struct {
	Tag            string
	I1, I2, J1, J2 int
}

// MarshalJSON は [tag, i1, i2, j1, j2] の形にする。
func (o OpcodeRec) MarshalJSON() ([]byte, error) { return marshalTuple(o.Tag, o.I1, o.I2, o.J1, o.J2) }

// UnmarshalJSON は [tag, i1, i2, j1, j2] を読む。
func (o *OpcodeRec) UnmarshalJSON(b []byte) error {
	return unmarshalTuple(b, &o.Tag, &o.I1, &o.I2, &o.J1, &o.J2)
}

// DifflibCase は 1 つの入力と、Python の照合の結果。driver は同じ入力を difflib.SequenceMatcher(None, a, b, autojunk=False) と
// 本家の _ExactSequenceMatcher の両方に通し、2 つが違えば止まる (本家の前提「difflib と同じ答え」を Python の側で確かめる)。Result は 2 つに共通の結果。
//   - Kind "str": A と B は文字列。Finds の範囲ごとに find_longest_match(alo, ahi, blo, bhi) の結果を Result.Finds に同じ順で持つ。
//     ExactOnly のときは本家の _ExactSequenceMatcher だけを通す (difflib では長さの 2 乗で時間がかかりすぎる大きさ。
//     Go の New と NewExact の突き合わせは internal/difflib の試験が、閾値を小さくして別に持つ)
//   - Kind "list": A と B は文字列のリスト (本家の _ExactSequenceMatcher は文字列でない並びを difflib に任せるので、2 つは同じ求め方になる)
//   - Kind "reuse": A (b に置く文) と B (a に置く文) の文字列のリスト。本家の使い方 (yomiyasu_diff.py の 3005 行と 1902 行あたり) と同じく、
//     照合を 1 つ作って set_seq2(A[i]) のあと set_seq1(B[j]) を回し、(i, j) の順に [real_quick_ratio, quick_ratio, ratio] を Result.Reuse に持つ
type DifflibCase struct {
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	A         DifflibSeq     `json:"a"`
	B         DifflibSeq     `json:"b"`
	Finds     [][4]int       `json:"finds,omitempty"`
	ExactOnly bool           `json:"exact_only,omitempty"`
	Result    *MatcherResult `json:"result"`
}

// DifflibCallSite は本家のソースで照合を作る呼び出し 1 つ (record_difflib.py が ast で読む)。
type DifflibCallSite struct {
	Line   int    `json:"line"`
	Callee string `json:"callee"`
}

// DifflibMethods は本家が照合に使うメソッドの名前のうち、Go の internal/difflib が持つもの。record_difflib.py は、本家のソースに
// これ以外の SequenceMatcher のメソッドの名前 (get_grouped_opcodes など) が現れたら止まる。driver へは指示で渡す (一覧を 2 か所に書かない)。
var DifflibMethods = []string{"set_seq1", "set_seq2", "set_seqs", "find_longest_match", "get_matching_blocks", "get_opcodes", "ratio", "quick_ratio", "real_quick_ratio"}

// DifflibGolden は difflib.json の中身。
type DifflibGolden struct {
	Header    Header            `json:"header"`
	CallSites []DifflibCallSite `json:"call_sites"`
	Methods   []string          `json:"methods"`
	Cases     []DifflibCase     `json:"cases"`
}

// MarshalDifflib は DifflibGolden を決まったバイト列にする (1 行 1 要素)。
func MarshalDifflib(g DifflibGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	sites, err := marshalLines(g.CallSites)
	if err != nil {
		return nil, err
	}
	methods, err := marshalField("methods", g.Methods)
	if err != nil {
		return nil, err
	}
	cases, err := marshalLines(g.Cases)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"call_sites\": " + sites + ",\n" + methods + ",\n\"cases\": " + cases + "\n}\n"), nil
}

// readPart は部分の中身 (Repeat 回つないだもの、または入力の集まりのファイル)。ファイルの SHA-256 が記録と違えばエラー。
func (p SeqPart) readPart(corpusDir string) (string, error) {
	if p.File == "" {
		return strings.Repeat(string(p.Text), max(1, p.Repeat)), nil
	}
	body, err := os.ReadFile(filepath.Join(corpusDir, filepath.FromSlash(p.File)))
	if err != nil {
		return "", fmt.Errorf("入力 %s を読めません: %w", p.File, err)
	}
	if got := SHA256Hex(body); got != p.SHA256 {
		return "", fmt.Errorf("入力 %s が記録を取ったときと違います (SHA-256 %s、記録は %s)。make golden で作り直す", p.File, got, p.SHA256)
	}
	return strings.Repeat(string(body), max(1, p.Repeat)), nil
}

// Text は Kind "str" の並びの文字列 (WTF-8)。Go の試験と、Python へ渡す指示の両方がこれで組み立てる。
func (s DifflibSeq) Text(corpusDir string) (WStr, error) {
	var b strings.Builder
	for _, p := range s.Parts {
		text, err := p.readPart(corpusDir)
		if err != nil {
			return "", err
		}
		b.WriteString(text)
	}
	return WStr(b.String()), nil
}

// Items は Kind "list" と "reuse" の並びの要素。
func (s DifflibSeq) Items(corpusDir string) ([]WStr, error) {
	if s.Lines == nil {
		return s.List, nil
	}
	text, err := s.Lines.readPart(corpusDir)
	if err != nil {
		return nil, err
	}
	var out []WStr
	for _, line := range strings.Split(text, "\n") {
		out = append(out, WStr(line))
	}
	return out, nil
}

// difflibSeqIn は driver へ渡す並び (組み立てた中身)。
type difflibSeqIn struct {
	Text  *WStr   `json:"text,omitempty"`
	Items *[]WStr `json:"items,omitempty"` // 空のリストも書く (nil にすると文字列の並びと見分けられない)
}

type difflibCaseIn struct {
	Kind      string       `json:"kind"`
	A         difflibSeqIn `json:"a"`
	B         difflibSeqIn `json:"b"`
	Finds     [][4]int     `json:"finds,omitempty"`
	ExactOnly bool         `json:"exact_only,omitempty"`
}

func (s DifflibSeq) input(kind, corpusDir string) (difflibSeqIn, error) {
	if kind == "str" {
		text, err := s.Text(corpusDir)
		return difflibSeqIn{Text: &text}, err
	}
	items, err := s.Items(corpusDir)
	if items == nil {
		items = []WStr{}
	}
	return difflibSeqIn{Items: &items}, err
}

// RecordDifflib は DifflibCases の入力を本家の Python (difflib と _ExactSequenceMatcher) に通して記録にする。
func (r Runner) RecordDifflib(ctx context.Context, header Header, corpusDir string) (DifflibGolden, error) {
	cases, err := DifflibCases(corpusDir)
	if err != nil {
		return DifflibGolden{}, err
	}
	in := struct {
		ScriptsDir string          `json:"scripts_dir"`
		Methods    []string        `json:"methods"`
		Cases      []difflibCaseIn `json:"cases"`
	}{Methods: DifflibMethods}
	for _, c := range cases {
		a, err := c.A.input(c.Kind, corpusDir)
		if err != nil {
			return DifflibGolden{}, err
		}
		b, err := c.B.input(c.Kind, corpusDir)
		if err != nil {
			return DifflibGolden{}, err
		}
		in.Cases = append(in.Cases, difflibCaseIn{Kind: c.Kind, A: a, B: b, Finds: c.Finds, ExactOnly: c.ExactOnly})
	}
	sb, err := r.newStagedSandbox()
	if err != nil {
		return DifflibGolden{}, err
	}
	defer sb.remove()
	in.ScriptsDir = sb.tree.Path(ScriptsDir)
	var out struct {
		CallSites []DifflibCallSite `json:"call_sites"`
		Methods   []string          `json:"methods"`
		Results   []*MatcherResult  `json:"results"`
	}
	if err := r.runDriver(ctx, sb, "record_difflib.py", in, &out); err != nil {
		return DifflibGolden{}, err
	}
	if len(out.Results) != len(cases) {
		return DifflibGolden{}, fmt.Errorf("照合の入力 %d 件に対して結果が %d 件でした", len(cases), len(out.Results))
	}
	if len(out.CallSites) == 0 {
		return DifflibGolden{}, errors.New("本家のソースに照合を作る呼び出しが 1 つも見つかりません (record_difflib.py の ast の読み方が本家とずれている)")
	}
	for k := range cases {
		if out.Results[k] == nil {
			return DifflibGolden{}, fmt.Errorf("照合の入力 %s の結果がありません", cases[k].Name)
		}
		cases[k].Result = out.Results[k]
	}
	return DifflibGolden{Header: header, CallSites: out.CallSites, Methods: out.Methods, Cases: cases}, nil
}

// ReadDifflibGolden は difflib.json を読む (試験から使う)。
func ReadDifflibGolden(goldenDir string) (DifflibGolden, error) {
	var g DifflibGolden
	err := ReadGolden(goldenDir, DifflibFile, &g)
	return g, err
}
