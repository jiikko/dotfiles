package norm

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"dupscan/internal/tree"
)

// FormNode retains native child boundaries; token kinds can themselves be parentheses.
// Header is the exact L1 node prefix, without the final close or any children.
type FormNode struct {
	Header   string
	Literal  bool
	Children []*FormNode
}

// L1View uses the normalizer's writer and unit-wide numbering, without folding.
// Limits bound the extra view allocation; callers must also bound selected source input.
func L1View(u *tree.Unit, maxBytes int) ([]*FormNode, error) {
	if u.Body == nil {
		return nil, errors.New("L1 body is unavailable")
	}
	type pending struct {
		node  *tree.Node
		depth int
	}
	stack := []pending{{u.Body, 1}}
	count := 0
	for len(stack) > 0 {
		at := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		if at.depth > 256 {
			return nil, errors.New("L1 nesting exceeds 256 nodes")
		}
		if count > maxBytes/2 {
			return nil, fmt.Errorf("L1 view exceeds %d bytes", maxBytes)
		}
		for _, child := range at.node.Children {
			stack = append(stack, pending{child, at.depth + 1})
		}
	}
	var head strings.Builder
	out := bufio.NewWriter(&head)
	w := &writer{out: out, level: L1, locals: localNames(u), nums: map[string]int{}}
	for _, p := range u.Params {
		if _, ok := w.nums[p]; !ok {
			w.nums[p] = len(w.nums) + 1
		}
	}
	ret := &FormNode{Header: "(ret " + strconv.Quote(u.ReturnType)}
	remaining := maxBytes - len(ret.Header) - 1
	var visit func(*tree.Node) (*FormNode, error)
	visit = func(n *tree.Node) (*FormNode, error) {
		// Reject before writing: even the prefix cannot fit in the remaining budget.
		if remaining < 2 || len(n.Kind)+2 > remaining {
			return nil, fmt.Errorf("L1 view exceeds %d bytes", maxBytes)
		}
		shallow := *n
		shallow.Children = nil
		head.Reset()
		w.write(&shallow, false)
		if err := out.Flush(); err != nil {
			return nil, err
		}
		remaining -= head.Len()
		if remaining < 0 {
			return nil, fmt.Errorf("L1 view exceeds %d bytes", maxBytes)
		}
		view := &FormNode{Header: strings.TrimSuffix(head.String(), ")"), Literal: n.Role == tree.RoleLiteral}
		for _, child := range n.Children {
			v, err := visit(child)
			if err != nil {
				return nil, err
			}
			view.Children = append(view.Children, v)
		}
		return view, nil
	}
	body, err := visit(u.Body)
	if err != nil {
		return nil, err
	}
	return []*FormNode{ret, body}, nil
}
