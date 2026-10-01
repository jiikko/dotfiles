package termwidth

import (
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// wrapAtoms は幅の数え方が食い違いやすい部品 (キーキャップ・VS16・肌色・ZWJ・国旗・結合文字・全角・SGR・空白)。
// キーキャップは x/ansi の Hardwrap / Wrap が幅を超える行を返した形 (issue 590)。
var wrapAtoms = []string{"a", "Z", " ", "-", "あ", "漢", "1\ufe0f\u20e3", "#\ufe0f\u20e3", "a\ufe0f", "e\u0301",
	"👍\U0001f3fd", "👨\u200d💻", "🇯🇵", "─", "\x1b[31m", "\x1b[0m", "\x1b[1;4m",
	"\x1b[39m", "\x1b[0;31m", "\x1b]x", "\x1b]8;;http://u\x1b\\", "\x1bPq", "\x1b(B", "\x1b[>4;1m", "\x07", "\r", "\u200b"}

// dropSpaceCR は空白と CR を除いた文字列。
func dropSpaceCR(s string) string { return strings.NewReplacer(" ", "", "\r", "").Replace(s) }

// startsMidCluster は v の先頭が、単独では書記素の頭にならない字 (VS16・キーキャップの U+20E3・結合文字・肌色修飾子・ZWJ) か。
func startsMidCluster(v string) bool {
	r, _ := utf8.DecodeRuneInString(v)
	return r == 0xfe0f || r == 0x20e3 || r == 0x0301 || (r >= 0x1f3fb && r <= 0x1f3ff) || r == 0x200d
}

// 乱数の入力で、Wrap / WordWrap の出力の各行が width 以下・行の頭が書記素の途中でない・見える字が欠けない (seed 固定)。
func TestWrapProperties(t *testing.T) {
	r := rand.New(rand.NewSource(590))
	for range 20000 {
		var b strings.Builder
		for n := r.Intn(40); n > 0; n-- {
			b.WriteString(wrapAtoms[r.Intn(len(wrapAtoms))])
		}
		s := b.String()
		w := 2 + r.Intn(14) // 幅 1 は全角が入らないので別のテスト
		for name, lines := range map[string][]string{
			"Wrap":       Wrap(s, w, false),
			"Wrap(trim)": Wrap(s, w, true),
			"WordWrap":   WordWrap(s, w),
		} {
			for _, l := range lines {
				if Of(l) > w {
					t.Fatalf("%s(%q, %d) の行 %q の幅 %d が要求を超えた", name, s, w, l, Of(l))
				}
				if startsMidCluster(ansi.Strip(l)) {
					t.Fatalf("%s(%q, %d) の行 %q が書記素の途中から始まる", name, s, w, l)
				}
			}
			// 改行の無い入力を trimSpace なしで折ると、つなげ直せば入力そのもの (色を張り直さない・ESC 列を捨てない・
			// 字を落とさない)。trim あり・WordWrap は空白だけが減ってよい
			if name == "Wrap" && strings.Join(lines, "") != s {
				t.Fatalf("Wrap(%q, %d) をつなげ直すと入力と違う: %q", s, w, strings.Join(lines, ""))
			}
			// WordWrap は境目を x/ansi の Wordwrap に任せており、Wordwrap は CR も落とす (置き換え前の ansi.Wrap と同じ)
			if name != "Wrap" && dropSpaceCR(strings.Join(lines, "")) != dropSpaceCR(s) {
				t.Fatalf("%s(%q, %d) で空白以外が変わった: %q", name, s, w, strings.Join(lines, ""))
			}
		}
	}
}

// キーキャップの列: x/ansi の Hardwrap は幅 4 で "1\ufe0f\u20e32\ufe0f\u20e3" (幅 4) の後ろに 3\ufe0f\u20e3 まで載せて幅を超えた。
func TestWrapKeycapsStayWithinWidth(t *testing.T) {
	got := Wrap("1\ufe0f\u20e32\ufe0f\u20e33\ufe0f\u20e34\ufe0f\u20e3", 5, false)
	want := []string{"1\ufe0f\u20e32\ufe0f\u20e3", "3\ufe0f\u20e34\ufe0f\u20e3"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

// 改行は行の区切り。trimSpace は折り返しで始まった行の頭の空白だけを落とし、改行の後の字下げは残す。
// 行をまたぐ色は次の行の頭で張り直す。幅より広いクラスタはその行だけ幅を超える (割らない)。
func TestWrapContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  []string
		want []string
	}{
		{"改行", Wrap("ab\n\ncd", 5, false), []string{"ab", "", "cd"}},
		{"空白を残す", Wrap("abc def", 4, false), []string{"abc ", "def"}},
		{"折り返しの頭の空白を落とす", Wrap("abcd  ef", 4, true), []string{"abcd", "ef"}},
		{"改行の後の字下げは残す", Wrap("ab\n  cd", 4, true), []string{"ab", "  cd"}},
		{"空白だけの残りは行にしない", Wrap("abcd   ", 4, true), []string{"abcd"}},
		{"色は張り直さない (x/ansi の Hardwrap と同じ)", Wrap("\x1b[31mabcdef", 3, false), []string{"\x1b[31mabc", "def"}},
		{"閉じていない OSC は Of と同じく ESC で切れる", Wrap("\x1b]x\x1b[31mabcdef", 3, false), []string{"\x1b]x\x1b[31mabc", "def"}},
		{"中間バイト付きの ESC 列を割らない", Wrap("ab\x1b(Bcd", 1, false), []string{"a", "b\x1b(B", "c", "d"}},
		{"空白だけの残りにある reset は前の行へ", Wrap("\x1b[31mabc   \x1b[0m", 3, true), []string{"\x1b[31mabc\x1b[0m"}},
		{"幅より広いクラスタ", Wrap("あい", 1, false), []string{"あ", "い"}},
		{"幅 0 は折らない", Wrap("abc", 0, false), []string{"abc"}},
		{"単語で折る", WordWrap("foo bar baz", 7), []string{"foo bar", "baz"}},
		{"長い単語は途中で折る", WordWrap("abcdefgh ij", 4), []string{"abcd", "efgh", "ij"}},
	} {
		if strings.Join(tc.got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: got %q want %q", tc.name, tc.got, tc.want)
		}
	}
}

// 先頭が幅 0 の字 (制御文字・CR・ZWSP・素の ESC) でも止まる。Cut + DropColumns で組んでいた版は幅 0 の字の前で
// 進まなくなった (敵対的レビュー P1)。🚨 時間は合否に使わない: 10 秒は止まらないときにスイートを止めないための安全網。
func TestWrapTerminatesOnZeroWidthHeads(t *testing.T) {
	inputs := []string{"\x00あ", "\x01あ", "\rあ", "\r\rあ", "\u200bあ", "\x1bあ", "\x1b\u26a0\ufe0fあ  ", "\x7fあ", "\x1b[あ"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, in := range inputs {
			for w := 1; w <= 6; w++ {
				Wrap(in, w, false)
				Wrap(in, w, true)
				WordWrap(in, w)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("幅 0 の字で始まる入力で Wrap が止まらない")
	}
}

// 色の解除 (ESC[39m 等) を繰り返す入力でも出力は入力に比例する (行ごとに SGR を張り直していた版は、部分的な解除で
// 張り直しが積み上がり 100 KB が 68 MB に膨らんだ。敵対的レビュー 2 周目)。幅 0 の字は 1 つの行にだけ入る。
func TestWrapOutputStaysProportional(t *testing.T) {
	in := strings.Repeat("\x1b[38;5;1mab\x1b[39m ", 2000)
	if got := len(strings.Join(Wrap(in, 10, false), "")); got != len(in) {
		t.Fatalf("出力 %d byte、入力は %d byte", got, len(in))
	}
	if cr := strings.Count(strings.Join(Wrap("abc\rdef", 1, false), ""), "\r"); cr != 1 {
		t.Fatalf("CR が %d 個になった (1 個のはず)", cr)
	}
}
