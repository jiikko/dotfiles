package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"dupscan/internal/tree"
)

// Query は索引の引き方。空の欄は絞らない。全部が当たる単位だけを返す。
type Query struct {
	Name      string // 名前の一部 (大文字小文字を区別しない)
	Returns   string // 戻り値の型の一部
	ParamType string // どれかの引数の型の一部
	Doc       string // doc comment の最初の行の語 (大文字小文字を区別しない)
	File      string // このファイル (repo の root からの相対) の単位だけ。行の順に並べる
	// Shared は共通の置き場所か (nil なら無し)。名前の一致の強さの次に、ここの定義を先に並べる。
	Shared func(file string) bool
}

// IndexEntry は索引の 1 行。
type IndexEntry struct {
	UnitRef
	ReturnType string   `json:"return_type,omitempty"`
	ParamTypes []string `json:"param_types,omitempty"`
	Doc        string   `json:"doc,omitempty"`
	// Calls は呼び出しの数 (名前と引数ラベルで当てる近似。callers と同じ当て方)。関数と初期化子だけ数え、それ以外は nil。
	Calls *int `json:"calls,omitempty"`
	// Shared は共通の置き場所 (設定の shared) にある定義。
	Shared bool `json:"shared,omitempty"`
}

// IndexSummary は索引の出力。
type IndexSummary struct {
	Units   int          `json:"units"`
	Matches int          `json:"matches"`
	Entries []IndexEntry `json:"entries"`
}

// BuildIndex は名前の付いた単位 (クロージャを除く) から q に当たるものを並べる。
// 並べ方: 名前の一致の強さ (同じ > 前方 > 一部) → 共通の置き場所を前 → prod を test より前 → private でないもの (そのまま使える) を前 → 呼び出しの多いもの → 場所。
// File を指したときはファイルの中の行の順 (outline)。
func BuildIndex(units []*tree.Unit, calls []tree.Call, q Query) IndexSummary {
	counts := callCounts(units, calls)
	type hit struct {
		u     *tree.Unit
		score int
	}
	var hits []hit
	n := 0
	for _, u := range units {
		if u.Kind == tree.KindClosure {
			continue
		}
		n++
		score, ok := q.match(u)
		if ok {
			hits = append(hits, hit{u, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if q.File == "" {
			if a.score != b.score {
				return a.score > b.score
			}
			if sa, sb := q.shared(a.u.File), q.shared(b.u.File); sa != sb {
				return sa
			}
			if a.u.Test != b.u.Test {
				return !a.u.Test
			}
			if pa, pb := hidden(a.u.Visibility), hidden(b.u.Visibility); pa != pb {
				return !pa
			}
			if ca, cb := counts[a.u], counts[b.u]; ca != cb {
				return ca > cb
			}
		}
		if a.u.File != b.u.File {
			return a.u.File < b.u.File
		}
		return a.u.Line < b.u.Line
	})
	s := IndexSummary{Units: n, Matches: len(hits), Entries: []IndexEntry{}}
	for _, h := range hits {
		e := IndexEntry{UnitRef: ref(h.u), ReturnType: h.u.ReturnType, ParamTypes: h.u.ParamTypes, Doc: h.u.Doc, Shared: q.shared(h.u.File)}
		if n, ok := counts[h.u]; ok {
			e.Calls = &n
		}
		s.Entries = append(s.Entries, e)
	}
	return s
}

func (q Query) shared(file string) bool { return q.Shared != nil && q.Shared(file) }

func (q Query) match(u *tree.Unit) (int, bool) {
	if q.File != "" && u.File != q.File {
		return 0, false
	}
	if q.Returns != "" && !strings.Contains(u.ReturnType, q.Returns) {
		return 0, false
	}
	if q.ParamType != "" && !containsAny(u.ParamTypes, q.ParamType) {
		return 0, false
	}
	if q.Doc != "" && !strings.Contains(strings.ToLower(u.Doc), strings.ToLower(q.Doc)) {
		return 0, false
	}
	if q.Name == "" {
		return 0, true
	}
	name, want := strings.ToLower(u.Name), strings.ToLower(q.Name)
	switch {
	case name == want:
		return 3, true
	case strings.HasPrefix(name, want):
		return 2, true
	case strings.Contains(name, want):
		return 1, true
	}
	return 0, false
}

// callCounts は関数と初期化子ごとの呼び出しの数。初期化子は型の名前 (`Foo(…)`) で当てる。
// 型を解決しないので同名の別のメソッドの呼び出しも数え、既定値のある引数を省いた呼び出しは数えない (近似)。
func callCounts(units []*tree.Unit, calls []tree.Call) map[*tree.Unit]int {
	byName := map[string][]tree.Call{}
	for _, c := range calls {
		byName[c.Name] = append(byName[c.Name], c)
	}
	out := map[*tree.Unit]int{}
	for _, u := range units {
		name := u.Name
		switch u.Kind {
		case tree.KindFunc:
		case tree.KindInit:
			name = typeName(u.Enclosing)
		default:
			continue
		}
		q := CallQuery{Name: name, Labels: append([]string{}, u.Labels...)}
		n := 0
		for _, c := range byName[name] {
			if q.matches(c) {
				n++
			}
		}
		out[u] = n
	}
	return out
}

// typeName は囲む型の表し (`Outer.Inner`・`Foo where T == Int`) から、初期化の呼び出しに書く型の名前を取る。
func typeName(enclosing string) string {
	head, _, _ := strings.Cut(enclosing, " ")
	if i := strings.LastIndex(head, "."); i >= 0 {
		head = head[i+1:]
	}
	return head
}

// hidden は、その可視性では他のファイルからそのまま使えないか (寄せる候補として区別する)。
func hidden(v string) bool { return v == "private" || v == "fileprivate" }

func containsAny(xs []string, sub string) bool {
	for _, x := range xs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

// IndexText は索引を 1 行 1 件で出す (`file:line 型.署名 [印] // doc の 1 行目`)。limit を超えた分は件数だけ出す。
func IndexText(w io.Writer, s IndexSummary, limit int) error {
	if s.Matches == 0 {
		fmt.Fprintf(w, "dupscan index: 当たる単位なし (単位 %d)\n", s.Units)
		return nil
	}
	for i, e := range s.Entries {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "… 他 %d 件 (-limit 0 か -format json で全部。語を足して絞る)\n", s.Matches-limit)
			break
		}
		line := fmt.Sprintf("%s:%d %s", e.File, e.Line, label(e.UnitRef))
		var marks []string
		if e.Shared {
			marks = append(marks, "shared")
		}
		if e.Test {
			marks = append(marks, "test")
		}
		if hidden(e.Visibility) {
			marks = append(marks, e.Visibility)
		}
		if len(marks) > 0 {
			line += " [" + strings.Join(marks, ",") + "]"
		}
		if e.Calls != nil {
			line += fmt.Sprintf(" (呼び出し ~%d)", *e.Calls)
		}
		if e.Doc != "" {
			line += " // " + e.Doc
		}
		fmt.Fprintln(w, line)
	}
	return nil
}
