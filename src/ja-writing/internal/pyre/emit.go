package pyre

import (
	"strconv"
	"strings"
)

// 構文木を regexp2 のパターンの文字列に直す。フラグは _compiler の _compile と同じく、部分パターンごとの (?i:...) などで変わる。

type emitter struct {
	st *state
	// explicit は捕獲グループを (?<N>...) と番号つきで書く。同じグループを 2 か所に書く (unrollRepeat) ときの印で、
	// regexp2 は同じ番号のグループを 1 つのグループにまとめる
	explicit bool
	// pastEnd は pos > endpos の match (開始位置が末尾より後ろ) 用に書く。本家は 1 字を読む繰り返し (_simple な本体) と、末尾の位置の判定を満たせない。
	pastEnd bool
}

// simpleBody は re._compiler._simple。1 字だけを読む本体の繰り返しは、本家では専用の命令 (REPEAT_ONE) で動く。
func simpleBody(s seq) bool {
	if len(s) != 1 {
		return false
	}
	switch n := s[0]; n.op {
	case opSubpattern:
		return n.group == 0 && simpleBody(n.sub)
	case opAny, opIn, opLiteral, opNotLiteral:
		return true
	default:
		return false
	}
}

// unrollRepeat は、空になれる本体の {m,n} (m >= 1・n は有限で m より大きい) か。
// Python の sre は m 回の繰り返しの直後に、本体がそこまで空だったかにかかわらず、もう 1 回の繰り返しを必ず試す (その後は直前が空なら止める)。
// regexp2 は直前の繰り返しが空なら m 回の直後でも止めるので、groups の値が違う ((a*?){1,2} を "a" に fullmatch すると group 1 は Python が (0,1))。
// {m} と {0,n-m} に割ると、後ろの {0,n-m} の最初の繰り返しが必ず試される
func (e *emitter) unrollRepeat(n *node) bool {
	return n.rkind != repeatPossessive && n.min >= 1 && n.max != maxRepeat && n.max > n.min && e.st.getWidth(n.item).lo == 0
}

// needsExplicit は unrollRepeat で割る繰り返しの本体に捕獲グループがあるか。
func (e *emitter) needsExplicit(body seq) bool {
	found := false
	visit(body, func(n *node) {
		if n.op == opRepeat && e.unrollRepeat(n) {
			visit(n.item, func(c *node) { found = found || c.op == opSubpattern && c.group != 0 })
		}
	})
	return found
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
		{
			// 補集合の形 (ほぼ全部の字から 1 字だけ抜く集合を含む) は、regexp2 が「1 字以外」の集合として扱い、先頭が \A などの選択肢のあとに置くと
			// 後戻りを間違える ((?:\A|;)[^a] が ";ab" に当たらない)。[^😀] や [^\U0010ffff] のように抜く字が BMP の外でも同じ。
			// 0xFFFF で 2 つのクラスの選択肢に割ると、その扱いにならない
			var low, high rset
			for _, r := range s {
				if r.lo <= 0xFFFF {
					low = append(low, runeRange{r.lo, min(r.hi, 0xFFFF)})
				}
				if r.hi > 0xFFFF {
					high = append(high, runeRange{max(r.lo, 0x10000), r.hi})
				}
			}
			if len(low) > 0 && len(high) > 0 {
				b.WriteString("(?:")
				emitClass(b, low)
				b.WriteByte('|')
				emitClass(b, high)
				b.WriteByte(')')
				return
			}
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
	switch {
	case n.group != 0 && e.explicit:
		b.WriteString("(?<" + strconv.Itoa(n.group) + ">")
	case n.group != 0:
		b.WriteByte('(')
	default:
		b.WriteString("(?:")
	}
	if err := e.seq(b, n.sub, combineFlags(flags, n.addFlags, n.delFlags)); err != nil {
		return err
	}
	b.WriteByte(')')
	return nil
}

func (e *emitter) repeat(b *strings.Builder, n *node, flags int) *Error {
	if e.pastEnd && simpleBody(n.item) {
		b.WriteString("(?!)")
		return nil
	}
	if e.unrollRepeat(n) {
		return e.repeatUnrolled(b, n, flags)
	}
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

// repeatUnrolled は unrollRepeat の {m,n} を {m} と {0,n-m} の 2 つに割って書く。
func (e *emitter) repeatUnrolled(b *strings.Builder, n *node, flags int) *Error {
	forced := &node{op: opRepeat, item: n.item, min: n.min, max: n.min}
	optional := &node{op: opRepeat, item: n.item, min: 0, max: n.max - n.min, rkind: n.rkind}
	b.WriteString("(?:")
	for _, part := range []*node{forced, optional} {
		if err := e.atom(b, part.item, flags); err != nil {
			return err
		}
		b.WriteString(quantifier(part))
		if part == optional && n.rkind == repeatLazy {
			b.WriteByte('?')
		}
	}
	b.WriteByte(')')
	return nil
}

func (e *emitter) assertion(b *strings.Builder, n *node, flags int) *Error {
	if n.dir < 0 {
		w := e.st.getWidth(n.sub)
		if w.lo != w.hi {
			return newError(ClassError, "look-behind requires fixed-width pattern")
		}
	}
	if e.pastEnd && n.dir < 0 && e.st.getWidth(n.sub).lo > 0 {
		// 後読みの本体は ptr の直前の字を読むが、その字は endpos の外で、本家はそれを読めない (本体は必ず失敗する)
		if n.op == opAssertNot {
			b.WriteString("(?:)")
		} else {
			b.WriteString("(?!)")
		}
		return nil
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
	if e.pastEnd && (k == atEnd || k == atEndString) {
		b.WriteString("(?!)")
		return
	}
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
