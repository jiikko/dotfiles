package visibility

import (
	"fmt"
	"strconv"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 位置は全部、Python の str の添字と同じコードポイントの添字。文字列は WTF-8 (単独のサロゲートも 1 字) で受け、中では []rune で扱う。

func runesOf(s string) []rune { return pyunicode.Runes(s) }

func str(r []rune) string { return pyunicode.FromRunes(r) }

// HasPrefixAt は hasPrefixAt を公開したもの (internal/bold が同じ判定を使う)。
func HasPrefixAt(text []rune, token string, pos int) bool { return hasPrefixAt(text, token, pos) }

// hasPrefixAt は text.startswith(token, pos)。token は ASCII だけ。
func hasPrefixAt(text []rune, token string, pos int) bool {
	if pos < 0 || pos+len(token) > len(text) {
		return false
	}
	for i := range len(token) {
		if text[pos+i] != rune(token[i]) {
			return false
		}
	}
	return true
}

// indexFrom は text.find(token, from)。見つからなければ -1。token は ASCII だけ。
func indexFrom(text []rune, token string, from int) int {
	for i := max(from, 0); i+len(token) <= len(text); i++ {
		if hasPrefixAt(text, token, i) {
			return i
		}
	}
	return -1
}

func isSpaceTab(r rune) bool { return r == ' ' || r == '\t' }

// stripSpaceTab は s.strip(' \t')、lstripSpaceTab は s.lstrip(' \t')。
func lstripSpaceTab(r []rune) []rune {
	i := 0
	for i < len(r) && isSpaceTab(r[i]) {
		i++
	}
	return r[i:]
}

func stripSpaceTab(r []rune) []rune {
	r = lstripSpaceTab(r)
	for len(r) > 0 && isSpaceTab(r[len(r)-1]) {
		r = r[:len(r)-1]
	}
	return r
}

// hasNonSpace は bool(s.strip())。Python の空白 (str.isspace) だけの文字列は偽。
func hasNonSpace(r []rune) bool {
	for _, c := range r {
		if !pyunicode.IsSpace(c) {
			return true
		}
	}
	return false
}

// pyLowerASCII は s.lower() を、ASCII の語と比べる用途に限って写す。
// 小文字化で ASCII の字になる非 ASCII の字は U+212A (KELVIN SIGN → k) だけで、U+0130 は "i" と結合点 U+0307 の 2 字になる (ASCII の語とは等しくならない)。
// 比べる相手が ASCII の語 (worktree・bare・core・true・使える値の一覧) だけの所で使う。
func pyLowerASCII(s string) string {
	var b strings.Builder
	for _, r := range runesOf(s) {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		case r == 0x212A:
			b.WriteByte('k')
		default:
			b.WriteString(pyunicode.FromRunes([]rune{r}))
		}
	}
	return b.String()
}

// commaInt は "{:,}".format(n)。
func commaInt(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// 正規表現は本家と同じパターン・同じフラグで internal/pyre を通す。pyre の照合は時間の上限を付けないので、失敗するのは内部の矛盾だけで、そのときは panic にする。

func mustRegexp(pattern string, flags ...int) *pyre.Pattern {
	f := 0
	for _, x := range flags {
		f |= x
	}
	return pyre.MustCompile(pattern, f)
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Errorf("visibility: 正規表現の照合に失敗しました: %w", err))
	}
	return v
}

func matchAt(p *pyre.Pattern, t pyre.Text, pos int) *pyre.Match {
	return must(p.Match(t, pos, pyre.NoEnd))
}

func searchFrom(p *pyre.Pattern, t pyre.Text, pos int) *pyre.Match {
	return must(p.Search(t, pos, pyre.NoEnd))
}

func fullMatches(p *pyre.Pattern, r []rune) bool {
	return must(p.FullMatch(pyre.NewText(str(r)), 0, pyre.NoEnd)) != nil
}

// stripPy は s.strip()。
func stripPy(s string) string { return pystr.Strip(s) }

func isASCIIPunct(r rune) bool {
	return r > 0 && r < 128 && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", r)
}
