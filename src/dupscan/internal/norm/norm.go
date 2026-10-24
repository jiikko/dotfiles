// Package norm は、単位の本体を段 (L1 / L2 / L3) ごとに正規化してハッシュを取る。
//
//   - L1: 局所の名前 (引数・ローカル変数・クロージャの引数) を、単位の中で初めて出た順の番号にする。それ以外の名前とリテラルは残す
//   - L2: L1 に加えて、リテラルを種類ごとの記号にする
//   - L3: L2 に加えて、局所の名前を参照しない最大の式を 1 つの記号に畳む (callee・代入の左辺の下と、文として置かれた式そのものは畳まない)
package norm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"dupscan/internal/tree"
)

// Level は正規化の段。
type Level int

// 段。
const (
	L1 Level = 1
	L2 Level = 2
	L3 Level = 3
)

// Levels は細かい順の全部の段。
var Levels = []Level{L1, L2, L3}

func (l Level) String() string { return "L" + strconv.Itoa(int(l)) }

// ParseLevel は "L1" / "L2" / "L3" を段にする。
func ParseLevel(s string) (Level, error) {
	for _, l := range Levels {
		if strings.TrimSpace(s) == l.String() {
			return l, nil
		}
	}
	return 0, fmt.Errorf("知らない段 %q (L1 / L2 / L3)", s)
}

// Result は 1 つの単位の正規化の結果。
type Result struct {
	Hash map[Level]string
	// Nodes は本体の名前つきの節の数 (下限の判定に使う)。
	Nodes int
	// L3Trivial は、本体が局所の名前を 1 つも参照しないことを表す。L3 では値の位置がどれも同じ記号に畳まれ、
	// 同じ型を返す同じ形の呼び出しならどれも一致するので、L3 の組には入れない。
	L3Trivial bool
	// Forms は段ごとに潰した形 (explain 用)。
	Forms map[Level]string
}

// Normalize は単位を正規化する。
func Normalize(u *tree.Unit) Result {
	locals := localNames(u)
	refs := map[*tree.Node]bool{}
	markRefs(u.Body, locals, refs)
	acts := map[*tree.Node]bool{}
	markActions(u.Body, acts)
	r := Result{Hash: map[Level]string{}, Forms: map[Level]string{}, Nodes: countNodes(u.Body)}
	r.L3Trivial = !refs[u.Body]
	for _, l := range Levels {
		w := &writer{level: l, locals: locals, refs: refs, acts: acts, nums: map[string]int{}}
		// 引数は宣言の順に番号を先に振る (本体に出た順だけだと `a - b` と `b - a` が同じになる)。
		for _, p := range u.Params {
			if _, ok := w.nums[p]; !ok {
				w.nums[p] = len(w.nums) + 1
			}
		}
		w.b.WriteString("(ret ")
		w.b.WriteString(strconv.Quote(u.ReturnType))
		w.b.WriteString(")")
		w.write(u.Body, false)
		form := w.b.String()
		sum := sha256.Sum256([]byte(form))
		r.Hash[l] = hex.EncodeToString(sum[:16])
		r.Forms[l] = form
	}
	return r
}

// captureKey は捕まえた名前を局所の名前の表に入れるときの鍵 (番号にする局所の名前と混ぜないよう、別の鍵にする)。
func captureKey(s string) string { return "\x00capture:" + s }

func localNames(u *tree.Unit) map[string]bool {
	m := map[string]bool{}
	for _, p := range u.Params {
		m[p] = true
	}
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		if n.Role == tree.RoleDecl {
			m[n.Text] = true
		}
		if n.Capture {
			m[captureKey(n.Text)] = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(u.Body)
	return m
}

func isLocal(n *tree.Node, locals map[string]bool) bool {
	return (n.Role == tree.RoleName || n.Role == tree.RoleDecl) && locals[n.Text]
}

// isImplicitParam は `$0` などの暗黙のクロージャの引数。位置に意味があるので番号にせず字面を残すが、
// 畳むかの判定では局所の名前として数える (`$isOn` のような projected value は含めない)。
func isImplicitParam(n *tree.Node) bool {
	if n.Role != tree.RoleName || len(n.Text) < 2 || n.Text[0] != '$' {
		return false
	}
	for _, c := range n.Text[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func markRefs(n *tree.Node, locals map[string]bool, refs map[*tree.Node]bool) bool {
	r := isLocal(n, locals) || isImplicitParam(n) || (n.Role == tree.RoleName && locals[captureKey(n.Text)])
	for _, c := range n.Children {
		if markRefs(c, locals, refs) {
			r = true
		}
	}
	refs[n] = r
	return r
}

func markActions(n *tree.Node, acts map[*tree.Node]bool) bool {
	a := n.Action
	for _, c := range n.Children {
		if markActions(c, acts) {
			a = true
		}
	}
	acts[n] = a
	return a
}

func countNodes(n *tree.Node) int {
	c := 0
	if n.Role != tree.RoleToken {
		c = 1
	}
	for _, ch := range n.Children {
		c += countNodes(ch)
	}
	return c
}

type writer struct {
	b      strings.Builder
	level  Level
	locals map[string]bool
	refs   map[*tree.Node]bool
	acts   map[*tree.Node]bool
	nums   map[string]int
}

func (w *writer) write(n *tree.Node, keep bool) {
	if w.level >= L3 && !keep && !n.Keep && n.Expr && !n.NoFold && !w.refs[n] && !w.acts[n] {
		w.b.WriteString("(#F)")
		return
	}
	w.b.WriteByte('(')
	w.b.WriteString(n.Kind)
	switch {
	case isLocal(n, w.locals):
		num, ok := w.nums[n.Text]
		if !ok {
			num = len(w.nums) + 1
			w.nums[n.Text] = num
		}
		w.b.WriteString(" $")
		w.b.WriteString(strconv.Itoa(num))
	case n.Role == tree.RoleLiteral:
		if w.level >= L2 {
			w.b.WriteString(" #lit:")
			w.b.WriteString(n.LitKind)
		} else {
			w.b.WriteString(" ")
			w.b.WriteString(strconv.Quote(n.Text))
		}
	case n.Role != tree.RoleStruct:
		w.b.WriteString(" ")
		w.b.WriteString(strconv.Quote(n.Text))
	}
	for _, c := range n.Children {
		w.write(c, keep || n.Keep)
	}
	w.b.WriteByte(')')
}
