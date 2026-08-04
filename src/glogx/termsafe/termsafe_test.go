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
