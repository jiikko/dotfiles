package main

import (
	"strings"
	"testing"
)

func TestHighlightDiffStructure(t *testing.T) {
	in := []string{
		"commit deadbeef",
		"    subject line",
		" a.go | 2 +-",
		"diff --git a/a.go b/a.go",
		"index 123..456 100644",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -1,2 +1,2 @@ func main",
		" var x = 1",
		"+return fmt.Sprintf(\"%d\", x)",
		"-return x",
	}
	out := HighlightDiff(in)
	if len(out) != len(in) {
		t.Fatalf("行数が変わった: %d -> %d", len(in), len(out))
	}
	if !strings.HasPrefix(out[0], ansiYellow) {
		t.Errorf("commit 行が黄色でない: %q", out[0])
	}
	if out[1] != in[1] || out[2] != in[2] {
		t.Errorf("メッセージ/stat 行は素のままのはず: %q %q", out[1], out[2])
	}
	if !strings.HasPrefix(out[7], ansiCyan) {
		t.Errorf("hunk 行がシアンでない: %q", out[7])
	}
	if !strings.HasPrefix(out[9], ansiGreen+"+") {
		t.Errorf("追加行の記号が緑でない: %q", out[9])
	}
	if !strings.HasPrefix(out[10], ansiRed+"-") {
		t.Errorf("削除行の記号が赤でない: %q", out[10])
	}
	// .go の lexer が効いてコード部分に SGR (256色 fg) が入る
	if !strings.Contains(out[9], "\x1b[38;5;") {
		t.Errorf("Go コードにシンタックスハイライトが入っていない: %q", out[9])
	}
}

func TestHighlightDiffUnknownLanguagePassesThrough(t *testing.T) {
	in := []string{
		"diff --git a/data.unknownext b/data.unknownext",
		"+++ b/data.unknownext",
		"@@ -0,0 +1 @@",
		"+opaque payload line",
	}
	out := HighlightDiff(in)
	want := ansiGreen + "+" + ansiReset + "opaque payload line"
	if out[3] != want {
		t.Errorf("言語不明は記号色のみのはず: %q; want %q", out[3], want)
	}
}

func TestHighlightDiffDeletedFileDevNull(t *testing.T) {
	in := []string{
		"diff --git a/a.go b/a.go",
		"deleted file mode 100644",
		"--- a/a.go",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-package main",
	}
	out := HighlightDiff(in)
	want := ansiRed + "-" + ansiReset + "package main"
	if out[5] != want {
		t.Errorf("/dev/null (削除ファイル) は素通しのはず: %q; want %q", out[5], want)
	}
}

// 性能ガード: 最悪ケース (maxDiffLines=5000 行が全部 Go コード) のハイライト時間の回帰検出。
// ローカル実測 ~0.9s (M3)。典型コミット (数百行) は ~0.1s で、取得は tea.Cmd 非同期 +
// スピナー付きなので体感は許容。
// BenchmarkHighlightDiff は maxDiffLines 相当 (diff popup の production 上限) の
// ハイライト所要時間を測る。以前は固定 5s の wall-clock を assert する Test だったが、
// 共有 CI runner (ubuntu-slim) の速度ムラで chroma が 5s を超え頻繁に flake した
// (2026-07-21: 実測 9.48s で fail)。固定 wall-clock は shared runner で構造的に flaky
// なので Benchmark へ移し、CI の go test ./... では走らせない (perf を見たいときは
// go test -bench=HighlightDiff で測る)。production の暴走ガードは diff 読み込み側の
// maxDiffLines 上限 (gitlog.go) が担うため、CI での wall-clock ゲートは不要。
func BenchmarkHighlightDiff(b *testing.B) {
	lines := []string{"diff --git a/a.go b/a.go", "+++ b/a.go", "@@ -1 +1 @@"}
	for range maxDiffLines {
		lines = append(lines, `+func f(x int) string { return fmt.Sprintf("%d", x) } // comment`)
	}
	b.ResetTimer()
	for range b.N {
		HighlightDiff(lines)
	}
}

// セルフレビューで検出した実バグの回帰ガード: hunk 内の "+++" / "---" 始まりコード行
// (先頭 ++ / -- の行の追加/削除) をファイルヘッダーと誤分類しないこと。誤分類すると
// ± マーカーを失う上、偽パスで lexer が潰れ以降のハイライトが消える。
func TestHighlightDiffHunkContentNotHeader(t *testing.T) {
	in := []string{
		"diff --git a/a.go b/a.go",
		"+++ b/a.go",
		"@@ -1,2 +1,2 @@",
		"--- decrement twice",
		"+++ increment twice",
		"+var x = 1", // lexer が生きていることの確認用
	}
	out := HighlightDiff(in)
	if !strings.HasPrefix(out[3], ansiRed+"-") {
		t.Errorf("hunk 内の --- 始まり削除行がヘッダー扱い: %q", out[3])
	}
	if !strings.HasPrefix(out[4], ansiGreen+"+") {
		t.Errorf("hunk 内の +++ 始まり追加行がヘッダー扱い: %q", out[4])
	}
	if !strings.Contains(out[5], "\x1b[38;5;") {
		t.Errorf("偽パスで lexer が潰れて以降のハイライトが消えている: %q", out[5])
	}
}

// 複数ファイルのコミット: 2 つ目の diff --git で lexer と hunk 状態がリセットされること。
func TestHighlightDiffMultiFileResetsState(t *testing.T) {
	in := []string{
		"diff --git a/a.go b/a.go",
		"+++ b/a.go",
		"@@ -1 +1 @@",
		"+var x = 1",
		"diff --git a/data.unknownext b/data.unknownext",
		"+++ b/data.unknownext",
		"@@ -1 +1 @@",
		"+var x = 1",
	}
	out := HighlightDiff(in)
	if !strings.Contains(out[3], "\x1b[38;5;") {
		t.Errorf("1 つ目のファイル (.go) がハイライトされていない: %q", out[3])
	}
	if strings.Contains(out[7], "\x1b[38;5;") {
		t.Errorf("2 つ目のファイル (言語不明) に前ファイルの lexer が残っている: %q", out[7])
	}
	if !strings.HasPrefix(out[5], ansiBold) {
		t.Errorf("2 つ目の +++ がヘッダー扱いされていない (inHunk リセット漏れ): %q", out[5])
	}
}
