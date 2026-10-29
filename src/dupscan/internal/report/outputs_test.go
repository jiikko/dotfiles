package report

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"dupscan/internal/group"
	"dupscan/internal/norm"
)

// 説明の表の鍵と、出力の型から辿った欄が両方向で一致する: 欄を足して説明を書き忘れても、欄を消して説明を残しても落ちる。
func TestOutputFieldDescsMatchTheOutputTypes(t *testing.T) {
	walked := map[string]bool{}
	for _, cmd := range OutputCommands() {
		o, _ := OutputsFor(cmd)
		for _, ty := range o.Types {
			for _, f := range ty.Fields {
				key := ty.Name + "." + f.Name
				walked[key] = true
				if strings.TrimSpace(f.Desc) == "" {
					t.Errorf("%s の説明が無い (fieldDescs に足す)", key)
				}
			}
		}
	}
	for key := range fieldDescs {
		if !walked[key] {
			t.Errorf("fieldDescs の %s は出力の型に無い (欄が消えた・改名した)", key)
		}
	}
	if len(walked) < 60 {
		t.Fatalf("前提: report と gate の欄を辿る (%d 欄しか無い)", len(walked))
	}
}

func TestOutputsWalkEmbeddedAndNestedTypes(t *testing.T) {
	o, ok := OutputsFor("gate")
	if !ok || o.Root != "GateSummary" {
		t.Fatalf("gate の根は GateSummary: %+v", o)
	}
	names := map[string]map[string]OutputField{}
	for _, ty := range o.Types {
		names[ty.Name] = map[string]OutputField{}
		for _, f := range ty.Fields {
			names[ty.Name][f.Name] = f
		}
	}
	if f, ok := names["GateSummary"]["stale_exclusions"]; !ok || f.Type != "[]Rule" {
		t.Errorf("埋め込みの Exclusions の欄を親の欄として出す: %+v", names["GateSummary"])
	}
	if _, ok := names["GateSummary"]["staleWhy"]; ok {
		t.Errorf("非公開の欄は出さない")
	}
	for _, ty := range []string{"ViolationJSON", "UnitRef", "ResolvedJSON", "Rule", "Element"} {
		if names[ty] == nil {
			t.Errorf("配列の要素の型 %s も辿る: %v", ty, o.Types)
		}
	}
	if !names["GateSummary"]["base_config_error"].Omitted || names["GateSummary"]["violations"].Omitted {
		t.Errorf("omitempty の欄だけに印を付ける: %+v", names["GateSummary"])
	}
}

type withMap struct {
	M map[string]int `json:"m"`
}
type withStringOpt struct {
	N int `json:"n,string"`
}
type withOmitZero struct {
	N int `json:",omitzero"`
}
type withTime struct {
	At time.Time `json:"at"`
}
type innerRoot struct {
	Root string `json:"root"`
}
type withCollision struct {
	Root string `json:"root"`
	innerRoot
}
type selfMarshaling struct {
	A string `json:"a"`
}

func (selfMarshaling) MarshalJSON() ([]byte, error) { return []byte(`"x"`), nil }

type embedsTime struct {
	time.Time
	A string `json:"a"`
}
type embedsPointer struct {
	*innerRoot
	A string `json:"a"`
}
type embedsTagged struct {
	innerRoot `json:",omitempty"`
	A         string `json:"a"`
}
type withStructOmitEmpty struct {
	In innerRoot `json:"in,omitempty"`
}

// 一覧に書けない形 (encoding/json が型と違う形・条件で書く) は、黙って違う一覧を出さずに誤りにする。
func TestOutputsRejectShapesTheListCannotDescribe(t *testing.T) {
	for name, typ := range map[string]reflect.Type{
		"map":             reflect.TypeFor[withMap](),
		",string":         reflect.TypeFor[withStringOpt](),
		",omitzero":       reflect.TypeFor[withOmitZero](),
		"Marshaler":       reflect.TypeFor[withTime](),
		"名前の重なり":          reflect.TypeFor[withCollision](),
		"Marshaler の埋め込み": reflect.TypeFor[embedsTime](),
		"自分で json を書く根":   reflect.TypeFor[selfMarshaling](),
		"ポインタの埋め込み":       reflect.TypeFor[embedsPointer](),
	} {
		if _, err := walkOutputTypes(typ); err == nil {
			t.Errorf("%s の欄は誤りにする", name)
		}
	}
	if types, err := walkOutputTypes(reflect.TypeFor[embedsTagged]()); err != nil || len(types[0].Fields) != 2 || types[0].Fields[0].Name != "root" {
		t.Errorf("タグの名前が無い埋め込みは親の欄として平たくする: %+v %v", types, err)
	}
	types, err := walkOutputTypes(reflect.TypeFor[withStructOmitEmpty]())
	if err != nil || types[0].Fields[0].Omitted {
		t.Errorf("struct の値の欄は omitempty でも必ず出るので、出ない印を付けない: %+v %v", types, err)
	}
}

// `-v` の行は vline でだけ書く: -v の分岐と -v 専用の関数の中で、fmt・io.WriteString・Write で直に書くと、表に無い見出しの行ができる。
// 分岐は条件のどこかに Verbose があるもの (`opt.Verbose && x` も)。-v の分岐から呼ぶ別の関数の中は見ない (出力の差分のテストが見る)。
func TestVerboseLinesAreWrittenOnlyThroughTheTable(t *testing.T) {
	files := parseReportPackage(t)
	verboseOnly := map[string]string{"write": "Exclusions", "writeErrorFiles": "", "writeUnreadableUnits": "", "writeCompare": ""}
	checked := 0
	for name, f := range files {
		fmtNames := map[string]bool{}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "fmt" || p == "io" {
				local := p
				if imp.Name != nil {
					local = imp.Name.Name
				}
				fmtNames[local] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var body *ast.BlockStmt
			switch n := n.(type) {
			case *ast.IfStmt:
				if mentions(n.Cond, "Verbose") {
					body = n.Body
				}
			case *ast.FuncDecl:
				if recv, ok := verboseOnly[n.Name.Name]; ok && receiverName(n) == recv {
					body = n.Body
				}
			}
			if body == nil {
				return true
			}
			checked++
			ast.Inspect(body, func(m ast.Node) bool {
				call, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && fmtNames[pkg.Name] && strings.HasPrefix(sel.Sel.Name, "Fprint") || sel.Sel.Name == "WriteString" || sel.Sel.Name == "Write" {
					t.Errorf("%s: -v の行を %s で直に書いている (vline と verboseLines の鍵を使う)", name, sel.Sel.Name)
				}
				return true
			})
			return true
		})
	}
	if checked < 5 {
		t.Fatalf("前提: -v の分岐と -v 専用の関数を見つける (%d か所しか無い)", checked)
	}
}

// mentions は式の中に、name を含む識別子 (大小を問わない。opt.Verbose を取った変数 verbose も) があるか。
func mentions(e ast.Expr, name string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.Contains(strings.ToLower(id.Name), strings.ToLower(name)) {
			found = true
		}
		return !found
	})
	return found
}

// vline の鍵は名前の付いた定数で渡し、定数は全部どこかで使う。文字列のリテラルは verboseKey へ黙って変わるので、
// 書き間違いがコンパイルを通り、本番の -v でだけ見出しの無い行になる。
func TestVerboseKeysArePassedAsNamedConstantsAndAllUsed(t *testing.T) {
	consts := map[string]bool{}
	for name := range verbosePrefix {
		consts[string(name)] = true
	}
	constNames := map[string]bool{}
	used := map[string]bool{}
	for name, f := range parseReportPackage(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ValueSpec:
				if id, ok := n.Type.(*ast.Ident); ok && id.Name == "verboseKey" {
					for i, nm := range n.Names {
						constNames[nm.Name] = true
						if lit, ok := n.Values[i].(*ast.BasicLit); !ok || !consts[mustUnquote(t, lit.Value)] {
							t.Errorf("verboseKey の定数 %s の値が -v の表の鍵に無い (見出しの無い行になる)", nm.Name)
						}
					}
				}
			case *ast.CallExpr:
				// 鍵は識別子 (定数か、定数を入れた変数) で渡す。リテラル・変換・式で渡すと、表に無い値が型を通る
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "vline" && len(n.Args) >= 2 {
					if _, ident := n.Args[1].(*ast.Ident); !ident {
						t.Errorf("%s: vline の鍵を識別子でない式で渡している (verboseKey の定数を使う)", name)
					}
				}
			case *ast.Ident:
				if name != "outputs.go" {
					used[n.Name] = true
				}
			}
			return true
		})
	}
	if len(constNames) != len(verboseLines) || len(consts) != len(verboseLines) {
		t.Fatalf("前提: -v の表の行ごとに定数が 1 つ (定数 %d・表 %d・見出し %d)", len(constNames), len(verboseLines), len(consts))
	}
	var unused []string
	for c := range constNames {
		if !used[c] {
			unused = append(unused, c)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf("使われていない -v の鍵: %v", unused)
	}
}

func mustUnquote(t *testing.T, s string) string {
	t.Helper()
	u, err := strconv.Unquote(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// compareHints の作る行は、どれも -v の表の見出しのどれかで始まる (writeCompare が見出しの無い行を書かない)。
func TestCompareHintsAllHaveATableHeading(t *testing.T) {
	var members []*group.Entry
	for _, f := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		members = append(members, entry(f+"||f()", f, 1, 1, 9))
	}
	outer := entry("x.go||f()", "x.go", 5, 1, 13)
	shadowed := entry("x.go|f|{closure}", "x.go", 5, 30, 12)
	hints := compareHints("/r", norm.L1, members, members)
	hints = append(hints, compareHints("/r", norm.L2, members, members)...)
	hints = append(hints, compareHints("/r", norm.L2, append([]*group.Entry{shadowed}, members[:2]...), append([]*group.Entry{outer, shadowed}, members...))...)
	kinds := map[verboseKey]bool{}
	for _, h := range hints {
		k := compareKey(h)
		if k == "" {
			t.Errorf("ヒント %q が -v の表のどの見出しでも始まらない", h)
		}
		kinds[k] = true
	}
	if len(kinds) != 4 {
		t.Fatalf("前提: 4 種類のヒントを作る (%d 種類): %q", len(kinds), hints)
	}
}

// receiverName はメソッドの受け口の型名 (関数なら空)。
func receiverName(f *ast.FuncDecl) string {
	if f.Recv == nil || len(f.Recv.List) == 0 {
		return ""
	}
	t := f.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func parseReportPackage(t *testing.T) map[string]*ast.File {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		files[p] = f
	}
	if len(files) == 0 {
		t.Fatal("前提: report package のファイルを読む")
	}
	return files
}
