package main

// issues viewer の「閉じる演出」(開く演出の逆再生) の検査。ユーザー要望 2026-08-01。
//
// ⚠️ 他のテストは newTestIssuesView / newTestBrowse で演出を切っている (close したら即座に
// 畳まれている前提で書かれているため)。ここだけ明示的に on にして演出そのものを見る。

import (
	"strings"
	"testing"
	"time"
)

// openAnimView は演出を on にしたまま viewer を開いた状態を作る。
func openAnimView(t *testing.T) *issuesView {
	t.Helper()
	v := newIssuesView() // 演出 on (zero value = 本番の既定)
	v.toggle(t.TempDir())
	v.root = t.TempDir() // screen() が覚えるのに要る (スキャン結果の代わり)
	v.finishAnim()       // 開く演出は着地させておく (見たいのは閉じる側)
	return &v
}

// 閉じるときは開く動きの逆再生になる (進捗が 1 から 0 へ落ちる)。
func TestIssuesCloseIsReversedOpen(t *testing.T) {
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }

	v := openAnimView(t)
	if p := v.animProgress(); p != 1 {
		t.Fatalf("前提が崩れた: 開き切っているのに進捗が 1 でない (%v)", p)
	}

	v.close()
	if !v.visible() {
		t.Fatal("閉じる演出の前に画面が消えた (逆再生する中身が無い)")
	}
	if p := v.animProgress(); p != 1 {
		t.Fatalf("閉じ始めが開き切った姿から始まっていない: %v", p)
	}
	now = now.Add(issuesAnimDuration / 2)
	mid := v.animProgress()
	if mid <= 0 || mid >= 1 {
		t.Fatalf("途中の進捗が 0..1 でない: %v", mid)
	}
	now = now.Add(issuesAnimDuration / 2)
	if p := v.animProgress(); p != 0 {
		t.Fatalf("所要を過ぎても画面外まで抜けていない: %v", p)
	}
	// ⚠️ 進捗は 0 で止まる (負に走らない)。負になると slideInWindow の局所進捗が壊れる。
	now = now.Add(issuesAnimDuration)
	if p := v.animProgress(); p != 0 {
		t.Fatalf("進捗が 0 を下回った: %v", p)
	}
}

// 閉じ始めの 1 フレームで目に見えて動く。開く動きをそのまま逆再生すると easeOutCubic の緩やかな
// 尾が立ち上がりの平坦部になり、最初のフレームでは 1 桁も動かない (esc の反応が鈍く見える)。
// 「動いたか」ではなく「1 フレームでどれだけ動いたか」を縛らないとこの平坦さを検出できない。
func TestIssuesCloseMovesOnFirstFrame(t *testing.T) {
	const width = 100
	window := make([]string, 20)
	for i := range window {
		window[i] = strings.Repeat("x", width)
	}
	// tick 1 拍 (scrollInterval) ぶん進んだ時点の進捗。閉じるので 1 から落ちる
	p := 1 - float64(scrollInterval)/float64(issuesAnimDuration)

	closing := maxRowShift(slideInWindow(window, p, width, true))
	if closing < width/10 {
		t.Fatalf("閉じ始めの 1 フレームで %d 桁しか動いていない (立ち上がりが潰れている)", closing)
	}
	// 開く側は右外から入ってくる = 1 フレーム目はまだ大きくずれている (逆向きの回帰よけ)
	if opening := maxRowShift(slideInWindow(window, 1-p, width, false)); opening <= closing {
		t.Fatalf("開き始めが右外から入ってきていない (ずれ %d 桁)", opening)
	}
}

// 閉じる向きが描画まで届いている。⚠️ 上の検査は slideInWindow を直接叩くので、lines() が
// closing を渡し忘れても気づかない (開く向きの平坦な立ち上がりへ黙って戻る)。
func TestIssuesCloseCurveReachesRender(t *testing.T) {
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }

	v := loadedView(sampleIssues()...)
	v.closeAnimOff = false
	v.close()
	// 折り返し地点で見る: 1 フレーム目は stagger で最下行しか動かず、そこが空行だと
	// 「渡し忘れ」と区別がつかない。中間なら中身のある行が大きくずれている
	now = now.Add(issuesAnimDuration / 2)

	shift := maxRowShift(v.lines(renderOpts(12)))
	if shift < 80/4 {
		t.Fatalf("閉じる向きが描画へ届いていない (中間フレームのずれが %d 桁。開く向きの式なら数桁で止まる)", shift)
	}
}

// 閉じるときは全行が同時に抜ける (板が 1 枚右へ出ていく)。行ごとにずらすと視線のある最上行が
// 最後に回され、0.25 秒静止してから動き出す。
func TestIssuesCloseMovesAllRowsTogether(t *testing.T) {
	const width = 100
	window := make([]string, 20)
	for i := range window {
		window[i] = strings.Repeat("x", width)
	}

	out := slideInWindow(window, 0.5, width, true)
	head := maxRowShift(out[:1])
	for i, ln := range out {
		if shift := maxRowShift([]string{ln}); shift != head {
			t.Fatalf("行 %d のずれが %d 桁で最上行 (%d 桁) と違う (行ごとにずれている)", i, shift, head)
		}
	}
}

// maxRowShift は窓の各行の右へのずれ (先頭の空白幅) の最大値。空行は演出で消えた行なので除く。
func maxRowShift(window []string) int {
	shift := 0
	for _, ln := range window {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		shift = max(shift, len(ln)-len(strings.TrimLeft(ln, " ")))
	}
	return shift
}

// 演出が着地するまで tick を止めない (時間で降ろすと片付け前にチェーンが切れて固まる)。
func TestIssuesCloseKeepsTickingUntilSettled(t *testing.T) {
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }

	v := openAnimView(t)
	v.close()
	now = now.Add(issuesAnimDuration * 3) // とっくに時間は過ぎている
	if !v.animating() {
		t.Fatal("片付け前に animating が false になった (最後の 1 拍が届かず閉じかけで固まる)")
	}
	v.settleClose()
	if v.animating() {
		t.Fatal("片付けた後も animating が true (tick が回り続ける)")
	}
	if v.visible() {
		t.Fatal("着地しても viewer が閉じていない")
	}
}

// 着地するまで片付けない = 逆再生の間は見張りも本文も生きている。
func TestIssuesCloseDefersTeardownUntilSettled(t *testing.T) {
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }

	root, path := watchTree(t, "# a\n")
	v := openedWatchView(t, root, path)
	v.closeAnimOff = false // このテストだけ演出を on に戻す
	gen := v.watch.gen

	v.close()
	if v.watch.gen != gen {
		t.Fatal("演出中に見張りを畳んだ (逆再生の途中で中身が死ぬ)")
	}
	now = now.Add(issuesAnimDuration)
	v.settleClose()
	if v.watch.gen == gen {
		t.Fatal("着地しても見張りが畳まれていない (watcher が残る)")
	}
}

// 演出の途中で終了しても画面を覚えない (閉じたはずの viewer が次の起動で蘇らない)。
func TestIssuesClosingIsNotRemembered(t *testing.T) {
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }

	v := openAnimView(t)
	if _, ok := v.screen(now); !ok {
		t.Fatal("前提が崩れた: 開いているのに覚えない")
	}
	v.close()
	if _, ok := v.screen(now); ok {
		t.Fatal("閉じる演出の途中を「開いている」として覚えた (次の起動で蘇る)")
	}
}

// キーは演出を即着地させ、かつ飲み込まれない (q で閉じた直後の q が効かない時間を作らない)。
func TestIssuesCloseLandsOnKeyWithoutSwallowing(t *testing.T) {
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.issuesOv.closeAnimOff = false
	m.issuesOv.toggle(t.TempDir())
	m.issuesOv.finishAnim()
	if !m.issuesOv.visible() {
		t.Fatal("前提が崩れた: viewer が開いていない")
	}

	m.issuesOv.close()
	if !m.issuesOv.visible() {
		t.Fatal("前提が崩れた: 演出なしで即閉じた")
	}
	releaseKey(m)
	m.handleKey("q") // 閉じる演出の途中に来たキー
	if m.issuesOv.visible() {
		t.Fatal("キーで演出が着地しない (入力が効かない時間ができる)")
	}
	// ⚠️ 飲み込まれず通常処理まで届く。届かないと「q で閉じた直後の q が効かない」ことになる。
	// newTestBrowse は終了演出も切っているので、q が届いていれば m.done が立つ
	if !m.done {
		t.Fatal("着地させたキーが飲み込まれた (viewer を閉じた直後の q が効かない)")
	}
}

// 演出中の i は「閉じ切ってから開き直す」= 見張りを二重に張らない。
func TestIssuesReopenDuringCloseDoesNotDoubleWatch(t *testing.T) {
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }

	root, path := watchTree(t, "# a\n")
	v := openedWatchView(t, root, path)
	v.closeAnimOff = false
	gen := v.watch.gen

	v.close()
	v.toggle(root) // 逆再生の途中で開き直す
	if !v.visible() {
		t.Fatal("開き直せていない")
	}
	if v.closing {
		t.Fatal("閉じる演出が残ったまま開き直した (次の settleClose が開いた viewer を畳む)")
	}
	if v.watch.gen == gen {
		t.Fatal("前の見張りを畳まずに開き直した (watcher が二重に居座る)")
	}
}
