package oracle

import (
	"context"
	"fmt"
	"time"
)

// TimingRequest は本家の check_pattern と search の所要の測定 (py/time_patterns.py)。行は Prefix を Repeat 回と Suffix。
type TimingRequest struct {
	Patterns []string `json:"patterns"`
	Prefix   string   `json:"prefix"`
	Repeat   int      `json:"repeat"`
	Suffix   string   `json:"suffix"`
}

// PatternTiming はパターン 1 つの所要 (秒)。
type PatternTiming struct {
	Check   float64 `json:"check"`
	Search  float64 `json:"search"`
	Cost    int     `json:"cost"`
	Matched bool    `json:"matched"`
}

// TimingResult は 1 回の Python のプロセスの測定。Inner は driver の中の全体、Import は本家の module の import、Wall は呼び出し側から見た起動から終了までの壁時計。
// 起動の時間は Wall から Inner を引いた値 (インタプリタの起動と、driver が標準入力を読む前)。
type TimingResult struct {
	Inner    float64         `json:"inner"`
	Import   float64         `json:"import"`
	Patterns []PatternTiming `json:"patterns"`
	Wall     time.Duration   `json:"-"`
}

// TimePatterns は本家の module でパターンを check_pattern し、長い行を search する所要を測る。毎回新しいプロセス。
func (r Runner) TimePatterns(ctx context.Context, req TimingRequest) (TimingResult, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return TimingResult{}, err
	}
	defer sb.remove()
	in := struct {
		ScriptsDir string `json:"scripts_dir"`
		TimingRequest
	}{sb.tree.Path(ScriptsDir), req}
	var out TimingResult
	begin := time.Now()
	if err := r.runDriver(ctx, sb, "time_patterns.py", in, &out); err != nil {
		return TimingResult{}, err
	}
	out.Wall = time.Since(begin)
	if len(out.Patterns) != len(req.Patterns) {
		return TimingResult{}, fmt.Errorf("パターン %d 件に対して測定が %d 件でした", len(req.Patterns), len(out.Patterns))
	}
	return out, nil
}
