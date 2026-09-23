package widthenv

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// このパッケージ自身も支持しない env の下では走らない。
// 🚨 自分が定義したガードを自分に掛け忘れると、TestAmbiguousIsNarrowByDefault が
// 「ansi.StringWidth("─")=2, want 1」の生ログを吐き、この仕組みが消そうとしている混乱が
// 当の本人から漏れる (実測 2026-08-15 の敵対的レビュー)。
func TestMain(m *testing.M) {
	ExitIfUnsupported()
	os.Exit(m.Run())
}

// 検出そのものが死ぬと、実行時の警告もテストのガードも黙って発火しなくなる (= 支持しない
// 判断が無言で無効化される)。真偽の解釈は x/ansi の init と揃っている必要があるので、
// ここで表として固定する。
func TestEastAsianAmbiguousParsesLikeAnsi(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"1", true}, {"true", true}, {"TRUE", true}, {"True", true}, {"t", true},
		{"0", false}, {"false", false}, {"", false}, {"yes", false}, {"2", false},
	} {
		t.Setenv(Name, tc.val)
		if got := EastAsianAmbiguous(); got != tc.want {
			t.Errorf("%s=%q: EastAsianAmbiguous()=%v, want %v", Name, tc.val, got, tc.want)
		}
	}
}

// env が立っていないときに偽を返すこと (env 未設定と空文字を区別しない)。
func TestEastAsianAmbiguousUnset(t *testing.T) {
	t.Setenv(Name, "")
	if EastAsianAmbiguous() {
		t.Fatalf("%s 未設定相当で真になった", Name)
	}
}

// 🚨 このパッケージが前提にしている「x/ansi は Ambiguous を既定で幅 1 と数える」を、
// ライブラリ側の実測で固定する。ここが 2 に変わったら (= 既定が変わったら) 支持しない
// 判断の前提そのものが変わるので、Message の文言と issue 054 を読み直すこと。
// 逆に、この env が glogx の描画を壊す理由 (罫線が 2 セルになる) もここに現れている。
func TestAmbiguousIsNarrowByDefault(t *testing.T) {
	// このテストプロセスは env 無しで走ることが上の TestMain で保証されている
	for _, s := range []string{"─", "│", "…", "·", "→"} {
		if w := ansi.StringWidth(s); w != 1 {
			t.Errorf("ansi.StringWidth(%q)=%d, want 1 (既定の幅モデルが変わった可能性)", s, w)
		}
	}
	// 枠を組むときの前提: グリフ数 = 表示幅 が既定では成り立つ
	if w := ansi.StringWidth(strings.Repeat("─", 10)); w != 10 {
		t.Errorf("罫線 10 個の幅が %d (want 10)", w)
	}
}
