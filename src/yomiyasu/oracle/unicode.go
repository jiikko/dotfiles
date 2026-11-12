package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// 記録のファイル名 (testdata/golden/ の中)。internal/pyunicode・pyjson・pystr の正解で、表のもとになる (unicode.json は tablegen.go が Go の表にする)。
const (
	UnicodeFile = "unicode.json"
	JSONFile    = "json.json"
	PystrFile   = "pystr.json"
)

// 配列の形で持つ要素 ([開始, 値] のような) の符号化。1 行に 1 要素を書くので、オブジェクトの形にして重くしない。
func marshalTuple(vs ...any) ([]byte, error) { return json.Marshal(vs) }

func unmarshalTuple(data []byte, dsts ...any) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) != len(dsts) {
		return fmt.Errorf("要素が %d 個、期待 %d 個: %s", len(raw), len(dsts), data)
	}
	for i, d := range dsts {
		if err := json.Unmarshal(raw[i], d); err != nil {
			return fmt.Errorf("%d 番目の要素: %w", i, err)
		}
	}
	return nil
}

// CategoryRun は [開始 cp, 一般カテゴリ]。次の開始の手前までがその値 (最後は 0x10FFFF まで)。
type CategoryRun struct {
	Start rune
	Cat   string
}

// MarshalJSON は [開始, カテゴリ] の形にする。
func (c CategoryRun) MarshalJSON() ([]byte, error) { return marshalTuple(c.Start, c.Cat) }

// UnmarshalJSON は [開始, カテゴリ] を読む。
func (c *CategoryRun) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &c.Start, &c.Cat) }

// CPRange は両端を含むコードポイントの範囲 [lo, hi]。
type CPRange struct{ Lo, Hi rune }

// MarshalJSON は [lo, hi] の形にする。
func (r CPRange) MarshalJSON() ([]byte, error) { return marshalTuple(r.Lo, r.Hi) }

// UnmarshalJSON は [lo, hi] を読む。
func (r *CPRange) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &r.Lo, &r.Hi) }

// CCCRun は結合クラスが 0 でない範囲 [lo, hi, クラス]。
type CCCRun struct {
	Lo, Hi rune
	Class  int
}

// MarshalJSON は [lo, hi, クラス] の形にする。
func (r CCCRun) MarshalJSON() ([]byte, error) { return marshalTuple(r.Lo, r.Hi, r.Class) }

// UnmarshalJSON は [lo, hi, クラス] を読む。
func (r *CCCRun) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &r.Lo, &r.Hi, &r.Class) }

// Decomp は 1 字の正規化の結果 [cp, [結果の cp...]]。
type Decomp struct {
	CP rune
	To []rune
}

// MarshalJSON は [cp, [結果...]] の形にする。
func (d Decomp) MarshalJSON() ([]byte, error) { return marshalTuple(d.CP, d.To) }

// UnmarshalJSON は [cp, [結果...]] を読む。
func (d *Decomp) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &d.CP, &d.To) }

// ComposePair は主合成 [先頭, 続き, 合成した字]。
type ComposePair struct{ First, Second, Composite rune }

// MarshalJSON は [先頭, 続き, 合成] の形にする。
func (p ComposePair) MarshalJSON() ([]byte, error) {
	return marshalTuple(p.First, p.Second, p.Composite)
}

// UnmarshalJSON は [先頭, 続き, 合成] を読む。
func (p *ComposePair) UnmarshalJSON(b []byte) error {
	return unmarshalTuple(b, &p.First, &p.Second, &p.Composite)
}

// NormCase は文字列の正規化 [元, NFC, NFD, NFKC, NFKD]。
type NormCase struct{ S, NFC, NFD, NFKC, NFKD string }

// MarshalJSON は [元, NFC, NFD, NFKC, NFKD] の形にする。
func (c NormCase) MarshalJSON() ([]byte, error) {
	return marshalTuple(c.S, c.NFC, c.NFD, c.NFKC, c.NFKD)
}

// UnmarshalJSON は [元, NFC, NFD, NFKC, NFKD] を読む。
func (c *NormCase) UnmarshalJSON(b []byte) error {
	return unmarshalTuple(b, &c.S, &c.NFC, &c.NFD, &c.NFKC, &c.NFKD)
}

// UnicodeBody は py/gen_unicode.py の出力 (キーの意味はその docstring)。
type UnicodeBody struct {
	Categories []CategoryRun        `json:"categories"`
	Props      map[string][]CPRange `json:"props"`
	CCC        []CCCRun             `json:"ccc"`
	NFD        []Decomp             `json:"nfd"`
	NFKD       []Decomp             `json:"nfkd"`
	NFC        []Decomp             `json:"nfc"`
	NFKC       []Decomp             `json:"nfkc"`
	Compose    []ComposePair        `json:"compose"`
	Digests    map[string]string    `json:"digests"`
	Cases      []NormCase           `json:"cases"`
}

// UnicodeGolden は unicode.json の中身。
type UnicodeGolden struct {
	Header Header
	UnicodeBody
}

// MarshalUnicode は UnicodeGolden を決まったバイト列にする。
func MarshalUnicode(g UnicodeGolden) ([]byte, error) {
	var parts []string
	add := func(s string, err error) error {
		if err != nil {
			return err
		}
		parts = append(parts, s)
		return nil
	}
	if err := add(marshalField("header", g.Header)); err != nil {
		return nil, err
	}
	for _, f := range []struct {
		name string
		v    any
	}{
		{"categories", g.Categories}, {"ccc", g.CCC}, {"nfd", g.NFD}, {"nfkd", g.NFKD}, {"nfc", g.NFC}, {"nfkc", g.NFKC},
		{"compose", g.Compose}, {"cases", g.Cases},
	} {
		lines, err := marshalLinesAny(f.v)
		if err != nil {
			return nil, err
		}
		parts = append(parts, fmt.Sprintf("%q: %s", f.name, lines))
	}
	if err := add(marshalField("props", g.Props)); err != nil {
		return nil, err
	}
	if err := add(marshalField("digests", g.Digests)); err != nil {
		return nil, err
	}
	return []byte("{\n" + strings.Join(parts, ",\n") + "\n}\n"), nil
}

// marshalLinesAny は marshalLines の any 版 (要素の型を呼び出し側で決められないとき用。要素は 1 行 1 つ)。
func marshalLinesAny(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("記録を JSON にできません: %w", err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", fmt.Errorf("配列でないものを行ごとに書けません: %w", err)
	}
	return marshalLines(items)
}

// RecordUnicode は本家の Python で全コードポイントの分類と正規化を読む。
func (r Runner) RecordUnicode(ctx context.Context, header Header) (UnicodeGolden, error) {
	sb, err := newSandbox()
	if err != nil {
		return UnicodeGolden{}, err
	}
	defer sb.remove()
	g := UnicodeGolden{Header: header}
	if err := r.runDriver(ctx, sb, "gen_unicode.py", struct{}{}, &g.UnicodeBody); err != nil {
		return UnicodeGolden{}, err
	}
	return g, nil
}

// PyError は Python の例外 (型・メッセージ・JSONDecodeError なら位置)。
type PyError struct {
	Type string `json:"type"`
	Msg  string `json:"msg"`
	JMsg string `json:"jmsg,omitempty"`
	Pos  int    `json:"pos,omitempty"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

// DumpOpts は json.dumps に渡した引数 (ensure_ascii は常に True)。
type DumpOpts struct {
	Indent     *int     `json:"indent,omitempty"`
	SortKeys   bool     `json:"sort_keys,omitempty"`
	Separators []string `json:"separators,omitempty"`
}

// DumpCase は json.dumps の 1 回の記録。Value は符号化した値 (py/record_json.py の docstring)。
// 出力が大きいときは Out の代わりに Digest (SHA-256) と Len (文字数) を持つ。
type DumpCase struct {
	Value  json.RawMessage `json:"value"`
	Opts   DumpOpts        `json:"opts"`
	Out    *string         `json:"out,omitempty"`
	Digest string          `json:"digest,omitempty"`
	Len    int             `json:"len,omitempty"`
	Error  *PyError        `json:"error,omitempty"`
}

// RepPart は入力を単位 Unit の N 回の繰り返しの連結で表したもの (大きな入力を記録に書かないため)。
type RepPart struct {
	Unit string
	N    int
}

// MarshalJSON は [単位, 回数] の形にする。
func (p RepPart) MarshalJSON() ([]byte, error) { return marshalTuple(p.Unit, p.N) }

// UnmarshalJSON は [単位, 回数] を読む。
func (p *RepPart) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &p.Unit, &p.N) }

// Summary は深い入れ子の結果を反復で数えた形 (節点の数・最大の深さ・葉の型の名前ごとの数)。
type Summary struct {
	Nodes  int            `json:"nodes"`
	Depth  int            `json:"depth"`
	Leaves map[string]int `json:"leaves"`
}

// LoadCase は json.loads(入力.decode("utf-8")) の 1 回の記録。入力は Text (UTF-8 の文字列)・Hex (バイト列)・Rep のどれか 1 つ。
// 成功なら V (符号化した値) か Summary (Rep のとき)、失敗なら Error。
type LoadCase struct {
	Text    *string         `json:"text,omitempty"`
	Hex     *string         `json:"hex,omitempty"`
	Rep     []RepPart       `json:"rep,omitempty"`
	V       json.RawMessage `json:"v,omitempty"`
	Summary *Summary        `json:"summary,omitempty"`
	Error   *PyError        `json:"error,omitempty"`
}

// RoundCase は round(x, n) の記録 (x は IEEE 754 の 16 進)。RoundIntCase は round(x) (n なし。戻り値は int)。
type RoundCase struct {
	X      string          `json:"x"`
	N      int             `json:"n"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *PyError        `json:"error,omitempty"`
}

// RoundIntCase は round(x) の記録。
type RoundIntCase struct {
	X      string          `json:"x"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *PyError        `json:"error,omitempty"`
}

// JSONBody は py/record_json.py の出力。
type JSONBody struct {
	Dumps    []DumpCase     `json:"dumps"`
	Loads    []LoadCase     `json:"loads"`
	Round    []RoundCase    `json:"round"`
	RoundInt []RoundIntCase `json:"round_int"`
}

// JSONGolden は json.json の中身。
type JSONGolden struct {
	Header Header
	JSONBody
}

// MarshalJSONGolden は JSONGolden を決まったバイト列にする。
func MarshalJSONGolden(g JSONGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	parts := []string{header}
	for _, f := range []struct {
		name string
		v    any
	}{{"dumps", g.Dumps}, {"loads", g.Loads}, {"round", g.Round}, {"round_int", g.RoundInt}} {
		lines, err := marshalLinesAny(f.v)
		if err != nil {
			return nil, err
		}
		parts = append(parts, fmt.Sprintf("%q: %s", f.name, lines))
	}
	return []byte("{\n" + strings.Join(parts, ",\n") + "\n}\n"), nil
}

// RecordJSON は本家の Python の json と round を決まった入力に通して記録にする。
func (r Runner) RecordJSON(ctx context.Context, header Header) (JSONGolden, error) {
	sb, err := newSandbox()
	if err != nil {
		return JSONGolden{}, err
	}
	defer sb.remove()
	g := JSONGolden{Header: header}
	if err := r.runDriver(ctx, sb, "record_json.py", struct{}{}, &g.JSONBody); err != nil {
		return JSONGolden{}, err
	}
	return g, nil
}

// PystrCase は 1 つの文字列に str のメソッドを当てた記録。文字列は JSON の文字列か {"ws": [cp...]} (サロゲートを含むもの)。
// Chars があるときは strip(chars) の 3 つも持つ。
type PystrCase struct {
	S           json.RawMessage   `json:"s"`
	Len         int               `json:"len"`
	Lines       []json.RawMessage `json:"lines"`
	LinesKeep   []json.RawMessage `json:"lines_keep"`
	Strip       json.RawMessage   `json:"strip"`
	LStrip      json.RawMessage   `json:"lstrip"`
	RStrip      json.RawMessage   `json:"rstrip"`
	IsSpace     bool              `json:"isspace"`
	IsAlpha     bool              `json:"isalpha"`
	IsAlnum     bool              `json:"isalnum"`
	IsDecimal   bool              `json:"isdecimal"`
	IsDigit     bool              `json:"isdigit"`
	IsNumeric   bool              `json:"isnumeric"`
	Chars       json.RawMessage   `json:"chars,omitempty"`
	StripChars  json.RawMessage   `json:"strip_chars,omitempty"`
	LStripChars json.RawMessage   `json:"lstrip_chars,omitempty"`
	RStripChars json.RawMessage   `json:"rstrip_chars,omitempty"`
}

// PystrGolden は pystr.json の中身。
type PystrGolden struct {
	Header Header      `json:"header"`
	Cases  []PystrCase `json:"cases"`
}

// MarshalPystr は PystrGolden を決まったバイト列にする。
func MarshalPystr(g PystrGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	cases, err := marshalLines(g.Cases)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"cases\": " + cases + "\n}\n"), nil
}

// RecordPystr は本家の Python の str のメソッドを決まった文字列に当てて記録にする。
func (r Runner) RecordPystr(ctx context.Context, header Header) (PystrGolden, error) {
	sb, err := newSandbox()
	if err != nil {
		return PystrGolden{}, err
	}
	defer sb.remove()
	g := PystrGolden{Header: header}
	if err := r.runDriver(ctx, sb, "record_pystr.py", struct{}{}, &g); err != nil {
		return PystrGolden{}, err
	}
	g.Header = header
	return g, nil
}

// ParseUnicode は MarshalUnicode が書いたバイト列を UnicodeGolden に読む。
func ParseUnicode(body []byte) (UnicodeGolden, error) {
	var file struct {
		Header Header `json:"header"`
		UnicodeBody
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return UnicodeGolden{}, fmt.Errorf("unicode の記録を読めません: %w", err)
	}
	return UnicodeGolden{Header: file.Header, UnicodeBody: file.UnicodeBody}, nil
}
