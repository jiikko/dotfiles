// Package typed は Go の呼び出し元を、go/packages の型の情報で確定する (明示の `callers -typed` のときだけ使う)。
//
// 構文の近似 (名前だけの照合) では、受け手の型が違う同名のメソッド (a.Save と b.Save) を分けられない。ここでは module ごとに
// 型を検査し、呼び出しの式が指す関数を「package のパス・受け手の型・名前」の鍵で比べる。
//
// 型のオブジェクトの同一性 (ポインタ) では比べない: module ごとに別々に型を検査するので、同じ関数でも module が違えば別のオブジェクトになる。
// 確定できないもの (interface を通した呼び出し・関数値として渡した先・読み込みや型の検査に失敗したパッケージ) は、確定に数えず別に返す。
// 確定できないパッケージを、正常な 0 件として返さない。
package typed

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Call は 1 つの呼び出し (か参照) の位置。File は repo の root からの相対。
type Call struct {
	Key    string `json:"key"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Caller string `json:"caller"` // 囲む関数 (Recv.Name か Name)。パッケージの直下なら空
	Kind   string `json:"kind"`   // call (確定) / interface (interface を通した呼び出し) / value (関数値・method value として渡した)
}

// Package は型で確定できなかったパッケージと、その理由。
type Package struct {
	Path    string   `json:"path"`
	Reason  string   `json:"reason"`
	Files   []string `json:"files"`
	Partial bool     `json:"partial"` // 依存の誤りで結果が欠けうるが、走査はして呼び出しを数えた (true) か、走査していない (false)
}

// Result は callers -typed の結果。
type Result struct {
	Modules   []string  `json:"modules"`
	Packages  int       `json:"packages"`
	Keys      []string  `json:"keys"`      // 問い合わせに当たった鍵
	Confirmed []Call    `json:"confirmed"` // 型で確定した呼び出し
	Unknown   []Call    `json:"unknown"`   // 呼び出し先を確定できない呼び出し (interface・関数値)
	Untyped   []Package `json:"untyped"`   // 型で確定できないパッケージ
	Unread    []string  `json:"unread"`    // 起点の中の Go のファイルのうち、この構成で読んでいないもの
}

// Modules は、起点の dir (root からの相対。"" は repo 全体) から読む module の root (絶対パス) を返す。
// 起点から上へ辿った最も近い go.mod と、起点の下の go.mod。third_party と testdata の下は除く (replace 用の stub・fixture)。
// 辿れない dir があれば誤りを返す (module を数え漏らしたまま結果を返さない)。
func Modules(root string, prefixes []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(dir string) {
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	for _, p := range prefixes {
		start := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(p, "/")))
		for dir := start; strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
			if fileExists(filepath.Join(dir, "go.mod")) {
				add(dir)
				break
			}
			if dir == root {
				break
			}
		}
		err := filepath.WalkDir(start, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "third_party", "testdata", ".git", "node_modules", "vendor":
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == "go.mod" {
				add(filepath.Dir(path))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Callers は modules を読み、query に当たる関数の呼び出し元を返す。files は起点の中の Go のファイル (root からの相対)。
func Callers(root string, modules []string, query string, files []string) (Result, error) {
	res := Result{Modules: rel(root, modules)}
	keys := map[string]bool{}
	seen := map[string]bool{}
	read := map[string]bool{}
	for _, dir := range modules {
		cfg := &packages.Config{
			Dir:   dir,
			Tests: true,
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax |
				packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
		}
		pkgs, err := packages.Load(cfg, "./...")
		if err != nil {
			res.Untyped = append(res.Untyped, Package{Path: relOne(root, dir), Reason: err.Error()})
			continue
		}
		for _, p := range pkgs {
			res.Packages++
			// 「読んだ」は構文を読んだファイルの位置 (//line で元のファイルへ写した後) から取る。一覧 (GoFiles・CompiledGoFiles) からは取らない:
			// cgo の処理が黙って落ちると、一覧に載っていても中身を走査していないファイルを読んだと数える。build tag で外れたファイルは構文が無いので読んでいない
			for _, file := range p.Syntax {
				read[relOne(root, p.Fset.Position(file.Package).Filename)] = true
			}
			// IllTyped は依存のパッケージの誤りでも真になる (x/tools の Package.IllTyped)。自分の誤りが無ければ走査し、結果が欠けうると注記する
			if p.IllTyped && len(p.Errors) == 0 && p.TypesInfo != nil {
				res.Untyped = append(res.Untyped, Package{Path: p.ID, Reason: "依存のパッケージに型の誤りがある (このパッケージの結果は欠けうる)", Files: rel(root, p.CompiledGoFiles), Partial: true})
			} else if len(p.Errors) > 0 || p.TypesInfo == nil {
				reason := "型の検査が通らない"
				if len(p.Errors) > 0 {
					reason = p.Errors[0].Error()
				}
				res.Untyped = append(res.Untyped, Package{Path: p.ID, Reason: reason, Files: rel(root, p.CompiledGoFiles)})
				continue
			}
			for _, file := range p.Syntax {
				scan(root, p, file, query, keys, seen, &res)
			}
		}
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".go") && !read[f] {
			res.Unread = append(res.Unread, f)
		}
	}
	for k := range keys {
		res.Keys = append(res.Keys, k)
	}
	sort.Strings(res.Keys)
	sortCalls(res.Confirmed)
	sortCalls(res.Unknown)
	sort.Slice(res.Untyped, func(i, j int) bool { return res.Untyped[i].Path < res.Untyped[j].Path })
	sort.Strings(res.Unread)
	return res, nil
}

// scan は 1 つのファイルの呼び出しの式と、関数を値として使った所を集める。
// test の変種 (p と p [p.test]) で同じファイルが 2 回読まれるので、位置と区分で重複を除く。
func scan(root string, p *packages.Package, file *ast.File, query string, keys, seen map[string]bool, res *Result) {
	info := p.TypesInfo
	inCall := map[*ast.Ident]bool{}
	record := func(id *ast.Ident, fn *types.Func, kind string, caller string) {
		key := Key(fn)
		if key == "" {
			return
		}
		iface := isInterfaceMethod(fn)
		if kind == "call" && iface {
			kind = "interface"
		}
		// interface を通した呼び出しは、名前が同じメソッドなら問い合わせの鍵の型に届きうる (型が interface を満たすかは見ない)。
		// Matches だけで絞らない: 鍵が違うので、A.Save で引くと Saver.Save を通した呼び出しが消える。確定には数えない
		matched := Matches(key, query)
		if !matched && (!iface || fn.Name() != lastName(query)) {
			return
		}
		pos := p.Fset.Position(id.Pos())
		c := Call{Key: key, File: relOne(root, pos.Filename), Line: pos.Line, Column: pos.Column, Caller: caller, Kind: kind}
		id2 := c.File + ":" + strconv.Itoa(c.Line) + ":" + strconv.Itoa(c.Column) + ":" + kind
		if seen[id2] {
			return
		}
		seen[id2] = true
		if matched {
			keys[key] = true // 名前が同じだけで広げた interface の鍵は、当たった鍵に数えない (存在しない Typo.Save でも当たったように見える)
		}
		if kind == "call" {
			res.Confirmed = append(res.Confirmed, c)
		} else {
			res.Unknown = append(res.Unknown, c)
		}
	}
	for _, decl := range file.Decls {
		caller := ""
		if fd, ok := decl.(*ast.FuncDecl); ok {
			caller = funcName(fd)
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id := calleeIdent(call.Fun); id != nil {
				if fn, ok := info.Uses[id].(*types.Func); ok {
					inCall[id] = true
					record(id, fn, "call", caller)
				}
			}
			return true
		})
		// 呼び出しの位置に無い関数の参照 (関数値・method value として渡した): 呼び出し先は渡した先で決まるので確定に数えない
		ast.Inspect(decl, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || inCall[id] {
				return true
			}
			if fn, ok := info.Uses[id].(*types.Func); ok {
				record(id, fn, "value", caller)
			}
			return true
		})
	}
}

// calleeIdent は呼び出しの式の関数の部分の識別子 (括弧・型引数の指定をほどく)。取れなければ nil。
func calleeIdent(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.SelectorExpr:
			return x.Sel
		case *ast.Ident:
			return x
		default:
			return nil
		}
	}
}

// Key は関数の鍵: package のパス + (受け手の型 +) 名前。ジェネリクスはインスタンス化の前に戻し、ポインタの受け手は型にそろえる。
// 埋め込みで昇格したメソッドは、types が宣言の受け手を返すので、宣言した型の鍵になる。package に属さない関数 (組み込み) は "" を返す。
func Key(fn *types.Func) string {
	fn = fn.Origin()
	if fn.Pkg() == nil {
		return ""
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig != nil && sig.Recv() != nil {
		t := sig.Recv().Type()
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		}
		switch nt := t.(type) {
		case *types.Named:
			return fn.Pkg().Path() + "." + nt.Origin().Obj().Name() + "." + fn.Name()
		case *types.Alias:
			return fn.Pkg().Path() + "." + types.Unalias(nt).String() + "." + fn.Name()
		}
		return fn.Pkg().Path() + "." + fn.Name()
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// Matches は鍵が問い合わせに当たるか: 完全一致・修飾の境目 (. か import path の /) からの後方一致 (Scope.Whole・Whole・repo.Scope.Whole)。
func Matches(key, query string) bool {
	return key == query || strings.HasSuffix(key, "."+query) || strings.HasSuffix(key, "/"+query)
}

// lastName は問い合わせの最後の要素 (A.Save なら Save)。
func lastName(query string) string {
	if i := strings.LastIndexAny(query, "./"); i >= 0 {
		return query[i+1:]
	}
	return query
}

func isInterfaceMethod(fn *types.Func) bool {
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() == nil {
		return false
	}
	_, ok := sig.Recv().Type().Underlying().(*types.Interface)
	return ok
}

func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	t := fd.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		case *ast.Ident:
			return x.Name + "." + fd.Name.Name
		}
		return fd.Name.Name
	}
}

func sortCalls(cs []Call) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Key != cs[j].Key {
			return cs[i].Key < cs[j].Key
		}
		if cs[i].File != cs[j].File {
			return cs[i].File < cs[j].File
		}
		if cs[i].Line != cs[j].Line {
			return cs[i].Line < cs[j].Line
		}
		return cs[i].Column < cs[j].Column
	})
}

func rel(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, relOne(root, p))
	}
	return out
}

func relOne(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}
