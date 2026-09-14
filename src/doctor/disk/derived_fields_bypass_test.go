package disk

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Report.Total / Result.Size は導出値なので、**所有者パッケージの外**からは
// WithResults / WithItems を通してしか差し替えない (issue 372)。
//
// なぜ機械で縛るか: `rep.Results = xs` と直接書いても build もテストも通り、
// **合計だけが古いまま残る** (画面上部の「解放可能 N GB」が行と食い違う / 消したのに減らない)。
// 寄せる前の glogx 側 3 箇所がまさにその形だった。「書く人が覚えているか」に依存させない
// (glogx の issues_rows_setter_test.go と同じ形)。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8):
//   - 止めるのは「うっかり `rep.Results = xs` / `r.Items = its` と直接書く」典型形だけ。
//     意図的な迂回 (reflect / unsafe) は review の責務
//   - 射程は **所有者パッケージ (disk) の外** かつ **doctor/disk を import しているファイル**。
//     disk 自身を射程に入れない理由は 2 つ: (a) WithResults / WithItems / Scan の実装そのものが
//     導出フィールドを書く唯一の出典であること (b) delete_test.go が
//     「Size と Items を故意に食い違わせる」fixture を持っており (1TB と申告する / Items だけ
//     空にする)、そこは不整合を作ることがテストの主張そのものであること
//   - 見るのは 2 組: `<disk.Report>.Results = / .Total =` と `<disk.Result>.Items = / .Size =`
//     (複合代入 `+=` と `x++` も含む)
//   - **検出しない**: reflect / json.Unmarshal 経由 (保存した snapshot の復元は Results と Total を
//     同時に埋めるので正しい) / 別名変数経由 (`p := &rep; p.Results = …`) / 関数へ渡して中で書く形 /
//     ローカル変数に代入した中間結果が call の戻り値のとき (`rep := build()` は型を追わない) /
//     import に別名を付けたうえで**さらに**その別名を再束縛する形 / 型エイリアス経由。
//     これらは review の責務
//   - フィールド名の表は **名前が衝突したら落とす** (同じ名前が disk 型と非 disk 型の両方で
//     宣言されていたら owner に数えない)。誤検出を出さない側へ倒しているので、
//     衝突を入れると**この検査は黙って弱くなる**。それを見えるようにするため、
//     実在する owner (`Disk` / `diskRep`) が表に居ることを下で assert している
//   - 🚨 **この射程は実装後に実物と突き合わせて確定させたもの**。初版は「所有者パッケージの外」
//     としか書いておらず、`disk.EntryOutcome.Items` (別の型だが同名フィールド) と
//     glogx の `doctorDiskCache.Total` を**誤検出する**形だった。どちらも実在するので、
//     除外ではなく「型で見分ける」側へ直した (canary の否定例がその 2 つ)
//
// 🚨 **CI の配線**: この検査は doctor の `make test` で走るが、走査対象には glogx が含まれる。
// そのため `.github/workflows/src_doctor.yml` の paths に `src/glogx/**` を足してある。
// 外すと「glogx だけ変えた push」でこの検査が 1 度も走らない (false green)。
//
// scanDerivedWrites は 1 ファイル分の違反と「型を解決できた参照の数」を返す
// (本走査と canary の共通経路)。
//
// 🚨 canary は**この関数を通す**こと。式をコピーして別に書くと、canary は「コピーした
// ロジック」を検査するだけで本走査の破損を検出しない。

type ownerKind int

const (
	ownerNone ownerKind = iota
	ownerReport
	ownerResult
	ownerResultSlice
)

// ownerFields は「disk.Report / disk.Result 型で宣言された struct フィールド名」の表。
// `sn.Disk.Results = …` / `v.diskRep.Results = …` のような**フィールド経由**の owner を
// 解決するために要る (型解決を go/types に頼らない代わりの近似)。
type ownerFields map[string]ownerKind

// diskScanner は 1 ファイルを見るための文脈 (disk import のローカル名 + フィールド表)。
type diskScanner struct {
	diskName string // このファイルでの doctor/disk のローカル名 (既定 "disk")
	fields   ownerFields
}

// diskLocalName は doctor/disk の import 名を返す (import していなければ "")。
func diskLocalName(file *ast.File) string {
	for _, im := range file.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil || p != "doctor/disk" {
			continue
		}
		if im.Name != nil {
			if im.Name.Name == "_" || im.Name.Name == "." {
				return "" // 検出しない形 (射程外)
			}
			return im.Name.Name
		}
		return "disk"
	}
	return ""
}

// typeKind は型式が disk.Report / disk.Result / []disk.Result のどれかを返す。
func (s diskScanner) typeKind(e ast.Expr) ownerKind {
	switch t := e.(type) {
	case *ast.StarExpr:
		return s.typeKind(t.X)
	case *ast.ParenExpr:
		return s.typeKind(t.X)
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok && id.Name == s.diskName {
			switch t.Sel.Name {
			case "Report":
				return ownerReport
			case "Result":
				return ownerResult
			}
		}
	case *ast.ArrayType:
		if t.Len == nil && s.typeKind(t.Elt) == ownerResult {
			return ownerResultSlice
		}
	}
	return ownerNone
}

// collectOwnerFields は struct のフィールド宣言を表へ足す。
// 同じ名前が別の型でも宣言されていたら (衝突) owner から落とす。
func (s diskScanner) collectOwnerFields(file *ast.File, into ownerFields, conflict map[string]bool) {
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, f := range st.Fields.List {
			k := s.typeKind(f.Type)
			for _, name := range f.Names {
				if k == ownerNone {
					conflict[name.Name] = true
					continue
				}
				if prev, seen := into[name.Name]; seen && prev != k {
					conflict[name.Name] = true
					continue
				}
				into[name.Name] = k
			}
		}
		return true
	})
}

// kindOf は式が指す値が disk.Report / disk.Result かを近似で返す。
func (s diskScanner) kindOf(e ast.Expr, owners map[string]ownerKind) ownerKind {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return s.kindOf(t.X, owners)
	case *ast.StarExpr: // (*p).Results = … / *p = …
		return s.kindOf(t.X, owners)
	case *ast.Ident:
		return owners[t.Name]
	case *ast.SelectorExpr:
		// Report.Results は []disk.Result なので、フィールド表を待たずにここで解決する
		// (Results は disk 内で宣言されており、射程外なので表には載らない)
		if t.Sel.Name == "Results" && s.kindOf(t.X, owners) == ownerReport {
			return ownerResultSlice
		}
		return s.fields[t.Sel.Name]
	case *ast.IndexExpr: // rep.Results[i].Items = … / rs[i].Size = …
		if s.kindOf(t.X, owners) == ownerResultSlice {
			return ownerResult
		}
	}
	return ownerNone
}

// rhsKind は右辺が disk.Report / disk.Result を作る式かを返す
// (`disk.Report{…}` / `&disk.Result{…}` / 既に owner と分かっている変数)。
func (s diskScanner) rhsKind(e ast.Expr, owners map[string]ownerKind) ownerKind {
	switch t := e.(type) {
	case *ast.UnaryExpr:
		return s.rhsKind(t.X, owners)
	case *ast.ParenExpr:
		return s.rhsKind(t.X, owners)
	case *ast.CompositeLit:
		return s.typeKind(t.Type)
	case *ast.Ident:
		return owners[t.Name]
	case *ast.SelectorExpr:
		return s.fields[t.Sel.Name]
	}
	return ownerNone
}

// derivedOf は「そのフィールド名が、どの owner の導出フィールドか」を返す。
func derivedOf(field string) ownerKind {
	switch field {
	case "Results", "Total":
		return ownerReport
	case "Items", "Size":
		return ownerResult
	}
	return ownerNone
}

func (s diskScanner) scanFunc(fset *token.FileSet, path string, fn *ast.FuncDecl, offenders *[]string, resolved *int) {
	owners := map[string]ownerKind{}
	addField := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			k := s.typeKind(f.Type)
			if k == ownerNone {
				continue
			}
			for _, n := range f.Names {
				owners[n.Name] = k
			}
		}
	}
	addField(fn.Recv)
	if fn.Type != nil {
		addField(fn.Type.Params)
		addField(fn.Type.Results)
	}

	report := func(pos token.Pos, what string) {
		p := fset.Position(pos)
		*offenders = append(*offenders,
			filepath.ToSlash(path)+":"+strconv.Itoa(p.Line)+" ("+fn.Name.Name+", "+what+")")
	}
	check := func(lhs ast.Expr, what string) {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok {
			return
		}
		want := derivedOf(sel.Sel.Name)
		if want == ownerNone {
			return
		}
		if s.kindOf(sel.X, owners) == want {
			report(sel.Pos(), what+" "+sel.Sel.Name)
		}
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		// var rep disk.Report / var rs []disk.Result
		if vs, ok := n.(*ast.ValueSpec); ok {
			if k := s.typeKind(vs.Type); k != ownerNone {
				for _, name := range vs.Names {
					owners[name.Name] = k
				}
			} else {
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if rk := s.rhsKind(vs.Values[i], owners); rk != ownerNone {
							owners[name.Name] = rk
						}
					}
				}
			}
		}
		// 型が解決できた参照を数える (読み書きの別を問わない)。
		// 🚨 違反を直し切ると違反数は 0 になるので、**判定が生きている証拠**をここで稼ぐ
		// (0 件 = 緑 の false green を塞ぐ。verify-execution-not-just-exit-code の「canary」)。
		if sel, ok := n.(*ast.SelectorExpr); ok && s.kindOf(sel.X, owners) != ownerNone {
			*resolved++
		}
		if inc, ok := n.(*ast.IncDecStmt); ok {
			check(inc.X, "インクリメント")
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		// rep := disk.Report{…} / r := other (owner)
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || i >= len(as.Rhs) {
				continue
			}
			if rk := s.rhsKind(as.Rhs[i], owners); rk != ownerNone {
				owners[id.Name] = rk
			}
		}
		what := "直接代入"
		if as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
			what = "複合代入"
		}
		for _, lhs := range as.Lhs {
			check(lhs, what)
		}
		return true
	})
}

// scanDerivedWrites は 1 ファイルを走査する (本走査と canary の共通経路)。
func scanDerivedWrites(fset *token.FileSet, path string, file *ast.File, fields ownerFields) (offenders []string, resolved int) {
	name := diskLocalName(file)
	if name == "" {
		return nil, 0
	}
	s := diskScanner{diskName: name, fields: fields}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		s.scanFunc(fset, path, fn, &offenders, &resolved)
	}
	return offenders, resolved
}

const derivedCanarySrc = `package zz

import "doctor/disk"

type holder struct {
	Disk    disk.Report
	diskRep *disk.Report
	rs      []disk.Result
}

// 別の型だが同じフィールド名を持つ (誤検出してはいけない側)
type cache struct {
	Total int64
	Items []string
}

func canaryReport(rep disk.Report, h *holder) {
	rep.Results = nil                 // 1 直接代入
	rep.Total = 0                     // 2 直接代入
	rep.Total += 1                    // 3 複合代入
	rep.Total++                       // 4 インクリメント
	h.Disk.Results = nil              // 5 フィールド経由
	h.diskRep.Total = 0               // 6 ポインタのフィールド経由
	local := disk.Report{}
	local.Results = nil               // 7 複合リテラル由来のローカル変数
}

func canaryResult(r disk.Result, h *holder, rep disk.Report) {
	r.Items = nil                     // 8 直接代入
	r.Size = 0                        // 9 直接代入
	h.rs[0].Items = nil               // 10 スライス要素経由
	rep.Results[1].Size = 0           // 11 Report.Results の要素経由
	var v disk.Result
	v.Size = 0                        // 12 var 宣言経由
}

// 以下は**報告されてはいけない**形
func canaryNegative(c cache, eo disk.EntryOutcome, it disk.Item, rep disk.Report) bool {
	c.Total = 0        // 別の型の Total
	c.Items = nil      // 別の型の Items
	eo.Items = nil     // disk.EntryOutcome.Items (Result ではない)
	it.Size = 0        // disk.Item.Size (Result ではない)
	return rep.Total == 0 && len(rep.Results) == 0
}
`

func TestDerivedFieldsGoThroughOwner(t *testing.T) {
	fset := token.NewFileSet()

	// 🚨 canary: 既知の違反と「報告してはいけない形」を**本走査と同じ関数**に通す。
	// これが無いと、判定が壊れて 0 件になっても「違反 0 件 = 緑」で通る。
	canaryFile, err := parser.ParseFile(fset, "zz_canary.go", derivedCanarySrc, 0)
	if err != nil {
		t.Fatalf("canary をパースできない: %v", err)
	}
	canaryFields := ownerFields{}
	canaryConflict := map[string]bool{}
	diskScanner{diskName: "disk"}.collectOwnerFields(canaryFile, canaryFields, canaryConflict)
	for name, want := range map[string]ownerKind{"Disk": ownerReport, "diskRep": ownerReport, "rs": ownerResultSlice} {
		if canaryFields[name] != want {
			t.Fatalf("canary のフィールド表が壊れている: %s=%v (want %v)", name, canaryFields[name], want)
		}
	}
	if !canaryConflict["Total"] || !canaryConflict["Items"] {
		t.Fatal("canary: 非 disk 型のフィールド名が衝突として記録されていない (誤検出を止める側が効いていない)")
	}
	canaryHits, canaryResolved := scanDerivedWrites(fset, "zz_canary.go", canaryFile, canaryFields)
	if len(canaryHits) != 12 {
		t.Fatalf("canary の検出が %d 件 (期待 12)。判定が壊れている:\n  %s",
			len(canaryHits), strings.Join(canaryHits, "\n  "))
	}
	if canaryResolved == 0 {
		t.Fatal("canary: 型を解決できた参照が 0 件 (kindOf が壊れている)")
	}
	for _, h := range canaryHits {
		if strings.Contains(h, "canaryNegative") {
			t.Fatalf("canary: 報告してはいけない形を報告した (誤検出): %s", h)
		}
	}

	// 本走査。所有者パッケージ (disk) 自身は射程外 (ヘッダの脅威モデル参照)。
	root := filepath.Join("..", "..") // src/
	owner := filepath.Clean(filepath.Join("..", "disk"))
	type parsed struct {
		path string
		file *ast.File
	}
	var files []parsed
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "vendor", "tmp", ".git":
				return filepath.SkipDir
			}
			if filepath.Clean(path) == owner {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		files = append(files, parsed{path: path, file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("走査できない: %v", err)
	}

	fields := ownerFields{}
	conflict := map[string]bool{}
	consumers := 0
	for _, p := range files {
		name := diskLocalName(p.file)
		if name == "" {
			continue
		}
		consumers++
		diskScanner{diskName: name}.collectOwnerFields(p.file, fields, conflict)
	}
	for name := range conflict {
		delete(fields, name)
	}

	// 🚨 走査が壊れて何も見なくなっても緑にならないよう下限を置く。
	// **owner 表の assert より先に見ること**: 走査が空振りしていると表も空になり、
	// 「衝突で表から落ちた」という誤った診断が出る (変異 M5 で実測した)。
	// 2026-09-14 実測: .go 270 件を走査し、うち doctor/disk を import しているのは 18 件。
	if len(files) < 100 {
		t.Fatalf("走査した .go が %d 件しかない (下限 100)。WalkDir の除外が壊れている", len(files))
	}
	if consumers < 10 {
		t.Fatalf("doctor/disk を import しているファイルが %d 件しかない (下限 10)。"+
			"import の判定が壊れているか、走査が glogx へ届いていない", consumers)
	}

	// 🚨 実在する owner が表に居ることを固定する。フィールド名が衝突すると表から落ちて
	// **検査が黙って弱くなる**ので、そのときはここで落ちて気づける (issue 372)。
	for name, want := range map[string]ownerKind{"Disk": ownerReport, "diskRep": ownerReport} {
		if fields[name] != want {
			names := make([]string, 0, len(fields))
			for n := range fields {
				names = append(names, n)
			}
			sort.Strings(names)
			t.Fatalf("owner フィールド表に %s (%v) が居ない。同名フィールドが別の型でも宣言されて "+
				"衝突で落ちた可能性がある (落ちると検査が黙って弱くなる)。表: %v", name, want, names)
		}
	}

	var offenders []string
	resolved, withoutFields := 0, 0
	for _, p := range files {
		o, r := scanDerivedWrites(fset, p.path, p.file, fields)
		offenders = append(offenders, o...)
		resolved += r
		// 空の表でもう一度走らせて「表が判定に効いているか」を測る (下の assert 用)
		_, r0 := scanDerivedWrites(fset, p.path, p.file, ownerFields{})
		withoutFields += r0
	}

	// 🚨 型解決そのものが生きていることの下限 (2026-09-14 実測: 117 件)。
	// 違反を直し切ると違反数は 0 になるので、**判定が生きている証拠**はここで稼ぐ。
	if resolved < 60 {
		t.Fatalf("型を解決できた Report/Result への参照が %d 件しかない (下限 60)。"+
			"kindOf が壊れている", resolved)
	}
	// 🚨 フィールド表が**本走査に配線されている**ことを固定する。表を作っても渡し忘れれば
	// `sn.Disk.Results` / `v.diskRep.Results` の経路が丸ごと見えなくなるが、canary は
	// 自前の表を作るので緑のまま通ってしまう (変異 M6 で実測した)。
	// 閾値ではなく「表あり > 表なし」で見るので、実測値が動いても腐らない。
	if resolved <= withoutFields {
		t.Fatalf("フィールド表が判定に効いていない (表あり %d 件 = 表なし %d 件)。"+
			"本走査へ fields を渡していない可能性がある", resolved, withoutFields)
	}
	if len(offenders) > 0 {
		t.Errorf("disk.Report / disk.Result の導出フィールドを所有者の外から直接書いている "+
			"(Results は WithResults、Items は WithItems を通すこと。通さないと Total / Size が "+
			"古いまま残る。issue 372):\n  %s", strings.Join(offenders, "\n  "))
	}
	t.Logf("走査 .go=%d 件 / disk を import=%d 件 / 型解決できた参照=%d 件 / 違反=%d 件",
		len(files), consumers, resolved, len(offenders))
}
