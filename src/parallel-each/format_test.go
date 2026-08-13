package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// visibleLen / truncate は「端末の桁」を数える (tui.go が m.width から引き算した残り桁に
// 合わせて使う)。⚠️ rune 数で数えると全角・絵文字で桁がずれ、罫線や列区切りが崩れる。
//
// ⚠️ オラクルは**手書きの期待値**にする。ansi.StringWidth と比べるだけだと
// 「実装と同じ関数で検算する」同義反復になり、幅モデルそのものの誤り (wcwidth への差し替え・
// 上流の更新) を検出できない。値は実測で確定させたもの。
func TestVisibleLenColumnsAgainstHandWrittenTable(t *testing.T) {
	for _, tt := range []struct {
		s    string
		want int
	}{
		{"abc", 3},
		{"", 0},
		{"日本語", 6},                  // 全角は 2 桁
		{"ログ: ok", 8},               // 全角 2 + ": ok" 4
		{"\x1b[31m赤\x1b[0m", 2},     // ANSI は幅 0
		{"\x1b[1m進捗\x1b[0m 12%", 8}, // 全角 2 + " 12%" 4
		{"⚠️", 2},                   // VS16 付き絵文字は 2 桁 (runewidth は 1 と数える)
		{"🇯🇵", 2},                   // 国旗 (regional indicator の組)
		{"①", 1},                    // East Asian Ambiguous は 1 桁 (GraphemeWidth)
		{"→", 1},                    // 同上
		{"a\tb", 2},                 // ⚠️ タブは幅 0。truncate 側で空白へ均す
	} {
		t.Run(tt.s, func(t *testing.T) {
			if got := visibleLen(tt.s); got != tt.want {
				t.Errorf("visibleLen(%q) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}

// 幅モデルが描画側 (lipgloss) と一致していること。⚠️ ここが食い違うと「測る側と描く側が
// 別モデル」になり、収めたつもりが 1 桁溢れる原因がどちらの責任か分からなくなる。
func TestVisibleLenMatchesLipgloss(t *testing.T) {
	for _, s := range []string{"abc", "日本語", "⚠️", "🇯🇵", "①", "→", "\x1b[31m赤\x1b[0m"} {
		if got, want := visibleLen(s), lipgloss.Width(s); got != want {
			t.Errorf("描画側と幅が違う: visibleLen(%q)=%d lipgloss.Width=%d", s, got, want)
		}
	}
}

// truncate は「max 桁に収める」もの。⚠️ rune 数で切ると全角で 2 倍の桁を出し、枠を越える。
// タブ (端末では次のタブストップまで伸びる) を含む行でも約束を守る。
func TestTruncateFitsColumns(t *testing.T) {
	for _, tt := range []struct {
		name string
		s    string
		max  int
	}{
		{name: "ASCII は切らない", s: "abc", max: 10},
		{name: "ASCII を切る", s: "abcdefghij", max: 5},
		{name: "全角を切る", s: "あいうえおかきくけこ", max: 6},
		{name: "全角ちょうど", s: "あいう", max: 6},
		{name: "全角 1 文字が入らない幅", s: "あいう", max: 3},
		{name: "ANSI 装飾つきを切る", s: "\x1b[31m赤青黄緑\x1b[0m", max: 4},
		{name: "絵文字を切る", s: "⚠️⚠️⚠️", max: 3},
		{name: "タブ区切り (go test / make の出力)", s: "col1\tcol2\tcol3\tcol4\tcol5", max: 10},
		{name: "タブだけ", s: "\t\t\t", max: 4},
		{name: "タブと全角の混在", s: "名前\tあいうえお", max: 8},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.max)
			if w := ansi.StringWidth(got); w > tt.max {
				t.Errorf("max=%d 桁に収まっていない: 幅=%d (%q → %q)", tt.max, w, tt.s, got)
			}
			if strings.Contains(got, "\t") {
				t.Errorf("タブが残っている (実端末で次のタブストップまで伸びる): %q", got)
			}
		})
	}
}

// 溢れたことが分かるように省略記号を付ける (2 桁以上あるとき)。
func TestTruncateMarksTrimmed(t *testing.T) {
	if got := truncate("abcdef", 4); !strings.HasSuffix(got, "…") {
		t.Errorf("切ったのに省略記号が付かない: %q", got)
	}
	if got := truncate("abc", 3); strings.Contains(got, "…") {
		t.Errorf("切っていないのに省略記号が付いた: %q", got)
	}
}

// 切り口で装飾を閉じる。⚠️ 閉じないと中身のない色指定が末尾に残り、以降の要素や次の行まで
// 染める (実測: "…\x1b[32m")。reset は幅 0 なので桁の約束は壊れない。
func TestTruncateClosesStyling(t *testing.T) {
	for _, tt := range []struct{ name, s string }{
		{name: "途中で色が変わる", s: "\x1b[38;5;196mfoo\x1b[0m bar \x1b[32mbaz"},
		{name: "reset が無い", s: "\x1b[31mred no reset here at all"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, 8)
			if !strings.HasSuffix(got, "\x1b[0m") {
				t.Errorf("装飾が開いたまま (次の行へ色が漏れる): %q", got)
			}
			if w := ansi.StringWidth(got); w > 8 {
				t.Errorf("reset を足して桁が増えた: 幅=%d (%q)", w, got)
			}
		})
	}
	if got := truncate("abcdefghij", 5); strings.Contains(got, "\x1b") {
		t.Errorf("装飾が無い文字列に reset を足している: %q", got)
	}
}

// max の境界の契約。⚠️ max == 1 は「省略記号で潰さず中身を優先」(元の実装からの意図的な挙動)。
// 呼び出し側は m.width から引き算した値を渡すので、極小端末で 0 以下・1 が実際に来る。
func TestTruncateEdgeContract(t *testing.T) {
	for _, tt := range []struct {
		name string
		s    string
		max  int
		want string
	}{
		{name: "max=0 は空", s: "abc", max: 0, want: ""},
		{name: "max が負でも空", s: "abc", max: -5, want: ""},
		{name: "ちょうど収まるものは切らない", s: "abc", max: 3, want: "abc"},
		{name: "max=1 は中身を優先 (省略記号で潰さない)", s: "abc", max: 1, want: "a"},
		{name: "max=1 に全角は入らない", s: "あ", max: 1, want: ""},
		{name: "max=2 は省略記号を付ける", s: "abc", max: 2, want: "a…"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.s, tt.max); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}
