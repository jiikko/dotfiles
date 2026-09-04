package disk

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// 表示用の構造体と、それを無害化する関門。ネストした型 (Item / EntryOutcome / ItemOutcome) は
// 親の関門がループの中で処理するので、親の関門を担当として書く。
//
// 🚨 **この表に載っていない型は検査されない**。新しい表示用の構造体を足したら、ここにも足すこと
// (それ自体は機械では止められない = 下の「検出しない形」の 2 つ目)。
//
// 🚨 **recv (受け手の識別子) までキーにする**。関門の名前だけで持つと、同じ関門の中の
// 同名フィールドが互いをマスクする。実測 2026-09-04 (敵対的レビュー): 名前だけのキーでは
// `it.Reason` (ItemOutcome) の無害化を丸ごと消しても `e.Reason` (EntryOutcome) が
// 集合に残るため**緑のまま通った**。
var sanitizeGate = map[string]struct{ gate, recv string }{
	"Report":        {"SanitizeForDisplay", "out"},
	"Result":        {"SanitizeResultForDisplay", "r"},
	"Entry":         {"SanitizeEntryForDisplay", "e"},
	"Item":          {"SanitizeResultForDisplay", "it"},
	"DeleteReport":  {"SanitizeDeleteReportForDisplay", "out"},
	"EntryOutcome":  {"SanitizeDeleteReportForDisplay", "e"},
	"ItemOutcome":   {"SanitizeDeleteReportForDisplay", "it"},
	"CommandRecord": {"SanitizeCommandRecordForDisplay", "c"},
}

// wantChecked は「非 exempt の文字列フィールド」の期待件数。
// 🚨 **射程が縮んだことを赤にする唯一の錨**。exempt を持たない型 (DeleteReport /
// CommandRecord) は未使用 exempt の検出では守れないので、件数そのものを固定する
// (実測 2026-09-04: `json:"-"` を飛ばす変異は exempt を全部使ったまま 23 → 21 に減る)。
// フィールドを足す / exempt を増やすときは、この数も同じ commit で直すこと。
const wantChecked = 23

// 無害化しないと決めた文字列フィールド。**理由を必ず書く** (書けないなら無害化する側が正しい)。
var sanitizeExempt = map[string]string{
	// 🚨 この 2 つは「外部由来ではない」ではなく **呼び出し側の不変条件に依存**している。
	// snapshot 復元経路では Entry / Status も保存ファイルの中身になるが、glogx が
	// doctorSnapshotInCatalog (doctor_cache.go) でカタログへ束ね直し、knownDiskStatus で
	// 未知の Status を落としている。**依存先が変わったら無害化する側へ倒すこと**
	// (Entry.ID は doctor_view.go の y のコピー本文に [%s] として出る)
	"Result.Status":        "glogx の knownDiskStatus (doctor_cache.go) が未知値を落とす前提",
	"Item.Path":            "同一性を持つ値。書き換えず DisplayablePath で落とす (display.go の 🚨)",
	"ItemOutcome.Path":     "同上。削除の照合 (planDelete の itemKey) から外れるので書き換えない",
	"Entry.ID":             "glogx の doctorSnapshotInCatalog がカタログへ束ね直す前提 (復元経路)",
	"Entry.Paths":          "カタログのリテラル (catalog.go)。glob の元で、展開後は Item.Path 側の規律に載る",
	"Entry.Processes":      "カタログのリテラル (catalog.go)。guard の突合に使う固定のプロセス名",
	"Entry.Guard":          "Guard 型の enum (boottime / sim-device / process-absent)",
	"EntryOutcome.ID":      "カタログの固定 ID。DeleteReport は復元経路を持たない (engine が毎回作る)",
	"EntryOutcome.Outcome": "内部生成の enum (planned / deleted / …)",
	"EntryOutcome.Method":  "カタログの DeleteVia から導く内部語 (rm / trash / cli / propose)",
	"ItemOutcome.Outcome":  "内部生成の enum",
	"ItemOutcome.Staged":   "内部生成の予測不能名 (.glogx-delete-<hex>)。外部由来の文字は入らない",
	// 🚨 Remaining は「触ったが残った」パスの記録。**同一性を持つ値なので書き換えない**
	// (Item.Path と同じ規律)。今は Reason の件数算出にしか使っておらず表示に出ないが、
	// 表示に出すなら DisplayablePath で落とす側へ倒すこと (書き換えると照合から外れる)
	"EntryOutcome.Remaining": "同一性を持つパスの記録。件数の算出のみに使い、表示には出さない",
}

// TestSanitizeForDisplayCoversEveryStringField は「表示用の構造体に新しい文字列フィールドを
// 足したのに Sanitize*ForDisplay へ通し忘れる」を止める (issue 251)。
//
// **なぜ読み手側の lint ではなくここで見るか**: 無害化は値の生成側 (この module) で済ませており、
// 読み手 (glogx の doctor_view.go) には termsafe の呼び出しが 1 つも現れない
// (実測 2026-09-04: doctorRow.text への代入 35 件のうち右辺が termsafe. なのは 0 件)。
// 読み手側の構文 lint では通し忘れを検出できないので、**関門そのものの網羅性**を見るしかない。
//
// 🚨 **脅威モデル**: うっかりの通し忘れを止める。意図的に無害化しない値は sanitizeExempt に
// 理由つきで書く。**意図的な迂回は review の責務**で、この検査の担当ではない。
//
// 🚨 **検出しない形** (この形の指摘は採用せず、ここに記録する):
//   - 構造体の外 (ローカル変数・関数の戻り値・引数) を経由して表示に出る値
//   - **新しい構造体そのもの**を足したとき (sanitizeGate に載っている型しか見ない)
//   - 無害化の**中身**が正しいか (右辺が termsafe.* / sanitize* を呼んでいるかまでしか見ない。
//     正しさは各 Sanitize* の個別テストが見る)
//   - **関門がコピーを無害化して親へ書き戻さない形**。`r.Items = kept` (display.go) のような
//     書き戻しの一文だけを消すと、ループ内の `it.Ref = termsafe…` は残るので緑のまま通る。
//     この関門で実際に起きた退行はこの形だった (display.go の 🚨 コメント参照) が、
//     代入の有無を見る作りでは原理的に捕まえられない → **sink テスト**
//     (glogx/untrusted_display_test.go) が end-to-end で見る担当
//   - `doctor/svc` 側の関門 (svc/display.go)。**この検査は disk だけ**を見る
func TestSanitizeForDisplayCoversEveryStringField(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("パースできない: %v", err)
	}
	pkg, ok := pkgs["disk"]
	if !ok {
		t.Fatal("package disk が見つからない (検査できないので緑にしない)")
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
	if len(strNamed) == 0 || len(structs) == 0 || len(touched) == 0 {
		t.Fatalf("抽出が空 (named=%d structs=%d gates=%d)。パースが壊れている", len(strNamed), len(structs), len(touched))
	}

	for _, u := range unknownFields {
		typ, _, _ := strings.Cut(u, ".")
		if _, ok := sanitizeGate[typ]; ok {
			t.Errorf("%s: この型の書き方は検査できない (ポインタ / map / 匿名構造体 / 他 package の型)。"+
				"文字列を含むなら stringish を広げるか、フィールドの型を変える", u)
		}
	}

	checked := 0
	usedExempt := map[string]bool{}
	for _, typ := range sortedGateKeys(sanitizeGate) {
		gate, recv := sanitizeGate[typ].gate, sanitizeGate[typ].recv
		if !structs[typ] {
			t.Errorf("%s: 構造体が見つからない (rename したら sanitizeGate も直すこと)", typ)
			continue
		}
		flds := fields[typ] // 文字列フィールドを 1 つも持たない型 (Report) は空で正常
		if _, ok := touched[gate]; !ok {
			t.Errorf("%s: 関門 %s が見つからない (rename したら sanitizeGate も直すこと)", typ, gate)
			continue
		}
		for _, fld := range flds {
			key := typ + "." + fld
			if why, ok := sanitizeExempt[key]; ok {
				usedExempt[key] = true
				if why == "" {
					t.Errorf("%s: sanitizeExempt に理由が書かれていない", key)
				}
				continue
			}
			checked++
			if !touched[gate][recv+"."+fld] {
				t.Errorf("%s が %s の %s.%s を通っていない (表示に出る文字列は関門で無害化するか、"+
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
			t.Errorf("%s: sanitizeExempt に在るが検査対象に無い (フィールドが消えたか、抽出の射程から外れた)", key)
		}
	}
	if checked != wantChecked {
		t.Errorf("検査した文字列フィールド = %d 件 (want %d)。射程が変わっている: "+
			"フィールドを足した / exempt を増やしたなら wantChecked も直す。"+
			"減っているなら抽出 (stringish / ParseDir) が壊れた可能性", checked, wantChecked)
	}
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

func sortedGateKeys(m map[string]struct{ gate, recv string }) []string {
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
