package term

import "testing"

// IsCopySafe の境界: タブだけ通し、貼ったときに端末やシェルが解釈するものは断る。
func TestIsCopySafe(t *testing.T) {
	for s, want := range map[string]bool{
		"/repo/issues/001-feat-x.md": true,
		"drive\tsync":                true,  // group 名の実例 (生でコピーする決定)
		"a\nrm -rf x":                false, // 貼った瞬間にコマンドが走る
		"a\x1b[201~b":                false, // bracketed paste の終わり
		"a\u0085b":                   false, // C1
		"a\u202eb":                   false, // BiDi
		"a\rb":                       false,
		"\u26a0\ufe0f-notes":         true, // 絵文字の VS16 は制御文字ではない (無害化の結果との一致で判定すると断っていた)
		"epic-\u2764\ufe0f":          true,
	} {
		if got := IsCopySafe(s); got != want {
			t.Errorf("IsCopySafe(%q) = %v, want %v", s, got, want)
		}
	}
}
