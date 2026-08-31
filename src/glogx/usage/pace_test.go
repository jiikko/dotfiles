package usage

import (
	"strings"
	"testing"
	"time"

	"glogx/sgr"
	"glogx/termwidth"
)

// ゲージは「[ + 番号と空白の 2 カラム x スロット数 + 左端の余白 + ]」= 2n+3 桁ちょうど。
// 色を付けても表示幅は変わらない (SGR は幅 0)。ここが崩れるとカード間で縦が揃わない。
func TestPaceGaugeWidth(t *testing.T) {
	for _, cells := range []int{1, 5, 7, 9} {
		for _, colored := range []bool{false, true} {
			got := paceGauge(cells, 40, 50, 1, colored)
			if w := termwidth.Of(got); w != cells*2+3 {
				t.Errorf("cells=%d colored=%v: 幅 %d, want %d (%q)", cells, colored, w, cells*2+3, got)
			}
		}
	}
}

// 目盛りの番号が 1..n まで出る (「窓を何等分した何番目か」が読めることがゲージの本体)。
func TestPaceGaugeShowsSlotNumbers(t *testing.T) {
	got := paceGauge(7, 34, 52, 3, false)
	if want := "[ 1 2 3 4 5 6 7 ]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// 10 等分以上は番号が 2 桁になって格子が崩れるので出さない (呼び出し側が素のバーへ落ちる)。
func TestPaceGaugeRejectsTooManyCells(t *testing.T) {
	if got := paceGauge(10, 50, 50, 0, false); got != "" {
		t.Errorf("cells=10 で %q (空であるべき)", got)
	}
	if got := paceGauge(0, 50, 50, 0, false); got != "" {
		t.Errorf("cells=0 で %q", got)
	}
}

// 塗りの意味: 想定内は緑背景 / 前借りは赤背景 / 使い残しは青 / 未来は暗灰。
// 色が意味を持つ唯一の場所なので、消費と経過の大小で色が入れ替わることを固定する。
func TestPaceGaugeColorsByPace(t *testing.T) {
	// 消費 80% > 経過 20%: 前借りなので赤背景が出て、青 (使い残し) は出ない。
	over := paceGauge(5, 80, 20, 1, true)
	if !strings.Contains(over, sgr.BgRedOnBlack) {
		t.Error("前借りなのに赤背景が無い")
	}
	if strings.Contains(over, sgr.BrightBlue) {
		t.Error("前借りなのに使い残し (青) が出ている")
	}
	// 消費 20% < 経過 80%: 使い残しなので青が出て、赤背景は出ない。
	under := paceGauge(5, 20, 80, 3, true)
	if !strings.Contains(under, sgr.BrightBlue) {
		t.Error("使い残しなのに青が無い")
	}
	if strings.Contains(under, sgr.BgRedOnBlack) {
		t.Error("使い残しなのに前借り (赤背景) が出ている")
	}
	// 想定どおりに消費: 緑背景。
	if onPace := paceGauge(5, 60, 60, 2, true); !strings.Contains(onPace, sgr.BgGreenOnBlack) {
		t.Error("想定内の消化に緑背景が無い")
	}
}

// いま居るスロットの番号に下線を引く (どこまで来たかの現在地)。
func TestPaceGaugeUnderlinesCurrentSlot(t *testing.T) {
	got := paceGauge(5, 50, 50, 2, true)
	if !strings.Contains(got, sgr.UnderlineBold+"3") {
		t.Errorf("3 番目のスロットに下線が無い: %q", got)
	}
	if strings.Contains(paceGauge(5, 50, 50, -1, true), sgr.UnderlineBold) {
		t.Error("現在地不明 (-1) なのに下線を引いた")
	}
}

// 助言は「状態語で言えないこと」だけを持つ。適正では出さない (語が既に言っている)。
func TestPaceAdvice(t *testing.T) {
	day := 7 * 24 * time.Hour
	cases := []struct {
		name    string
		used    int
		elapsed float64
		span    time.Duration
		cells   int
		want    string
	}{
		{"適正は出さない", 62, 64, 5 * time.Hour, 5, ""},
		{"上限はリセット待ち", 100, 50, day, 7, "リセット待ち"},
		{"weekly の前借り", 78, 66, day, 7, "0.8日分の前借り"},
		{"weekly の余り", 30, 66, day, 7, "2.5日分の余り"},
		{"5h は帯が広い", 80, 60, 5 * time.Hour, 5, ""},
		{"5h の前借り", 90, 60, 5 * time.Hour, 5, "1.5時間分の前借り"},
	}
	for _, c := range cases {
		if got := paceAdvice(c.used, c.elapsed, c.span, c.cells); got != c.want {
			t.Errorf("%s: paceAdvice(%d, %v) = %q, want %q", c.name, c.used, c.elapsed, got, c.want)
		}
	}
}

// 1 スロットあたりの予算。残りが 1 スロット未満のときは %/スロット を出さない
// (「残 12 時間で 110%/日」はその 1 日が来ないので実行不能な数字になる)。
func TestPaceBudget(t *testing.T) {
	day := 7 * 24 * time.Hour
	if got := paceBudget(30, 3*24*time.Hour, day, 7); got != "23.3%/日" {
		t.Errorf("weekly = %q", got)
	}
	if got := paceBudget(30, 12*time.Hour, day, 7); got != "残枠70%" {
		t.Errorf("残り 1 日未満 = %q (残枠を出すべき)", got)
	}
	// 超過 (used > 100) でも負の残枠は出さない。0 まで倒して「もう配れない」と読める形にする。
	if got := paceBudget(120, 3*time.Hour, 5*time.Hour, 5); got != "0.0%/時" {
		t.Errorf("超過時 = %q (負の残枠を出さない)", got)
	}
	if got := paceBudget(30, time.Hour, 0, 5); got != "" {
		t.Errorf("窓幅不明 = %q (空であるべき)", got)
	}
}

// ダッシュボードにはゲージ・目盛り・想定・乖離・予算・助言が揃って出る
// (ユーザー要望 2026-08-31: ゲージを出すなら目盛りとペースへの助言も)。
func TestRenderDashboardShowsGaugeAndAdvice(t *testing.T) {
	all := strings.Join(RenderDashboard(dialTestSnap(), dialTestNow(), 120, 40, false), "\n")
	for _, want := range []string{
		"[ 1 2 3 4 5 ]",     // 5h の目盛り
		"[ 1 2 3 4 5 6 7 ]", // weekly の目盛り
		"想定 66%",            // 想定ペース
		"+11.9pt 先行",        // 乖離と状態語
		"0.8日分の前借り",         // 助言
		"%/日",               // 1 スロットあたりの予算
	} {
		if !strings.Contains(all, want) {
			t.Errorf("%q が出ていない:\n%s", want, all)
		}
	}
}
