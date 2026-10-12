package filer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

func rowWidths(t *testing.T, lines []string, w int, what string) {
	t.Helper()
	for i, l := range lines {
		if got := termwidth.Of(termwidth.StripSGR(l)); got != w {
			t.Fatalf("%s: %d 行目の幅 = %d, want %d: %q", what, i, got, w, termwidth.StripSGR(l))
		}
	}
}

// 全角の片割れを上書きしても、行の幅は画面の幅のまま (左半分・右半分のどちらを上書きしても。issue 731)。
func TestOverwritingHalfOfWideKeepsRowWidth(t *testing.T) {
	for _, x := range []int{2, 3} { // 全角は 2〜3 桁目。2 = 左半分、3 = 右半分
		c := newCanvas(8, 1)
		c.put(2, 0, "あ", cText, false)
		c.put(x, 0, "│", cText, false)
		rowWidths(t, c.lines(), 8, "put")
		c = newCanvas(8, 1)
		c.put(2, 0, "あ", cText, false)
		c.clear(x, 0, x, 0, cBg)
		rowWidths(t, c.lines(), 8, "clear")
		c = newCanvas(8, 1)
		c.put(2, 0, "あ", cText, false)
		c.put(x-1, 0, "い", cText, false) // 全角で全角の片割れを踏む
		rowWidths(t, c.lines(), 8, "wide over wide")
	}
}

// 画面の外・空・逆向きの範囲の clear は、範囲の外の全角を割らない (敵対レビューで再現: 左半分だけが残って 1 桁長くなった)。
func TestClearOutsideRangeKeepsWide(t *testing.T) {
	for _, r := range [][2]int{{-5, -2}, {4, 2}, {10, 12}} {
		for _, x := range []int{0, 2, 4} {
			c := newCanvas(6, 1)
			c.put(x, 0, "あ", cText, false)
			c.clear(r[0], 0, r[1], 0, cBg)
			rowWidths(t, c.lines(), 6, "clear")
			if got := termwidth.StripSGR(c.lines()[0]); !strings.Contains(got, "あ") {
				t.Errorf("clear(%d..%d) が範囲の外の x=%d の全角を消した: %q", r[0], r[1], x, got)
			}
		}
	}
}

// 日本語の名前が並ぶ木でタイルを開いても、どの大きさの画面でも全行の幅が画面の幅 (調査体の再現: 390 通りで短い行が 48 本)。
func TestRealFramesKeepRowWidthWithWideNames(t *testing.T) {
	root := fixture(t)
	names := []string{"しりとり.txt", "日本語の名前のファイル.md", "あいうえおかきくけこさしすせそ.go"}
	for i := range 30 { // 全角の名前を木の全体に並べる (タイルの枠がどの列に落ちても、どこかの全角に重なるように)
		names = append(names, "ファイル名"+string(rune('ア'+i))+"の長い名前です"+".txt")
	}
	for _, d := range []string{"a", "日本語のフォルダ"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			mustWrite(t, filepath.Join(root, d, n), "本文の 1 行目\n二行目\n")
		}
	}
	for w := 30; w <= 160; w += 7 {
		for _, h := range []int{20, 24, 31} {
			m := newAt(t, root)
			m.Resize(w, h)
			settle(m)
			cdTo(t, m, "日本語のフォルダ/しりとり.txt")
			m.root.kids[0].expanded = true
			for _, k := range []string{"l", "J", "J", "q", "j", "j", "l"} { // タイルを開いて隣へ (枠が全角の名前に重なる)
				m.HandleKey(k)
				settle(m)
				rowWidths(t, m.View(), w, k)
			}
		}
	}
}

// 低い端末でキー一覧が収まらないときは、黙って切らずに残りの行数を出す (issue 731)。
func TestHelpShowsRestWhenTooShort(t *testing.T) {
	m := newTest(t)
	m.Resize(100, 20)
	settle(m)
	m.HandleKey("?")
	if v := strings.Join(m.View(), "\n"); !strings.Contains(termwidth.StripSGR(v), "残り") {
		t.Fatalf("20 行の端末で、切れたキー一覧に残りの印が無い")
	}
	m.Resize(100, 40)
	settle(m)
	if v := termwidth.StripSGR(strings.Join(m.View(), "\n")); strings.Contains(v, "残り") || !strings.Contains(v, "このキー一覧") {
		t.Fatalf("40 行の端末で全部出ない / 残りの印が出た")
	}
}
