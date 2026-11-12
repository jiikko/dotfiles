package lists_test

import (
	"os"
	"strings"
	"testing"

	"ja-writing/lists"
	"ja-writing/oracle"
)

// 埋め込んだ語の一覧が、固定した本家の commit のファイルと同じバイト列であること (SHA-256 を upstream.sha256 と照合する)。
func TestEmbeddedListsMatchUpstream(t *testing.T) {
	sums, err := oracle.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	names := lists.Names()
	if len(names) != 8 {
		t.Fatalf("語の一覧は 8 本のはず: %v", names)
	}
	for _, name := range names {
		got, err := lists.SHA256(name)
		if err != nil {
			t.Fatal(err)
		}
		want, ok := sums[oracle.ListsDir+"/"+name]
		if !ok {
			t.Fatalf("%s が upstream.sha256 に無い", name)
		}
		if got != want {
			t.Errorf("%s の SHA-256 %s が本家の %s と違う (改行の変換・整形を疑う)", name, got, want)
		}
	}
	for _, rel := range []string{"skills/yomiyasu/LICENSE", "skills/yomiyasu/UNICODE-LICENSE.txt"} {
		body, err := os.ReadFile("../" + strings.TrimPrefix(rel, "skills/yomiyasu/"))
		if err != nil {
			t.Fatal(err)
		}
		if got := oracle.SHA256Hex(body); got != sums[rel] {
			t.Errorf("%s の写しが本家と違う: %s", rel, got)
		}
	}
}

func TestReadUnknownListFails(t *testing.T) {
	if _, err := lists.Read("nope.txt"); err == nil {
		t.Fatal("無い名前はエラー")
	}
	if _, err := lists.SHA256("nope.txt"); err == nil {
		t.Fatal("無い名前はエラー")
	}
}
