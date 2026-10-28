package tree

import "testing"

// MatchName は、全体を囲む 1 組のバッククォートだけを外す。Go の名前・演算子・途中にバッククォートを含む文字列は変えない。
func TestMatchName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"`repeat`", "repeat"},
		{"repeat", "repeat"},
		{"`default`", "default"},
		{"", ""},
		{"``", "``"},       // 中身が空: 名前ではない
		{"`", "`"},         // 1 文字
		{"`a", "`a"},       // 片側だけ
		{"a`", "a`"},       // 片側だけ
		{"`a`b`", "`a`b`"}, // 途中にもある: 外さない
		{"a`b`c", "a`b`c"},
		{"A.`repeat`", "A.`repeat`"}, // 型の修飾つきの全体は名前ではないので、呼び出し側で名前だけを渡す
		{"==", "=="},
		{"`a b`", "a b"}, // 空白を含む囲みは Swift では無効だが、囲みがあれば外す
	} {
		if got := MatchName(c.in); got != c.want {
			t.Errorf("MatchName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
