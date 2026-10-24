package gate

import (
	"strings"
	"testing"

	"dupscan/internal/exclude"
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
	if len(r.Violations) != 1 || len(r.Violations[0].New) != 1 || r.Violations[0].New[0].Unit.Key != "b" || r.Violations[0].Level != norm.L1 || !r.Failed() {
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
	// 同じ形の新しい単位が 2 つ (b と c) 増えても、違反は 1 件で、2 つとも新しい側に入る。
	base := []*group.Entry{e("a", "x", "", "")}
	head := []*group.Entry{e("a", "x", "", ""), e("b", "x", "", ""), e("c", "x", "", "")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || len(r.Violations[0].New) != 2 || len(r.Violations[0].Same) != 1 {
		t.Fatalf("違反 1 件・新しい 2 つ・前からいた 1 つを期待: %+v", r)
	}
}

func TestSameEditToExistingCopiesIsNotViolation(t *testing.T) {
	// 既にある写し a・b に同じ修正を入れた: head の形 (y) は base に無いが、2 つとも base で同じ組 (x) だったので違反にしない。
	base := []*group.Entry{e("a", "x", "", ""), e("b", "x", "", "")}
	head := []*group.Entry{e("a", "y", "", ""), e("b", "y", "", "")}
	if r := Judge(base, head, opt); len(r.Violations) != 0 || r.Changed != 2 {
		t.Errorf("同じ修正で違反になった: %+v", r)
	}
	// その上で新しい写し c を足せば違反。
	head = append(head, e("c", "y", "", ""))
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || len(r.Violations[0].New) != 1 || r.Violations[0].New[0].Unit.Key != "c" {
		t.Errorf("新しい写し c だけを寄せる側に期待: %+v", r)
	}
}

func TestUnreadableCopyDoesNotHideNewCopy(t *testing.T) {
	// 既にある写し b が読めない構文になり、新しい写し n を足した: b は数えられないが、n は新しい写しなので違反。
	base := []*group.Entry{e("a", "x", "", ""), e("b", "x", "", "")}
	b := e("b", "broken", "", "")
	b.Unit.Unreadable = true
	head := []*group.Entry{e("a", "x", "", ""), b, e("n", "x", "", "")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || r.Violations[0].New[0].Unit.Key != "n" {
		t.Errorf("読めない写しが新しい写しを隠した: %+v", r)
	}
}

func TestMovedCopyIsNotNamedAsNew(t *testing.T) {
	// 古い写し b を b2 へ移し、新しい写し z を足した: 寄せる側は z で、移した b2 は前からいた側。
	base := []*group.Entry{e("a", "x", "", ""), e("b", "x", "", "")}
	head := []*group.Entry{e("a", "x", "", ""), e("b2", "x", "", ""), e("z", "x", "", "")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || len(r.Violations[0].New) != 1 || r.Violations[0].New[0].Unit.Key != "z" {
		t.Errorf("移した写しを寄せる側にした: %+v", r)
	}
}

func TestFailLevelsAreMonotonic(t *testing.T) {
	// L1 で一致する写しは、L2 を止める設定でも止める (L1 の一致は L2 の一致を含む)。
	base := []*group.Entry{e("a", "x", "y", "z")}
	head := []*group.Entry{e("a", "x", "y", "z"), e("b", "x", "y", "z")}
	o := opt
	o.Fail = map[norm.Level]bool{norm.L2: true}
	if r := Judge(base, head, o); !r.Failed() {
		t.Errorf("L2 を止める設定で L1 の写しが止まらない: %+v", r)
	}
}

func TestClosureRenumberIsNotViolation(t *testing.T) {
	// f の先頭にクロージャを 1 つ足すと、後ろのクロージャの鍵の番号がずれる。既にある組 (f#closure2, g) はずれても違反にしない。
	c := func(key, h string) *group.Entry { return e(key, h, h+"2", h+"3") }
	base := []*group.Entry{c("f#closure1", "p"), c("f#closure2", "q"), c("g", "q")}
	head := []*group.Entry{c("f#closure1", "r"), c("f#closure2", "p"), c("f#closure3", "q"), c("g", "q")}
	if r := Judge(base, head, opt); len(r.Violations) != 0 {
		t.Errorf("クロージャの番号のずれで違反になった: %+v", r)
	}
}

func TestRenameWithSmallEditIsNotViolation(t *testing.T) {
	// 既にある写し b を改名しながら小さく直した (L1 は変わり L2 は同じ): 組 (L2) の数は増えていない。
	base := []*group.Entry{e("a", "x", "y", ""), e("b", "x", "y", "")}
	head := []*group.Entry{e("a", "x", "y", ""), e("b2", "x2", "y", "")}
	if r := Judge(base, head, opt); len(r.Violations) != 0 {
		t.Errorf("改名と小さな編集で違反になった: %+v", r)
	}
}

func TestUntouchedUnitIsNotNamedAsNew(t *testing.T) {
	// 別の組 (b, c) を a と同じ形に揃えた: 違反だが、寄せる側は触った b・c で、触っていない a は前からいた側。
	base := []*group.Entry{e("a", "x", "", ""), e("b", "y", "", ""), e("c", "y", "", "")}
	head := []*group.Entry{e("a", "x", "", ""), e("b", "x", "", ""), e("c", "x", "", "")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 {
		t.Fatalf("違反 1 件を期待: %+v", r)
	}
	v := r.Violations[0]
	if len(v.Same) != 1 || v.Same[0].Unit.Key != "a" || len(v.New) != 2 {
		t.Errorf("触っていない a を寄せる側にした: new=%v same=%v", keys(v.New), keys(v.Same))
	}
}

func keys(es []*group.Entry) []string {
	var out []string
	for _, x := range es {
		out = append(out, x.Unit.Key)
	}
	return out
}

// cl は段ごとに別の値を持つクロージャ。
func cl(key, h1, h2, h3 string) *group.Entry {
	x := e(key, h1, h2, h3)
	x.Unit.Kind = tree.KindClosure
	return x
}

func TestClosureShiftWithEditIsNotViolation(t *testing.T) {
	// 先頭のクロージャを消して後ろの番号がずれ、ずれた写しを小さく直した (鍵は消えたクロージャと衝突する)。
	base := []*group.Entry{cl("f#closure1", "p", "p2", "p3"), cl("f#closure2", "q", "q2", "q3"), e("g", "q", "q2", "q3")}
	head := []*group.Entry{cl("f#closure1", "q'", "q2", "q3"), e("g", "q", "q2", "q3")}
	if r := Judge(base, head, opt); len(r.Violations) != 0 {
		t.Errorf("番号のずれと小さな編集で違反になった: %+v", r)
	}
	// ずれたうえで、組の 2 つに同じ修正を入れた。
	head = []*group.Entry{cl("f#closure1", "r", "r2", "r3"), e("g", "r", "r2", "r3")}
	if r := Judge(base, head, opt); len(r.Violations) != 0 {
		t.Errorf("番号のずれと同じ修正で違反になった: %+v", r)
	}
}

func TestOriginOrderDoesNotDecide(t *testing.T) {
	// 写しの組 (m, d) に同じ修正を入れて d を移し (n1)、L3 でだけ似ている新しい単位 n2 を足した: head の並びに依らず止めない。
	base := []*group.Entry{e("m", "D", "Dy", "Z"), e("d", "D", "Dy", "Z")}
	mk := func() (m, n1, n2 *group.Entry) {
		m, n1, n2 = e("m", "P", "Py", "Z"), e("n1", "P", "Py", "Z"), e("n2", "Q", "Qy", "Z")
		n1.Unit.Name = "d"
		return
	}
	m, n1, n2 := mk()
	a := Judge(base, []*group.Entry{m, n1, n2}, opt)
	m, n1, n2 = mk()
	b := Judge(base, []*group.Entry{m, n2, n1}, opt)
	if a.Failed() || b.Failed() {
		t.Errorf("同じ修正と移動で止まった (並び m,n1,n2: %v / m,n2,n1: %v)", a.Failed(), b.Failed())
	}
}

func TestIneligibleBaseUnitIsNotOrigin(t *testing.T) {
	// base でクロージャの下限より小さく組に入らなかった単位を消し、同じ本体の関数 n を足した: n は新しい写し。
	o := opt
	o.MinClosureNodes = 20
	small := func(x *group.Entry) *group.Entry { x.Result.Nodes = 15; return x }
	base := []*group.Entry{small(e("a", "x", "x2", "x3")), small(cl("h#closure1", "x", "x2", "x3"))}
	head := []*group.Entry{small(e("a", "x", "x2", "x3")), small(e("n", "x", "x2", "x3"))}
	r := Judge(base, head, o)
	if !r.Failed() || len(r.Violations) != 1 || keys(r.Violations[0].New)[0] != "n" {
		t.Errorf("比べる対象でなかった単位を出どころにして新しい写しを通した: %+v", r)
	}
}

func TestRenameInFileIsNotNamedAsNew(t *testing.T) {
	// B の fmt を render へ改名し、別のファイル C に同じ名前の写し fmt を足した: 新しいのは C の fmt。
	at := func(key, file, name string) *group.Entry {
		x := e(key, "x", "x2", "x3")
		x.Unit.File, x.Unit.Name = file, name
		return x
	}
	base := []*group.Entry{at("A|a", "A", "a"), at("B|fmt", "B", "fmt")}
	head := []*group.Entry{at("A|a", "A", "a"), at("B|render", "B", "render"), at("C|fmt", "C", "fmt")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || len(r.Violations[0].New) != 1 || r.Violations[0].New[0].Unit.Key != "C|fmt" {
		t.Errorf("改名した写しを新しいと名指しした: %+v", r)
	}
}

func TestSmallCopiesGrowingTogetherAreNotViolation(t *testing.T) {
	// base で下限より小さかったクロージャの写し 2 つが、同じ修正で下限を超えた: base で既に写しだったので止めない。
	o := opt
	o.MinClosureNodes = 20
	small := func(x *group.Entry) *group.Entry { x.Result.Nodes = 15; return x }
	base := []*group.Entry{small(cl("f#closure1", "x", "x2", "x3")), small(cl("g#closure1", "x", "x2", "x3"))}
	head := []*group.Entry{cl("f#closure1", "y", "y2", "y3"), cl("g#closure1", "y", "y2", "y3")}
	if r := Judge(base, head, o); len(r.Violations) != 0 {
		t.Errorf("小さかった写しが一緒に育って違反になった: %+v", r)
	}
}

func TestIneligibleHeadUnitDoesNotTakeOrigin(t *testing.T) {
	// 写し b を改名しながら小さく直し、別のファイルに下限より小さいクロージャ Y を足した: Y が b を相手に取って改名した写しを止めない。
	o := opt
	o.MinClosureNodes = 20
	small := func(x *group.Entry) *group.Entry { x.Result.Nodes = 15; return x }
	base := []*group.Entry{small(e("a", "x", "y", "z")), small(e("b", "x", "y", "z"))}
	for _, yl1 := range []string{"x", "x3"} { // L1 で b と同じ / L2 でだけ同じ
		for _, yFirst := range []bool{false, true} {
			a, b2, y := small(e("a", "x", "y", "z")), small(e("b2", "x2", "y", "z")), small(cl("Y#closure1", yl1, "y", "z"))
			head := []*group.Entry{a, b2, y}
			if yFirst {
				head = []*group.Entry{a, y, b2}
			}
			if r := Judge(base, head, o); r.Failed() {
				t.Errorf("小さい単位 Y (L1=%s, 先=%v) が出どころを取って止めた: %+v", yl1, yFirst, r)
			}
		}
	}
}

func TestSameEditJoiningAnotherGroupAtCoarserLevelIsViolation(t *testing.T) {
	// 写し a・b に同じ修正を入れ、L1 では a・b の組のままだが、L2 で触っていない d と同じ形になった: d が新しく写しになる。
	base := []*group.Entry{e("a", "x", "y", "z"), e("b", "x", "y", "z"), e("d", "p", "q", "r")}
	head := []*group.Entry{e("a", "x2", "q", "r"), e("b", "x2", "q", "r"), e("d", "p", "q", "r")}
	r := Judge(base, head, opt)
	if !r.Failed() || len(r.Violations) != 1 || r.Violations[0].Level != norm.L2 {
		t.Fatalf("L2 の違反 1 件を期待: %+v", r)
	}
	if v := r.Violations[0]; len(v.Same) != 1 || v.Same[0].Unit.Key != "d" {
		t.Errorf("触っていない d を寄せる側にした: new=%v same=%v", keys(v.New), keys(v.Same))
	}
}

func TestSmallUnitGrowingWithItsCopyIsNotViolation(t *testing.T) {
	// 関数 a と、同じ本体で下限より小さかったクロージャが、同じ修正で一緒に育った: base で既に写しだった。
	o := opt
	o.MinClosureNodes = 20
	a, f := e("a", "x", "x2", "x3"), cl("f#closure1", "x", "x2", "x3")
	a.Result.Nodes, f.Result.Nodes = 15, 15
	head := []*group.Entry{e("a", "y", "y2", "y3"), cl("f#closure1", "y", "y2", "y3")}
	if r := Judge([]*group.Entry{a, f}, head, o); len(r.Violations) != 0 {
		t.Errorf("一緒に育った写しが違反になった: %+v", r)
	}
}

// sized は節の数を変えた単位。
func sized(x *group.Entry, n int) *group.Entry { x.Result.Nodes = n; return x }

func TestNewCopiesDoNotTakeSmallUnitsAsOrigin(t *testing.T) {
	o := opt
	o.MinClosureNodes = 20
	smalls := func() []*group.Entry {
		return []*group.Entry{sized(cl("S|f#closure1", "s1", "SY", "Z"), 8), sized(cl("S|f#closure2", "s2", "SY", "Z"), 8)}
	}
	news := []*group.Entry{sized(cl("V|g#closure1", "N", "NY", "Z"), 28), sized(cl("W|h#closure1", "N", "NY", "Z"), 28)}
	// 触っていない小さい写しの相手を、新しい写し 2 つが取って通っていた。
	r := Judge(smalls(), append(smalls(), news...), o)
	if !r.Failed() || r.Changed != 2 {
		t.Errorf("新しい写しが小さい単位の相手を取って通った (changed は新しい 2 つだけのはず): %+v", r)
	}
	// 小さい写しを消して新しい写しを足しても、小さい単位は比べていなかったので新しい写し。
	r = Judge(smalls(), news, o)
	if !r.Failed() {
		t.Errorf("消した小さい写しの代わりの新しい写しが通った: %+v", r)
	}
	// 新しい写しどうしの組では、どちらも新しい側に出す。
	if v := r.Violations; len(v) != 1 || len(v[0].New) != 2 || len(v[0].Same) != 0 {
		t.Errorf("新しい写しどうしの組の名指し: %+v", r)
	}
}

func TestMoveWithSmallSameBodyClosureIsNotViolation(t *testing.T) {
	// 写し b を別のファイルへ移しただけ。同じ本体の小さいクロージャ s が base で先に並んでいても、s が b の相手を取らない。
	o := opt
	o.MinClosureNodes = 20
	at := func(key, file string, small bool) *group.Entry {
		var x *group.Entry
		if small {
			x = cl(key, "X", "XY", "XZ")
		} else {
			x = e(key, "X", "XY", "XZ")
		}
		x.Unit.File = file
		return sized(x, 15)
	}
	for _, sFirst := range []bool{true, false} {
		a, s, b := at("A|a", "A", false), at("S|f#closure1", "S", true), at("B|b", "B", false)
		base := []*group.Entry{a, s, b}
		if !sFirst {
			base = []*group.Entry{a, b, s}
		}
		head := []*group.Entry{at("A|a", "A", false), at("S|f#closure1", "S", true), at("C|b", "C", false)}
		if r := Judge(base, head, o); len(r.Violations) != 0 {
			t.Errorf("移しただけで違反になった (s が先=%v): %+v", sFirst, r)
		}
	}
}

func TestL3TrivialOriginCountsSeparately(t *testing.T) {
	// base で L3 だけ比べなかった (L3Trivial) 単位 t を、移して直したら L3 で a と同じ形になった: L3 の新しい写し (報告だけ)。
	tb := e("t", "q", "qy", "z")
	tb.Result.L3Trivial = true
	base := []*group.Entry{e("a", "x", "y", "z"), tb}
	head := []*group.Entry{e("a", "x", "y", "z"), e("t2", "q2", "qy2", "z")}
	r := Judge(base, head, opt)
	if len(r.Violations) != 1 || r.Violations[0].Level != norm.L3 || r.Failed() {
		t.Errorf("L3 で比べていなかった単位が組に入ったのを報告しない: %+v", r)
	}
}

func TestMovedCopyNamingDoesNotDependOnOrder(t *testing.T) {
	// 写し b を別のファイル Z へ移し (名前はそのまま)、新しいファイル A に新しい写し n を足した: 並びに依らず新しいのは n。
	at := func(key, file, name string) *group.Entry {
		x := e(key, "x", "x2", "x3")
		x.Unit.File, x.Unit.Name = file, name
		return x
	}
	base := []*group.Entry{at("X|a", "X", "a"), at("B|b", "B", "b")}
	for _, nFirst := range []bool{true, false} {
		n, z := at("A|n", "A", "n"), at("Z|b", "Z", "b")
		head := []*group.Entry{at("X|a", "X", "a"), n, z}
		if !nFirst {
			head = []*group.Entry{at("X|a", "X", "a"), z, n}
		}
		r := Judge(base, head, opt)
		if len(r.Violations) != 1 || len(r.Violations[0].New) != 1 || r.Violations[0].New[0].Unit.Key != "A|n" {
			t.Errorf("移した写しを新しいと名指しした (n が先=%v): %+v", nFirst, keys(r.Violations[0].New))
		}
	}
}

func TestAllowedUnitIsNotNamedButDoesNotHideNewCopy(t *testing.T) {
	allow := func(es ...*group.Entry) *exclude.Set {
		var rules []exclude.Rule
		for _, x := range es {
			rules = append(rules, exclude.Rule{Unit: exclude.Pin(x), Kind: "intentional", Reason: "r"})
		}
		s := exclude.New(&exclude.Config{Version: 1, Exclude: rules}, es, nil)
		return s
	}
	// 許可した単位 n は寄せる側に出ず、寄せる側が空になった違反は出ない。
	base := []*group.Entry{e("a", "x", "x2", "x3")}
	n := e("n", "x", "x2", "x3")
	o := opt
	o.Exclude = allow(n)
	if r := Judge(base, []*group.Entry{e("a", "x", "x2", "x3"), n}, o); len(r.Violations) != 0 {
		t.Errorf("許可した単位が違反に出た: %+v", r)
	}
	// 古い単位 a に許可を付けても、新しい写し n の違反は消えない (数には入れたまま)。
	a := e("a", "x", "x2", "x3")
	o.Exclude = allow(a)
	r := Judge(base, []*group.Entry{a, e("n", "x", "x2", "x3")}, o)
	if len(r.Violations) != 1 || keys(r.Violations[0].New)[0] != "n" {
		t.Errorf("古い単位の許可で新しい写しの違反が消えた: %+v", r)
	}
}

func pathSet(t *testing.T, glob string, es ...*group.Entry) *exclude.Set {
	t.Helper()
	c, err := exclude.Parse([]byte(`{"version":1,"exclude":[{"paths":["` + glob + `"],"kind":"false-positive","reason":"r"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return exclude.New(c, es, nil)
}

func inFile(x *group.Entry, file string) *group.Entry { x.Unit.File = file; return x }

func TestExcludedUnitDoesNotShieldOutsideCopies(t *testing.T) {
	// 生成コード G と手書きの写しの組 (b1, b2) を、3 つとも同じ形に書き換えた: 手書きの側が生成コードの写しになった。
	// 除外の下の G を寄せる側に選んでから外すと、外の写しが名指しされずに違反ごと消える。
	g := func() *group.Entry { return inFile(e("g", "A", "A2", "A3"), "Gen/G.swift") }
	base := []*group.Entry{g(), e("b1", "B", "B2", "B3"), e("b2", "B", "B2", "B3")}
	head := []*group.Entry{inFile(e("g", "C", "C2", "C3"), "Gen/G.swift"), e("b1", "C", "C2", "C3"), e("b2", "C", "C2", "C3")}
	o := opt
	o.Exclude = pathSet(t, "Gen/**", head...)
	r := Judge(base, head, o)
	if !r.Failed() || len(r.Violations) != 1 || len(r.Violations[0].New) != 2 {
		t.Errorf("外の写し b1・b2 を寄せる側に期待: %+v", r)
	}
	// 同じ数の出どころでも、ハッシュの並びに依らず外の単位を名指しする。
	for _, hs := range [][2]string{{"a", "b"}, {"b", "a"}} {
		base := []*group.Entry{inFile(e("g", hs[0], hs[0]+"2", hs[0]+"3"), "Gen/G.swift"), e("s", hs[1], hs[1]+"2", hs[1]+"3")}
		head := []*group.Entry{inFile(e("g", "c", "c2", "c3"), "Gen/G.swift"), e("s", "c", "c2", "c3")}
		o.Exclude = pathSet(t, "Gen/**", head...)
		if r := Judge(base, head, o); !r.Failed() || keys(r.Violations[0].New)[0] != "s" {
			t.Errorf("ハッシュの並び %v で外の単位 s を名指ししない: %+v", hs, r)
		}
	}
}

func TestPathPassedCountsOnlyRealCopies(t *testing.T) {
	// 除外の下の写しの組に同じ修正を入れた (作り直し)・片方を改名した: 除外が無くても違反ではないので、通した数に入れない。
	gen := func(key, h string) *group.Entry { return inFile(e(key, h, h+"2", h+"3"), "Gen/"+key+".swift") }
	base := []*group.Entry{gen("g1", "x"), gen("g2", "x")}
	for name, head := range map[string][]*group.Entry{
		"同じ修正": {gen("g1", "y"), gen("g2", "y")},
		"改名":   {gen("g1", "x"), inFile(e("g3", "x", "x2", "x3"), "Gen/g2.swift")},
	} {
		o := opt
		o.Exclude = pathSet(t, "Gen/**", head...)
		if r := Judge(base, head, o); len(r.PathPassed) != 0 || len(r.Violations) != 0 {
			t.Errorf("%s: 通した数に入った: passed=%d %+v", name, len(r.PathPassed), r)
		}
	}
	// 除外の下に新しい写しを足したら数える。
	head := []*group.Entry{gen("g1", "x"), gen("g2", "x"), gen("g9", "x")}
	o := opt
	o.Exclude = pathSet(t, "Gen/**", head...)
	if r := Judge(base, head, o); len(r.PathPassed) != 1 || keys(r.PathPassed)[0] != "g9" {
		t.Errorf("除外の下の新しい写しを数えない: %+v", r)
	}
}

func TestAllowedNewCopyDoesNotBlameSameEditedCopies(t *testing.T) {
	// 写しの組 a・b に同じ修正を入れ、意図した写し n (許可) を足した: 修正しただけの a・b を寄せる側にしない。
	base := []*group.Entry{e("a", "x", "x2", "x3"), e("b", "x", "x2", "x3")}
	n := e("n", "y", "y2", "y3")
	head := []*group.Entry{e("a", "y", "y2", "y3"), e("b", "y", "y2", "y3"), n}
	o := opt
	o.Exclude = exclude.New(&exclude.Config{Version: 1, Exclude: []exclude.Rule{{Unit: exclude.Pin(n), Kind: "intentional", Reason: "r"}}}, head, nil)
	if r := Judge(base, head, o); len(r.Violations) != 0 {
		t.Errorf("許可した写しの代わりに修正しただけの写しを止めた: %+v", r)
	}
}

func TestOutsideCopyIsNotBlamedForNewExcludedCopy(t *testing.T) {
	// 生成コード g と手書きの h は前から写しの組。生成の新しい写し g2 が増え、h は移しただけ・同じ修正を入れただけ:
	// h を寄せる側にせず、g2 を除外で通した数に入れる。
	gen := func(key, h string) *group.Entry { return inFile(e(key, h, h+"2", h+"3"), "Gen/"+key+".swift") }
	hb := e("h", "x", "x2", "x3")
	hb.Unit.Name = "h"
	base := []*group.Entry{gen("g", "x"), hb}
	for name, head := range map[string][]*group.Entry{
		"移した":  {gen("g", "x"), gen("g2", "x"), inFile(e("h2", "x", "x2", "x3"), "h2")},
		"同じ修正": {gen("g", "y"), gen("g2", "y"), e("h", "y", "y2", "y3")},
	} {
		head[2].Unit.Name = "h"
		o := opt
		o.Exclude = pathSet(t, "Gen/**", head...)
		r := Judge(base, head, o)
		if r.Failed() || len(r.PathPassed) != 1 || keys(r.PathPassed)[0] != "g2" {
			t.Errorf("%s: 外の h を止めた・g2 を数えない: passed=%v %+v", name, keys(r.PathPassed), r)
		}
	}
}

func TestRenumberedClosureIsNotNamed(t *testing.T) {
	// 写しの組 b1・b2 をクロージャ K と同じ形に書き換え、同時に K の番号がずれた (本体は同じ): 寄せる側は b1・b2。
	// K に許可が付いていても、違反は消えない。
	base := []*group.Entry{cl("V|body#closure1", "k", "k2", "k3"), e("b1", "b", "b2", "b3"), e("b2", "b", "b2", "b3")}
	head := []*group.Entry{cl("V|body#closure1", "z", "z2", "z3"), cl("V|body#closure2", "k", "k2", "k3"), e("b1", "k", "k2", "k3"), e("b2", "k", "k2", "k3")}
	for _, allow := range []bool{false, true} {
		o := opt
		if allow {
			o.Exclude = exclude.New(&exclude.Config{Version: 1, Exclude: []exclude.Rule{{Unit: exclude.Pin(head[1]), Kind: "intentional", Reason: "r"}}}, head, nil)
		}
		r := Judge(base, head, o)
		if !r.Failed() || len(r.Violations) != 1 || strings.Join(keys(r.Violations[0].New), ",") != "b1,b2" {
			t.Errorf("許可=%v: 番号がずれただけの K を名指しした・違反が消えた: %+v", allow, r)
		}
	}
}

func TestOutsideCopyOfNewExcludedUnitIsViolation(t *testing.T) {
	// 生成の新しい単位 F と同じ commit で、F の写しの外の単位を足した・外の単位 s を F の形に書き換えた: 外の単位は止める。
	f := func() *group.Entry { return inFile(e("F", "c", "c2", "c3"), "Gen/F.swift") }
	for name, c := range map[string]struct{ base, head []*group.Entry }{
		"新しい外の写し":    {[]*group.Entry{e("other", "o", "o2", "o3")}, []*group.Entry{e("other", "o", "o2", "o3"), f(), e("N", "c", "c2", "c3")}},
		"外の単位を書き換えた": {[]*group.Entry{e("s", "k", "k2", "k3")}, []*group.Entry{f(), e("s", "c", "c2", "c3")}},
	} {
		o := opt
		o.Exclude = pathSet(t, "Gen/**", c.head...)
		r := Judge(c.base, c.head, o)
		if !r.Failed() || len(r.Violations) != 1 || len(r.Violations[0].New) != 1 || strings.HasPrefix(r.Violations[0].New[0].Unit.File, "Gen/") {
			t.Errorf("%s: 外の単位を止めない: %+v", name, r)
		}
	}
}

func TestMovedRenamedExcludedUnitDoesNotDependOnOrder(t *testing.T) {
	// 生成の a を同じディレクトリの b へ改名して移し、外に新しい写し c を足した: 並びに依らず c を止める。
	base := []*group.Entry{inFile(e("Gen/a|foo", "x", "x2", "x3"), "Gen/a.swift"), inFile(e("App/h|h", "x", "x2", "x3"), "App/h.swift")}
	for _, gFirst := range []bool{true, false} {
		h, g, c := inFile(e("App/h|h", "x", "x2", "x3"), "App/h.swift"), inFile(e("Gen/b|bar", "x", "x2", "x3"), "Gen/b.swift"), inFile(e("App/c|baz", "x", "x2", "x3"), "App/c.swift")
		head := []*group.Entry{h, c, g}
		if gFirst {
			head = []*group.Entry{h, g, c}
		}
		o := opt
		o.Exclude = pathSet(t, "Gen/**", head...)
		if r := Judge(base, head, o); !r.Failed() || keys(r.Violations[0].New)[0] != "App/c|baz" {
			t.Errorf("生成が先=%v: 外の新しい写し c を止めない: %+v", gFirst, r)
		}
	}
}
