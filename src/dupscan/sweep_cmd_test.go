package main

import (
	"fmt"
	"strings"
	"testing"
)

// sweep は 4 つの見方を節ごとに出す。節ごとの中身を、節の見出しで切って確かめる。

func sweepSection(t *testing.T, out string, n int) string {
	t.Helper()
	parts := strings.Split(out, "\n## ")
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, fmt.Sprintf("%d. ", n)) {
			return p
		}
	}
	t.Fatalf("節 %d が無い: %q", n, out)
	return ""
}

func sweep(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runReport(t, append([]string{"sweep"}, args...)...)
}

// 似ているが形は違う 2 関数 (片方に文が 1 つ多い)。どの段のハッシュも一致しないが、部分木は大半が共有される。
const goNearPair = `package a

func sumPositive(xs []int, limit int) int {
	total := 0
	for _, x := range xs {
		if x > 0 && total < limit {
			total += x
		}
	}
	if total > limit {
		total = limit
	}
	return total
}

func sumLogged(xs []int, limit int) int {
	total := 0
	for _, x := range xs {
		if x > 0 && total < limit {
			total += x
		}
	}
	if total > limit {
		total = limit
	}
	println(total)
	return total
}
`

func clusterFile(prefix string, n int) string {
	var b strings.Builder
	b.WriteString("package a\n\n")
	for i := range n {
		fmt.Fprintf(&b, "func %sThing%d(s string) bool { return len(s) > %d }\n", prefix, i, i)
	}
	return b.String()
}

func TestSweepL1mGroupAppearsOnlyInDeepSection(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": goTwoSinks})
	rc, out, errs := sweep(t, root)
	if rc != 0 {
		t.Fatalf("rc=%d %q", rc, errs)
	}
	if s := sweepSection(t, out, 1); !strings.Contains(s, "なし") {
		t.Errorf("L1m は既定の節に出ない: %q", s)
	}
	if s := sweepSection(t, out, 2); !strings.Contains(s, "[L1m]") || !strings.Contains(s, "captureOut") || !strings.Contains(s, "captureErr") {
		t.Errorf("下限を下げた節に L1m の組が出る: %q", s)
	}
}

func TestSweepSimilarPairsAndSkipOverCap(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": goNearPair})
	_, out, _ := sweep(t, "-similar-threshold", "0.5", root)
	if s := sweepSection(t, out, 3); !strings.Contains(s, "sumPositive") || !strings.Contains(s, "sumLogged") {
		t.Errorf("似ている対が出る: %q", s)
	}
	_, out, _ = sweep(t, "-similar-threshold", "0.5", "-similar-max-units", "1", root)
	if s := sweepSection(t, out, 3); !strings.Contains(s, "飛ばした") || !strings.Contains(s, "上限 1") || strings.Contains(s, "sumPositive") {
		t.Errorf("上限を超えたら比べず、飛ばしたと書く: %q", s)
	}
}

func TestSweepNameClusters(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/a.go":      clusterFile("check", 4),
		"b/b.go":      clusterFile("scan", 3),
		"c/c_test.go": strings.Replace(clusterFile("check", 5), "package a", "package c", 1),
	})
	_, out, _ := sweep(t, root)
	s := sweepSection(t, out, 4)
	if !strings.Contains(s, "a/ check* ×4") {
		t.Errorf("4 個の束が出る: %q", s)
	}
	if strings.Contains(s, "scan*") || strings.Contains(s, "c/") {
		t.Errorf("3 個の束と test の束は出ない: %q", s)
	}
}

func TestSweepRejectsNonText(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": goTwoSinks})
	if rc, _, errs := sweep(t, "-format", "json", root); rc != 2 || !strings.Contains(errs, "text だけ") {
		t.Errorf("rc=%d %q", rc, errs)
	}
}

// 組に入った対は部分木の節に重ねて出さない (同じ写しを 2 節に出すと、数が膨らむだけ)。
func TestSweepSimilarSkipsPairsAlreadyGrouped(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": goTwoSinks})
	_, out, _ := sweep(t, "-similar-threshold", "0.1", root)
	if s := sweepSection(t, out, 2); !strings.Contains(s, "captureOut") {
		t.Fatalf("前提: 組になる: %q", s)
	}
	if s := sweepSection(t, out, 3); strings.Contains(s, "captureOut") {
		t.Errorf("組の対は節 3 に出ない: %q", s)
	}
}

// 名前の束は小さい単位 (下限未満) だけ。大きい単位は report の領分で、束にすると同じ動詞の大きい関数が雑音になる。
func TestSweepNameClustersIgnoreUnitsAtOrAboveMinNodes(t *testing.T) {
	var b strings.Builder
	b.WriteString("package a\n\n")
	for i := range 4 {
		fmt.Fprintf(&b, "func bigThing%d(xs []int) int {\n\ttotal := 0\n\tfor _, x := range xs {\n\t\tif x > %d && total < 100 {\n\t\t\ttotal += x * 2\n\t\t} else {\n\t\t\ttotal -= x\n\t\t}\n\t}\n\treturn total\n}\n\n", i, i)
	}
	_, out, _ := sweep(t, "-min-nodes", "10", repoWith(t, map[string]string{"a/a.go": b.String()}))
	if s := sweepSection(t, out, 4); strings.Contains(s, "big*") {
		t.Errorf("下限以上の単位は束にしない: %q", s)
	}
}

const swiftNestedPair = `func outer(ys: [Int], cap: Int) -> Int {
    func inner(ys: [Int], cap: Int) -> Int {
        var total = 0
        for y in ys {
            if y > 0 && total < cap {
                total += y
            }
        }
        if total > cap {
            total = cap
        }
        return total
    }
    var total = 0
    for y in ys {
        if y > 0 && total < cap {
            total += y
        }
    }
    if total > cap {
        total = cap
    }
    return total
}
`

// 入れ子の単位 (外の関数の中の関数) は外と似ていて当たり前なので、対にしない。
func TestSweepSimilarSkipsNestedUnits(t *testing.T) {
	_, out, _ := sweep(t, "-similar-threshold", "0.1", repoWith(t, map[string]string{"A.swift": swiftNestedPair}))
	if s := sweepSection(t, out, 3); strings.Contains(s, "inner") {
		t.Errorf("入れ子の対は出ない: %q", s)
	}
}

func methodFile(name string, recvs ...string) string {
	var b strings.Builder
	b.WriteString("package a\n\n")
	for _, r := range recvs {
		fmt.Fprintf(&b, "type %s struct{ v int }\n\nfunc (x *%s) %s(n int) bool { return x.v == n }\n\n", r, r, name)
	}
	return b.String()
}

// 同名のメソッドの束: 名前の先頭の語では束ねられない `take` / `busy` のような同名を、受け口の型が 2 種類以上・3 個以上のときだけ出す。
func TestSweepSameNameMethodClusters(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/a.go": methodFile("take", "alpha", "beta", "gamma"),
		"b/b.go": methodFile("take", "alpha", "beta"),
		"c/c.go": methodFile("String", "alpha", "beta", "gamma"),
		"e/e.go": methodFile("add", "alpha", "beta", "gamma"),
		"d/d.go": "package a\n\ntype one struct{ v int }\n\nfunc (x *one) busy(n int) bool { return x.v == n }\nfunc (x *one) busyAgain(n int) bool { return x.v == n }\n",
	})
	_, out, _ := sweep(t, root)
	s := sweepSection(t, out, 4)
	if !strings.Contains(s, "a/ take (同名のメソッド) 受け口 ×3: alpha, beta, gamma") {
		t.Errorf("3 個・3 種類の同名のメソッドの束が出る: %q", s)
	}
	for _, not := range []string{"b/ take", "String", "e/ add"} {
		if strings.Contains(s, not) {
			t.Errorf("2 個の束と標準の名前 (String) は出ない: %q in %q", not, s)
		}
	}
}

func TestSweepSameNameMethodsNeedSimilarSize(t *testing.T) {
	big := "package a\n\ntype big struct{ v int }\n\nfunc (x *big) take(n int) bool {\n\tif n > 1 && x.v < n {\n\t\tx.v += n * 2\n\t\treturn true\n\t}\n\treturn x.v == n || n > 3\n}\n"
	root := repoWith(t, map[string]string{"a/a.go": methodFile("take", "alpha", "beta"), "a/big.go": big})
	_, out, _ := sweep(t, "-min-nodes", "60", root)
	if s := sweepSection(t, out, 4); strings.Contains(s, "take (同名") {
		t.Errorf("節の数が 3 倍を超えて違う同名は別の仕事として束にしない: %q", s)
	}
}

func TestSweepNameClustersSkipPredicateVerbs(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": clusterFile("is", 5)})
	_, out, _ := sweep(t, root)
	if s := sweepSection(t, out, 4); strings.Contains(s, "is*") {
		t.Errorf("述語 (is*) は束にしない: %q", s)
	}
}

// 末尾の数字だけが違う名前は、名前を避けた写しの手がかりとして出す。
func TestSweepSequentialNames(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": "package a\n\nfunc contains(xs []int, n int) bool { return len(xs) > n }\n\nfunc contains2(xs []int, n int) bool { return len(xs) > n }\n\nfunc other1() {}\n\nfunc foo() {}\n\nfunc foo2() {}\n"})
	_, out, _ := sweep(t, root)
	s := sweepSection(t, out, 4)
	if !strings.Contains(s, "a/ 連番の名前 contains: contains, contains2") || strings.Contains(s, "other") || strings.Contains(s, "foo") {
		t.Errorf("連番の名前の束: %q", s)
	}
}

// testdata の下だけでできる組は、go tool が無視する fixture なので prod/test でなく testdata と出す。
func TestSweepLabelsTestdataGroups(t *testing.T) {
	fn := func(pkg string) string { return strings.Replace(midFunctions, "package a", "package "+pkg, 1) }
	root := repoWith(t, map[string]string{"x/testdata/a/a.go": fn("a"), "x/testdata/b/b.go": fn("b")})
	_, out, _ := sweep(t, "-min-nodes", "12", root)
	if s := sweepSection(t, out, 1); !strings.Contains(s, "testdata ×") || strings.Contains(s, "prod ×") {
		t.Errorf("testdata の組の種別: %q", s)
	}
}

// 数字が桁数を表す名前 (Round / Round3) は大きさが違う別の仕事なので、連番の束にしない。
func TestSweepSequentialNamesNeedSimilarSize(t *testing.T) {
	big := "func roundN(x float64) float64 {\n\tif x < 0 {\n\t\treturn -roundN(-x)\n\t}\n\tscaled := x * 1000\n\tfloor := float64(int64(scaled))\n\tif scaled-floor >= 0.5 {\n\t\tfloor++\n\t}\n\treturn floor / 1000\n}\n"
	root := repoWith(t, map[string]string{"a/a.go": "package a\n\nfunc roundNum3(x float64) float64 { return x }\n\n" + strings.ReplaceAll(big, "roundN", "roundNum")})
	_, out, _ := sweep(t, root)
	if s := sweepSection(t, out, 4); strings.Contains(s, "連番の名前 roundNum") {
		t.Errorf("大きさが違う連番の名前は束にしない: %q", s)
	}
}

// bigGroupFile は、同じ形で名前だけ違う関数を n 個並べたファイル (shape で形を変えて、別の群にする)。
func bigGroupFile(prefix string, n int, shape string) string {
	var b strings.Builder
	b.WriteString("package a\n\n")
	for i := range n {
		fmt.Fprintf(&b, shape, prefix, i)
	}
	return b.String()
}

const (
	smallShape = "func %s%d(s string, n int) bool {\n\tif len(s) > n && n > 0 {\n\t\treturn true\n\t}\n\treturn false\n}\n\n"
	bigShape   = "func %s%d(s string, n int) int {\n\tt := 0\n\tfor _, c := range s {\n\t\tt += int(c) + n\n\t}\n\treturn t\n}\n\n"
)

// 20 個以上が同じ形の群は、重みが大きくても後ろに回し、先頭の数件だけ出す。19 個の群は重みの順のまま全員出す。
func TestSweepBigGroupsGoLastAndFold(t *testing.T) {
	for _, tc := range []struct {
		n       int
		wantTop string // 先頭に出る群の関数名の接頭辞
		folded  bool
	}{
		{19, "big", false},
		{20, "small", true},
	} {
		root := repoWith(t, map[string]string{
			"a/a.go": bigGroupFile("big", tc.n, bigShape),
			"b/b.go": strings.Replace(bigGroupFile("small", 3, smallShape), "package a", "package b", 1),
		})
		_, out, _ := sweep(t, "-limit", "0", root)
		s := sweepSection(t, out, 2)
		if i, j := strings.Index(s, "big0"), strings.Index(s, "small0"); i < 0 || j < 0 || (i < j) != (tc.wantTop == "big") {
			t.Errorf("n=%d: 先頭は %s の群: %q", tc.n, tc.wantTop, s)
		}
		if got := strings.Contains(s, "… 他 "); got != tc.folded {
			t.Errorf("n=%d: 畳み=%v のはず: %q", tc.n, tc.folded, s)
		}
		if tc.folded && strings.Contains(s, "big5") {
			t.Errorf("n=%d: 6 件目以降は出ない: %q", tc.n, s)
		}
	}
}

// 見たあとの手段 (許可印の付け方) を、出力の先頭に 1 行添える。判断済みの組が毎回出る repo で、次の人が読み直さずに済む。
func TestSweepShowsHowToSilenceAGroup(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": goTwoSinks})
	_, out, _ := sweep(t, root)
	if !strings.Contains(out, "// dupscan:allow <false-positive|intentional> <理由>") {
		t.Errorf("許可印の付け方の案内が無い: %q", out)
	}
}

// 単位が既定の上限に収まる repo では節 3 を飛ばさない (既定を上げた根拠は 6128 単位で総当たり約 4.5 秒の実測)。
func TestSweepDefaultCapKeepsMidSizedReposInSection3(t *testing.T) {
	if sweepMaxSimilarUnit < 6000 {
		t.Errorf("既定の上限 %d が 6000 を下回る: 6128 単位の repo で節 3 が飛ばされる", sweepMaxSimilarUnit)
	}
}

const goTwoSinksAllowed = `package a

import (
	"io"
	"os"
)

// dupscan:allow intentional 出力先が違うだけの別の窓口 (テストの注記)
func captureOut(fn func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// dupscan:allow intentional 出力先が違うだけの別の窓口 (テストの注記)
func captureErr(fn func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stderr = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}
`

// `dupscan:allow` はメンバー全員に付いた組を節 2 から除く。片方だけの組は出し続ける (もう片方の新しい写しを見逃さない)。
func TestSweepAllowMarkersSilenceGroupsOnlyWhenEveryMemberIsAllowed(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": goTwoSinksAllowed})
	_, out, _ := sweep(t, root)
	if s := sweepSection(t, out, 2); strings.Contains(s, "captureOut") {
		t.Errorf("全員が許可済みの組が出ている: %q", s)
	}
	half := strings.Replace(goTwoSinksAllowed, "// dupscan:allow intentional 出力先が違うだけの別の窓口 (テストの注記)\nfunc captureErr", "func captureErr", 1)
	_, out, _ = sweep(t, repoWith(t, map[string]string{"a/a.go": half}))
	if s := sweepSection(t, out, 2); !strings.Contains(s, "captureOut") {
		t.Errorf("片方だけ許可済みの組は出し続ける: %q", s)
	}
}

// 節 3 の対も、両方が許可済みなら除く。
func TestSweepAllowMarkersSilenceSimilarPairs(t *testing.T) {
	marked := strings.Replace(goNearPair, "func sumPositive", "// dupscan:allow intentional 注記\nfunc sumPositive", 1)
	marked = strings.Replace(marked, "func sumLogged", "// dupscan:allow intentional 注記\nfunc sumLogged", 1)
	_, out, _ := sweep(t, "-similar-threshold", "0.5", repoWith(t, map[string]string{"a/a.go": marked}))
	if s := sweepSection(t, out, 3); strings.Contains(s, "sumPositive") {
		t.Errorf("両方が許可済みの対が出ている: %q", s)
	}
	_, out, _ = sweep(t, "-similar-threshold", "0.5", repoWith(t, map[string]string{"a/a.go": goNearPair}))
	if s := sweepSection(t, out, 3); !strings.Contains(s, "sumPositive") {
		t.Fatalf("前提: 印が無ければ出る: %q", s)
	}
}

// 節 1 (既定の下限の組) でも、全員が許可済みなら除く。
func TestSweepAllowMarkersSilenceSection1Groups(t *testing.T) {
	marked := strings.Replace(midFunctions, "func firstMid", "// dupscan:allow intentional 注記\nfunc firstMid", 1)
	marked = strings.Replace(marked, "func secondMid", "// dupscan:allow intentional 注記\nfunc secondMid", 1)
	_, out, _ := sweep(t, "-min-nodes", "12", repoWith(t, map[string]string{"a.go": midFunctions}))
	if s := sweepSection(t, out, 1); !strings.Contains(s, "firstMid") {
		t.Fatalf("前提: 印が無ければ節 1 に出る: %q", s)
	}
	_, out, _ = sweep(t, "-min-nodes", "12", repoWith(t, map[string]string{"a.go": marked}))
	if s := sweepSection(t, out, 1); strings.Contains(s, "firstMid") {
		t.Errorf("全員が許可済みの組が節 1 に出ている: %q", s)
	}
}

// 節 3 の対は、片方だけ許可済みなら出し続ける (もう片方と新しい写しを作っても見逃さない)。
func TestSweepSimilarPairKeptWhenOnlyOneSideIsAllowed(t *testing.T) {
	marked := strings.Replace(goNearPair, "func sumPositive", "// dupscan:allow intentional 注記\nfunc sumPositive", 1)
	_, out, _ := sweep(t, "-similar-threshold", "0.5", repoWith(t, map[string]string{"a/a.go": marked}))
	if s := sweepSection(t, out, 3); !strings.Contains(s, "sumPositive") {
		t.Errorf("片方だけ許可済みの対が消えた: %q", s)
	}
}

// 節 3 は、一致度だけでなく共有している重みの大きい順に並べる。小さい対が一致度 1.0 で上位を占めて、
// 寄せたときに減る量の大きい対が沈まないように。
func TestSweepSimilarRanksByIsSharedWeightNotOnlyScore(t *testing.T) {
	// tinyA / tinyB: 本体は同じで戻り値の型だけが違う小さい対 (どの段のハッシュも一致しないが Dice は 1.000)
	tiny := `package a

func tinyA(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

func tinyB(xs []int, n int) int {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}
`
	big := strings.ReplaceAll(strings.ReplaceAll(goNearPair, "sumPositive", "bigA"), "sumLogged", "bigB")
	_, out, _ := sweep(t, "-similar-threshold", "0.5", repoWith(t, map[string]string{"a/tiny.go": tiny, "b/big.go": big}))
	s := sweepSection(t, out, 3)
	ti, bi := strings.Index(s, "tinyA"), strings.Index(s, "bigA")
	if ti < 0 || bi < 0 {
		t.Fatalf("前提: 両方の対が出る: %q", s)
	}
	if bi > ti {
		t.Errorf("共有の重みが大きい対 (bigA) が、一致度が高いだけの小さい対 (tinyA) より後ろ: %q", s)
	}
	if !strings.Contains(s, " w=") {
		t.Errorf("共有の重みを出す: %q", s)
	}
	if head, _, _ := strings.Cut(s, "\n"); !strings.Contains(head, "w = 共有している部分木の重み") {
		t.Errorf("節 3 の見出しで w の意味を言う: %q", head)
	}
}

// 印を足した後の sweep は、印で消した組の数を出す (「なし」だけでは印が効いたのか元から組が無いのかを区別できない)。
// 人数の合わない allow-group は組を消さず、印の場所と数の食い違いを出す (黙って組を出し直すだけにしない)。
func TestSweepReportsSilencedGroupsAndMiscountedAllowGroup(t *testing.T) {
	_, out, _ := sweep(t, repoWith(t, map[string]string{"a/a.go": goTwoSinks}))
	if !strings.Contains(out, "印・除外で静かにした組 (節 1・2): 0\n") || strings.Contains(out, "効いていない allow-group") {
		t.Fatalf("前提: 印が無ければ消した組は 0・食い違いも無い: %q", out)
	}
	marked := strings.Replace(goTwoSinks, "func captureOut", "// dupscan:allow-group 2 intentional 注記\nfunc captureOut", 1)
	_, out, _ = sweep(t, repoWith(t, map[string]string{"a/a.go": marked}))
	// 関数の中のクロージャどうしは別の組 (L1) として残るので、関数の組 (L1m) で見る
	if s := sweepSection(t, out, 2); strings.Contains(s, "[L1m]") {
		t.Fatalf("前提: 人数の合う allow-group は組を消す: %q", s)
	}
	if !strings.Contains(out, "印・除外で静かにした組 (節 1・2): 1\n") {
		t.Errorf("消した組の数が出ていない: %q", out)
	}
	miscount := strings.Replace(marked, "allow-group 2 ", "allow-group 3 ", 1)
	_, out, _ = sweep(t, repoWith(t, map[string]string{"a/a.go": miscount}))
	if s := sweepSection(t, out, 2); !strings.Contains(s, "[L1m]") {
		t.Fatalf("前提: 人数の合わない allow-group は組を消さない: %q", s)
	}
	if !strings.Contains(out, "🚨 効いていない allow-group (組は出し続ける。寄せるか、確かめてから人数を直す): a/a.go:8 (人数 3・") {
		t.Errorf("人数の合わない印の場所と数が出ていない: %q", out)
	}
	if !strings.Contains(out, "印・除外で静かにした組 (節 1・2): 0\n") {
		t.Errorf("効かなかった印を消した組に数えている: %q", out)
	}
}
