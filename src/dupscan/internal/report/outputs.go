package report

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

// Outputs は 1 つの subcommand の出力の説明 (`dupscan capabilities -outputs <command>`)。
// 欄の一覧は出力の型を reflect で辿って作り、説明は fieldDescs から引く (手で欄を写さない)。
type Outputs struct {
	Command string        `json:"command"`
	Root    string        `json:"root_type"`
	Types   []OutputType  `json:"types"`
	Verbose []VerboseLine `json:"verbose_lines"`
}

// OutputType は json に出る型 1 つと、その欄。
type OutputType struct {
	Name   string        `json:"name"`
	Fields []OutputField `json:"fields"`
}

// OutputField は json の欄 1 つ。Omitted は、空・false・0 のとき欄ごと出ないこと (jq で null と読む)。
type OutputField struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Omitted bool   `json:"omitted_when_empty,omitempty"`
	Desc    string `json:"desc"`
}

// VerboseLine は `-v` でだけ出る行の種類 1 つ。Prefix で始まる行として出る。
type VerboseLine struct {
	Key      string   `json:"key"`
	Prefix   string   `json:"prefix"`
	Commands []string `json:"commands"`
	JSON     string   `json:"json,omitempty"` // 同じ中身を持つ json の欄 (無ければ text だけ)
	Desc     string   `json:"desc"`
}

// outputRoots は、-outputs で引ける subcommand と出力の型。
var outputRoots = map[string]reflect.Type{
	"report": reflect.TypeFor[Summary](),
	"gate":   reflect.TypeFor[GateSummary](),
}

// OutputCommands は -outputs で引ける subcommand の名前 (名前の順)。
func OutputCommands() []string {
	names := make([]string, 0, len(outputRoots))
	for n := range outputRoots {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// OutputsFor は command の出力の説明を返す。引けない command なら false。
func OutputsFor(command string) (Outputs, bool) {
	root, ok := outputRoots[command]
	if !ok {
		return Outputs{}, false
	}
	types, err := walkOutputTypes(root)
	if err != nil {
		panic(err) // 出力の型に辿れない欄がある (テストが先に落とす)
	}
	o := Outputs{Command: command, Root: root.Name(), Types: types, Verbose: []VerboseLine{}}
	for _, v := range verboseLines {
		for _, c := range v.Commands {
			if c == command {
				o.Verbose = append(o.Verbose, v)
				break
			}
		}
	}
	return o, true
}

// walkOutputTypes は root から辿れる struct の型と、encoding/json と同じ規則で出る欄を返す (根から幅優先)。
// 埋め込みのタグ無しの struct は親の欄として平たくする。対応しない種類 (map・interface・関数) が出たら誤りにする:
// 黙って型の名前だけを書くと、一覧が出力の形と食い違う。
func walkOutputTypes(root reflect.Type) ([]OutputType, error) {
	var out []OutputType
	seen := map[reflect.Type]bool{root: true}
	queue := []reflect.Type{root}
	for len(queue) > 0 {
		t := queue[0]
		if _, _, err := jsonTypeName(t); err != nil {
			return nil, err // 根や辿った型が自分で json を書くなら、欄を辿っても出力の形にならない
		}
		queue = queue[1:]
		ot := OutputType{Name: t.Name()}
		names := map[string]bool{}
		var fields func(t reflect.Type) error
		fields = func(t reflect.Type) error {
			for i := range t.NumField() {
				f := t.Field(i)
				tag, hasTag := f.Tag.Lookup("json")
				tagName, _, _ := strings.Cut(tag, ",")
				if f.Anonymous && f.Type.Kind() == reflect.Pointer {
					return fmt.Errorf("%s.%s: ポインタの埋め込みは一覧に書けない", t.Name(), f.Name)
				}
				// encoding/json は、タグの名前が無い埋め込みの struct を親の欄として平たくする (タグにオプションだけあっても)
				if f.Anonymous && (!hasTag || tagName == "") && f.Type.Kind() == reflect.Struct {
					if _, _, err := jsonTypeName(f.Type); err != nil {
						return fmt.Errorf("%s.%s: %w", t.Name(), f.Name, err)
					}
					if err := fields(f.Type); err != nil {
						return err
					}
					continue
				}
				if !f.IsExported() || tag == "-" {
					continue
				}
				name, opts, _ := strings.Cut(tag, ",")
				if name == "" {
					name = f.Name
				}
				optSet := map[string]bool{}
				for _, o := range strings.Split(opts, ",") {
					optSet[o] = true
				}
				if optSet["string"] || optSet["omitzero"] {
					return fmt.Errorf("%s.%s: タグのオプション %q は一覧に書けない (値の形・出る条件が変わる)", t.Name(), f.Name, opts)
				}
				if names[name] {
					return fmt.Errorf("%s.%s: 欄の名前 %q が埋め込みと重なる (encoding/json は浅い方を採るか両方消す)", t.Name(), f.Name, name)
				}
				names[name] = true
				typ, elem, err := jsonTypeName(f.Type)
				if err != nil {
					return fmt.Errorf("%s.%s: %w", t.Name(), f.Name, err)
				}
				if elem != nil && !seen[elem] {
					seen[elem] = true
					queue = append(queue, elem)
				}
				// struct の値の欄は omitempty でも必ず出る
				ot.Fields = append(ot.Fields, OutputField{Name: name, Type: typ, Omitted: optSet["omitempty"] && f.Type.Kind() != reflect.Struct,
					Desc: fieldDescs[ot.Name+"."+name]})
			}
			return nil
		}
		if err := fields(t); err != nil {
			return nil, err
		}
		out = append(out, ot)
	}
	return out, nil
}

var (
	jsonMarshaler = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

// jsonTypeName は欄の型の表示名と、辿る先の struct の型 (無ければ nil)。
func jsonTypeName(t reflect.Type) (string, reflect.Type, error) {
	for _, m := range []reflect.Type{t, reflect.PointerTo(t)} {
		if m.Implements(jsonMarshaler) || m.Implements(textMarshaler) {
			return "", nil, fmt.Errorf("%s は自分で json を書く型なので、欄の形を型から読めない", t)
		}
	}
	switch t.Kind() {
	case reflect.String:
		return "string", nil, nil
	case reflect.Bool:
		return "bool", nil, nil
	case reflect.Int, reflect.Int64:
		return "int", nil, nil
	case reflect.Slice:
		name, elem, err := jsonTypeName(t.Elem())
		return "[]" + name, elem, err
	case reflect.Struct:
		if t.Name() == "" {
			return "", nil, errors.New("名前の無い struct は一覧に書けない")
		}
		return t.Name(), t, nil
	}
	return "", nil, fmt.Errorf("一覧に書けない種類 %s", t.Kind())
}

// fieldDescs は json の欄の説明 (鍵は「型名.json の欄名」)。欄の一覧と鍵が両方向で一致することはテストが確かめる。
// 出る場所で意味が違う欄は、説明にそう書く (型 1 つに説明 1 つなので)。
var fieldDescs = map[string]string{
	"Summary.root":                 "走査した repo の root (絶対パス)",
	"Summary.files":                "読んだファイルの数",
	"Summary.units":                "比べる単位 (関数・初期化子・property・クロージャ) の数",
	"Summary.files_with_errors":    "読めない構文があるファイル (repo の root からの相対パス)",
	"Summary.generated_files":      "生成されたファイル (Go の `// Code generated … DO NOT EDIT.`) として単位を出さなかったファイル",
	"Summary.below_min_nodes":      "節が下限に満たず、比べなかった単位の数 (0 件の「重複なし」は、これらを比べていない)",
	"Summary.min_nodes":            "比べる単位の節の下限 (-min-nodes)",
	"Summary.below_min_groups":     "比べなかった小さい単位だけでできる同じ形の組の数 (below_min_floor 節まで下げたとき)",
	"Summary.below_min_floor":      "below_min_groups を数えた節の下限",
	"Summary.unreadable_units":     "読めない構文で比べなかった単位",
	"Summary.groups":               "同じ形の組 (節の大きい順)",
	"Summary.file_pairs":           "組をメンバーのファイルの組み合わせでまとめた件数 (節の合計の大きい順)",
	"Summary.stale_exclusions":     "何も消していない除外 (寄せ終わった・改名した・本体を直した)。text の -v の「効いていない除外:」と同じ",
	"Summary.ambiguous_exclusions": "釘が 2 つ以上の単位に当たるので効かせなかった除外",

	"GroupJSON.level":       "段 (L1 / L2 / L3)",
	"GroupJSON.hash":        "その段の正規化した本体の hash (同じ組の目印。寄せる前後で組を突き合わせるなら gate -v を先に使う)",
	"GroupJSON.partition":   "メンバーの種別 (prod / test / mixed)",
	"GroupJSON.members":     "組のメンバー",
	"GroupJSON.exclude":     "この組を指す組の除外の 1 行 (hash 付きの釘。kind と reason を埋めて .dupscan.json の exclude に貼る。text の -v を切り出さない)",
	"GroupJSON.nodes":       "メンバーのうち最も大きい単位の節の数 (組はこの大きい順)",
	"GroupJSON.single_call": "メンバー全員の本体が、1 つの呼び出しを 1 回するだけ (寄せた後に残る委譲の印)",

	"FilePair.files":  "組のメンバーのファイルの組み合わせ",
	"FilePair.groups": "その組み合わせに属する組の数",
	"FilePair.nodes":  "その組み合わせの組の、最大の単位の節の合計",

	"UnitRef.key":        "単位の鍵 (ファイル|型|署名。除外の釘の @ の前)",
	"UnitRef.file":       "ファイル (repo の root からの相対パス。gate の resolved では base の側の位置)",
	"UnitRef.line":       "開始の行 (gate の resolved では base の側の行)",
	"UnitRef.end_line":   "終わりの行 (Read に範囲を渡す。0 か 1 行の単位なら出ない)",
	"UnitRef.col":        "開始の桁",
	"UnitRef.kind":       "単位の種類 (func / init / property / closure)",
	"UnitRef.name":       "名前",
	"UnitRef.signature":  "署名 (引数ラベルと型)",
	"UnitRef.allowed":    "単位の除外 (dupscan:allow) が効いている。report と near でだけ入り、gate では出ない",
	"UnitRef.enclosing":  "囲む型の名前",
	"UnitRef.owner_name": "クロージャを囲む単位の表示名",
	"UnitRef.test":       "test のファイルにある",
	"UnitRef.visibility": "可視性 (Go は public / unexported、Swift は書いた修飾子 (無ければ internal)。関数の中の単位は local)",

	"Rule.paths":   "パスの除外の glob",
	"Rule.preset":  "形で組を外す既定の規則の名前",
	"Rule.levels":  "パスの除外を効かせる段 (無ければ全部)",
	"Rule.group":   "組の除外の釘 (単位の鍵@本体の hash) の一覧",
	"Rule.level":   "組の除外の段",
	"Rule.unit":    "単位の除外の釘",
	"Rule.kind":    "除外の種別 (false-positive / intentional)",
	"Rule.reason":  "除外の理由",
	"Rule.comment": "単位の直前のコメント (dupscan:allow / allow-group) から作った許可の場所 (file:line)",

	"GateSummary.root":                       "走査した repo の root",
	"GateSummary.base":                       "比べた base の commit (head との merge-base)",
	"GateSummary.head":                       "比べた head (作業ツリーなら「作業ツリー」、-head なら commit の hash の先頭 12 文字)",
	"GateSummary.changed_units":              "base から変わった単位の数",
	"GateSummary.base_is_head":               "base が head の commit と同じ (CI で -base を付け忘れた疑い)",
	"GateSummary.base_given":                 "base を -base で渡して決めたか",
	"GateSummary.scope_partial":              "走査の範囲が repo 全体でない (起点がサブディレクトリ)",
	"GateSummary.scope_prefixes":             "走査の起点 (scope_partial のとき)",
	"GateSummary.failed":                     "止める段の違反がある (rc=1)",
	"GateSummary.violations":                 "新しい写しを含む組 (前からある写しに同じ修正を入れて別の形へ移った組は違反にしない。前後の hash の差と同じではない)",
	"GateSummary.unreadable_changed_units":   "変わった単位のうち、読めない構文で比べなかったもの",
	"GateSummary.analysis_incomplete":        "base か head に読めない構文のファイルがある",
	"GateSummary.strict_analysis":            "-strict-analysis で回した (解析が不完全なら rc=2)",
	"GateSummary.base_files_with_errors":     "base で読めない構文のファイル",
	"GateSummary.head_files_with_errors":     "head で読めない構文のファイル",
	"GateSummary.new_generated_files":        "head で生成されたファイルとして単位を出さなかったファイルのうち、base に無いか、base では生成でなかったか、中身が base と違うもの (そこの写しは比べていない)",
	"GateSummary.new_exclusions":             "base の設定に無く head にある除外の要素 (釘・glob・プリセット)。黙らせた組の一覧ではない",
	"GateSummary.untracked_files":            "head の作業ツリーで、ignore されていない untracked から読んだファイル",
	"GateSummary.path_excluded_passed_units": "パスの除外の下にあるので違反にしなかった、変わった単位の数",
	"GateSummary.path_excluded_passed":       "パスの除外の下にあるので違反にしなかった、変わった単位",
	"GateSummary.preset_passed":              "プリセットが組を外したので違反にしなかった、変わった単位",
	"GateSummary.resolved":                   "base の組のうち head で消えた・縮んだもの (寄せた結果。除外を掛けない素の組で比べる)",
	"GateSummary.base_config_error":          "base の除外の設定を読めなかった理由 (空とみなした)",
	"GateSummary.stale_exclusions":           "何も消していない除外 (Summary.stale_exclusions と同じ)",
	"GateSummary.ambiguous_exclusions":       "釘が 2 つ以上の単位に当たるので効かせなかった除外",

	"ViolationJSON.level":       "段",
	"ViolationJSON.fail":        "止める段 (-fail-levels) の違反",
	"ViolationJSON.new":         "組に新しく入った単位 (寄せる側)",
	"ViolationJSON.same":        "組に前からいた単位 (寄せる先の候補)",
	"ViolationJSON.exclude":     "この組を指す組の除外の 1 行 (GroupJSON.exclude と同じ形)",
	"ViolationJSON.single_call": "組の全員の本体が、1 つの呼び出しを 1 回するだけ",
	"ViolationJSON.origins":     "new の単位のうち base に出どころを当てた単位ごとの出どころ (新しく書いた単位は入らない)",

	"OriginJSON.key":          "寄せる側の単位の鍵 (new の key)",
	"OriginJSON.from":         "base の側の出どころの単位",
	"OriginJSON.body_changed": "出どころから本体 (L1) を変えたか",
	"OriginJSON.sure":         "出どころを確かに言えるか (同じ鍵か本体が L2 まで同じ)。false は L3 の形やクロージャの順番だけで当てた相手",

	"ResolvedJSON.level":   "段",
	"ResolvedJSON.members": "base の組のメンバー (base の側の位置)",
	"ResolvedJSON.remain":  "head で同じ組に揃っている数 (0 は消えた。1 以上は縮んだ)",

	"Element.element": "足された除外の要素 (glob と段・組の段と釘・単位の釘・プリセット)",
	"Element.comment": "コメントから作った許可なら、その場所",
	"Element.kind":    "要素を書いた除外の種別 (false-positive / intentional)",
	"Element.reason":  "要素を書いた除外の理由",
}

// verboseKey は -v の行の種類の鍵。vline には下の名前の付いた定数で渡す (文字列のリテラルで渡すと、書き間違いが
// 本番の -v でだけ落ちる。リテラルで渡していないことはテストが AST で確かめる)。
type verboseKey string

const (
	verboseExclude            verboseKey = "exclude"
	verboseErrorFile          verboseKey = "error-file"
	verboseNewGenerated       verboseKey = "new-generated"
	verboseStale              verboseKey = "stale"
	verboseStaleWhy           verboseKey = "stale-why"
	verboseStaleDetail        verboseKey = "stale-detail"
	verboseStaleCandidate     verboseKey = "stale-candidate"
	verboseAmbiguous          verboseKey = "ambiguous"
	verboseUnreadable         verboseKey = "unreadable"
	verboseNewExclusion       verboseKey = "new-exclusion"
	verboseUntracked          verboseKey = "untracked"
	verbosePathPassed         verboseKey = "path-passed"
	verbosePresetPassed       verboseKey = "preset-passed"
	verboseBaseConfig         verboseKey = "base-config"
	verboseResolvedGone       verboseKey = "resolved-gone"
	verboseResolvedShrunk     verboseKey = "resolved-shrunk"
	verboseResolvedMember     verboseKey = "resolved-member"
	verboseCompareSame        verboseKey = "compare-same"
	verboseCompareArgv        verboseKey = "compare-argv"
	verboseCompareMore        verboseKey = "compare-more"
	verboseCompareUnpointable verboseKey = "compare-unpointable"
)

// verboseLines は `-v` でだけ出る行の種類。描く処理は vline でこの鍵を引いて書く (見出しを 2 か所に書かない)。
var verboseLines = []VerboseLine{
	{Key: string(verboseExclude), Prefix: "  除外の 1 行: ", Commands: []string{"report", "gate"}, JSON: "GroupJSON.exclude / ViolationJSON.exclude", Desc: "組・違反の下に、その組を指す組の除外の 1 行"},
	{Key: string(verboseErrorFile), Prefix: "読めない構文があるファイル: ", Commands: []string{"report"}, JSON: "Summary.files_with_errors", Desc: "読めない構文があるファイル"},
	{Key: string(verboseStale), Prefix: "効いていない除外: ", Commands: []string{"report", "gate"}, JSON: "Summary.stale_exclusions / GateSummary.stale_exclusions", Desc: "何も消していない除外 (行の残りは除外 1 つの json)"},
	{Key: string(verboseStaleWhy), Prefix: "  理由: ", Commands: []string{"report", "gate"}, Desc: "効いていない理由 (text だけ)"},
	{Key: string(verboseStaleDetail), Prefix: "  詳しく: ", Commands: []string{"report", "gate"}, Desc: "釘ごとの、今の木との違い (text だけ)"},
	{Key: string(verboseStaleCandidate), Prefix: "  同じ鍵の組候補。再審査が必要: ", Commands: []string{"report", "gate"}, Desc: "同じ鍵の単位でできる今の組の除外の候補 (貼る前に同じ規則か読む。text だけ)"},
	{Key: string(verboseAmbiguous), Prefix: "名指しできない除外: ", Commands: []string{"report", "gate"}, JSON: "Summary.ambiguous_exclusions / GateSummary.ambiguous_exclusions", Desc: "釘が 2 つ以上の単位に当たるので効かせなかった除外"},
	{Key: string(verboseUnreadable), Prefix: "読めない: ", Commands: []string{"report", "gate"}, JSON: "Summary.unreadable_units / GateSummary.unreadable_changed_units", Desc: "読めない構文で比べなかった単位 (gate は変わった単位だけ)"},
	{Key: string(verboseNewGenerated), Prefix: "生成ファイルとして読まなかった: ", Commands: []string{"gate"}, JSON: "GateSummary.new_generated_files", Desc: "head で新しく生成ファイルとして読まなかったファイル"},
	{Key: string(verboseNewExclusion), Prefix: "新しく足された除外: ", Commands: []string{"gate"}, JSON: "GateSummary.new_exclusions", Desc: "base に無く head にある除外の要素 (黙らせた組の一覧ではない)"},
	{Key: string(verboseUntracked), Prefix: "untracked: ", Commands: []string{"gate"}, JSON: "GateSummary.untracked_files", Desc: "untracked から読んだファイル"},
	{Key: string(verbosePathPassed), Prefix: "パスの除外で通した: ", Commands: []string{"gate"}, JSON: "GateSummary.path_excluded_passed", Desc: "パスの除外の下にあるので違反にしなかった、変わった単位"},
	{Key: string(verbosePresetPassed), Prefix: "プリセットで通した: ", Commands: []string{"gate"}, JSON: "GateSummary.preset_passed", Desc: "プリセットが組を外したので違反にしなかった、変わった単位"},
	{Key: string(verboseBaseConfig), Prefix: "base の除外の設定: ", Commands: []string{"gate"}, JSON: "GateSummary.base_config_error", Desc: "base の設定を読めなかった理由"},
	{Key: string(verboseResolvedGone), Prefix: "消えた組: ", Commands: []string{"gate"}, JSON: "GateSummary.resolved (remain が 0)", Desc: "base の組が head で消えた (寄せた結果。段と、人数 → 0)"},
	{Key: string(verboseResolvedShrunk), Prefix: "縮んだ組: ", Commands: []string{"gate"}, JSON: "GateSummary.resolved (remain が 1 以上)", Desc: "base の組が head で縮んだ (段と、人数 → 残りの人数)"},
	{Key: string(verboseCompareSame), Prefix: "  違い: ", Commands: []string{"report", "gate"}, Desc: "L1 の組の下に、何が同じで何を比べていないか (text だけ)"},
	{Key: string(verboseCompareArgv), Prefix: "  違いを見る: ", Commands: []string{"report", "gate"}, Desc: "L2 以上の組の下に、メンバーの違いを確かめる dupscan explain -compare の argv (text だけ。gate の head が commit なら出ない)"},
	{Key: string(verboseCompareMore), Prefix: "  … 他 ", Commands: []string{"report", "gate"}, Desc: "argv を出さなかった残りのメンバーの数 (text だけ)"},
	{Key: string(verboseCompareUnpointable), Prefix: "  行で指せないメンバー ", Commands: []string{"report", "gate"}, Desc: "その行で explain が別の単位を選ぶので argv を出さなかったメンバーの数 (text だけ)"},
	{Key: string(verboseResolvedMember), Prefix: "  base: ", Commands: []string{"gate"}, JSON: "ResolvedJSON.members", Desc: "消えた・縮んだ組の base の側のメンバー (file:開始-終了 と名前)"},
}

var verbosePrefix = func() map[verboseKey]string {
	m := make(map[verboseKey]string, len(verboseLines))
	for _, v := range verboseLines {
		m[verboseKey(v.Key)] = v.Prefix
	}
	return m
}()

// vline は `-v` の行を、表の見出しを付けて書く。見出しは format に混ぜない (見出しの % を書式と読まない)。
func vline(w io.Writer, key verboseKey, format string, args ...any) {
	fmt.Fprint(w, verbosePrefix[key])
	fmt.Fprintf(w, format+"\n", args...)
}
