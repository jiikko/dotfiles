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
