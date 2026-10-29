// Package swift は、Swift のソースを tree-sitter で読み、比べる単位 (関数・初期化子・クロージャなど) を言語に依らない木に写す adapter。
package swift

import (
	"bytes"
	"fmt"
	"path"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	swiftgrammar "github.com/alex-pinkus/tree-sitter-swift/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"

	"dupscan/internal/lang"
	"dupscan/internal/tree"
)

// Extensions は adapter が読むファイルの拡張子。
var Extensions = []string{".swift"}

// Adapter は Swift の読み方。
var Adapter = lang.Adapter{Name: "swift", Extensions: Extensions, IsTestPath: IsTestPath, NewParser: newLangParser, Symbols: true}

func newLangParser(o lang.Options) (lang.Parser, error) {
	p, err := NewParser()
	if err != nil {
		return nil, err
	}
	p.CollectCalls, p.CollectDefs, p.CollectRefs = o.Calls, o.Defs, o.Refs
	p.MetadataOnly = o.MetadataOnly
	return p, nil
}

// Language は文法を返す。
func Language() *sitter.Language { return sitter.NewLanguage(swiftgrammar.Language()) }

// kindNames は文法の節の種類の名前を番号で引く表。`Node.Kind()` は呼ぶたびに C の文字列を Go の文字列へ写して確保するので、
// 全部の節を何度も見る walker では番号 (KindId) から引く。
var kindNames = sync.OnceValue(func() []string {
	l := Language()
	names := make([]string, l.NodeKindCount())
	for id := range names {
		names[id] = l.NodeKindForId(uint16(id))
	}
	return names
})

// kindOf は n の種類の名前 (`n.Kind()` と同じ値)。
func kindOf(n *sitter.Node) string {
	names := kindNames()
	if id := int(n.KindId()); id < len(names) {
		return names[id]
	}
	return n.Kind()
}

// Parser はファイルを読んで単位を取り出す。goroutine ごとに 1 つ作る (tree-sitter の parser は共有できない)。
type Parser struct {
	MetadataOnly bool
	p            *sitter.Parser
	// CollectCalls は呼び出しの箇所も集める (index と callers だけが使う。全部の節をもう 1 度歩くので、要らないときは集めない)。
	CollectCalls bool
	// CollectDefs は定義と型宣言を別に集める (重複の比較単位にはしない)。
	CollectDefs bool
	// CollectRefs は型・式の位置にある識別子の出現を集める。
	CollectRefs bool
}

// NewParser は Parser を作る。
func NewParser() (*Parser, error) {
	p := sitter.NewParser()
	if err := p.SetLanguage(Language()); err != nil {
		p.Close()
		return nil, err
	}
	return &Parser{p: p}, nil
}

// Close は parser を閉じる。
func (p *Parser) Close() { p.p.Close() }

// Parse は file (repo の root からの相対) の src から単位を取り出す。テスト関数そのものとその中のクロージャは単位にしない。
func (p *Parser) Parse(file string, src []byte) (lang.FileResult, error) {
	t := p.p.Parse(src, nil)
	if t == nil {
		return lang.FileResult{}, fmt.Errorf("%s を parse できない", file)
	}
	if t.RootNode().HasError() {
		t = p.reparseKnownMisparses(src, t)
	}
	defer t.Close()
	root := t.RootNode()
	w := &walker{metadataOnly: p.MetadataOnly, file: file, src: src, test: IsTestPath(file), closures: map[string]int{}, seen: map[string]int{}, isUnit: map[string]bool{}}
	w.walk(root, ctx{})
	r := lang.FileResult{Units: w.units, HasError: root.HasError()}
	if p.CollectCalls {
		r.Calls = w.calls(root)
	}
	if p.CollectDefs {
		r.Definitions, r.Types = w.definitions(root)
	}
	if p.CollectRefs {
		r.References = w.references(root)
	}
	return r, nil
}

// isolationModifier は、文法 (tree-sitter-swift 0.7.4 と、その後の main でも同じ) が関数の中の `nonisolated(unsafe) let` を
// 呼び出し式 + 別の文として読み、その関数の本体を ERROR にする書き方。ERROR 節の中の出現を同じ長さの空白にする (落ちるのは修飾子の語だけ)。
const isolationModifier = "nonisolated(unsafe)"

// 空のタプルを渡す呼び出し `f(())` (`continuation.yield(())` / `send(())`) は、文法が内側の `()` に長さ 0 の欠けた節 (`!`) を置く。
// 欠けた節が、ラベルの無い唯一の引数の空のタプルの中 (isSoleEmptyTupleArgument) にあるときだけ、その `()` を空白にする
// (引数の無い呼び出しとして比べる。`f(())` と `f( ())` と `f()` は同じ形になる)。
//   - 節の形で確かめる: 字面 `(())` で照合すると、空白・改行だけが違う `f( ())` が外れて別の規則 (repairEmptyTuple の `[]`) に落ち、
//     引数 1 つの別の形になる。欠けた節の前後に広げた範囲で探すと、別の関数の型 `(())` や壊れた宣言 `g(())` まで書き換える
//   - 引数の中に限る: 代入の右辺 `let y = (())` やパターン `case (()):` も同じ欠けた節を持つが、ここでは空白にしない (repairEmptyTuple の対象)

// reparseKnownMisparses は、ERROR のあるファイルについて、既知の読み違えの書き方の出現だけを同じ長さの字面に置き換えて読み直した木を返し、
// ERROR の節が減ったときだけそれを採る (採らなければ t をそのまま返す)。置き換えは節の位置を変えないので、walker には元の src を渡したまま使える。
//
// 置き換えを読み違えの箇所に限るのは、ファイル全体を置き換えると、正常に読めていた箇所 (メンバー宣言の修飾子・文字列リテラルの中の同じ字面・
// 式文・型) まで書き換わり、未変更の単位のハッシュが揺れる / 新しい ERROR を作る / 別の ERROR を隠すため。
// 文法側が直ったら不要になる: 直った版では最初の parse に ERROR が無く、ここへ来ない。
func (p *Parser) reparseKnownMisparses(src []byte, t *sitter.Tree) *sitter.Tree {
	fixed := bytes.Clone(src)
	changed := false
	var attributeBlanks [][2]uint
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if !n.HasError() {
			return
		}
		if repairConditionalAttributes(n, fixed, &attributeBlanks) {
			changed = true
		}
		if repairConditionOperators(n, fixed) {
			changed = true
		}
		if repairFinalAssignment(n, fixed) {
			changed = true
		}
		if repairClosureSemicolon(n, fixed) {
			changed = true
		}
		if n.IsError() {
			s, e := int(n.StartByte()), int(n.EndByte())
			for off := s; ; {
				k := bytes.Index(fixed[off:e], []byte(isolationModifier))
				if k < 0 {
					break
				}
				at := off + k
				copy(fixed[at:], bytes.Repeat([]byte(" "), len(isolationModifier)))
				changed = true
				off = at + len(isolationModifier)
			}
		}
		if n.IsMissing() {
			if isSoleEmptyTupleArgument(n) {
				tuple := n.Parent().Parent()
				copy(fixed[tuple.StartByte():], "  ")
				changed = true
			} else if repairEmptyTuple(n, fixed) {
				changed = true
			}
			return
		}
		for i := range n.ChildCount() {
			visit(n.Child(i))
		}
	}
	visit(t.RootNode())
	if !changed {
		return t
	}
	t2 := p.p.Parse(fixed, nil)
	if t2 == nil {
		return t
	}
	if countErrors(t2.RootNode()) >= countErrors(t.RootNode()) || !preservesTopLevel(t.RootNode(), t2.RootNode(), attributeBlanks...) {
		t2.Close()
		return t
	}
	t.Close()
	return t2
}

// preservesTopLevel は I5: 元の source_file 直下の正常な子を、範囲・kind・正常性まで保つ。
// B が空白化した範囲に全体が収まる子だけを除く。他の directive まで除くと、
// ERROR の減少との相殺で正常な条件分岐を飲み込む候補を採用してしまう。
func preservesTopLevel(before, after *sitter.Node, attributeBlanks ...[2]uint) bool {
	if kindOf(before) != "source_file" {
		return true
	}
	j := uint(0)
	for i := range before.ChildCount() {
		old := before.Child(i)
		blanked := false
		for _, span := range attributeBlanks {
			if span[0] <= old.StartByte() && old.EndByte() <= span[1] {
				blanked = true
			}
		}
		if old.HasError() || blanked {
			continue
		}
		for j < after.ChildCount() && after.Child(j).StartByte() < old.StartByte() {
			j++
		}
		if j == after.ChildCount() {
			return false
		}
		n := after.Child(j)
		if n.StartByte() != old.StartByte() || n.EndByte() != old.EndByte() || n.KindId() != old.KindId() || n.HasError() {
			return false
		}
	}
	return true
}

// A: 欠けた ! → bang → tuple_expression の、ちょうど () の中だけを [] にする。
// 先に既存の (()) 引数規則を試すので g(()) の形は変えない。範囲検索は型やパターンまで拾うため使わない。
func repairEmptyTuple(n *sitter.Node, fixed []byte) bool {
	if !n.IsMissing() || kindOf(n) != "!" {
		return false
	}
	bang := n.Parent()
	if bang == nil || kindOf(bang) != "bang" {
		return false
	}
	tuple := bang.Parent()
	if tuple == nil || kindOf(tuple) != "tuple_expression" || tuple.EndByte()-tuple.StartByte() != 2 || n.StartByte() != tuple.StartByte()+1 || tuple.Utf8Text(fixed) != "()" {
		return false
	}
	copy(fixed[tuple.StartByte():], "[]")
	return true
}

// B: 宣言の modifiers が属性・ERROR(#else)・属性だけから成り、直後に
// ERROR(#endif)、直前の兄弟に directive(#if) がある一段の分岐を対象にする。
// 最初の属性群だけを残し、区切りと後半を ASCII 空白にする。区切りだけを
// 消すと排他的な属性が同じ宣言に付く。範囲検索は宣言・入れ子・文字列中の
// #if まで拾うため行わない。source_file と class_body 直下で同じ条件を使う。
// 直前行にコメントがある形は、空白化で宣言との関連が切れるため残す。
// 両分岐の Test 属性の有無が異なる形も、比較単位からの除外が変わるため残す。
func repairConditionalAttributes(n *sitter.Node, fixed []byte, blanks *[][2]uint) bool {
	if !n.HasError() || kindOf(n) != "function_declaration" || n.ChildCount() < 3 {
		return false
	}
	if parent := n.Parent(); parent == nil || (kindOf(parent) != "class_body" && kindOf(parent) != "source_file") {
		return false
	}
	mods, end := n.Child(0), n.Child(1)
	start := n.PrevSibling()
	if kindOf(mods) != "modifiers" || !end.IsError() || strings.TrimSpace(end.Utf8Text(fixed)) != "#endif" || start == nil || kindOf(start) != "directive" || !hasToken(start, "#if") {
		return false
	}
	if prev := start.PrevSibling(); prev != nil && isComment(prev) && prev.EndPosition().Row+1 >= start.StartPosition().Row {
		return false
	}
	// 先行する未閉鎖の #if があれば、この分岐は入れ子なので扱わない。見るのは同じ親の兄弟だけで、型を囲む外側の
	// `#if OUTER\nstruct S {…}\n#endif` は見ない。そこで発火しても、属性の分岐は 1 段で閉じていて外側の directive は残るので、
	// 最初の分岐を残した木は同じ規則の結果と変わらない。外側の分岐で誤った木・L1・Line になる入力が示されたら判定を広げる。
	depth := 0
	for c := start.PrevSibling(); c != nil; c = c.PrevSibling() {
		if kindOf(c) != "directive" {
			continue
		}
		if hasToken(c, "#endif") {
			depth++
		}
		if hasToken(c, "#if") {
			if depth == 0 {
				return false
			}
			depth--
		}
	}
	var other *sitter.Node
	before, after := 0, 0
	beforeTest, afterTest := false, false
	for i := range mods.ChildCount() {
		c := mods.Child(i)
		if c.IsError() && strings.TrimSpace(c.Utf8Text(fixed)) == "#else" && other == nil {
			other = c
		} else if kindOf(c) == "attribute" && !c.HasError() {
			if other == nil {
				before++
				beforeTest = beforeTest || isTestAttribute(c, fixed)
			} else {
				after++
				afterTest = afterTest || isTestAttribute(c, fixed)
			}
		} else {
			return false
		}
	}
	if other == nil || before == 0 || after == 0 || beforeTest != afterTest {
		return false
	}
	// 節間に未解析の字句がある場合も触らない。
	if len(bytes.TrimSpace(fixed[start.EndByte():mods.StartByte()])) != 0 || len(bytes.TrimSpace(fixed[mods.EndByte():end.StartByte()])) != 0 {
		return false
	}
	spans := [][2]uint{{start.StartByte(), start.EndByte()}, {other.StartByte(), end.EndByte()}}
	for _, span := range spans {
		for _, b := range fixed[span[0]:span[1]] {
			if b >= 128 {
				return false
			}
		}
	}
	*blanks = append(*blanks, spans...)
	for _, span := range spans {
		for i := span[0]; i < span[1]; i++ {
			if fixed[i] != '\n' && fixed[i] != '\r' {
				fixed[i] = ' '
			}
		}
	}
	return true
}

// isTestAttribute は、属性が swift-testing の `@Test` (`@Testing.Test` を含む) か。B の分岐の照合とテスト関数の判定が同じ式を使う。
func isTestAttribute(n *sitter.Node, src []byte) bool {
	for i := range n.NamedChildCount() {
		c := n.NamedChild(i)
		if kindOf(c) == "user_type" {
			s := c.Utf8Text(src)
			if s == "Test" || strings.HasSuffix(s, ".Test") {
				return true
			}
		}
	}
	return false
}

// D: ERROR またはエラーを含む条件文の、if/switch/where に続く式の演算子だけを空白にする。
// 字面を検索すると box.await や本体の await まで消すので、式の親子だけをたどり本体・呼び出しの中へは入らない。
// try! / try? は演算子全体で除外する (語だけ消すと ! が否定として読まれる)。
// 読めない単位を比べられるようにする代わりに、素の await/try の有無だけ違う写しが同じ形になるのは受け入れる。
func repairConditionOperators(n *sitter.Node, fixed []byte) bool {
	k := kindOf(n)
	if !n.IsError() && k != "if_statement" && k != "switch_statement" && k != "for_statement" {
		return false
	}
	// 文として読めている if / switch / for は、エラーが条件の側にあるときだけ直す。本体にだけエラーがある文 (`if let v = try await load() { g(()) }`)
	// で条件の try / await を消すと、正しく読めていた条件の形が変わる
	if !n.IsError() && !conditionHasError(n) {
		return false
	}
	active, changed := false, false
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch kindOf(c) {
		case "if", "switch":
			active = true
		case "{", "}", "statements":
			active = false
		case "where_keyword":
			active = n.IsError()
		case "where_clause":
			if blankConditionOperator(c, fixed) || (n.IsError() && blankMisparsedConditionHead(c, fixed)) {
				changed = true
			}
		default:
			if active && (blankConditionOperator(c, fixed) || (n.IsError() && blankMisparsedConditionHead(c, fixed))) {
				changed = true
			}
			if n.IsError() {
				active = false
			}
		}
	}
	return changed
}

// ERROR の条件先頭は await_expression を失い await トークンだけになるか、
// 後続の文を call_expression / navigation_expression の末尾に飲み込む。
// その先頭の鎖だけをたどって await/try を空白にする。全子へ降りると
// 引数・末尾クロージャ・後続の文の演算子まで消すため、callee/target 以外は見ない。
// ERROR の where_keyword も条件の開始とし、最初の子を処理したら止める。
// 区切りの { } を消すと文の境界を壊すため、演算子の ASCII 字句だけを替える。
func blankMisparsedConditionHead(n *sitter.Node, fixed []byte) bool {
	if n.IsError() && n.NamedChildCount() > 0 {
		return blankMisparsedConditionHead(n.NamedChild(0), fixed)
	}
	switch kindOf(n) {
	case "await":
		if n.Utf8Text(fixed) == "await" {
			copy(fixed[n.StartByte():], "     ")
			return true
		}
	case "await_expression", "try_expression":
		return blankConditionOperator(n, fixed)
	case "call_expression", "navigation_expression":
		if n.NamedChildCount() > 0 {
			return blankMisparsedConditionHead(n.NamedChild(0), fixed)
		}
	case "where_clause":
		if n.NamedChildCount() == 2 && kindOf(n.NamedChild(0)) == "where_keyword" {
			return blankMisparsedConditionHead(n.NamedChild(1), fixed)
		}
	}
	return false
}

// conditionHasError は、D の読み違えの形か: 条件の `await` / `try` の式が本体の `{…}` を末尾クロージャとして飲み込む。印は 2 つで、
// 本体の `{` が文の直下に無い・欠けている、または条件の式 (where 節を含む) の末尾がクロージャで終わる (`if let x = await y { … } else { … }` では
// else の `{` が文の直下に残るので、後者で見る)。「条件の側の子に ERROR があるか」では判定しない: 条件の中の別の読み違え (`send(())`・`== ()`) でも
// 真になり、正しく読めている条件の try / await まで消す。
func conditionHasError(n *sitter.Node) bool {
	active, brace := false, false
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch kindOf(c) {
		case "if", "switch":
			active = true
		case "{":
			if c.IsMissing() {
				return true
			}
			brace, active = true, false
		case "}", "statements":
			active = false
		case "where_clause":
			if endsWithClosure(c) {
				return true
			}
		default:
			if active && endsWithClosure(c) {
				return true
			}
		}
	}
	return !brace
}

// endsWithClosure は、節の最後の名前つきの子をたどった先が、呼び出しの末尾クロージャ (call_suffix の直下の lambda_literal) か。
// 括弧の中の引数のクロージャ (`contains(where: { … })`) と、文字列の補間・括弧・配列の中の末尾クロージャ (`"\(xs.map { $0 })"`) は、
// 正しい条件にも現れるので数えない (その中へは降りない)。
func endsWithClosure(n *sitter.Node) bool {
	for n.NamedChildCount() > 0 {
		parent := n
		n = n.NamedChild(n.NamedChildCount() - 1)
		switch kindOf(n) {
		case "lambda_literal":
			return kindOf(parent) == "call_suffix"
		case "line_string_literal", "multi_line_string_literal", "raw_string_literal", "interpolated_expression", "tuple_expression", "array_literal", "dictionary_literal":
			return false
		}
	}
	return false
}

func blankConditionOperator(n *sitter.Node, fixed []byte) bool {
	changed := false
	switch kindOf(n) {
	case "await_expression":
		// 名前 await の直後の本体がオペランドの lambda_literal と読まれた場合は、
		// 演算子とは確定できない。呼び出し式を包む await と区別して残す。
		if expr := n.ChildByFieldName("expr"); expr == nil || kindOf(expr) == "lambda_literal" {
			return false
		}
		keyword := n.Child(0)
		if keyword != nil && kindOf(keyword) == "await" && keyword.Utf8Text(fixed) == "await" {
			copy(fixed[keyword.StartByte():], "     ")
			changed = true
		}
	case "try_expression":
		op := n.Child(0)
		if op == nil || kindOf(op) != "try_operator" || op.Utf8Text(fixed) != "try" {
			return false
		}
		copy(fixed[op.StartByte():], "   ")
		changed = true
	case "assignment":
		// 条件の await が代入の左辺に誤認された形。右辺は次の文なので触らない。
		c := n.ChildByFieldName("target")
		return c != nil && blankConditionOperator(c, fixed)
	case "directly_assignable_expression", "where_clause", "comparison_expression", "equality_expression", "conjunction_expression", "disjunction_expression", "infix_expression":
	default:
		return false
	}
	for i := range n.NamedChildCount() {
		if blankConditionOperator(n.NamedChild(i), fixed) {
			changed = true
		}
	}
	return changed
}

// C: 文の位置で ERROR の唯一の子が inheritance_modifier(final) であり、次の節が = で始まる代入だけ finaL にする。
// ERROR 全域の final を探すと宣言の修飾子も変える。語を消すと代入の左辺が無くなるので同じ長さの名前を使う。
func repairFinalAssignment(n *sitter.Node, fixed []byte) bool {
	if !n.IsError() || n.Utf8Text(fixed) != "final" || n.NamedChildCount() != 1 || kindOf(n.NamedChild(0)) != "inheritance_modifier" {
		return false
	}
	parent, next := n.Parent(), n.NextSibling()
	if parent == nil || next == nil {
		return false
	}
	switch kindOf(parent) {
	case "statements", "if_statement", "function_body":
	default:
		return false
	}
	for next.ChildCount() > 0 {
		next = next.Child(0)
	}
	if kindOf(next) != "=" {
		return false
	}
	for _, b := range fixed[n.EndByte():next.StartByte()] {
		if b != ' ' && b != '\t' {
			return false
		}
	}
	copy(fixed[n.StartByte():], "finaL")
	return true
}

// E: lambda_literal の statements 内で、呼び出しと return の間にある長さ 0 の ERROR が ; を指す形だけ。
// 直前の呼び出しの終わりまでの水平空白と ; を並べ替える。語の削除や改行の追加は文や行位置を変えるので行わない。
func repairClosureSemicolon(n *sitter.Node, fixed []byte) bool {
	if !n.IsError() || n.StartByte() != n.EndByte() {
		return false
	}
	parent, prev, next := n.Parent(), n.PrevSibling(), n.NextSibling()
	if parent == nil || kindOf(parent) != "statements" || parent.Parent() == nil || kindOf(parent.Parent()) != "lambda_literal" || prev == nil || kindOf(prev) != "call_expression" || next == nil || kindOf(next) != "control_transfer_statement" || !hasToken(next, "return") {
		return false
	}
	start, end := prev.EndByte(), n.StartByte()
	if start >= end || end >= uint(len(fixed)) || fixed[end] != ';' {
		return false
	}
	for _, b := range fixed[start:end] {
		if b != ' ' && b != '\t' {
			return false
		}
	}
	copy(fixed[start+1:end+1], fixed[start:end])
	fixed[start] = ';'
	return true
}

// isSoleEmptyTupleArgument は、欠けた節が「ラベルの無い唯一の引数 (value_argument) の、字面が `()` の空のタプルの中の bang」にあるか。
func isSoleEmptyTupleArgument(missing *sitter.Node) bool {
	if !isEmptyTupleArgument(missing) {
		return false
	}
	tuple := missing.Parent().Parent()
	arg := tuple.Parent()
	args := arg.Parent()
	return tuple.EndByte()-tuple.StartByte() == 2 && namedNonComment(arg) == 1 && args != nil && kindOf(args) == "value_arguments" && namedNonComment(args) == 1
}

// namedNonComment は、コメントを除いた名前つきの子の数 (コメントは名前つきの節なので、引数の中に書くと数に入る)。
func namedNonComment(n *sitter.Node) int {
	c := 0
	for i := range n.NamedChildCount() {
		switch kindOf(n.NamedChild(i)) {
		case "comment", "multiline_comment":
		default:
			c++
		}
	}
	return c
}

// isEmptyTupleArgument は、欠けた節が「呼び出しの引数 (value_argument) の空のタプル (tuple_expression) の中の bang」にあるか。
func isEmptyTupleArgument(missing *sitter.Node) bool {
	bang := missing.Parent()
	if bang == nil || kindOf(bang) != "bang" {
		return false
	}
	tuple := bang.Parent()
	if tuple == nil || kindOf(tuple) != "tuple_expression" {
		return false
	}
	arg := tuple.Parent()
	return arg != nil && kindOf(arg) == "value_argument"
}

// countErrors は ERROR と欠けた節の数。
func countErrors(n *sitter.Node) int {
	if !n.HasError() {
		return 0
	}
	c := 0
	if n.IsError() || n.IsMissing() {
		c = 1
	}
	for i := range n.ChildCount() {
		c += countErrors(n.Child(i))
	}
	return c
}

// calls はファイルの中の呼び出しを全部集める (テスト関数の中も。単位にしない関数の中も呼び出し元に数える)。
func (w *walker) calls(root *sitter.Node) []tree.Call {
	lines := strings.Split(string(w.src), "\n")
	var out []tree.Call
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if kindOf(n) == "call_expression" && n.NamedChildCount() > 0 {
			if name := calleeName(n.NamedChild(0), w.src); name != "" {
				pos := n.StartPosition()
				c := tree.Call{File: w.file, Line: int(pos.Row) + 1, Col: int(pos.Column) + 1, Name: name, Labels: []string{}}
				if suf := childOfKind(n, "call_suffix"); suf != nil {
					c.Trailing = childOfKind(suf, "lambda_literal") != nil
					if args := childOfKind(suf, "value_arguments"); args != nil {
						for i := range args.NamedChildCount() {
							label := "_"
							if l := childOfKind(args.NamedChild(i), "value_argument_label"); l != nil {
								label = l.Utf8Text(w.src)
							}
							c.Labels = append(c.Labels, label)
						}
					}
				}
				if int(pos.Row) < len(lines) {
					c.Text = strings.TrimSpace(lines[pos.Row])
				}
				out = append(out, c)
			}
		}
		for i := range n.NamedChildCount() {
			visit(n.NamedChild(i))
		}
	}
	visit(root)
	return out
}

// definitions walks declaration metadata only when def was requested. These records never enter Unit,
// so adding types and stored properties cannot affect duplicate grouping.
func (w *walker) definitions(root *sitter.Node) ([]lang.Definition, []lang.TypeDeclaration) {
	var defs []lang.Definition
	var types []lang.TypeDeclaration
	type context struct {
		enclosing      []string
		testClass      bool
		insideCallable bool
	}
	addDefinition := func(n *sitter.Node, c context, name, kind, signature string, labels []string) {
		pos := n.StartPosition()
		test := w.test || c.testClass
		if kind == string(tree.KindFunc) && w.isTestFunc(n, ctx{enclosing: c.enclosing, testClass: c.testClass}) {
			test = true
		}
		defs = append(defs, lang.Definition{
			Name: name, Kind: kind, File: w.file, Line: int(pos.Row) + 1, EndLine: int(n.EndPosition().Row) + 1,
			Col: int(pos.Column) + 1, Enclosing: strings.Join(c.enclosing, "."), Signature: signature,
			Labels: labels, Test: test, Nested: c.insideCallable, Visibility: w.visibility(n), Doc: w.docComment(n),
		})
		defs[len(defs)-1].DocStart, defs[len(defs)-1].DocEnd = w.docRange(n)
	}
	var visit func(*sitter.Node, context)
	visit = func(n *sitter.Node, c context) {
		k := kindOf(n)
		childContext := c
		if isTypeDeclarationKind(k) {
			childContext.insideCallable = false
			rawName := w.typeName(n)
			name := typeIdentity(rawName)
			if name != "" {
				pos := n.StartPosition()
				kind := w.typeDeclarationKind(n)
				scope := rawName
				if kind != "extension" && len(c.enclosing) > 0 {
					scope = strings.Join(append(append([]string(nil), c.enclosing...), rawName), ".")
				}
				types = append(types, lang.TypeDeclaration{
					Name: name, Kind: kind, File: w.file,
					Line: int(pos.Row) + 1, EndLine: int(n.EndPosition().Row) + 1, Col: int(pos.Column) + 1,
					Enclosing: strings.Join(c.enclosing, "."), Scope: scope, Signature: rawName,
					Test: w.test || c.testClass, Nested: c.insideCallable,
					Visibility: w.visibility(n), Doc: w.docComment(n),
				})
				types[len(types)-1].DocStart, types[len(types)-1].DocEnd = w.docRange(n)
				childContext.enclosing = append(append([]string(nil), c.enclosing...), rawName)
			}
			childContext.testClass = c.testClass || w.inheritsTestCase(n)
		}

		switch k {
		case "function_declaration", "protocol_function_declaration", "init_declaration", "deinit_declaration", "subscript_declaration":
			u := w.declUnit(n, ctx{enclosing: c.enclosing, testClass: c.testClass})
			d := definitionFromUnit(u, c.insideCallable)
			if w.isTestFunc(n, ctx{enclosing: c.enclosing, testClass: c.testClass}) {
				d.Test = true
			}
			defs = append(defs, d)
			childContext.insideCallable = true
		case "property_declaration":
			if !c.insideCallable {
				for _, name := range w.boundNames(n) {
					if computed := n.ChildByFieldName("computed_value"); computed != nil {
						u := w.propertyUnit(n, computed, name, ctx{enclosing: c.enclosing, testClass: c.testClass})
						defs = append(defs, definitionFromUnit(u, c.insideCallable))
					} else {
						addDefinition(n, c, name, string(tree.KindProperty), w.storedPropertySignature(n, name), nil)
					}
				}
			}
			if n.ChildByFieldName("computed_value") != nil {
				childContext.insideCallable = true
			}
		case "didset_clause", "willset_clause":
			name := "didSet"
			if k == "willset_clause" {
				name = "willSet"
			}
			addDefinition(n, c, name, string(tree.KindProperty), name, nil)
			childContext.insideCallable = true
		case "protocol_property_declaration":
			if name := w.boundName(n); name != "" {
				addDefinition(n, c, name, string(tree.KindProperty), w.storedPropertySignature(n, name), nil)
			}
		case "enum_entry":
			for i := range n.ChildCount() {
				name := n.Child(i)
				if n.FieldNameForChild(uint32(i)) != "name" {
					continue
				}
				// 終わりは次の `,` の手前の子の終わり。`,` の開始で切ると、`,` を次の行の頭に書いた形で次の case の行まで含む
				last := name
				for j := i + 1; j < n.ChildCount(); j++ {
					child := n.Child(j)
					if !child.IsNamed() && w.text(child) == "," {
						break
					}
					if !isComment(child) {
						last = child
					}
				}
				pos := name.StartPosition()
				signature := collapse(strings.TrimSpace(string(w.src[name.StartByte():last.EndByte()])))
				endLine := int(last.EndPosition().Row) + 1
				defs = append(defs, lang.Definition{
					Name: w.text(name), Kind: "case", File: w.file, Line: int(pos.Row) + 1, EndLine: endLine,
					Col: int(pos.Column) + 1, Enclosing: strings.Join(c.enclosing, "."), Signature: signature,
					Test: w.test || c.testClass, Nested: c.insideCallable, Visibility: w.visibility(n), Doc: w.docComment(n),
				})
				defs[len(defs)-1].DocStart, defs[len(defs)-1].DocEnd = w.docRange(n)
			}
		case "typealias_declaration", "associatedtype_declaration":
			if name := w.declaredName(n); name != "" {
				kind := "typealias"
				if k == "associatedtype_declaration" {
					kind = "associatedtype"
				}
				addDefinition(n, c, name, kind, collapse(w.text(n)), nil)
			}
		case "lambda_literal":
			childContext.insideCallable = true
		}
		for i := range n.ChildCount() {
			visit(n.Child(i), childContext)
		}
	}
	visit(root, context{})
	return defs, types
}

func definitionFromUnit(u *tree.Unit, nested bool) lang.Definition {
	return lang.Definition{
		Name: u.Name, Kind: string(u.Kind), File: u.File, Line: u.Line, EndLine: u.EndLine, Col: u.Col,
		Enclosing: u.Enclosing, Signature: u.Signature, Labels: append([]string(nil), u.Labels...), Test: u.Test,
		Nested: nested, Visibility: u.Visibility, Doc: u.Doc, DocStart: u.DocStart, DocEnd: u.DocEnd,
	}
}

func (w *walker) declaredName(n *sitter.Node) string {
	if name := n.ChildByFieldName("name"); name != nil {
		return w.text(name)
	}
	for i := range n.NamedChildCount() {
		child := n.NamedChild(i)
		if kindOf(child) == "simple_identifier" || kindOf(child) == "type_identifier" {
			return w.text(child)
		}
	}
	return ""
}

func (w *walker) storedPropertySignature(n *sitter.Node, name string) string {
	keyword := "var"
	var findKeyword func(*sitter.Node) bool
	findKeyword = func(node *sitter.Node) bool {
		if !node.IsNamed() {
			if text := w.text(node); text == "let" || text == "var" {
				keyword = text
				return true
			}
		}
		for i := range node.ChildCount() {
			if findKeyword(node.Child(i)) {
				return true
			}
		}
		return false
	}
	findKeyword(n)
	signature := keyword + " " + name
	var findType func(*sitter.Node) string
	findType = func(node *sitter.Node) string {
		if kindOf(node) == "type_annotation" {
			return collapse(strings.TrimPrefix(w.text(node), ":"))
		}
		for i := range node.NamedChildCount() {
			if value := findType(node.NamedChild(i)); value != "" {
				return value
			}
		}
		return ""
	}
	typeNode := n
	if kindOf(n) == "property_declaration" {
		typeNode = nil
		for i := range n.ChildCount() {
			if n.FieldNameForChild(uint32(i)) != "name" {
				continue
			}
			pattern := n.Child(i)
			if w.text(pattern) != name {
				continue
			}
			// `let a, b: String` の a は後ろの束縛の型注釈を共有する。初期化式 (`= 値`) が先に来たら型は推論なので付けない
			for j := i + 1; j < n.ChildCount(); j++ {
				child := n.Child(j)
				if kindOf(child) == "type_annotation" {
					typeNode = child
				}
				if typeNode != nil || kindOf(child) == "=" || n.FieldNameForChild(uint32(j)) == "computed_value" {
					break
				}
			}
			break
		}
	}
	if typeNode != nil {
		if typ := findType(typeNode); typ != "" {
			signature += ": " + typ
		}
	}
	return signature
}

func isTypeDeclarationKind(kind string) bool {
	switch kind {
	case "class_declaration", "protocol_declaration":
		return true
	default:
		return false
	}
}

func typeIdentity(name string) string {
	name = collapse(name)
	if before, _, ok := strings.Cut(name, " where "); ok {
		name = before
	}
	var identity strings.Builder
	depth := 0
	for _, r := range name {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				identity.WriteRune(r)
			}
		}
	}
	name = strings.TrimSpace(identity.String())
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func (w *walker) typeDeclarationKind(n *sitter.Node) string {
	switch kindOf(n) {
	case "protocol_declaration":
		return "protocol"
	}
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child.IsNamed() {
			continue
		}
		switch text := w.text(child); text {
		case "class", "struct", "enum", "actor", "protocol", "extension":
			return text
		}
	}
	return "type"
}

func (w *walker) references(root *sitter.Node) []lang.Reference {
	lines := strings.Split(string(w.src), "\n")
	var out []lang.Reference
	type context struct {
		binding, afterDot, declarationName bool
		inTest, testClass                  bool
		enclosing                          []string
		typeEnclosing                      string
		testOwner                          string
	}
	var visit func(n, parent *sitter.Node, field string, c context)
	visit = func(n, parent *sitter.Node, field string, c context) {
		k := kindOf(n)
		// 宣言の名前かは、親の最初の name の子と位置で比べる (isNameField)。extension の名前 (extension Foo の Foo) は既にある型への参照として数える
		c.declarationName = c.declarationName || (parent != nil && declarationNameKind(kindOf(parent)) && isNameField(parent, n) &&
			(kindOf(parent) != "class_declaration" || w.typeDeclarationKind(parent) != "extension"))
		pos := n.StartPosition()
		if k == "simple_identifier" && !c.declarationName && !isGenericParameterName(parent, n, field) {
			role := w.nameRole(parent, field, c.binding, c.afterDot)
			if (role != tree.RoleDecl || isOptionalBindingShorthand(parent, n, field)) && role != tree.RoleLabel {
				row := int(pos.Row)
				text := ""
				if row >= 0 && row < len(lines) {
					text = strings.TrimSpace(lines[row])
				}
				out = append(out, lang.Reference{
					Name: w.text(n), Kind: "expression_position", File: w.file, Line: row + 1, Col: int(pos.Column) + 1,
					Text: text, Test: c.inTest, Enclosing: c.typeEnclosing, TestOwner: c.testOwner,
				})
			}
		} else if k == "type_identifier" && !c.declarationName && !isGenericParameterName(parent, n, field) {
			row := int(pos.Row)
			text := ""
			if row >= 0 && row < len(lines) {
				text = strings.TrimSpace(lines[row])
			}
			out = append(out, lang.Reference{
				Name: w.text(n), Kind: "type_position", File: w.file, Line: row + 1, Col: int(pos.Column) + 1,
				Text: text, Test: c.inTest, Enclosing: c.typeEnclosing, TestOwner: c.testOwner,
			})
		}

		childContext := c
		if isTypeDeclarationKind(k) {
			if rawName := w.typeName(n); rawName != "" {
				childContext.enclosing = append(append([]string(nil), c.enclosing...), rawName)
				childContext.typeEnclosing = strings.Join(childContext.enclosing, ".")
			}
			if w.inheritsTestCase(n) {
				childContext.testClass = true
				childContext.inTest = true
			}
		}
		if k == "function_declaration" && w.isTestFunc(n, ctx{enclosing: c.enclosing, testClass: c.testClass}) {
			childContext.inTest = true
			unit := w.declUnit(n, ctx{enclosing: c.enclosing, testClass: c.testClass})
			childContext.testOwner = unit.Signature
			if enclosing := strings.Join(c.enclosing, "."); enclosing != "" {
				childContext.testOwner = enclosing + "." + unit.Signature
			}
		}

		inPattern := k == "pattern"
		patternBinds := inPattern && c.binding
		if inPattern {
			for i := range n.ChildCount() {
				if isBindingKeyword(n.Child(i), w.src) {
					patternBinds = true
				}
			}
		}
		condList := k == "if_statement" || k == "guard_statement" || k == "while_statement" || k == "repeat_while_statement"
		clause := false
		depth, prevDot := 0, false
		for i := range n.ChildCount() {
			child := n.Child(i)
			if isBindingKeyword(child, w.src) {
				clause = true
			}
			if condList && !child.IsNamed() {
				switch w.text(child) {
				case "(":
					depth++
				case ")":
					depth--
				case ",":
					if depth == 0 {
						clause = false
					}
				}
			}
			childField := n.FieldNameForChild(uint32(i))
			forItem := k == "for_statement" && childField == "item" && !hasToken(child, "case")
			childBinding := patternBinds || (clause && (kindOf(child) == "pattern" || childField == "bound_identifier")) || forItem
			nextContext := childContext
			nextContext.binding = childBinding
			nextContext.afterDot = prevDot
			nextContext.declarationName = c.declarationName
			// 型の名前・継承節・generic の制約はその型の外の参照として読む (`extension Foo` の Foo を Foo の中と出さない)
			if isTypeDeclarationKind(k) && childField != "body" {
				nextContext.typeEnclosing = c.typeEnclosing
			}
			visit(child, n, childField, nextContext)
			prevDot = !child.IsNamed() && w.text(child) == "."
		}
	}
	visit(root, nil, "", context{inTest: w.test})
	return out
}

func declarationNameKind(kind string) bool {
	switch kind {
	case "class_declaration", "protocol_declaration",
		// subscript_declaration は入れない: 自分の名前を持たず、最初の name の子が戻り値の型になる
		"function_declaration", "protocol_function_declaration", "init_declaration", "deinit_declaration", "property_declaration",
		"protocol_property_declaration", "typealias_declaration", "associatedtype_declaration", "enum_entry", "macro_declaration":
		// type_parameter は入れない: 引数の名前 (`<S: P>` の S) にはフィールドが無く、制約の P の側に name が付くので、name で読むと
		// 制約の型を宣言名として落とす。引数の名前は isGenericParameterName (最初の名前つきの子と位置で比べる) が除く
		return true
	default:
		return false
	}
}

// isNameField は n が parent の最初の name フィールドの子か。位置 (byte の範囲) で比べる。
// この文法は戻り値の型 (`-> Box`) や typealias の右辺 (`= Box`) にも name のフィールドを付けるので、宣言の名前は最初の 1 つだけ
// (2 つ目以降を宣言名と読むと、型の参照を部分木ごと落とす)。
// enum の case (`case a, b`) だけは name の子が全部宣言の名前 (関連値の型には name が付かない)。
func isNameField(parent, n *sitter.Node) bool {
	if kindOf(parent) == "enum_entry" {
		cursor := parent.Walk()
		defer cursor.Close()
		for _, c := range parent.ChildrenByFieldName("name", cursor) {
			if c.StartByte() == n.StartByte() && c.EndByte() == n.EndByte() {
				return true
			}
		}
		return false
	}
	c := parent.ChildByFieldName("name")
	return c != nil && c.StartByte() == n.StartByte() && c.EndByte() == n.EndByte()
}

func isTypeParameterName(parent *sitter.Node, field string) bool {
	return parent != nil && kindOf(parent) == "type_parameter" && field == "name"
}

func isGenericParameterName(parent, n *sitter.Node, field string) bool {
	if parent == nil || n == nil {
		return false
	}
	if kindOf(parent) == "type_parameter" || kindOf(parent) == "type_parameter_pack" {
		if field == "name" || isTypeParameterName(parent, field) {
			return true
		}
		first := parent.NamedChild(0)
		if first != nil && first.StartByte() == n.StartByte() && first.EndByte() == n.EndByte() {
			return true
		}
	}
	// pack の宣言は type_parameter → type_parameter_pack → user_type → T の形。`repeat each T` の使用にも同じ user_type が出るが、
	// そちらの pack の親は type_pack_expansion なので、親まで見ないと使用の側まで落とす
	if kindOf(parent) == "user_type" && kindOf(n) == "type_identifier" {
		pack := parent.Parent()
		if pack != nil && kindOf(pack) == "type_parameter_pack" {
			parameter := pack.Parent()
			if parameter != nil && kindOf(parameter) == "type_parameter" {
				return true
			}
		}
	}
	return false
}

// isOptionalBindingShorthand は `if let foo` (guard / while も) の省略形の名前か。束縛であると同時に、外側の foo への参照でもある。
func isOptionalBindingShorthand(parent, n *sitter.Node, field string) bool {
	if parent == nil || n == nil || kindOf(n) != "simple_identifier" || field != "bound_identifier" {
		return false
	}
	switch kindOf(parent) {
	case "if_statement", "guard_statement", "while_statement":
	default:
		return false
	}
	for i := range parent.ChildCount() {
		child := parent.Child(i)
		if child.StartByte() != n.StartByte() || child.EndByte() != n.EndByte() {
			continue
		}
		// 省略形は名前の直後で条件が終わる (`,` / `{` / guard の else)。`= 値`・型注釈 (`: Int = x`)・`case let foo? = x` は新しい束縛
		prev := prevNonComment(parent, i)
		if prev == nil || kindOf(prev) != "value_binding_pattern" {
			return false
		}
		next := nextNonComment(parent, i)
		if next == nil {
			return false
		}
		switch kindOf(next) {
		case ",", "{", "else":
			return true
		}
		return false
	}
	return false
}

// calleeName は呼び出しの先頭の式から呼ばれる名前を取る (`f`・`a.b.f`・`a?.f`)。それ以外 (`(f)()`・`x[0]()`・`f()()`) は空。
// 文法は `a + b.f(x)` を「`a + b.f` を呼ぶ」と読むので、二項式・前置の式は右端の式へ降りる。
func calleeName(n *sitter.Node, src []byte) string {
	switch k := kindOf(n); {
	case k == "simple_identifier":
		return n.Utf8Text(src)
	case k == "navigation_expression":
		for i := int(n.NamedChildCount()) - 1; i >= 0; i-- {
			if c := n.NamedChild(uint(i)); kindOf(c) == "navigation_suffix" {
				if id := childOfKind(c, "simple_identifier"); id != nil {
					return id.Utf8Text(src)
				}
			}
		}
	case strings.HasSuffix(k, "_expression") && k != "call_expression" && k != "tuple_expression" && n.NamedChildCount() > 0:
		return calleeName(n.NamedChild(n.NamedChildCount()-1), src)
	}
	return ""
}

// IsTestPath は Swift の慣習で test のファイルかを決める (`Tests` / `…Tests` / `…UITests` のディレクトリの下、`…Tests.swift`)。
// repo ごとの上書きは設定ファイルで行う (コードに特定の repo の規則を入れない)。
func IsTestPath(file string) bool {
	if strings.HasSuffix(path.Base(file), "Tests.swift") {
		return true
	}
	for _, c := range strings.Split(path.Dir(file), "/") {
		if c == "Tests" || strings.HasSuffix(c, "Tests") {
			return true
		}
	}
	return false
}

type walker struct {
	metadataOnly bool
	file         string
	src          []byte
	test         bool
	units        []*tree.Unit
	closures     map[string]int
	seen         map[string]int  // 鍵の重なりの数 (`#if` の両側の同じ関数など)
	static       bool            // static / class のメソッドの本体を変換している間
	isUnit       map[string]bool // 単位の鍵 (クロージャの Owner にしてよいか)
}

// ctx は辿っている場所の文脈。
type ctx struct {
	enclosing []string // 囲む型の名前
	owner     string   // 囲む単位の鍵
	ownerName string   // 囲む単位の表示名 (クロージャの表示に使う)
	inError   bool     // 読めない構文 (ERROR) の中
	testClass bool     // XCTestCase を継承した型の中
	property  string   // 囲む property の名前 (didSet などの単位の名前に使う)
}

func (w *walker) text(n *sitter.Node) string { return n.Utf8Text(w.src) }

func (w *walker) walk(n *sitter.Node, c ctx) {
	if n.IsError() || n.IsMissing() {
		c.inError = true
	}
	switch kindOf(n) {
	case "class_declaration", "protocol_declaration":
		if name := w.typeName(n); name != "" {
			c.enclosing = append(append([]string(nil), c.enclosing...), name)
		}
		if w.inheritsTestCase(n) {
			c.testClass = true
		}
		// 関数の中の型の中は、関数の中として表示しない。鍵に入る owner と property は今の形のまま残す (変えると除外の釘が外れる)。
		c.ownerName = ""
	case "function_declaration", "init_declaration", "deinit_declaration", "subscript_declaration":
		if w.isTestFunc(n, c) {
			return
		}
		c.owner, c.ownerName = w.addOwner(w.declUnit(n, c), c)
	case "property_declaration":
		if name := w.boundName(n); name != "" {
			c.property = name
		}
		if cp := n.ChildByFieldName("computed_value"); cp != nil {
			c.owner, c.ownerName = w.addOwner(w.propertyUnit(n, cp, c.property, c), c)
		}
	case "didset_clause", "willset_clause":
		name := "didSet"
		if kindOf(n) == "willset_clause" {
			name = "willSet"
		}
		c.owner, c.ownerName = w.addOwner(w.newUnit(n, tree.KindProperty, c.property+"."+name, "", nil, n, c), c)
	case "lambda_literal":
		c.owner, c.ownerName = w.addOwner(w.closureUnit(n, c), c)
	}
	for i := range n.ChildCount() {
		w.walk(n.Child(i), c)
	}
}

// add は単位を足し、囲む単位として使う鍵を返す。鍵が重なったら (`#if` の両側の同じ関数・同じ署名の入れ子の関数) 番号を付けて分ける。
func (w *walker) add(u *tree.Unit, c ctx) string {
	w.seen[u.Key]++
	if n := w.seen[u.Key]; n > 1 {
		u.Key = fmt.Sprintf("%s#%d", u.Key, n)
	}
	u.Unreadable = u.Unreadable || c.inError
	w.units = append(w.units, u)
	w.isUnit[u.Key] = true
	return u.Key
}

// addOwner は単位を足し、中の単位から見た囲む単位の鍵と表示名を返す。
func (w *walker) addOwner(u *tree.Unit, c ctx) (string, string) {
	name := u.Name
	if u.OwnerName != "" {
		name = u.OwnerName + "." + name
	}
	return w.add(u, c), name
}

func (w *walker) typeName(n *sitter.Node) string {
	c := n.ChildByFieldName("name")
	if c == nil {
		for i := range n.NamedChildCount() {
			child := n.NamedChild(i)
			switch kindOf(child) {
			case "type_identifier", "user_type", "navigation_expression":
				c = child
			}
			if c != nil {
				break
			}
		}
	}
	if c == nil {
		return ""
	}
	name := w.text(c)
	// 条件つきの extension (`extension Array where Element == Int`) どうしを分ける。
	for i := range n.NamedChildCount() {
		if ch := n.NamedChild(i); kindOf(ch) == "where_clause" || kindOf(ch) == "type_constraints" {
			name += " " + collapse(w.text(ch))
		}
	}
	return name
}

func (w *walker) inheritsTestCase(n *sitter.Node) bool {
	for i := range n.NamedChildCount() {
		if ch := n.NamedChild(i); kindOf(ch) == "inheritance_specifier" && strings.HasSuffix(w.text(ch), "TestCase") {
			return true
		}
	}
	return false
}

func (w *walker) boundName(n *sitter.Node) string {
	p := n.ChildByFieldName("name")
	if p == nil {
		return ""
	}
	if b := p.ChildByFieldName("bound_identifier"); b != nil {
		return w.text(b)
	}
	return ""
}

// boundNames は property の宣言の束縛の名前を全部返す。`var x = 0, y = 0` は name の子を複数持つ 1 つの property_declaration になる。
func (w *walker) boundNames(n *sitter.Node) []string {
	var names []string
	seen := map[string]bool{}
	var visit func(*sitter.Node)
	visit = func(pattern *sitter.Node) {
		for i := range pattern.ChildCount() {
			child := pattern.Child(i)
			// tuple の pattern のラベル (`let (label: a) = …` の label) は束縛ではない。束縛は後ろの子の pattern の中にある
			if next := nextNonComment(pattern, i); kindOf(child) == "simple_identifier" && next != nil && kindOf(next) == ":" {
				continue
			}
			if kindOf(child) == "simple_identifier" {
				name := w.text(child)
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
				continue
			}
			if child.IsNamed() {
				visit(child)
			}
		}
	}
	for i := range n.ChildCount() {
		if n.FieldNameForChild(uint32(i)) == "name" {
			visit(n.Child(i))
		}
	}
	return names
}

func (w *walker) modifiers(n *sitter.Node) *sitter.Node {
	for i := range n.NamedChildCount() {
		if c := n.NamedChild(i); kindOf(c) == "modifiers" {
			return c
		}
	}
	return nil
}

// isTestFunc は Swift Testing の `@Test` (`@Testing.Test` も) か、XCTest の `func test…()` (引数なし。test のファイルか XCTestCase を継承した型の中) か。
func (w *walker) isTestFunc(n *sitter.Node, c ctx) bool {
	if kindOf(n) != "function_declaration" {
		return false
	}
	if m := w.modifiers(n); m != nil {
		for i := range m.NamedChildCount() {
			if a := m.NamedChild(i); kindOf(a) == "attribute" && isTestAttribute(a, w.src) {
				return true
			}
		}
	}
	name := ""
	if ch := n.ChildByFieldName("name"); ch != nil {
		name = w.text(ch)
	}
	return (w.test || c.testClass) && len(c.enclosing) > 0 && strings.HasPrefix(name, "test") && len(params(n)) == 0
}

func params(n *sitter.Node) []*sitter.Node {
	var ps []*sitter.Node
	for i := range n.NamedChildCount() {
		if c := n.NamedChild(i); kindOf(c) == "parameter" {
			ps = append(ps, c)
		}
	}
	return ps
}

func (w *walker) isStatic(n *sitter.Node) bool {
	if m := w.modifiers(n); m != nil {
		for i := range m.NamedChildCount() {
			if s := w.text(m.NamedChild(i)); s == "static" || s == "class" {
				return true
			}
		}
	}
	return false
}

func (w *walker) declUnit(n *sitter.Node, c ctx) *tree.Unit {
	kind := tree.KindFunc
	var name string
	switch kindOf(n) {
	case "init_declaration":
		kind, name = tree.KindInit, "init"
	case "deinit_declaration":
		kind, name = tree.KindProperty, "deinit"
	case "subscript_declaration":
		kind, name = tree.KindProperty, "subscript"
	default:
		if ch := n.ChildByFieldName("name"); ch != nil {
			name = w.text(ch)
		}
	}
	var sig, locals, paramTypes, labels []string
	for _, p := range params(n) {
		sig = append(sig, collapse(w.text(p)))
		if in := internalName(p, w.src); in != "" {
			locals = append(locals, in)
		}
		labels = append(labels, externalLabel(p, w.src))
		if _, typ, ok := strings.Cut(collapse(w.text(p)), ":"); ok {
			paramTypes = append(paramTypes, strings.TrimSpace(typ))
		}
	}
	signature := name + "(" + strings.Join(sig, ", ") + ")"
	ret := returnType(n, w.src)
	if ret != "" {
		signature += " -> " + ret
	}
	if w.isStatic(n) {
		signature = "static " + signature
		w.static = true
		defer func() { w.static = false }()
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		body = childOfKind(n, "computed_property") // subscript の本体 (引数の宣言を本体に入れない)
	}
	if body == nil && kindOf(n) == "deinit_declaration" {
		body = childOfKind(n, "function_body")
	}
	u := w.newUnit(n, kind, name, signature, locals, body, c)
	u.ReturnType = ret
	u.ParamTypes = paramTypes
	u.Labels = labels
	u.Visibility = w.visibility(n)
	u.Allow, u.AllowLine, u.HasAllow, u.AllowExtraLine = w.allowComment(n)
	u.Doc = w.docComment(n)
	u.DocStart, u.DocEnd = w.docRange(n)
	return u
}

func (w *walker) propertyUnit(decl, computed *sitter.Node, name string, c ctx) *tree.Unit {
	u := w.newUnit(decl, tree.KindProperty, name, "var "+name, nil, computed, c)
	for i := range decl.NamedChildCount() {
		if ch := decl.NamedChild(i); kindOf(ch) == "type_annotation" {
			u.ReturnType = collapse(strings.TrimPrefix(w.text(ch), ":"))
			u.Signature += ": " + u.ReturnType
		}
	}
	u.Visibility = w.visibility(decl)
	u.Allow, u.AllowLine, u.HasAllow, u.AllowExtraLine = w.allowComment(decl)
	u.Doc = w.docComment(decl)
	u.DocStart, u.DocEnd = w.docRange(decl)
	return u
}

// allowMarker は単位の直前のコメントに書く許可の印。
const allowMarker = "dupscan:allow"

func isComment(n *sitter.Node) bool {
	return kindOf(n) == "comment" || kindOf(n) == "multiline_comment"
}

// prevNonComment は parent の i 番目の子の前で、最初のコメントでない子を返す (無ければ nil)。
func prevNonComment(parent *sitter.Node, i uint) *sitter.Node {
	for j := int(i) - 1; j >= 0; j-- {
		if c := parent.Child(uint(j)); !isComment(c) {
			return c
		}
	}
	return nil
}

// nextNonComment は parent の i 番目の子の後ろで、最初のコメントでない子を返す (無ければ nil)。
// この文法はコメントを名前つきの子として木に入れるので、隣の子を見る判定はコメントを飛ばさないと `foo /* x */ {` で外れる
func nextNonComment(parent *sitter.Node, i uint) *sitter.Node {
	for j := i + 1; j < parent.ChildCount(); j++ {
		if c := parent.Child(j); !isComment(c) {
			return c
		}
	}
	return nil
}

// leadingComments は宣言 n の直前に続くコメント (空行を挟まない) を近い順に返す。
// 前の行の末尾のコメント (`let a = 1 // …`) はその行のものなので数えない。
func leadingComments(n *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	next := n
	for s := n.PrevSibling(); s != nil && isComment(s); s = s.PrevSibling() {
		if next.StartPosition().Row > s.EndPosition().Row+1 {
			break
		}
		if p := s.PrevSibling(); p != nil && p.EndPosition().Row == s.StartPosition().Row {
			break
		}
		out = append(out, s)
		next = s
	}
	return out
}

// commentBody はコメントの記号 (`//`・`///`・`/*`・`*/`・行頭の `*`) を除いた中身。改行も削る (`/**` の次の行の ` * …` を読む)。
func (w *walker) commentBody(c *sitter.Node) string {
	return strings.TrimLeft(strings.TrimSpace(strings.TrimSuffix(w.text(c), "*/")), "/*! \t\r\n")
}

// allowComment は宣言 n の直前のコメントの先頭から許可の印を探し、印の後ろを返す。クロージャ・didSet は名前で指せないので対象にしない。
// extra は 2 つ目の印の行 (無ければ 0)。
func (w *walker) allowComment(n *sitter.Node) (allow string, line int, ok bool, extra int) {
	for _, c := range leadingComments(n) {
		// 印はコメントの先頭だけで読む (`// TODO: dupscan:allow は使わない` のような文を許可にしない)。
		after, found := strings.CutPrefix(w.commentBody(c), allowMarker)
		if !found {
			continue
		}
		if ok {
			return allow, line, true, int(c.StartPosition().Row) + 1
		}
		allow, line, ok = strings.TrimSpace(after), int(c.StartPosition().Row)+1, true
	}
	return allow, line, ok, 0
}

// docRange は宣言 n の直前の doc comment (`///` か `/**`) の行の範囲 (1 始まり。無ければ 0, 0)。宣言に近い側から続く doc comment だけを数える
// (doc と宣言の間の `// dupscan:allow` などは飛ばし、doc の上に離れた `// MARK:` と別の doc があっても、そこで止める)。
func (w *walker) docRange(n *sitter.Node) (start, end int) {
	for _, c := range leadingComments(n) {
		if !isDocComment(w.text(c)) {
			if start > 0 {
				break
			}
			continue
		}
		start = int(c.StartPosition().Row) + 1
		if end == 0 {
			end = int(c.EndPosition().Row) + 1
		}
	}
	return start, end
}

func isDocComment(text string) bool {
	return strings.HasPrefix(text, "///") || strings.HasPrefix(text, "/**")
}

// docComment は宣言 n の doc comment (`///` か `/** */`) の最初の行。許可の印の行は doc に数えない。
func (w *walker) docComment(n *sitter.Node) string {
	cs := leadingComments(n)
	for i := len(cs) - 1; i >= 0; i-- {
		if !isDocComment(w.text(cs[i])) {
			continue
		}
		for _, line := range strings.Split(w.commentBody(cs[i]), "\n") {
			line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "*"))
			if line != "" && !strings.HasPrefix(line, allowMarker) {
				return line
			}
		}
	}
	return ""
}

func (w *walker) closureUnit(n *sitter.Node, c ctx) *tree.Unit {
	owner := c.owner
	if owner == "" {
		owner = w.file + "|" + strings.Join(c.enclosing, ".")
	}
	w.closures[owner]++
	u := w.newUnit(n, tree.KindClosure, "{closure}", "", nil, n, c)
	u.Key = fmt.Sprintf("%s#closure%d", owner, w.closures[owner])
	if w.isUnit[c.owner] {
		u.Owner = c.owner
	}
	u.OwnerName = c.ownerName
	if u.OwnerName == "" {
		u.OwnerName = c.property // 格納 property の初期値のクロージャ (`let x = { … }()`)
	}
	u.Visibility = "local"
	return u
}

// newUnit は単位を作る。鍵はファイル + 囲む型 + (入れ子なら囲む単位) + 署名。
func (w *walker) newUnit(n *sitter.Node, kind tree.UnitKind, name, sig string, locals []string, body *sitter.Node, c ctx) *tree.Unit {
	pos := n.StartPosition()
	encl := strings.Join(c.enclosing, ".")
	label := sig
	if label == "" {
		label = name
	}
	key := w.file + "|" + encl + "|" + label
	if c.owner != "" && kind != tree.KindClosure {
		key = c.owner + "/" + label // 関数の中の関数は、囲む単位ごとに分ける
	}
	u := &tree.Unit{
		Key: key, File: w.file, Line: int(pos.Row) + 1, EndLine: int(n.EndPosition().Row) + 1, Col: int(pos.Column) + 1,
		Name: name, Signature: sig, Kind: kind, Enclosing: encl, Test: w.test || c.testClass,
		Params: locals, Unreadable: broken(n), Visibility: "internal",
	}
	if w.metadataOnly {
		return u
	}
	if body != nil {
		u.Body = w.conv(body, nil, "", false, false)
		markOuter(u.Body, locals)
		if call := w.soleCall(body); call != nil {
			u.SingleCall = w.thinCall(call)
			u.Delegation = w.atomCall(call, true)
		}
	} else {
		u.Body = &tree.Node{Kind: "empty"}
	}
	return u
}

// soleCall は、本体の直下の文がちょうど 1 つ (コメントを除く) で、その文が呼び出し (return・try・await・後置の ! で包んでよい) なら、その呼び出しを返す。
// 委譲の 2 つの印はこの呼び出しから決める: Delegation (プリセット l2-single-call が外す) は後置のクロージャが無く引数が全部原子のときだけ。
// Delegation を「1 文」だけで判定しない: 1 つの大きな式や guard 1 文の写しを、定数を変えるだけで除外に通せてしまう。
// SingleCall (thinCall) は外さず印を付けるだけなので、文が 1 つのクロージャの引数も許す。
func (w *walker) soleCall(body *sitter.Node) *sitter.Node {
	stmts := childOfKind(body, "statements")
	if stmts == nil {
		if fb := childOfKind(body, "function_body"); fb != nil {
			stmts = childOfKind(fb, "statements")
		}
	}
	if stmts == nil {
		return nil
	}
	var only *sitter.Node
	for i := range stmts.NamedChildCount() {
		c := stmts.NamedChild(i)
		switch c.Kind() {
		case "comment", "multiline_comment":
			continue
		}
		if only != nil {
			return nil
		}
		only = c
	}
	if only == nil {
		return nil
	}
	call := w.unwrapStatement(only)
	if call == nil || call.Kind() != "call_expression" {
		return nil
	}
	return call
}

// thinCall は呼び出しが薄い委譲か (tree.Unit.SingleCall): 呼ぶ先が原子 (名前・メンバーの参照) で、引数が後置のクロージャを含めて
// thinCallArgs 個まで、どの引数も原子か文が 1 つのクロージャ。引数の数で絞るのは、引数を並べた大きな初期化子の呼び出し
// (`StorageCapabilities(a: …, b: …, …)` の 20 個) まで印が付くと、寄せる価値のある写しと見分けが付かなくなるため。
func (w *walker) thinCall(call *sitter.Node) bool {
	if call.NamedChildCount() != 2 || !w.atom(call.NamedChild(0)) {
		return false
	}
	suffix := call.NamedChild(1)
	if suffix.Kind() != "call_suffix" {
		return false
	}
	n := 0
	for i := range suffix.NamedChildCount() {
		c := suffix.NamedChild(i)
		switch c.Kind() {
		case "value_arguments":
			if !strings.HasPrefix(w.text(c), "(") {
				return false // 添字 (`values[key]`) も同じ value_arguments で表される (atomCall と同じ)
			}
			for j := range c.NamedChildCount() {
				a := c.NamedChild(j)
				v := a.ChildByFieldName("value")
				if a.Kind() != "value_argument" || v == nil || !w.atom(v) && !oneStatementClosure(v) {
					return false
				}
				n++
			}
		case "lambda_literal":
			if !oneStatementClosure(c) {
				return false
			}
			n++
		}
	}
	return n <= thinCallArgs
}

// thinCallArgs は薄い委譲の引数の数の上限 (後置のクロージャを含む)。
const thinCallArgs = 2

// oneStatementClosure は、文がちょうど 1 つ (コメントを除く) で、その文が制御文でなく、中のクロージャも同じ条件を満たすクロージャか。
// 制御文と、文が複数のクロージャを中に持つ文を数えないのは、その 1 文の中に任意の量の処理を入れられるため (寄せる価値のある写しに印が付く)。
// 入れ子を一律に断らない: 述語の 1 文 (`{ self.updates.contains { $0.id == id } }`) は待機の道具へ寄せた後の委譲に普通に現れる。
// `defer { … }` は文法が呼び出しとして読むので、制御文の一覧では止まらず、入れ子のクロージャの文の数で止まる。
// 1 文の大きさは測らない: 引数に呼び出しを並べた式 (`use(a(), b(), c())`)・複数の束縛の let・配列のリテラルは 1 文のまま印が付く。
// 印は組を隠さず gate も変えないので、測るための下限 (節の数) は置いていない。大きな 1 文に印が付いて除外を誤った例が出たら、節の数の上限を足す。
func oneStatementClosure(n *sitter.Node) bool {
	if n.Kind() != "lambda_literal" {
		return false
	}
	stmts := childOfKind(n, "statements")
	if stmts == nil {
		return false
	}
	var only *sitter.Node
	for i := range stmts.NamedChildCount() {
		c := stmts.NamedChild(i)
		switch c.Kind() {
		case "comment", "multiline_comment":
			continue
		case "for_statement", "while_statement", "repeat_while_statement", "if_statement", "guard_statement", "switch_statement", "do_statement":
			return false
		}
		if only != nil {
			return false
		}
		only = c
	}
	return only != nil && nestedClosuresThin(only)
}

// nestedClosuresThin は、n 自身と n の中のクロージャ (いちばん外側のもの。その中は oneStatementClosure が辿る) が全部 oneStatementClosure か。
func nestedClosuresThin(n *sitter.Node) bool {
	if kindOf(n) == "lambda_literal" {
		return oneStatementClosure(n)
	}
	for i := range n.NamedChildCount() {
		if !nestedClosuresThin(n.NamedChild(i)) {
			return false
		}
	}
	return true
}

// unwrapStatement は return・try・await・後置の ! を外す。
func (w *walker) unwrapStatement(n *sitter.Node) *sitter.Node {
	for n != nil {
		switch n.Kind() {
		case "control_transfer_statement":
			if !strings.HasPrefix(w.text(n), "return") {
				return nil
			}
			n = n.ChildByFieldName("result")
		case "try_expression", "await_expression":
			n = n.ChildByFieldName("expr")
		case "postfix_expression":
			if childOfKind(n, "bang") == nil {
				return nil
			}
			n = n.ChildByFieldName("target")
		default:
			return n
		}
	}
	return nil
}

// atomCall は呼び出しの callee が原子で、後置のクロージャが無く、引数が全部原子か。withArgs が false なら引数を許さない (原子の中の呼び出し `UUID()`)。
func (w *walker) atomCall(call *sitter.Node, withArgs bool) bool {
	if call.NamedChildCount() != 2 || !w.atom(call.NamedChild(0)) {
		return false
	}
	suffix := call.NamedChild(1)
	if suffix.Kind() != "call_suffix" || childOfKind(suffix, "lambda_literal") != nil {
		return false
	}
	args := childOfKind(suffix, "value_arguments")
	if args == nil || !strings.HasPrefix(w.text(args), "(") {
		return false // 添字 (`values["a"]`) も同じ value_arguments で表されるので、丸括弧の呼び出しに限る
	}
	n := 0
	for i := range args.NamedChildCount() {
		a := args.NamedChild(i)
		if a.Kind() != "value_argument" {
			return false
		}
		n++
		v := a.ChildByFieldName("value")
		if v == nil || !w.atom(v) {
			return false
		}
	}
	return withArgs || n == 0
}

// atom は計算をしない値か: リテラル・名前・self / super・メンバーの参照・暗黙のメンバー・原子だけを埋め込んだ文字列・引数の無い呼び出し、とそれらの ! / try / await。
func (w *walker) atom(n *sitter.Node) bool {
	switch n.Kind() {
	case "simple_identifier", "self_expression", "super_expression",
		"integer_literal", "real_literal", "boolean_literal", "hex_literal", "oct_literal", "bin_literal", "nil_literal":
		return true
	case "line_string_literal", "multi_line_string_literal":
		for i := range n.NamedChildCount() {
			c := n.NamedChild(i)
			switch c.Kind() {
			case "line_str_text", "multi_line_str_text", "str_escaped_char":
			case "interpolated_expression":
				v := c.ChildByFieldName("value")
				if v == nil || !w.atom(v) {
					return false
				}
			default:
				return false
			}
		}
		return true
	case "navigation_expression":
		t := n.ChildByFieldName("target")
		return t != nil && w.atom(t)
	case "prefix_expression":
		// 暗黙のメンバー (`.root`) と数値のリテラルの負号 (`-1`) だけ。`-y`・`~x`・`!flag` は計算なので原子にしない。
		t := n.ChildByFieldName("target")
		if t == nil || n.NamedChildCount() != 1 {
			return false
		}
		switch op := strings.TrimSpace(strings.TrimSuffix(w.text(n), w.text(t))); op {
		case ".":
			return t.Kind() == "simple_identifier"
		case "-":
			switch t.Kind() {
			case "integer_literal", "real_literal", "hex_literal", "oct_literal", "bin_literal":
				return true
			}
			return false
		}
		return false
	case "postfix_expression":
		t := n.ChildByFieldName("target")
		return t != nil && childOfKind(n, "bang") != nil && w.atom(t)
	case "try_expression", "await_expression":
		e := n.ChildByFieldName("expr")
		return e != nil && w.atom(e)
	case "call_expression":
		return w.atomCall(n, false)
	}
	return false
}

func (w *walker) visibility(n *sitter.Node) string {
	if m := w.modifiers(n); m != nil {
		for i := range m.NamedChildCount() {
			if c := m.NamedChild(i); kindOf(c) == "visibility_modifier" {
				return w.text(c)
			}
		}
	}
	return "internal"
}

// externalLabel は呼び出しで書く引数ラベル (`_ p: String` なら _、`to url: URL` なら to、`path: String` なら path)。
func externalLabel(p *sitter.Node, src []byte) string {
	for i := range p.ChildCount() {
		c := p.Child(i)
		if kindOf(c) == ":" {
			break
		}
		if kindOf(c) == "simple_identifier" { // `_` もこの種類で読まれる
			return c.Utf8Text(src)
		}
	}
	return "_"
}

// internalName は引数の本体の中で使う名前 (`_ p: String` なら p、`path: String` なら path)。
func internalName(p *sitter.Node, src []byte) string {
	last := ""
	for i := range p.ChildCount() {
		c := p.Child(i)
		if kindOf(c) == ":" {
			break
		}
		if kindOf(c) == "simple_identifier" {
			last = c.Utf8Text(src)
		}
	}
	return last
}

func returnType(n *sitter.Node, src []byte) string {
	after := false
	for i := range n.ChildCount() {
		c := n.Child(i)
		if kindOf(c) == "->" {
			after = true
			continue
		}
		if after && c.IsNamed() {
			return collapse(c.Utf8Text(src))
		}
	}
	return ""
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func childOfKind(n *sitter.Node, kind string) *sitter.Node {
	for i := range n.NamedChildCount() {
		if c := n.NamedChild(i); kindOf(c) == kind {
			return c
		}
	}
	return nil
}

// containerKinds は単位の親として数えない入れ物 (ファイル・型の本体)。ここに読めない構文があっても、隣の単位は読めている。
var containerKinds = map[string]bool{"source_file": true, "class_body": true, "enum_class_body": true, "protocol_body": true, "statements": true}

// broken は単位の節か、入れ物 (型・ファイルの本体) に着くまでの祖先のどれかが読めない構文を含むか
// (読めない部分に飲み込まれた関数の本体が、隣のクロージャとして読まれることがある)。
func broken(n *sitter.Node) bool {
	for c := n; c != nil && !containerKinds[kindOf(c)]; c = c.Parent() {
		if c.HasError() {
			return true
		}
	}
	return false
}

// conv は tree-sitter の節を言語に依らない木に写す。binding は let / var で束縛する pattern の中か、afterDot は直前のトークンが `.` か。
func (w *walker) conv(n, parent *sitter.Node, field string, binding, afterDot bool) *tree.Node {
	k := kindOf(n)
	out := &tree.Node{Kind: k}
	switch {
	case !n.IsNamed():
		out.Role = tree.RoleToken
		out.Text = w.text(n)
		return out
	case classify(k) == className:
		out.Text = w.text(n)
		out.Role = w.nameRole(parent, field, binding, afterDot)
		out.Expr = out.Role == tree.RoleName
		out.Capture = parent != nil && kindOf(parent) == "capture_list_item" && out.Role == tree.RoleName && field == "name"
		if out.Role == tree.RoleDecl && isOptionalBindingShorthand(parent, n, field) {
			// `if let user` は新しい局所の名前であると同時に、user の参照。参照の分を子に持つ。参照が外側 (プロパティ) か
			// 引数・既出の局所かは markOuter が決める (外側なら字面、局所なら番号。いつも字面にすると引数を読む写しを落とす)
			out.Children = []*tree.Node{{Kind: k, Role: tree.RoleName, Text: out.Text, Expr: true}}
		}
		return out
	case classify(k) == classType:
		out.Role = tree.RoleType
		out.Text = w.text(n)
		return out
	case classify(k) == classLiteral:
		out.Role = tree.RoleLiteral
		out.LitKind = literalKinds[k]
		out.Text = w.text(n)
		out.Expr = out.LitKind != "str" // 文字列の断片は、囲む文字列リテラルの節で畳む
		return out
	case n.ChildCount() == 0:
		// 子の無い名前つきの節 (custom_operator の `<+>`・statement_label の `outer:`) は字面をハッシュに入れる。
		out.Role = tree.RoleToken
		out.Text = w.text(n)
		out.Expr = classify(k) == classExpr
		return out
	}
	out.Expr = classify(k) == classExpr
	if k == "directly_assignable_expression" {
		out.Keep = true
	}
	// let / var は pattern の中 (`case let .some(z)`) にも、その外 (`let (a, b) = t` の property_declaration・
	// `if case let .pair(x, y) = v` の if_statement に平らに置かれたトークン) にも現れる。
	//   - pattern の中: 外から渡された束縛か、pattern 自身が let / var を持てば、その下全部に渡す
	//   - それ以外: let / var から、条件の区切りの `,` までを 1 つの節とし、その中の pattern と bound_identifier にだけ渡す
	//     (本体・初期化子の側へ渡すと中の switch の `case minValue:` まで局所になり、文の単位で渡すと
	//     `if let a = o, case .y(minValue) = z` の minValue まで局所になる)
	//   - for の項目 (`for (a, b) in`) は let が無くても束縛
	inPattern := k == "pattern"
	patternBinds := inPattern && binding
	if inPattern {
		for i := range n.ChildCount() {
			if isBindingKeyword(n.Child(i), w.src) {
				patternBinds = true
			}
		}
	}
	condList := k == "if_statement" || k == "guard_statement" || k == "while_statement" || k == "repeat_while_statement"
	clause := false
	depth := 0 // case パターンの `.pair(x, y)` の括弧と , も if 文の直下に平らに並ぶので、深さ 0 の , だけを条件の区切りにする
	firstNamed := true
	prevDot := false
	casePattern := false // `if case .timeout = e` の case から = までは pattern (case 名を畳まない)
	for i := range n.ChildCount() {
		c := n.Child(i)
		if classify(kindOf(c)) == classComment {
			continue
		}
		f := n.FieldNameForChild(uint32(i))
		if isBindingKeyword(c, w.src) {
			clause = true
		}
		if condList && !c.IsNamed() {
			switch w.text(c) {
			case "case":
				casePattern = true
			case "=":
				casePattern = false
			case "(":
				depth++
			case ")":
				depth--
			case ",":
				if depth == 0 {
					clause = false
				}
			}
		}
		// `for case .p(lo) in` の項目は値のパターン (束縛は中の let / var に任せる)。
		forItem := k == "for_statement" && f == "item" && !hasToken(c, "case")
		childBinds := patternBinds || (clause && (kindOf(c) == "pattern" || f == "bound_identifier")) || forItem
		cn := w.conv(c, n, f, childBinds, prevDot)
		if casePattern && c.IsNamed() {
			cn.Keep = true
		}
		prevDot = !c.IsNamed() && w.text(c) == "."
		if c.IsNamed() && firstNamed {
			firstNamed = false
			if k == "call_expression" || k == "constructor_expression" {
				cn.Keep = true
			}
		}
		if k == "macro_invocation" && kindOf(c) == "simple_identifier" {
			cn.Keep = true
		}
		if k == "statements" {
			cn.NoFold = true
		}
		out.Children = append(out.Children, cn)
	}
	if k == "call_expression" && len(out.Children) > 0 {
		out.Action = isActionCallee(n.NamedChild(0), w.src, w.static)
	}
	return out
}

func hasToken(n *sitter.Node, tok string) bool {
	for i := range n.ChildCount() {
		if c := n.Child(i); !c.IsNamed() && kindOf(c) == tok {
			return true
		}
	}
	return false
}

func isBindingKeyword(c *sitter.Node, src []byte) bool {
	if kindOf(c) == "value_binding_pattern" {
		return true
	}
	if c.IsNamed() {
		return false
	}
	t := c.Utf8Text(src)
	return t == "let" || t == "var"
}

// isActionCallee は callee が処理の呼び出しか。値を作るだけの呼び出し (大文字で始まる型の初期化 `ConnectionID(…)`・
// 暗黙のメンバー `.bucket(…)`) 以外は全部処理に数える (`_save`・“ `delete` “・英字以外の名前も処理)。
// 大文字で始まる処理 (`XCTUnwrap(…)`) は構文だけでは型の初期化と区別できず、値として畳まれる (docs/dupscan-detection.md の限界)。
func isActionCallee(c *sitter.Node, src []byte, static bool) bool {
	for c != nil {
		switch kindOf(c) {
		case "postfix_expression": // onDelete!() の !
			c = c.NamedChild(0)
			continue
		case "tuple_expression": // (deleteAll)()
			if c.NamedChildCount() == 1 {
				c = c.NamedChild(0)
				continue
			}
		case "prefix_expression": // 暗黙のメンバー
			if op := c.ChildByFieldName("operation"); op != nil && op.Utf8Text(src) == "." {
				return false
			}
		case "array_literal", "dictionary_literal": // [Int](repeating:count:) などの型の初期化
			return false
		case "simple_identifier":
			return !startsUpper(c.Utf8Text(src))
		case "navigation_expression":
			if s := c.ChildByFieldName("suffix"); s != nil {
				if id := s.ChildByFieldName("suffix"); id != nil {
					name := id.Utf8Text(src)
					if name == "init" {
						// ConnectionID.init(rawValue:) は型の初期化。self.init / super.init は委譲の処理。
						// static のメソッドの中の self は型そのものなので、self.init も型の初期化
						t := c.ChildByFieldName("target")
						return t != nil && ((kindOf(t) == "self_expression" && !static) || kindOf(t) == "super_expression")
					}
					return !startsUpper(name)
				}
			}
		}
		return true
	}
	return true
}

func startsUpper(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

// nameRole は simple_identifier の役割を、親・field 名・束縛の文脈から決める。
func (w *walker) nameRole(parent *sitter.Node, field string, binding, afterDot bool) tree.Role {
	if parent == nil {
		return tree.RoleName
	}
	if field == "bound_identifier" && binding {
		return tree.RoleDecl // `if case minValue = z` の minValue も bound_identifier だが、let / var が無ければ値
	}
	switch kindOf(parent) {
	case "value_argument_label":
		return tree.RoleLabel
	case "navigation_suffix":
		return tree.RoleMember
	case "capture_list_item":
		// `[k = store]` の k だけが新しい局所の名前。`[store]`・`[weak delegate]` と `=` の右辺は外側の名前の参照
		// (局所にすると、別々の変数を捕まえるクロージャが L1 で同じになる)。
		if field == "name" && hasToken(parent, "=") {
			return tree.RoleDecl
		}
		return tree.RoleName
	case "lambda_parameter", "parameter", "didset_clause", "willset_clause":
		return tree.RoleDecl // didSet(old) の old も引数
	case "pattern":
		// `case .light:` の light・`case let .some(z)` の some は enum の case 名。`case minValue:` は値。
		// 束縛 (let / var) の pattern の中の名前だけが局所の名前。
		if afterDot {
			return tree.RoleMember
		}
		if binding {
			return tree.RoleDecl
		}
	case "control_transfer_statement":
		// `break outer` / `continue outer` のラベル。
		if first := parent.Child(0); first != nil && (kindOf(first) == "break" || kindOf(first) == "continue") {
			return tree.RoleLabel
		}
	}
	return tree.RoleName
}
