package disk

// 🚨 削除エンジンのテストハーネス。**実ファイルを絶対に消さない**ことを構造で保証する。
//
// なぜ要るか: このパッケージのテストは os.RemoveAll と renameatx_np を本物として呼ぶ。
// fixture の作り方を 1 行間違えれば (Env.Home を実ホームにする / Result に実パスを入れる)、
// テストが実データを消す。「気をつける」では守れないので、破壊的操作の出口 (allowDestructive) に
// 検査を差し、**明示的に登録したディレクトリの外は実行前に拒否**する。
//
// 3 段構え:
//  1. 既定は**全部拒否**。fixture が sandboxAllow で自分の一時ディレクトリを登録して初めて通る
//     (登録漏れは「消えない」側に倒れる = fail-closed)
//  2. XDG_CACHE_HOME を一時ディレクトリへ向ける。HistoryDir を渡し忘れたテストが
//     実キャッシュ (~/.cache/glog) に書くのを防ぐ
//  3. 拒否した記録が 1 件でも残ったまま終わったら、**テストが全部緑でもスイートを落とす**
//     (拒否は「ハーネスが助けた」ではなく「テストが壊れている」の証拠。自己テストだけが drain する)
//
// 破壊的操作を新しく足したときにハーネスを通し忘れないよう、TestDestructiveCallsGoThroughHook が
// ソースを走査して「破壊的な呼び出しを持つ関数は allowDestructive も呼ぶ」を強制する。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	sandboxMu         sync.Mutex
	sandboxRoots      []string
	sandboxViolations []string
)

// sandboxAllow はこのテストの間だけ root 配下への破壊的操作を許す。
func sandboxAllow(t *testing.T, root string) {
	t.Helper()
	r := normalizeSystemLinks(filepath.Clean(root))
	sandboxMu.Lock()
	sandboxRoots = append(sandboxRoots, r)
	sandboxMu.Unlock()
	t.Cleanup(func() {
		sandboxMu.Lock()
		defer sandboxMu.Unlock()
		for i, x := range sandboxRoots {
			if x == r {
				sandboxRoots = append(sandboxRoots[:i], sandboxRoots[i+1:]...)
				return
			}
		}
	})
}

func sandboxCheck(op, path string) error {
	p := normalizeSystemLinks(filepath.Clean(path))
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	for _, r := range sandboxRoots {
		if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
			return nil
		}
	}
	sandboxViolations = append(sandboxViolations, op+" "+path)
	return fmt.Errorf("テストハーネス: サンドボックス外への %s を拒否しました: %s", op, path)
}

// takeSandboxViolations は記録を取り出して空にする (自己テストだけが呼ぶ)。
func takeSandboxViolations() []string {
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	v := sandboxViolations
	sandboxViolations = nil
	return v
}

func TestMain(m *testing.M) {
	cache, err := os.MkdirTemp("", "disk-delete-cache-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ハーネスの一時ディレクトリを作れない:", err)
		os.Exit(1)
	}
	// HistoryDir 未指定のテストが実キャッシュに書かないよう、置き場ごと一時領域へ向ける
	_ = os.Setenv("XDG_CACHE_HOME", cache)
	destructiveHook = sandboxCheck

	code := m.Run()

	if v := takeSandboxViolations(); len(v) > 0 {
		fmt.Fprintf(os.Stderr, "\n🚨 テストがサンドボックス外への破壊的操作を試みた (ハーネスが止めた):\n  %s\n"+
			"  fixture の作り方が壊れている。テストが緑でもこのスイートは失敗扱いにする\n",
			strings.Join(v, "\n  "))
		code = 1
	}
	_ = os.RemoveAll(cache)
	os.Exit(code)
}

// ハーネス自身の異常系: 登録していない場所は、実行前に拒否され実体が残る。
// (この検査が無いと「ハーネスがあること」と「ハーネスが効いていること」を区別できない)
func TestSandboxHarnessBlocksUnregisteredPaths(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "not-registered") // sandboxAllow を**呼ばない**
	victim := filepath.Join(outside, "precious")
	mkfile(t, victim, 4)
	dev, ino := identityOf(t, outside)

	o := &ItemOutcome{Path: outside, dev: dev, ino: ino}
	if err := removeItem(o); err == nil {
		t.Fatal("ハーネスがサンドボックス外の削除を止めなかった")
	}
	if !exists(victim) {
		t.Fatal("サンドボックス外の実体が消えた")
	}
	if o.Outcome != OutcomeFailed || !strings.Contains(o.Reason, "テストハーネス") {
		t.Errorf("結末 = %+v", *o)
	}

	trash := filepath.Join(t.TempDir(), "Trash")
	sandboxAllow(t, trash) // 移動先は許しても、移動元が許されていないので通らない
	if _, err := trashMove(outside, trash, dev, ino); err == nil {
		t.Fatal("ハーネスがサンドボックス外のゴミ箱移動を止めなかった")
	}
	if !exists(victim) {
		t.Fatal("サンドボックス外の実体が移動された")
	}

	if v := takeSandboxViolations(); len(v) != 2 {
		t.Fatalf("拒否の記録が %d 件 (2 件のはず): %v", len(v), v)
	}
}

// 登録した場所は通る (ハーネスが全部拒否して他のテストを空振りさせていないことの確認)。
func TestSandboxHarnessAllowsRegisteredPaths(t *testing.T) {
	root := t.TempDir()
	sandboxAllow(t, root)
	target := filepath.Join(root, "tree")
	mkfile(t, filepath.Join(target, "x"), 1)
	dev, ino := identityOf(t, target)
	o := &ItemOutcome{Path: target, dev: dev, ino: ino}
	if err := removeItem(o); err != nil {
		t.Fatalf("登録済みの場所を消せない: %v", err)
	}
	if exists(target) {
		t.Fatal("消えていない")
	}
	if v := takeSandboxViolations(); len(v) != 0 {
		t.Fatalf("拒否の記録が出た: %v", v)
	}
}

func identityOf(t *testing.T, p string) (uint64, uint64) {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	dev, ino, ok := statIdentity(fi)
	if !ok {
		t.Fatal("実体を識別できない")
	}
	return dev, ino
}

// 破壊的な呼び出しを持つ関数は allowDestructive も呼ぶ。ハーネスを迂回する経路が
// 後から生えるのを止める (列挙表を持たず、ソースを走査して確かめる)。
func TestDestructiveCallsGoThroughHook(t *testing.T) {
	// 消す / 動かす操作だけを見る。記録の書き込み (CreateTemp / Rename) は対象外で、
	// そちらは XDG_CACHE_HOME の差し替えで実領域から引き離してある
	destructive := map[string]bool{
		"os.RemoveAll": true, "os.Remove": true,
		"unix.RenameatxNp": true, "unix.Unlinkat": true, "unix.Renameat": true,
		"root.RemoveAll": true, "root.Remove": true,
		"renameExcl": true,
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				var found []string
				hooked := false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					switch fun := call.Fun.(type) {
					case *ast.Ident:
						if fun.Name == "allowDestructive" {
							hooked = true
						}
						if destructive[fun.Name] {
							found = append(found, fun.Name)
						}
					case *ast.SelectorExpr:
						if x, ok := fun.X.(*ast.Ident); ok && destructive[x.Name+"."+fun.Sel.Name] {
							found = append(found, x.Name+"."+fun.Sel.Name)
						}
					}
					return true
				})
				if len(found) == 0 {
					continue
				}
				checked++
				// 行内マーカーで例外にできる (後始末など、対象がツール自身の産物のとき)
				if hooked || hasAllowMarker(t, name, fn, fset) {
					continue
				}
				t.Errorf("%s: %s を呼ぶのに allowDestructive を通していない (テストハーネスを迂回する)",
					fset.Position(fn.Pos()), strings.Join(found, ", "))
			}
		}
	}
	if checked == 0 {
		t.Fatal("破壊的な呼び出しが 1 つも見つからない (走査が壊れている)")
	}
}

func hasAllowMarker(t *testing.T, file string, fn *ast.FuncDecl, fset *token.FileSet) bool {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	start := fn.Pos()
	if fn.Doc != nil {
		start = fn.Doc.Pos() // マーカーは doc コメントに書けるようにする (理由を添えられるため)
	}
	from, to := fset.Position(start).Line, fset.Position(fn.End()).Line
	for i := from - 1; i < to && i < len(lines); i++ {
		if strings.Contains(lines[i], "destructive-op: allow") {
			return true
		}
	}
	return false
}
