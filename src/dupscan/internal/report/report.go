// Package report は重複の報告を、人が読む形・JSON・Xcode / SwiftLint と同じ形式で書く。
package report

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"dupscan/internal/exclude"
	"dupscan/internal/group"
	"dupscan/internal/jsonout"
	"dupscan/internal/tree"
)

// Summary は走査の数え上げ。
type Summary struct {
	Root            string   `json:"root"`
	Files           int      `json:"files"`
	Units           int      `json:"units"`
	FilesWithErrors []string `json:"files_with_errors"`
	// BelowMinNodes は節が下限 (MinNodes) に満たず、比べなかった単位の数。0 件の「重複なし」は、これらを比べていない。
	BelowMinNodes int `json:"below_min_nodes"`
	MinNodes      int `json:"min_nodes"`
	// BelowMinGroups は、比べなかった小さい単位だけでできる同じ形の組の数 (BelowMinFloor 節まで下げたとき)。
	BelowMinGroups int         `json:"below_min_groups"`
	BelowMinFloor  int         `json:"below_min_floor"`
	Unreadable     []UnitRef   `json:"unreadable_units"`
	Groups         []GroupJSON `json:"groups"`
	// FilePairs は組をメンバーのファイルの組み合わせでまとめた件数 (節の合計の大きい順)。同じ画面・同じ型の写しが細切れの組で並ぶのを、1 行で見せる。
	FilePairs []FilePair `json:"file_pairs"`
	Exclusions
}

// FilePair は、同じファイルの組み合わせに属する組の数と、各組の最大の単位の節の合計。
type FilePair struct {
	Files  []string `json:"files"`
	Groups int      `json:"groups"`
	Nodes  int      `json:"nodes"`
}

// Exclusions は除外の設定の状態 (report と gate で共通)。
type Exclusions struct {
	// Stale は何も消していない除外 (寄せ終わった・改名した・本体を直した)。
	Stale []exclude.Rule `json:"stale_exclusions"`
	// Ambiguous は釘が 2 つ以上の単位に当たるので効かせなかった除外。
	Ambiguous []exclude.Rule `json:"ambiguous_exclusions"`
	// staleWhy は Stale と同じ順の、効かない理由 (exclude.Set.StaleWhy の値)。text の一覧にだけ使う (JSON の形は変えない)。
	staleWhy        []string
	staleCandidates []string
}

func exclusions(ex *exclude.Set) Exclusions {
	x := Exclusions{Stale: []exclude.Rule{}, Ambiguous: []exclude.Rule{}}
	if ex != nil {
		for _, r := range ex.Stale() {
			x.Stale = append(x.Stale, r)
			x.staleWhy = append(x.staleWhy, ex.StaleWhy(r))
			x.staleCandidates = append(x.staleCandidates, ex.StaleGroupCandidate(r))
		}
		x.Ambiguous = append(x.Ambiguous, ex.Ambiguous()...)
	}
	return x
}

func (x Exclusions) note() string {
	out := ""
	if n := len(x.Stale); n > 0 {
		out += fmt.Sprintf("・効いていない除外 %d", n)
	}
	if n := len(x.Ambiguous); n > 0 {
		out += fmt.Sprintf("・名指しできない除外 %d (釘が 2 つ以上の単位に当たる)", n)
	}
	if out != "" {
		out += " (-v で一覧)"
	}
	return out
}

func (x Exclusions) write(w io.Writer) {
	for i, r := range x.Stale {
		// 「効いていない除外: 」の行は、接頭辞も行全体が JSON であることも変えない (grep や、行を JSON として読む手が頼る)。
		// 理由は次の行に別に出す。
		fmt.Fprintf(w, "効いていない除外: %s\n", ruleJSON(r))
		if why := staleWhyText(x.staleWhy, i); why != "" {
			if miss := r.GroupMiss(); miss != "" {
				why += " (" + miss + ")"
			}
			fmt.Fprintf(w, "  理由: %s\n", why)
		}
		if i < len(x.staleCandidates) && x.staleCandidates[i] != "" {
			fmt.Fprintf(w, "  同じ鍵の組候補。再審査が必要: %s\n", x.staleCandidates[i])
		}
	}
	for _, r := range x.Ambiguous {
		fmt.Fprintf(w, "名指しできない除外: %s\n", ruleJSON(r))
	}
}

func ruleJSON(r exclude.Rule) string {
	b, _ := jsonout.Line(r)
	return string(b)
}

// UnitRef は単位 1 つの出力。
type UnitRef struct {
	Key        string `json:"key"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Col        int    `json:"col"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Signature  string `json:"signature,omitempty"`
	Allowed    bool   `json:"allowed,omitempty"` // 単位の除外 (許可) が効いている
	Enclosing  string `json:"enclosing,omitempty"`
	OwnerName  string `json:"owner_name,omitempty"` // クロージャを囲む単位の表示名
	Test       bool   `json:"test"`
	Visibility string `json:"visibility"`
}

// GroupJSON は組 1 つの出力。
type GroupJSON struct {
	Level     string    `json:"level"`
	Hash      string    `json:"hash"`
	Partition string    `json:"partition"`
	Members   []UnitRef `json:"members"`
	// Exclude はこの組を指す組の除外の 1 行 (kind と reason を埋めて .dupscan.json に貼る)。
	Exclude string `json:"exclude"`
	// Nodes はメンバーのうち最も大きい単位の節の数 (寄せる価値の目安。組はこの大きい順に並べる)。
	Nodes int `json:"nodes"`
}

// Build は出力の形を組み立てる。ex は除外 (nil なら無し)。単位の許可の印を付け、その後で効いていない除外を数える。
func Build(root string, files int, units []*tree.Unit, errFiles []string, groups []group.Group, ex *exclude.Set) Summary {
	s := Summary{Root: root, Files: files, Units: len(units), FilesWithErrors: errFiles}
	if s.FilesWithErrors == nil {
		s.FilesWithErrors = []string{}
	}
	s.Unreadable = []UnitRef{}
	for _, u := range units {
		if u.Unreadable {
			s.Unreadable = append(s.Unreadable, ref(u))
		}
	}
	s.Groups = []GroupJSON{}
	for _, g := range groups {
		gj := GroupJSON{Level: g.Level.String(), Hash: g.Hash, Partition: g.Partition, Exclude: exclude.GroupSnippet(g.Level, g.Members)}
		for _, m := range g.Members {
			r := ref(m.Unit)
			r.Allowed = ex != nil && ex.Allowed(m)
			gj.Members = append(gj.Members, r)
			gj.Nodes = max(gj.Nodes, m.Result.Nodes)
		}
		s.Groups = append(s.Groups, gj)
	}
	// 大きい写しから並べる (寄せる価値が大きく、小さい組ほど雑音が多い)。同じ大きさなら細かい段・元の順。
	sort.SliceStable(s.Groups, func(i, j int) bool { return s.Groups[i].Nodes > s.Groups[j].Nodes })
	s.FilePairs = filePairs(s.Groups)
	s.Exclusions = exclusions(ex)
	return s
}

func filePairs(groups []GroupJSON) []FilePair {
	idx := map[string]int{}
	out := []FilePair{}
	for _, g := range groups {
		seen := map[string]bool{}
		var files []string
		for _, m := range g.Members {
			if !seen[m.File] {
				seen[m.File] = true
				files = append(files, m.File)
			}
		}
		sort.Strings(files)
		k := strings.Join(files, "\x00")
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, FilePair{Files: files})
		}
		out[i].Groups++
		out[i].Nodes += g.Nodes
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Nodes > out[j].Nodes })
	return out
}

func ref(u *tree.Unit) UnitRef {
	return UnitRef{Key: u.Key, File: u.File, Line: u.Line, Col: u.Col, Kind: string(u.Kind), Name: u.Name,
		Signature: u.Signature, Enclosing: u.Enclosing, OwnerName: u.OwnerName, Test: u.Test, Visibility: u.Visibility}
}

// JSON は JSON で書く。
func JSON(w io.Writer, s Summary) error {
	return WriteJSON(w, s)
}

// TextOptions は人が読む形の設定。
type TextOptions struct {
	// Limit は出す組の上限 (0 は全部)。Claude が何度も回すので、既定は短くする。
	Limit int
	// Verbose は読めない構文の単位を 1 つずつ出す。
	Verbose bool
}

// Text は人が読む形で書く。重複が無いときは 1 行だけにする (何度も回す Claude の文脈を食わないように)。
func Text(w io.Writer, s Summary, opt TextOptions) error {
	if len(s.Groups) == 0 {
		fmt.Fprintf(w, "dupscan: 重複なし (%d ファイル・%d 単位)%s%s%s\n", s.Files, s.Units, unreadableNote(s), belowMinNote(s), s.note())
		if opt.Verbose {
			writeErrorFiles(w, s)
			s.write(w)
			writeUnreadableUnits(w, s)
		}
		return nil
	}
	fmt.Fprintf(w, "dupscan: 組 %d (%s)・%d ファイル・%d 単位%s%s%s\n", len(s.Groups), levelCounts(s), s.Files, s.Units, unreadableNote(s), belowMinNote(s), s.note())
	writeFilePairs(w, s.FilePairs)
	shown := s.Groups
	if opt.Limit > 0 && len(shown) > opt.Limit {
		shown = shown[:opt.Limit]
	}
	for _, g := range shown {
		fmt.Fprintf(w, "%s %s (節 %d):\n", g.Level, g.Partition, g.Nodes)
		for _, m := range g.Members {
			fmt.Fprintf(w, "  %s:%d %s%s\n", m.File, m.Line, label(m), allowedMark(m))
		}
		if opt.Verbose {
			fmt.Fprintf(w, "  除外の 1 行: %s\n", g.Exclude)
		}
	}
	if rest := len(s.Groups) - len(shown); rest > 0 {
		fmt.Fprintf(w, "… 他 %d 組 (-limit 0 か -format json で全部)\n", rest)
	}
	if opt.Verbose {
		writeErrorFiles(w, s)
		s.write(w)
		writeUnreadableUnits(w, s)
	}
	return nil
}

// writeUnreadableUnits は読めない構文で比べなかった単位を 1 つずつ書く。重複が無いとき (寄せ終わって確かめる場面) も同じ形で出す:
// 無いと、-v でもファイル名しか出ず、どの関数が外れたかが分からない。
func writeUnreadableUnits(w io.Writer, s Summary) {
	for _, u := range s.Unreadable {
		fmt.Fprintf(w, "読めない: %s:%d %s\n", u.File, u.Line, label(u))
	}
}

// filePairsShown は text で出すファイルの組の数。
const filePairsShown = 5

// writeFilePairs は、組が 2 つ以上あるファイルの組み合わせを、節の合計の大きい順に上位だけ出す (1 組だけのものは組の一覧と同じなので出さない)。
func writeFilePairs(w io.Writer, pairs []FilePair) {
	n := 0
	for _, p := range pairs {
		if p.Groups < 2 {
			continue
		}
		if n == 0 {
			fmt.Fprintln(w, "ファイルの組 (組が 2 つ以上・節の合計の大きい順):")
		}
		if n == filePairsShown {
			fmt.Fprintln(w, "  … (-format json の file_pairs で全部)")
			break
		}
		fmt.Fprintf(w, "  %d 組・節 %d: %s\n", p.Groups, p.Nodes, strings.Join(p.Files, " ↔ "))
		n++
	}
}

func writeErrorFiles(w io.Writer, s Summary) {
	for _, f := range s.FilesWithErrors {
		fmt.Fprintf(w, "読めない構文があるファイル: %s\n", f)
	}
}

func levelCounts(s Summary) string {
	n := map[string]int{}
	var order []string
	for _, g := range s.Groups {
		if n[g.Level] == 0 {
			order = append(order, g.Level)
		}
		n[g.Level]++
	}
	parts := make([]string, len(order))
	for i, l := range order {
		parts[i] = fmt.Sprintf("%s %d", l, n[l])
	}
	return strings.Join(parts, "・")
}

// unreadableNote は読めない構文の件数。読めない部分に飲み込まれた関数は単位として出てこないので、単位の数だけでなく
// ファイルの数も出す (「重複なし」と並べて、比べていない部分があることを黙らせない)。
func unreadableNote(s Summary) string {
	if len(s.FilesWithErrors) == 0 && len(s.Unreadable) == 0 {
		return ""
	}
	return fmt.Sprintf("・読めない構文があるファイル %d・比べなかった単位 %d (-v で一覧)", len(s.FilesWithErrors), len(s.Unreadable))
}

// belowMinNote は節が下限に満たず比べなかった単位の数。小さい helper の写し (1 語だけ違う 10 行前後の関数など) はここに入りうる。
func belowMinNote(s Summary) string {
	if s.BelowMinNodes == 0 {
		return ""
	}
	note := fmt.Sprintf("・小さい単位 %d 個は比べていない (節が %d 未満。-min-nodes で下げる)", s.BelowMinNodes, s.MinNodes)
	if s.BelowMinGroups > 0 {
		note += fmt.Sprintf("・うち同じ形の組が %d 組ある (-min-nodes %d で見える)", s.BelowMinGroups, s.BelowMinFloor)
	}
	return note
}

// Xcode は `ファイル:行:列: warning: …` の形式で書く (swiftlint と並べて回し、Xcode の警告として出すため)。
// 組の先頭のメンバーを warning の 1 行にし、残りのメンバーを note の行で続ける。パスは絶対パス (Xcode が飛べるように)。
func Xcode(w io.Writer, s Summary) error {
	for _, g := range s.Groups {
		first := g.Members[0]
		fmt.Fprintf(w, "%s:%d:%d: warning: %s は他の %d 個と本体が同じ形 (dupscan.%s)\n",
			filepath.Join(s.Root, first.File), first.Line, first.Col, label(first), len(g.Members)-1, g.Level)
		for _, m := range g.Members[1:] {
			fmt.Fprintf(w, "%s:%d:%d: note: 同じ形: %s (dupscan.%s)\n", filepath.Join(s.Root, m.File), m.Line, m.Col, label(m), g.Level)
		}
	}
	for _, f := range s.FilesWithErrors {
		fmt.Fprintf(w, "%s:1:1: note: 読めない構文があり、比べていない部分がある (dupscan)\n", filepath.Join(s.Root, f))
	}
	return nil
}

func allowedMark(u UnitRef) string {
	if u.Allowed {
		return " (許可)"
	}
	return ""
}

func label(u UnitRef) string {
	name := u.Signature
	if name == "" {
		name = u.Name
	}
	if u.OwnerName != "" {
		name = u.OwnerName + "." + name
	}
	if u.Enclosing != "" {
		return u.Enclosing + "." + name
	}
	return name
}

// staleWhyText は効いていない除外の理由 (値の意味は exclude.Set.StaleWhy)。
func staleWhyText(why []string, i int) string {
	if i >= len(why) {
		return ""
	}
	switch why[i] {
	case "hash":
		return "同じ鍵 (ファイル・型・署名) の単位は今も在る。効かない原因は特定していない (本体が変わった・組の顔ぶれが変わった・同じ除外が二重に書かれている等)。この除外がまだ要るか見直す"
	case "group-count":
		return "dupscan:allow-group の人数と、同じ本体の単位の数が合わないので効かせていない (写しが増えた・減った・本体を直した): 増えた写しは寄せるか、確かめてから人数を直す"
	case "allow-unchanged":
		return "文面 (人数・種別・理由) が base のどれかの dupscan:allow-group と同じ許可は、変わっていないとみなして base にあった単位だけを覆う (コメントが diff に出ないまま、後から足した写しを覆わない)。base に無かった単位 (足した写し・移した単位) を指している: 足した写しなら寄せるか、確かめてから文面を base のどの allow-group とも違うものに書き換える (人数を直しても、別の組の許可と文面が同じなら変わっていないとみなす)。単位を移しただけなら、次の base からは効く"
	case "missing":
		return "指す単位が無い (削除・改名・起点の外): 除外を直すか消す"
	case "duplicate":
		return "同じ釘の除外が別にあって、そちらが効いている: 重複を消す"
	case "ungrouped":
		return "釘の指す単位は釘の本体のまま今も在る (本体は変わっていない。組の除外なら顔ぶれも同じ)。効かないのは組の側の理由で、どれかは区別しない: 組に入らない (節が下限未満・本体が他と一致しない) / 別の除外が先に組を消した / gate の base の制約。sweep (節 2 は下限 12 節) は組にならない単位の許可・組の除外も読むので、そのために置いたのなら残してよい"
	}
	return ""
}
