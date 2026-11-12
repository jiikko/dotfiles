package pyre

import (
	"slices"
	"strconv"
	"strings"

	"ja-writing/internal/pyunicode"
)

// CPython の re._parser の写し。エラーの文と位置は本家に合わせる。パースの途中の失敗は *Error を panic で投げ、parsePattern が受ける。

const (
	specialChars = ".\\[{()*+?^$|"
	repeatChars  = "*+?{"
	digits       = "0123456789"
	octDigits    = "01234567"
	hexDigits    = "0123456789abcdefABCDEF"
	asciiLetters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	whitespace   = " \t\n\r\v\f"
)

var flagLetters = map[rune]int{'i': IGNORECASE, 'L': LOCALE, 'm': MULTILINE, 's': DOTALL, 'x': VERBOSE, 'a': ASCII, 'u': UNICODE}

// token は字 1 つか、バックスラッシュとその次の字 (nil は入力の終わり)。
type token []rune

func (t token) is(r rune) bool     { return len(t) == 1 && t[0] == r }
func (t token) in(set string) bool { return len(t) == 1 && strings.ContainsRune(set, t[0]) }
func (t token) text() string       { return pyunicode.FromRunes(t) }
func (t token) isBackslash() bool  { return len(t) > 0 && t[0] == '\\' }

func runeIn(r rune, set string) bool { return strings.ContainsRune(set, r) }

// tokenizer は re._parser.Tokenizer。
type tokenizer struct {
	src   []rune
	index int
	next  token
}

func newTokenizer(src []rune) *tokenizer {
	t := &tokenizer{src: src}
	t.advance()
	return t
}

func (t *tokenizer) advance() {
	index := t.index
	if index >= len(t.src) {
		t.next = nil
		return
	}
	ch := t.src[index]
	if ch == '\\' {
		index++
		if index >= len(t.src) {
			panic(newPatternError("bad escape (end of pattern)", t.src, len(t.src)-1))
		}
		t.next = token{ch, t.src[index]}
	} else {
		t.next = token{ch}
	}
	t.index = index + 1
}

func (t *tokenizer) match(r rune) bool {
	if t.next.is(r) {
		t.advance()
		return true
	}
	return false
}

func (t *tokenizer) nextIn(set string) bool { return t.next != nil && t.next.in(set) }

func (t *tokenizer) get() token {
	this := t.next
	t.advance()
	return this
}

func (t *tokenizer) getWhile(n int, set string) string {
	var sb []rune
	for range n {
		if !t.nextIn(set) {
			break
		}
		sb = append(sb, t.next[0])
		t.advance()
	}
	return string(sb)
}

func (t *tokenizer) getUntil(terminator rune, name string) string {
	var result []rune
	for {
		c := t.next
		t.advance()
		if c == nil {
			if len(result) == 0 {
				panic(t.errorAt("missing "+name, 0))
			}
			panic(t.errorAt("missing "+string(terminator)+", unterminated name", len(result)))
		}
		if c.is(terminator) {
			if len(result) == 0 {
				panic(t.errorAt("missing "+name, 1))
			}
			break
		}
		result = append(result, c...)
	}
	return pyunicode.FromRunes(result)
}

func (t *tokenizer) tell() int { return t.index - len(t.next) }

func (t *tokenizer) seek(index int) {
	t.index = index
	t.advance()
}

func (t *tokenizer) errorAt(msg string, offset int) *Error {
	return newPatternError(msg, t.src, t.tell()-offset)
}

// pyRepr は Python の str の repr (簡易。単引用符で囲み、中に単引用符があって二重引用符が無ければ二重引用符で囲む)。
func pyRepr(s string) string {
	q := "'"
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		q = `"`
	}
	return q + strings.ReplaceAll(s, `\`, `\\`) + q
}

func runeLen(s string) int { return len(pyunicode.Runes(s)) }

// isIdentifier は str.isidentifier。
func isIdentifier(name string) bool {
	rs := pyunicode.Runes(name)
	if len(rs) == 0 || !pyunicode.IsIdentStart(rs[0]) {
		return false
	}
	return !slices.ContainsFunc(rs[1:], func(r rune) bool { return !pyunicode.IsIdentContinue(r) })
}

func (t *tokenizer) checkGroupName(name string, offset int) {
	if !isIdentifier(name) {
		panic(t.errorAt("bad character in group name "+pyRepr(name), runeLen(name)+offset))
	}
}

func isASCIIDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// state は re._parser.State。
type state struct {
	flags            int
	groupDict        map[string]int
	groupWidths      []width // 添字 0 は全体の分
	widthSet         []bool  // グループを閉じたか (閉じる前は参照できない)
	lookbehindGroups int     // -1 は None
	groupRefPos      map[int]int
}

func newState(flags int) *state {
	return &state{flags: flags, groupDict: map[string]int{}, groupWidths: []width{{}}, widthSet: []bool{false}, lookbehindGroups: -1, groupRefPos: map[int]int{}}
}

func (st *state) groups() int { return len(st.groupWidths) }

func (st *state) openGroup(name string, hasName bool) (int, *Error) {
	gid := st.groups()
	st.groupWidths = append(st.groupWidths, width{})
	st.widthSet = append(st.widthSet, false)
	if st.groups() > maxGroups {
		return 0, newError(ClassError, "too many groups")
	}
	if hasName {
		if ogid, ok := st.groupDict[name]; ok {
			return 0, newError(ClassError, "redefinition of group name "+pyRepr(name)+" as group "+strconv.Itoa(gid)+"; was group "+strconv.Itoa(ogid))
		}
		st.groupDict[name] = gid
	}
	return gid, nil
}

func (st *state) closeGroup(gid int, p seq) {
	st.groupWidths[gid] = st.getWidth(p)
	st.widthSet[gid] = true
}

func (st *state) checkGroup(gid int) bool { return gid < st.groups() && st.widthSet[gid] }

func (st *state) checkLookbehindGroup(gid int, src *tokenizer) {
	if st.lookbehindGroups >= 0 {
		if !st.checkGroup(gid) {
			panic(src.errorAt("cannot refer to an open group", 0))
		}
		if gid >= st.lookbehindGroups {
			panic(src.errorAt("cannot refer to group defined in the same lookbehind subpattern", 0))
		}
	}
}

var simpleEscapes = map[rune]rune{'a': 7, 'b': 8, 'f': 12, 'n': 10, 'r': 13, 't': 9, 'v': 11, '\\': '\\'}

var categoryEscapes = map[rune]category{'d': catDigit, 'D': catNotDigit, 's': catSpace, 'S': catNotSpace, 'w': catWord, 'W': catNotWord}

var atEscapes = map[rune]atKind{'A': atBeginningString, 'b': atBoundary, 'B': atNonBoundary, 'z': atEndString, 'Z': atEndString}

type escKind int

const (
	escLiteral escKind = iota
	escCategory
	escAt
	escGroupRef
)

// escapeResult は _escape / _class_escape の戻り値。
type escapeResult struct {
	kind  escKind
	r     rune
	cat   category
	at    atKind
	group int
}

func hexEscape(src *tokenizer, escape []rune, n int) rune {
	escape = append(escape, []rune(src.getWhile(n, hexDigits))...)
	if len(escape) != n+2 {
		panic(src.errorAt("incomplete escape "+string(escape), len(escape)))
	}
	v, _ := strconv.ParseUint(string(escape[2:]), 16, 32)
	return rune(v)
}

func unicodeEscape8(src *tokenizer, escape []rune) rune {
	escape = append(escape, []rune(src.getWhile(8, hexDigits))...)
	if len(escape) != 10 {
		panic(src.errorAt("incomplete escape "+string(escape), len(escape)))
	}
	v, _ := strconv.ParseUint(string(escape[2:]), 16, 64)
	if v > maxRune {
		// chr() の ValueError は _escape が受けて、同じ「bad escape」にする
		panic(src.errorAt("bad escape "+string(escape), len(escape)))
	}
	return rune(v)
}

// namedEscape は \N{名前}。Unicode の字の名前の表を持たないので、名前を引けないものとして断る (本家と揃わない範囲)。
func namedEscape(src *tokenizer) rune {
	if !src.match('{') {
		panic(src.errorAt("missing {", 0))
	}
	name := src.getUntil('}', "character name")
	panic(src.errorAt("undefined character name "+pyRepr(name)+" (\\N{...} is not supported)", runeLen(name)+len(`\N{}`)))
}

func octalEscape(src *tokenizer, escape []rune) rune {
	v, _ := strconv.ParseInt(string(escape[1:]), 8, 32)
	if v > 0o377 {
		panic(src.errorAt("octal escape value "+string(escape)+" outside of range 0-0o377", len(escape)))
	}
	return rune(v)
}

func classEscape(src *tokenizer, esc token) escapeResult {
	c := esc[1]
	if r, ok := simpleEscapes[c]; ok {
		return escapeResult{kind: escLiteral, r: r}
	}
	if cat, ok := categoryEscapes[c]; ok {
		return escapeResult{kind: escCategory, cat: cat}
	}
	escape := append([]rune(nil), esc...)
	switch {
	case c == 'x':
		return escapeResult{r: hexEscape(src, escape, 2)}
	case c == 'u':
		return escapeResult{r: hexEscape(src, escape, 4)}
	case c == 'U':
		return escapeResult{r: unicodeEscape8(src, escape)}
	case c == 'N':
		return escapeResult{r: namedEscape(src)}
	case runeIn(c, octDigits):
		escape = append(escape, []rune(src.getWhile(2, octDigits))...)
		return escapeResult{r: octalEscape(src, escape)}
	}
	if runeIn(c, digits) || runeIn(c, asciiLetters) {
		panic(src.errorAt("bad escape "+string(escape), len(escape)))
	}
	return escapeResult{r: c}
}

func parseEscape(src *tokenizer, esc token, st *state) escapeResult {
	c := esc[1]
	if cat, ok := categoryEscapes[c]; ok {
		return escapeResult{kind: escCategory, cat: cat}
	}
	if at, ok := atEscapes[c]; ok {
		return escapeResult{kind: escAt, at: at}
	}
	if r, ok := simpleEscapes[c]; ok {
		return escapeResult{r: r}
	}
	escape := append([]rune(nil), esc...)
	switch {
	case c == 'x':
		return escapeResult{r: hexEscape(src, escape, 2)}
	case c == 'u':
		return escapeResult{r: hexEscape(src, escape, 4)}
	case c == 'U':
		return escapeResult{r: unicodeEscape8(src, escape)}
	case c == 'N':
		return escapeResult{r: namedEscape(src)}
	case c == '0':
		escape = append(escape, []rune(src.getWhile(2, octDigits))...)
		v, _ := strconv.ParseInt(string(escape[1:]), 8, 32)
		return escapeResult{r: rune(v)}
	case runeIn(c, digits):
		if src.nextIn(digits) {
			escape = append(escape, src.get()[0])
			if runeIn(escape[1], octDigits) && runeIn(escape[2], octDigits) && src.nextIn(octDigits) {
				escape = append(escape, src.get()[0])
				return escapeResult{r: octalEscape(src, escape)}
			}
		}
		group, _ := strconv.Atoi(string(escape[1:]))
		if group < st.groups() {
			if !st.checkGroup(group) {
				panic(src.errorAt("cannot refer to an open group", len(escape)))
			}
			st.checkLookbehindGroup(group, src)
			return escapeResult{kind: escGroupRef, group: group}
		}
		panic(src.errorAt("invalid group reference "+strconv.Itoa(group), len(escape)-1))
	}
	if runeIn(c, asciiLetters) {
		panic(src.errorAt("bad escape "+string(escape), len(escape)))
	}
	return escapeResult{r: c}
}

func parseSub(src *tokenizer, st *state, verbose bool, nested int) seq {
	var items []seq
	for {
		items = append(items, parse(src, st, verbose, nested+1, nested == 0 && len(items) == 0))
		if !src.match('|') {
			break
		}
		if nested == 0 {
			verbose = st.flags&VERBOSE != 0
		}
	}
	if len(items) == 1 {
		return items[0]
	}
	return mergeBranch(items)
}

// sameNode は CPython の SubPattern の要素どうしの == (re._parser._parse_sub が共通の先頭を見つける比べ方)。
// 入れ子の SubPattern (捕獲しないグループを含む繰り返し・分岐・グループ) を持つ要素は別のオブジェクトなので、常に違うものとして扱う。
func sameNode(a, b *node) bool {
	if a.op != b.op {
		return false
	}
	switch a.op {
	case opLiteral, opNotLiteral:
		return a.r == b.r
	case opAny, opFailure:
		return true
	case opAt:
		return a.at == b.at
	case opIn:
		return a.negate == b.negate && slices.Equal(a.items, b.items)
	case opGroupRef:
		return a.ref == b.ref
	default:
		return false
	}
}

// mergeBranch は re._parser._parse_sub の分岐のまとめ方。全部の選択肢に共通の先頭の要素は分岐の外へ出し、
// 残りが 1 つずつの字か集合なら 1 つの集合にする。検索の最初の字の絞り込み (searchFilter) は、この形を前提に本家の _compile_info を写す。
func mergeBranch(items []seq) seq {
	var out seq
	for {
		var prefix *node
		same := true
		for _, it := range items {
			if len(it) == 0 || prefix != nil && !sameNode(it[0], prefix) {
				same = false
				break
			}
			if prefix == nil {
				prefix = it[0]
			}
		}
		if !same {
			break
		}
		for i := range items {
			items[i] = items[i][1:]
		}
		out = append(out, prefix)
	}
	var set []setItem
	for _, it := range items {
		switch {
		case len(it) != 1:
		case it[0].op == opLiteral:
			set = append(set, setItem{kind: itemLiteral, lo: it[0].r})
			continue
		case it[0].op == opIn && !it[0].negate:
			set = append(set, it[0].items...)
			continue
		}
		return append(out, &node{op: opBranch, alts: items})
	}
	return append(out, &node{op: opIn, items: uniqItems(set)})
}

func atNode(k atKind) *node { return &node{op: opAt, at: k} }

func parse(src *tokenizer, st *state, verbose bool, nested int, first bool) seq {
	var sp seq
	for {
		this := src.next
		if this == nil || this.in("|)") {
			break
		}
		src.get()

		if verbose {
			if this.in(whitespace) {
				continue
			}
			if this.is('#') {
				for {
					this = src.get()
					if this == nil || this.is('\n') {
						break
					}
				}
				continue
			}
		}

		switch {
		case this.isBackslash():
			res := parseEscape(src, this, st)
			switch res.kind {
			case escLiteral:
				sp = append(sp, &node{op: opLiteral, r: res.r})
			case escCategory:
				sp = append(sp, &node{op: opIn, items: []setItem{{kind: itemCategory, cat: res.cat}}})
			case escAt:
				sp = append(sp, atNode(res.at))
			case escGroupRef:
				sp = append(sp, &node{op: opGroupRef, ref: res.group})
			}
		case !this.in(specialChars):
			sp = append(sp, &node{op: opLiteral, r: this[0]})
		case this.is('['):
			sp = append(sp, parseSet(src))
		case this.in(repeatChars):
			sp = parseRepeat(src, this, sp)
		case this.is('.'):
			sp = append(sp, &node{op: opAny})
		case this.is('('):
			if n := parseGroup(src, st, verbose, nested, first && len(sp) == 0, &verbose); n != nil {
				sp = append(sp, n)
			}
		case this.is('^'):
			sp = append(sp, atNode(atBeginning))
		case this.is('$'):
			sp = append(sp, atNode(atEnd))
		default:
			panic(newError(ClassError, "unsupported special character"))
		}
	}
	// 捕獲しない・フラグの無い (?:...) の中身を親の列へ展開する
	for i := len(sp) - 1; i >= 0; i-- {
		n := sp[i]
		if n.op == opSubpattern && n.group == 0 && n.addFlags == 0 && n.delFlags == 0 {
			rest := append(append(seq{}, n.sub...), sp[i+1:]...)
			sp = append(sp[:i], rest...)
		}
	}
	return sp
}

func parseSet(src *tokenizer) *node {
	here := src.tell() - 1
	var set []setItem
	negate := src.match('^')
	for {
		this := src.get()
		if this == nil {
			panic(src.errorAt("unterminated character set", src.tell()-here))
		}
		if this.is(']') && len(set) > 0 {
			break
		}
		code1 := escapeResult{kind: escLiteral}
		if this.isBackslash() {
			code1 = classEscape(src, this)
		} else {
			code1.r = this[0]
		}
		if !src.match('-') {
			set = append(set, itemOf(code1))
			continue
		}
		that := src.get()
		if that == nil {
			panic(src.errorAt("unterminated character set", src.tell()-here))
		}
		if that.is(']') {
			set = append(set, itemOf(code1), setItem{kind: itemLiteral, lo: '-'})
			break
		}
		code2 := escapeResult{kind: escLiteral}
		if that.isBackslash() {
			code2 = classEscape(src, that)
		} else {
			code2.r = that[0]
		}
		bad := "bad character range " + this.text() + "-" + that.text()
		badLen := len(this) + 1 + len(that)
		if code1.kind != escLiteral || code2.kind != escLiteral || code2.r < code1.r {
			panic(src.errorAt(bad, badLen))
		}
		set = append(set, setItem{kind: itemRange, lo: code1.r, hi: code2.r})
	}
	set = uniqItems(set)
	if len(set) == 1 && set[0].kind == itemLiteral {
		if negate {
			return &node{op: opNotLiteral, r: set[0].lo}
		}
		return &node{op: opLiteral, r: set[0].lo}
	}
	return &node{op: opIn, items: set, negate: negate}
}

func itemOf(e escapeResult) setItem {
	if e.kind == escCategory {
		return setItem{kind: itemCategory, cat: e.cat}
	}
	return setItem{kind: itemLiteral, lo: e.r}
}

func uniqItems(items []setItem) []setItem {
	var out []setItem
	for _, it := range items {
		dup := false
		for _, o := range out {
			if o == it {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, it)
		}
	}
	return out
}

func parseRepeat(src *tokenizer, this token, sp seq) seq {
	here := src.tell()
	lo, hi := 0, maxRepeat
	switch {
	case this.is('?'):
		lo, hi = 0, 1
	case this.is('*'):
	case this.is('+'):
		lo = 1
	default: // '{'
		if src.next.is('}') {
			return append(sp, &node{op: opLiteral, r: '{'})
		}
		var loS, hiS []rune
		for src.nextIn(digits) {
			loS = append(loS, src.get()[0])
		}
		if src.match(',') {
			for src.nextIn(digits) {
				hiS = append(hiS, src.get()[0])
			}
		} else {
			hiS = loS
		}
		if !src.match('}') {
			sp = append(sp, &node{op: opLiteral, r: '{'})
			src.seek(here)
			return sp
		}
		if len(loS) > 0 {
			lo = repeatNumber(string(loS))
		}
		if len(hiS) > 0 {
			hi = repeatNumber(string(hiS))
			if hi < lo {
				panic(src.errorAt("min repeat greater than max repeat", src.tell()-here))
			}
		}
	}
	if len(sp) == 0 || sp[len(sp)-1].op == opAt {
		panic(src.errorAt("nothing to repeat", src.tell()-here+len(this)))
	}
	last := sp[len(sp)-1]
	if last.op == opRepeat {
		panic(src.errorAt("multiple repeat", src.tell()-here+len(this)))
	}
	item := seq{last}
	if last.op == opSubpattern && last.group == 0 && last.addFlags == 0 && last.delFlags == 0 {
		item = last.sub
	}
	kind := repeatGreedy
	switch {
	case src.match('?'):
		kind = repeatLazy
	case src.match('+'):
		kind = repeatPossessive
	}
	sp[len(sp)-1] = &node{op: opRepeat, min: lo, max: hi, rkind: kind, item: item}
	return sp
}

func repeatNumber(digits string) int {
	v, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || v >= maxRepeat {
		panic(newError(ClassOverflow, "the repetition number is too large"))
	}
	return int(v)
}

// parseGroup は '(' の続きを読む。nil を返すときは何も足さない (コメント・グローバルなフラグ)。
// atStart は式の先頭か (グローバルなフラグを許す位置か)。verbose はグローバルなフラグで変わったら書き換える。
func parseGroup(src *tokenizer, st *state, verbose bool, nested int, atStart bool, verboseOut *bool) *node {
	start := src.tell() - 1
	capture := true
	atomic := false
	name, hasName := "", false
	addFlags, delFlags := 0, 0
	if src.match('?') {
		char := src.get()
		if char == nil {
			panic(src.errorAt("unexpected end of pattern", 0))
		}
		switch {
		case char.is('P'):
			switch {
			case src.match('<'):
				name = src.getUntil('>', "group name")
				hasName = true
				src.checkGroupName(name, 1)
			case src.match('='):
				return parseNamedBackref(src, st)
			default:
				c := src.get()
				if c == nil {
					panic(src.errorAt("unexpected end of pattern", 0))
				}
				panic(src.errorAt("unknown extension ?P"+c.text(), len(c)+2))
			}
		case char.is(':'):
			capture = false
		case char.is('#'):
			for {
				if src.next == nil {
					panic(src.errorAt("missing ), unterminated comment", src.tell()-start))
				}
				if src.get().is(')') {
					break
				}
			}
			return nil
		case char.in("=!<"):
			return parseLook(src, st, verbose, nested, char, start)
		case char.is('('):
			return parseConditional(src, st, verbose, nested, start)
		case char.is('>'):
			capture = false
			atomic = true
		case char.is('-') || len(char) == 1 && flagLetters[char[0]] != 0:
			add, del, global := parseFlags(src, st, char)
			if global {
				if !atStart {
					panic(src.errorAt("global flags not at the start of the expression", src.tell()-start))
				}
				*verboseOut = st.flags&VERBOSE != 0
				return nil
			}
			addFlags, delFlags = add, del
			capture = false
		default:
			panic(src.errorAt("unknown extension ?"+char.text(), len(char)+1))
		}
	}
	group := 0
	if capture {
		g, err := st.openGroup(name, hasName)
		if err != nil {
			panic(src.errorAt(err.Msg, runeLen(name)+1))
		}
		group = g
	}
	subVerbose := (verbose || addFlags&VERBOSE != 0) && delFlags&VERBOSE == 0
	p := parseSub(src, st, subVerbose, nested+1)
	if !src.match(')') {
		panic(src.errorAt("missing ), unterminated subpattern", src.tell()-start))
	}
	if group != 0 {
		st.closeGroup(group, p)
	}
	if atomic {
		return &node{op: opAtomic, sub: p}
	}
	return &node{op: opSubpattern, group: group, addFlags: addFlags, delFlags: delFlags, sub: p}
}

func parseNamedBackref(src *tokenizer, st *state) *node {
	name := src.getUntil(')', "group name")
	src.checkGroupName(name, 1)
	gid, ok := st.groupDict[name]
	if !ok {
		panic(src.errorAt("unknown group name "+pyRepr(name), runeLen(name)+1))
	}
	if !st.checkGroup(gid) {
		panic(src.errorAt("cannot refer to an open group", runeLen(name)+1))
	}
	st.checkLookbehindGroup(gid, src)
	return &node{op: opGroupRef, ref: gid}
}

func parseLook(src *tokenizer, st *state, verbose bool, nested int, char token, start int) *node {
	dir := 1
	prev := -1
	if char.is('<') {
		char = src.get()
		if char == nil {
			panic(src.errorAt("unexpected end of pattern", 0))
		}
		if !char.in("=!") {
			panic(src.errorAt("unknown extension ?<"+char.text(), len(char)+2))
		}
		dir = -1
		prev = st.lookbehindGroups
		if prev < 0 {
			st.lookbehindGroups = st.groups()
		}
	}
	p := parseSub(src, st, verbose, nested+1)
	if dir < 0 && prev < 0 {
		st.lookbehindGroups = -1
	}
	if !src.match(')') {
		panic(src.errorAt("missing ), unterminated subpattern", src.tell()-start))
	}
	switch {
	case char.is('='):
		return &node{op: opAssert, dir: dir, sub: p}
	case len(p) > 0:
		return &node{op: opAssertNot, dir: dir, sub: p}
	default:
		return &node{op: opFailure}
	}
}

func parseConditional(src *tokenizer, st *state, verbose bool, nested int, start int) *node {
	condName := src.getUntil(')', "group name")
	var condGroup int
	if !isASCIIDecimal(condName) {
		src.checkGroupName(condName, 1)
		g, ok := st.groupDict[condName]
		if !ok {
			panic(src.errorAt("unknown group name "+pyRepr(condName), runeLen(condName)+1))
		}
		condGroup = g
	} else {
		v, err := strconv.Atoi(condName)
		if err != nil || v >= maxGroups {
			panic(src.errorAt("invalid group reference "+condName, len(condName)+1))
		}
		condGroup = v
		if condGroup == 0 {
			panic(src.errorAt("bad group number", len(condName)+1))
		}
		if _, ok := st.groupRefPos[condGroup]; !ok {
			st.groupRefPos[condGroup] = src.tell() - len(condName) - 1
		}
	}
	st.checkLookbehindGroup(condGroup, src)
	n := &node{op: opGroupRefExists, ref: condGroup, yes: parse(src, st, verbose, nested+1, false)}
	if src.match('|') {
		n.no = parse(src, st, verbose, nested+1, false)
		n.hasNo = true
		if src.next.is('|') {
			panic(src.errorAt("conditional backref with more than two branches", 0))
		}
	}
	if !src.match(')') {
		panic(src.errorAt("missing ), unterminated subpattern", src.tell()-start))
	}
	return n
}

func flagError(src *tokenizer, next token, fallback string) *Error {
	msg := fallback
	if len(next) == 1 && pyunicode.IsAlpha(next[0]) {
		msg = "unknown flag"
	}
	return src.errorAt(msg, len(next))
}

// parseFlags は (?aiLmsux-...) を読む。global は (?i) のようにグローバルなフラグを立てたとき。
func parseFlags(src *tokenizer, st *state, char token) (add, del int, global bool) {
	c := char[0]
	if c != '-' {
		for {
			flag := flagLetters[c]
			if c == 'L' {
				panic(src.errorAt("bad inline flags: cannot use 'L' flag with a str pattern", 0))
			}
			add |= flag
			if flag&typeFlags != 0 && add&typeFlags != flag {
				panic(src.errorAt("bad inline flags: flags 'a', 'u' and 'L' are incompatible", 0))
			}
			next := src.get()
			if next == nil {
				panic(src.errorAt("missing -, : or )", 0))
			}
			if next.in(")-:") {
				c = next[0]
				break
			}
			if len(next) != 1 || flagLetters[next[0]] == 0 {
				panic(flagError(src, next, "missing -, : or )"))
			}
			c = next[0]
		}
	}
	if c == ')' {
		st.flags |= add
		return 0, 0, true
	}
	if add&DEBUG != 0 {
		panic(src.errorAt("bad inline flags: cannot turn on global flag", 1))
	}
	if c == '-' {
		next := src.get()
		if next == nil {
			panic(src.errorAt("missing flag", 0))
		}
		if len(next) != 1 || flagLetters[next[0]] == 0 {
			panic(flagError(src, next, "missing flag"))
		}
		c = next[0]
		for {
			flag := flagLetters[c]
			if flag&typeFlags != 0 {
				panic(src.errorAt("bad inline flags: cannot turn off flags 'a', 'u' and 'L'", 0))
			}
			del |= flag
			next = src.get()
			if next == nil {
				panic(src.errorAt("missing :", 0))
			}
			if next.is(':') {
				break
			}
			if len(next) != 1 || flagLetters[next[0]] == 0 {
				panic(flagError(src, next, "missing :"))
			}
			c = next[0]
		}
	}
	if del&DEBUG != 0 {
		panic(src.errorAt("bad inline flags: cannot turn off global flag", 1))
	}
	if add&del != 0 {
		panic(src.errorAt("bad inline flags: flag turned on and off", 1))
	}
	return add, del, false
}

// fixFlags は re._parser.fix_flags (str のパターン)。
func fixFlags(flags int) (int, *Error) {
	if flags&LOCALE != 0 {
		return 0, newError(ClassValue, "cannot use LOCALE flag with a str pattern")
	}
	if flags&ASCII == 0 {
		flags |= UNICODE
	} else if flags&UNICODE != 0 {
		return 0, newError(ClassValue, "ASCII and UNICODE flags are incompatible")
	}
	return flags, nil
}

type parsed struct {
	body   seq
	state  *state
	flags  int
	groups int
}

// parsePattern は re._parser.parse。
func parsePattern(src []rune, flags int) (res *parsed, err *Error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			res, err = nil, e
		}
	}()
	tk := newTokenizer(src)
	st := newState(flags)
	body := parseSub(tk, st, flags&VERBOSE != 0, 0)
	fixed, ferr := fixFlags(st.flags)
	if ferr != nil {
		return nil, ferr
	}
	st.flags = fixed
	if tk.next != nil {
		return nil, tk.errorAt("unbalanced parenthesis", 0)
	}
	for g, pos := range st.groupRefPos {
		if g >= st.groups() {
			return nil, newPatternError("invalid group reference "+strconv.Itoa(g), src, pos)
		}
	}
	return &parsed{body: body, state: st, flags: fixed, groups: st.groups() - 1}, nil
}
