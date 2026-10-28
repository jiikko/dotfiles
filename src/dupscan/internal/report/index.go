package report

import (
	"fmt"
	"io"
	"slices"
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
	// Kinds は単位の種類 (func / init / property) の絞り込み。空なら全部。`-returns` で関数を探すとき、同じ型を返す property (`var location: T`) が
	// 混ざって上位を占めるのを避ける。
	Kinds []tree.UnitKind
	// Shared は共通の置き場所か (nil なら無し)。名前の一致の強さの次に、ここの定義を先に並べる。
	Shared func(file string) bool
}

// IndexEntry は索引の 1 行。
type IndexEntry struct {
	UnitRef
	// EndLine は単位の終わりの行。Read に範囲 (offset / limit) を渡すために text の `file:開始-終了` に出す。無い (0) か 1 行の単位なら出さない。
	EndLine    int      `json:"end_line,omitempty"`
	ReturnType string   `json:"return_type,omitempty"`
	ParamTypes []string `json:"param_types,omitempty"`
	Doc        string   `json:"doc,omitempty"`
	// Calls は呼び出しの数 (名前と引数ラベルで当てる近似。callers と同じ当て方)。関数と初期化子だけ数え、それ以外は nil。
	// 欄の名前に approx を入れる: 型を解決しないので同名の別のメソッドも数えうる。text の "~" と同じく、読む側が正確な数と取り違えないため。
	Calls *int `json:"calls_approx,omitempty"`
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
// 並べ方: 名前の一致の強さ (同じ > 前方 > 一部) → 共通の置き場所を前 → prod を test より前 → private でないものを前 (他 module/target からの利用可否は未解決) → 呼び出しの多いもの → 場所。
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
		e := IndexEntry{UnitRef: ref(h.u), EndLine: h.u.EndLine, ReturnType: h.u.ReturnType, ParamTypes: h.u.ParamTypes, Doc: h.u.Doc, Shared: q.shared(h.u.File)}
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
	if len(q.Kinds) > 0 && !slices.Contains(q.Kinds, u.Kind) {
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
	name, want := strings.ToLower(tree.MatchName(u.Name)), strings.ToLower(tree.MatchName(q.Name))
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
		byName[tree.MatchName(c.Name)] = append(byName[tree.MatchName(c.Name)], c)
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
		for _, c := range byName[tree.MatchName(name)] {
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

// hidden は、その可視性では他のファイル (Go は他の package) からそのまま使えないか (寄せる候補として区別する)。
// unexported は Go の adapter が小文字で始まる名前に付ける (Swift の `package` 修飾子と混ざらない名前にしている)。
func hidden(v string) bool { return v == "private" || v == "fileprivate" || v == "unexported" }

func containsAny(xs []string, sub string) bool {
	for _, x := range xs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

// IndexTextOptions は index の text 表示の設定。
type IndexTextOptions struct {
	Limit    int            // 出す件数の上限 (0 は全部)
	DocBytes int            // doc の先頭行を、最初の文まで・この byte 数までに縮める (0 は縮めない)。JSON と -doc の絞り込みは全文のまま
	Hint     TruncationHint // 打ち切ったときだけ先頭に出す案内
}

// IndexText は索引を 1 行 1 件で出す (`file:開始-終了 型.署名 [印] // doc の先頭`)。上限を超えた分は件数だけ出し、
// 打ち切ったときだけ先頭に全件の件数・大きさと次の手を 1 行で示す。
func IndexText(w io.Writer, s IndexSummary, o IndexTextOptions) error {
	if s.Matches == 0 {
		fmt.Fprintf(w, "dupscan index: 当たる単位なし (単位 %d)\n", s.Units)
		return nil
	}
	if o.Hint.Label != "" && o.Limit > 0 && len(s.Entries) > o.Limit {
		all := 0
		for _, e := range s.Entries {
			all += len(IndexEntryTextWith(e, o.DocBytes)) + 1
		}
		fmt.Fprintln(w, o.Hint.header(o.Limit, s.Matches, all))
	}
	for i, e := range s.Entries {
		if o.Limit > 0 && i == o.Limit {
			fmt.Fprintf(w, "… 他 %d 件 (-limit 0 か -format json で全部。語を足して絞る)\n", s.Matches-o.Limit)
			break
		}
		fmt.Fprintln(w, IndexEntryTextWith(e, o.DocBytes))
	}
	return nil
}

// IndexEntryText formats one outline entry. def uses the same row shape for type members.
func IndexEntryText(e IndexEntry) string { return IndexEntryTextWith(e, 0) }

// IndexEntryTextWith は docBytes (0 以下は縮めない) で doc を縮めた 1 行。
func IndexEntryTextWith(e IndexEntry, docBytes int) string {
	where := fmt.Sprintf("%s:%d", e.File, e.Line)
	if e.EndLine > e.Line {
		where += fmt.Sprintf("-%d", e.EndLine)
	}
	line := fmt.Sprintf("%s %s", where, label(e.UnitRef))
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
		line += " // " + ShortDoc(e.Doc, docBytes)
	}
	return line
}
