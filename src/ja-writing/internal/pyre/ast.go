package pyre

// 構文木 (CPython の re._parser の SubPattern の写し)。

type opKind int

const (
	opLiteral opKind = iota
	opNotLiteral
	opAny
	opIn
	opAt
	opSubpattern
	opBranch
	opRepeat
	opAssert
	opAssertNot
	opFailure
	opGroupRef
	opGroupRefExists
	opAtomic
)

type atKind int

const (
	atBeginning atKind = iota
	atEnd
	atBeginningString
	atEndString
	atBoundary
	atNonBoundary
)

type category int

const (
	catDigit category = iota
	catNotDigit
	catSpace
	catNotSpace
	catWord
	catNotWord
)

type itemKind int

const (
	itemLiteral itemKind = iota
	itemRange
	itemCategory
)

// setItem は文字の集合の 1 つの要素 (字・範囲・\d などの分類)。
type setItem struct {
	kind   itemKind
	lo, hi rune
	cat    category
}

type repeatKind int

const (
	repeatGreedy repeatKind = iota
	repeatLazy
	repeatPossessive
)

type seq []*node

type node struct {
	op opKind
	r  rune // opLiteral / opNotLiteral

	items  []setItem // opIn
	negate bool

	at atKind

	group              int // opSubpattern: 捕獲するグループの番号 (捕獲しないなら 0)
	addFlags, delFlags int
	sub                seq // opSubpattern / opAtomic / opAssert / opAssertNot

	alts []seq // opBranch

	min, max int // opRepeat。max が maxRepeat なら上限なし
	rkind    repeatKind
	item     seq

	dir int // opAssert / opAssertNot: 1 は先読み、-1 は後読み

	ref   int // opGroupRef / opGroupRefExists
	yes   seq
	no    seq
	hasNo bool
}

// maxRepeat は繰り返しの上限なしを表す値 (_sre.MAXREPEAT)。
const maxRepeat = 1<<32 - 1

// maxGroups はグループの数の上限 (_sre.MAXGROUPS)。
const maxGroups = 1<<30 - 1

// maxWidth は後読みの幅の上限 (re._parser.MAXWIDTH)。
const maxWidth = 1 << 62

type width struct{ lo, hi int }

func satAdd(a, b int) int {
	if a >= maxWidth-b {
		return maxWidth
	}
	return a + b
}

func satMul(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	if a > maxWidth/b {
		return maxWidth
	}
	return a * b
}

// getWidth は SubPattern.getwidth の写し。後読みは断言の中身の幅が一定かを見る。
func (st *state) getWidth(s seq) width {
	lo, hi := 0, 0
	for _, n := range s {
		switch n.op {
		case opBranch:
			i, j := maxWidth, 0
			for _, alt := range n.alts {
				w := st.getWidth(alt)
				i, j = min(i, w.lo), max(j, w.hi)
			}
			lo, hi = satAdd(lo, i), satAdd(hi, j)
		case opAtomic:
			w := st.getWidth(n.sub)
			lo, hi = satAdd(lo, w.lo), satAdd(hi, w.hi)
		case opSubpattern:
			w := st.getWidth(n.sub)
			lo, hi = satAdd(lo, w.lo), satAdd(hi, w.hi)
		case opRepeat:
			w := st.getWidth(n.item)
			lo = satAdd(lo, satMul(w.lo, n.min))
			if n.max == maxRepeat && w.hi != 0 {
				hi = maxWidth
			} else {
				hi = satAdd(hi, satMul(w.hi, n.max))
			}
		case opLiteral, opNotLiteral, opAny, opIn:
			lo, hi = lo+1, hi+1
		case opGroupRef:
			w := st.groupWidths[n.ref]
			lo, hi = satAdd(lo, w.lo), satAdd(hi, w.hi)
		case opGroupRefExists:
			w := st.getWidth(n.yes)
			i, j := w.lo, w.hi
			if n.hasNo {
				w2 := st.getWidth(n.no)
				i, j = min(i, w2.lo), max(j, w2.hi)
			} else {
				i = 0
			}
			lo, hi = satAdd(lo, i), satAdd(hi, j)
		case opAt, opAssert, opAssertNot, opFailure:
		}
	}
	return width{min(lo, maxWidth), min(hi, maxWidth)}
}

// visit は s の全部の節点 (入れ子の中も) を、親より先に fn へ渡す。
func visit(s seq, fn func(*node)) {
	for _, n := range s {
		fn(n)
		for _, child := range []seq{n.sub, n.item, n.yes, n.no} {
			visit(child, fn)
		}
		for _, alt := range n.alts {
			visit(alt, fn)
		}
	}
}
