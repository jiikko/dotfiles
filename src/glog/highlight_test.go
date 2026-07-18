package main

import (
	"strings"
	"testing"
	"time"
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
// ローカル実測 ~0.9s (M3)。CI runner はローカル比 3〜4 倍遅い実績があるため閾値は 5s
// (桁級の回帰だけ捕まえる)。典型コミット (数百行) は ~0.1s で、取得は tea.Cmd 非同期 +
// スピナー付きなので体感は許容。これを超えて遅くなったら highlight.go ごと切り捨てる判断
// (同ファイル冒頭コメントの手順) を再評価する。
func TestHighlightDiffPerformance(t *testing.T) {
	lines := []string{"diff --git a/a.go b/a.go", "+++ b/a.go", "@@ -1 +1 @@"}
	for range maxDiffLines {
		lines = append(lines, `+func f(x int) string { return fmt.Sprintf("%d", x) } // comment`)
	}
	start := time.Now()
	HighlightDiff(lines)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("5000 行のハイライトに %v (5s 超): 切り捨て判断の閾値超過", elapsed)
	}
}
