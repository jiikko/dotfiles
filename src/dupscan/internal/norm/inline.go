package norm

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/fnv"
	"strings"

	"dupscan/internal/tree"
)

// Seq は単位の中の文の並び 1 つ: 節の子のうち、文の位置にある子 (adapter が NoFold を付けた子。Go の Block と case の文・Swift の statements の子) が
// 2 つ以上続く列。式の子の列 (引数・composite literal の要素) は文ではないので含めない。
// Shape は子ごとの形 (L1 の形で、局所の名前を番号にせず 1 つの記号にしたもの) のハッシュ。並びの候補を引く鍵にだけ使い、
// 一致の確定は WindowHash でする (局所の名前の結び付き (`a = b; f(a)` と `a = b; f(b)`) を Shape は区別しない)。
type Seq struct {
	Nodes  []*tree.Node
	Shape  []uint64
	Weight []int // 子ごとの名前つきの節の数
}

// Statements は単位の本体の一番上の文の並び。文の位置の子を持たない節で、名前つきの子が 1 つだけなら下へ降りる
// (Swift の function_body → statements)。
func Statements(u *tree.Unit) []*tree.Node {
	if u.Body == nil {
		return nil
	}
	n := u.Body
	for {
		var stmts, named []*tree.Node
		for _, c := range n.Children {
			if c.Role == tree.RoleToken {
				continue
			}
			named = append(named, c)
			if c.NoFold {
				stmts = append(stmts, c)
			}
		}
		if len(stmts) > 0 || len(named) != 1 {
			return stmts
		}
		n = named[0]
	}
}

// Seqs は単位の中の文の並びを全部返す (入れ子のブロックの中も)。
func Seqs(u *tree.Unit) []Seq {
	if u.Body == nil {
		return nil
	}
	sw := newShapeWriter(u)
	var out []Seq
	var walk func(*tree.Node) (uint64, int)
	walk = func(n *tree.Node) (uint64, int) {
		shapes := make([]uint64, len(n.Children))
		weights := make([]int, len(n.Children))
		total := countOwn(n)
		for i, c := range n.Children {
			shapes[i], weights[i] = walk(c)
			total += weights[i]
		}
		var run Seq
		flush := func() {
			if len(run.Nodes) >= 2 {
				out = append(out, run)
			}
			run = Seq{}
		}
		for i, c := range n.Children {
			switch {
			case c.Role == tree.RoleToken:
			case c.NoFold:
				run.Nodes = append(run.Nodes, c)
				run.Shape = append(run.Shape, shapes[i])
				run.Weight = append(run.Weight, weights[i])
			default:
				flush()
			}
		}
		flush()
		return sw.shape(n, shapes), total
	}
	walk(u.Body)
	return out
}

// SeqKey は形の列を 1 つの鍵にする。
func SeqKey(shapes []uint64) string {
	b := make([]byte, 8*len(shapes))
	for i, s := range shapes {
		binary.LittleEndian.PutUint64(b[8*i:], s)
	}
	return string(b)
}

// WindowHasher は単位 u の文の並びを L1 の形でハッシュする関数を返す。局所の名前の番号は並びの中で初めて出た順に 1 から振り直す
// (並びの外 (関数の引数・前の文) で何番目に出たかに依らず、切り出した並びどうしを比べられる)。
// 局所の名前の表は単位ごとに 1 回だけ作る (並びごとに作ると、同じ形の並びが多い単位で単位の大きさの 2 乗になる)。
func WindowHasher(u *tree.Unit) func(nodes []*tree.Node) string {
	locals := localNames(u)
	return func(nodes []*tree.Node) string {
		h := sha256.New()
		out := bufio.NewWriter(h)
		w := &writer{out: out, level: L1, locals: locals, nums: map[string]int{}}
		for _, n := range nodes {
			w.write(n, false)
		}
		_ = out.Flush()
		return hex.EncodeToString(h.Sum(nil)[:16])
	}
}

func countOwn(n *tree.Node) int {
	if n.Role == tree.RoleToken {
		return 0
	}
	return 1
}

type shapeWriter struct {
	head strings.Builder
	out  *bufio.Writer
	w    *writer
}

func newShapeWriter(u *tree.Unit) *shapeWriter {
	sw := &shapeWriter{}
	sw.out = bufio.NewWriter(&sw.head)
	sw.w = &writer{out: sw.out, level: L1, locals: localNames(u), nums: map[string]int{}, anon: true}
	return sw
}

func (sw *shapeWriter) shape(n *tree.Node, children []uint64) uint64 {
	sw.head.Reset()
	sw.w.head(n)
	_ = sw.out.Flush()
	h := fnv.New64a()
	_, _ = h.Write([]byte(sw.head.String()))
	var b [8]byte
	for _, c := range children {
		binary.LittleEndian.PutUint64(b[:], c)
		_, _ = h.Write(b[:])
	}
	_, _ = h.Write([]byte(")"))
	return h.Sum64()
}
