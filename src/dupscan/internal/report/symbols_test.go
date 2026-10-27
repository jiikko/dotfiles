package report

import "testing"

func TestDefQueryTypeSearchTermIsLexicalAndSharesScopeNormalization(t *testing.T) {
	for _, c := range []struct{ value, want string }{
		{"BoundedFIFOArray.append", "BoundedFIFOArray"},
		{"BoundedFIFOArray<Int>.append", "BoundedFIFOArray"},
		{"Module.Outer<Pair<Int,String>>.Queue<Double>.append", "Queue"},
		{"Queue<T> where T: Sendable.append", "Queue"},
		{"append", ""},
		{"BoundedFIFOArray", ""},
	} {
		q, err := ParseDefQuery(c.value)
		if err != nil || q.TypeSearchTerm() != c.want {
			t.Fatalf("lexical term %q: query=%+v term=%q error=%v", c.value, q, q.TypeSearchTerm(), err)
		}
	}
}
