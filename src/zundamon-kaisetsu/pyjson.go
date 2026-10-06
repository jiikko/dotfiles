package main

// Python 版 (dialogue_video.py) と値・文字列を 1 バイトも違えずに作るための部品。
//
// 合成のキャッシュの鍵は Python の json.dumps(params, sort_keys=True, ensure_ascii=False) の sha256 なので、
// 直列化が 1 文字でも違うと既存の <台本>.work/ の合成結果を全部取りこぼす (issue 641 の不変条件 1)。
// Go の encoding/json は 1.0 を 1 と書き、< > & と U+2028/2029 をエスケープするので使えない。
// 正解は testdata/cachekey.json (Python 版から生成した golden) で、pyjson_test.go が突き合わせる。

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// pyFloatRepr は Python の repr(float) と同じ文字列を返す (最短の往復表記。指数は 10 進の小数点位置が
// -4 以下か 16 を超えるときだけ使い、整数値には ".0" を付ける)。
func pyFloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN" // json.dumps の allow_nan=True の表記
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // 例: "1.5e+300" / "1e-05" / "-0e+00"
	mant, exp, _ := strings.Cut(e, "e")
	n, _ := strconv.Atoi(exp)
	decpt := n + 1
	if decpt > -4 && decpt <= 16 {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.ContainsAny(s, ".") {
			s += ".0"
		}
		return s
	}
	return mant + "e" + exp
}

// pyJSONString は json.dumps(s, ensure_ascii=False) と同じ表記を返す。エスケープするのは " \ と
// U+0020 未満の制御文字だけ (U+007F・U+2028・非 BMP はそのまま)。
func pyJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// pyRound3 は Python の round(x, 3)。どちらも正確な 2 進値からの正しい丸め (ちょうど半分は偶数側) なので、
// 小数 3 桁へ整形して読み直すと同じ値になる。
func pyRound3(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return v
}

// pyRound は Python の round(x) (引数 1 つ。ちょうど半分は偶数側)。
func pyRound(x float64) int {
	return int(math.RoundToEven(x))
}

// pyIsSpace は Python の str.isspace() の 1 文字版。Go の unicode.IsSpace に無い U+001C〜U+001F を足す。
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

func pyStrip(s string) string { return strings.TrimFunc(s, pyIsSpace) }

// --- JSON の値 (json.Decoder.UseNumber で読んだ any) を Python と同じに扱う ---

// isPyInt は JSON の数が Python で int になるか (小数点も指数も無い)。
func isPyInt(n json.Number) bool { return !strings.ContainsAny(string(n), ".eE") }

// pyRepr は Python の repr(v) (JSON から読んだ値)。エラー文の「実際: ...」に使う。
// dict は Python では元の順序だが、Go の map は順序を持たないのでキーの順に並べる (エラー文の見た目だけの差)。
func pyRepr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case json.Number:
		if isPyInt(x) {
			return trimIntLiteral(string(x))
		}
		f, err := x.Float64()
		if err != nil && !math.IsInf(f, 0) {
			return string(x)
		}
		return pyFloatRepr(f)
	case string:
		return pyStrRepr(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = pyRepr(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = pyStrRepr(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := sortedKeys(x)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = pyStrRepr(k) + ": " + pyRepr(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprint(x)
	}
}

// trimIntLiteral は JSON の整数表記を Python の int の repr にする ("-0" は 0、先頭の + は JSON に無い)。
func trimIntLiteral(s string) string {
	if s == "-0" {
		return "0"
	}
	return s
}

// pyStrRepr は Python の repr(str)。' を含み " を含まないときだけ " で囲む。
func pyStrRepr(s string) string {
	q := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		q = '"'
	}
	var b strings.Builder
	b.WriteByte(q)
	for _, r := range s {
		switch {
		case r == rune(q) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case !unicode.IsPrint(r) && r > 0x7f:
			if r <= 0xff {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else if r <= 0xffff {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				fmt.Fprintf(&b, `\U%08x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(q)
	return b.String()
}

// pyStr は Python の str(v) (文字列はそのまま、それ以外は repr)。
func pyStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return pyRepr(v)
}

// pyTruthy は Python の bool(v)。
func pyTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case json.Number:
		f, err := x.Float64()
		return err != nil || f != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	default:
		return true
	}
}

// pyFloat は Python の float(v)。失敗は Python の例外に相当する error。
func pyFloat(v any) (float64, error) {
	switch x := v.(type) {
	case json.Number:
		if x == "-0" {
			return 0, nil // Python では整数の -0 は 0 で、float(0) は 0.0 (-0.0 ではない)。鍵の "pitch": 0.0 を合わせる
		}
		f, err := x.Float64()
		if err != nil && !math.IsInf(f, 0) {
			return 0, fmt.Errorf("could not convert string to float: %s", pyStrRepr(string(x)))
		}
		return f, nil
	case float64:
		return x, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		s := pyStrip(x)
		if t := strings.TrimLeft(strings.ToLower(s), "+-"); strings.HasPrefix(t, "0x") || strings.Contains(s, "_") {
			return 0, fmt.Errorf("could not convert string to float: %s", pyStrRepr(x))
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil && !math.IsInf(f, 0) {
			return 0, fmt.Errorf("could not convert string to float: %s", pyStrRepr(x))
		}
		return f, nil
	case nil:
		return 0, fmt.Errorf("float() argument must be a string or a real number, not 'NoneType'")
	default:
		return 0, fmt.Errorf("float() argument must be a string or a real number, not %T", x)
	}
}

// pyInt は Python の int(v) (float は 0 方向へ切り捨て、文字列は 10 進の整数だけ)。
func pyInt(v any) (int64, error) {
	switch x := v.(type) {
	case json.Number:
		if isPyInt(x) {
			n, err := strconv.ParseInt(string(x), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("int too large: %s", x)
			}
			return n, nil
		}
		f, err := x.Float64()
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return 0, fmt.Errorf("cannot convert float %s to integer", x)
		}
		return int64(f), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(pyStrip(x), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid literal for int() with base 10: %s", pyStrRepr(x))
		}
		return n, nil
	case nil:
		return 0, fmt.Errorf("int() argument must be a string, a bytes-like object or a real number, not 'NoneType'")
	default:
		return 0, fmt.Errorf("int() argument must be a string, a bytes-like object or a real number, not %T", x)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
