package report

import (
	"strings"
	"testing"

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

func entry(key, file string, line, col, end int) *group.Entry {
	return &group.Entry{Unit: &tree.Unit{Key: key, File: file, Line: line, Col: col, EndLine: end}}
}

// 同じ行で左に別の単位が始まるメンバーは、行で指すと explain がその単位を選ぶので argv を出さない (別の単位どうしを比べさせない)。
// 黙って消さず、行で指せない数を出す。先頭が指せなければ、指せる次のメンバーを比べる基点にする。
func TestCompareHintsSkipMemberShadowedOnItsLine(t *testing.T) {
	outer := entry("a.go||f()", "a.go", 5, 1, 13)
	closure := entry("a.go|f|{closure}", "a.go", 5, 30, 12)
	other := entry("b.go|g|{closure}", "b.go", 7, 9, 14)
	third := entry("c.go|h|{closure}", "c.go", 3, 9, 10)
	entries := []*group.Entry{outer, closure, other, third}
	got := compareHints("/r", norm.L2, []*group.Entry{closure, other, third}, entries)
	want := []string{"違いを見る: dupscan explain -compare /r/c.go:3 /r/b.go:7", "行で指せないメンバー 1 個 (その行で explain が別の単位を選ぶ) は比べる argv を出さない。範囲で Read して比べる"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("行で指せるメンバーどうしを比べ、指せない数を出す: %q", got)
	}
	if got := compareHints("/r", norm.L2, []*group.Entry{other, third}, nil); got != nil {
		t.Errorf("entries が無い (gate の head が commit) なら argv を出さない: %q", got)
	}
}

func TestCompareHintsCapsOthers(t *testing.T) {
	var members []*group.Entry
	for _, f := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		members = append(members, entry(f+"||f()", f, 1, 1, 9))
	}
	got := compareHints("/r", norm.L2, members, members)
	if len(got) != compareShown+1 || got[compareShown] != "… 他 1 個も同じ形で比べる" {
		t.Errorf("他のメンバーは %d 個まで、残りは数だけ: %q", compareShown, got)
	}
}
