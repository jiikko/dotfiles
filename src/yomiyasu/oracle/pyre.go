package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"strings"
)

// PyreFile は internal/pyre の表と正解の記録 (testdata/golden/ の中)。表は PyreTablesPath の Go のソースにする。
const (
	PyreFile       = "pyre.json"
	PyreTablesPath = "internal/pyre/casetables_gen.go"
)

// LowerPair は [x, 小文字]。re の IGNORECASE が使う単純な小文字化 (_sre.unicode_tolower) が x を別の字にするもの。
type LowerPair struct{ X, Lower rune }

// MarshalJSON は [x, 小文字] の形にする。
func (p LowerPair) MarshalJSON() ([]byte, error) { return marshalTuple(p.X, p.Lower) }

// UnmarshalJSON は [x, 小文字] を読む。
func (p *LowerPair) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &p.X, &p.Lower) }

// ExtraCase は [小文字, [同じ大文字を持つ別の小文字...]] (re._casefix._EXTRA_CASES)。
type ExtraCase struct {
	Lower  rune
	Others []rune
}

// MarshalJSON は [小文字, [別の小文字...]] の形にする。
func (e ExtraCase) MarshalJSON() ([]byte, error) { return marshalTuple(e.Lower, e.Others) }

// UnmarshalJSON は [小文字, [別の小文字...]] を読む。
func (e *ExtraCase) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &e.Lower, &e.Others) }

// CaseHits は [cp, [re.escape(chr(cp)) を re.I で候補に当てて当たった cp...]]。
type CaseHits struct {
	CP   rune
	Hits []rune
}

// MarshalJSON は [cp, [当たった cp...]] の形にする。
func (c CaseHits) MarshalJSON() ([]byte, error) { return marshalTuple(c.CP, c.Hits) }

// UnmarshalJSON は [cp, [当たった cp...]] を読む。
func (c *CaseHits) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &c.CP, &c.Hits) }

// ClassHits は文字の集合の書き方とフラグに対して、候補のうち当たった cp。
type ClassHits struct {
	Src   string `json:"src"`
	Flags int    `json:"flags"`
	Hits  []rune `json:"hits"`
}

// PyreTables は re の IGNORECASE の規則そのもの (py/record_pyre.py の docstring)。
type PyreTables struct {
	Lower []LowerPair `json:"lower"`
	Cased []CPRange   `json:"cased"`
	Extra []ExtraCase `json:"extra"`
}

// PyreChars は全コードポイントを本物の re に通した結果。
type PyreChars struct {
	Word          []CPRange   `json:"word"`
	Space         []CPRange   `json:"space"`
	Digit         []CPRange   `json:"digit"`
	WordASCII     []CPRange   `json:"word_ascii"`
	SpaceASCII    []CPRange   `json:"space_ascii"`
	DigitASCII    []CPRange   `json:"digit_ascii"`
	Boundary      []int       `json:"boundary"`
	BoundaryASCII []int       `json:"boundary_ascii"`
	Candidates    []rune      `json:"candidates"`
	IgnoreCase    []CaseHits  `json:"ignorecase"`
	Classes       []ClassHits `json:"classes"`
}

// PyreCase は関数 1 回の入力と、Python の結果 (op ごとに形が違う JSON。読み方は internal/pyre のテストにある)。
type PyreCase struct {
	Op      string          `json:"op"`
	Pattern string          `json:"pattern"`
	S       string          `json:"s"`
	Flags   int             `json:"flags,omitempty"`
	Pos     int             `json:"pos,omitempty"`
	EndPos  *int            `json:"endpos,omitempty"`
	N       int             `json:"n,omitempty"`
	Arg     string          `json:"arg,omitempty"`
	Func    bool            `json:"func,omitempty"`
	Result  json.RawMessage `json:"result"`
}

// PyreBody は py/record_pyre.py の出力。
type PyreBody struct {
	Tables PyreTables `json:"tables"`
	Chars  PyreChars  `json:"chars"`
	Funcs  []PyreCase `json:"funcs"`
}

// PyreGolden は pyre.json の中身。
type PyreGolden struct {
	Header Header
	PyreBody
}

// MarshalPyre は PyreGolden を決まったバイト列にする (1 行 1 要素)。
func MarshalPyre(g PyreGolden) ([]byte, error) {
	var parts []string
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	parts = append(parts, header)
	tables, err := marshalObject("tables", []namedLines{{"lower", g.Tables.Lower}, {"cased", g.Tables.Cased}, {"extra", g.Tables.Extra}})
	if err != nil {
		return nil, err
	}
	parts = append(parts, tables)
	c := g.Chars
	chars, err := marshalObject("chars", []namedLines{
		{"word", c.Word}, {"space", c.Space}, {"digit", c.Digit}, {"word_ascii", c.WordASCII}, {"space_ascii", c.SpaceASCII}, {"digit_ascii", c.DigitASCII},
		{"boundary", c.Boundary}, {"boundary_ascii", c.BoundaryASCII}, {"candidates", c.Candidates}, {"ignorecase", c.IgnoreCase}, {"classes", c.Classes},
	})
	if err != nil {
		return nil, err
	}
	parts = append(parts, chars)
	funcs, err := marshalLines(g.Funcs)
	if err != nil {
		return nil, err
	}
	parts = append(parts, fmt.Sprintf("%q: %s", "funcs", funcs))
	return []byte("{\n" + strings.Join(parts, ",\n") + "\n}\n"), nil
}

type namedLines struct {
	name string
	v    any
}

// marshalObject は {"名前": [要素を 1 行ずつ...], ...} を 1 つのキーの値として書く。
func marshalObject(name string, fields []namedLines) (string, error) {
	var parts []string
	for _, f := range fields {
		lines, err := marshalLinesAny(f.v)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("  %q: %s", f.name, strings.ReplaceAll(lines, "\n", "\n  ")))
	}
	return fmt.Sprintf("%q: {\n%s\n}", name, strings.Join(parts, ",\n")), nil
}

// ParsePyre は MarshalPyre が書いたバイト列を PyreGolden に読む。
func ParsePyre(body []byte) (PyreGolden, error) {
	var file struct {
		Header Header `json:"header"`
		PyreBody
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return PyreGolden{}, fmt.Errorf("pyre の記録を読めません: %w", err)
	}
	return PyreGolden{Header: file.Header, PyreBody: file.PyreBody}, nil
}

// RecordPyre は本家の Python で re の字の集合・大文字小文字の対応・関数の結果を読む。
func (r Runner) RecordPyre(ctx context.Context, header Header) (PyreGolden, error) {
	sb, err := newSandbox()
	if err != nil {
		return PyreGolden{}, err
	}
	defer sb.remove()
	g := PyreGolden{Header: header}
	if err := r.runDriver(ctx, sb, "record_pyre.py", struct{}{}, &g.PyreBody); err != nil {
		return PyreGolden{}, err
	}
	return g, nil
}

// GeneratePyreTables は pyre.json の tables から internal/pyre の大文字小文字の表の Go のソースを作る。
// 表は Python の re を読んだ結果そのもので、手で編集しない。同じ記録からは同じバイト列ができる。
func GeneratePyreTables(g PyreGolden) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by yomiyasu/oracle from testdata/golden/pyre.json (make golden); DO NOT EDIT.\n\n")
	b.WriteString("package pyre\n\n")
	b.WriteString("// lowerPairs は re の IGNORECASE の小文字化 (_sre.unicode_tolower) が字を別の字にするもの {字, 小文字}。字の昇順。\nvar lowerPairs = [...]lowerPair{\n")
	for _, p := range g.Tables.Lower {
		fmt.Fprintf(&b, "\t{0x%x, 0x%x},\n", p.X, p.Lower)
	}
	b.WriteString("}\n\n")
	b.WriteString("// casedRanges は _sre.unicode_iscased が真の範囲。\nvar casedRanges = [...]runeRange{\n")
	for _, r := range g.Tables.Cased {
		fmt.Fprintf(&b, "\t{0x%x, 0x%x},\n", r.Lo, r.Hi)
	}
	b.WriteString("}\n\n")
	b.WriteString("// extraCases は小文字化した字が同じ大文字を持つ別の小文字 (re._casefix._EXTRA_CASES)。小文字の昇順。\nvar extraCases = [...]extraCase{\n")
	for _, e := range g.Tables.Extra {
		fmt.Fprintf(&b, "\t{0x%x, []rune{", e.Lower)
		for i, o := range e.Others {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "0x%x", o)
		}
		b.WriteString("}},\n")
	}
	b.WriteString("}\n")
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("生成した Go のソースを整形できません: %w", err)
	}
	return out, nil
}
