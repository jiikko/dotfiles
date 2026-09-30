// Package displaycheck は「表示用の構造体へ新しい文字列フィールドを足したのに
// `Sanitize*ForDisplay` へ通し忘れる」を止める検査の本体 (issue 251 / 252)。
//
// disk / svc / docker の 3 つが同じ脅威を持つので、**判定を 1 実装に寄せる**
// (写経すると片方だけ直る形ができる。`mutation-verify-new-tests.md` の
// 「同じ判定を 2 箇所で別実装していないか」)。package ごとに違うのは
// 「どの型をどの関門が担当するか (Gates)」と「無害化しないと決めたフィールドとその理由
// (Exempt)」だけなので、それを Spec で受け取る。
//
// **なぜ読み手側の lint ではなくここで見るか**: 無害化は値の生成側 (doctor の各 package) で
// 済ませており、読み手 (glogx の doctor 画面) には termsafe の呼び出しが 1 つも現れない。
// 読み手側の構文 lint では通し忘れを検出できないので、**関門そのものの網羅性**を見るしかない。
//
// 🚨 **脅威モデル**: うっかりの通し忘れを止める。意図的に無害化しない値は Exempt に
// 理由つきで書く。**意図的な迂回は review の責務**で、この検査の担当ではない。
//
// 🚨 **検出しない形** (この形の指摘は採用せず、ここに記録する):
//   - 構造体の外 (ローカル変数・関数の戻り値・引数) を経由して表示に出る値
//   - **新しい構造体そのもの**を足したとき (Gates に載っている型しか見ない)
//   - 無害化の**中身**が正しいか (右辺が termsafe.* / sanitize* を呼んでいるかまでしか見ない。
//     正しさは各 Sanitize* の個別テストが見る)
//   - **関門がコピーを無害化して親へ書き戻さない形**。`r.Items = kept` のような書き戻しの
//     一文だけを消すと、ループ内の `it.X = termsafe…` は残るので緑のまま通る。
//     代入の有無を見る作りでは原理的に捕まえられない → **sink テスト**
//     (glogx/untrusted_display_test.go) が end-to-end で見る担当
package displaycheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// Gate は「その型を担当する関門と、関門の中でその型を指す識別子」。
//
// 🚨 **Recv (受け手の識別子) までキーにする**。関門の名前だけで持つと、同じ関門の中の
// 同名フィールドが互いをマスクする (実測 2026-09-04: 名前だけのキーでは、ある型の
// `Reason` の無害化を丸ごと消しても別の型の `Reason` が集合に残るため緑で通った)。
type Gate struct {
	Func string // Sanitize*ForDisplay の関数名
	Recv string // その関数の中でこの型を指す識別子 (out / r / it / …)
}

// Spec は 1 package ぶんの検査条件。
type Spec struct {
	// Dir はパースするディレクトリ (呼び出し側の package なら ".")
	Dir string
	// Package は package 名 (見つからなければ失敗させる。緑にしない)
	Package string
	// Gates は型 → 担当の関門。**ここに載っていない型は検査されない**
	// (新しい表示用の構造体を足したら、ここにも足すこと。それ自体は機械では止められない)
	Gates map[string]Gate
	// Exempt は無害化しないと決めた `型.フィールド` → **理由**。理由が空なら違反。
	// 未使用の Exempt も違反にする (抽出の射程が縮んだときの唯一の検出手段)
	Exempt map[string]string
	// MinNamedStringTypes は「underlying が string の named type」の期待下限。
	// 🚨 **package ごとに違う** (disk は Status / Risk / Guard / Outcome を持ち、svc は 0)。
	// 下の canary が「抽出が空」を検出するための値で、**0 の package では 0 を明示する**
	// (disk の形を前提に一律 >0 にすると、named type を持たない package で誤検知する)。
	MinNamedStringTypes int
	// WantChecked は「非 exempt の文字列フィールド」の期待件数。
	// 🚨 **射程が縮んだことを赤にする唯一の錨**。フィールドを足す / Exempt を増やすときは
	// この数も同じ commit で直すこと
	WantChecked int
}

// Run は 1 つの package について検査する。呼び出し側 (各 package の _test.go) が
// 自分の関門表と免除表を渡す。
func Run(t *testing.T, s Spec) {
	t.Helper()
	problems, fatal := check(s)
	if fatal != "" {
		t.Fatal(fatal)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// check は検査の本体。**t を触らず、見つけた違反を文字列で返す** —
// 🚨 こうしないと**検査器自身をテストできない** (t.Errorf を呼ぶ実装は、呼ばれたことを
// 呼び出し側から観測できないので、「違反を見逃す変異」を当てても緑のままになる。
// 実測 2026-09-04: 未使用 exempt の検出を外す変異が緑で通った)。
// fatal は「検査できなかった」(空でなければ問題の一覧は意味を持たない)。
func check(s Spec) (problems []string, fatal string) {
	sanitizeGate, sanitizeExempt, wantChecked := s.Gates, s.Exempt, s.WantChecked
	addf := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, s.Dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, fmt.Sprintf("パースできない: %v", err)
	}
	pkg, ok := pkgs[s.Package]
	if !ok {
		return nil, fmt.Sprintf("package %s が見つからない (検査できないので緑にしない)", s.Package)
	}

	// underlying が string の named type (Status / Risk / Outcome …)。これらも表示に出る文字列
	strNamed := map[string]bool{}
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if id, ok := ts.Type.(*ast.Ident); ok && id.Name == "string" {
				strNamed[ts.Name.Name] = true
			}
			return true
		})
	}

	// 構造体ごとの文字列系フィールド (string / []string / underlying が string の named type)。
	// 🚨 存在確認は structs で別に持つ。fields の有無で見ると **文字列フィールドを 1 つも持たない型**
	// (Report がそう) を「構造体が見つからない」と誤報する
	structs := map[string]bool{}
	fields := map[string][]string{}
	var unknownFields []string
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			structs[ts.Name.Name] = true
			for _, fld := range st.Fields.List {
				yes, unknown := stringish(fld.Type, strNamed)
				if unknown {
					// 型式を扱えない = 文字列かどうか判定できない。緑にしない
					name := "(埋め込み)"
					if len(fld.Names) > 0 {
						name = fld.Names[0].Name
					}
					unknownFields = append(unknownFields, ts.Name.Name+"."+name)
					continue
				}
				if !yes {
					continue
				}
				if len(fld.Names) == 0 {
					// 埋め込みフィールド (型だけ書く形)。名前が無いので突合できない
					unknownFields = append(unknownFields, ts.Name.Name+".(埋め込み文字列型)")
					continue
				}
				for _, nm := range fld.Names {
					if nm.IsExported() {
						fields[ts.Name.Name] = append(fields[ts.Name.Name], nm.Name)
					}
				}
			}
			return true
		})
	}

	// 関門が代入するフィールド名。`r.Entry = SanitizeEntryForDisplay(...)` のような
	// 別の関門への委譲も「触った」に数える (委譲先はその型の担当として別途検査される)
	touched := map[string]map[string]bool{}
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fd.Name.Name, "Sanitize") || fd.Body == nil {
				return true
			}
			set := map[string]bool{}
			ast.Inspect(fd.Body, func(n2 ast.Node) bool {
				as, ok := n2.(*ast.AssignStmt)
				if !ok {
					return true
				}
				// 🚨 **右辺が無害化を通っている代入だけ**を数える。代入の有無だけで見ると、
				// 同じ受け手・同じ名前の**別の代入**が集合に残って互いをマスクする
				// (実測 2026-09-04: `r.Failures = sanitizeLines(...)` を消しても
				// `r.Failures = append(r.Failures, …)` が残るため緑だった)。
				// `e.Label = e.Label` のような「触るだけ」もこれで落ちる
				for i, lhs := range as.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					id, ok := sel.X.(*ast.Ident) // `out.Entries` の `out`
					if !ok {
						continue
					}
					if i < len(as.Rhs) && !sanitizingRHS(as.Rhs[i]) {
						continue
					}
					set[id.Name+"."+sel.Sel.Name] = true
				}
				return true
			})
			touched[fd.Name.Name] = set
			return true
		})
	}

	// 🚨 抽出が空だと「違反 0 件」で緑になる。本走査と同じ経路の出力で先に止める
	if len(strNamed) < s.MinNamedStringTypes || len(structs) == 0 || len(touched) == 0 {
		return nil, fmt.Sprintf("抽出が空か射程が縮んだ (named=%d (want >= %d) structs=%d gates=%d)。パースが壊れている",
			len(strNamed), s.MinNamedStringTypes, len(structs), len(touched))
	}

	for _, u := range unknownFields {
		typ, _, _ := strings.Cut(u, ".")
		if _, ok := sanitizeGate[typ]; ok {
			addf("%s: この型の書き方は検査できない (ポインタ / map / 匿名構造体 / 他 package の型)。"+
				"文字列を含むなら stringish を広げるか、フィールドの型を変える", u)
		}
	}

	checked := 0
	usedExempt := map[string]bool{}
	for _, typ := range sortedGateKeys(sanitizeGate) {
		gate, recv := sanitizeGate[typ].Func, sanitizeGate[typ].Recv
		if !structs[typ] {
			addf("%s: 構造体が見つからない (rename したら sanitizeGate も直すこと)", typ)
			continue
		}
		flds := fields[typ] // 文字列フィールドを 1 つも持たない型 (Report) は空で正常
		if _, ok := touched[gate]; !ok {
			addf("%s: 関門 %s が見つからない (rename したら sanitizeGate も直すこと)", typ, gate)
			continue
		}
		for _, fld := range flds {
			key := typ + "." + fld
			if why, ok := sanitizeExempt[key]; ok {
				usedExempt[key] = true
				if why == "" {
					addf("%s: sanitizeExempt に理由が書かれていない", key)
				}
				continue
			}
			checked++
			if !touched[gate][recv+"."+fld] {
				addf("%s が %s の %s.%s を通っていない (表示に出る文字列は関門で無害化するか、"+
					"sanitizeExempt に理由つきで書く)", key, gate, recv, fld)
			}
		}
	}
	// 🚨 **未使用の exempt を違反にする**。exempt は「検査対象に在るが免除する」宣言なので、
	// 使われない = そのフィールドが検査から外れた印。**抽出の射程が縮んだときの唯一の検出手段**
	// (実測 2026-09-04: stringish が named type を見なくなる変異は、件数が 23 → 22 に減るだけで
	// 緑のまま通った)。フィールドを消したときの stale もここで落ちる
	for _, key := range sortedKeys(sanitizeExempt) {
		if !usedExempt[key] {
			addf("%s: sanitizeExempt に在るが検査対象に無い (フィールドが消えたか、抽出の射程から外れた)", key)
		}
	}
	if checked != wantChecked {
		addf("検査した文字列フィールド = %d 件 (want %d)。射程が変わっている: "+
			"フィールドを足した / exempt を増やしたなら wantChecked も直す。"+
			"減っているなら抽出 (stringish / ParseDir) が壊れた可能性", checked, wantChecked)
	}
	return problems, ""
}

// sanitizingRHS は右辺が無害化の経路を通っているか (termsafe.* / sanitize* / Sanitize* /
// DisplayablePath)。**名前ベースの近似**で、「無害化と名乗る関数を呼んでいるか」しか見ない
// (中身が正しいかは各 Sanitize* の個別テストが見る = README の「検出しない形」の 3 つ目)。
func sanitizingRHS(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		switch t := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := t.X.(*ast.Ident); ok && id.Name == "termsafe" {
				found = true
			}
		case *ast.Ident:
			if strings.HasPrefix(t.Name, "sanitize") || strings.HasPrefix(t.Name, "Sanitize") ||
				t.Name == "DisplayablePath" {
				found = true
			}
		}
		return !found
	})
	return found
}

// stringish は「表示に出る文字列」として扱う型か (string / []string / underlying が string)。
//
// 🚨 **fail-closed**。文字列を含みうるのに検査の仕組みが扱えない型式 (ポインタ / map /
// 匿名構造体 / 他 package の named type) は unknown で返し、呼び出し側が違反にする。
// false を返すと「新しい文字列フィールドを足したのに無音で緑」になる (敵対的レビュー 2026-09-04)。
func stringish(e ast.Expr, strNamed map[string]bool) (yes, unknown bool) {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name == "string" || strNamed[t.Name], false
	case *ast.ArrayType:
		return stringish(t.Elt, strNamed)
	case *ast.StarExpr:
		return stringish(t.X, strNamed)
	case *ast.SelectorExpr:
		// time.Time / time.Duration は文字列ではない。それ以外の他 package 型は判定できない
		if id, ok := t.X.(*ast.Ident); ok && id.Name == "time" {
			return false, false
		}
		return false, true
	case *ast.MapType, *ast.StructType, *ast.InterfaceType:
		return false, true
	}
	return false, false // func / chan / … は文字列を含まない
}

func sortedGateKeys(m map[string]Gate) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
