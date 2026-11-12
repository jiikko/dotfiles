// Command worstcase は flag-patterns.txt の試す数の上限 (PATTERNS_MAX_COST = 512) の最悪の組を、本家 (Python の re) と Go (internal/pyre) で測る。手で打つ道具で、テストの合否には使わない。
//
// 最悪の組: 10 万字の行 (a が 99,999 字と d) に、最短一致の繰り返しを重ねたパターン (a{0,7}?[ab]{0,7}?[ac]{0,7}?d: 試す数 8*8*8 = 512) を search する。
// どの開始位置でも 512 通りの切り方を全部試して、行の終わりの d でしか当たらない。同じ入力を Python と Go で交互に複数回測り、
// 起動 (インタプリタ・バイナリの起動と module の初期化。壁時計から中の所要を引いた値) と、check_pattern (compile) と、search を分けて出す。
//
//	worstcase [-rounds N] [-shape single|word|greedy|merged|split]
//	worstcase -child -shape ...   (Go 側の測定の本体。親が子プロセスとして起こす)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"subproc"

	"ja-writing/internal/pyre"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

const (
	lineRepeat = 99999
	lineSuffix = "d"
)

// shapes は測るパターンの組。どれも試す数の合計が上限 512 ちょうど。
var shapes = map[string][]string{
	// 1 つのパターンが上限いっぱい (8*8*8)。同じ字の繰り返し (a{0,7}?a{0,7}?) は regexp2 が 1 つの繰り返しに畳んで後戻りが減るので、字の集合を変えて畳めなくする。
	"single": {"a{0,7}?[ab]{0,7}?[ac]{0,7}?d"},
	// \w の繰り返し (pyre が Python の \w の字の集合を範囲の列に開くので、集合の大きい形)。
	"word": {"\\w{0,7}?[a-z]{0,7}?\\S{0,7}?d"},
	// 最短一致でなく最長一致。
	"greedy": {"a{0,7}[ab]{0,7}[ac]{0,7}d"},
	// 同じ字の繰り返しのまま (regexp2 が畳める形)。
	"merged": {"a{0,7}?a{0,7}?a{0,7}?d"},
	// 試す数 64 のパターン 8 つで合計 512 (全部のパターンが全部の行を走る)。
	"split": {
		"a{0,7}?[ab]{0,7}?d", "[ab]{0,7}?a{0,7}?d", "a{0,7}?[ac]{0,7}?d", "[ac]{0,7}?a{0,7}?d",
		"a{0,7}?[ad]{0,7}?d", "[ad]{0,7}?a{0,7}?d", "a{0,7}?[ae]{0,7}?d", "[ae]{0,7}?a{0,7}?d",
	},
}

// childResult は Go の子プロセスの中の所要 (秒)。
type childResult struct {
	Inner    float64                `json:"inner"`
	Patterns []oracle.PatternTiming `json:"patterns"`
}

func main() {
	child := flag.Bool("child", false, "Go 側の測定の本体 (標準出力に JSON)")
	rounds := flag.Int("rounds", 5, "Python と Go を交互に測る回数")
	shape := flag.String("shape", "single", "測るパターンの組 (single・word・greedy・merged・split)")
	flag.Parse()
	patterns, ok := shapes[*shape]
	if !ok {
		fmt.Fprintf(os.Stderr, "worstcase: 未知の組 %q\n", *shape)
		os.Exit(2)
	}
	if *child {
		out, err := measureGo(patterns)
		if err != nil {
			fmt.Fprintf(os.Stderr, "worstcase: %v\n", err)
			os.Exit(1)
		}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "worstcase: %v\n", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := parent(ctx, patterns, *rounds, *shape)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "worstcase: %v\n", err)
		os.Exit(1)
	}
}

func measureGo(patterns []string) (childResult, error) {
	begin := time.Now()
	line := pyre.NewText(strings.Repeat("a", lineRepeat) + lineSuffix)
	var rows []oracle.PatternTiming
	for _, p := range patterns {
		start := time.Now()
		checked, err := visibility.CheckPattern(p)
		if err != nil {
			return childResult{}, fmt.Errorf("%q: %w", p, err)
		}
		check := time.Since(start).Seconds()
		start = time.Now()
		m, err := checked.Compiled.Search(line, 0, pyre.NoEnd)
		if err != nil {
			return childResult{}, err
		}
		rows = append(rows, oracle.PatternTiming{Check: check, Search: time.Since(start).Seconds(), Cost: checked.Cost, Matched: m != nil})
	}
	return childResult{Inner: time.Since(begin).Seconds(), Patterns: rows}, nil
}

type sample struct{ startup, check, search float64 }

// newSample は 1 回の実行の測定 (起動は壁時計から中の所要を引いた値。check と search はパターンごとの所要の合計)。
func newSample(wall, inner float64, rows []oracle.PatternTiming) sample {
	s := sample{startup: wall - inner}
	for _, r := range rows {
		s.check += r.Check
		s.search += r.Search
	}
	return s
}

func parent(ctx context.Context, patterns []string, rounds int, shape string) error {
	cache, err := oracle.DefaultCacheRoot()
	if err != nil {
		return err
	}
	tree, err := oracle.Fetch(ctx, cache, oracle.HTTPGetter)
	if err != nil {
		return err
	}
	runner, err := oracle.NewRunner(tree)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	req := oracle.TimingRequest{Patterns: patterns, Prefix: "a", Repeat: lineRepeat, Suffix: lineSuffix}
	var py, goes []sample
	var pyImport []float64
	total := 0
	for _, p := range patterns {
		checked, err := visibility.CheckPattern(p)
		if err != nil {
			return fmt.Errorf("%q: %w", p, err)
		}
		total += checked.Cost
	}
	runPython := func() error {
		res, err := runner.TimePatterns(ctx, req)
		if err != nil {
			return err
		}
		py = append(py, newSample(res.Wall.Seconds(), res.Inner, res.Patterns))
		pyImport = append(pyImport, res.Import)
		return nil
	}
	runGo := func() error {
		begin := time.Now()
		cmd := subproc.CommandContext(ctx, exe, "-child", "-shape", shape)
		out, err := cmd.Output()
		wall := time.Since(begin).Seconds()
		if err != nil {
			return fmt.Errorf("子プロセス (Go): %w", err)
		}
		var res childResult
		if err := json.Unmarshal(out, &res); err != nil {
			return err
		}
		goes = append(goes, newSample(wall, res.Inner, res.Patterns))
		return nil
	}
	for i := range rounds {
		first, second := runPython, runGo
		if i%2 == 1 {
			first, second = runGo, runPython
		}
		if err := first(); err != nil {
			return err
		}
		if err := second(); err != nil {
			return err
		}
	}
	fmt.Printf("組 %s: パターン %d 個・試す数の合計 %d・行 %d 字 (a が %d 字と %s)・%d 回ずつ交互 (奇数回目は Go を先に起こす)\n", shape, len(patterns), total, lineRepeat+len(lineSuffix), lineRepeat, lineSuffix, rounds)
	report := func(name string, rows []sample) {
		col := func(f func(sample) float64) string {
			v := make([]float64, len(rows))
			for i, r := range rows {
				v[i] = f(r)
			}
			slices.Sort(v)
			return fmt.Sprintf("最小 %.3f / 中央 %.3f / 最大 %.3f", v[0], v[len(v)/2], v[len(v)-1])
		}
		fmt.Printf("%s\n  起動: %s 秒\n  check_pattern: %s 秒\n  search: %s 秒\n", name, col(func(s sample) float64 { return s.startup }), col(func(s sample) float64 { return s.check }), col(func(s sample) float64 { return s.search }))
	}
	report("Python (本家の re)", py)
	imp := slices.Clone(pyImport)
	slices.Sort(imp)
	fmt.Printf("  (起動のうち markdown_visibility の import: 最小 %.3f / 中央 %.3f 秒)\n", imp[0], imp[len(imp)/2])
	report("Go (internal/pyre)", goes)
	return nil
}
