package view

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jiikko/dotfiles/src/tuikit/sgr"
	"slack-bbs/render"
)

// 本文の色。配色は style.go (mac-lan-manager に揃えた 24bit の色) で、本文のカードの面の上に描く。見出しは橙の太字。
// 解除は太字・下線 (22・24) と前景だけを戻し (前景は端末の文字色でなく cText へ)、面の地を消さない。
var (
	sgrHeading = sgr.Bold + cAccent.fg() // 欄の見出し
	sgrName    = sgr.Bold + fgText       // 人の名前 (太字)
	sgrMe      = sgr.Bold + cPurple.fg() // 自分の名前 (紫の太字)
	sgrQuote   = cOK.fg()                // 「」で引いた言葉 (緑)
	sgrNumber  = cYellow.fg()            // PR・issue などの番号 (黄)
	sgrDim     = cDim.fg()               // 原文の時刻 (薄く)
	sgrOff     = offFg
)

// minNameLen より短い名前には色を付けない (短い名前は英単語の中などに当たって雑音になる)。
const minNameLen = 3

// highlighter は読む画面の本文の行に色を付ける。色は折り返した後の行ごとに付ける: 行をまたぐ色を持たないので、
// 枠の線や次の行へ色が漏れない (その代わり、折り返しで割れた「」や名前には色が付かない)。
// tuikit の highlight に寄せない: あちらはコードの構文の色付けで、Slack の名前・番号・時刻の色付けとは役割が違う。
//
// 受け入れていること (直すなら実害の実例を添えて再提起する):
//   - 語の境目は英数字でしか見ないので、全角の名前は長い語の一部にも当たる (「山田太郎」が「小山田太郎」の中に当たる)。
//     Slack の表示名はほとんど英字で、当たっても色が付くだけ
//   - 結合文字・異体字セレクタの直前に色を挟むと書記素を割りうる (未確認)。名前・番号の終わりは英数字の後なので、まず起きない
type highlighter struct {
	names []string // 板に出てくる発言者の名前 (長い順。長い名前を先に当てて、短い名前が長い名前の一部に当たらないようにする)
	mine  map[string]bool
}

func newHighlighter(p render.Page) highlighter {
	h := highlighter{mine: map[string]bool{}}
	seen := map[string]bool{}
	for _, t := range p.Topics {
		for _, u := range t.Units {
			for _, m := range u.Msgs {
				if m.Mine {
					h.mine[m.Name] = true
				}
				if utf8.RuneCountInString(m.Name) < minNameLen || seen[m.Name] {
					continue
				}
				seen[m.Name] = true
				h.names = append(h.names, m.Name)
			}
		}
	}
	sort.SliceStable(h.names, func(i, j int) bool { return len(h.names[i]) > len(h.names[j]) })
	return h
}

// line は本文の 1 行 (折り返した後) に色を付ける。first は元の行の 1 行目か (折り返しの続きでないか)。
// 見出しと発言の頭は、1 行目で、かつ字下げの深さが合うときだけ見分ける (見出しは字下げ無し、発言の頭は render.Page.unit の 4 桁)。
// 原文の 2 行目から・折り返しの続きは、本文が「▍」や「時刻 名前: 」で始まっても見出し・発言の頭の色にならない。
func (h highlighter) line(s string, first bool) string {
	body := strings.TrimLeft(s, " ")
	indent := s[:len(s)-len(body)]
	if first && indent == "" && strings.HasPrefix(body, render.HeadingMark) {
		return sgrHeading + body + sgrOff
	}
	if first && indent == render.MsgIndent {
		if head, rest, ok := h.messageHead(body); ok {
			return indent + head + h.text(rest)
		}
	}
	return indent + h.text(body)
}

// messageHead は原文の発言の行の頭 (「(前) 」・時刻・名前・「: 」) に色を付ける。発言の行でなければ ok が偽。
// 行の形は render.Page.unit が組む「[(前) ]MM/DD HH:MM 名前: 本文」。
func (h highlighter) messageHead(s string) (head, rest string, ok bool) {
	pre := ""
	if p := render.BeforeMark; strings.HasPrefix(s, p) {
		pre, s = p, s[len(p):]
	}
	const stamp = len(render.StampLayout)
	if len(s) < stamp+1 || !isStamp(s[:stamp]) || s[stamp] != ' ' {
		return "", "", false
	}
	after := s[stamp+1:]
	name, _, found := strings.Cut(after, ": ")
	if !found || name == "" {
		return "", "", false
	}
	color := sgrName
	if h.mine[name] {
		color = sgrMe
	}
	head = pre + sgrDim + s[:stamp] + sgrOff + " " + color + name + sgrOff + ": "
	return head, after[len(name)+2:], true
}

func isStamp(s string) bool {
	for i, r := range s {
		switch i {
		case 2:
			if r != '/' {
				return false
			}
		case 5:
			if r != ' ' {
				return false
			}
		case 8:
			if r != ':' {
				return false
			}
		default:
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// text は本文の中の「」・人の名前・番号に色を付ける。名前と番号は語の境目でだけ当てる (英単語や URL の途中の数字に当てない)。
func (h highlighter) text(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "「") {
			if j := strings.Index(rest, "」"); j >= 0 {
				end := j + len("」")
				b.WriteString(sgrQuote + rest[:end] + sgrOff)
				i += end
				continue
			}
		}
		if boundaryBefore(s, i) {
			if n := h.nameAt(rest); n != "" {
				color := sgrName
				if h.mine[n] {
					color = sgrMe
				}
				b.WriteString(color + n + sgrOff)
				i += len(n)
				continue
			}
		}
		if numberStart(s, i) {
			if n := numberAt(rest); n > 0 {
				b.WriteString(sgrNumber + rest[:n] + sgrOff)
				i += n
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(rest)
		b.WriteString(rest[:size])
		i += size
	}
	return b.String()
}

func (h highlighter) nameAt(s string) string {
	for _, n := range h.names {
		if strings.HasPrefix(s, n) && boundaryAfter(s, len(n)) {
			return n
		}
	}
	return ""
}

// numberStart は s の i バイト目から番号を探してよいか。英字の直後 (「PR6600」) は許し、数字・「.」の直後 (長い数字の途中・版の番号) は許さない。
func numberStart(s string, i int) bool {
	r, ok := runeBefore(s, i)
	return !ok || (r < '0' || r > '9') && r != '.'
}

// maxNumberDigits より長い数字は番号とみなさない (Slack の ts や permalink の数字に色を付けない)。
const maxNumberDigits = 6

// numberAt は s の頭の番号 (「#」と 3〜6 桁の数字、「〜」で続く範囲も含む) の長さ。番号でなければ 0。
func numberAt(s string) int {
	i := 0
	if strings.HasPrefix(s, "#") {
		i = 1
	}
	digits := func(i int) int {
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		return j
	}
	j := digits(i)
	if j-i < 3 || j-i > maxNumberDigits {
		return 0
	}
	if r := "〜"; strings.HasPrefix(s[j:], r) {
		if k := digits(j + len(r)); k-(j+len(r)) >= 3 && k-(j+len(r)) <= maxNumberDigits {
			j = k
		}
	}
	if !boundaryAfter(s, j) {
		return 0
	}
	return j
}

// wordRune は名前・番号の続きとみなす字 (英数字と、表示名に使われる . _ -)。
func wordRune(r rune) bool {
	return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-')
}

// boundaryBefore は s の i バイト目の前で語が切れているか (先頭か、直前が語の字でない)。
func boundaryBefore(s string, i int) bool {
	r, ok := runeBefore(s, i)
	return !ok || !wordRune(r)
}

// runeBefore は s の i バイト目の直前の字。i が先頭なら ok は偽。
func runeBefore(s string, i int) (r rune, ok bool) {
	if i == 0 {
		return 0, false
	}
	r, _ = utf8.DecodeLastRuneInString(s[:i])
	return r, true
}

// boundaryAfter は s の i バイト目で語が終わるか。文末の「.」(次が語の字でない) は語の終わりとみなす。
func boundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	if r == '.' {
		next, _ := utf8.DecodeRuneInString(s[i+size:])
		return i+size >= len(s) || !wordRune(next)
	}
	return !wordRune(r)
}
