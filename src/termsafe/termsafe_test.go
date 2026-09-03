package termsafe

import (
	"strings"
	"testing"
)

const (
	esc = "\x1b"
	bel = "\a"
)

// OSC (タイトル変更・OSC52 のクリップボード書き込み) はシーケンスごと消える。
// ⚠️ 「ESC が残っていない」だけでなく「中身の文字列も残っていない」ことを見る: ESC だけ落として
// 中身を素通しすると、端末には無害でも本文に "0;pwned" のようなゴミが出る。
func TestDropsOSCSequenceWholly(t *testing.T) {
	for _, fn := range []struct {
		name string
		f    func(string) string
	}{{"DetailLine", DetailLine}, {"LineKeepTabs", LineKeepTabs}, {"PlainLine", PlainLine}} {
		got := fn.f("before" + esc + "]0;pwned" + bel + "after")
		if got != "beforeafter" {
			t.Errorf("%s: OSC が丸ごと落ちていない: %q", fn.name, got)
		}
		// OSC52 は ST (ESC \) 終端も使う
		if got := fn.f("a" + esc + "]52;c;aGVsbG8=" + esc + `\` + "b"); got != "ab" {
			t.Errorf("%s: ST 終端の OSC が落ちていない: %q", fn.name, got)
		}
	}
}

// SGR 以外の CSI (画面消去・カーソル移動) は落とす。
func TestDropsNonSGRCSI(t *testing.T) {
	if got := DetailLine("a" + esc + "[2Jb"); got != "ab" {
		t.Errorf("画面消去 CSI が残った: %q", got)
	}
	if got := DetailLine("a" + esc + "[10;20Hb"); got != "ab" {
		t.Errorf("カーソル移動 CSI が残った: %q", got)
	}
}

// SGR の扱いが 3 関数で分かれる (ここが PlainLine を分けた理由)。
func TestSGRPolicyDiffersByVariant(t *testing.T) {
	in := "a" + esc + "[31mred" + esc + "[0m"
	if got := DetailLine(in); got != in {
		t.Errorf("DetailLine が SGR を落とした: %q", got)
	}
	if got := LineKeepTabs(in); got != in {
		t.Errorf("LineKeepTabs が SGR を落とした: %q", got)
	}
	if got := PlainLine(in); got != "ared" {
		t.Errorf("PlainLine が SGR を残した: %q", got)
	}
}

// タブの扱い: git 由来だけ保持 (静的出力のパリティ契約)、他は幅計算のためスペース 4 へ。
func TestTabPolicy(t *testing.T) {
	if got := LineKeepTabs("a\tb"); got != "a\tb" {
		t.Errorf("git 由来のタブが保持されていない: %q", got)
	}
	if got := DetailLine("a\tb"); got != "a    b" {
		t.Errorf("タブが展開されていない: %q", got)
	}
	if got := PlainLine("a\tb"); got != "a    b" {
		t.Errorf("タブが展開されていない: %q", got)
	}
}

// 制御文字 (C0 / DEL / BOM) は落ちる。
func TestDropsControlChars(t *testing.T) {
	if got := PlainLine("a\rb\x00c\x7fd\ufeffe"); got != "abcde" {
		t.Errorf("制御文字が残った: %q", got)
	}
}

// 制御文字を含まない多数派は素通り (fast path が中身を変えない)。
func TestPlainInputUnchanged(t *testing.T) {
	in := "ふつうの日本語 with ASCII 123"
	for _, f := range []func(string) string{DetailLine, LineKeepTabs, PlainLine} {
		if got := f(in); got != in {
			t.Errorf("無害な入力を変えてしまった: %q", got)
		}
	}
}

// VS16 は落として bare 記号にする (幅の解釈が層ごとに割れる字を出さない)。
func TestDropEmojiVS16(t *testing.T) {
	vs16 := string(rune(0xfe0f)) // 直接書くと不可視文字になるのでコードで組む
	got := DropEmojiVS16("警告 ⚠" + vs16 + " あり")
	if strings.Contains(got, vs16) {
		t.Errorf("VS16 が残った: %q", got)
	}
	if !strings.ContainsRune(got, '⚠') {
		t.Errorf("bare 記号まで消えた: %q", got)
	}
	if got := DropEmojiVS16("VS16 なし"); got != "VS16 なし" {
		t.Errorf("VS16 を含まない文字列を変えてしまった: %q", got)
	}
}

// 途切れたシーケンス (末尾の裸 ESC・終端の来ない CSI/OSC) でも panic せず、残骸も出さない。
func TestTruncatedSequences(t *testing.T) {
	for _, in := range []string{"a" + esc, "a" + esc + "[", "a" + esc + "[31", "a" + esc + "]0;x"} {
		if got := PlainLine(in); strings.Contains(got, esc) {
			t.Errorf("途切れたシーケンスの ESC が残った: in=%q got=%q", in, got)
		}
	}
}

// 8bit の C1 制御文字 (U+009B CSI / U+009D OSC / U+009C ST) も落とす。
//
// ⚠️ 最初の実装はここが素通しだった。fast path の述語と本体の drop 条件を別々に書いていて
// 片方にしか C1 が無かった (= 判定の二重実装) のが原因なので、needsSanitize / mustStrip を
// 共有する形に直した。テスト側も「ESC と BEL が無いこと」でなく「許可した文字だけが残ること」
// で判定する (前者だと C1 を原理的に見逃す)。
func TestDropsC1ControlChars(t *testing.T) {
	const (
		csi8 = "\u009b"
		osc8 = "\u009d"
		st8  = "\u009c"
	)
	cases := map[string]string{
		"8bit OSC":    "a" + osc8 + "0;PWNED" + st8 + "b",
		"8bit CSI":    "a" + csi8 + "2Jb",
		"C1 単体 (NEL)": "a\u0085b",
		"8bit DCS":    "a\u0090q#0" + st8 + "b",
	}
	for name, in := range cases {
		for _, f := range []func(string) string{DetailLine, LineKeepTabs, PlainLine, PlainLineKeepTabs} {
			got := f(in)
			if hasControl(got) {
				t.Errorf("%s: 制御文字が残った: %q", name, got)
			}
			if got != "ab" {
				t.Errorf("%s: シーケンスが丸ごと落ちていない: %q", name, got)
			}
		}
	}
}

// 終端の無いシーケンスは導入子だけ落として本文を残す (行末まで捨てると「文字を隠す」手段になる)。
func TestTruncatedSequenceKeepsPayloadAsText(t *testing.T) {
	cases := map[string]string{
		"BUILD " + esc + "]FAILED: 12 tests failed": "BUILD ]FAILED: 12 tests failed",
		"prefix " + esc + "[0123456789;:":           "prefix [0123456789;:",
		"prefix " + esc + "PBUILD FAILED":           "prefix PBUILD FAILED",
	}
	for in, want := range cases {
		if got := DetailLine(in); got != want {
			t.Errorf("DetailLine(%q) = %q; want %q", in, got, want)
		}
	}
}

// 制御文字だけが違う 2 つの入力が同じ表示に潰れない (status viewer は破壊的操作を持つので、
// ユーザーが行を区別できないまま捨てるキーを押せる状態を作らない)。
func TestSanitizeDoesNotCollapseDistinctInputs(t *testing.T) {
	a := PlainLine("keep" + esc + "]DELETE-ME.txt")
	b := PlainLine("keep" + esc + "]KEEP-ME.txt")
	if a == b {
		t.Errorf("別の入力が同じ表示に潰れた: %q", a)
	}
}

// 不正な UTF-8 は fast path / 本体で結果が割れない (どちらも U+FFFD へ正規化される)。
func TestInvalidUTF8IsNormalizedConsistently(t *testing.T) {
	if got := PlainLine("a\xffb"); got != "a\ufffdb" {
		t.Errorf("不正 UTF-8 の正規化 = %q, want a\\ufffdb", got)
	}
}

// hasControl は「端末が制御として解釈しうる文字が残っているか」(C0 / DEL / C1)。
// ⚠️ ESC と BEL だけを見る判定にすると 8bit の C1 を原理的に見逃す。
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

// PlainBlock は改行だけ残し、他は PlainLine と同じに落とす。
// 🚨 「改行を残す」と「行構造を偽装させない」は両立しない。両立させる側 (1 件 1 行) の
// 呼び出しが PlainBlock を使っていないことは、呼び出し側のテストが守る。
func TestPlainBlockKeepsOnlyNewlines(t *testing.T) {
	in := "Warning: a\nb" + esc + "[2J" + esc + "]52;c;cHduZWQ=" + bel + "\tc\rd" + esc + "[31me"
	got := PlainBlock(in)
	if want := "Warning: a\nb    cde"; got != want {
		t.Errorf("PlainBlock = %q, want %q", got, want)
	}
	if strings.ContainsAny(got, esc+bel+"\r") {
		t.Errorf("制御文字が残った: %q", got)
	}
	// 1 行版は改行も落とす (ここが唯一の差)
	if got := PlainLine("a\nb"); got != "ab" {
		t.Errorf("PlainLine が改行を残した: %q", got)
	}
	if got := PlainBlock("a\nb"); got != "a\nb" {
		t.Errorf("PlainBlock が改行を落とした: %q", got)
	}
}
