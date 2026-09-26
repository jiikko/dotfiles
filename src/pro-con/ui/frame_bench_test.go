package ui

import (
	"fmt"
	"testing"
	"time"

	"pro-con/card"
)

// 演出の 1 コマ (View) の時間を、揺れ・カードの移動・枠の滑走に分けて測る (issue 524 / 523 の段 2 の材料)。
// 幅計算の速い道 (termwidth の ASCII + SGR + 記号) を外れる日本語の題名を各レーンに置く: 実際の画面の形で、
// ansi の走査へ落ちる行を含める。
//
//	go test -run '^$' -bench BenchmarkFrame -benchmem -count 10 ./ui/ | tee new.txt
//
// 時刻は演出の所要の中で回し、Update は呼ばない (所要を過ぎると演出が片付き、2 周目から素の画面を測ることになる)。

// benchModel は日本語の題名のカードを各レーンに数枚ずつ置いた、幅 200 × 高さ 50 の画面。
func benchModel() (*Model, *spy, *clock) {
	be := newSpy()
	titles := []string{"端末の幅の計算を共通の層へ寄せる", "演出のコマの確保量を測り直す", "fix: 請求計算の境界条件を是正する", "README の索引を足す"}
	for i, st := range []card.State{card.Planned, card.Planned, card.Running, card.Done, card.Done, card.Done} {
		be.snap.Cards = append(be.snap.Cards, card.Card{ID: fmt.Sprintf("B%d", i), State: st, Since: be.snap.Now, Repo: "dotfiles"})
	}
	for i := range be.snap.Cards { // newSpy の R1 / W1 / W2 にも題名を付ける
		be.snap.Cards[i].Title = titles[i%len(titles)]
	}
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 200, 50
	m.Update(frameMsg{})
	return m, be, clk
}

// runFrames は start から dur の中を frameInterval ずつ進めながら View を測る。
func runFrames(b *testing.B, m *Model, clk *clock, dur time.Duration) {
	b.Helper()
	start := clk.t
	n := int(dur / frameInterval)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		clk.t = start.Add(time.Duration(i%n) * frameInterval)
		_ = m.View()
		i++
	}
}

// 作業中のレーンの上端で k: レーンごと揺れる (bump.go の Cut / fit を毎コマ全行で通る)。
func BenchmarkFrameBump(b *testing.B) {
	m, _, clk := benchModel()
	m.selected = "R1"
	m.Update(frameMsg{})
	for range 5 { // 上端へ着いてから、もう 1 回で揺れる
		if press(m, "k"); m.bumping(clk.t) {
			break
		}
	}
	if !m.bumping(clk.t) {
		b.Fatal("前提: 揺れが始まっていない")
	}
	runFrames(b, m, clk, bumpDuration)
}

// 分解済みから作業中へ移ったカードが列のあいだを滑る (motion.go の splice)。
func BenchmarkFrameMove(b *testing.B) {
	m, be, clk := benchModel()
	m.resetSlots()
	for i := range be.snap.Cards {
		if be.snap.Cards[i].ID == "B0" {
			be.snap.Cards[i].State = card.Running
		}
	}
	m.Update(tickMsg{})
	if _, ok := m.moves["B0"]; !ok {
		b.Fatal("前提: 移動の演出が始まっていない")
	}
	runFrames(b, m, clk, animDuration())
}

// 選択の枠がレーンを跨いで滑る (cursor.go の cellsOf / softEdge / softSide)。
func BenchmarkFrameGlide(b *testing.B) {
	m, _, clk := benchModel()
	m.selected = "R1"
	m.Update(frameMsg{})
	press(m, "l")
	if !m.cursorGliding(clk.t) {
		b.Fatal("前提: 枠が滑り始めていない")
	}
	runFrames(b, m, clk, cursorDuration)
}
