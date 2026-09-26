package metrics

// 行を束ねて比べる (pro-con stats。issue 516)。束ねごとに件数と、所要・作業中・人の番の中央値と最大、枠の中央値を出す。
// 列ごとの時間の無い行 (足跡の無い古いカード)・枠の取れなかった行は、その数だけを除いて数える (0 として混ぜない)。

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"
)

// 束ね方。
const (
	ByPoints = "points"
	ByRepo   = "repo"
	ByWeek   = "week"
)

// Summary は数の並びの件数・中央値・最大。N が 0 なら中央値と最大は意味を持たない。
type Summary struct {
	N      int
	Median float64
	Max    float64
}

func summarize(xs []float64) Summary {
	if len(xs) == 0 {
		return Summary{}
	}
	s := slices.Clone(xs)
	sort.Float64s(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return Summary{N: len(s), Median: m, Max: s[len(s)-1]}
}

// Group は束ね 1 つ。時間は秒。
type Group struct {
	Key     string
	N       int
	Total   Summary // 依頼を受けてから閉じるまで
	Running Summary // 作業中の列に居た時間 (足跡のある行だけ)
	Human   Summary // 人の番だった時間 (足跡のある行だけ)
	USD     Summary // 枠の API 料金換算 (取れた行だけ)
	Pct     Summary // 5 時間枠の % (取れた行だけ)
}

// Summarize は rows を by で束ねる (by が空なら全体で 1 つ)。束ねの並びはポイント・週は小さい順、repo は名前の順。
func Summarize(rows []Row, by string) ([]Group, error) {
	key := func(Row) string { return "全体" }
	switch by {
	case "":
	case ByPoints:
		key = func(r Row) string {
			if r.Points == 0 {
				return "見積もり無し"
			}
			return strconv.Itoa(r.Points) + "pt"
		}
	case ByRepo:
		key = func(r Row) string {
			if r.Repo == "" {
				return "(repo 無し)"
			}
			return r.Repo
		}
	case ByWeek:
		key = func(r Row) string {
			y, w := r.ClosedAt.ISOWeek()
			return fmt.Sprintf("%d-W%02d", y, w)
		}
	default:
		return nil, fmt.Errorf("束ね方 %q を知らない (points / repo / week)", by)
	}
	type acc struct {
		order                          int // ポイントの並び (見積もり無しは最後)
		total, running, human, usd, pc []float64
		n                              int
	}
	groups := map[string]*acc{}
	for _, r := range rows {
		k := key(r)
		a := groups[k]
		if a == nil {
			a = &acc{order: r.Points}
			if by == ByPoints && r.Points == 0 {
				a.order = 1 << 30
			}
			groups[k] = a
		}
		a.n++
		a.total = append(a.total, r.ClosedAt.Sub(r.RequestedAt).Seconds())
		if r.Stay != nil {
			a.running = append(a.running, float64(r.Stay.Running))
		}
		if r.Human != nil {
			a.human = append(a.human, float64(r.Human.Total()))
		}
		if r.Usage != nil && r.Usage.USD != nil {
			a.usd = append(a.usd, *r.Usage.USD)
			a.pc = append(a.pc, *r.Usage.FiveHourPct)
		}
	}
	out := make([]Group, 0, len(groups))
	for k, a := range groups {
		out = append(out, Group{Key: k, N: a.n, Total: summarize(a.total), Running: summarize(a.running), Human: summarize(a.human),
			USD: summarize(a.usd), Pct: summarize(a.pc)})
	}
	sort.Slice(out, func(i, j int) bool {
		if by == ByPoints && groups[out[i].Key].order != groups[out[j].Key].order {
			return groups[out[i].Key].order < groups[out[j].Key].order
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

// Since は rows のうち閉じた時刻が from 以降のもの。
func Since(rows []Row, from time.Time) []Row {
	var out []Row
	for _, r := range rows {
		if !r.ClosedAt.Before(from) {
			out = append(out, r)
		}
	}
	return out
}
