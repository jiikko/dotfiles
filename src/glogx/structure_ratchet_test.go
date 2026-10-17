package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// main を薄く保つための構造の検査 (issue 817 の H)。golangci には「main を薄く」をそのまま表す規則が無いので、
// 構造をテストで検査し、例外は下の一覧に理由つきで書かせる (一覧の差分がレビューに出る)。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8):
//   - 止めるもの: main の直下に非テストの .go が増える (H-1)・browseModel のメソッドを持つ非テストのファイルが増える
//     (新しい画面が browseModel にメソッドを生やす形。filer / lan が辿った形。H-2)・internal の package の公開名が
//     golden と食い違う (742 で doctorview が機械的に 201 個を公開した形。H-3)
//   - 一覧から消し忘れも落とす (移したのに一覧に残っている = 一覧が実体とずれる)
//   - **止めない形**: 一覧にあるファイルの中での肥大 (tui.go に 1,000 行足す・既存のファイルに別の画面を書き足す)。
//     総行数の上限 (H-4) は採らなかった: 正当な機能追加も止め、通すために行数のためだけの移設を誘う
//     (src/glogx/CLAUDE.md「行数や責務の見た目で割らない」と逆向き)。肥大はレビューの責務
//   - 止めない形: 一覧・golden を更新して通す変更 (理由の妥当性はレビューの責務)・internal の package どうしの
//     依存の向き (Go のコンパイラは main の import だけを禁じる。必要になったら depguard で足す)・
//     テストのファイル (main に残るテストは browseModel と組み合わせるものに限る、はレビューの責務)

// mainSourceFiles は main の直下に置いてよい非テストの .go と、置く理由 (H-1)。
// 足すときは理由を書く。画面の本体なら、まず internal/ のパッケージに置けないかを考える (issue 817)。
var mainSourceFiles = map[string]string{
	"main.go":              "起動 (CLI の入口)",
	"options.go":           "起動 (CLI フラグ)",
	"tui.go":               "browseModel の状態機械 (画面の配線)",
	"fullscreen.go":        "全画面ビューアの排他と開く口 (browseModel のメソッド)",
	"hint_surfaces.go":     "最下行の案内の面ごとの配線 (browseModel のメソッド)",
	"zoom.go":              "開閉演出 (browseModel の描画が使う)",
	"probe.go":             "計測フック (起動時の配線)",
	"autobuild.go":         "自動再ビルドの通知 (起動時の配線)",
	"ime.go":               "ブラウズ中の IME 英数切替 (起動時の配線)",
	"ime_tis_darwin.go":    "IME の macOS 実装 (ビルドタグ)",
	"ime_tis_stub.go":      "IME の macOS 以外の空実装 (ビルドタグ)",
	"open_workspace.go":    "e で nvim を開く (browseModel のメソッド)",
	"render.go":            "コミット行の描画 (gitlog / github の型を使う)",
	"gitlog_watch.go":      "git log の外部変更の監視 (browseModel のメソッド)",
	"external_commands.go": "外部コマンドのラッパーと差し替え点 (C-1 で extcmd へ)",
	"cli_health.go":        "claude / codex CLI のログイン状態の検査",
	"claude_version.go":    "claude CLI の新版の検出",
	"codex_version.go":     "codex CLI の新版の検出",
	"action_modal.go":      "push / pull / claude update の確認モーダル (G-2 で置き場所を決める)",
}

// browseModelMethodFiles は browseModel のメソッドを持ってよい非テストのファイル (H-2)。
// 画面は browseModel にメソッドを生やさず、自分の型を持って internal/ に置く (main が仲介する)。
var browseModelMethodFiles = map[string]string{
	"tui.go":            "状態機械の本体",
	"gitlog_watch.go":   "git log の監視は browseModel の状態を直接動かす",
	"fullscreen.go":     "全画面の排他と開く口",
	"hint_surfaces.go":  "面ごとの最下行の案内",
	"open_workspace.go": "e のキーの処理",
}

func mainNonTestFiles(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, n)
	}
	// 走査が空振りしたら緑で通さない (cwd がずれた・置き場所が変わった)。tui.go は必ず在る
	if !slices.Contains(out, "tui.go") {
		t.Fatalf("main の直下の .go の走査が tui.go を見つけない (cwd=%q, 見つけた %d 個)", mustGetwd(t), len(out))
	}
	return out
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// checkAllowlist は実体の集合と一覧を突き合わせ、増えたもの・消し忘れを両方報告する。
func checkAllowlist(t *testing.T, what string, actual []string, allow map[string]string) {
	t.Helper()
	for _, f := range actual {
		if _, ok := allow[f]; !ok {
			t.Errorf("%s: %s が一覧に無い。画面の本体なら internal/ のパッケージに置く。main に置くなら structure_ratchet_test.go の一覧に理由を書く (issue 817)", what, f)
		}
	}
	for f, why := range allow {
		if why == "" {
			t.Errorf("%s: 一覧の %s に理由が無い", what, f)
		}
		if !slices.Contains(actual, f) {
			t.Errorf("%s: 一覧の %s が実体に無い (移した・消したなら一覧からも消す)", what, f)
		}
	}
}

func TestMainSourceFilesAllowlist(t *testing.T) {
	checkAllowlist(t, "main の非テストのファイル", mainNonTestFiles(t), mainSourceFiles)
}

func TestBrowseModelMethodFilesAllowlist(t *testing.T) {
	fset := token.NewFileSet()
	var withMethods []string
	files := map[string]*ast.File{}
	for _, f := range mainNonTestFiles(t) {
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files[f] = file
	}
	// 型の別名 (`type bm = browseModel`) で受けるメソッドも browseModel のメソッド (codex の敵対的レビュー 2026-10-17)。
	// 別名の別名もあるので、増えなくなるまで集める
	names := map[string]bool{"browseModel": true}
	for grew := true; grew; {
		grew = false
		for _, file := range files {
			for _, d := range file.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, sp := range gd.Specs {
					ts := sp.(*ast.TypeSpec)
					if ts.Assign.IsValid() && names[recvTypeName(ts.Type)] && !names[ts.Name.Name] {
						names[ts.Name.Name] = true
						grew = true
					}
				}
			}
		}
	}
	methods := 0
	for f, file := range files {
		has := false
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if names[recvTypeName(fn.Recv.List[0].Type)] {
				has = true
				methods++
			}
		}
		if has {
			withMethods = append(withMethods, f)
		}
	}
	// canary: tui.go には状態機械のメソッドが 100 本以上ある。数え方が壊れて 0 になったら緑で通さない
	if methods < 100 {
		t.Fatalf("browseModel のメソッドを %d 本しか数えていない (数え方が壊れている)", methods)
	}
	checkAllowlist(t, "browseModel のメソッドを持つファイル", withMethods, browseModelMethodFiles)
}

// TestInternalAPIGolden は internal/ の各パッケージの公開名を testdata/api/ の golden と突き合わせる (H-3)。
// 公開名は「main が画面に頼むこと」に絞る (issue 817)。増やすときは golden の差分がレビューに出る。
// 減ったときも落とす (golden が実体とずれると、次に増えたものが紛れる)。
// 更新: GLOGX_API_UPDATE=1 go test -run TestInternalAPIGolden .
//
// 数えるもの: パッケージの直下の非テストの .go のトップレベルの公開名 (func / type / const / var)・
// 公開のメソッド (受け手の型が非公開でも数える。interface を通して外から呼べる)・構造体の公開のフィールド。
// 数えないもの: 埋め込みで昇格する名前・テスト専用のパッケージの中のテストのファイル
func TestInternalAPIGolden(t *testing.T) {
	var pkgs []string
	err := filepath.WalkDir("internal", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		ents, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				pkgs = append(pkgs, p)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// canary: 土台の 2 つは必ず在る
	if !slices.Contains(pkgs, filepath.Join("internal", "term")) || !slices.Contains(pkgs, filepath.Join("internal", "app")) {
		t.Fatalf("internal/ の走査が term / app を見つけない: %v", pkgs)
	}
	update := os.Getenv("GLOGX_API_UPDATE") == "1"
	goldenDir := filepath.Join("testdata", "api")
	seen := map[string]bool{}
	for _, p := range pkgs {
		got := apiSurface(t, p)
		name := strings.ReplaceAll(strings.TrimPrefix(filepath.ToSlash(p), "internal/"), "/", "_") + ".txt"
		seen[name] = true
		path := filepath.Join(goldenDir, name)
		if update {
			if err := os.MkdirAll(goldenDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s の公開名の golden が無い (%v)。GLOGX_API_UPDATE=1 で作り、公開名が main から頼む口だけかを見てから commit する", p, err)
			continue
		}
		if string(want) != got {
			t.Errorf("%s の公開名が golden (%s) と違う:\n%s\n公開名を足すなら、それが main から画面に頼む口かを見てから GLOGX_API_UPDATE=1 で golden を直す (issue 817)",
				p, path, lineDiff(string(want), got))
		}
	}
	if update {
		return
	}
	ents, _ := os.ReadDir(goldenDir)
	for _, e := range ents {
		if !seen[e.Name()] {
			t.Errorf("golden %s に対応するパッケージが無い (消したなら golden も消す)", e.Name())
		}
	}
}

func apiSurface(t *testing.T, dir string) string {
	t.Helper()
	fset := token.NewFileSet()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	add := func(format string, a ...any) { lines = append(lines, fmt.Sprintf(format, a...)) }
	// addFields は構造体の型 (括弧で包んでいても) の公開のフィールドを owner.名前 で数える。埋め込みは数えない
	addFields := func(owner string, typ ast.Expr) {
		st, ok := ast.Unparen(typ).(*ast.StructType)
		if !ok {
			return
		}
		for _, fld := range st.Fields.List {
			for _, fn := range fld.Names {
				if fn.IsExported() {
					add("field %s.%s", owner, fn.Name)
				}
			}
		}
	}
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Recv == nil || len(d.Recv.List) == 0 {
					add("func %s", d.Name.Name)
					continue
				}
				add("method %s.%s", recvTypeName(d.Recv.List[0].Type), d.Name.Name)
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							add("type %s", s.Name.Name)
						}
						addFields(s.Name.Name, s.Type)
						// 括弧で包んだ型 (`type T (interface{...})`) も中を見る (codex の敵対的レビュー 2026-10-17)
						if it, ok := ast.Unparen(s.Type).(*ast.InterfaceType); ok {
							for _, m := range it.Methods.List {
								for _, mn := range m.Names {
									if mn.IsExported() {
										add("method %s.%s", s.Name.Name, mn.Name)
									}
								}
							}
						}
					case *ast.ValueSpec:
						kind := "var"
						if d.Tok == token.CONST {
							kind = "const"
						}
						for i, vn := range s.Names {
							if !vn.IsExported() {
								continue
							}
							add("%s %s", kind, vn.Name)
							// 公開の変数の型が無名の構造体なら、そのフィールドも外から触れる (codex の敵対的レビュー 2026-10-17)
							if s.Type != nil {
								addFields(vn.Name, s.Type)
							} else if i < len(s.Values) {
								if cl, ok := ast.Unparen(s.Values[i]).(*ast.CompositeLit); ok {
									addFields(vn.Name, cl.Type)
								}
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func lineDiff(want, got string) string {
	w := map[string]bool{}
	for l := range strings.SplitSeq(strings.TrimSpace(want), "\n") {
		w[l] = true
	}
	g := map[string]bool{}
	for l := range strings.SplitSeq(strings.TrimSpace(got), "\n") {
		g[l] = true
	}
	var out []string
	for l := range g {
		if !w[l] {
			out = append(out, "+ "+l)
		}
	}
	for l := range w {
		if !g[l] {
			out = append(out, "- "+l)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// TestNoFixtureCallsInProduction は画面のパッケージの Fixture() (main のテストが状態を置く口) を本番のコードが
// 呼んでいないことを確かめる (issue 817。internal/doctorview/fixture.go)。Fixture は状態の不変条件を飛ばして
// 書き込めるので、本番から呼ぶと画面の状態機械を迂回できる。
// 止めるもの: fixture.go の外の本番のコードでの、名前が Fixture で始まる識別子 (`.Fixture()`・`Fixture{...}`・
// `issuesview.FixtureScanMsg` のようなパッケージの関数)。
// 止めないもの: fixture.go の中に Fixture で始まらない名前の関数を足して、本番から包んで呼ぶ形 (レビューの責務)
func TestNoFixtureCallsInProduction(t *testing.T) {
	fset := token.NewFileSet()
	scanned := 0
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "testdata" || (strings.HasPrefix(n, ".") && p != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			return err
		}
		scanned++
		isDef := filepath.Base(p) == "fixture.go" // Fixture を定義するファイル (`return Fixture{v}` は定義の一部)
		if isDef {
			// 外してよいのは nofixture のビルドから消えるものだけ。build tag の無い fixture.go は本番に残るので、
			// そこから本番が Fixture を使っても字句の検査も nofixture のビルドも素通りする (opus の敵対的レビュー 2026-10-18)
			if !hasNofixtureConstraint(f) {
				t.Errorf("%s: fixture.go に //go:build !nofixture が無い (本番のビルドに残り、Fixture の検査を素通りする)", p)
			}
			return nil
		}
		// 名前が Fixture で始まる識別子 (メソッドの `.Fixture()`・`Fixture{v}` の直接構築・パッケージの関数
		// `issuesview.FixtureScanMsg`) を、Fixture を定義するファイルの外の本番のコードでは使わない
		// (直接構築の抜け道は codex の敵対的レビュー 2026-10-17)
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Fixture") {
				t.Errorf("%s: 本番のコードが %s を使っている (Fixture… は main のテスト専用の口)", fset.Position(id.Pos()), id.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// canary: main の直下と internal/ の非テストの .go は 60 個を超える。走査が空振りしたら緑で通さない
	if scanned < 50 {
		t.Fatalf("非テストの .go を %d 個しか読んでいない (走査が壊れている)", scanned)
	}
}

// TestEveryTestPackageUsesTestenv はテストを持つ main と internal の各パッケージが、TestMain で testenv.Main を呼ぶことを
// 確かめる。TestMain の前処理 (隔離・幅 env のガード・外部作用の既定) はパッケージ単位でしか掛からないので、テストを
// 新しいパッケージへ移すと黙って外れる (retro 815。issue 742 で doctor のテストを移したとき、幅 env のガードと
// キャッシュの隔離が外れて緑のまま通った)。前処理の中身は internal/testenv が正本 (issue 817)。
// 止めないもの: TestMain の中で testenv.Main 以外の前処理を足す形・testenv.Main を呼ぶ前に何かを走らせる形 (レビューの責務)
func TestEveryTestPackageUsesTestenv(t *testing.T) {
	dirs := map[string]bool{}
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// issues/ は glogx 本体に依存しない独立のドメインのパッケージ (キャッシュ・クリップボードに触らない) なので外す。
			// 外すのは直下の issues/ だけ (internal/issuesview は見る)
			if n := d.Name(); p != "." && (n == "testdata" || n == "tools" || n == "gorules" || n == "cmd" || p == "issues" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, "_test.go") {
			dirs[filepath.Dir(p)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// canary: main と doctorview・term はテストを持つ
	for _, want := range []string{".", filepath.Join("internal", "doctorview"), filepath.Join("internal", "term"), filepath.Join("internal", "issuesview"), filepath.Join("internal", "statusview")} {
		if !dirs[want] {
			t.Fatalf("テストを持つパッケージの走査が %s を見つけない: %v", want, dirs)
		}
	}
	fset := token.NewFileSet()
	for dir := range dirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution|parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "TestMain" || fn.Recv != nil || fn.Body == nil {
					continue
				}
				// 🚨 build 制約の付いたファイルの TestMain は数えない (`//go:build linux` だと macOS では走らない。
				// ParseFile は制約を見ないので、字面だけでは通ってしまう。issue 817 の反証レビュー)
				for _, cg := range f.Comments {
					if cg.Pos() < f.Package && strings.Contains(cg.Text(), "+build") || cg.Pos() < f.Package && strings.HasPrefix(strings.TrimSpace(cg.List[0].Text), "//go:build") {
						t.Errorf("%s: TestMain のファイルに build 制約がある (環境によって前処理が掛からない)", filepath.Join(dir, e.Name()))
					}
				}
				// 参照ではなく呼び出しを数える (`_ = testenv.Main` で通さない)
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Main" {
							if id, ok := sel.X.(*ast.Ident); ok && id.Name == "testenv" {
								found = true
							}
						}
					}
					return true
				})
			}
		}
		if !found {
			t.Errorf("%s: テストを持つのに TestMain で testenv.Main を呼んでいない (隔離・幅 env のガード・外部作用の既定が掛からない)", dir)
		}
	}
}

// hasNofixtureConstraint は f の先頭の //go:build 行が `!nofixture` を含むか。
func hasNofixtureConstraint(f *ast.File) bool {
	for _, cg := range f.Comments {
		if cg.Pos() >= f.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build ") && strings.Contains(c.Text, "!nofixture") {
				return true
			}
		}
	}
	return false
}
