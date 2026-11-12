package pyre

import "strings"

// search の探索の開始位置の絞り込み。CPython は先頭が文字の集合のパターンの INFO の文字の集合を、パターンの中の (?a:...) などではなく
// 全体のフラグで compile する (re._compiler._compile_info)。\W などの分類は全体のフラグの規則で引かれ、
// その字で始まらない位置は search が試さない ((?a:\W) は "É" に当たらない)。match / fullmatch は INFO の集合を見ない。
// 分類を含まない集合・単独の字・字の選択肢は全体のフラグでも結果が変わらないので、絞り込みを足さない。

// literalPrefixLen は re._compiler._get_literal_prefix の長さと、先頭から全部が字だったか。
func literalPrefixLen(s seq, flags int) (n int, gotAll bool) {
	for _, nd := range s {
		switch nd.op {
		case opLiteral:
			if flags&IGNORECASE != 0 && rulesFor(flags).isCased(nd.r) {
				return n, false
			}
			n++
		case opSubpattern:
			n1, all := literalPrefixLen(nd.sub, combineFlags(flags, nd.addFlags, nd.delFlags))
			n += n1
			if !all {
				return n, false
			}
		default:
			return n, false
		}
	}
	return n, true
}

// searchFilter は search が開始位置の字に課す先読み (regexp2 のパターンの断片)。絞り込みが結果を変えないときは空。
func (st *state) searchFilter(body seq, flags int) string {
	if st.getWidth(body).lo == 0 {
		return ""
	}
	if n, _ := literalPrefixLen(body, flags); n > 0 {
		return ""
	}
	cur, f := body, flags
	for {
		if len(cur) == 0 {
			return ""
		}
		if cur[0].op != opSubpattern {
			break
		}
		f = combineFlags(f, cur[0].addFlags, cur[0].delFlags)
		cur = cur[0].sub
	}
	first := cur[0]
	if first.op != opIn {
		return ""
	}
	hasCategory := false
	rules := rulesFor(f)
	for _, it := range first.items {
		switch it.kind {
		case itemCategory:
			hasCategory = true
		case itemLiteral:
			if f&IGNORECASE != 0 && rules.isCased(it.lo) {
				return ""
			}
		case itemRange:
			if f&IGNORECASE != 0 && (it.hi > 0xFFFF || rules.anyCased(it.lo, it.hi)) {
				return ""
			}
		}
	}
	if !hasCategory {
		return ""
	}
	var b strings.Builder
	b.WriteString("(?=")
	emitSet(&b, classSet(first.items, first.negate, false, rulesFor(flags)))
	b.WriteByte(')')
	return b.String()
}
