// Package pystr は Python の str の振る舞い (splitlines・strip・isspace などの分類・len) を Go で再現する。
//
// 字の分類は Python 3.15.0 の表 (internal/pyunicode) を使う。Go の strings.TrimSpace・strings.Fields・unicode.IsSpace などは
// 空白の集合が違う (Python の isspace は \x1c〜\x1f も空白とみなし、Unicode の版も新しい) ので使わない。
// 文字列は pyunicode と同じく WTF-8 で、単独のサロゲートも 1 字として数える。
package pystr

import (
	"fmt"
	"slices"
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
