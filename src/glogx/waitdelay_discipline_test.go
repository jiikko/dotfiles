package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryCommandContextSetsWaitDelay は「exec.CommandContext を呼んだら WaitDelay を張る」
// 規律をソース走査で強制する。
//
// なぜテストで縛るか (issue 105): この規律は 13 箇所中 12 箇所で守られ、1 箇所
// (issues/discover.go の RepoRoot) だけが静かに抜けていた。抜けても手元でも CI でも何も起きず、
// 実際にハングして初めて分かる種類の欠落なので、**書く人が覚えているか**に依存させない。
//
// 例外は行内に `subproc: no-waitdelay` と理由を書く。出力を取らない Run() は stdout/stderr が
// /dev/null 直結でパイプを作らないため、ctx の kill だけで足りる (WaitDelay が要るのは
// Output / CombinedOutput / Stdout に io.Writer を張る形)。
func TestEveryCommandContextSetsWaitDelay(t *testing.T) {
	const lookahead = 8 // CommandContext の直後に WaitDelay を探す行数

	var offenders []string
	checked := 0
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// subproc 自身は規律の実装元。tools/ は本体から参照しない調査ツール。
			if d.Name() == "subproc" || d.Name() == "tools" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "exec.CommandContext(") {
				continue
			}
			checked++
			if strings.Contains(line, "subproc: no-waitdelay") {
				continue
			}
			found := false
			for j := i; j < len(lines) && j <= i+lookahead; j++ {
				if strings.Contains(lines[j], "WaitDelay") {
					found = true
					break
				}
			}
			if !found {
				offenders = append(offenders, filepath.ToSlash(path)+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ソース走査に失敗: %v", err)
	}
	// 走査対象 0 件を緑にしない (パス変更や WalkDir の失敗で検査が消えるのを防ぐ)
	if checked == 0 {
		t.Fatal("exec.CommandContext を 1 つも見つけられなかった (走査が壊れている)")
	}
	if len(offenders) > 0 {
		t.Fatalf("WaitDelay を張っていない exec.CommandContext がある (subproc.CommandContext を使うか、"+
			"理由つきで `subproc: no-waitdelay` を行内に書く):\n  %s", strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
