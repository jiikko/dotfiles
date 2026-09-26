package ui

import (
	"strings"
	"testing"
)

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
