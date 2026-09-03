package disk

// 🚨 削除エンジンのテストハーネス。**実ファイルを絶対に消さない**ことを構造で保証する。
//
// なぜ要るか: このパッケージのテストは os.RemoveAll と renameatx_np を本物として呼ぶ。
// fixture の作り方を 1 行間違えれば (Env.Home を実ホームにする / Result に実パスを入れる /
// Run を差し忘れて実カタログの `brew cleanup` を本当に走らせる)、テストが実データを壊す。
// 「気をつける」では守れないので、破壊的操作の出口 (allowDestructive) に検査を差し、
// **明示的に登録した一時ディレクトリの外は実行前に拒否**する。
//
// 4 段構え (どれも「効いていること」を自己テストで固定してある):
//  1. 既定は**全部拒否**。テストが sandboxAllow で自分の一時ディレクトリを登録して初めて通る
//     (登録漏れは「消えない」側に倒れる = fail-closed)。登録できるのは os.TempDir() 配下だけで、
//     実ホームを登録しようとしたら Fatal にする (それを許すと 1 行でハーネスが無力化する)
//  2. 判定は**親ディレクトリを実解決してから**行う。破壊的操作はカーネルにパスを解決させるので、
//     文字列の prefix 比較だけだと sandbox 内から外を指す symlink で抜けられる
//  3. XDG_CACHE_HOME を一時ディレクトリへ向ける。HistoryDir を渡し忘れたテストが
//     実キャッシュ (~/.cache/glog) に書くのを防ぐ
//  4. 拒否した記録が 1 件でも残ったまま終わったら、**テストが全部緑でもスイートを落とす**
//     (拒否は「ハーネスが助けた」ではなく「テストが壊れている」の証拠)
//
// 破壊的操作を新しく足したときにハーネスを通し忘れないよう、TestDestructiveCallsGoThroughHook が
// **テストファイルも含めて**ソースを走査し、「破壊的な呼び出しを持つ関数は allowDestructive
// (テストなら sandboxCheck) も呼ぶ」を強制する。
//
// 🚨 このゲートの脅威モデルは「**うっかり書く典型形**を止める」こと。関数値への代入
// (`f := os.RemoveAll; f(p)`)、allowDestructive に別のパスを渡す、といった**意図的な迂回**は
// 検出しない (字句のゲートで全部塞ごうとすると迂回が無限に出る)。そこはレビューの責務。

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
	sandboxCommands   []string
	sandboxViolations []string
	sandboxTmpRoot    string // os.TempDir() の実解決値 (登録できる範囲)
)

// sandboxAllow はこのテストの間だけ root 配下への破壊的操作を許す。
// **os.TempDir() の外は登録できない** (実ホームを登録するとハーネスが丸ごと無力化するため)。
func sandboxAllow(t *testing.T, root string) {
	t.Helper()
	if err := sandboxAllowable(root); err != nil {
		t.Fatal(err)
	}
	r := resolveForSandbox(root)
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

// sandboxTempDir は一時ディレクトリを作って同時に登録する (登録忘れの入口を減らす)。
func sandboxTempDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sandboxAllow(t, dir)
	return dir
}

// sandboxAllowCommand は cli: 経路で実行してよいコマンド名を登録する。
// 登録しないと execCLI は拒否される (Run を差し忘れた fixture が実カタログの
// `go clean -modcache` / `brew cleanup` を本当に実行するのを防ぐ)。
func sandboxAllowCommand(t *testing.T, name string) {
	t.Helper()
	sandboxMu.Lock()
	sandboxCommands = append(sandboxCommands, name)
	sandboxMu.Unlock()
	t.Cleanup(func() {
		sandboxMu.Lock()
		defer sandboxMu.Unlock()
		for i, x := range sandboxCommands {
			if x == name {
				sandboxCommands = append(sandboxCommands[:i], sandboxCommands[i+1:]...)
				return
			}
		}
	})
}

// resolveForSandbox は「カーネルが解決する先」に寄せた形へ直す。
// 🚨 親を実解決する (EvalSymlinks) のが要点。os.OpenRoot / openat はパスをカーネルに
// 解決させるので、文字列の prefix 比較だけだと sandbox 内から外を指す symlink で抜けられる。
// 対象自身は解決しない (symlink そのものを消す / 移すのが正しい振る舞いのため)。
func resolveForSandbox(p string) string {
	p = filepath.Clean(p)
	dir, base := filepath.Dir(p), filepath.Base(p)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return normalizeSystemLinks(filepath.Join(normalizeSystemLinks(dir), base))
}

func sandboxCheck(op, path string) error {
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	if op == "cli" {
		name := strings.Fields(path)
		for _, c := range sandboxCommands {
			if len(name) > 0 && name[0] == c {
				return nil
			}
		}
		sandboxViolations = append(sandboxViolations, op+" "+path)
		return fmt.Errorf("テストハーネス: 登録していないコマンドの実行を拒否しました: %s", path)
	}
	p := resolveForSandbox(path)
	for _, r := range sandboxRoots {
		if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
			return nil
		}
	}
	sandboxViolations = append(sandboxViolations, op+" "+path)
	return fmt.Errorf("テストハーネス: サンドボックス外への %s を拒否しました: %s", op, path)
}

// takeSandboxViolations は記録のうち want に一致するものだけを取り出して消す
// (全部 drain すると、別のテストの登録漏れまで飲み込んで TestMain の報告が消える)。
func takeSandboxViolations(want string) []string {
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	got := make([]string, 0, len(sandboxViolations))
	rest := make([]string, 0, len(sandboxViolations))
	for _, v := range sandboxViolations {
		if want != "" && strings.Contains(v, want) {
			got = append(got, v)
			continue
		}
		rest = append(rest, v)
	}
	sandboxViolations = rest
	return got
}

// checkViolations は「拒否の記録が残っていないか」。残っていれば error (スイートを落とす)。
func checkViolations() error {
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	if len(sandboxViolations) == 0 {
		return nil
	}
	return fmt.Errorf("テストがサンドボックス外への破壊的操作を試みた (ハーネスが止めた):\n  %s\n"+
		"  fixture の作り方が壊れている。テストが緑でもこのスイートは失敗扱いにする",
		strings.Join(sandboxViolations, "\n  "))
}

func TestMain(m *testing.M) {
	sandboxTmpRoot = resolveForSandbox(os.TempDir())
	cache, err := os.MkdirTemp("", "disk-delete-cache-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ハーネスの一時ディレクトリを作れない:", err)
		os.Exit(1)
	}
	// HistoryDir 未指定のテストが実キャッシュに書かないよう、置き場ごと一時領域へ向ける
	_ = os.Setenv("XDG_CACHE_HOME", cache)
	destructiveHook = sandboxCheck

	code := m.Run()

	if err := checkViolations(); err != nil {
		fmt.Fprintf(os.Stderr, "\n🚨 %v\n", err)
		code = 1
	}
	_ = os.RemoveAll(cache) // destructive-op: allow ハーネス自身が作った一時ディレクトリ
	os.Exit(code)
}

// ---- ハーネス自身の検査 (「あること」と「効いていること」は別) ----

// 登録していない場所は、実行前に拒否され実体が残る。
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

	trash := sandboxTempDir(t, "Trash") // 移動先は許しても、移動元が許されていないので通らない
	if _, err := trashMove(outside, trash, dev, ino); err == nil {
		t.Fatal("ハーネスがサンドボックス外のゴミ箱移動を止めなかった")
	}
	if !exists(victim) {
		t.Fatal("サンドボックス外の実体が移動された")
	}

	if v := takeSandboxViolations("not-registered"); len(v) != 2 {
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
	if err := removeItem(&ItemOutcome{Path: target, dev: dev, ino: ino}); err != nil {
		t.Fatalf("登録済みの場所を消せない: %v", err)
	}
	if exists(target) {
		t.Fatal("消えていない")
	}
	// ゴミ箱側も許可の向きを確かめる (removeItem だけだと trash の allow 経路が未検証になる)
	src := filepath.Join(root, "movable")
	mkfile(t, src, 1)
	dev, ino = identityOf(t, src)
	if _, err := trashMove(src, filepath.Join(root, "Trash"), dev, ino); err != nil {
		t.Fatalf("登録済みの場所へ移動できない: %v", err)
	}
	if v := takeSandboxViolations(""); len(v) != 0 {
		t.Fatalf("拒否の記録が出た: %v", v)
	}
}

// 🚨 sandbox 内から外を指す symlink を経由しても抜けられない。
// 文字列の prefix 比較だけだと、カーネルの解決先と食い違って素通りする。
func TestSandboxHarnessResolvesSymlinkedParents(t *testing.T) {
	box := t.TempDir()
	sandboxAllow(t, box)
	outside := t.TempDir() // 登録しない
	victim := filepath.Join(outside, "real", "PRECIOUS")
	mkfile(t, victim, 2)
	if err := os.Symlink(outside, filepath.Join(box, "link")); err != nil {
		t.Fatal(err)
	}
	via := filepath.Join(box, "link", "real", "PRECIOUS") // 文字列上は box の中
	dev, ino := identityOf(t, via)
	if err := removeItem(&ItemOutcome{Path: via, dev: dev, ino: ino}); err == nil {
		t.Fatal("symlink 経由でサンドボックス外を消せた")
	}
	if !exists(victim) {
		t.Fatal("サンドボックス外の実体が消えた")
	}
	if v := takeSandboxViolations("PRECIOUS"); len(v) != 1 {
		t.Fatalf("拒否の記録 = %v", v)
	}
}

// 🚨 実ホームなど os.TempDir() の外は登録できない (登録できるとハーネスが 1 行で無力化する)。
func TestSandboxAllowRejectsPathsOutsideTempDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("HOME が取れない")
	}
	for _, p := range []string{home, "/", filepath.Join(home, "Documents")} {
		if err := sandboxAllowable(p); err == nil {
			t.Errorf("%s の登録を許した", p)
		}
	}
	// 逆向き (登録してよい側) も見る: 判定が deny-all になっても気づけるように
	if err := sandboxAllowable(t.TempDir()); err != nil {
		t.Errorf("一時領域の登録を拒んだ: %v", err)
	}
}

// sandboxAllowable は「この root を登録してよいか」の**唯一の判定**。
// 🚨 sandboxAllow と自己テストが**同じ関数を通る**ことが要点。以前は自己テストが判定式を
// 別に書き写しており、本走査 (sandboxAllow) の検査を外す変異を当てても緑のままだった
// (issue 234 (a)。~/.claude/rules/verify-execution-not-just-exit-code.md の
// 「canary と本走査は同じ関数を通す」)。
func sandboxAllowable(root string) error {
	r := resolveForSandbox(root)
	if r != sandboxTmpRoot && !strings.HasPrefix(r, sandboxTmpRoot+string(filepath.Separator)) {
		return fmt.Errorf("サンドボックスに登録できるのは %s 配下だけです (実データを守るため): %s", sandboxTmpRoot, root)
	}
	return nil
}

// XDG_CACHE_HOME が一時領域へ向いている (HistoryDir 未指定でも実キャッシュに書かない)。
func TestHarnessRedirectsCacheHome(t *testing.T) {
	base, err := DefaultHistoryDir()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(base, filepath.Join(home, ".cache")) {
		t.Fatalf("既定の記録の置き場が実キャッシュを指している: %s", base)
	}
	if !strings.HasPrefix(resolveForSandbox(base), sandboxTmpRoot) {
		t.Fatalf("既定の記録の置き場が一時領域の外: %s", base)
	}
}

// 拒否の記録が残っていたら error になる (TestMain の最終判定そのもの)。
func TestCheckViolationsReportsLeftovers(t *testing.T) {
	if err := checkViolations(); err != nil {
		t.Fatalf("前提が崩れている: 開始時に記録が残っている: %v", err)
	}
	sandboxMu.Lock()
	sandboxViolations = append(sandboxViolations, "remove /tmp/harness-self-test")
	sandboxMu.Unlock()
	if err := checkViolations(); err == nil {
		t.Fatal("記録が残っているのに error にならない")
	}
	if v := takeSandboxViolations("harness-self-test"); len(v) != 1 {
		t.Fatalf("自分の記録を取り除けない: %v", v)
	}
	if err := checkViolations(); err != nil {
		t.Fatalf("取り除いた後も error: %v", err)
	}
}

// 登録はテストが終わったら外れる (残ると以降の全テストで sandbox が広がったままになる)。
func TestSandboxRootIsReleasedAfterTest(t *testing.T) {
	var leaked string
	t.Run("register", func(t *testing.T) {
		leaked = t.TempDir()
		sandboxAllow(t, leaked)
		if err := sandboxCheck("remove", filepath.Join(leaked, "x")); err != nil {
			t.Fatalf("登録が効いていない: %v", err)
		}
	})
	if err := sandboxCheck("remove", filepath.Join(leaked, "x")); err == nil {
		t.Fatal("サブテストが終わっても登録が残っている")
	}
	takeSandboxViolations(leaked)
}

// cli: は登録したコマンドだけ実行できる。
func TestSandboxHarnessBlocksUnregisteredCommands(t *testing.T) {
	if err := sandboxCheck("cli", "rm -rf /"); err == nil {
		t.Fatal("登録していないコマンドを許した")
	}
	takeSandboxViolations("rm -rf")
	sandboxAllowCommand(t, "faketool")
	if err := sandboxCheck("cli", "faketool purge"); err != nil {
		t.Fatalf("登録したコマンドを拒否した: %v", err)
	}
}

// 記録の書き込み / 削除も検査点を通る (置き場は呼び出し側が決められるので実ファイルを上書きしうる)。
func TestHistoryWritesGoThroughHook(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim.json") // 登録しない
	if err := os.WriteFile(victim, []byte("PRECIOUS"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &history{path: victim}
	if err := h.write(DeleteReport{}, phasePlanned); err == nil {
		t.Fatal("サンドボックス外への記録の書き込みを許した")
	}
	h.discard()
	b, err := os.ReadFile(victim)
	if err != nil || string(b) != "PRECIOUS" {
		t.Fatalf("実ファイルが壊れた (err=%v content=%q)", err, string(b))
	}
	if v := takeSandboxViolations("victim.json"); len(v) != 2 {
		t.Fatalf("拒否の記録 = %v (write と remove の 2 件)", v)
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

// rmFixture はテストの下ごしらえで木を消すときの入口。**テストも検査点を通す**
// (fixture のパスが 1 行間違っていれば、production の経路に入る前にテスト本体が実データを消す)。
func rmFixture(t *testing.T, p string) {
	t.Helper()
	if err := sandboxCheck("remove", p); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
}

// 破壊的な呼び出しを持つ関数は検査点 (allowDestructive / sandboxCheck) も呼ぶ。
// **テストファイルも対象**にする (fixture の下ごしらえこそが最も間違えやすい)。
func TestDestructiveCallsGoThroughHook(t *testing.T) {
	// 消す / 動かす操作の**メソッド名**で見る。受け手の変数名や import の別名に依存しない
	// (`root.RemoveAll` を `r.RemoveAll` に書き換えただけで沈黙する形を避ける)
	// 🚨 見るのは「**消す・動かす**」だけ。Truncate / Create / WriteFile は入れない
	// (fixture の下ごしらえで正当に使われ、この package では実データを壊す経路にならない)
	destructive := map[string]bool{
		"RemoveAll": true, "Remove": true, "Rename": true,
		"RenameatxNp": true, "Renameat": true, "Unlinkat": true, "renameExcl": true,
		// 🚨 unix.Unlink / unix.Rmdir が抜けていた (issue 234 (c))。`unix.Unlink(p)` と
		// 書くだけでゲートが無音で素通りする形だった
		"Unlink": true, "Rmdir": true,
	}
	hooks := map[string]bool{"allowDestructive": true, "sandboxCheck": true, "rmFixture": true}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	sawFunc := map[string]bool{}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				var found []*ast.CallExpr
				var names []string
				hooked := false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					var id string
					switch fun := call.Fun.(type) {
					case *ast.Ident:
						id = fun.Name
					case *ast.SelectorExpr:
						id = fun.Sel.Name
					}
					if hooks[id] {
						hooked = true
					}
					if destructive[id] {
						found = append(found, call)
						names = append(names, id)
					}
					return true
				})
				if len(found) == 0 {
					continue
				}
				sawFunc[fn.Name.Name] = true
				if hooked {
					continue
				}
				for i, call := range found {
					// 例外は**その行**にだけ効く (関数まるごとの恒久免除にしない)
					if hasAllowMarker(t, name, fset.Position(call.Pos()).Line) {
						continue
					}
					t.Errorf("%s: %s を呼ぶのに検査点を通していない (テストハーネスを迂回する)",
						fset.Position(call.Pos()), names[i])
				}
			}
		}
	}
	// 0 件だけでなく、**要になる関数が走査に入っていること**を見る
	// (表からキーが 1 つ落ちても他で checked > 0 になり、穴が緑に埋もれるため)
	for _, want := range []string{"removeItem", "trashMove", "discard", "TestMain"} {
		if !sawFunc[want] {
			t.Errorf("%s が走査に入っていない (検査の対象が壊れている)", want)
		}
	}
}

// hasAllowMarker はその行 (または直前の行) に例外マーカーがあるか。
func hasAllowMarker(t *testing.T, file string, line int) bool {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for _, i := range []int{line - 1, line - 2} {
		if i >= 0 && i < len(lines) && strings.Contains(lines[i], "destructive-op: allow") {
			return true
		}
	}
	return false
}
