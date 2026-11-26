package pyre

import (
	"testing"

	"github.com/dlclark/regexp2/v2/syntax"
)

// regexp2 は go.mod の replace で third_party/regexp2 の写しを使う (compile の最適化の 1 字ずつの走査に上限を置いた写し。
// third_party/regexp2/README.md)。replace が外れるか、写し直しで上限が消えると、lint の起動込みの所要が 3 割ほど延びるが、
// 答えは変わらないので記録の比べでは気づけない。
// BMP の全部のループの後ろに BMP の外の集合が来る形は、重ならないことを知るのに 100 万字を回す。上流は回し切って重ならないと知り、
// ループを atomic にする (NtSetloopatomic)。写しは上限で「重なりうる」と答え、atomic にしない (NtSetloop)。
func TestRegexp2IsTheLocalFork(t *testing.T) {
	tree, err := syntax.Parse(`[\x{0}-\x{FFFF}]*[\x{10000}-\x{10FFFF}]`, syntax.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	loop := tree.Root
	for loop.T != syntax.NtSetloop && loop.T != syntax.NtSetloopatomic {
		if len(loop.Children) == 0 {
			t.Fatalf("ループのノードが見つからない (%v)", loop.T)
		}
		loop = loop.Children[0]
	}
	if loop.T != syntax.NtSetloop {
		t.Fatalf("ループが %v になった。regexp2 が third_party/regexp2 の写し (1 字ずつの走査に上限) でない", loop.T)
	}
}

// 後方参照が、前のループの手放した字を後ろの 0 回からのループに取らせて当たる形で、本家の re と同じ答えになる
// (third_party/regexp2/README.md の「直した所」の 2。上流の regexp2 は前のループを atomic にして当たらない)。
// 1 つ目は後ろのループが前のループと重なる形 (上流の誤り)、2 つ目は上限を超える大きい集合で「重なりうる」と答える形。
func TestRegexp2BackreferenceAfterNullableLoopMatchesPython(t *testing.T) {
	for _, c := range []struct{ pattern, text string }{
		{`([ad]*)([ab]*)c\2b`, "aacab"},
		{`([\x00-\u1fff]*)([\U00010000-\U0010ffff]*)([ab]*)\u3000\3b`, "aa\u3000ab"},
	} {
		m, err := MustCompile(c.pattern, UNICODE).Search(NewText(c.text), 0, NoEnd)
		if err != nil {
			t.Fatal(err)
		}
		if m == nil || m.Start(0) != 0 || m.End(0) != 5 {
			t.Errorf("%q を %q に当てた答えが本家の re (span (0, 5)) と違う: %v", c.pattern, c.text, m)
		}
	}
}

// 大きい集合の後ろに 0 回からの小さい集合のループが来る形は、小さい方を回して重ならないと知り、前のループを atomic にする
// (third_party/regexp2/README.md の「直した所」の 3。大きい方を回すと上限で「重なりうる」に倒れ、長い漢字の並びで約 6 倍遅くなる)。
// lint の本番のパターン (片仮名・漢字の語の後ろに平仮名の送り) と同じ形。
func TestRegexp2EnumeratesTheSmallerSet(t *testing.T) {
	tree, err := syntax.Parse(`[\x{3005}-\x{3006}\x{30A1}-\x{30F6}\x{30FC}\x{4E00}-\x{9FA5}]*[\x{3041}-\x{3093}]*(?=\n?\z)`, syntax.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	loop := tree.Root
	for loop.T != syntax.NtSetloop && loop.T != syntax.NtSetloopatomic {
		if len(loop.Children) == 0 {
			t.Fatalf("ループのノードが見つからない (%v)", loop.T)
		}
		loop = loop.Children[0]
	}
	if loop.T != syntax.NtSetloopatomic {
		t.Fatalf("前のループが %v になった (atomic でない)。MayOverlap が字の少ない方を回していない", loop.T)
	}
}
