package pyre

import (
	"strconv"
	"strings"

	"yomiyasu/internal/pyunicode"
)

// 置換の書式 (re._parser.parse_template の写し)。

type templatePart struct {
	literal string
	group   int
	isGroup bool
}

// Template は sub と expand が使う、パターンに結び付けた置換の書式を読んだ結果。
type Template struct{ parts []templatePart }

// ParseTemplate は置換の書式 (\1 \g<名前> \g<0> \n \0 など) を読む。パターンに無いグループを指すとエラーにする。
func (p *Pattern) ParseTemplate(src string) (t *Template, err error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			t, err = nil, e
		}
	}()
	rs := pyunicode.Runes(src)
	s := newTokenizer(rs)
	var parts []templatePart
	var literal []rune
	flush := func() {
		parts = append(parts, templatePart{literal: pyunicode.FromRunes(literal)})
		literal = literal[:0]
	}
	addGroup := func(index, pos int) {
		if index > p.groups {
			panic(newPatternError("invalid group reference "+strconv.Itoa(index), rs, s.tell()-pos))
		}
		flush()
		parts = append(parts, templatePart{group: index, isGroup: true})
	}
	for {
		this := s.get()
		if this == nil {
			break
		}
		if !this.isBackslash() {
			literal = append(literal, this...)
			continue
		}
		c := this[1]
		switch {
		case c == 'g':
			if !s.match('<') {
				panic(s.errorAt("missing <", 0))
			}
			name := s.getUntil('>', "group name")
			var index int
			if !isASCIIDecimal(name) {
				s.checkGroupName(name, 1)
				i, ok := p.groupIndex[name]
				if !ok {
					panic(newError(ClassIndex, "unknown group name "+pyRepr(name)))
				}
				index = i
			} else {
				v, err := strconv.Atoi(name)
				if err != nil || v >= maxGroups {
					panic(s.errorAt("invalid group reference "+name, len(name)+1))
				}
				index = v
			}
			addGroup(index, len(name)+1)
		case c == '0':
			esc := string(this[1:])
			if s.nextIn(octDigits) {
				esc += string(s.get())
				if s.nextIn(octDigits) {
					esc += string(s.get())
				}
			}
			v, _ := strconv.ParseInt(esc, 8, 32)
			literal = append(literal, rune(v&0xff))
		case runeIn(c, digits):
			esc := string(this[1:])
			isOctal := false
			if s.nextIn(digits) {
				esc += string(s.get())
				if runeIn(c, octDigits) && runeIn(rune(esc[1]), octDigits) && s.nextIn(octDigits) {
					esc += string(s.get())
					isOctal = true
					v, _ := strconv.ParseInt(esc, 8, 32)
					if v > 0o377 {
						panic(s.errorAt("octal escape value \\"+esc+" outside of range 0-0o377", len(esc)+1))
					}
					literal = append(literal, rune(v))
				}
			}
			if !isOctal {
				v, _ := strconv.Atoi(esc)
				addGroup(v, len(esc))
			}
		default:
			if r, ok := simpleEscapes[c]; ok {
				literal = append(literal, r)
				continue
			}
			if runeIn(c, asciiLetters) {
				panic(s.errorAt("bad escape "+this.text(), len(this)))
			}
			literal = append(literal, this...)
		}
	}
	flush()
	return &Template{parts: parts}, nil
}

// expand は置換の結果を組み立てる。一致しなかったグループは空の文字列になる。
func (t *Template) expand(m *Match) string {
	var sb strings.Builder
	for _, part := range t.parts {
		if !part.isGroup {
			sb.WriteString(part.literal)
			continue
		}
		if s, ok := m.Group(part.group); ok {
			sb.WriteString(s)
		}
	}
	return sb.String()
}
