package issues

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// インライン記法の解析。段落を 1 本の文字列へ連結した後に呼ばれ、意味付きスパン列を返す。
//
// 対応: `code` / **strong** / *em* / ~~strike~~ / [label](dest) / ![alt](dest) / 生 URL /
// バックスラッシュエスケープ。閉じ記号が見つからない場合は記号をそのまま文字として出す
// (壊れた記法で本文が消えるより、記号が見えるほうが実害が小さい)。
//
// リンクは **表示テキストだけ** を出し URL は出さない: この repo の issue は
// [`rules/foo.md`](../_claude/rules/foo.md) のように相対パスの長い URL を多用し、
// 併記すると本文が URL で埋まる。label が空、または label と dest が同じときだけ dest を出す。

// parseInline はインライン記法を解析してスパン列を返す。
func parseInline(s string) []span {
	s = expandTabs(s)
	out := make([]span, 0, 8)
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			out = append(out, span{Text: lit.String(), Style: styleText})
			lit.Reset()
		}
	}
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] < 0x80 && isASCIIPunct(s[i+1]):
			lit.WriteByte(s[i+1])
			i += 2
		case s[i] == '`':
			if content, next, ok := matchCode(s, i); ok {
				flush()
				out = append(out, span{Text: content, Style: styleCodeSpan})
				i = next
				continue
			}
			lit.WriteByte(s[i])
			i++
		case strings.HasPrefix(rest, "!["):
			if label, dest, next, ok := matchLink(s, i+1); ok {
				flush()
				out = append(out, span{Text: "[画像] " + linkText(label, dest), Style: styleDim})
				i = next
				continue
			}
			lit.WriteByte(s[i])
			i++
		case s[i] == '[':
			if label, dest, next, ok := matchLink(s, i); ok {
				flush()
				out = append(out, restyleText(parseInline(linkText(label, dest)), styleLink)...)
				i = next
				continue
			}
			lit.WriteByte(s[i])
			i++
		case strings.HasPrefix(rest, "**"):
			if content, next, ok := matchDelim(s, i, "**"); ok {
				flush()
				out = append(out, restyleText(parseInline(content), styleStrong)...)
				i = next
				continue
			}
			lit.WriteString("**")
			i += 2
		case strings.HasPrefix(rest, "~~"):
			if content, next, ok := matchDelim(s, i, "~~"); ok {
				flush()
				out = append(out, restyleText(parseInline(content), styleStrike)...)
				i = next
				continue
			}
			lit.WriteString("~~")
			i += 2
		case s[i] == '*' && canOpenEm(s, i):
			if content, next, ok := matchDelim(s, i, "*"); ok {
				flush()
				out = append(out, restyleText(parseInline(content), styleEm)...)
				i = next
				continue
			}
			lit.WriteByte(s[i])
			i++
		case strings.HasPrefix(rest, "http://"), strings.HasPrefix(rest, "https://"):
			url, next := matchURL(s, i)
			flush()
			out = append(out, span{Text: url, Style: styleLink})
			i = next
		default:
			r, size := utf8.DecodeRuneInString(rest)
			lit.WriteRune(r)
			i += size
		}
	}
	flush()
	return mergeSpans(out)
}

// isASCIIPunct はバックスラッシュエスケープの対象 (ASCII 記号) か。
func isASCIIPunct(b byte) bool {
	return strings.IndexByte("\\`*_{}[]()#+-.!|~<>\"'", b) >= 0
}

// linkText はリンクの表示テキストを決める (label が無い/dest と同じなら dest を出す)。
func linkText(label, dest string) string {
	if strings.TrimSpace(label) == "" {
		return dest
	}
	return label
}

// matchCode はインラインコード (`...`) を読む。開きと同じ長さのバックティック列で閉じる。
// 前後 1 個の空白は落とす (CommonMark と同じ: `` ` `` のように記号自体を囲む書き方のため)。
func matchCode(s string, i int) (content string, next int, ok bool) {
	n := 0
	for i+n < len(s) && s[i+n] == '`' {
		n++
	}
	fence := s[i : i+n]
	rest := s[i+n:]
	for j := 0; j+n <= len(rest); {
		k := strings.Index(rest[j:], fence)
		if k < 0 {
			return "", 0, false
		}
		j += k
		// 開きより長いバックティック列は閉じにしない (``a` の中の ` は本文)
		if j+n < len(rest) && rest[j+n] == '`' {
			for j+n < len(rest) && rest[j+n] == '`' {
				j++
			}
			continue
		}
		content = rest[:j]
		if len(content) >= 2 && strings.HasPrefix(content, " ") && strings.HasSuffix(content, " ") &&
			strings.TrimSpace(content) != "" {
			content = content[1 : len(content)-1]
		}
		return content, i + n + j + n, true
	}
	return "", 0, false
}

// matchLink は [label](dest) を読む。label / dest の括弧の入れ子は深さで数える。
func matchLink(s string, i int) (label, dest string, next int, ok bool) {
	if i >= len(s) || s[i] != '[' {
		return "", "", 0, false
	}
	depth, labelEnd := 0, -1
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				labelEnd = j
			}
		default:
		}
		if labelEnd >= 0 {
			break
		}
	}
	if labelEnd < 0 || labelEnd+1 >= len(s) || s[labelEnd+1] != '(' {
		return "", "", 0, false
	}
	depth, end := 0, -1
	for j := labelEnd + 1; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = j
			}
		default:
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return "", "", 0, false
	}
	return s[i+1 : labelEnd], s[labelEnd+2 : end], end + 1, true
}

// canOpenEm は `*` を斜体の開きとして扱ってよいか (直前が英数字なら中置の * = 文字扱い)。
func canOpenEm(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// matchDelim は delim で囲まれた区間を読む。開き直後と閉じ直前が空白の場合は記法にしない
// (箇条書きの "* " や掛け算の "a * b" を強調と誤読しないため)。
func matchDelim(s string, i int, delim string) (content string, next int, ok bool) {
	start := i + len(delim)
	if start >= len(s) || s[start] == ' ' {
		return "", 0, false
	}
	for j := start; j < len(s); {
		k := strings.Index(s[j:], delim)
		if k < 0 {
			return "", 0, false
		}
		j += k
		if j == start || s[j-1] == ' ' {
			j += len(delim)
			continue
		}
		return s[start:j], j + len(delim), true
	}
	return "", 0, false
}

// matchURL は生 URL を読む。末尾の句読点・閉じ括弧は URL に含めない (文末の URL が
// "…foo.md)" のように壊れるのを防ぐ)。
func matchURL(s string, i int) (url string, next int) {
	j := i
	for j < len(s) {
		r, size := utf8.DecodeRuneInString(s[j:])
		if unicode.IsSpace(r) || strings.ContainsRune("<>\"'|", r) || r > 0x2000 {
			break
		}
		j += size
	}
	url = s[i:j]
	for len(url) > 0 && strings.ContainsRune(".,;:!?)]", rune(url[len(url)-1])) {
		url = url[:len(url)-1]
	}
	return url, i + len(url)
}

// mergeSpans は同じ style の隣接スパンを 1 本にまとめる (色の切り替えを最小にする)。
func mergeSpans(spans []span) []span {
	out := make([]span, 0, len(spans))
	for _, sp := range spans {
		if sp.Text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Style == sp.Style {
			out[n-1].Text += sp.Text
			continue
		}
		out = append(out, sp)
	}
	return out
}
