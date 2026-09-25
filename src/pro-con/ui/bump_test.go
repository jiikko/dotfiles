package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// framePos は画面に描かれた選択の枠の左上の角 (┏) の (桁, 行)。見つからなければ (-1, -1)。
func framePos(m *Model) (int, int) {
	for r, l := range strings.Split(ansi.Strip(m.render()), "\n") {
		if i := strings.Index(l, "┏"); i >= 0 {
			return ansi.StringWidth(l[:i]), r
		}
	}
	return -1, -1
}

// labelCol はレーンの見出し (label) の表示桁。見つからなければ -1。
func labelCol(m *Model, label string) int {
	for _, l := range strings.Split(ansi.Strip(m.render()), "\n") {
		if i := strings.Index(l, label); i >= 0 {
			return ansi.StringWidth(l[:i])
		}
	}
	return -1
}

// peak は揺れが壁の方へ出きっている時刻 (bumpOffset が 1 になる所)。
const peak = bumpDuration * 15 / 100

// 上端 (カード 1 枚のレーン) で ↑ / k を押すと、レーンごと 1 行上へずれ、枠も一緒に動く。下端の ↓ / j は 1 行下。
// 下へずれても下枠は切らない (ボードの下の空き行へ出す)。
func TestBumpVerticalAtEdge(t *testing.T) {
	for _, tc := range []struct {
		key string
		dy  int
	}{{"k", -1}, {"j", 1}} {
		m, clk := cursorModel(t) // 作業中のレーンは R1 の 1 枚だけ
		x0, y0 := framePos(m)
		corners := strings.Count(ansi.Strip(m.render()), "╰") // レーンの下枠の左の角 (6 本)
		start := clk.t
		if cmd := press(m, tc.key); cmd == nil {
			t.Fatalf("%s: 端でぶつかったのに演出の tick が回らない", tc.key)
		}
		if m.selected != "R1" {
			t.Fatalf("前提: 端なので選択は動かない: %q", m.selected)
		}
		clk.t = start.Add(peak)
		if x, y := framePos(m); x != x0 || y != y0+tc.dy {
			t.Fatalf("%s: 出きった所で枠が (%d, %d) (期待 (%d, %d))", tc.key, x, y, x0, y0+tc.dy)
		}
		if got := strings.Count(ansi.Strip(m.render()), "╰"); got != corners {
			t.Fatalf("%s: 揺れたレーンの下枠が消えた (角 %d → %d)", tc.key, corners, got)
		}
	}
}

// 左端のレーンで ← / h、右端のレーンで → / l を押すと、そのレーンが押した向きへ 2 桁ずれる (空のレーンでも揺れる)。
func TestBumpHorizontalAtEdge(t *testing.T) {
	for _, tc := range []struct {
		key, label string
		lane, dx   int
	}{{"h", "1 依頼", 0, -2}, {"l", "6 完了", 5, 2}} {
		m, clk := cursorModel(t)
		m.focusLane(tc.lane, 0) // 依頼 / 完了のレーンは空
		m.Update(frameMsg{})
		clk.t = clk.t.Add(cursorDuration) // 枠とレーンの色が移り終わるのを待つ (tick を止める)
		m.Update(frameMsg{})
		x0 := labelCol(m, tc.label)
		if x0 < 0 {
			t.Fatalf("前提: 見出し %q が見つからない:\n%s", tc.label, ansi.Strip(m.render()))
		}
		start := clk.t
		if cmd := press(m, tc.key); cmd == nil {
			t.Fatalf("%s: 端でぶつかったのに演出の tick が回らない", tc.key)
		}
		clk.t = start.Add(peak)
		if got := labelCol(m, tc.label); got != x0+tc.dx {
			t.Fatalf("%s: 出きった所で見出しが %d 桁目 (期待 %d)", tc.key, got, x0+tc.dx)
		}
	}
}

// 端でなければ揺れない (動けたなら選択の枠が滑るだけ)。
func TestBumpOnlyAtEdge(t *testing.T) {
	m, _ := cursorModel(t)
	press(m, "l") // 作業中 → 質問待ち (W1)
	press(m, "j") // W1 → W2
	press(m, "k") // W2 → W1
	press(m, "h") // 質問待ち → 作業中
	if !m.bump.start.IsZero() {
		t.Fatalf("端でないのに揺れが始まった: %+v", m.bump)
	}
}

// 揺れは 1000 ms で止まり、画面は揺れる前と同じに戻る。tick も止まる。
func TestBumpStopsAfterDuration(t *testing.T) {
	m, clk := cursorModel(t)
	before := m.render()
	start := clk.t
	press(m, "k")
	clk.t = start.Add(bumpDuration - time.Millisecond)
	m.onFrame()
	if !m.animating() {
		t.Fatal("1000 ms より前に揺れが止まった")
	}
	clk.t = start.Add(bumpDuration)
	if m.onFrame() != nil || m.animating() {
		t.Fatal("1000 ms 経っても演出の tick が止まらない")
	}
	if got := m.render(); got != before {
		t.Fatalf("揺れの後に画面が戻らない:\n%s\n---\n%s", ansi.Strip(got), ansi.Strip(before))
	}
}

// 揺れの間も次のキーを受ける (演出が終わるのを待たせない)。
func TestBumpKeepsTakingKeys(t *testing.T) {
	m, clk := cursorModel(t)
	start := clk.t
	press(m, "k")
	clk.t = start.Add(peak)
	press(m, "l")
	if m.selected != "W1" {
		t.Fatalf("揺れの途中の l で質問待ちのレーンへ移るはず: %q", m.selected)
	}
}

// 画面が低くボードの下に空き行が無いときは、下へ揺れてもボードを伸ばさない (伸ばすと画面が 1 行増えて端末が送られる)。
func TestBumpDownKeepsScreenHeight(t *testing.T) {
	for _, h := range []int{12, 13, 14, 30} {
		m, clk := cursorModel(t)
		m.height = h
		lines := len(strings.Split(m.render(), "\n"))
		start := clk.t
		press(m, "j")
		clk.t = start.Add(peak)
		if got := len(strings.Split(m.render(), "\n")); got != lines {
			t.Fatalf("高さ %d: 下へ揺れて画面の行数が %d → %d に変わった", h, lines, got)
		}
	}
}
