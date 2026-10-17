package commitview

import (
	"glogx/internal/term"
	"testing"
)

// uniformWidth は box 描画行の表示幅が全行で揃っている (枠が崩れていない) ことを検証して
// その幅を返す。diff / job 詳細のスクロールバーテストが共有する (同一クロージャが 2 ファイルへ
// コピーされていた。issue 030)。
func uniformWidth(t *testing.T, box []string) int {
	t.Helper()
	w := term.DispWidth(term.StripANSI(box[0]))
	for i, l := range box {
		if got := term.DispWidth(term.StripANSI(l)); got != w {
			t.Fatalf("行 %d の表示幅 = %d, 他の行 = %d: %q", i, got, w, l)
		}
	}
	return w
}
