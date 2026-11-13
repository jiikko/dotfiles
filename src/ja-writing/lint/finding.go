package lint

import (
	"ja-writing/internal/pyjson"
	"ja-writing/visibility"
)

// finding は候補 1 件 (本家の候補の dict)。公開の欄は --json の findings に出す。内部の欄 (本家の _ で始まる鍵) は、語の一覧で外すときと
// 点数・外す規則の判定、後から足す規則 (G17 が G13 の _objects と _heads を読む) のために持ち、出力には出さない。
//
// 内部の欄の型は、使う群 (G14 の WordTypes、G20 の Replacements・PatternMatches・FlagHits) が入るときに、その群が実際の使い方に合わせて直してよい。
// 並びは本家の dict・tuple の挿入の順で、map にしない (message と記録の並びになる)。
// スライスの欄は nil が「その欄を持たない」で、空でも持つなら空のスライス ([]string{}) を入れる (本家は pause_comma の _objects に空の tuple を持つ)。
type finding struct {
	Rule     string
	Line     int
	Severity string
	Message  string
	Snippet  string

	Words          []string
	WordTypes      []wordType
	Objects        []string
	Heads          bool
	Replacements   []wordPair
	PatternMatches []patternMatch
	FlagHits       []string
}

// wordType は calque の _word_types の 1 要素 (語, 型の番号)。
type wordType struct {
	Word   string
	Number int
}

// wordPair は _replacements の 1 要素 (使わない語, 使う語)。
type wordPair struct{ Word, Replacement string }

// patternMatch は _pattern_matches の 1 要素 (パターンの名前, その行で当たった文字列)。
type patternMatch struct {
	Name    string
	Strings []string
}

// newFinding は公開の 5 つの欄だけを持つ候補を作る。
func newFinding(rule string, line int, severity, message, snippet string) finding {
	return finding{Rule: rule, Line: line, Severity: severity, Message: message, Snippet: snippet}
}

// publicValue は _public_finding (内部の欄を除いた写し) を --json の値にする。鍵の順は本家の候補の dict の順。
func (f finding) publicValue() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("rule", f.Rule)
	o.Set("line", int64(f.Line))
	o.Set("severity", f.Severity)
	o.Set("message", f.Message)
	o.Set("snippet", f.Snippet)
	return o
}

// lintContext は lintText の引数のうち、群の検出が見るもの (CLI が正規化した種類と読者・語の一覧)。
type lintContext struct {
	docType *string
	reader  *string
	words   *visibility.Words
}

// docTypeName は種類の名前 (指定しないときは空。種類の名前に空のものは無い)。本家の `doc_type != "essay"` のような比べで、None と別の種類を同じ側に置く用。
func (c lintContext) docTypeName() string {
	if c.docType == nil {
		return ""
	}
	return *c.docType
}
