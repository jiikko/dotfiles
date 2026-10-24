package gate

import (
	"testing"

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// e は鍵 key、段ごとのハッシュ (L1・L2・L3) の単位。
func e(key, h1, h2, h3 string) *group.Entry {
	return &group.Entry{
		Unit:   &tree.Unit{Key: key, File: key},
		Result: norm.Result{Hash: map[norm.Level]string{norm.L1: h1, norm.L2: h2, norm.L3: h3}, Nodes: 30},
	}
}

var opt = Options{
	Options: group.Options{MinNodes: 10, Levels: norm.Levels},
	Fail:    map[norm.Level]bool{norm.L1: true, norm.L2: true},
}

func TestNewCopyIsViolation(t *testing.T) {
	base := []*group.Entry{e("a", "x", "x2", "x3")}
	head := []*group.Entry{e("a", "x", "x2", "x3"), e("b", "x", "x2", "x3")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || r.Violations[0].Changed.Unit.Key != "b" || r.Violations[0].Level != norm.L1 || !r.Failed() {
		t.Fatalf("新しい写し b の L1 の違反を期待: %+v", r)
	}
	if len(r.Violations[0].Same) != 1 || r.Violations[0].Same[0].Unit.Key != "a" {
		t.Errorf("寄せる先の候補に a が無い: %+v", r.Violations[0].Same)
	}
}

func TestMoveRenameAndExistingCopiesAreNotViolations(t *testing.T) {
	// 既にある組 (a, b) は、触らなければ違反にならない。
	base := []*group.Entry{e("a", "x", "", ""), e("b", "x", "", "")}
	if r := Judge(base, []*group.Entry{e("a", "x", "", ""), e("b", "x", "", "")}, opt); len(r.Violations) != 0 || r.Changed != 0 {
		t.Errorf("変わっていないのに違反・変更がある: %+v", r)
	}
	// b を別のファイルへ移す・改名する (鍵が変わる) のは、数が増えないので違反にならない。
	if r := Judge(base, []*group.Entry{e("a", "x", "", ""), e("b2", "x", "", "")}, opt); len(r.Violations) != 0 || r.Changed != 1 {
		t.Errorf("移動・改名で違反になった: %+v", r)
	}
	// 写しを 1 つ寄せた (b が消えた) のは違反にならない。
	if r := Judge(base, []*group.Entry{e("a", "x", "", "")}, opt); len(r.Violations) != 0 {
		t.Errorf("寄せる作業で違反になった: %+v", r)
	}
}

func TestFirstMatchingLevelDecides(t *testing.T) {
	// c は L1 では誰とも一致せず、L2 で a と一致する: L2 の違反として 1 回だけ出る。
	base := []*group.Entry{e("a", "x", "y", "z")}
	head := []*group.Entry{e("a", "x", "y", "z"), e("c", "q", "y", "z")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || r.Violations[0].Level != norm.L2 {
		t.Fatalf("L2 の違反 1 つを期待: %+v", r)
	}
}

func TestReportOnlyLevelDoesNotFail(t *testing.T) {
	base := []*group.Entry{e("a", "x", "y", "z")}
	head := []*group.Entry{e("a", "x", "y", "z"), e("c", "q", "w", "z")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || r.Violations[0].Level != norm.L3 || r.Violations[0].Fail || r.Failed() {
		t.Fatalf("L3 の報告だけの違反を期待: %+v", r)
	}
}

func TestSmallTrivialAndUnreadableAreSkipped(t *testing.T) {
	base := []*group.Entry{e("a", "x", "y", "z")}
	small := e("s", "x", "y", "z")
	small.Result.Nodes = 3
	if r := Judge(base, []*group.Entry{e("a", "x", "y", "z"), small}, opt); len(r.Violations) != 0 {
		t.Errorf("下限より小さい単位が違反になった: %+v", r)
	}
	tr := e("t", "q", "w", "z")
	tr.Result.L3Trivial = true
	at := e("a", "x", "y", "z")
	at.Result.L3Trivial = true
	if r := Judge([]*group.Entry{at}, []*group.Entry{at, tr}, opt); len(r.Violations) != 0 {
		t.Errorf("L3Trivial の単位が L3 の違反になった: %+v", r)
	}
	un := e("u", "x", "y", "z")
	un.Unit.Unreadable = true
	r := Judge(base, []*group.Entry{e("a", "x", "y", "z"), un}, opt)
	if len(r.Violations) != 0 || len(r.Unreadable) != 1 {
		t.Errorf("読めない単位は違反にせず、確かめられなかった一覧に出すはず: %+v", r)
	}
}

func TestSwapIsNotViolation(t *testing.T) {
	// 組の古い写し b を消して新しい写し c を足す入れ替えは、数が増えないので違反にしない (検出しない形として README に書く)。
	base := []*group.Entry{e("a", "x", "", ""), e("b", "x", "", "")}
	head := []*group.Entry{e("a", "x", "", ""), e("c", "x", "", "")}
	if r := Judge(base, head, opt); len(r.Violations) != 0 {
		t.Errorf("入れ替えが違反になった: %+v", r)
	}
}

func TestOneViolationPerGroup(t *testing.T) {
	// 同じ形の新しい単位が 2 つ (b と c) 増えても、違反は 1 件で、残りは同じ形の一覧に入る。
	base := []*group.Entry{e("a", "x", "", "")}
	head := []*group.Entry{e("a", "x", "", ""), e("b", "x", "", ""), e("c", "x", "", "")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || len(r.Violations[0].Same) != 2 {
		t.Fatalf("違反 1 件・同じ形 2 つを期待: %+v", r)
	}
}
