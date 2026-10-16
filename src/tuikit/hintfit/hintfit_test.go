package hintfit

import (
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
)

func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	m.Run()
}

// 入らない項目は切らずに落とし、より短い後続は入れる。残った項目は元の並び順で繋ぐ。
func TestFitDropsWholeItemsInOrder(t *testing.T) {
	items := []Item{{"a: 長い長い案内", 3}, {"b: 短い", 4}, {"q: 終了", 1}}
	got := Fit(termwidth.Of("b: 短い  q: 終了"), "  ", items)
	if got != "b: 短い  q: 終了" {
		t.Errorf("Fit = %q (入らない a を落とし、短い b は入れて、並びは元のまま)", got)
	}
	if got := Fit(80, " • ", items); got != "a: 長い長い案内 • b: 短い • q: 終了" {
		t.Errorf("広い幅: %q", got)
	}
	for w := range 40 {
		if got := Fit(w, "  ", items); strings.Contains(got, "…") {
			t.Fatalf("w=%d: 語を切った: %q", w, got)
		}
	}
}

// 優先度 1 は席を予約する (優先度は採る順序を決めるだけなので、以前は出口 (17 桁) が入らない幅で、より短い低優先 (9 桁) だけが残った)。
func TestFitReservesExit(t *testing.T) {
	items := []Item{
		{"j/k: 移動", 2},      // 9 桁
		{"D/q/esc: 閉じる", 1}, // 17 桁 = 出口
		{"y: URL コピー", 4},
	}
	for w := 1; w < termwidth.Of("D/q/esc: 閉じる"); w++ {
		got := Fit(w, "  ", items)
		if !strings.Contains(got, "閉じる") || strings.Contains(got, "j/k") {
			t.Fatalf("w=%d: 出口が入らない幅で低優先が出た・出口が消えた: %q", w, got)
		}
	}
	wide := Fit(80, "  ", items)
	for _, want := range []string{"D/q/esc: 閉じる", "j/k: 移動", "y: URL コピー"} {
		if !strings.Contains(wide, want) {
			t.Errorf("広い幅で %q が出ていない: %q", want, wide)
		}
	}
	// 🚨 優先度 1 が複数あるとき、狭い幅では短い方が残る (これを見ないと最長を返す退行が緑で通った)
	two := []Item{{"j/k: 移動", 2}, {"s: 一覧へ戻る (長い方)", 1}, {"q: 終了", 1}}
	for w := 1; w < termwidth.Of("q: 終了"); w++ {
		if got := Fit(w, "  ", two); got != "q: 終了" {
			t.Fatalf("w=%d: 優先度 1 が 2 つあるとき短い方を返さない: %q", w, got)
		}
	}
	if ShortestExit([]Item{{"x", 2}}) != "" {
		t.Error("優先度 1 が無いのに出口を返した")
	}
}

// 優先度の値は飛んでいてもよい (1..7 に限らない。mac-lan-manager は 1..11 を使う)。
func TestFitAnyPrioRange(t *testing.T) {
	items := []Item{{"low", 40}, {"mid", 12}, {"q", 1}}
	if got := Fit(termwidth.Of("mid  q"), "  ", items); got != "mid  q" {
		t.Errorf("Fit = %q (12 を 40 より先に採る)", got)
	}
}
