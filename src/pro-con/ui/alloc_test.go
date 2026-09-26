package ui

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"pro-con/card"
)

// frameBytes は完了のカードを n 枚置いた画面で、揺れの 1 コマ (Update + View) が確保するバイト数 (10 コマの平均)。
// repo を渡すと、すべてのカードをその repo にしてタブを選ぶ (タブを選んでいるときだけ通る絞り込みの経路も測る)。
func frameBytes(t *testing.T, n int, repo string) uint64 {
	t.Helper()
	be := newSpy()
	for i := range be.snap.Cards {
		be.snap.Cards[i].Repo = repo
	}
	for i := range n {
		be.snap.Cards = append(be.snap.Cards, card.Card{ID: fmt.Sprintf("D%d", i), State: card.Done, Since: be.snap.Now, Repo: repo})
	}
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 200, 50
	m.tab = repo
	m.Update(frameMsg{})
	press(m, "k") // 作業中のレーンの上端でぶつかる (揺れの間は毎コマ描き直す)
	start := clk.t
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	const frames = 10
	for i := range frames {
		clk.t = start.Add(time.Duration(i) * frameInterval)
		m.Update(frameMsg{})
		_ = m.View()
	}
	runtime.ReadMemStats(&b)
	return (b.TotalAlloc - a.TotalAlloc) / frames
}

// 演出の 1 コマの確保量はカードの枚数で増えない (issue 494)。見えないカードまで毎コマ複製・描画すると GC が回り続けて
// 揺れやカードの移動がカクついた。時間ではなく確保量の伸び率で見る。実測 (2026-09-26): 直す前は 50 → 500 枚で 4.75 倍、
// 直した後は 1.06 倍。
// 見ていないもの: 1 コマの外の経路 (キー操作・Snapshot の差し替えで呼ぶ columns() は全枚数を複製する) / 1 枚あたり数十バイト以下の
// 比例 (固定費に埋もれて 1.3 倍に届かない) / 枚数によらない大きな確保 (比では原理的に見えない) / 移動中のカードの点線の枠の経路
func TestFrameAllocDoesNotGrowWithCards(t *testing.T) {
	for _, repo := range []string{"", "dotfiles"} { // global とタブを選んだとき
		small, large := frameBytes(t, 50, repo), frameBytes(t, 500, repo)
		if r := float64(large) / float64(small); r > 1.3 {
			t.Fatalf("タブ %q: カード 50 → 500 枚で 1 コマの確保量が %.2f 倍 (%d → %d バイト)。カードの枚数に比例する複製・描画が毎コマの経路に入った", repo, r, small, large)
		}
	}
}
