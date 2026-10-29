package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 型 name に、methods の名前のメソッドを持たせた Go のファイル。field はメソッドが触る slice の名前。
func goTypeWith(name, field string, methods ...string) string {
	bodies := map[string]string{
		"Write":   "func (b *@T) Write(p []byte) (int, error) {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\tb.@F = append(b.@F, p...)\n\treturn len(p), nil\n}\n",
		"String":  "func (b *@T) String() string {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\treturn string(b.@F) + \"/\" + string(rune(len(b.@F)))\n}\n",
		"Other":   "func (b *@T) String() string {\n\tif len(b.@F) == 0 {\n\t\treturn \"empty\"\n\t}\n\treturn string(b.@F[:1])\n}\n",
		"Reset":   "func (b *@T) Reset(n int) {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\tif n > len(b.@F) {\n\t\tn = len(b.@F)\n\t}\n\tb.@F = b.@F[:n]\n}\n",
		"Head":    "func (b *@T) Head(n int) []byte {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\tif n >= len(b.@F) {\n\t\tn = len(b.@F) - 1\n\t}\n\treturn append([]byte{}, b.@F[:n]...)\n}\n",
		"Put":     "func (b *@T) Put(p []byte) (int, error) {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\tb.@F = append(b.@F, p...)\n\treturn len(p), nil\n}\n",
		"String2": "func (b *@T) String() string {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\treturn string(b.@F) + \"-\" + string(rune(len(b.@F)))\n}\n",
		"Head2":   "func (b *@T) Head(n int) []byte {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\tif n >= len(b.@F) {\n\t\tn = len(b.@F) - 2\n\t}\n\treturn append([]byte{}, b.@F[:n]...)\n}\n",
		"Count":   "func (b *@T) Count(x byte) (n int) {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\tfor i, c := range b.@F {\n\t\tif c == x && i%2 == 0 {\n\t\t\tn += i + 1\n\t\t} else if c == x {\n\t\t\tn -= i\n\t\t}\n\t}\n\treturn n\n}\n",
		"Tail":    "func (b *@T) Tail(n int) []byte {\n\tb.mu.Lock()\n\tdefer b.mu.Unlock()\n\tif n > len(b.@F) {\n\t\treturn b.@F\n\t}\n\treturn b.@F[len(b.@F)-n:]\n}\n",
	}
	src := "package a\n\ntype @T struct {\n\tmu sync.Mutex\n\t@F []byte\n}\n\n"
	if strings.HasPrefix(name, "+") {
		name, src = name[1:], "package a\n\n"
	}
	for _, m := range methods {
		src += bodies[m] + "\n"
	}
	return strings.ReplaceAll(strings.ReplaceAll(src, "@T", name), "@F", field)
}

func sweepTypeCopyHeads(t *testing.T, root string) (string, []string) {
	t.Helper()
	rc, out, errs := sweep(t, "-limit", "0", root)
	if rc != 0 {
		t.Fatalf("sweep: rc=%d err=%q", rc, errs)
	}
	s := sweepSection(t, out, 6)
	var heads []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "型 ×") {
			heads = append(heads, line)
		}
	}
	return s, heads
}

// 同じメソッドの写しを持つ 4 つの型は、型の対 6 つではなく 1 つの候補になる。
func TestSweepTypeCopiesGroupsTypesSharingMembers(t *testing.T) {
	files := map[string]string{}
	for _, n := range []string{"Aa", "Bb", "Cc", "Dd"} {
		files["a/"+strings.ToLower(n)+".go"] = goTypeWith(n, "buf", "Write", "String")
	}
	root := repoWith(t, files)
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 1 || !strings.HasPrefix(heads[0], "型 ×4 ") {
		t.Fatalf("4 つの型を 1 つの候補に: %q", s)
	}
	if want := fmt.Sprintf("型 ×4 節 %d", groupWeights(t, root)); heads[0] != want {
		t.Errorf("重みは組ごとの 節 × (人数 − 1) の和 (%s): %q", want, s)
	}
	if a, d := strings.Index(s, "Aa ("), strings.Index(s, "Dd ("); a < 0 || a > d {
		t.Errorf("候補の中の型はファイルの順: %q", s)
	}
	for _, n := range []string{"Aa (String, Write)", "Bb (String, Write)", "Cc (String, Write)", "Dd (String, Write)"} {
		if !strings.Contains(s, n) {
			t.Errorf("型と同じ形のメンバーを出す (%s): %q", n, s)
		}
	}
}

// 同じ形のメンバーが 1 つだけの型の対は、節 1・2 の組 1 つで足りるので出さない。
func TestSweepTypeCopiesNeedTwoMembersOnEachSide(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "Other"),
	})
	if s, heads := sweepTypeCopyHeads(t, root); len(heads) != 0 {
		t.Errorf("1 つだけ同じ型の対は出さない: %q", s)
	}
}

// 別々の写しで 1 つの型を共有する型どうしは 1 つの候補につなぎ、候補の中の型には、その候補で相手と同じ形のメンバーを出す。
func TestSweepTypeCopiesChainThroughASharedType(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String", "Reset", "Tail"),
		"a/cc.go": goTypeWith("Cc", "buf", "Reset", "Tail"),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if want := fmt.Sprintf("型 ×3 節 %d", groupWeights(t, root)); len(heads) != 1 || heads[0] != want {
		t.Fatalf("つながる 3 つの型を 1 つの候補に、重み %s で: %q", want, s)
	}
	for _, n := range []string{"Aa (String, Write)", "Bb (Reset, String, Tail, Write)", "Cc (Reset, Tail)"} {
		if !strings.Contains(s, n) {
			t.Errorf("型ごとに相手と同じ形のメンバーを出す (%s): %q", n, s)
		}
	}
}

// 2 つの「全員が互いに写し」の集まりが型の対を共有しても、型を 2 つの候補に入れず、同じ組を 2 度数えない。
func TestSweepTypeCopiesCountASharedPairOnce(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "Reset", "Tail", "Head"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "Reset", "Tail", "Head"),
		"a/cc.go": goTypeWith("Cc", "buf", "Write", "Tail"),
		"a/dd.go": goTypeWith("Dd", "buf", "Reset", "Head"),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if want := fmt.Sprintf("型 ×4 節 %d", groupWeights(t, root)); len(heads) != 1 || heads[0] != want {
		t.Errorf("1 つの候補・重み %s: %q", want, s)
	}
}

// メンバー名だけ違う写し (L1m) の組は束ねない。同じ組の中の名前違いのメソッドが全部対になり、候補を写しでないもので埋める。
func TestSweepTypeCopiesSkipL1mGroups(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String"),
		"a/bb.go": goTypeWith("Bb", "data", "Write", "String"),
	})
	rc, out, _ := sweep(t, "-limit", "0", root)
	if rc != 0 || !strings.Contains(sweepSection(t, out, 2), "[L1m]") {
		t.Fatalf("前提: 節 2 に L1m の組が出る: %q", out)
	}
	if s, heads := sweepTypeCopyHeads(t, root); len(heads) != 0 {
		t.Errorf("L1m だけで一致する型は出さない: %q", s)
	}
}

// 型のメンバーの中の同じ形のクロージャは束ねない。大きいクロージャの組 1 つが、型の数の掛け算で候補を増やす。
// Swift で書く (Go のクロージャは囲む型を持たないので、この条件が無くても束ねられない)。
func TestSweepTypeCopiesSkipClosures(t *testing.T) {
	closureType := func(name, extra string) string {
		body := "    func @N(_ k: Int) -> () -> Int {\n        @X\n        return { [weak self] in\n            guard let self else { return 0 }\n            if self.items.count > k { return k * @K }\n            return self.items.count + k\n        }\n    }\n"
		src := "final class @T {\n    var items: [Int] = []\n"
		// 2 つのクロージャは形が違う (`k * 2` と `k * k - 1`): 型の対を結ぶ組が 2 つになる
		for n, k := range map[string]string{"first": "2", "second": "k - 1"} {
			src += strings.ReplaceAll(strings.ReplaceAll(body, "@N", n), "@K", k)
		}
		return strings.ReplaceAll(strings.ReplaceAll(src+"}\n", "@T", name), "@X", extra)
	}
	root := repoWith(t, map[string]string{
		"Sources/A.swift": closureType("Aa", "print(k)"),
		"Sources/B.swift": closureType("Bb", "debugPrint(k, k + 1)"),
	})
	rc, out, _ := sweep(t, "-limit", "0", root)
	if rc != 0 || !strings.Contains(out, "{closure} in first") || !strings.Contains(out, "{closure} in second") {
		t.Fatalf("前提: クロージャの組が節 1・2 に出る: %q", out)
	}
	if s, heads := sweepTypeCopyHeads(t, root); len(heads) != 0 {
		t.Errorf("クロージャだけで一致する型は出さない: %q", s)
	}
}

// 候補は型 1 つ増えるごとに減る節の大きい順: つながった 3 つの型 (和は大きい) より、2 メソッドを丸写しした 2 つの型が先。
func TestSweepTypeCopiesRankByWeightPerType(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String", "Reset", "Tail"),
		"a/cc.go": goTypeWith("Cc", "buf", "Reset", "Tail"),
		"a/dd.go": goTypeWith("Dd", "buf", "Head", "Count"),
		"a/ee.go": goTypeWith("Ee", "buf", "Head", "Count"),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 2 {
		t.Fatalf("前提: 2 つの候補: %q", s)
	}
	weight := map[string]int{}
	for _, h := range heads {
		if f := strings.Fields(h); len(f) == 4 {
			weight[f[1]], _ = strconv.Atoi(f[3])
		}
	}
	w3, w2 := weight["×3"], weight["×2"]
	if w3 <= w2 || w2 <= w3/2 {
		t.Fatalf("前提: 3 つの型の和は大きく、型 1 つあたりでは 2 つの型が大きい (×3 %d・×2 %d): %q", w3, w2, s)
	}
	if !strings.HasPrefix(heads[0], "型 ×2 ") {
		t.Errorf("型 1 つあたりの大きい ×2 を先に: %q", s)
	}
}

var sweepGroupHead = regexp.MustCompile(`^\[(L1|L2)\] \S+ ×(\d+) 節 (\d+)$`)

// groupWeights は、節 1・2 に出た組 (L1m を除く) の 節 × (人数 − 1) の和。全部の組が 1 つの候補に入る fixture で、候補の重みと比べる。
func groupWeights(t *testing.T, root string) int {
	t.Helper()
	_, out, _ := sweep(t, "-limit", "0", root)
	sum := 0
	for _, n := range []int{1, 2} {
		for _, line := range strings.Split(sweepSection(t, out, n), "\n") {
			if m := sweepGroupHead.FindStringSubmatch(line); m != nil {
				k, _ := strconv.Atoi(m[2])
				nodes, _ := strconv.Atoi(m[3])
				sum += nodes * (k - 1)
			}
		}
	}
	if sum == 0 {
		t.Fatalf("前提: 節 1・2 に組が出る: %q", out)
	}
	return sum
}

// 1 つの対だけが余分なメソッドを共有しても、全員が互いに写しなら 1 つの候補にし、同じ対・同じ組を 2 つの候補に重ねない。
func TestSweepTypeCopiesMergeTypesThatAllCopyEachOther(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String", "Reset"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String", "Reset"),
		"a/cc.go": goTypeWith("Cc", "buf", "Write", "String"),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if want := fmt.Sprintf("型 ×3 節 %d", groupWeights(t, root)); len(heads) != 1 || heads[0] != want {
		t.Fatalf("3 つの型を 1 つの候補に、重み %s で: %q", want, s)
	}
	if !strings.Contains(s, "Aa (Reset, String, Write)") || !strings.Contains(s, "Cc (String, Write)") {
		t.Errorf("型ごとに同じ形のメンバーを出す: %q", s)
	}
}

// 同じメンバーが L1 の組と、それを含む L2 の組の両方に入っても、重みを 2 度数えず、候補も 1 つ。
func TestSweepTypeCopiesCountAGroupInsideAnotherOnce(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "String", "Head"),
		"a/bb.go": goTypeWith("Bb", "buf", "String", "Head"),
		"a/cc.go": goTypeWith("Cc", "buf", "String2", "Head2"),
	})
	_, out, _ := sweep(t, "-limit", "0", root)
	if sec := sweepSection(t, out, 1); !strings.Contains(sec, "[L1]") || !strings.Contains(sec, "[L2]") {
		t.Fatalf("前提: 節 1 に L1 の組と、それを含む L2 の組が出る: %q", out)
	}
	s, heads := sweepTypeCopyHeads(t, root)
	// L2 の組 (3 人) だけを数える: Head と String の L2 の組の 節 × 2
	l2 := 0
	for _, line := range strings.Split(sweepSection(t, out, 1), "\n") {
		if m := sweepGroupHead.FindStringSubmatch(line); m != nil && m[1] == "L2" {
			nodes, _ := strconv.Atoi(m[3])
			l2 += nodes * 2
		}
	}
	if want := fmt.Sprintf("型 ×3 節 %d", l2); len(heads) != 1 || heads[0] != want {
		t.Errorf("1 つの候補・重み %s: %q", want, s)
	}
}

// 片側だけが 2 つ以上でも出さない: Bb は Aa の Write と Put の両方と同じ形だが、Bb のメンバーは 1 つ。
func TestSweepTypeCopiesNeedTwoMembersOnBothSides(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "Put"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write"),
	})
	if s, heads := sweepTypeCopyHeads(t, root); len(heads) != 0 {
		t.Errorf("片側だけ 2 つ以上の型の対は出さない: %q", s)
	}
}

// 候補の型のメンバーでも、候補の外の型とだけ同じ形のものは、候補に出さず重みにも入れない。
func TestSweepTypeCopiesIgnoreCopiesWithTypesOutsideTheCandidate(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String", "Reset"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String"),
		"a/cc.go": goTypeWith("Cc", "buf", "Reset"),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 1 || !strings.Contains(s, "Aa (String, Write)") {
		t.Errorf("Cc とだけ同じ形の Reset は Aa~Bb の候補に出さない: %q", s)
	}
}

// 下限は組の数でなくメンバーの数で見る: 名前違いの同じ本体の 2 メソッドを写した型 (組は 1 つ) も型の写し。
func TestSweepTypeCopiesCountMembersNotGroups(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "Put"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "Put"),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 1 || !strings.Contains(s, "Aa (Put, Write)") || !strings.Contains(s, "Bb (Put, Write)") {
		t.Errorf("組 1 つにメンバーが 2 つずつ入る型の対も出す: %q", s)
	}
}

// 別の dir の同名の型は別の型として比べる。
func TestSweepTypeCopiesSameNameInAnotherDirIsAnotherType(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/buf.go": goTypeWith("Buf", "buf", "Write", "String"),
		"b/buf.go": strings.Replace(goTypeWith("Buf", "buf", "Write", "String"), "package a", "package b", 1),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 1 || !strings.Contains(s, "a/buf.go:") || !strings.Contains(s, "b/buf.go:") {
		t.Errorf("a/Buf と b/Buf を 1 つの候補に: %q", s)
	}
}

// 同じ重みの候補は、型の場所の順で決める (map の順で揺れない)。
func TestSweepTypeCopiesBreakTiesByPlace(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String"),
		"a/cc.go": goTypeWith("Cc", "data", "Write", "String"),
		"a/dd.go": goTypeWith("Dd", "data", "Write", "String"),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 2 || heads[0] != heads[1] {
		t.Fatalf("前提: 同じ重みの候補が 2 つ: %q", s)
	}
	if strings.Index(s, "Aa (") > strings.Index(s, "Cc (") {
		t.Errorf("同じ重みなら場所の早い候補が先: %q", s)
	}
}

// 1 つの型のメソッドが package の別のファイルに分かれていても、1 つの型として数える。
func TestSweepTypeCopiesJoinMethodsAcrossFilesOfAPackage(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go":  goTypeWith("Aa", "buf", "String"),
		"a/aa2.go": goTypeWith("+Aa", "buf", "Write"),
		"a/bb.go":  goTypeWith("Bb", "buf", "Write", "String"),
	})
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 1 || !strings.Contains(s, "Aa (String, Write)") {
		t.Errorf("別のファイルのメソッドも Aa のメンバーとして数える: %q", s)
	}
	// 行の場所は、ファイルと行の順で最初のメンバー (組の順ではない。節の大きい String の組が先に並んでも、Bb は 8 行目の Write)
	if !strings.Contains(s, "a/aa.go:8 Aa (") || !strings.Contains(s, "a/bb.go:8 Bb (") {
		t.Errorf("型の行は最初のメンバーの場所: %q", s)
	}
}

// 1 つの Swift の型の中 (本体と、同じ dir の extension) の写しは、型の写しとして出さない。
func TestSweepTypeCopiesSkipCopiesInsideOneType(t *testing.T) {
	sum := func(name string) string {
		return "    func " + name + "(_ xs: [Int], _ limit: Int) -> Int {\n        var total = 0\n        for x in xs where x > 0 && total < limit {\n            total += x\n        }\n        return min(total, limit)\n    }\n"
	}
	render := func(name string) string {
		return "    func " + name + "(_ s: String, _ n: Int) -> String {\n        let head = s.prefix(n)\n        let tail = s.suffix(n)\n        return String(head) + \":\" + String(tail) + String(n)\n    }\n"
	}
	root := repoWith(t, map[string]string{
		"Sources/Foo.swift":      "final class Foo {\n" + sum("first") + render("render") + "}\n",
		"Sources/Foo+Copy.swift": "extension Foo {\n" + sum("second") + render("render2") + "}\n",
	})
	rc, out, _ := sweep(t, "-limit", "0", root)
	if rc != 0 || !strings.Contains(out, "first") || !strings.Contains(out, "render2") {
		t.Fatalf("前提: 節 1・2 に写しの組が出る: %q", out)
	}
	if s, heads := sweepTypeCopyHeads(t, root); len(heads) != 0 {
		t.Errorf("1 つの型の中の写しは出さない: %q", s)
	}
}

// sweepBigGroupMin 個以上が同じ形の群 (決まった形の羅列) は束ねない。
func TestSweepTypeCopiesSkipBigGroups(t *testing.T) {
	files := map[string]string{}
	for i := range sweepBigGroupMin {
		n := fmt.Sprintf("T%02d", i)
		files["a/"+strings.ToLower(n)+".go"] = goTypeWith(n, "buf", "Write", "String")
	}
	root := repoWith(t, files)
	_, out, _ := sweep(t, "-limit", "0", root)
	if !strings.Contains(out, fmt.Sprintf("×%d", sweepBigGroupMin)) {
		t.Fatalf("前提: %d 人の組が出る: %q", sweepBigGroupMin, out)
	}
	if s, heads := sweepTypeCopyHeads(t, root); len(heads) != 0 {
		t.Errorf("%d 人以上の群は束ねない: %q", sweepBigGroupMin, s)
	}
}

// 20 人以上の組を除いても、その中の小さい組 (L2 の群の中の L1 の組) は束ねる。
func TestSweepTypeCopiesBindSmallerGroupsInsideBigOnes(t *testing.T) {
	files := map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String"),
	}
	for i := range sweepBigGroupMin {
		n := fmt.Sprintf("T%02d", i)
		files["a/"+strings.ToLower(n)+".go"] = goTypeWith(n, "buf", "String2")
	}
	root := repoWith(t, files)
	_, out, _ := sweep(t, "-limit", "0", root)
	if !strings.Contains(sweepSection(t, out, 1), fmt.Sprintf("[L2] prod ×%d", sweepBigGroupMin+2)) {
		t.Fatalf("前提: String と String2 の %d 人の L2 の群が出る: %q", sweepBigGroupMin+2, out)
	}
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 1 || !strings.Contains(s, "Aa (String, Write)") || !strings.Contains(s, "Bb (String, Write)") {
		t.Errorf("Aa~Bb の丸写しを、String が大きい群に入っても出す: %q", s)
	}
}

// 重みは候補の中の型だけで数える: Write の組に入る Cc (Aa・Bb と写しの対にならない) は数えない。
func TestSweepTypeCopiesWeightIgnoresTypesOutsideTheCandidate(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String"),
		"a/cc.go": goTypeWith("Cc", "buf", "Write", "Other"),
	})
	_, out, _ := sweep(t, "-limit", "0", root)
	want := 0
	for _, line := range strings.Split(sweepSection(t, out, 1)+sweepSection(t, out, 2), "\n") {
		if m := sweepGroupHead.FindStringSubmatch(line); m != nil {
			nodes, _ := strconv.Atoi(m[3])
			want += nodes
		}
	}
	if !strings.Contains(out, "×3 節") {
		t.Fatalf("前提: Write の組に Cc も入る: %q", out)
	}
	s, heads := sweepTypeCopyHeads(t, root)
	if w := fmt.Sprintf("型 ×2 節 %d", want); len(heads) != 1 || heads[0] != w {
		t.Errorf("組ごとに 節 × (候補の中の型の数 − 1) (%s): %q", w, s)
	}
}

// 重みは型の数で数える: 1 つの型の中の同じ形のメンバー (Write と Put) は、型を寄せても減らないので重みに入れない。
func TestSweepTypeCopiesWeightCountsTypesNotMembers(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "Put", "String"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String"),
	})
	_, out, _ := sweep(t, "-limit", "0", root)
	want := 0
	for _, line := range strings.Split(sweepSection(t, out, 1)+sweepSection(t, out, 2), "\n") {
		if m := sweepGroupHead.FindStringSubmatch(line); m != nil {
			nodes, _ := strconv.Atoi(m[3])
			want += nodes
		}
	}
	s, heads := sweepTypeCopyHeads(t, root)
	if w := fmt.Sprintf("型 ×2 節 %d", want); len(heads) != 1 || heads[0] != w {
		t.Errorf("組ごとに 節 × (型の数 − 1) (%s): %q", w, s)
	}
}

// 型のメンバーだけで見て同じ集合になる組 (トップレベルの関数を含む L1 の組と、それを含む L2 の組) は 1 度だけ数える。
func TestSweepTypeCopiesCountGroupsWithTheSameTypedMembersOnce(t *testing.T) {
	sum := func(start string) string {
		return "func sum(_ xs: [Int], _ limit: Int) -> Int {\n    var total = " + start + "\n    for x in xs where x > 0 && total < limit {\n        total += x\n    }\n    return min(total, limit)\n}\n"
	}
	typ := func(name string) string {
		write := "    func write(_ p: [UInt8]) -> Int {\n        lock.lock()\n        defer { lock.unlock() }\n        buf.append(contentsOf: p)\n        return p.count + buf.count\n    }\n"
		return "final class " + name + " {\n    let lock = NSLock()\n    var buf: [UInt8] = []\n" + write + strings.ReplaceAll(sum("0"), "\n", "\n    ") + "}\n"
	}
	root := repoWith(t, map[string]string{
		"Sources/A/Aa.swift": typ("Aa"), "Sources/A/Bb.swift": typ("Bb"),
		"Sources/C/X.swift": sum("0"), "Sources/D/Y.swift": sum("1"),
	})
	_, out, _ := sweep(t, "-limit", "0", root)
	nodes := map[string]int{}
	for _, line := range strings.Split(sweepSection(t, out, 1)+sweepSection(t, out, 2), "\n") {
		if m := sweepGroupHead.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[3])
			nodes[m[1]+"×"+m[2]] = n
		}
	}
	if nodes["L1×3"] == 0 || nodes["L2×4"] == 0 || nodes["L1×2"] == 0 {
		t.Fatalf("前提: write の L1 の組と、sum の L1 (3 人) と L2 (4 人) の組が出る: %q", out)
	}
	s, heads := sweepTypeCopyHeads(t, root)
	if w := fmt.Sprintf("型 ×2 節 %d", nodes["L1×2"]+nodes["L2×4"]); len(heads) != 1 || heads[0] != w {
		t.Errorf("sum を 1 度だけ数える (%s): %q", w, s)
	}
}

// トップレベルの関数 (囲む型の無い単位) は束ねない。
func TestSweepTypeCopiesSkipTopLevelFunctions(t *testing.T) {
	src := "package a\n\n" + strings.TrimPrefix(goNearPair, "package a\n\n")
	root := repoWith(t, map[string]string{"a/x.go": src, "b/y.go": strings.Replace(src, "package a", "package b", 1)})
	rc, out, _ := sweep(t, "-limit", "0", root)
	if rc != 0 || !strings.Contains(sweepSection(t, out, 1)+sweepSection(t, out, 2), "×2") {
		t.Fatalf("前提: トップレベルの関数の組が出る: %q", out)
	}
	if s, heads := sweepTypeCopyHeads(t, root); len(heads) != 0 {
		t.Errorf("トップレベルの関数は束ねない: %q", s)
	}
}

// overload は名前に数を添える。行の場所は最初のメンバー。
func TestSweepTypeCopiesShowOverloadCountAndFirstMember(t *testing.T) {
	typ := func(name string) string {
		f := func(param string) string {
			return "    func render(_ x: " + param + ", _ n: Int) -> String {\n        let head = String(describing: x).prefix(n)\n        let tail = String(describing: x).suffix(n)\n        return String(head) + \":\" + String(tail) + String(n)\n    }\n"
		}
		return "final class " + name + " {\n" + f("Int") + f("String") + "}\n"
	}
	root := repoWith(t, map[string]string{"Sources/A.swift": typ("Aa"), "Sources/B.swift": typ("Bb")})
	s, heads := sweepTypeCopyHeads(t, root)
	if len(heads) != 1 || !strings.Contains(s, "Sources/A.swift:2 Aa (render ×2)") {
		t.Errorf("overload の数と、最初のメンバーの行を出す: %q", s)
	}
}

// 節 6 も -limit で畳む。
func TestSweepTypeCopiesHonorLimit(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/aa.go": goTypeWith("Aa", "buf", "Write", "String"),
		"a/bb.go": goTypeWith("Bb", "buf", "Write", "String"),
		"a/cc.go": goTypeWith("Cc", "buf", "Reset", "Tail"),
		"a/dd.go": goTypeWith("Dd", "buf", "Reset", "Tail"),
	})
	_, out, _ := sweep(t, "-limit", "1", root)
	if s := sweepSection(t, out, 6); strings.Count(s, "型 ×") != 1 || !strings.Contains(s, "… 他 1 件") {
		t.Errorf("-limit 1 で 1 件と「他 1 件」: %q", s)
	}
}
