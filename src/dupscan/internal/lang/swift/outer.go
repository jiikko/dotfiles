package swift

import "dupscan/internal/tree"

// markOuter は本体 body の参照のうち、その位置で効いている局所の名前 (引数 params と、それまでに効いた宣言) に無いものを
// Outer にする。norm は局所の名前を綴りで決めるので、印が無いと `let title = title.uppercased()` の右辺 (プロパティ) と
// 別のプロパティを読む写しが同じ形になる。
// 宣言は、同じ並びで後ろに `=` (for 文では `in`) があればその右辺を読み終えてから効く (`let x = x…`・`if let x = x`・
// `for x in x.items`)。無ければその場で効く (引数・クロージャの引数。クロージャの `{ x in … }` の `in` は右辺ではない)。
// 宣言は囲む scope (scopeKinds) の中だけで効き、scope を出たら外す。tree-sitter-swift の木は if / guard の条件・then・else を
// 同じ並びに平らに置くので、並びの中の `else` で切り替える: if は else から先で条件の束縛を外し、guard は else の本体でだけ外して
// guard の後ろ (親の並びの続き) では効かせる。
// (Swift の tree-sitter に名前解決が無いので近似。Go は parser の名前解決で決めている)。
func markOuter(body *tree.Node, params []string) {
	known := make(map[string]bool, len(params))
	for _, p := range params {
		known[p] = true
	}
	var visit func(n *tree.Node, deferred *[]string)
	visit = func(n *tree.Node, deferred *[]string) {
		switch n.Role {
		case tree.RoleName:
			if !known[n.Text] {
				n.Outer = true
			}
		case tree.RoleDecl:
			// shorthand の `if let x` は、宣言の子に x の参照を持つ。参照は束縛より前に読まれるので、宣言を効かせる前に決める
			for _, c := range n.Children {
				visit(c, nil)
			}
			if deferred != nil {
				*deferred = append(*deferred, n.Text)
			} else {
				known[n.Text] = true
			}
			return
		}
		var snap, afterGuard map[string]bool
		scope := scopeKinds[n.Kind]
		if scope {
			snap = cloneSet(known)
		}
		var pending []string
		activateAt, assignAt := -1, -1
		for i, c := range n.Children {
			if scope && c.Role == tree.RoleToken && c.Text == "else" && (n.Kind == "if_statement" || n.Kind == "guard_statement") {
				if n.Kind == "guard_statement" {
					afterGuard = cloneSet(known) // 条件の束縛は guard の後ろで効く
				}
				known = cloneSet(snap) // else の側には条件の束縛が届かない
			}
			if i < assignAt { // 宣言から `=` までの間 (型注釈・`?`・pattern の残り) の宣言も、右辺の後で効く
				visit(c, &pending)
				continue
			}
			if j := assignAfterDecl(n, i); j > i {
				visit(c, &pending)
				assignAt, activateAt = j, j+1 // `=` の次の子 (右辺) を読み終えたら効く
				continue
			}
			visit(c, deferred)
			if i == activateAt {
				for _, name := range pending {
					known[name] = true
				}
				pending, activateAt = nil, -1
			}
		}
		for _, name := range pending { // 右辺が無いまま並びが終わった
			known[name] = true
		}
		switch {
		case afterGuard != nil:
			known = afterGuard
		case scope:
			known = snap
		}
	}
	visit(body, nil)
}

// scopeKinds は、中で宣言した名前が外へ届かない節 (block・closure・ループ・switch の case・catch・if / guard・入れ子の関数・
// 局所の変数の didSet / willSet・関数の中の局所の型の本体 (struct / class / actor は class_body、enum は enum_class_body))。
// 文法の節のうち中に宣言を持ちうるものは、全部 scope_kinds_test.go の scopeKindDecisions で scope / 素通り / 関数の外に分類してある
// (局所の型の init / subscript の引数は、型の本体の中の後ろのメソッドへ漏れないよう、それ自身を scope にする。
// repeat-while の本体は statements で閉じるので素通り)。
var scopeKinds = map[string]bool{
	"statements": true, "lambda_literal": true, "for_statement": true, "while_statement": true,
	"switch_entry": true, "catch_block": true, "if_statement": true, "guard_statement": true, "function_declaration": true,
	"class_body": true, "enum_class_body": true, "didset_clause": true, "willset_clause": true,
	"init_declaration": true, "subscript_declaration": true,
}

func cloneSet(m map[string]bool) map[string]bool {
	c := make(map[string]bool, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// assignAfterDecl は n.Children[i] が宣言を含むとき、同じ並びで後ろにある `=` (for 文では `in`) の token の位置を返す。
// 間に型注釈・`?`・pattern の閉じが挟まってよい (`let x: T = x`・`if case let x? = x`・`.some(let x) = x`)。
// 括弧の外の `,` で止める (`if let a, let b = y` の `=` は b の束縛のもの)。conv は if / guard / while の条件の case パターンの
// `(` `,` `)` を平らに並べるので、深さを数えないと `.pair(let x, let w) = x` の括弧の中の `,` で止まってしまう。無ければ -1。
func assignAfterDecl(n *tree.Node, i int) int {
	if !containsDecl(n.Children[i]) {
		return -1
	}
	depth := 0
	for _, c := range n.Children[:i] {
		depth += parenDelta(c)
	}
	for j := i + 1; j < len(n.Children); j++ {
		c := n.Children[j]
		depth += parenDelta(c)
		if c.Role != tree.RoleToken {
			continue
		}
		switch {
		case c.Text == "=" || (c.Text == "in" && n.Kind == "for_statement"):
			return j
		case c.Text == "," && depth <= 0:
			return -1
		}
	}
	return -1
}

func parenDelta(c *tree.Node) int {
	if c.Role != tree.RoleToken {
		return 0
	}
	switch c.Text {
	case "(":
		return 1
	case ")":
		return -1
	}
	return 0
}

func containsDecl(n *tree.Node) bool {
	if n.Role == tree.RoleDecl {
		return true
	}
	for _, c := range n.Children {
		if containsDecl(c) {
			return true
		}
	}
	return false
}
