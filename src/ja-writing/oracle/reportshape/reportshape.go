// Package reportshape は report-shape.json の記録 (oracle.ReportShapeFile) を読む側の道具。書き換えた dict を作り、口の関数に通して本家の結果と比べる。
// lint と diff の両方の test が使う (oracle は internal/pyjson の test から import されるので、pyjson を使う道具をここに分ける)。
package reportshape

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
)

// Stats は Check が数えた件数。
type Stats struct {
	Mutations int // 通した書き換えの数 (書き換えない基準を含む)
	Raised    int // 本家が例外になった呼び出しの数
	Compared  int // 例外にならず、戻り値を比べた呼び出しの数
}

// variants は py/record_report_shape.py の VARIANTS の名前の順 (書き換えの並びはこの順)。
var variants = []string{"del", "null", "empty_str", "str", "empty_list", "empty_dict", "zero", "neg", "true", "false", "float", "big", "huge", "case_str"}

// maxDepth は py/record_report_shape.py の MAX_DEPTH (位置は最上位から maxDepth+1 段まで)。
const maxDepth = 4

// Mutation は書き換え 1 つ。Path の要素は dict の鍵 (string) か list の先頭 (int の 0)。Variant が "base" なら書き換えない。
type Mutation struct {
	Path    []any
	Variant string
}

// Plans は基準の書き換えの並び (py/record_report_shape.py の plans と同じ順)。
func Plans(base *pyjson.Object) []Mutation {
	out := []Mutation{{Variant: "base"}}
	var walk func(v pyjson.Value, prefix []any, depth int)
	add := func(path []any) {
		for _, name := range variants {
			out = append(out, Mutation{Path: path, Variant: name})
		}
	}
	walk = func(v pyjson.Value, prefix []any, depth int) {
		if depth > maxDepth {
			return
		}
		switch x := v.(type) {
		case *pyjson.Object:
			for _, key := range x.Keys() {
				path := append(slices.Clone(prefix), key)
				add(path)
				child, _ := x.Get(key)
				walk(child, path, depth+1)
			}
		case []pyjson.Value:
			if len(x) > 0 {
				path := append(slices.Clone(prefix), 0)
				add(path)
				walk(x[0], path, depth+1)
			}
		}
	}
	walk(base, nil, 0)
	return out
}

// planDigest は書き換えの並びの digest (本家の json.dumps(list(path)) + " " + 種類 を "\n" でつないだもの)。
func planDigest(plans []Mutation) (string, error) {
	lines := make([]string, len(plans))
	for i, m := range plans {
		path := make([]pyjson.Value, len(m.Path))
		for j, step := range m.Path {
			if n, ok := step.(int); ok {
				step = int64(n)
			}
			path[j] = step
		}
		text, err := pyjson.Dumps(path, pyjson.Options{})
		if err != nil {
			return "", err
		}
		lines[i] = text + " " + m.Variant
	}
	return digest(strings.Join(lines, "\n")), nil
}

// Check は prefix ("lint/" か "diff/") の基準の書き換えを全部 call に通し、本家の記録と食い違ったものを返す。call は関数の添字 (記録の関数の順) と
// dict を受け、戻り値の文字列 (本家の driver が SHA-256 を取ったのと同じ文字列) か error を返す。条件は「本家が例外 ⇔ error」と、例外にならないときの戻り値の一致で、
// 書き換えごとの digest と、基準ごとの例外と戻り値の数で比べる。書き換えの並びが記録と違うときは比べずに error を返す。
func Check(g oracle.ReportShapeGolden, prefix string, fns int, call func(i int, d *pyjson.Object) (string, error)) (Stats, []string, error) {
	var st Stats
	var bad []string
	for _, b := range g.Bases {
		if !strings.HasPrefix(b.Name, prefix) {
			continue
		}
		v, err := pyjson.Loads(string(b.D))
		if err != nil {
			return st, nil, fmt.Errorf("%s: %w", b.Name, err)
		}
		base, ok := v.(*pyjson.Object)
		if !ok {
			return st, nil, fmt.Errorf("%s: 基準が dict ではありません", b.Name)
		}
		plans := Plans(base)
		plan, err := planDigest(plans)
		if err != nil {
			return st, nil, fmt.Errorf("%s: %w", b.Name, err)
		}
		if plan != b.Plan || len(plans) != len(b.Digests) {
			return st, nil, fmt.Errorf("%s: 書き換えの並びが記録と違う (Go は %d 件・%s、記録は %d 件・%s)", b.Name, len(plans), plan, len(b.Digests), b.Plan)
		}
		var raised, compared int
		for i, m := range plans {
			results := make([]string, fns)
			for fn := range fns {
				d, err := Input(base, m)
				if err != nil {
					return st, nil, fmt.Errorf("%s 書き換え %d %v %s: %w", b.Name, i, m.Path, m.Variant, err)
				}
				got, err := call(fn, d)
				if err != nil {
					raised++
					results[fn] = "E"
					continue
				}
				compared++
				results[fn] = digest(got)
			}
			st.Mutations++
			if want := b.Digests[i]; digest(strings.Join(results, "\n")) != want {
				bad = append(bad, fmt.Sprintf("%s 書き換え %d %v %s: Go の結果 %v が記録 %s と違う (本家の結果は go run ./oracle/cmd/golden -shape-detail '%s:%d')",
					b.Name, i, m.Path, m.Variant, results, want, b.Name, i))
			}
		}
		if raised != b.Raised || compared != b.Compared {
			bad = append(bad, fmt.Sprintf("%s: Go は error %d・戻り値 %d、本家は例外 %d・戻り値 %d", b.Name, raised, compared, b.Raised, b.Compared))
		}
		st.Raised += raised
		st.Compared += compared
	}
	return st, bad, nil
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// Input は基準を写して m の位置を書き換えた dict を作る (基準は変えない)。
func Input(base *pyjson.Object, m Mutation) (*pyjson.Object, error) {
	d, _ := deepCopy(base).(*pyjson.Object)
	if m.Variant == "base" {
		return d, nil
	}
	value, err := variant(m.Variant)
	if err != nil {
		return nil, err
	}
	if len(m.Path) == 0 {
		return nil, errors.New("書き換えの位置が空です")
	}
	parent := pyjson.Value(d)
	for _, step := range m.Path[:len(m.Path)-1] {
		if parent, err = descend(parent, step); err != nil {
			return nil, err
		}
	}
	last := m.Path[len(m.Path)-1]
	switch p := parent.(type) {
	case *pyjson.Object:
		key, ok := last.(string)
		if !ok {
			return nil, fmt.Errorf("dict の位置が文字列ではありません: %v", last)
		}
		if m.Variant == "del" {
			p.Delete(key)
		} else {
			p.Set(key, value)
		}
	case []pyjson.Value:
		if last != 0 {
			return nil, fmt.Errorf("list の位置が 0 ではありません: %v", last)
		}
		if m.Variant == "del" {
			// 先頭を消した list は長さが変わるので、親の欄に置き直す
			return d, replace(d, m.Path[:len(m.Path)-1], p[1:])
		}
		p[0] = value
	default:
		return nil, fmt.Errorf("dict か list でない値の中は書き換えられません: %T", parent)
	}
	return d, nil
}

// deepCopy は dict と list を作り直した写し (口の関数が受けた dict を書き換えても、次の呼び出しの基準が変わらない)。
func deepCopy(v pyjson.Value) pyjson.Value {
	switch x := v.(type) {
	case *pyjson.Object:
		out := pyjson.NewObject()
		for _, key := range x.Keys() {
			child, _ := x.Get(key)
			out.Set(key, deepCopy(child))
		}
		return out
	case []pyjson.Value:
		out := make([]pyjson.Value, len(x))
		for i, child := range x {
			out[i] = deepCopy(child)
		}
		return out
	}
	return v
}

func descend(v pyjson.Value, step any) (pyjson.Value, error) {
	switch x := v.(type) {
	case *pyjson.Object:
		key, _ := step.(string)
		child, ok := x.Get(key)
		if !ok {
			return nil, fmt.Errorf("鍵 %v がありません", step)
		}
		return child, nil
	case []pyjson.Value:
		if step != 0 || len(x) == 0 {
			return nil, fmt.Errorf("list の位置 %v はたどれません", step)
		}
		return x[0], nil
	}
	return nil, fmt.Errorf("%T の中はたどれません", v)
}

func replace(d *pyjson.Object, path []any, value pyjson.Value) error {
	if len(path) == 0 {
		return errors.New("最上位は list ではありません")
	}
	parent := pyjson.Value(d)
	var err error
	for _, step := range path[:len(path)-1] {
		if parent, err = descend(parent, step); err != nil {
			return err
		}
	}
	switch p := parent.(type) {
	case *pyjson.Object:
		key, _ := path[len(path)-1].(string)
		p.Set(key, value)
	case []pyjson.Value:
		p[0] = value
	default:
		return fmt.Errorf("%T の中は書き換えられません", parent)
	}
	return nil
}

// variant は py/record_report_shape.py の VARIANTS の値 (del は消すので値を使わない)。
func variant(name string) (pyjson.Value, error) {
	switch name {
	case "del", "null":
		return nil, nil
	case "empty_str":
		return "", nil
	case "str":
		return "3", nil
	case "empty_list":
		return []pyjson.Value{}, nil
	case "empty_dict":
		return pyjson.NewObject(), nil
	case "zero":
		return int64(0), nil
	case "neg":
		return int64(-2), nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "float":
		return 2.0, nil
	case "big":
		return pyjson.Loads("10000000000000000000000000")
	case "huge":
		return pyjson.Loads("1" + strings.Repeat("0", 400))
	case "case_str":
		return pyjson.Loads(`"\u00df\ufb01\u0149\ud800x"`)
	}
	return nil, fmt.Errorf("書き換えの種類 %q を知りません", name)
}
