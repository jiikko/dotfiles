package pyre

import (
	"os"
	"strings"
	"testing"
)

// regexp2 は go.mod の replace で third_party/regexp2 の写しを使う (compile の最適化の 1 字ずつの走査に上限を置いた写し。
// third_party/regexp2/README.md)。replace が外れるか、写し直しで上限が消えると、lint の起動込みの所要が 3 割ほど延びるが、
// 答えは変わらないので記録の比べでは気づけない。テストのバイナリのビルド情報は依存の一覧を持たないので、ファイルを読んで確かめる。
func TestRegexp2IsTheLocalFork(t *testing.T) {
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "\nreplace github.com/dlclark/regexp2/v2 => ./third_party/regexp2\n") {
		t.Error("go.mod に regexp2 を third_party/regexp2 の写しに置き換える replace が無い")
	}
	src, err := os.ReadFile("../../third_party/regexp2/syntax/charclass.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "budget := enumerationBudget") {
		t.Error("third_party/regexp2 の mayOverlapByEnumeration に上限 (enumerationBudget) が無い")
	}
}
