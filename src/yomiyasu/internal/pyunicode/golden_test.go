package pyunicode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"

	"yomiyasu/oracle"
)

const goldenDir = "../../testdata/golden"

var (
	goldenOnce sync.Once
	goldenData oracle.UnicodeGolden
	goldenErr  error
)

// loadGolden は Python から取った記録 (unicode.json) を 1 回だけ読む。記録が無いときは落とす (飛ばして緑にしない)。
func loadGolden(t *testing.T) oracle.UnicodeGolden {
	t.Helper()
	goldenOnce.Do(func() { goldenErr = oracle.ReadGolden(goldenDir, oracle.UnicodeFile, &goldenData) })
	if goldenErr != nil {
		t.Fatalf("make golden で記録を作る: %v", goldenErr)
	}
	return goldenData
}

func TestGoldenIsFromPython317(t *testing.T) {
	g := loadGolden(t)
	if g.Header.UnicodedataVersion != UnicodeVersion || UnicodeVersion != "17.0.0" {
		t.Fatalf("記録の unicodedata %s・表の %s", g.Header.UnicodedataVersion, UnicodeVersion)
	}
}

func TestCategoryMatchesPythonForEveryCodePoint(t *testing.T) {
	g := loadGolden(t)
	runs := g.Categories
	if len(runs) < 1000 || runs[0].Start != 0 {
		t.Fatalf("記録の一般カテゴリが想定と違う (%d 区間)", len(runs))
	}
	checked := 0
	for i, run := range runs {
		end := rune(MaxRune)
		if i+1 < len(runs) {
			end = runs[i+1].Start - 1
		}
		for r := run.Start; r <= end; r++ {
			if got := Category(r); got != run.Cat {
				t.Fatalf("Category(U+%04X) = %s、Python は %s", r, got, run.Cat)
			}
			checked++
		}
	}
	if checked != MaxRune+1 {
		t.Fatalf("確かめた字が %d 字 (期待 %d)", checked, MaxRune+1)
	}
	for _, r := range []rune{-1, MaxRune + 1} {
		if Category(r) != "Cn" {
			t.Errorf("範囲外の Category(%d) = %s", r, Category(r))
		}
	}
}

// Unicode 16 / 17 で割り当てられた字を Python の表のとおりに分類する (Go 1.26 の unicode は 15.0.0 で、これらを割り当てなしとみなす)。
func TestCategoryKnowsUnicode16And17Characters(t *testing.T) {
	for r, want := range map[rune]string{0x1E5F1: "Nd", 0x16D70: "Nd", 0x11BF1: "Nd", 0x10D40: "Nd", 0x1CCF0: "Nd", 0x2B73A: "Lo"} {
		if got := Category(r); got != want {
			t.Errorf("Category(U+%04X) = %s、期待 %s", r, got, want)
		}
	}
}

func expand(rs []oracle.CPRange) []bool {
	out := make([]bool, MaxRune+1)
	for _, r := range rs {
		for c := r.Lo; c <= r.Hi; c++ {
			out[c] = true
		}
	}
	return out
}

func TestPropertiesMatchPythonForEveryCodePoint(t *testing.T) {
	g := loadGolden(t)
	for name, fn := range map[string]func(rune) bool{
		"space": IsSpace, "alpha": IsAlpha, "decimal": IsDecimal, "digit": IsDigit, "numeric": IsNumeric, "linebreak": IsLineBreak,
	} {
		want := expand(g.Props[name])
		count := 0
		for r := rune(0); r <= MaxRune; r++ {
			if fn(r) != want[r] {
				t.Fatalf("%s(U+%04X) = %v、Python は %v", name, r, fn(r), want[r])
			}
			if want[r] {
				count++
			}
		}
		if count == 0 {
			t.Fatalf("%s に当たる字が 0 字 (記録が空)", name)
		}
	}
	// isalnum は 4 つの性質の和 (記録に無い組み合わせなので、定義どおりであることを字で確かめる)
	for _, c := range []struct {
		r    rune
		want bool
	}{{'a', true}, {'5', true}, {'²', true}, {'①', true}, {'Ⅷ', true}, {'½', true}, {'_', false}, {'-', false}, {' ', false}, {'日', true}} {
		if IsAlnum(c.r) != c.want {
			t.Errorf("IsAlnum(%q) = %v", c.r, IsAlnum(c.r))
		}
	}
}

func TestCombiningClassMatchesPythonForEveryCodePoint(t *testing.T) {
	g := loadGolden(t)
	want := make([]uint8, MaxRune+1)
	for _, run := range g.CCC {
		for r := run.Lo; r <= run.Hi; r++ {
			want[r] = uint8(run.Class)
		}
	}
	for r := rune(0); r <= MaxRune; r++ {
		if got := CombiningClass(r); got != want[r] {
			t.Fatalf("CombiningClass(U+%04X) = %d、Python は %d", r, got, want[r])
		}
	}
}

func isSurrogate(r rune) bool { return r >= 0xD800 && r <= 0xDFFF }

// 全コードポイントの 1 字ずつの正規化。NFC / NFKC は元と違う字を記録の一覧と照らし (診断のため)、4 形式とも記録の digest と照らす。
func TestNormalizeSingleCharactersMatchPython(t *testing.T) {
	g := loadGolden(t)
	exceptions := map[Form]map[rune][]rune{NFC: {}, NFKC: {}}
	for _, d := range g.NFC {
		exceptions[NFC][d.CP] = d.To
	}
	for _, d := range g.NFKC {
		exceptions[NFKC][d.CP] = d.To
	}
	for _, c := range []struct {
		form   Form
		name   string
		digest string
	}{{NFC, "nfc", g.Digests["nfc"]}, {NFD, "nfd", g.Digests["nfd"]}, {NFKC, "nfkc", g.Digests["nfkc"]}, {NFKD, "nfkd", g.Digests["nfkd"]}} {
		if len(c.digest) != 64 {
			t.Fatalf("%s の digest が記録に無い", c.name)
		}
		h := sha256.New()
		var line strings.Builder
		for r := rune(0); r <= MaxRune; r++ {
			if isSurrogate(r) {
				continue
			}
			got := Runes(Normalize(c.form, string(r)))
			if want, ok := exceptions[c.form]; ok {
				w, differs := want[r]
				if !differs {
					w = []rune{r}
				}
				if fmt.Sprint(got) != fmt.Sprint(w) {
					t.Fatalf("Normalize(%s, U+%04X) = %U、Python は %U", c.name, r, got, w)
				}
			}
			line.Reset()
			fmt.Fprintf(&line, "%x:", r)
			for i, x := range got {
				if i > 0 {
					line.WriteByte(' ')
				}
				fmt.Fprintf(&line, "%x", x)
			}
			line.WriteByte('\n')
			h.Write([]byte(line.String()))
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != c.digest {
			t.Errorf("%s の全コードポイントの digest %s、Python は %s", c.name, got, c.digest)
		}
	}
}

func TestNormalizeStringsMatchPython(t *testing.T) {
	g := loadGolden(t)
	if len(g.Cases) < 1000 {
		t.Fatalf("文字列の記録が %d 件しかない", len(g.Cases))
	}
	for _, c := range g.Cases {
		for _, f := range []struct {
			form Form
			name string
			want string
		}{{NFC, "NFC", c.NFC}, {NFD, "NFD", c.NFD}, {NFKC, "NFKC", c.NFKC}, {NFKD, "NFKD", c.NFKD}} {
			if got := Normalize(f.form, c.S); got != f.want {
				t.Fatalf("Normalize(%s, %U) = %U、Python は %U", f.name, []rune(c.S), []rune(got), []rune(f.want))
			}
		}
	}
}

// 記録の合成のうち、Go 1.26 の x/text (Unicode 15.0.0) の知らないものがあること (表を作り直す理由) と、それが合成されること。
func TestNormalizeComposesUnicode16And17Pairs(t *testing.T) {
	g := loadGolden(t)
	newer := 0
	for _, p := range g.Compose {
		if Category(p.Composite) == "Cn" {
			t.Fatalf("合成した字 U+%04X が割り当てなし", p.Composite)
		}
		s := string([]rune{p.First, p.Second})
		if got := Normalize(NFC, s); got != string(p.Composite) {
			t.Fatalf("Normalize(NFC, U+%04X U+%04X) = %U、期待 U+%04X", p.First, p.Second, []rune(got), p.Composite)
		}
		if p.Composite >= 0x11380 && (p.Composite <= 0x113FF || (p.Composite >= 0x16D60 && p.Composite <= 0x16D7F)) {
			newer++
		}
	}
	if newer == 0 {
		t.Fatal("Unicode 16 で増えた合成 (Tulu-Tigalari・Kirat Rai) が記録に 1 つも無い")
	}
}
