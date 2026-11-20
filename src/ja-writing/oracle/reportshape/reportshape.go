// Package reportshape は report-shape.json の記録 (oracle.ReportShapeFile) を読む側の道具。書き換えた dict を作り、口の関数に通して本家の結果と比べる。
// lint と diff の両方の test が使う (oracle は internal/pyjson の test から import されるので、pyjson を使う道具をここに分ける)。
package reportshape

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
)

// Stats は Check が数えた件数。
type Stats struct {
	Mutations int // 通した書き換えの数
	Raised    int // 本家が例外になった呼び出しの数
	Compared  int // 例外にならず、戻り値を比べた呼び出しの数
}

// Check は prefix ("lint/" か "diff/") の基準の書き換えを全部 call に通し、本家の記録と食い違ったものを返す。call は関数の添字 (記録の Results の並び) と
// dict を受け、戻り値の文字列 (本家の driver が SHA-256 を取ったのと同じ文字列) か error を返す。条件は「本家が例外 ⇔ error」と、例外にならないときの戻り値の一致。
func Check(g oracle.ReportShapeGolden, prefix string, fns int, call func(i int, d *pyjson.Object) (string, error)) (Stats, []string, error) {
	bases := map[string]json.RawMessage{}
	for _, b := range g.Bases {
		bases[b.Name] = b.D
	}
	var st Stats
	var bad []string
	for _, m := range g.Mutations {
		if !strings.HasPrefix(m.Base, prefix) {
			continue
		}
		if len(m.Results) != fns {
			return st, nil, fmt.Errorf("%s %v %s: 結果が %d 個 (関数は %d 個)", m.Base, m.Path, m.Variant, len(m.Results), fns)
		}
		st.Mutations++
		for i, want := range m.Results {
			d, err := Input(bases[m.Base], m)
			if err != nil {
				return st, nil, fmt.Errorf("%s %v %s: %w", m.Base, m.Path, m.Variant, err)
			}
			got, err := call(i, d)
			where := fmt.Sprintf("%s %v %s 関数 %d", m.Base, m.Path, m.Variant, i)
			switch {
			case strings.HasPrefix(want, "E:"):
				st.Raised++
				if err == nil {
					bad = append(bad, where+": 本家は "+want+" だが Go は error を返さない")
				}
			case err != nil:
				bad = append(bad, where+": 本家は例外にならないが Go は error: "+err.Error())
			default:
				st.Compared++
				if digest(got) != want {
					bad = append(bad, where+": 戻り値が本家と違う")
				}
			}
		}
	}
	return st, bad, nil
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// Input は書き換えた dict を作る (基準を pyjson.Loads で読み直して、path の位置を書き換える)。
func Input(base json.RawMessage, m oracle.ReportShapeMutation) (*pyjson.Object, error) {
	v, err := pyjson.Loads(string(base))
	if err != nil {
		return nil, err
	}
	d, ok := v.(*pyjson.Object)
	if !ok {
		return nil, errors.New("基準が dict ではありません")
	}
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
		if last != float64(0) {
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
		if step != float64(0) || len(x) == 0 {
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
	}
	return nil, fmt.Errorf("書き換えの種類 %q を知りません", name)
}
