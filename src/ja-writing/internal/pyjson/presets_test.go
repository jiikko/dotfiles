package pyjson

import "testing"

// 3 通りの出し方が、本家の json.dumps と同じ字になること。期待値は Python 3.15.0 の json.dumps に同じ値を通した出力
// (鍵の順は挿入順と sort_keys で別。é と 日 は ensure_ascii で \u に直る)。
func TestPresetsMatchPythonDumps(t *testing.T) {
	inner := NewObject()
	inner.Set("é", "日")
	inner.Set("a", []Value{})
	v := NewObject()
	v.Set("b", []Value{int64(1), 2.5, inner})
	v.Set("a", nil)
	v.Set("c", true)
	v.Set("d", NewObject())
	for _, c := range []struct {
		name string
		opts Options
		want string
	}{
		{"Indent1", Indent1(), "{\n \"b\": [\n  1,\n  2.5,\n  {\n   \"\\u00e9\": \"\\u65e5\",\n   \"a\": []\n  }\n ],\n \"a\": null,\n \"c\": true,\n \"d\": {}\n}"},
		{"Compact", Compact(), `{"a":null,"b":[1,2.5,{"a":[],"\u00e9":"\u65e5"}],"c":true,"d":{}}`},
		{"Plain", Plain(), `{"b": [1, 2.5, {"\u00e9": "\u65e5", "a": []}], "a": null, "c": true, "d": {}}`},
	} {
		got, err := Dumps(v, c.opts)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}
