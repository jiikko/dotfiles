package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 包み忘れを機械で止める。
//
// **脅威モデル**: 「新しい I/O 経路を足すときに `--io-timeout` で包むのを忘れる」を止める。
// issue 357 で実際に起きた形で、包みが 1 つ欠けると `--io-timeout` の目的
// (スクリプトが無言で固まらない) が機構ごと無意味になる。しかも壊れ方は無言 (固まるだけ)。
//
// **検出しない形** (塞ぎに行かない。意図的な迂回は review の責務):
//   - `withTimeout` を直接呼んで期限を別の値にする
//   - Locker のメソッドを関数値に代入してから呼ぶ (`f := l.Renew; f(tok)`)
//   - reflect 経由の呼び出し
//   - **`wrappedMethods` に無い新しいメソッド**を足して生の I/O を書く (allowlist 設計に
//     内在する。敵対レビュー 2026-09-15 が `Steal` を足して素通りさせた)
//   - **Locker のメソッド以外の I/O** (`os.Stat` / `os.ReadFile` を直接書く)。
//     `NewLocker` の `os.Stat` と token-file の読み書きは 2026-09-15 に手で包んだが、
//     ここは数えていない — 数えると production の全 `os.*` が対象になり、
//     「包まなくてよい I/O」(ローカルの一時ファイル等) と区別できない
//
// 🚨 **①と②の両方を数える。** 「`l.timeout` の読み出し箇所が増えたか」だけを数える検査は、
// deferred Cleanup のような**呼び出し側の包み忘れ**を素通りさせる (issue 357 の 🚨)。
// ここが数えるのは「生の I/O メソッドを呼んでいる箇所」なので、どちらの形も当たる。

// wrappedMethods は timeout.go が包みを用意している Locker のメソッド。
// ここに足したら timeout.go にも `〜Timed` を足すこと (この対応が崩れると下で落ちる)。
var wrappedMethods = []string{"Acquire", "Renew", "Release", "Inspect", "Break", "Cleanup"}

func TestRawLockerIOIsOnlyCalledFromTimeoutGo(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	wrapped := map[string]bool{}
	for _, m := range wrappedMethods {
		wrapped[m] = true
	}

	var offenders []string
	scanned, calls := 0, 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !wrapped[sel.Sel.Name] {
				return true
			}
			// 🚨 レシーバが Locker かは go/types 無しでは確定できない。**変数名で近似しない** —
			// 最初は `l` / `locker` に限っていたが、敵対レビューが `lk.Renew("tok")` で
			// 素通りさせた (「うっかり書く典型形」そのものなので、宣言した脅威モデルに
			// 正面から当たる)。名前を問わず拾う (この package で これらのメソッドを持つ型は
			// Locker だけ)。
			calls++
			if name != "timeout.go" {
				offenders = append(offenders,
					fset.Position(call.Pos()).String()+": "+recvText(sel.X)+"."+sel.Sel.Name)
			}
			return true
		})
	}

	// 🚨 走査が空だと「違反 0 件」= 緑になる。先に母集合を固定する
	if scanned < 5 {
		t.Fatalf("走査した production ファイルが %d 件しかない (走査が壊れている)", scanned)
	}
	if calls < len(wrappedMethods) {
		t.Fatalf("生の I/O 呼び出しを %d 件しか見つけていない (期待: timeout.go の %d 件以上)",
			calls, len(wrappedMethods))
	}
	if len(offenders) > 0 {
		t.Errorf("--io-timeout で包まれていない I/O 呼び出しがある (timeout.go の 〜Timed を使うこと):\n  %s",
			strings.Join(offenders, "\n  "))
	}

	// 🚨 Locker のメソッド以外でも、**共有の上を触ることが分かっている** 3 経路だけは
	// 名指しで pin する。挙動のテストは書けない (Stat がブロックするマウントを手元で
	// 作れない) ので、配線を静的に固定する。ここを緩めると、敵対レビューが実測した
	// 「`check --io-timeout 200ms` が 3.001s 固まる」が黙って戻る。
	for file, want := range map[string][]string{
		"lock.go": {"statDirTimed(abs, timeout)"},
		"main.go": {"readFileTimed(o.tokenFile", "writeFileTimed(o.tokenFile"},
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s に %q が無い (共有を触る I/O が --io-timeout の外に出ている)", file, w)
			}
		}
	}

	// 対応する 〜Timed が実在することも固定する (片方だけ足して満足しないため)
	src, err := os.ReadFile(filepath.Join(".", "timeout.go"))
	if err != nil {
		t.Fatalf("timeout.go: %v", err)
	}
	for _, m := range wrappedMethods {
		if !strings.Contains(string(src), ") "+m+"Timed(") {
			t.Errorf("timeout.go に %sTimed が無い", m)
		}
	}
}

// recvText は診断用にレシーバの式を文字列化する (Ident 以外も出せるようにする)。
func recvText(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return recvText(t.X) + "." + t.Sel.Name
	}
	return "?"
}
