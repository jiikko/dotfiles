// Package norm は、単位の本体を段 (L1 / L2 / L3) ごとに正規化してハッシュを取る。
//
//   - L1: 局所の名前 (引数・ローカル変数・クロージャの引数) を、単位の中で初めて出た順の番号にする。それ以外の名前とリテラルは残す
//   - L2: L1 に加えて、リテラルを種類ごとの記号にする
//   - L3: L2 に加えて、局所の名前を参照しない最大の式を 1 つの記号に畳む (callee・代入の左辺の下と、文として置かれた式そのものは畳まない)
//   - L1m: L1 に加えて、メンバー名 (`a.b` の `b`) を記号にする (リテラルは残す)。`os.Stdout` と `os.Stderr` だけが違う写しを見つける。
//     既定の段 (Levels) には入れない: 明示 (-levels L1,L2,L1m) したときだけ組を作る。Swift は修飾子の連鎖 (`.padding()` `.font()`) もメンバー名なので、
//     view の本体が大量に一致する
package norm

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"dupscan/internal/tree"
)

// Level は正規化の段。
type Level int

// 段。
const (
	L1 Level = 1
	L2 Level = 2
	L3 Level = 3
	// LM は L1 に加えてメンバー名 (`a.b` の `b`) を畳む段 (L1m)。Levels には入れない。-levels で明示したときだけ組を作る。
	LM Level = 4
)

// Levels は細かい順の全部の段。
var Levels = []Level{L1, L2, L3}

func (l Level) String() string {
	if l == LM {
		return "L1m"
	}
	return "L" + strconv.Itoa(int(l))
}

// ParseLevel は "L1" / "L2" / "L3" / "L1m" を段にする。
func ParseLevel(s string) (Level, error) {
	for _, l := range append(append([]Level(nil), Levels...), LM) {
		if strings.TrimSpace(s) == l.String() {
			return l, nil
		}
	}
	return 0, fmt.Errorf("知らない段 %q (L1 / L2 / L3 / L1m)", s)
}

// Result は 1 つの単位の正規化の結果。
type Result struct {
	Hash map[Level]string
	// Nodes は本体の名前つきの節の数 (下限の判定に使う)。
	Nodes int
	// L3Trivial は、本体が局所の名前を 1 つも参照しないことを表す。L3 では値の位置がどれも同じ記号に畳まれ、
	// 同じ型を返す同じ形の呼び出しならどれも一致するので、L3 の組には入れない。
	L3Trivial bool
	// Forms は段ごとに潰した形 (explain 用。NormalizeWithForms のときだけ入る)。
	Forms map[Level]string
}

// Normalize は単位を正規化する。
func Normalize(u *tree.Unit) Result { return normalize(u, false) }

// NormalizeWithForms は Normalize に加えて、段ごとに潰した形の文字列を Forms に入れる (explain とテストの失敗の表示用)。
func NormalizeWithForms(u *tree.Unit) Result { return normalize(u, true) }

// scratch は normalize が単位ごとに作っていた作業領域 (節の旗の表 2 つと書き出しの buffer)。使い回すと、1 単位あたりの確保が
// 大きく減る (src の report で確保の約 3 割が、この 3 つだった)。Result には載せないので、返した後に使い回してよい。
type scratch struct {
	refs, acts map[*tree.Node]bool
	out        *bufio.Writer
	quoted     []byte // strconv.AppendQuote の作業 buffer (Quote は呼ぶたびに string を確保する。節ごと・段ごとに呼ぶので数が多い)
}

// scratchKeepMax は、これより大きく育った表は pool に戻さない (巨大な単位 1 つが、小さい単位の走査の間ずっと大きい表を抱えない)。
const scratchKeepMax = 1 << 14

// quotedKeepMax は、引用の作業 buffer をこの byte 数を超えて育てたまま pool に戻さない。数節の関数に巨大な文字列リテラルがあると、
// 表は小さいままで buffer だけが育つ。後続の `[:0]` は容量を縮めないので、要素数でなく容量で見る。
const quotedKeepMax = 1 << 16

var scratchPool = sync.Pool{New: func() any {
	return &scratch{refs: map[*tree.Node]bool{}, acts: map[*tree.Node]bool{}, out: bufio.NewWriterSize(io.Discard, 4096)}
}}

func (s *scratch) release() {
	if len(s.refs) > scratchKeepMax || len(s.acts) > scratchKeepMax {
		return
	}
	if cap(s.quoted) > quotedKeepMax {
		s.quoted = nil
	}
	clear(s.refs)
	clear(s.acts)
	s.out.Reset(io.Discard) // 書き込み先 (sha256 / strings.Builder) への参照を残さない
	scratchPool.Put(s)
}

// normalize は段ごとの形を sha256 へ直接流してハッシュを取る。形の文字列は withForms のときだけ作る
// (全部の単位で形を持つと、repo 全体の走査で一時的な確保が数百 MB になる)。
func normalize(u *tree.Unit, withForms bool) Result {
	locals := localNames(u)
	sc := scratchPool.Get().(*scratch)
	defer sc.release()
	refs, acts := sc.refs, sc.acts
	markRefs(u.Body, locals, refs)
	markActions(u.Body, acts)
	r := Result{Hash: map[Level]string{}, Nodes: countNodes(u.Body)}
	if withForms {
		r.Forms = map[Level]string{}
	}
	r.L3Trivial = !refs[u.Body]
	h := sha256.New()
	out := sc.out
	for _, l := range append(append([]Level(nil), Levels...), LM) {
		h.Reset()
		var form strings.Builder
		if withForms {
			out.Reset(io.MultiWriter(h, &form))
		} else {
			out.Reset(h)
		}
		w := &writer{out: out, level: l, locals: locals, refs: refs, acts: acts, nums: map[string]int{}, quoted: &sc.quoted}
		// 引数は宣言の順に番号を先に振る (本体に出た順だけだと `a - b` と `b - a` が同じになる)。
		for _, p := range u.Params {
			if _, ok := w.nums[p]; !ok {
				w.nums[p] = len(w.nums) + 1
			}
		}
		w.str("(ret ")
		w.quote(u.ReturnType)
		w.str(")")
		w.write(u.Body, false)
		if err := out.Flush(); err != nil {
			panic(err) // sha256 と strings.Builder への書き込みは失敗しない
		}
		r.Hash[l] = hex.EncodeToString(h.Sum(nil)[:16])
		if withForms {
			r.Forms[l] = form.String()
		}
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
	out    *bufio.Writer
	level  Level
	locals map[string]bool
	refs   map[*tree.Node]bool
	acts   map[*tree.Node]bool
	nums   map[string]int
	quoted *[]byte // nil なら writer 自身の buf を使う (scratch を持たない呼び出し元用)
	buf    []byte
}

// str と byte は形を書き出す。bufio の書き込みの失敗は Flush が返すので、ここでは見ない (先は sha256 と strings.Builder で失敗しない)。
func (w *writer) str(s string) { _, _ = w.out.WriteString(s) }

// quote は strconv.Quote(s) と同じ並びを、確保なしで書き出す (作業 buffer は単位をまたいで使い回す)。
func (w *writer) quote(s string) {
	if w.quoted == nil {
		w.quoted = &w.buf
	}
	*w.quoted = strconv.AppendQuote((*w.quoted)[:0], s)
	_, _ = w.out.Write(*w.quoted)
}
func (w *writer) byte(c byte) { _ = w.out.WriteByte(c) }

func (w *writer) write(n *tree.Node, keep bool) {
	if w.level == L3 && !keep && !n.Keep && n.Expr && !n.NoFold && !w.refs[n] && !w.acts[n] {
		w.str("(#F)")
		return
	}
	w.head(n)
	for _, c := range n.Children {
		w.write(c, keep || n.Keep)
	}
	w.byte(')')
}

func (w *writer) head(n *tree.Node) {
	w.byte('(')
	w.str(n.Kind)
	switch {
	case isLocal(n, w.locals):
		num, ok := w.nums[n.Text]
		if !ok {
			num = len(w.nums) + 1
			w.nums[n.Text] = num
		}
		w.str(" $")
		w.str(strconv.Itoa(num))
	case n.Role == tree.RoleLiteral:
		if w.level == L2 || w.level == L3 {
			w.str(" #lit:")
			w.str(n.LitKind)
		} else {
			w.str(" ")
			w.quote(n.Text)
		}
	case n.Role == tree.RoleMember && w.level == LM:
		w.str(" #m")
	case n.Role != tree.RoleStruct:
		w.str(" ")
		w.quote(n.Text)
	}
}
