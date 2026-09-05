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
	"time"

	"doctor/disk"
)

// 永続キャッシュの鮮度判定は「未来の時刻」を fresh にしない。
//
// 🚨 これらの時刻は **disk 上の JSON から読む**ので、時計を戻した / NTP が大きく補正した /
// 別マシンのキャッシュを持ってきた、のいずれかで未来になりうる。`now.Sub(x) < TTL` は
// 未来だと負になって**永久に真**を返し、古い値を使い続ける。しかも silent で、
// 「なぜか更新されない」としか見えない (issue 174 の doctor トーストが実際にこれで沈黙した)。
//
// issue 201 の時点で、同じ形が **8 箇所中 4 箇所だけガード済み**という非対称だった。
func TestPersistedCacheRejectsFutureTimestamps(t *testing.T) {
	now := time.Now()

	t.Run("cacheEntry.fresh", func(t *testing.T) {
		e := cacheEntry{FetchedAt: now.Add(time.Hour), State: StateSuccess}
		if e.fresh(now) {
			t.Error("FetchedAt が 1 時間後なのに fresh 扱い (時計を戻すと古い CI 状態を永久に使う)")
		}
		ok := cacheEntry{FetchedAt: now, State: StateSuccess}
		if !ok.fresh(now) {
			t.Error("現在時刻の entry が fresh でない (ガードが広すぎる)")
		}
	})

	t.Run("doctorStartupToast", func(t *testing.T) {
		// 🚨 Total を直接入れても効かない。sanitizeDiskCache (issue 193) が Entries から
		// 再計算するので、実在するカタログ ID のエントリを積む必要がある。
		c := doctorDiskCache{
			ScannedAt: now.Add(48 * time.Hour),
			Entries: []doctorDiskCacheEntry{{
				ID: "xcode-deriveddata", Label: "Xcode DerivedData",
				Size: doctorToastThreshold * 2, Status: string(disk.StatusOK),
			}},
		}
		got := doctorStartupToast(c, true, now)
		if strings.Contains(got, "-") && strings.Contains(got, "日前") {
			t.Errorf("負の日数を出した: %q", got)
		}
		if !strings.Contains(got, "未来") {
			t.Errorf("診断時刻が未来であることを伝えていない: %q", got)
		}
	})
}

// 上のテストは 2 箇所しか見ていないので、**新しい鮮度判定が足されたときに追随を強制する**
// 走査を併せて持つ (列挙表は持たない。issue 117 の「列挙すると兄弟を足したときに追随を忘れる =
// この検査が守りたい事故を検査自身が踏む」を適用)。
//
// 対象は `now.Sub(x)` / `timeNow().Sub(x)` を **TTL 系の定数と比較している関数**だけ。
// アニメーションの経過 (statusOpenDuration 等) は比較先が Duration 系なので対象外になる。
func TestFreshnessChecksGuardAgainstClockRollback(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("パースできない: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("走査対象の package が 0 件 (ParseDir のフィルタが壊れている)")
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
		for _, tok := range []string{"age < 0", "age >= 0", ".After(now)", ".After(timeNow())"} {
			if strings.Contains(body, tok) {
				return true
			}
		}
		return false
	}

	checked := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
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
