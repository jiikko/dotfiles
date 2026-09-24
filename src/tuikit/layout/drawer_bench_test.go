package layout

import (
	"fmt"
	"strings"
	"testing"

	"tuikit/sgr"
	"tuikit/termwidth"
)

// drawerFixture は glogx の issues viewer に寄せた一覧と本文 (SGR つき・日本語と記号混じり)。
// 日本語が入ると termwidth の近道を通らず ansi の grapheme 走査になるので、実運用の重さが出る。
//
// 本文は ComposeDrawer の契約どおり**開ききった幅で整形済み** (板の幅 - 区切り線 - スクロールバー)。
func drawerFixture(rows, width, target int) (list, panel []string) {
	inner := target - 1 - ScrollbarWidth
	for i := range rows {
		list = append(list, fmt.Sprintf("%s→ %03d ● feat     一覧のタイトル %s%s", sgr.Yellow, i, strings.Repeat("x", i%40), sgr.Reset))
		body := sgr.Cyan + " ## 節 " + sgr.Reset + strings.Repeat("本文の説明が続く ", 1+i%(width/16))
		panel = append(panel, termwidth.Truncate(body, inner, ""))
	}
	return list, panel
}

// 開ききった引き出し (本文を読んでいる間、再描画のたびに全行で走る) と、開く途中の 1 フレーム。
func BenchmarkComposeDrawer(b *testing.B) {
	geo := DrawerGeometry{Ratio: 0.8, Extra: 10, MinList: 8, MaxPeek: 18}
	for _, width := range []int{120, 250} {
		target := geo.Target(width)
		list, panel := drawerFixture(50, width, target)
		for _, c := range []struct {
			name string
			w    int
		}{{"open", target}, {"half", target / 2}} {
			b.Run(fmt.Sprintf("w%d/%s", width, c.name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					ComposeDrawer(list, panel, c.w, width, true)
				}
			})
		}
	}
}
