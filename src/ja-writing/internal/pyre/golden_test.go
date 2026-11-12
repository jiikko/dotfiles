package pyre

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"ja-writing/oracle"
)

const goldenDir = "../../testdata/golden"

var (
	pyreOnce sync.Once
	pyreData oracle.PyreGolden
	pyreErr  error
)

// loadPyre は Python から取った記録 (pyre.json) を 1 回だけ読む。記録が無いときは落とす (飛ばして緑にしない)。
func loadPyre(t *testing.T) oracle.PyreGolden {
	t.Helper()
	pyreOnce.Do(func() { pyreErr = oracle.ReadGolden(goldenDir, oracle.PyreFile, &pyreData) })
	if pyreErr != nil {
		t.Fatalf("make golden で記録を作る: %v", pyreErr)
	}
	return pyreData
}

// toAny は値を JSON を通して any にする (Python の結果と同じ形で比べるため)。
func toAny(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func optional(s Str) any {
	if !s.Set {
		return nil
	}
	return s.Value
}

func matchJSON(m *Match) map[string]any {
	groups := make([]any, 0, m.re.groups+1)
	for g := 0; g <= m.re.groups; g++ {
		s, ok := m.Group(g)
		if !ok {
			groups = append(groups, nil)
			continue
		}
		start, end := m.Span(g)
		groups = append(groups, []any{start, end, s})
	}
	out := map[string]any{"groups": groups, "lastindex": nil, "lastgroup": nil}
	if i, ok := m.LastIndex(); ok {
		out["lastindex"] = i
	}
	if name, ok := m.LastGroup(); ok {
		out["lastgroup"] = name
	}
	dict := map[string]any{}
	for name, s := range m.GroupDict() {
		dict[name] = optional(s)
	}
	out["groupdict"] = dict
	return out
}

func errorJSON(err error) any {
	var pe *Error
	if !errors.As(err, &pe) {
		return map[string]any{"error": "非 re のエラー", "msg": err.Error()}
	}
	out := map[string]any{"error": pe.Class, "msg": pe.Msg}
	if pe.Pos >= 0 {
		out["pos"] = pe.Pos
	}
	return out
}

func brackets(m *Match) string {
	s, _ := m.Group(0)
	return "<" + s + ">"
}

// runCase は 1 つの記録の入力を Go で動かして、Python の結果と同じ形の値にする。
func runCase(c oracle.PyreCase) (any, error) {
	if c.Op == "escape" {
		return Escape(c.Pattern), nil
	}
	p, err := Compile(c.Pattern, c.Flags)
	if err != nil {
		return errorJSON(err), nil
	}
	end := NoEnd
	if c.EndPos != nil {
		end = *c.EndPos
	}
	text := NewText(c.S)
	var out any
	switch c.Op {
	case "compile":
		return map[string]any{"flags": p.Flags(), "groups": p.Groups(), "groupindex": p.GroupIndex()}, nil
	case "search", "match", "fullmatch":
		fn := map[string]func(Text, int, int) (*Match, error){"search": p.Search, "match": p.Match, "fullmatch": p.FullMatch}[c.Op]
		m, err := fn(text, c.Pos, end)
		if err != nil {
			return nil, err
		}
		if m == nil {
			return nil, nil
		}
		return matchJSON(m), nil
	case "finditer":
		ms, err := p.Finditer(text, c.Pos, end)
		if err != nil {
			return nil, err
		}
		list := []any{}
		for _, m := range ms {
			list = append(list, matchJSON(m))
		}
		return list, nil
	case "findall":
		rows, err := p.Findall(text, c.Pos, end)
		if err != nil {
			return nil, err
		}
		list := []any{}
		for _, row := range rows {
			if p.Groups() <= 1 {
				list = append(list, row[0])
			} else {
				list = append(list, row)
			}
		}
		return list, nil
	case "sub", "subn":
		var s string
		var n int
		if c.Func {
			s, n, err = p.SubnFunc(brackets, text, c.N)
		} else {
			s, n, err = p.Subn(c.Arg, text, c.N)
		}
		if err != nil {
			return errorJSON(err), nil
		}
		if c.Op == "sub" {
			return s, nil
		}
		return []any{s, n}, nil
	case "split":
		parts, err := p.Split(text, c.N)
		if err != nil {
			return nil, err
		}
		list := []any{}
		for _, part := range parts {
			list = append(list, optional(part))
		}
		return list, nil
	case "expand":
		m, err := p.Search(text, 0, NoEnd)
		if err != nil {
			return nil, err
		}
		if m == nil {
			return nil, nil
		}
		s, err := m.Expand(c.Arg)
		if err != nil {
			return errorJSON(err), nil
		}
		return s, nil
	}
	return out, errors.New("未知の op " + c.Op)
}

func TestFunctionsMatchPython(t *testing.T) {
	g := loadPyre(t)
	if len(g.Funcs) < 500 {
		t.Fatalf("関数の記録が %d 件しかない", len(g.Funcs))
	}
	bad := 0
	for i, c := range g.Funcs {
		got, err := runCase(c)
		if err != nil {
			t.Errorf("#%d %s %q %q: %v", i, c.Op, c.Pattern, c.S, err)
			bad++
			continue
		}
		var want any
		if err := json.Unmarshal(c.Result, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(toAny(t, got), want) {
			t.Errorf("#%d %s %q s=%q flags=%d pos=%d end=%v n=%d arg=%q func=%v\n  Go     %s\n  Python %s", i, c.Op, c.Pattern, c.S, c.Flags, c.Pos, c.EndPos, c.N, c.Arg, c.Func, mustJSON(t, got), c.Result)
			bad++
		}
		if bad > 40 {
			t.Fatal("差が多すぎるので止める")
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
