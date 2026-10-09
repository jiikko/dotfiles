package markdown

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
	sc := newInlineScan(s)
	sc.parse(0, len(s), styleText, nil)
	return mergeSpans(sc.out)
}

// parse は sc.s[a:b] を読み、span を sc.out に足す。強調・リンクの中身は同じ表のまま区間を狭めて読み直す (中身は元の
// 文字列の部分文字列なので、表を作り直すと入れ子の深さぶん段落の長さの 2 乗になる。issue 699 の反証レビュー)。
// base は地の文字の style、ref は属するリンク (外側のリンクが中の全部を持つ)。どちらも外から渡して、読み終えた span を
// 入れ子のたびに歩き直さない (歩き直すと深さ × span の数になる。2 周目の反証レビュー)。内側の強調の style は外側より勝つ。
func (sc *inlineScan) parse(a, b int, base style, ref *linkRef) {
	s := sc.s[:b] // 区間の外 (b 以降) を見ない。a より前は canOpenEm で区間の頭として扱う
	var lit strings.Builder
	emit := func(sp span) {
		if ref != nil {
			sp.link = ref
		}
		sc.out = append(sc.out, sp)
	}
	flush := func() {
		if lit.Len() > 0 {
			emit(span{Text: lit.String(), Style: base})
			lit.Reset()
		}
	}
	for i := a; i < b; {
		rest := s[i:]
		switch {
		case s[i] == '\\' && i+1 < b && s[i+1] < 0x80 && isASCIIPunct(s[i+1]):
			lit.WriteByte(s[i+1])
			i += 2
		case s[i] == '`':
			if content, next, ok := matchCode(s, i); ok {
				flush()
				emit(span{Text: content, Style: styleCodeSpan, link: &linkRef{kind: LinkCode, dest: content}})
				i = next
				continue
			}
			lit.WriteByte(s[i])
			i++
		case strings.HasPrefix(rest, "!["):
			if l, ok := sc.matchLink(i+1, b); ok {
				flush()
				emit(span{Text: "[画像] " + linkText(l.label(s), l.dest(s)), Style: styleDim})
				i = l.next
				continue
			}
			lit.WriteByte(s[i])
			i++
		case s[i] == '[':
			if l, ok := sc.matchLink(i, b); ok {
				flush()
				ta, tb := l.textRange(s)
				inner := ref // 外側のリンクの中なら、外側のリンクが開く先 (ラベルの文字列ではなく外側の dest)
				if inner == nil {
					inner = &linkRef{kind: LinkDest, dest: l.dest(s)}
				}
				sc.parse(ta, tb, styleLink, inner)
				i = l.next
				continue
			}
			lit.WriteByte(s[i])
			i++
		case strings.HasPrefix(rest, "**"):
			if start, end, ok := sc.matchDelim(i, b, "**"); ok {
				flush()
				sc.parse(start, end, styleStrong, ref)
				i = end + 2
				continue
			}
			lit.WriteString("**")
			i += 2
		case strings.HasPrefix(rest, "~~"):
			if start, end, ok := sc.matchDelim(i, b, "~~"); ok {
				flush()
				sc.parse(start, end, styleStrike, ref)
				i = end + 2
				continue
			}
			lit.WriteString("~~")
			i += 2
		case s[i] == '*' && canOpenEm(s[a:], i-a):
			if start, end, ok := sc.matchDelim(i, b, "*"); ok {
				flush()
				sc.parse(start, end, styleEm, ref)
				i = end + 1
				continue
			}
			lit.WriteByte(s[i])
			i++
		case strings.HasPrefix(rest, "http://"), strings.HasPrefix(rest, "https://"):
			url, next := matchURL(s, i)
			flush()
			emit(span{Text: url, Style: styleLink})
			i = next
		default:
			r, size := utf8.DecodeRuneInString(rest)
			lit.WriteRune(r)
			i += size
		}
	}
	flush()
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
// 前後 1 個の空白は落とす (CommonMark と同じ: バックティック記号自体をコードとして囲む
// 書き方に対応するため)。
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

// inlineScan は 1 段落の括弧の対応と強調の閉じの位置を前もって 1 回の走査で求めておく。開きのたびに段落の末尾まで探すと、
// 閉じない `*` `~~` `[` が多い段落で長さの 2 乗になる (12 万字で 7〜9 秒。issue 699 の 2)。
type inlineScan struct {
	s      string
	pair   []int                 // pair[i] = s[i] が '[' / '(' のとき対応する ']' / ')' の位置 (無ければ -1)
	closer map[string]delimTable // 区切りごとの表 (closers が作る)
	out    []span                // parse が読んだ span (まとめる前)
}

func newInlineScan(s string) *inlineScan {
	sc := &inlineScan{s: s, pair: make([]int, len(s)), closer: map[string]delimTable{}}
	var sq, rd []int
	for i := range len(s) {
		sc.pair[i] = -1
		switch s[i] {
		case '[':
			sq = append(sq, i)
		case '(':
			rd = append(rd, i)
		case ']':
			if n := len(sq); n > 0 {
				sc.pair[sq[n-1]], sq = i, sq[:n-1]
			}
		case ')':
			if n := len(rd); n > 0 {
				sc.pair[rd[n-1]], rd = i, rd[:n-1]
			}
		}
	}
	return sc
}

// closers は delim の閉じの表 (初めて使うときに作る)。c[p] = p から旧来の走査 (次の出現を探し、直前が空白なら区切りの
// 長さだけ飛ばして続ける) で最初に見つかる閉じの位置 (無ければ -1)。飛ばし方まで同じにして、連続する区切り (`****`) の
// 読み方を変えない。
// delimTable は 1 つの区切りの表。occ[p] = p 以降で最初の出現、close[p] = p から走査して見つかる閉じ。
type delimTable struct{ occ, close []int }

func (sc *inlineScan) closers(delim string) delimTable {
	if t, ok := sc.closer[delim]; ok {
		return t
	}
	n, w := len(sc.s), len(delim)
	occ := make([]int, n+w+1)
	c := make([]int, n+w+1)
	for p := n + w; p >= 0; p-- {
		occ[p], c[p] = -1, -1
		if p < n {
			occ[p] = occ[p+1]
			if strings.HasPrefix(sc.s[p:], delim) {
				occ[p] = p
			}
		}
		if j := occ[p]; j >= 0 {
			if j > 0 && sc.s[j-1] != ' ' {
				c[p] = j
			} else {
				c[p] = c[j+w]
			}
		}
	}
	t := delimTable{occ: occ, close: c}
	sc.closer[delim] = t
	return t
}

// link は matchLink が読んだ [label](dest) の位置 (sc.s の中の添字)。
type link struct{ la, lb, da, db, next int }

func (l link) label(s string) string { return s[l.la:l.lb] }
func (l link) dest(s string) string  { return s[l.da:l.db] }

// textRange は表示する文字列 (linkText と同じ選び方: label が空白だけなら dest) の区間。
func (l link) textRange(s string) (int, int) {
	if strings.TrimSpace(l.label(s)) == "" {
		return l.da, l.db
	}
	return l.la, l.lb
}

// matchLink は sc.s[:b] の i から [label](dest) を読む。label / dest の括弧の入れ子は深さで数える (対応は newInlineScan が求めた。
// 対応の相手は後ろの文字だけで決まるので、区間の外 (b 以降) に出た相手は「無い」と読めば区間で数え直したのと同じ)。
func (sc *inlineScan) matchLink(i, b int) (link, bool) {
	s := sc.s
	if i >= b || s[i] != '[' {
		return link{}, false
	}
	labelEnd := sc.pair[i]
	if labelEnd < 0 || labelEnd+1 >= b || s[labelEnd+1] != '(' {
		return link{}, false
	}
	end := sc.pair[labelEnd+1]
	if end < 0 || end >= b {
		return link{}, false
	}
	return link{la: i + 1, lb: labelEnd, da: labelEnd + 2, db: end, next: end + 1}, true
}

// canOpenEm は `*` を斜体の開きとして扱ってよいか (直前が英数字なら中置の * = 文字扱い)。
func canOpenEm(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// matchDelim は sc.s[:b] の i から delim で囲まれた区間 [start, end) を読む。開き直後と閉じ直前が空白の場合は記法にしない
// (箇条書きの "* " や掛け算の "a * b" を強調と誤読しないため)。閉じが区間の外にはみ出すなら無い (closers の注)。
func (sc *inlineScan) matchDelim(i, b int, delim string) (start, end int, ok bool) {
	start = i + len(delim)
	if start >= b || sc.s[start] == ' ' {
		return 0, 0, false
	}
	t := sc.closers(delim)
	j := t.close[start]
	if t.occ[start] == start { // 開きの直後の出現は閉じにしない (空の強調)
		j = t.close[start+len(delim)]
	}
	if j < 0 || j+len(delim) > b {
		return 0, 0, false
	}
	return start, j, true
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

// mergeSpans は同じ (style, link) の隣接スパンを 1 本にまとめる (色の切り替えを最小にする)。
// link が違えば割る (mergeCells と同じ理由)。
func mergeSpans(spans []span) []span {
	out := make([]span, 0, len(spans))
	var text strings.Builder // 続く同じ style の span の文字を足していく (span ごとに += すると 2 乗)
	for _, sp := range spans {
		if sp.Text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Style == sp.Style && out[n-1].link == sp.link {
			text.WriteString(sp.Text)
			continue
		}
		if n := len(out); n > 0 {
			out[n-1].Text = text.String()
		}
		text.Reset()
		text.WriteString(sp.Text)
		out = append(out, sp)
	}
	if n := len(out); n > 0 {
		out[n-1].Text = text.String()
	}
	return out
}
