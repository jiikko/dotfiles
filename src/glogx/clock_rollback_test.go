package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 上のテストは 2 箇所しか見ていないので、**新しい鮮度判定が足されたときに追随を強制する**
// 走査を併せて持つ (列挙表は持たない。issue 117 の「列挙すると兄弟を足したときに追随を忘れる =
// この検査が守りたい事故を検査自身が踏む」を適用)。
//
// 対象は `now.Sub(x)` / `app.TimeNow().Sub(x)` を **TTL 系の定数と比較している関数**だけ。
// アニメーションの経過 (statusOpenDuration 等) は比較先が Duration 系なので対象外になる。
func TestFreshnessChecksGuardAgainstClockRollback(t *testing.T) {
	// 🚨 main (".") と internal/ の下を再帰で読む。parser.ParseDir(".") は再帰しないので、internal/ へ移した
	// 鮮度判定 (screen_memory.go など) を黙って見なくなる (issue 742)。issues/ など別の package は前から対象外
	fset := token.NewFileSet()
	var files []*ast.File
	var paths []string
	for _, root := range []string{".", "internal"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if root == "." && path != "." {
					return filepath.SkipDir // "." は直下だけ (internal は 2 本目の根で読む)
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			files = append(files, f)
			paths = append(paths, path)
			return nil
		})
		if err != nil {
			t.Fatalf("%s を走査できない: %v", root, err)
		}
	}
	if len(files) == 0 {
		t.Fatal("走査対象のファイルが 0 件 (走査が壊れている)")
	}

	// TTL 系の名前 (この語を含む定数と比較していたら「鮮度判定」とみなす)
	ttlish := func(s string) bool {
		return strings.Contains(s, "TTL") || strings.Contains(s, "StaleAfter") ||
			strings.Contains(s, "Cooldown")
	}
	// 🚨 **コメントを除いてから判定する**。生ソースのままだと「// age < 0 (未来) も取り直す」の
	// ような**説明コメントがガードとして数えられ**、コードから条件を消しても green になる
	// (実測 2026-09-03: 最初の実装がこれで、変異 3 / 4 が素通りした)。
	stripComments := func(src string) string {
		var b strings.Builder
		for _, line := range strings.Split(src, "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		return b.String()
	}
	// 巻き戻しガードとして認めるトークン
	//
	// 🚨 `IsZero()` を入れないこと (issue 282)。あれは「まだ一度も取得していない」の判定であって
	// 「取得時刻が未来」の判定ではない。しかも `if x.IsZero() { return false }` は新しい鮮度判定を
	// 書くときの**最も自然な書き方**なので、認めると巻き戻しガードを 1 つも持たない新規コードが
	// 無審査で通る (実測: ガード無しの鮮度判定を足すと走査は 11 → 12 に増えたのにテストは緑だった)。
	// 意図的に持たない関数は `// clock: elapsed-only` + 理由で明示する (doctor_cache.go:carryFresh)。
	guarded := func(body string) bool {
		body = stripComments(body)
		for _, tok := range []string{"age < 0", "age >= 0", ".After(now)", ".After(app.TimeNow())", ".After(app.TimeNow())", ".After(TimeNow())"} {
			if strings.Contains(body, tok) {
				return true
			}
		}
		return false
	}

	checked := 0
	for i, file := range files {
		path := paths[i]
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			var b strings.Builder
			ast.Inspect(fn.Body, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok {
					b.WriteString(id.Name)
					b.WriteString(" ")
				}
				return true
			})
			names := b.String()
			// 鮮度判定か: Sub( があり、TTL 系の定数名が同じ関数に出る
			if !strings.Contains(names, "Sub ") || !ttlish(names) {
				return true
			}
			start := fset.Position(fn.Pos()).Offset
			end := fset.Position(fn.End()).Offset
			src, err := readRange(path, start, end)
			if err != nil {
				t.Fatalf("%s を読めない: %v", path, err)
			}
			if strings.Contains(src, "// clock: elapsed-only") {
				return true
			}
			checked++
			if !guarded(src) {
				t.Errorf("%s:%s に巻き戻しガードが無い (age < 0 / age >= 0 / .After(now) のいずれか、"+
					"または理由つきの `// clock: elapsed-only` が要る)",
					filepath.Base(path), fn.Name.Name)
			}
			return true
		})
	}
	// 🚨 下限は 0 でなく実件数の近くに置く (issue 280 と同じ形)。走査条件が壊れて対象が
	// 数件まで縮んでも `checked == 0` では落ちず、「違反 0 件 = 緑」になる。
	// 増える分には落とさない (鮮度判定が増えるのは正常)。
	const minChecked = 8 // 2026-09-06 実測 10 件。除外マーカー付き (carryFresh) は数えない
	if checked < minChecked {
		t.Fatalf("鮮度判定が %d 件しか見つからない (下限 %d)。走査条件が壊れている疑い",
			checked, minChecked)
	}
	t.Logf("鮮度判定 %d 件を検査した", checked)
}

// readRange はファイルの [start,end) を読む (AST の位置から元ソースを取り出す)。
func readRange(path string, start, end int) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if end > len(b) {
		end = len(b)
	}
	return string(b[start:end]), nil
}
