package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// acceptedSymbols は fast-path が受理する記号を表から数え上げる (列挙の写しを持たない)。
func acceptedSymbols(t testing.TB) []rune {
	t.Helper()
	var out []rune
	for r := rune(symTableLo); r <= symTableHi; r++ {
		if symbolWidth(r) != 0 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		t.Fatal("受理する記号が 0 個 (表の構築が壊れている)")
	}
	return out
}

// 表の幅がライブラリと一致していること。⚠️ 「幅 1」ではなく「ライブラリと同じ」を
// 主張する: RUNEWIDTH_EASTASIAN が真のとき ansi は罫線等を幅 2 と数えるので、
// 1 を期待値に焼くとその環境で嘘になる (R1 レビューで実証された退行)。
func TestSymbolWidthTableMatchesLibrary(t *testing.T) {
	syms := acceptedSymbols(t)
	for _, r := range syms {
		if got, want := symbolWidth(r), ansi.StringWidth(string(r)); got != want {
			t.Errorf("U+%04X (%q): 表=%d ansi=%d", r, r, got, want)
		}
	}
	t.Logf("受理する記号 %d 個の幅がライブラリと一致 (EASTASIAN=%q)",
		len(syms), os.Getenv("RUNEWIDTH_EASTASIAN"))
}

// 表の範囲外が受理されないこと。R1 の指摘 (以前の総当たりが U+FFFF で止まっていた)
// への対処で、範囲の外側は BMP 外まで含めて 0 であることを主張する。
func TestSymbolWidthRejectsOutsideTable(t *testing.T) {
	for _, r := range []rune{0, 0x20, 0x7e, 0x7f, symTableLo - 1, symTableHi + 1,
		0x3000, 0x4E00, 0xFFFD, 0xFFFE, 0xFFFF, 0x1F680, 0x1F1EF, 0x10FFFF, -1} {
		if w := symbolWidth(r); w != 0 {
			t.Errorf("U+%04X を受理してしまった (w=%d)", r, w)
		}
	}
	// 受理される rune が表の上限を超えないこと (fastDispWidth の default 分岐の前提)
	for _, r := range acceptedSymbols(t) {
		if r < symTableLo || r > symTableHi {
			t.Errorf("受理 rune U+%04X が表の範囲外", r)
		}
	}
}

// 受理文字同士は決してクラスタを結合しないこと。
//
// なぜ総当たりのペアなのか: クラスタが伸びる規則には「後続が Extend/ZWJ/…」(GB9 系) と
// 「先行が Prepend / Indic conjunct」(GB9b/9c) の 2 方向がある。後者は受理文字が**直前の
// 文字に飲まれる**形で効くので、片方向だけ考えると見落とす (U+0600 + "1" が 1 クラスタ・
// 幅 0 になる実例がある)。受理集合が閉じていることは、集合内の全ペアで
// 「クラスタ数 = 2」かつ「fast の幅 = ansi の幅」を確かめて初めて言える。
func TestAcceptedSymbolsNeverCombineWithEachOther(t *testing.T) {
	syms := acceptedSymbols(t)
	// ASCII 代表も混ぜる (Prepend が ASCII を飲むケースを塞ぐため)
	probes := append([]rune{'a', '0', ' ', '~', '!'}, syms...)
	pairs := 0
	for _, a := range syms {
		for _, b := range probes {
			s := string(a) + string(b)
			if n := uniseg.GraphemeClusterCount(s); n != 2 {
				t.Fatalf("U+%04X + U+%04X が %d クラスタに結合した", a, b, n)
			}
			w, ok := fastDispWidth(s)
			if !ok {
				t.Fatalf("U+%04X + U+%04X が受理されなかった", a, b)
			}
			if lib := ansi.StringWidth(s); w != lib {
				t.Fatalf("U+%04X + U+%04X: fast=%d ansi=%d", a, b, w, lib)
			}
			pairs++
		}
	}
	t.Logf("%d ペアで結合なし・幅一致を確認", pairs)
}

// RUNEWIDTH_EASTASIAN=1 の子プロセスでも dispWidth が ansi と一致すること。
//
// ⚠️ env は x/ansi の init が読むので、同一プロセス内で os.Setenv しても効かない。
// 子プロセスを起こして確かめるしかない (この形でしか退行を捕まえられない)。
func TestDispWidthAgreesUnderEastAsianEnv(t *testing.T) {
	if os.Getenv("GLOGX_EAW_CHILD") == "1" {
		// ⚠️ まず env が実際に効いていることを止まる形で確かめる。これが無いと、x/ansi が
		// env の読み方を変えた日にこのテストは**無言で恒真になる** (assert が
		// dispWidth == ansi.StringWidth の形なので、両辺が同じ幅モデルに乗ると常に通る)。
		if w := ansi.StringWidth("─"); w != 2 {
			t.Fatalf("RUNEWIDTH_EASTASIAN=1 が効いていない (ansi.StringWidth(\"─\")=%d, want 2)。"+
				"x/ansi の env の読み方が変わった可能性がある — このテストは効かなくなっている", w)
		}
		// 子プロセス側: 罫線・矢印・… を含む文字列で一致を確かめる
		for _, s := range []string{"─", "←", "…", "·", strings.Repeat("─", 10),
			"✓ ok  ─── \x1b[32m→\x1b[0m …", "┌──┐"} {
			if got, want := dispWidth(s), ansi.StringWidth(s); got != want {
				t.Fatalf("EASTASIAN=1: dispWidth=%d ansi=%d for %q", got, want, s)
			}
		}
		// レイアウト算術が破れないこと (退行の実害はここに出た。罫線 10 個で
		// fillRight(s,30) が実幅 40、truncateDispLeft(...,8) が 19 になっていた)
		s := strings.Repeat("─", 10)
		if got := ansi.StringWidth(fillRight(s, 30)); got != 30 {
			t.Fatalf("EASTASIAN=1: fillRight(s,30) の実幅が %d (要求 30)", got)
		}
		// truncateDispLeft は「dispWidth が ansi.StringWidth だったとき」と同じ結果で
		// なければならない。⚠️ 実幅 <= 要求幅では主張できない: 全角グリフが切り位置を
		// またぐと ansi.TruncateLeft の丸めで要求 8 に対し 9 になる (fast-path 導入前から
		// そうなので本修正の責任範囲ではない。ここでは「変えていないこと」を主張する)
		for _, in := range []string{s + "abc", "─┌┐abc", strings.Repeat("→", 7) + "x"} {
			for _, wd := range []int{4, 8, 15} {
				drop := ansi.StringWidth(in) - wd + ansi.StringWidth("…")
				lib := in
				if drop > 0 {
					lib = ansi.TruncateLeft(in, drop, "…")
				}
				if got := truncateDispLeft(in, wd, "…"); got != lib {
					t.Fatalf("EASTASIAN=1: truncateDispLeft(%q,%d) が ansi 基準と違う: %q != %q",
						in, wd, got, lib)
				}
			}
		}
		return
	}
	// 子で走らせるテスト集合。⚠️ 幅系だけに絞る: suite 全体を EASTASIAN=1 で走らせると、
	// 幅 1 を期待値に焼いている既存テストが 22 本 fail する (本修正の前からそう。
	// この env は元々未サポート領域)。ここで幅系を**まとめて**走らせるのが要点で、
	// 子が自分 1 本しか走らないと TestSymbolWidthTableMatchesLibrary が EASTASIAN 環境で
	// 一度も走らず、「幅を 1 と決め打ちする退行」を守るテストが実質 1 本になる (R3 の指摘)。
	//
	// ⚠️ run filter に t.Name() を使う: リテラルで書くと関数名を変えたときに無言で
	// 食い違い、子は「no tests to run」で 0 本走って exit 0 になり、親は緑を返す (R3 が実証)。
	filter := "^(" + t.Name() + "|TestSymbolWidthTableMatchesLibrary|" +
		"TestFastDispWidthMatchesLibrary|TestAcceptedSymbolsNeverCombineWithEachOther)$"
	cmd := exec.Command(os.Args[0], "-test.run="+filter, "-test.v")
	cmd.Env = append(os.Environ(), "GLOGX_EAW_CHILD=1", "RUNEWIDTH_EASTASIAN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("RUNEWIDTH_EASTASIAN=1 の子プロセスが失敗した: %v\n%s", err, out)
	}
	// ⚠️ 「落ちなかったこと」を成功の根拠にしない。0 本走っても exit 0 になるため、
	// 期待した本数が実際に PASS したことを出力で確かめる (これが無いと上の filter が
	// 壊れた瞬間に検証が消える)。
	const wantPass = 4
	if got := strings.Count(string(out), "--- PASS"); got != wantPass {
		t.Fatalf("子プロセスで PASS したのが %d 本 (want %d)。run filter が壊れて "+
			"検証が消えている可能性がある\n%s", got, wantPass, out)
	}
}

// 受理してはいけないもの: 結合してクラスタを伸ばす文字。これらが混ざったら
// fast-path は必ず失敗し、ansi へ落ちなければならない。
func TestFastDispWidthRejectsCombining(t *testing.T) {
	cases := []struct {
		name string
		s    string
	}{
		{"VS16 付き警告記号", "⚠\ufe0f"},
		{"ZWJ 絵文字", "\U0001f468\u200d\U0001f4bb"},
		{"regional indicator (国旗)", "\U0001f1ef\U0001f1f5"},
		{"結合ダイアクリティカル", "é"},
		{"keycap", "1\ufe0f\u20e3"},
		{"CJK", "漢字"},
		{"全角スペース", "\u3000"},
		{"ZWSP", "a\u200bb"},
		{"soft hyphen", "a\u00adb"},
		{"Prepend (先行が飲む)", "\u0600─"},
		// ⚠️ 次の 2 件は「幅が壊れる」ケースではなく、**SGR 以外は ansi に委ねる**という
		// 実装の厳しさを固定している (この 2 つの幅は自前で数えても偶然正しい)。CSI の
		// 扱いを意図的に広げる変更をするなら、この 2 件は正しさではなく方針の変更として
		// 落とすことになる (R3 の指摘)。
		{"SGR 以外の CSI (消去)", "\x1b[2Ka"},
		{"カーソル移動 CSI", "\x1b[10;20Ha"},
		{"途中で切れた SGR", "\x1b[38;5;"},
		{"ESC 単体", "\x1b"},
		{"ESC の次が [ でない", "\x1bXa"},
		{"OSC", "\x1b]0;title\x07"},
		{"8bit CSI", "\u009b31ma"},
		{"DEL", "a\x7fb"},
		{"タブ", "a\tb"},
		{"不正な UTF-8", "a\xffb"},
	}
	for _, c := range cases {
		if w, ok := fastDispWidth(c.s); ok {
			t.Errorf("%s: fast-path が受理してしまった (w=%d)。ansi に委ねるべき: %q",
				c.name, w, c.s)
		}
	}
}

// 受理する形は必ず ansi.StringWidth と一致すること (代表例)。
func TestFastDispWidthMatchesLibrary(t *testing.T) {
	cases := []string{
		"",
		"plain ascii",
		"\x1b[32m+\x1b[0mvar x = 1",
		"\x1b[38;5;214mcolored\x1b[m",
		"\x1b[m",
		strings.Repeat("─", 120),
		"┌" + strings.Repeat("─", 40) + "┐",
		"✓ build  ✗ lint  ● test  ⊘ skip  ↑ unpushed",
		"⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏", // スピナーの全コマ
		"truncated…",
		"a·b•c‥d…e‹f›g",
		"\x1b[1m┃\x1b[0m ▰▰▱▱▱ 40%",
		"→ ← ↑ ↓ ▶ ▸ ◐ ◦ ○ ■ ☐ ☑ ⚠ ❯ ⏸ █▓▒░▏▌",
	}
	for _, s := range cases {
		lib := ansi.StringWidth(s)
		w, ok := fastDispWidth(s)
		if !ok {
			// 受理しないのは安全側。ただし上の代表例は受理してほしいので気づけるようにする
			t.Errorf("受理されなかった (fast-path の取りこぼし): %q", s)
			continue
		}
		if w != lib {
			t.Errorf("幅が違う: fast=%d ansi=%d for %q", w, lib, s)
		}
		if got := dispWidth(s); got != lib {
			t.Errorf("dispWidth=%d ansi=%d for %q", got, lib, s)
		}
	}
}

// 差分 fuzz: dispWidth は入力が何であれ ansi.StringWidth と一致しなければならない。
// fast-path が受理する/しないの境界を fuzzer に探させる (手で選んだ表では境界を外す)。
//
//	go test -run '^$' -fuzz FuzzDispWidthMatchesLibrary -fuzztime=60s .
func FuzzDispWidthMatchesLibrary(f *testing.F) {
	seeds := []string{
		"", "abc", "\x1b[32mx\x1b[0m", "─│┌┐", "⚠\ufe0f", "漢字", "é",
		"\U0001f1ef\U0001f1f5", "\x1b[2K", "\x1b[38;5;", "a\x7f", "\u200d",
		"✓✗●⊘↑…→⠋", "\x1b[m\x1b[m", "\xff\xfe", "1\ufe0f\u20e3", "\u3000", "\u0600─",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := dispWidth(s)
		if w := ansi.StringWidth(s); got != w {
			t.Fatalf("dispWidth=%d ansi.StringWidth=%d for %q", got, w, s)
		}
	})
}
