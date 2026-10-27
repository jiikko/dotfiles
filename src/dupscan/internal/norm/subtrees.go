package norm

import (
	"bufio"
	"crypto/sha256"
	"strings"

	"dupscan/internal/tree"
)

// SubtreeBag holds L1 Merkle fingerprints, weighted by subtree size.
// Local numbering is unit-wide: inserting a local can change later fingerprints.
type SubtreeBag map[[32]byte]int

// Subtrees preserves L1's names, types, literals and actions. It does not fold.
// Nested subtrees contribute independently; the score is not semantic equivalence.
func Subtrees(u *tree.Unit, minSize int) SubtreeBag {
	bag := SubtreeBag{}
	var head strings.Builder
	out := bufio.NewWriter(&head)
	w := &writer{out: out, level: L1, locals: localNames(u), nums: map[string]int{}}
	for _, p := range u.Params {
		if _, ok := w.nums[p]; !ok {
			w.nums[p] = len(w.nums) + 1
		}
	}
	var walk func(*tree.Node) ([32]byte, int)
	walk = func(n *tree.Node) ([32]byte, int) {
		head.Reset()
		w.head(n)
		_ = out.Flush()
		h := sha256.New()
		_, _ = h.Write([]byte(head.String()))
		size := 1
		for _, c := range n.Children {
			digest, count := walk(c)
			_, _ = h.Write(digest[:])
			size += count
		}
		_, _ = h.Write([]byte(")"))
		var digest [32]byte
		copy(digest[:], h.Sum(nil))
		if size >= minSize {
			bag[digest] += size
		}
		return digest, size
	}
	if u.Body != nil {
		walk(u.Body)
	}
	return bag
}

// Dice returns weighted multiset overlap; duplicate occurrences are not a set.
func (a SubtreeBag) Dice(b SubtreeBag) (score float64, shared int) {
	total := 0
	for hash, weight := range a {
		total += weight
		shared += min(weight, b[hash])
	}
	for _, weight := range b {
		total += weight
	}
	if total > 0 {
		score = 2 * float64(shared) / float64(total)
	}
	return score, shared
}
