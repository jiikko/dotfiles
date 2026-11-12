package pyre

import (
	"strconv"
	"strings"
)

// 構文木を regexp2 のパターンの文字列に直す。フラグは _compiler の _compile と同じく、部分パターンごとの (?i:...) などで変わる。

type emitter struct {
	st *state
}

func combineFlags(flags, add, del int) int {
	if add&typeFlags != 0 {
		flags &^= typeFlags
	}
	return (flags | add) &^ del
}

func rulesFor(flags int) caseRules { return caseRules{ascii: flags&UNICODE == 0} }

func emitRune(b *strings.Builder, r rune) {
	switch {
	case r < 0x80 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
		b.WriteRune(r)
	case r <= 0xFFFF:
		b.WriteString(`\u`)
		hex := strings.ToUpper(strconv.FormatInt(int64(r), 16))
		b.WriteString(strings.Repeat("0", 4-len(hex)) + hex)
	default:
		b.WriteRune(r)
	}
}

// emitSet は字の集合を 1 つの字か文字クラスにする。空の集合は決して当たらない (?!)。
func emitSet(b *strings.Builder, s rset) {
	switch {
	case len(s) == 0:
		b.WriteString("(?!)")
	case len(s) == 1 && s[0].lo == s[0].hi:
		emitRune(b, s[0].lo)
	default:
		if last := s[len(s)-1]; last.hi == maxRune && last.lo <= 0xFFFF {
			// 補集合の形 (末尾が 0x10FFFF まで続く) の集合は、regexp2 が「1 字以外」の集合として扱い、先頭が \A などの選択肢のあとに置くと
			// 後戻りを間違える ((?:\A|;)[^a] が ";ab" に当たらない)。0xFFFF で 2 つのクラスの選択肢に割ると、その扱いにならない
			head := append(rset{}, s[:len(s)-1]...)
			head = append(head, runeRange{last.lo, 0xFFFF})
			b.WriteString("(?:")
			emitClass(b, head)
			b.WriteByte('|')
			emitClass(b, rset{{0x10000, maxRune}})
			b.WriteByte(')')
			return
		}
		emitClass(b, s)
	}
}

func emitClass(b *strings.Builder, s rset) {
	b.WriteByte('[')
	for _, r := range s {
		emitRune(b, r.lo)
		if r.hi != r.lo {
			b.WriteByte('-')
			emitRune(b, r.hi)
		}
	}
	b.WriteByte(']')
}

func (e *emitter) seq(b *strings.Builder, s seq, flags int) *Error {
	for _, n := range s {
		if err := e.node(b, n, flags); err != nil {
			return err
		}
	}
	return nil
}

// atom は量指定子を付けられる 1 つのまとまりとして s を書く。
func (e *emitter) atom(b *strings.Builder, s seq, flags int) *Error {
	if len(s) == 1 {
		switch s[0].op {
		case opLiteral, opNotLiteral, opAny, opIn, opBranch, opAtomic, opGroupRef:
			return e.node(b, s[0], flags)
		case opSubpattern:
			if s[0].group != 0 {
				return e.node(b, s[0], flags)
			}
		default:
		}
	}
	b.WriteString("(?:")
	if err := e.seq(b, s, flags); err != nil {
		return err
	}
	b.WriteByte(')')
	return nil
}

func quantifier(n *node) string {
	switch {
	case n.min == 0 && n.max == maxRepeat:
		return "*"
	case n.min == 1 && n.max == maxRepeat:
		return "+"
	case n.min == 0 && n.max == 1:
		return "?"
	case n.max == maxRepeat:
		return "{" + strconv.Itoa(n.min) + ",}"
	case n.min == n.max:
		return "{" + strconv.Itoa(n.min) + "}"
	default:
		return "{" + strconv.Itoa(n.min) + "," + strconv.Itoa(n.max) + "}"
	}
}

func (e *emitter) node(b *strings.Builder, n *node, flags int) *Error {
	rules := rulesFor(flags)
	ignore := flags&IGNORECASE != 0
	switch n.op {
	case opLiteral:
		if ignore {
			emitSet(b, rules.literalSet(n.r))
		} else {
			emitRune(b, n.r)
		}
	case opNotLiteral:
		set := single(n.r)
		if ignore {
			set = rules.literalSet(n.r)
		}
		emitSet(b, complement(set))
	case opAny:
		if flags&DOTALL != 0 {
			emitSet(b, rangeSet(0, maxRune))
		} else {
			emitSet(b, complement(single('\n')))
		}
	case opIn:
		emitSet(b, classSet(n.items, n.negate, ignore, rules))
	case opAt:
		e.at(b, n.at, flags)
	case opSubpattern:
		return e.subpattern(b, n, flags)
	case opBranch:
		b.WriteString("(?:")
		for i, alt := range n.alts {
			if i > 0 {
				b.WriteByte('|')
			}
			if err := e.seq(b, alt, flags); err != nil {
				return err
			}
		}
		b.WriteByte(')')
	case opRepeat:
		return e.repeat(b, n, flags)
	case opAssert, opAssertNot:
		return e.assertion(b, n, flags)
	case opFailure:
		b.WriteString("(?!)")
	case opGroupRef:
		ref := `\k<` + strconv.Itoa(n.ref) + `>`
		if ignore {
			ref = "(?i:" + ref + ")"
		}
		b.WriteString(ref)
	case opGroupRefExists:
		b.WriteString("(?(" + strconv.Itoa(n.ref) + ")")
		if err := e.seq(b, n.yes, flags); err != nil {
			return err
		}
		if n.hasNo {
			b.WriteByte('|')
			if err := e.seq(b, n.no, flags); err != nil {
				return err
			}
		}
		b.WriteByte(')')
	case opAtomic:
		b.WriteString("(?>")
		if err := e.seq(b, n.sub, flags); err != nil {
			return err
		}
		b.WriteByte(')')
	}
	return nil
}

func (e *emitter) subpattern(b *strings.Builder, n *node, flags int) *Error {
	if n.group != 0 {
		b.WriteByte('(')
	} else {
		b.WriteString("(?:")
	}
	if err := e.seq(b, n.sub, combineFlags(flags, n.addFlags, n.delFlags)); err != nil {
		return err
	}
	b.WriteByte(')')
	return nil
}

func (e *emitter) repeat(b *strings.Builder, n *node, flags int) *Error {
	if n.rkind == repeatPossessive {
		b.WriteString("(?>")
	}
	if err := e.atom(b, n.item, flags); err != nil {
		return err
	}
	b.WriteString(quantifier(n))
	switch n.rkind {
	case repeatLazy:
		b.WriteByte('?')
	case repeatPossessive:
		b.WriteByte(')')
	case repeatGreedy:
	}
	return nil
}

func (e *emitter) assertion(b *strings.Builder, n *node, flags int) *Error {
	if n.dir < 0 {
		w := e.st.getWidth(n.sub)
		if w.lo != w.hi {
			return newError(ClassError, "look-behind requires fixed-width pattern")
		}
	}
	open := map[[2]bool]string{{true, false}: "(?=", {true, true}: "(?!", {false, false}: "(?<=", {false, true}: "(?<!"}
	b.WriteString(open[[2]bool{n.dir > 0, n.op == opAssertNot}])
	if err := e.seq(b, n.sub, flags); err != nil {
		return err
	}
	b.WriteByte(')')
	return nil
}

func (e *emitter) at(b *strings.Builder, k atKind, flags int) {
	multiline := flags&MULTILINE != 0
	switch k {
	case atBeginning:
		if multiline {
			b.WriteString(`(?<![^\n])`)
		} else {
			b.WriteString(`\A`)
		}
	case atEnd:
		if multiline {
			b.WriteString(`(?![^\n])`)
		} else {
			b.WriteString(`(?=\n?\z)`)
		}
	case atBeginningString:
		b.WriteString(`\A`)
	case atEndString:
		b.WriteString(`\z`)
	case atBoundary, atNonBoundary:
		var w strings.Builder
		emitSet(&w, categorySet(catWord, flags&UNICODE == 0))
		word := w.String()
		if k == atBoundary {
			b.WriteString("(?:(?<=" + word + ")(?!" + word + ")|(?<!" + word + ")(?=" + word + "))")
		} else {
			b.WriteString("(?:(?<=" + word + ")(?=" + word + ")|(?<!" + word + ")(?!" + word + "))")
		}
	}
}
