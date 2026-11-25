// Package pyshlex は Python の shlex (posix=True) の split・quote・join と、punctuation_chars=True で whitespace_split にした
// shlex.shlex の字句の切り方を写す。字は WTF-8 の Go の文字列で持ち (pyunicode と同じ)、添字はコードポイントで数える。
//
// read_token の状態機械を、そのままの枝の順で写している。枝の順を「同じ意味の素直な形」に組み替えない: 字が複数の集合に入る
// (posix の escape と wordchars の判定の順・punctuation と whitespace_split の判定の順) ので、順を変えると切り方が変わる。
package pyshlex

import (
	"strings"

	"ja-writing/internal/pyops"
	"ja-writing/internal/pyunicode"
)

const (
	whitespace = " \t\r\n"
	quotes     = "'\""
	escape     = "\\"
	// escapedQuotes は posix で中の \ が効く引用符。
	escapedQuotes = "\""
	// punctuationChars は punctuation_chars=True のときの字。
	punctuationChars = "();<>|&"
)

// wordchars は posix の shlex の wordchars (punctuation があれば ~-./*?= を足し、punctuation の字を除く。どれも punctuation に無い)。
var (
	baseWordchars = "abcdfeghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_" +
		"ßàáâãäåæçèéêëìíîïðñòóôõöøùúûüýþÿÀÁÂÃÄÅÆÇÈÉÊËÌÍÎÏÐÑÒÓÔÕÖØÙÚÛÜÝÞ"
	punctWordchars = baseWordchars + "~-./*?="
)

func in(r rune, set string) bool { return strings.ContainsRune(set, r) }

// lexer は shlex.shlex(s, posix=True) に whitespace_split=True と commenters=” を付けたもの (punct が真なら punctuation_chars=True)。
type lexer struct {
	runes     []rune
	pos       int
	punct     bool
	wordchars string
	state     rune // ' '・'a'・'c'・引用符・'\\'。eof は stateEOF
	pushback  []rune
}

const stateEOF rune = -1

// read は instream.read(1) (_pushback_chars があればそこから)。終わりなら ok が偽 (本家の ”)。
func (l *lexer) read() (rune, bool) {
	if l.punct && len(l.pushback) > 0 {
		r := l.pushback[len(l.pushback)-1]
		l.pushback = l.pushback[:len(l.pushback)-1]
		return r, true
	}
	if l.pos >= len(l.runes) {
		return 0, false
	}
	r := l.runes[l.pos]
	l.pos++
	return r, true
}

// errNoClosing・errNoEscaped は本家の ValueError の文。
func errNoClosing() error { return &pyops.Exception{Type: "ValueError", Msg: "No closing quotation"} }
func errNoEscaped() error { return &pyops.Exception{Type: "ValueError", Msg: "No escaped character"} }

// readToken は read_token。終わり (posix の None) なら ok が偽。
func (l *lexer) readToken() (token []rune, ok bool, err error) {
	quoted := false
	escapedState := ' '
	punctuation := ""
	if l.punct {
		punctuation = punctuationChars
	}
	emit := func() bool { return len(token) > 0 || quoted }
	for {
		next, more := l.read()
		if l.state == stateEOF {
			token = nil
			break
		}
		switch {
		case l.state == ' ':
			switch {
			case !more:
				l.state = stateEOF
			case in(next, whitespace):
				if emit() {
					break
				}
				continue
			case in(next, escape):
				escapedState = 'a'
				l.state = next
				continue
			case in(next, l.wordchars):
				token = []rune{next}
				l.state = 'a'
				continue
			case in(next, punctuation):
				token = []rune{next}
				l.state = 'c'
				continue
			case in(next, quotes):
				l.state = next
				continue
			default: // whitespace_split
				token = []rune{next}
				l.state = 'a'
				continue
			}
		case in(l.state, quotes):
			quoted = true
			if !more {
				return nil, false, errNoClosing()
			}
			switch {
			case next == l.state:
				l.state = 'a'
			case in(next, escape) && in(l.state, escapedQuotes):
				escapedState = l.state
				l.state = next
			default:
				token = append(token, next)
			}
			continue
		case in(l.state, escape):
			if !more {
				return nil, false, errNoEscaped()
			}
			if in(escapedState, quotes) && next != l.state && next != escapedState {
				token = append(token, l.state)
			}
			token = append(token, next)
			l.state = escapedState
			continue
		default: // 'a'・'c'
			switch {
			case !more:
				l.state = stateEOF
			case in(next, whitespace):
				l.state = ' '
				if emit() {
					break
				}
				continue
			case l.state == 'c':
				if in(next, punctuation) {
					token = append(token, next)
					continue
				}
				if !in(next, whitespace) {
					l.pushback = append(l.pushback, next)
				}
				l.state = ' '
			case in(next, quotes):
				l.state = next
				continue
			case in(next, escape):
				escapedState = 'a'
				l.state = next
				continue
			case in(next, l.wordchars) || !in(next, punctuation):
				token = append(token, next)
				continue
			default:
				l.pushback = append(l.pushback, next)
				l.state = ' '
				if emit() {
					break
				}
				continue
			}
		}
		break
	}
	if !quoted && len(token) == 0 {
		return nil, false, nil
	}
	return token, true, nil
}

func (l *lexer) all() ([]string, error) {
	out := []string{}
	for {
		token, ok, err := l.readToken()
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, pyunicode.FromRunes(token))
	}
}

// Split は shlex.split(s) (posix・コメントなし)。引用符が閉じない・末尾の \ は ValueError の *pyops.Exception。
func Split(s string) ([]string, error) {
	l := &lexer{runes: pyunicode.Runes(s), state: ' ', wordchars: baseWordchars}
	return l.all()
}

// PunctuationWords は list(shlex.shlex(s, posix=True, punctuation_chars=True)) に whitespace_split=True と commenters=” を付けたもの。
func PunctuationWords(s string) ([]string, error) {
	l := &lexer{runes: pyunicode.Runes(s), state: ' ', wordchars: punctWordchars, punct: true}
	return l.all()
}

const safeChars = "%+,-./0123456789:=@ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz"

// Quote は shlex.quote(s)。
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for i := range len(s) {
		if !strings.ContainsRune(safeChars, rune(s[i])) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Join は shlex.join(words)。
func Join(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = Quote(w)
	}
	return strings.Join(quoted, " ")
}
