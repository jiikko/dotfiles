// Package pystr は Python の str の振る舞い (splitlines・strip・isspace などの分類・len) を Go で再現する。
//
// 字の分類は Python 3.15.0 の表 (internal/pyunicode) を使う。Go の strings.TrimSpace・strings.Fields・unicode.IsSpace などは
// 空白の集合が違う (Python の isspace は \x1c〜\x1f も空白とみなし、Unicode の版も新しい) ので使わない。
// 文字列は pyunicode と同じく WTF-8 で、単独のサロゲートも 1 字として数える。
package pystr

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"ja-writing/internal/pyunicode"
)

// Len は Python の len(s)。コードポイントの数で、バイト数でも書記素の数でもない。
func Len(s string) int { return pyunicode.RuneCount(s) }

// 以下の分類は Python の str.isspace() などと同じ。空文字列は偽、1 字でも条件を満たさない字があれば偽。
func allRunes(s string, pred func(rune) bool) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); {
		r, n := pyunicode.DecodeRuneInString(s[i:])
		if !pred(r) {
			return false
		}
		i += n
	}
	return true
}

// IsSpace は s.isspace()。
func IsSpace(s string) bool { return allRunes(s, pyunicode.IsSpace) }

// IsAlpha は s.isalpha()。
func IsAlpha(s string) bool { return allRunes(s, pyunicode.IsAlpha) }

// IsAlnum は s.isalnum()。
func IsAlnum(s string) bool { return allRunes(s, pyunicode.IsAlnum) }

// IsDecimal は s.isdecimal()。
func IsDecimal(s string) bool { return allRunes(s, pyunicode.IsDecimal) }

// IsDigit は s.isdigit()。
func IsDigit(s string) bool { return allRunes(s, pyunicode.IsDigit) }

// IsNumeric は s.isnumeric()。
func IsNumeric(s string) bool { return allRunes(s, pyunicode.IsNumeric) }

// Strip は s.strip() (両端の空白を落とす。空白は IsSpace の字)。
func Strip(s string) string { return RStrip(LStrip(s)) }

// LStrip は s.lstrip()。
func LStrip(s string) string { return lstripFunc(s, pyunicode.IsSpace) }

// RStrip は s.rstrip()。
func RStrip(s string) string { return rstripFunc(s, pyunicode.IsSpace) }

// StripChars は s.strip(chars)。chars の中の字を両端から落とす (chars は文字の集合で、接頭辞や接尾辞の文字列ではない)。chars が空なら何も落とさない。
func StripChars(s, chars string) string { return RStripChars(LStripChars(s, chars), chars) }

// LStripChars は s.lstrip(chars)。
func LStripChars(s, chars string) string { return lstripFunc(s, inSet(chars)) }

// RStripChars は s.rstrip(chars)。
func RStripChars(s, chars string) string { return rstripFunc(s, inSet(chars)) }

func inSet(chars string) func(rune) bool {
	set := map[rune]bool{}
	for _, r := range pyunicode.Runes(chars) {
		set[r] = true
	}
	return func(r rune) bool { return set[r] }
}

func lstripFunc(s string, drop func(rune) bool) string {
	i := 0
	for i < len(s) {
		r, n := pyunicode.DecodeRuneInString(s[i:])
		if !drop(r) {
			break
		}
		i += n
	}
	return s[i:]
}

func rstripFunc(s string, drop func(rune) bool) string {
	end := len(s)
	for end > 0 {
		r, n := pyunicode.DecodeLastRuneInString(s[:end])
		if !drop(r) {
			break
		}
		end -= n
	}
	return s[:end]
}

// SplitLines は s.splitlines(keepends)。区切りは pyunicode.IsLineBreak の字と "\r\n" の組 (組は 1 つの区切り)。
// 最後の区切りの後ろに字が無ければ、空の行は足さない。空文字列は 0 行。
func SplitLines(s string, keepEnds bool) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, n := pyunicode.DecodeRuneInString(s[i:])
		if !pyunicode.IsLineBreak(r) {
			i += n
			continue
		}
		eol := i
		i += n
		if r == '\r' && strings.HasPrefix(s[i:], "\n") {
			i++
		}
		if keepEnds {
			lines = append(lines, s[start:i])
		} else {
			lines = append(lines, s[start:eol])
		}
		start = i
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// Repr は Python の repr(str)。引用符は単引用符で、中に単引用符があって二重引用符が無いときだけ二重引用符にする。
// バックスラッシュ・引用符・\t \n \r は短い escape、印字できない字 (str.isprintable が偽) は \xNN \uNNNN \UNNNNNNNN にし、印字できる字はそのまま出す。
func Repr(s string) string {
	q := '\''
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		q = '"'
	}
	var b strings.Builder
	b.WriteRune(q)
	for _, r := range pyunicode.Runes(s) {
		switch {
		case r == q || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case pyunicode.IsPrintable(r):
			b.WriteString(pyunicode.FromRunes([]rune{r}))
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(q)
	return b.String()
}

// EndsWithAny は s.endswith(suffixes) (str.endswith に tuple を渡す形)。
func EndsWithAny(s string, suffixes []string) bool {
	return slices.ContainsFunc(suffixes, func(x string) bool { return strings.HasSuffix(s, x) })
}

// Float は Python の float(s) が受ける文字列を数にする。受けなければ ok が偽。
// 前後の空白 (str.isspace の字) を落とし、十進数字は全角やアラビア・インドの数字も読み、数字の間の「_」を許す。
// 「inf」「infinity」「nan」は大文字小文字を区別せず、符号を付けられる。16 進・桁の途中の空白・「_」の置き場所の誤りは受けない。
// ASCII の空白は " \t\n\v\f\r" だけを空白とみなす (CPython は ASCII の字を変換せずに先頭と末尾の ASCII の空白だけを落とすので、\x1c〜\x1f は残って誤りになる)。
// 範囲を超える値は ±Inf になる (Python も float("1e999") が inf)。
func Float(s string) (float64, bool) {
	var b strings.Builder
	for _, r := range pyunicode.Runes(s) {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case pyunicode.IsSpace(r):
			b.WriteByte(' ')
		default:
			if v, ok := pyunicode.DecimalValue(r); ok {
				b.WriteByte(byte('0' + v))
			} else {
				return 0, false
			}
		}
	}
	text := strings.Trim(b.String(), " \t\n\v\f\r")
	body := strings.TrimLeft(text, "+-")
	if len(text)-len(body) > 1 {
		return 0, false
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		if strings.HasPrefix(text, "-") {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	case "nan":
		return math.NaN(), true
	}
	clean, ok := floatDigits(body)
	if !ok {
		return 0, false
	}
	clean = floatMantissa(clean)
	if strings.HasPrefix(text, "-") {
		clean = "-" + clean
	}
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return f, true
}

// floatDigits は符号を除いた十進の浮動小数点数 (数字の間の「_」を許す) を、「_」を除いた文字列にする。
func floatDigits(s string) (string, bool) {
	var out strings.Builder
	i := 0
	digits := func() bool {
		start := i
		for i < len(s) {
			switch {
			case s[i] >= '0' && s[i] <= '9':
				out.WriteByte(s[i])
				i++
			case s[i] == '_' && i > start && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
				i++
			default:
				return i > start
			}
		}
		return i > start
	}
	intPart := digits()
	fracPart := false
	if i < len(s) && s[i] == '.' {
		out.WriteByte('.')
		i++
		fracPart = digits()
	}
	if !intPart && !fracPart {
		return "", false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		out.WriteByte('e')
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			out.WriteByte(s[i])
			i++
		}
		if !digits() {
			return "", false
		}
	}
	return out.String(), i == len(s)
}

// DecimalInt は十進数字 (Unicode の isdecimal の字) だけの文字列を、Python の int(s) と同じ値にする。
// 符号・空白・「_」・数字でない字を含むと ok が偽 (int() は符号と空白と「_」も受けるが、呼び出し側が正規表現で数字の並びだけを切り出した値にだけ使う)。値が int に収まらなくても ok が偽。
func DecimalInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range pyunicode.Runes(s) {
		v, ok := pyunicode.DecimalValue(r)
		if !ok || n > (math.MaxInt-v)/10 {
			return 0, false
		}
		n = n*10 + v
	}
	return n, true
}

// floatMantissa は floatDigits の出力を「0.<先頭と末尾の 0 を除いた数字>e<指数>」に書き直す。
// strconv.ParseFloat にそのまま渡さない: Go 1.26.0 の ParseFloat は、仮数の 0 が 800 個を超えて並び指数で打ち消す入力
// ("1" + "0"*800 + "e-800") を 0.1 と読む (Python の float は 1.0)。数字の並びを先に縮めると、この経路を通らない。
func floatMantissa(clean string) string {
	mant, expText, _ := strings.Cut(clean, "e")
	intPart, frac, _ := strings.Cut(mant, ".")
	digits := intPart + frac
	lead := len(digits) - len(strings.TrimLeft(digits, "0"))
	digits = strings.Trim(digits, "0")
	if digits == "" {
		return "0"
	}
	exp := int64(len(intPart) - lead)
	if expText != "" {
		sign := int64(1)
		switch expText[0] {
		case '-':
			sign = -1
			expText = expText[1:]
		case '+':
			expText = expText[1:]
		}
		// 指数が大きすぎる値は、どの仮数でも 0 か inf になる所で止める (int64 のあふれを避ける)
		var e int64
		for i := 0; i < len(expText); i++ {
			if e < 1e12 {
				e = e*10 + int64(expText[i]-'0')
			}
		}
		exp += sign * e
	}
	return "0." + digits + "e" + strconv.FormatInt(exp, 10)
}
