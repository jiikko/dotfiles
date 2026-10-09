package markdown

import (
	"strings"
	"testing"
	"time"
)

// 閉じない `*` `~~` `[` が多い段落と、とても長い段落は線形の時間で描く (issue 699 の 2)。
// 入力は旧実装 (開きのたびに末尾まで探す・行ごとに段落を作り直す) なら数分かかる大きさ (12 万字で 7〜9 秒の 2 乗)。
// 今の実装は数十 ms。10 秒は hang guard で、実時間の合否ではない (2 乗に戻ったときだけ落ちる)。
func TestRenderPathologicalParagraphsAreLinear(t *testing.T) {
	for name, in := range map[string]string{
		"em":     strings.Repeat("*a ", 200000),
		"strike": strings.Repeat("~~a ", 200000),
		"link":   strings.Repeat("[a](", 200000),
		"lines":  strings.Repeat("x\n", 1000000),
		// 入れ子のリンクとリンクの中の強調: 中身を読み直すたびに表を作り直すと深さぶん 2 乗 (反証レビューで 150 KB 4.97 秒)
		"nested-link":    strings.Repeat("[", 100000) + "x" + strings.Repeat("](u)", 100000),
		"strong-in-link": strings.Repeat("[**", 50000) + "x" + strings.Repeat("**](u)", 50000),
		// 入れ子のリンクの中に span が多い: 入れ子のたびに中の span を歩き直すと深さ × span の数 (2 周目の反証レビューで 90 KB 2.85 秒)
		"spans-in-nested-link": strings.Repeat("[", 200000) + strings.Repeat("a`b`", 200000) + strings.Repeat("](x)", 200000),
	} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Render(in, 80, false)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: 10 秒で描き終わらない (段落の長さの 2 乗に戻っている)", name)
		}
	}
}
