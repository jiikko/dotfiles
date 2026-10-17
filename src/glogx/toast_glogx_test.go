package main

import (
	"strings"
	"testing"

	"glogx/internal/term"

	"github.com/jiikko/dotfiles/src/tuikit/toast"
)

func TestToastDrawBudget(t *testing.T) {
	for _, c := range []struct {
		page int
		want int
	}{
		{4, 4},
		{8, 7},
		{9, 8},
		{11, 8},
		{15, 8},
		{16, 8},
		{24, 12},
	} {
		if got := toastDrawBudget(c.page, 0); got != c.want {
			t.Errorf("page=%d: 予算=%d, want %d", c.page, got, c.want)
		}
	}
}

func TestToastBoxLinesDoesNotCutSecondBoxAtPageEightBudget(t *testing.T) {
	var s toast.Stack
	s.Show("警告A", false)
	s.Show("警告B", false)
	for range toast.SlideFrames + 2 {
		s.Advance()
	}

	budget := toastDrawBudget(8, s.ReservedHeight(2, 0))
	got := s.BoxLines(false, budget, 0)
	if len(got) > budget {
		t.Fatalf("page=8 の予算 %d 行を超えた: %d 行", budget, len(got))
	}
	if len(got)%toast.BoxHeight != 0 {
		t.Fatalf("箱が途中で切れている: %d 行", len(got))
	}
	if strings.Contains(strings.Join(got, "\n"), "警告A") {
		t.Fatalf("page=8 相当では 2 枚目の警告まで描かれた:\n%s", strings.Join(got, "\n"))
	}
}

// 通知の箱の落ち影は、ほかの板と同じ glogx の影の色 (term.AnsiShadowFg)。github.com/jiikko/dotfiles/src/tuikit/toast へ切り出してから、影の色は画面のモデルを作るときに
// 渡す配線になった。🚨 今は term.AnsiShadowFg が layout.Panel の既定 (layout.ShadowNearBlack) と同じ値なので、渡し忘れても見た目は変わらない
// (配線を外す変異は等価。2026-09-25 に確かめた)。glogx のテーマで影の色を変えたとき、通知の箱だけが既定の色に取り残されないことを守る
func TestToastShadowIsGlogxNearBlack(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)
	m.toast.Show("pulled", true)
	for range toast.SlideFrames + 2 {
		m.toast.Advance()
	}
	joined := strings.Join(m.toast.BoxLines(true, 20, 0), "\n")
	if !strings.Contains(joined, term.AnsiShadowFg+"█") && !strings.Contains(joined, term.AnsiShadowFg+"▓") {
		t.Fatalf("通知の箱の落ち影が term.AnsiShadowFg でない:\n%q", joined)
	}
}
