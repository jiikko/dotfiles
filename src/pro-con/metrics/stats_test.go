package metrics_test

import (
	"testing"
	"time"

	"pro-con/metrics"
)

func row(id string, points int, total time.Duration, stay *metrics.Stays, usd *float64) metrics.Row {
	r := metrics.Row{Card: id, Points: points, Repo: "dotfiles", RequestedAt: t0, ClosedAt: t0.Add(total), Stay: stay}
	if stay != nil {
		r.Human = &metrics.Stays{Waiting: stay.Waiting}
	}
	if usd != nil {
		pct := *usd / metrics.FiveHourUSDPerPct
		r.Usage = &metrics.Usage{USD: usd, FiveHourPct: &pct}
	}
	return r
}

func f(v float64) *float64 { return &v }

// ポイントごとに件数・中央値・最大を出す (偶数件の中央値は真ん中 2 つの平均)。列ごとの時間・枠の無い行は、その値からだけ除く。
func TestSummarizeByPoints(t *testing.T) {
	rows := []metrics.Row{
		row("C-001", 3, 30*time.Minute, &metrics.Stays{Running: 600, Waiting: 60}, f(2)),
		row("C-002", 3, 90*time.Minute, &metrics.Stays{Running: 1800, Waiting: 0}, f(6)),
		row("C-003", 3, 60*time.Minute, nil, nil), // 足跡も枠も無い古い行
		row("C-004", 1, 10*time.Minute, &metrics.Stays{Running: 300}, f(1)),
		row("C-005", 0, 5*time.Minute, &metrics.Stays{}, f(0.5)),
	}
	gs, err := metrics.Summarize(rows, metrics.ByPoints)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 3 || gs[0].Key != "1pt" || gs[1].Key != "3pt" || gs[2].Key != "見積もり無し" {
		t.Fatalf("束ねの並び = %+v", gs)
	}
	g := gs[1]
	if g.N != 3 || g.Total != (metrics.Summary{N: 3, Median: 3600, Max: 5400}) {
		t.Fatalf("所要 = %d %+v", g.N, g.Total)
	}
	if g.Running != (metrics.Summary{N: 2, Median: 1200, Max: 1800}) || g.Human != (metrics.Summary{N: 2, Median: 30, Max: 60}) {
		t.Fatalf("作業中・人の番 = %+v %+v", g.Running, g.Human)
	}
	if g.USD != (metrics.Summary{N: 2, Median: 4, Max: 6}) {
		t.Fatalf("枠 = %+v", g.USD)
	}
}

// 週ごとは閉じた時刻の ISO 週で束ね、Since は閉じた時刻で絞る。知らない束ね方はエラー。
func TestSummarizeByWeekAndSince(t *testing.T) {
	rows := []metrics.Row{row("C-001", 1, time.Hour, nil, nil), row("C-002", 1, 8*24*time.Hour, nil, nil)}
	gs, err := metrics.Summarize(rows, metrics.ByWeek)
	if err != nil || len(gs) != 2 || gs[0].Key != "2026-W39" || gs[1].Key != "2026-W40" {
		t.Fatalf("週ごと = %+v %v", gs, err)
	}
	if got := metrics.Since(rows, t0.Add(2*time.Hour)); len(got) != 1 || got[0].Card != "C-002" {
		t.Fatalf("Since = %+v", got)
	}
	if _, err := metrics.Summarize(rows, "moon"); err == nil {
		t.Fatal("知らない束ね方を受けた")
	}
}
