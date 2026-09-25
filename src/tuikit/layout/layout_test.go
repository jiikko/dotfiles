package layout

import (
	"math"
	"strings"
	"testing"

	"tuikit/termwidth"
)

var geo = DrawerGeometry{Ratio: 0.8, Extra: 10, MinList: 8, MaxPeek: 18}

func TestDrawerTarget(t *testing.T) {
	for _, tc := range []struct {
		name        string
		g           DrawerGeometry
		total, want int
	}{
		{"広い端末は覗き見が上限で止まる", geo, 312, 312 - 18},
		{"中くらいは比率 + 上乗せ", geo, 100, 90},
		{"覗き見の余地が無い狭さは全幅", geo, 16, 16},
		{"最小幅は残す", geo, 40, 32},
		{"上限なし (MaxPeek=0) は比率 + 上乗せのまま", DrawerGeometry{Ratio: 0.8, Extra: 10, MinList: 8}, 312, 260},
	} {
		if got := tc.g.Target(tc.total); got != tc.want {
			t.Errorf("%s: Target(%d) = %d, want %d", tc.name, tc.total, got, tc.want)
		}
	}
	for total := 1; total <= 400; total++ {
		got := geo.Target(total)
		if got > total {
			t.Fatalf("total=%d: 本文 %d が画面幅を超えた", total, got)
		}
		if total > geo.MinList*2 && total-got > geo.MaxPeek {
			t.Fatalf("total=%d: 覗き見 %d 桁が上限 %d を超えた", total, total-got, geo.MaxPeek)
		}
		// 本文は比率ぶんより狭くしない (MinList は Extra の分しか抑えない。DrawerGeometry の doc)
		if ratio := int(math.Round(float64(total) * geo.Ratio)); total > geo.MinList*2 && got < ratio {
			t.Fatalf("total=%d: 本文 %d が比率ぶん %d より狭い", total, got, ratio)
		}
	}
}

// 合成した各行は、中身に ANSI や全角が混ざっていても画面幅ちょうど (枠が崩れない)。
func TestComposeDrawerKeepsWidth(t *testing.T) {
	const total = 40
	base := []string{"\x1b[31m→ 014 ○ research\x1b[0m の一覧行が長く続く……", "短い", ""}
	panel := []string{"# 見出し\x1b[1m太字\x1b[0m", "本文の段落が画面より長く続く場合でも切られる", "x"}
	for _, colored := range []bool{false, true} {
		for w := 1; w <= total; w++ {
			out := ComposeDrawer(base, panel, w, total, colored)
			if len(out) != len(base) {
				t.Fatalf("w=%d: 行数が変わった %d -> %d", w, len(base), len(out))
			}
			for i, ln := range out {
				if got := termwidth.Of(ln); got != total {
					t.Fatalf("colored=%v w=%d 行 %d: 幅 %d, want %d: %q", colored, w, i, got, total, ln)
				}
			}
		}
	}
}

// 板は右外から位置だけを変えて入る: 見えているのは本文の**左端から** w-1 桁。
func TestComposeDrawerShowsPanelFromLeftEdge(t *testing.T) {
	out := ComposeDrawer([]string{strings.Repeat("a", 20)}, []string{"0123456789"}, 5, 20, false)
	if want := strings.Repeat("a", 15) + "▏" + "0123"; out[0] != want {
		t.Fatalf("got %q, want %q", out[0], want)
	}
	if got := ComposeDrawer([]string{"base"}, []string{"p"}, 0, 20, false); got[0] != "base" {
		t.Fatalf("幅 0 で一覧が変わった: %q", got[0])
	}
}

func TestSlideIn(t *testing.T) {
	window := []string{"one", "two", "three", "four"}
	if got := SlideIn(window, 1, 20, false, 0.35); strings.Join(got, "|") != strings.Join(window, "|") {
		t.Fatalf("着地 (progress=1) で窓が変わった: %q", got)
	}
	for _, ln := range SlideIn(window, 0, 20, false, 0.35) {
		if ln != "" {
			t.Fatalf("開始 (progress=0) で画面内に行がある: %q", ln)
		}
	}
	// stagger があると上の行ほど先に入ってくる (横ずれが小さい)
	// (0.5 は全行が画面内に居て、まだ着地していない行もある進捗。実測: 0 / 1 / 4 / 9 桁)
	mid := SlideIn(window, 0.5, 20, false, 0.35)
	shift := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
	for i := 1; i < len(mid); i++ {
		if mid[i] == "" || shift(mid[i]) <= shift(mid[i-1]) {
			t.Fatalf("行 %d が上の行より先に入っている / 画面外: %q", i, mid)
		}
	}
	// 出ていくときは全行同時・等速 (着地点が無いので、ずらすと最上行が遅れて残る)
	// 等速 = 横ずれが進捗に比例する (width 20 で残り 0.25 なら 5 桁)。減速を掛けると終端で
	// ほぼ動かなくなり、この位置にいない
	out := SlideIn(window, 0.75, 20, true, 0.35)
	for i := range out {
		if got := len(out[i]) - len(window[i]); got != 5 {
			t.Fatalf("閉じるときの行 %d の横ずれ = %d, want 5 (全行同時・等速): %q", i, got, out)
		}
	}
}

// issue 416: ASCII + VS16 (キーキャップ) を含む行で、合成した行が 1 桁はみ出していた
// (ansi.Truncate がこのクラスタを幅 1 と数えて切るため。termwidth.Truncate が測り直す)。
func TestComposeKeepsWidthWithKeycaps(t *testing.T) {
	const keycap = "1️⃣"
	for _, colored := range []bool{false, true} {
		for i, ln := range ComposeDrawer([]string{keycap, keycap + "a"}, []string{keycap + keycap}, 11, 12, colored) {
			if w := termwidth.Of(ln); w != 12 {
				t.Errorf("colored=%v 行 %d: ComposeDrawer の幅 %d (want 12): %q", colored, i, w, ln)
			}
		}
		for i, ln := range Scrollbar([]string{keycap + "a", "b"}, 4, 3, 0, colored) {
			if w := termwidth.Of(ln); w > 4 {
				t.Errorf("colored=%v 行 %d: Scrollbar の幅 %d > 4: %q", colored, i, w, ln)
			}
		}
	}
}
