package termwidth

import (
	"math/rand"
	"strings"
	"testing"
)

// issue 416 の再現例。どれも「切った結果が要求幅を超える」形だった。
func TestTruncatesNeverExceedWidth(t *testing.T) {
	for _, c := range []struct {
		name string
		got  string
		max  int
	}{
		{"TruncateLeft: 全角 1 字を幅 1 へ", TruncateLeft("あ", 1, ""), 1},
		{"TruncateLeft: 全角の途中で切れる", TruncateLeft("docs/日本語.go", 5, "…"), 5},
		{"TruncateLeft: 幅 0 は空 (… だけ返さない)", TruncateLeft("abc", 0, "…"), 0},
		{"Cut: キーキャップを幅 1 へ", Cut("1️⃣", 1), 1},
		{"Clip: キーキャップの後ろで切る", Clip("1️⃣ ", 2), 2},
	} {
		if w := Of(c.got); w > c.max {
			t.Errorf("%s: %q の幅 %d > %d", c.name, c.got, w, c.max)
		}
	}
	// 収まるものは変えない (切り詰めの結果を狭めすぎない)
	if got := TruncateLeft("docs/日本語.go", 7, "…"); got != "…語.go" {
		t.Errorf("TruncateLeft(…, 7) = %q, want %q", got, "…語.go")
	}
}

// 乱数の入力で「切り詰めの結果は要求幅を超えない」を見る (seed 固定)。
// 文字集合は幅の数え方が食い違いやすいもの: 全角・キーキャップ (ASCII+VS16+U+20E3)・VS16 付き ASCII・
// 結合文字・ZWJ 絵文字・国旗・SGR。
func TestTruncatePropertyWidth(t *testing.T) {
	atoms := []string{"a", "Z", " ", "あ", "漢", "1️⃣", "#️⃣", "a️", "é",
		"👨‍💻", "🇯🇵", "─", "…", "\x1b[31m", "\x1b[0m", "\x1b[1;4m"}
	r := rand.New(rand.NewSource(416))
	checked := 0
	for range 20000 {
		var b strings.Builder
		for n := r.Intn(12); n > 0; n-- {
			b.WriteString(atoms[r.Intn(len(atoms))])
		}
		s := b.String()
		w := r.Intn(14)
		for name, got := range map[string]string{
			"Truncate":     Truncate(s, w, "…"),
			"Clip":         Clip(s, w),
			"Cut":          Cut(s, w),
			"TruncateLeft": TruncateLeft(s, w, "…"),
		} {
			if Of(got) > w {
				t.Fatalf("%s(%q, %d) = %q の幅 %d が要求を超えた", name, s, w, got, Of(got))
			}
		}
		if got, gw := ClipMeasure(s, w); gw != Of(got) || gw > w {
			t.Fatalf("ClipMeasure(%q, %d) = (%q, %d) が不正", s, w, got, gw)
		}
		checked++
	}
	if checked != 20000 {
		t.Fatalf("検査が %d 件 (20000 のはず)", checked)
	}
}

// 削りすぎていないこと (下限)。SGR を含まない入力で、クラスタの境界で切れる最良の幅と一致するかを見る。
// 敵対的レビュー (issue 416): 最初の修正は切り直すたびに目標の幅まで詰めていて、`Cut("x1️⃣y", 3)` が
// "x"、キーキャップだけの行が空になっていた (上限だけの検査では見えない)。
func TestTruncateDoesNotOverTruncate(t *testing.T) {
	const kc = "1️⃣"
	for _, c := range []struct {
		name, got, want string
	}{
		{"Cut: キーキャップの後ろで切る", Cut("x"+kc+"y", 3), "x" + kc},
		{"Cut: キーキャップだけの行", Cut(kc+kc+kc, 4), kc + kc},
		{"Cut: 長い行 (幅 8 ちょうどに収まる)", Cut("ab"+kc+kc+kc+"cdefgh", 8), "ab" + kc + kc + kc},
		{"TruncateLeft: キーキャップを削りすぎない", TruncateLeft("ab"+kc+"─", 1, ""), "─"},
		{"TruncateLeft: 収まっている行は削らない (印の幅を足して削っていた)", TruncateLeft("abc", 3, "…"), "abc"},
	} {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
		}
	}

	clusters := func(s string) (cs []string, ws []int) {
		for s != "" {
			c, w := FirstCluster(s)
			cs, ws = append(cs, c), append(ws, w)
			s = s[len(c):]
		}
		return cs, ws
	}
	atoms := []string{"a", "Z", " ", "あ", "漢", kc, "#️⃣", "a️", "é", "👨‍💻", "🇯🇵", "─"}
	r := rand.New(rand.NewSource(4160))
	for range 20000 {
		var b strings.Builder
		for n := r.Intn(12); n > 0; n-- {
			b.WriteString(atoms[r.Intn(len(atoms))])
		}
		s := b.String()
		w := r.Intn(14)
		_, ws := clusters(s)
		// 先頭から: 足していって w に収まる最大の幅
		best := 0
		for acc, i := 0, 0; i < len(ws) && acc+ws[i] <= w; i++ {
			acc += ws[i]
			best = acc
		}
		if got := Of(Cut(s, w)); got != best {
			t.Fatalf("Cut(%q, %d) の幅 %d, 最良は %d (削りすぎ or はみ出し)", s, w, got, best)
		}
		// 末尾から: 足していって w に収まる最大の幅
		bestL := 0
		for acc, i := 0, len(ws)-1; i >= 0 && acc+ws[i] <= w; i-- {
			acc += ws[i]
			bestL = acc
		}
		if got := Of(TruncateLeft(s, w, "")); got != bestL {
			t.Fatalf("TruncateLeft(%q, %d) の幅 %d, 最良は %d", s, w, got, bestL)
		}
	}
}
