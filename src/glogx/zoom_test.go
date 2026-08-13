package main

import (
	"strings"
	"testing"
	"time"
)

// 演出の中間フレームは「中央に置かれた小さい枠 + 実画面の左上部分」になる
// (左上アンカーの理由は zoomWindow の doc。最初のフレームから 1 行目の文字が見える)。
func TestZoomWindowShrinksToCenter(t *testing.T) {
	const w, h = 60, 12
	lines := make([]string, h)
	for i := range lines {
		lines[i] = strings.Repeat("x", w)
	}
	lines[0] = "TOPLEFT" + strings.Repeat("x", w-7)
	lines[h/2] = strings.Repeat("x", w/2-3) + "MIDDLE" + strings.Repeat("x", w/2-3)

	small := zoomWindow(lines, 0.4, w, false, true)
	if len(small) != h {
		t.Fatalf("行数が変わった: %d (want %d)", len(small), h)
	}
	for i, ln := range small {
		if got := dispWidth(ln); got > w {
			t.Fatalf("行 %d が幅を超えた (w=%d): %q", i, got, ln)
		}
	}
	joined := strings.Join(small, "\n")
	if !strings.Contains(joined, "╔") || !strings.Contains(joined, "║") {
		t.Fatalf("枠が描かれていない:\n%s", joined)
	}
	// 中身は実画面の左上から切り出す (1 行目の文字が最初のフレームから見える)
	if !strings.Contains(joined, "TOPLEFT") {
		t.Fatalf("左上の中身が切り出されていない:\n%s", joined)
	}
	// 上下の端は空く (中央に寄っている)
	if strings.TrimSpace(small[0]) != "" || strings.TrimSpace(small[h-1]) != "" {
		t.Fatalf("中央に寄っていない: 先頭=%q 末尾=%q", small[0], small[h-1])
	}
	// 小さいほど枠も小さい
	wide := zoomWindow(lines, 0.8, w, false, true)
	if boxWidth(t, wide) <= boxWidth(t, small) {
		t.Fatal("進捗が進んでも枠が広がっていない")
	}
}

// boxWidth は演出フレームの枠の表示幅 (いちばん長い行)。
func boxWidth(t *testing.T, lines []string) int {
	t.Helper()
	w := 0
	for _, ln := range lines {
		w = max(w, dispWidth(strings.TrimRight(ln, " ")))
	}
	return w
}

// 開き切ったら実画面をそのまま返す (演出の枠と本物の枠が二重に見えないように)。
func TestZoomWindowPassesThroughWhenOpen(t *testing.T) {
	lines := []string{"a", "b", "c"}
	got := zoomWindow(lines, 1, 40, false, true)
	if len(got) != len(lines) || got[0] != "a" {
		t.Fatalf("開き切っているのに変換した: %+v", got)
	}
}

// 起動は開く演出から始まり、時間で着地する。
func TestAppZoomOpensAndSettles(t *testing.T) {
	now := time.Unix(1000, 0)
	var z appZoom
	if z.scale(now) != 1 {
		t.Fatal("zero value は演出なし (実画面) のはず")
	}
	z.start(now)
	if !z.animating(now) || z.scale(now) != 0 {
		t.Fatalf("開始直後が点になっていない: animating=%v scale=%v", z.animating(now), z.scale(now))
	}
	mid := now.Add(appZoomDuration / 2)
	if s := z.scale(mid); s <= 0 || s >= 1 {
		t.Fatalf("途中の大きさが 0..1 でない: %v", s)
	}
	end := now.Add(appZoomDuration)
	if z.animating(end) {
		t.Fatal("所要を過ぎても演出中のまま")
	}
	if closed := z.settle(end); closed {
		t.Fatal("開く演出の着地を「閉じ切った」と誤判定した")
	}
	if z.scale(end) != 1 {
		t.Fatal("着地後も実画面になっていない")
	}
}

// 終了は演出を挟んでから抜ける。⚠️ キーが来たら即着地させる (q が効かない時間を作らない)。
func TestAppZoomCloseThenQuit(t *testing.T) {
	advance := stubClock(t)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.zoom.off = false // このテストだけ演出を有効にする (ヘルパーは既定で切る)

	model, cmd := m.quit()
	if m.done {
		t.Fatal("演出の前に終了した (吸い込まれる姿が 1 フレームも出ない)")
	}
	if cmd == nil {
		t.Fatal("演出を進める tick が返らない")
	}
	if !m.zoom.closing() {
		t.Fatal("閉じる演出に入っていない")
	}
	// 後始末は演出の前に済ませる (演出中に端末を閉じられても止まっている)
	if m.actModal.cancel != nil {
		t.Error("走行中 subprocess の後始末が演出待ちになっている")
	}
	_ = model

	// 着地したら終了する
	advance(appZoomDuration)
	m.Update(tickMsg{})
	if !m.done {
		t.Fatal("演出が着地しても終了しない")
	}
}

// キーで即着地 (待たされない)。
func TestAppZoomCloseFinishesOnKey(t *testing.T) {
	stubClock(t)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.zoom.off = false
	m.quit()
	if m.done {
		t.Fatal("前提が崩れた: 演出なしで終了した")
	}
	m.handleKey("j") // 演出中の任意キー
	if !m.done {
		t.Fatal("キーを押しても演出が着地せず終了しない (q が効かない時間ができる)")
	}
}

// Ctrl-C は演出なしで即終了する (緊急脱出は最短)。
func TestCtrlCSkipsZoom(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.zoom.off = false
	m.handleKey("ctrl+c")
	if !m.done {
		t.Fatal("Ctrl-C で即終了しない")
	}
	if m.zoom.closing() {
		t.Fatal("Ctrl-C で演出に入った (緊急脱出が待たされる)")
	}
}

// 枠を持たない画面 (--no-frame / 小さい端末) では演出も枠を描かない。
// ⚠️ 描くと、開き切った瞬間に枠が消える段差が出る。
func TestZoomWindowMatchesFrameState(t *testing.T) {
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = strings.Repeat("x", 40)
	}
	if out := strings.Join(zoomWindow(lines, 0.5, 40, false, false), "\n"); strings.Contains(out, "╔") {
		t.Fatalf("枠なしの画面で演出だけ枠を描いた:\n%s", out)
	}
	if out := strings.Join(zoomWindow(lines, 0.5, 40, false, true), "\n"); !strings.Contains(out, "╔") {
		t.Fatalf("枠つきの画面で演出の枠が出ない:\n%s", out)
	}
}

// 演出中はフレーム周期を上げる。⚠️ ここが効いていないと「チェーンは回るが 12.5fps」になり、
// 220ms の演出に中間フレームが 2 枚しか出ず (4行 → 30行 → 実画面) 点滅に見える (実測 2026-08-01)。
// 固定値でなく「何枚出るか」で縛るのは、所要 (appZoomDuration) を変えたときに一緒に守るため。
func TestZoomRendersEnoughFrames(t *testing.T) {
	stubClock(t)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.zoom.off = false
	m.zoom.start(timeNow())

	interval := m.tickInterval()
	if interval >= spinnerInterval {
		t.Fatalf("演出中もスピナーと同じ周期 (%v): 中間フレームが消えて点滅に見える", interval)
	}
	// 実画面へ寄り切る (appZoomSnap) までに何フレーム出るかを数える
	shown := 0
	for tt := time.Duration(0); tt < appZoomDuration; tt += interval {
		if m.zoom.scale(timeNow().Add(tt)) < appZoomSnap {
			shown++
		}
	}
	if shown < 8 {
		t.Fatalf("開く演出の中間フレームが %d 枚しかない (滑らかに見えない)", shown)
	}
}

// 演出が終われば周期は元へ戻る (常時 60fps で回し続けない)。
func TestZoomFrameRateDropsAfterSettle(t *testing.T) {
	advance := stubClock(t)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.zoom.off = false
	m.zoom.start(timeNow())
	advance(appZoomDuration)
	if got := m.tickInterval(); got == zoomInterval {
		t.Fatal("演出が終わっても 60fps で回り続けている (CPU を無駄に食う)")
	}
}

// 演出は所要いっぱいまで動く。⚠️ 素の easeOutCubic は進捗 69% で appZoomSnap に達してしまい、
// 残り 31% は絵が変わらない (開くときは早々に静止、閉じるときは 68ms 何も起きてから縮み始める)。
// scale が終点を snap 閾値に合わせているかを、値でなく「最後まで動くか」で縛る。
func TestZoomMovesForWholeDuration(t *testing.T) {
	var z appZoom
	now := time.Unix(1000, 0)
	z.start(now)

	// 終端の直前でもまだ変形している (= 死に時間がない)
	almost := now.Add(appZoomDuration - time.Millisecond)
	if s := z.scale(almost); s >= appZoomSnap {
		t.Fatalf("所要の終わり際に絵が止まっている (scale=%v ≥ snap=%v)", s, appZoomSnap)
	}
	// 閉じるときも同じ (開始直後に「何も起きない時間」を作らない)
	var c appZoom
	c.startClose(now)
	justAfter := now.Add(appZoomDuration / 20) // 所要の 5% 経過
	if s := c.scale(justAfter); s >= appZoomSnap {
		t.Fatalf("閉じ始めに何も起きない時間がある (scale=%v ≥ snap=%v)", s, appZoomSnap)
	}
}

// フレームあたりの跳びが小さいこと。⚠️ 端末は文字セル単位でしか動けないので、フレーム数だけ
// 増やしても曲線が前のめりだと 1 フレームで何行も跳ぶ。40 行の画面を想定して平均の跳びを見る。
func TestZoomStepsAreSmall(t *testing.T) {
	stubClock(t)

	// ⚠️ 開くときと閉じるときを同じ基準で見る。片方だけ直すと「開くのは滑らかなのに終了だけ
	// カクつく」ずれが入る (周期・所要・曲線の 3 つとも両方向へ効いている必要がある)。
	for _, tc := range []struct {
		name  string
		begin func(*appZoom)
	}{
		{"開く", func(z *appZoom) { z.start(timeNow()) }},
		{"閉じる", func(z *appZoom) { z.startClose(timeNow()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.zoom.off = false
			tc.begin(&m.zoom)
			interval := m.tickInterval()
			if interval >= spinnerInterval {
				t.Fatalf("周期が上がっていない (%v): 中間フレームが消える", interval)
			}

			const rows = 40
			prev, total, steps := -1, 0, 0
			for tt := time.Duration(0); tt <= appZoomDuration; tt += interval {
				s := m.zoom.scale(timeNow().Add(tt))
				if s >= appZoomSnap {
					if prev < 0 {
						continue // 閉じ始めの 1 フレームは実画面 (ここが起点)
					}
					break
				}
				h := max(int(float64(rows)*s+0.5), appZoomMinRows)
				if prev >= 0 {
					total += max(h-prev, prev-h)
					steps++
				}
				prev = h
			}
			if steps == 0 {
				t.Fatal("動くフレームが 1 枚も無い")
			}
			if avg := float64(total) / float64(steps); avg > 2.2 {
				t.Fatalf("1 フレームで平均 %.1f 行も跳んでいる (カクついて見える。%d 枚)", avg, steps)
			}
		})
	}
}
