package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"regexp"
	"strings"
	"unicode/utf8"
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

// WStr は Python の str を WTF-8 (単独のサロゲートを 3 バイトの形のまま持つ Go の string) で持つ型。
// JSON では、サロゲートを含まなければ普通の文字列、含めば {"ws": [cp, ...]} にする。JSON の "\ud800" は encoding/json が U+FFFD に変えるので、
// 入力も期待値も別の字になったまま比べてしまう (README の「pystr / pyjson と同じくコードポイントの列」)。
type WStr string

// MarshalJSON はサロゲートを含まない文字列は文字列として、含むものは {"ws": [cp, ...]} として書く。
func (w WStr) MarshalJSON() ([]byte, error) {
	if rs, ok := surrogateRunes(string(w)); ok {
		return json.Marshal(struct {
			WS []rune `json:"ws"`
		}{rs})
	}
	return json.Marshal(string(w))
}

// UnmarshalJSON は MarshalJSON の逆。サロゲートの escape を含む普通の文字列は、読むと U+FFFD になるので断る。
func (w *WStr) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		if hasLoneSurrogateEscape(string(b)) {
			return fmt.Errorf("単独のサロゲートを文字列で書いてはいけません ({\"ws\": [...]} にする): %s", b)
		}
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*w = WStr(s)
		return nil
	}
	var ws struct {
		WS []rune `json:"ws"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ws); err != nil || ws.WS == nil {
		return fmt.Errorf("文字列を読めません: %s", b)
	}
	*w = WStr(fromRunes(ws.WS))
	return nil
}

// surrogateEscape は JSON の文字列の中の \uXXXX のうち、サロゲート (D800〜DFFF) のもの。上位 (D800〜DBFF) と下位 (DC00〜DFFF) を捕獲で分ける。
var surrogateEscape = regexp.MustCompile(`(?i)\\u(?:(d[89ab][0-9a-f]{2})|(d[c-f][0-9a-f]{2}))`)

// hasLoneSurrogateEscape は、上位の次に下位が続く組 (ensure_ascii が書く、BMP の外の 1 字) でない単独のサロゲートの escape があるか。
func hasLoneSurrogateEscape(raw string) bool {
	ms := surrogateEscape.FindAllStringSubmatchIndex(raw, -1)
	for i := 0; i < len(ms); i++ {
		high := ms[i][2] >= 0
		if high && i+1 < len(ms) && ms[i+1][4] >= 0 && ms[i+1][0] == ms[i][1] {
			i++
			continue
		}
		return true
	}
	return false
}

// surrogateRunes は s が単独のサロゲートを 3 バイトの形 (ED A0..BF xx) で含むとき、s を 1 字ずつの列にして true を返す。
func surrogateRunes(s string) ([]rune, bool) {
	var rs []rune
	found := false
	for i := 0; i < len(s); {
		if i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80 {
			rs = append(rs, 0xD000|rune(s[i+1]&0x3F)<<6|rune(s[i+2]&0x3F))
			found = true
			i += 3
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		rs = append(rs, r)
		i += n
	}
	return rs, found
}

// fromRunes は文字の列を WTF-8 の string にする (サロゲートも 3 バイトの形で入れる)。
func fromRunes(rs []rune) string {
	var b []byte
	for _, r := range rs {
		if r >= 0xD800 && r <= 0xDFFF {
			b = append(b, 0xED, 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
		} else {
			b = utf8.AppendRune(b, r)
		}
	}
	return string(b)
}

// PyreCase は関数 1 回の入力と、Python の結果 (op ごとに形が違う JSON。読み方は internal/pyre のテストにある)。
// Result の中の文字列も、サロゲートを含むものは {"ws": [cp, ...]} の形 (WStr と同じ)。
type PyreCase struct {
	Op      string          `json:"op"`
	Pattern WStr            `json:"pattern"`
	S       WStr            `json:"s"`
	Flags   int             `json:"flags,omitempty"`
	Pos     int             `json:"pos,omitempty"`
	EndPos  *int            `json:"endpos,omitempty"`
	N       int             `json:"n,omitempty"`
	Arg     WStr            `json:"arg,omitempty"`
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
	b.WriteString("// Code generated by ja-writing/oracle from testdata/golden/pyre.json (make golden); DO NOT EDIT.\n\n")
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
