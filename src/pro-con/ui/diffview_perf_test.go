package ui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"pro-con/card"
)

// synthDiff は nFiles 個のファイルに割り振った、およそ nLines 行の `git diff` の本文 (色付けと見出しの判定を通る形)。
func synthDiff(nFiles, nLines int) string {
	var b strings.Builder
	for f := range nFiles {
		fmt.Fprintf(&b, "diff --git a/src/f%d.go b/src/f%d.go\nindex 1111111..2222222 100644\n--- a/src/f%d.go\n+++ b/src/f%d.go\n", f, f, f, f)
		for i := range nLines/nFiles - 4 {
			switch i % 12 {
			case 0:
				fmt.Fprintf(&b, "@@ -%d,10 +%d,11 @@ func f%d() {\n", i, i, f)
			case 3, 4:
				fmt.Fprintf(&b, "+\tx%d := fmt.Sprintf(\"%%d 件\", n) // 描くたびに組み直さない\n", i)
			case 7:
				fmt.Fprintf(&b, "-\ty%d := strings.Repeat(\"─\", w)\n", i)
			default:
				fmt.Fprintf(&b, " \treturn z%d\n", i)
			}
		}
	}
	return b.String()
}

// openedDiffModel は synthDiff(nFiles, nLines) を読み終えた差分の板を開いた、幅 200 × 高さ 50 の画面 (切った知らせも出す)。
func openedDiffModel(tb testing.TB, nFiles, nLines int) *Model {
	tb.Helper()
	m := diffModelWith(tb, synthDiff(nFiles, nLines))
	d := m.snap.Cards[0].Progress.Diff
	d.Files, d.Cut = nFiles, true
	m.width, m.height = 200, 50
	cmd := press(m, "D")
	m.Update(cmd())
	if !m.diff.open || m.diff.loading || len(m.diff.files) != nFiles {
		tb.Fatal("前提: 差分の板を読み終えて開いていない")
	}
	return m
}

// viewBytes は View 1 回が確保するバイト数 (10 回の平均)。
func viewBytes(m *Model) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	const n = 10
	for range n {
		_ = m.View()
	}
	runtime.ReadMemStats(&b)
	return (b.TotalAlloc - a.TotalAlloc) / n
}

// 差分の板を開いた画面の 1 描画の確保量は本文の行数で増えない (issue 529)。出すのは窓の数十行なのに、描くたびに全行
// (最大 card.DiffMaxLines) へ ansi.Strip と結合をかけていた。時間ではなく確保量の伸び率で見る。
// 見ていないもの: ファイルの数への比例 (畳みの位置は O(ファイル数) で持つ。ここでは両方 50 ファイル) / キー操作の経路
func TestDiffBoardViewAllocDoesNotGrowWithLines(t *testing.T) {
	small, large := viewBytes(openedDiffModel(t, 50, 500)), viewBytes(openedDiffModel(t, 50, card.DiffMaxLines))
	if r := float64(large) / float64(small); r > 1.3 {
		t.Fatalf("本文 500 → %d 行で 1 描画の確保量が %.2f 倍 (%d → %d バイト)。本文の行数に比例する組み立てが描く経路に入った", card.DiffMaxLines, r, small, large)
	}
}

// 全行の列を持たずに位置から引く行 (rowText / fileAt) は、畳みのどの組み合わせでも「見出し + 畳んでいない本文を並べ、
// 切った知らせを最後のファイルの行として足す」列と一致する (最後のファイルを畳んだときの知らせの行を含む)。
func TestDiffBoardRowsMatchFoldsAndCut(t *testing.T) {
	m := openedDiffModel(t, 3, 60)
	d := &m.diff
	for pat := range 1 << len(d.files) {
		for i := range d.folded {
			d.folded[i] = pat&(1<<i) != 0
		}
		d.relayout()
		var want []string
		var owner []int
		for i := range d.files {
			want, owner = append(want, d.headerText(i)), append(owner, i)
			if !d.folded[i] {
				for _, l := range d.body[i] {
					want, owner = append(want, l), append(owner, i)
				}
			}
		}
		want, owner = append(want, "知らせ"), append(owner, len(d.files)-1)
		if d.total != len(want) {
			t.Fatalf("畳み %03b: 行数 %d、並べた列は %d", pat, d.total, len(want))
		}
		for r := range want {
			got := d.rowText(r)
			if r == len(want)-1 {
				if !strings.Contains(got, "行で切った") {
					t.Fatalf("畳み %03b: 最後の行が切った知らせでない: %q", pat, got)
				}
			} else if got != want[r] {
				t.Fatalf("畳み %03b: 行 %d が %q (want %q)", pat, r, got, want[r])
			}
			if d.fileAt(r) != owner[r] {
				t.Fatalf("畳み %03b: 行 %d のファイルが %d (want %d)", pat, r, d.fileAt(r), owner[r])
			}
		}
	}
}

// 差分の板を閉じた / 開いた画面の View (issue 529 の前後を測る。本文は 82 ファイル・card.DiffMaxLines 行)。
//
//	go test -run '^$' -bench BenchmarkDiffBoardView -benchmem -count 10 ./ui/
func BenchmarkDiffBoardView(b *testing.B) {
	for _, open := range []bool{false, true} {
		name := map[bool]string{false: "closed", true: "open"}[open]
		b.Run(name, func(b *testing.B) {
			m := openedDiffModel(b, 82, card.DiffMaxLines)
			if !open {
				press(m, "D")
			}
			b.ReportAllocs()
			for b.Loop() {
				_ = m.View()
			}
		})
	}
}
