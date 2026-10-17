package issuesview

import (
	"glogx/internal/app"
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/testenv"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/layout"
)

// openAnimView は演出を on にしたまま viewer を開いた状態を作る。
func openAnimView(t *testing.T) *IssuesView {
	t.Helper()
	v := NewIssuesView() // 演出 on (zero value = 本番の既定)
	v.Toggle(t.TempDir())
	v.root = t.TempDir() // screen() が覚えるのに要る (スキャン結果の代わり)
	v.finishAnim()       // 開く演出は着地させておく (見たいのは閉じる側)
	return &v
}

// 閉じるときは進捗が 1 から 0 へ落ち、0 で止まる。
func TestIssuesCloseProgressFallsToZero(t *testing.T) {
	advance := testenv.StubClock(t)

	v := openAnimView(t)
	if p := v.animProgress(); p != 1 {
		t.Fatalf("前提が崩れた: 開き切っているのに進捗が 1 でない (%v)", p)
	}

	v.close()
	if !v.Visible() {
		t.Fatal("閉じる演出の前に画面が消えた (抜けていく中身が無い)")
	}
	if p := v.animProgress(); p != 1 {
		t.Fatalf("閉じ始めが開き切った姿から始まっていない: %v", p)
	}
	advance(IssuesCloseDuration / 2)
	mid := v.animProgress()
	if mid <= 0 || mid >= 1 {
		t.Fatalf("途中の進捗が 0..1 でない: %v", mid)
	}
	advance(IssuesCloseDuration / 2)
	if p := v.animProgress(); p != 0 {
		t.Fatalf("所要を過ぎても画面外まで抜けていない: %v", p)
	}
	// 🚨 進捗は 0 で止まる (負に走らない)。負になると layout.SlideIn の局所進捗が壊れる。
	advance(IssuesCloseDuration)
	if p := v.animProgress(); p != 0 {
		t.Fatalf("進捗が 0 を下回った: %v", p)
	}
}

// 閉じる向きが描画まで届いている。🚨 上の検査は layout.SlideIn を直接叩くので、lines() が
// closing を渡し忘れても気づかない (開く向きの平坦な立ち上がりへ黙って戻る)。
func TestIssuesCloseCurveReachesRender(t *testing.T) {
	advance := testenv.StubClock(t)

	v := loadedView(issuesviewtest.SampleIssues()...)
	v.closeAnimOff = false
	v.close()
	// 折り返し地点で見る: 閉じる向きなら中身のある行が幅の半分ずれ、開く向き (渡し忘れ) なら
	// stagger で最下行しか動かない。そこは一覧の外なので空行 = ずれは数桁で止まる
	advance(IssuesCloseDuration / 2)

	shift := maxRowShift(v.Lines(renderOpts(12)))
	if shift < renderOpts(12).Width/4 {
		t.Fatalf("閉じる向きが描画へ届いていない (中間フレームのずれが %d 桁。開く向きの式なら数桁で止まる)", shift)
	}
}

// 閉じるときは全行が同時に抜ける (板が 1 枚右へ出ていく)。行ごとにずらすと視線のある最上行が
// 最後に回され、静止してから動き出す。
func TestIssuesCloseMovesAllRowsTogether(t *testing.T) {
	const width = 100
	window := make([]string, 20)
	for i := range window {
		window[i] = strings.Repeat("x", width)
	}

	out := layout.SlideIn(window, 0.5, width, true, IssuesAnimStagger)
	head := maxRowShift(out[:1])
	for i, ln := range out {
		if shift := maxRowShift([]string{ln}); shift != head {
			t.Fatalf("行 %d のずれが %d 桁で最上行 (%d 桁) と違う (行ごとにずれている)", i, shift, head)
		}
	}
}

// 演出が着地するまで tick を止めない (時間で降ろすと片付け前にチェーンが切れて固まる)。
func TestIssuesCloseKeepsTickingUntilSettled(t *testing.T) {
	advance := testenv.StubClock(t)

	v := openAnimView(t)
	v.close()
	advance(IssuesCloseDuration * 3) // とっくに時間は過ぎている
	if !v.Animating() {
		t.Fatal("片付け前に animating が false になった (最後の 1 拍が届かず閉じかけで固まる)")
	}
	v.SettleClose()
	if v.Animating() {
		t.Fatal("片付けた後も animating が true (tick が回り続ける)")
	}
	if v.Visible() {
		t.Fatal("着地しても viewer が閉じていない")
	}
}

// 着地するまで片付けない = 逆再生の間は見張りも本文も生きている。
func TestIssuesCloseDefersTeardownUntilSettled(t *testing.T) {
	advance := testenv.StubClock(t)

	root, path := issuesviewtest.WatchTree(t, "# a\n")
	v := openedWatchView(t, root, path)
	v.closeAnimOff = false // このテストだけ演出を on に戻す
	gen := v.watch.Gen

	v.close()
	if v.watch.Gen != gen {
		t.Fatal("演出中に見張りを畳んだ (逆再生の途中で中身が死ぬ)")
	}
	advance(IssuesCloseDuration)
	v.SettleClose()
	if v.watch.Gen == gen {
		t.Fatal("着地しても見張りが畳まれていない (watcher が残る)")
	}
}

// 演出の途中で終了しても画面を覚えない (閉じたはずの viewer が次の起動で蘇らない)。
func TestIssuesClosingIsNotRemembered(t *testing.T) {
	testenv.StubClock(t)

	v := openAnimView(t)
	if _, ok := v.screen(app.TimeNow()); !ok {
		t.Fatal("前提が崩れた: 開いているのに覚えない")
	}
	v.close()
	if _, ok := v.screen(app.TimeNow()); ok {
		t.Fatal("閉じる演出の途中を「開いている」として覚えた (次の起動で蘇る)")
	}
}

// 演出中の i は「閉じ切ってから開き直す」= 見張りを二重に張らない。
func TestIssuesReopenDuringCloseDoesNotDoubleWatch(t *testing.T) {
	testenv.StubClock(t)

	root, path := issuesviewtest.WatchTree(t, "# a\n")
	v := openedWatchView(t, root, path)
	v.closeAnimOff = false
	gen := v.watch.Gen

	v.close()
	v.Toggle(root) // 逆再生の途中で開き直す
	if !v.Visible() {
		t.Fatal("開き直せていない")
	}
	if v.closing {
		t.Fatal("閉じる演出が残ったまま開き直した (次の settleClose が開いた viewer を畳む)")
	}
	if v.watch.Gen == gen {
		t.Fatal("前の見張りを畳まずに開き直した (watcher が二重に居座る)")
	}
}
